package flclash

import (
	"crypto/subtle"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	socksVersion             = 0x05
	methodUserPassword       = 0x02
	methodNotAcceptable      = 0xff
	commandConnect           = 0x01
	commandUDPAssociate      = 0x03
	replySucceeded           = 0x00
	replyGeneralFailure      = 0x01
	replyHostUnreachable     = 0x04
	replyCommandNotSupported = 0x07
)

// Serve 启动 FlClash 可直接连接的 SOCKS5 over TLS 服务端。
func Serve(cfg Config) error {
	fingerprint, err := EnsureCertificate(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return err
	}
	certificate, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return fmt.Errorf("flclash: 加载 TLS 证书失败: %w", err)
	}
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	}
	limit := make(chan struct{}, cfg.MaxConnections)
	log.Printf("[flclash-server] SOCKS5 over TLS 监听于 %s", cfg.Listen)
	log.Printf("[flclash-server] SNI=%s", serverName)
	log.Printf("[flclash-server] CERT_SHA256=%s", fingerprint)

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		select {
		case limit <- struct{}{}:
			go func() {
				defer func() { <-limit }()
				handleConnection(tls.Server(conn, tlsConfig), cfg)
			}()
		default:
			log.Printf("[flclash-server] 连接数达到上限，拒绝 %s", conn.RemoteAddr())
			_ = conn.Close()
		}
	}
}

func handleConnection(conn *tls.Conn, cfg Config) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(cfg.handshakeTimeout))
	if err := conn.Handshake(); err != nil {
		log.Printf("[flclash-server] TLS 握手失败 %s: %v", conn.RemoteAddr(), err)
		return
	}
	if err := authenticate(conn, cfg.Username, cfg.Password); err != nil {
		log.Printf("[flclash-server] SOCKS5 认证失败 %s: %v", conn.RemoteAddr(), err)
		return
	}
	targetAddr, replyCode, err := readConnectRequest(conn)
	if err != nil {
		_ = writeReply(conn, replyCode, nil)
		log.Printf("[flclash-server] SOCKS5 请求失败 %s: %v", conn.RemoteAddr(), err)
		return
	}

	dialer := net.Dialer{Timeout: cfg.handshakeTimeout, KeepAlive: 30 * time.Second}
	target, err := dialer.Dial("tcp", targetAddr)
	if err != nil {
		_ = writeReply(conn, replyHostUnreachable, nil)
		log.Printf("[flclash-server] 连接目标 %s 失败: %v", targetAddr, err)
		return
	}
	defer target.Close()
	if err := writeReply(conn, replySucceeded, target.LocalAddr()); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	log.Printf("[flclash-server] [访问] %s -> %s", conn.RemoteAddr(), targetAddr)
	proxyBidirectional(conn, target, cfg.idleTimeout)
}

func authenticate(conn io.ReadWriter, username, password string) error {
	var header [2]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return err
	}
	if header[0] != socksVersion || header[1] == 0 {
		return errors.New("无效的 SOCKS5 方法协商")
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	found := false
	for _, method := range methods {
		if method == methodUserPassword {
			found = true
			break
		}
	}
	if !found {
		_ = writeAll(conn, []byte{socksVersion, methodNotAcceptable})
		return errors.New("客户端未提供用户名密码认证")
	}
	if err := writeAll(conn, []byte{socksVersion, methodUserPassword}); err != nil {
		return err
	}

	var authHeader [2]byte
	if _, err := io.ReadFull(conn, authHeader[:]); err != nil {
		return err
	}
	if authHeader[0] != 0x01 || authHeader[1] == 0 {
		return errors.New("无效的 RFC 1929 认证请求")
	}
	user := make([]byte, int(authHeader[1]))
	if _, err := io.ReadFull(conn, user); err != nil {
		return err
	}
	var passwordLength [1]byte
	if _, err := io.ReadFull(conn, passwordLength[:]); err != nil {
		return err
	}
	if passwordLength[0] == 0 {
		return errors.New("密码不能为空")
	}
	pass := make([]byte, int(passwordLength[0]))
	if _, err := io.ReadFull(conn, pass); err != nil {
		return err
	}
	valid := subtle.ConstantTimeCompare(user, []byte(username)) == 1 &&
		subtle.ConstantTimeCompare(pass, []byte(password)) == 1
	status := byte(0x00)
	if !valid {
		status = 0x01
	}
	if err := writeAll(conn, []byte{0x01, status}); err != nil {
		return err
	}
	if !valid {
		return errors.New("用户名或密码错误")
	}
	return nil
}

