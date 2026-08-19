package transport

import (
	"bufio"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ClientTunnel 客户端隧道传输层。
// 它连接服务端 53 端口，在一个 TCP 连接上多路复用多个会话，
// 每个会话对应一个本地 SOCKS5 客户端连接。
type ClientTunnel struct {
	conn     net.Conn
	reader   *bufio.Reader
	cipher   *Cipher
	writeMu  sync.Mutex
	sessions sync.Map // uint32 -> *ClientSession
	nextID   uint32
	closed   atomic.Bool
	closeCh  chan struct{}
}

// ClientSession 表示隧道上的一个会话，实现了 net.Conn 接口。
type ClientSession struct {
	t    *ClientTunnel
	id   uint32
	pr   *io.PipeReader
	pw   *io.PipeWriter
	recv chan []byte
	mu   sync.Mutex
	dead bool
	once sync.Once
}

const sessionQueueSize = 64

// Dial 连接服务端地址并完成认证握手。
// password 为空表示明文模式。
func Dial(addr, password string) (*ClientTunnel, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	t := &ClientTunnel{
		conn:    conn,
		reader:  bufio.NewReader(conn),
		cipher:  NewCipher(password),
		nextID:  1,
		closeCh: make(chan struct{}),
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	// 发送握手帧（口令哈希作为认证标识）
	if err := WriteFrame(conn, t.cipher, Frame{Type: FrameHandshake, ConnID: 0, Payload: t.authPayload()}); err != nil {
		conn.Close()
		return nil, err
	}
	// 等待服务端握手确认
	ack, err := ReadFrame(t.reader, t.cipher)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if ack.Type != FrameHandshake || string(ack.Payload) != "OK" {
		conn.Close()
		return nil, errors.New("transport: 服务端握手失败，口令可能不一致")
	}
	_ = conn.SetDeadline(time.Time{})

	go t.readLoop()
	return t, nil
}

// authPayload 生成握手认证负载（口令 SHA256 摘要）。
func (t *ClientTunnel) authPayload() []byte {
	// 复用 Cipher 的密钥派生逻辑，保证两端一致
	c := t.cipher
	if c == nil {
		return []byte("noauth")
	}
	return c.key
}

// Open 创建一个新会话并分配连接 ID。
func (t *ClientTunnel) Open() (*ClientSession, error) {
	if t.closed.Load() {
		return nil, ErrTunnelClosed
	}
	pr, pw := io.Pipe()
	id := atomic.AddUint32(&t.nextID, 1)
	s := &ClientSession{t: t, id: id, pr: pr, pw: pw, recv: make(chan []byte, sessionQueueSize)}
	t.sessions.Store(id, s)
	if t.closed.Load() {
		t.sessions.Delete(id)
		s.closeIncoming()
		return nil, ErrTunnelClosed
	}
	go s.pump()
	return s, nil
}

// readLoop 持续读取隧道帧并分发到对应会话。
func (t *ClientTunnel) readLoop() {
	for {
		f, err := ReadFrame(t.reader, t.cipher)
		if err != nil {
			t.closeAll()
			return
		}
		switch f.Type {
		case FrameSocks:
			if v, ok := t.sessions.Load(f.ConnID); ok {
				s := v.(*ClientSession)
				// 写入 Pipe 写端；若隧道已关闭则丢弃
				select {
				case <-t.closeCh:
					return
				default:
				}
				if !s.enqueue(f.Payload) {
					s.Close()
				}
			}
		case FrameClose:
			if v, ok := t.sessions.Load(f.ConnID); ok {
				s := v.(*ClientSession)
				t.sessions.Delete(f.ConnID)
				s.closeIncoming()
			}
		}
	}
}

// writeSession 将一个会话的数据封装为帧写入隧道。
func (t *ClientTunnel) writeSession(id uint32, p []byte) error {
	if t.closed.Load() {
		return ErrTunnelClosed
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	return WriteFrame(t.conn, t.cipher, Frame{Type: FrameSocks, ConnID: id, Payload: p})
}

// closeSession 关闭指定会话并向服务端发送关闭帧。
func (t *ClientTunnel) closeSession(id uint32) {
	if _, ok := t.sessions.LoadAndDelete(id); !ok {
		return
	}
	if !t.closed.Load() {
		t.writeMu.Lock()
		_ = WriteFrame(t.conn, t.cipher, Frame{Type: FrameClose, ConnID: id})
		t.writeMu.Unlock()
	}
}

// closeAll 隧道关闭时清理所有会话。
func (t *ClientTunnel) closeAll() {
	if !t.closed.CompareAndSwap(false, true) {
		return
	}
	close(t.closeCh)
	t.sessions.Range(func(k, v interface{}) bool {
		s := v.(*ClientSession)
		s.closeIncoming()
		t.sessions.Delete(k)
		return true
	})
	t.conn.Close()
}

// Read 实现 io.Reader。
func (s *ClientSession) Read(p []byte) (int, error) { return s.pr.Read(p) }

// Write 实现 io.Writer，将数据封装为帧发出。
func (s *ClientSession) Write(p []byte) (int, error) {
	if err := s.t.writeSession(s.id, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close 关闭会话。
func (s *ClientSession) Close() error {
	var err error
	s.once.Do(func() {
		s.t.closeSession(s.id)
		s.closeIncoming()
		err = s.pr.Close()
	})
	return err
}

func (s *ClientSession) enqueue(p []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dead {
		return false
	}
	select {
	case s.recv <- p:
		return true
	default:
		return false
	}
}

func (s *ClientSession) closeIncoming() {
	s.mu.Lock()
	if !s.dead {
		s.dead = true
		close(s.recv)
	}
	s.mu.Unlock()
}

func (s *ClientSession) pump() {
	defer s.pw.Close()
	for p := range s.recv {
		if _, err := s.pw.Write(p); err != nil {
			return
		}
	}
}

// Close 主动关闭隧道连接并清理所有会话。
func (t *ClientTunnel) Close() error {
	t.closeAll()
	return nil
}

// Done 返回一个在隧道关闭时被关闭的通道。
func (t *ClientTunnel) Done() <-chan struct{} { return t.closeCh }

// LocalAddr 实现 net.Conn。
func (s *ClientSession) LocalAddr() net.Addr { return s.t.conn.LocalAddr() }

// RemoteAddr 实现 net.Conn。
func (s *ClientSession) RemoteAddr() net.Addr { return s.t.conn.RemoteAddr() }

// SetDeadline 实现 net.Conn（隧道级 deadline 略过）。
func (s *ClientSession) SetDeadline(t time.Time) error { return nil }

// SetReadDeadline 实现 net.Conn。
func (s *ClientSession) SetReadDeadline(t time.Time) error { return nil }

// SetWriteDeadline 实现 net.Conn。
func (s *ClientSession) SetWriteDeadline(t time.Time) error { return nil }
