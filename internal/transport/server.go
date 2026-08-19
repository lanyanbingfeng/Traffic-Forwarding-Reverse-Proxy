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

// ServerTunnel 服务端隧道传输层。
// 接受一个已建立连接的客户端隧道，验证握手后多路复用会话，
// 每个会话由 onSession 回调绑定一个真实出站连接（SOCKS5 服务端逻辑）。
type ServerTunnel struct {
	conn      net.Conn
	reader    *bufio.Reader
	cipher    *Cipher
	writeMu   sync.Mutex
	sessions  sync.Map // uint32 -> *ServerSession
	closed    atomic.Bool
	closeCh   chan struct{}
	onSession func(s *ServerSession)
}

// ServerSession 服务端会话，实现 net.Conn 接口。
// Read 来自隧道（客户端发来的数据），Write 发往隧道（回传给客户端）。
type ServerSession struct {
	t    *ServerTunnel
	id   uint32
	pr   *io.PipeReader
	pw   *io.PipeWriter
	recv chan []byte
	mu   sync.Mutex
	dead bool
	once sync.Once
}

// Accept 处理一个入站隧道连接：完成握手认证并启动读循环。
// password 非空时要求客户端口令一致；onSession 在每产生一个新会话时被调用，
// 上层在此回调中启动该会话的 SOCKS5 处理逻辑。
func Accept(conn net.Conn, password string, onSession func(s *ServerSession)) (*ServerTunnel, error) {
	cipher := NewCipher(password)
	br := bufio.NewReader(conn)
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	// 读取握手帧
	hand, err := ReadFrame(br, cipher)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if hand.Type != FrameHandshake {
		conn.Close()
		return nil, errors.New("transport: 收到非握手帧")
	}

	// 校验口令
	ok := verifyAuth(cipher, hand.Payload)
	reply := []byte("OK")
	if !ok {
		reply = []byte("ERR")
	}
	_ = conn.SetDeadline(time.Time{})
	_ = WriteFrame(conn, cipher, Frame{Type: FrameHandshake, ConnID: 0, Payload: reply})
	if !ok {
		conn.Close()
		return nil, errors.New("transport: 客户端口令验证失败")
	}

	t := &ServerTunnel{
		conn:      conn,
		reader:    br,
		cipher:    cipher,
		closeCh:   make(chan struct{}),
		onSession: onSession,
	}
	go t.readLoop()
	return t, nil
}

// verifyAuth 校验客户端握手负载是否与预期口令匹配。
func verifyAuth(cipher *Cipher, got []byte) bool {
	if cipher == nil {
		// 明文模式：客户端应发送 "noauth"
		return string(got) == "noauth"
	}
	// 客户端发送的是口令 SHA256 摘要，即密钥本身
	return string(got) == string(cipher.key)
}

// NewSession 为指定连接 ID 创建服务端会话。
func (t *ServerTunnel) NewSession(id uint32) *ServerSession {
	pr, pw := io.Pipe()
	s := &ServerSession{t: t, id: id, pr: pr, pw: pw, recv: make(chan []byte, sessionQueueSize)}
	t.sessions.Store(id, s)
	go s.pump()
	return s
}

// readLoop 读取帧并分发。
func (t *ServerTunnel) readLoop() {
	for {
		f, err := ReadFrame(t.reader, t.cipher)
		if err != nil {
			t.closeAll()
			return
		}
		switch f.Type {
		case FrameSocks:
			v, ok := t.sessions.Load(f.ConnID)
			if !ok {
				// 新会话：自动创建并交给上层处理
				s := t.NewSession(f.ConnID)
				if t.onSession != nil {
					go t.onSession(s)
				}
				v = s
			}
			s := v.(*ServerSession)
			select {
			case <-t.closeCh:
				return
			default:
			}
			if !s.enqueue(f.Payload) {
				s.Close()
			}
		case FrameClose:
			if v, ok := t.sessions.LoadAndDelete(f.ConnID); ok {
				s := v.(*ServerSession)
				s.closeIncoming()
			}
		}
	}
}

// writeSession 封装会话数据为帧发出。
func (t *ServerTunnel) writeSession(id uint32, p []byte) error {
	if t.closed.Load() {
		return ErrTunnelClosed
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	return WriteFrame(t.conn, t.cipher, Frame{Type: FrameSocks, ConnID: id, Payload: p})
}

// closeSession 关闭会话并通知客户端。
func (t *ServerTunnel) closeSession(id uint32) {
	if _, ok := t.sessions.LoadAndDelete(id); !ok {
		return
	}
	if !t.closed.Load() {
		t.writeMu.Lock()
		_ = WriteFrame(t.conn, t.cipher, Frame{Type: FrameClose, ConnID: id})
		t.writeMu.Unlock()
	}
}

// closeAll 隧道关闭时清理。
func (t *ServerTunnel) closeAll() {
	if !t.closed.CompareAndSwap(false, true) {
		return
	}
	close(t.closeCh)
	t.sessions.Range(func(k, v interface{}) bool {
		s := v.(*ServerSession)
		s.closeIncoming()
		t.sessions.Delete(k)
		return true
	})
	t.conn.Close()
}

// Done 返回一个在隧道关闭时被关闭的通道，供外层等待。
func (t *ServerTunnel) Done() <-chan struct{} { return t.closeCh }

// Close 主动关闭隧道连接并清理所有会话。
func (t *ServerTunnel) Close() error {
	t.closeAll()
	return nil
}

// Read 实现 io.Reader。
func (s *ServerSession) Read(p []byte) (int, error) { return s.pr.Read(p) }

// Write 实现 io.Writer。
func (s *ServerSession) Write(p []byte) (int, error) {
	if err := s.t.writeSession(s.id, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close 关闭会话。
func (s *ServerSession) Close() error {
	var err error
	s.once.Do(func() {
		s.t.closeSession(s.id)
		s.closeIncoming()
		err = s.pr.Close()
	})
	return err
}

func (s *ServerSession) enqueue(p []byte) bool {
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

func (s *ServerSession) closeIncoming() {
	s.mu.Lock()
	if !s.dead {
		s.dead = true
		close(s.recv)
	}
	s.mu.Unlock()
}

func (s *ServerSession) pump() {
	defer s.pw.Close()
	for p := range s.recv {
		if _, err := s.pw.Write(p); err != nil {
			return
		}
	}
}

// LocalAddr 实现 net.Conn。
func (s *ServerSession) LocalAddr() net.Addr { return s.t.conn.LocalAddr() }

// RemoteAddr 实现 net.Conn。
func (s *ServerSession) RemoteAddr() net.Addr { return s.t.conn.RemoteAddr() }

// SetDeadline 实现 net.Conn。
func (s *ServerSession) SetDeadline(t time.Time) error { return nil }

// SetReadDeadline 实现 net.Conn。
func (s *ServerSession) SetReadDeadline(t time.Time) error { return nil }

// SetWriteDeadline 实现 net.Conn。
func (s *ServerSession) SetWriteDeadline(t time.Time) error { return nil }