func readConnectRequest(conn io.Reader) (string, byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return "", replyGeneralFailure, err
	}
	if header[0] != socksVersion || header[2] != 0x00 {
		return "", replyGeneralFailure, errors.New("无效的 SOCKS5 请求头")
	}
	if header[1] == commandUDPAssociate {
		return "", replyCommandNotSupported, errors.New("第一版不支持 UDP ASSOCIATE")
	}
	if header[1] != commandConnect {
		return "", replyCommandNotSupported, fmt.Errorf("不支持的 SOCKS5 命令 %d", header[1])
	}
	host, err := readHost(conn, header[3])
	if err != nil {
		return "", replyGeneralFailure, err
	}
	var portBytes [2]byte
	if _, err := io.ReadFull(conn, portBytes[:]); err != nil {
		return "", replyGeneralFailure, err
	}
	port := binary.BigEndian.Uint16(portBytes[:])
	if port == 0 {
		return "", replyGeneralFailure, errors.New("目标端口不能为 0")
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), replySucceeded, nil
}

func readHost(r io.Reader, addressType byte) (string, error) {
	switch addressType {
	case 0x01:
		data := make([]byte, net.IPv4len)
		_, err := io.ReadFull(r, data)
		return net.IP(data).String(), err
	case 0x04:
		data := make([]byte, net.IPv6len)
		_, err := io.ReadFull(r, data)
		return net.IP(data).String(), err
	case 0x03:
		var length [1]byte
		if _, err := io.ReadFull(r, length[:]); err != nil {
			return "", err
		}
		if length[0] == 0 {
			return "", errors.New("域名不能为空")
		}
		data := make([]byte, int(length[0]))
		if _, err := io.ReadFull(r, data); err != nil {
			return "", err
		}
		return string(data), nil
	default:
		return "", fmt.Errorf("不支持的地址类型 %d", addressType)
	}
}

func writeReply(w io.Writer, code byte, addr net.Addr) error {
	host := net.IPv4zero
	port := 0
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		host = tcpAddr.IP
		port = tcpAddr.Port
	}
	response := []byte{socksVersion, code, 0x00}
	if ip4 := host.To4(); ip4 != nil {
		response = append(response, 0x01)
		response = append(response, ip4...)
	} else {
		response = append(response, 0x04)
		response = append(response, host.To16()...)
	}
	var portBytes [2]byte
	binary.BigEndian.PutUint16(portBytes[:], uint16(port))
	response = append(response, portBytes[:]...)
	return writeAll(w, response)
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

type activityConn struct {
	net.Conn
	touch func()
}

func (c activityConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.touch()
	}
	return n, err
}

func (c activityConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.touch()
	}
	return n, err
}

func proxyBidirectional(client, target net.Conn, idleTimeout time.Duration) {
	var timerMu sync.Mutex
	timer := time.AfterFunc(idleTimeout, func() {
		_ = client.Close()
		_ = target.Close()
	})
	touch := func() {
		timerMu.Lock()
		timer.Reset(idleTimeout)
		timerMu.Unlock()
	}
	clientActive := activityConn{Conn: client, touch: touch}
	targetActive := activityConn{Conn: target, touch: touch}
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(targetActive, clientActive)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(clientActive, targetActive)
		done <- struct{}{}
	}()
	<-done
	timerMu.Lock()
	timer.Stop()
	timerMu.Unlock()
	_ = client.Close()
	_ = target.Close()
	<-done
}
