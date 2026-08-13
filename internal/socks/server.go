package socks

import (
	"io"
	"log"
	"net"

	"tunnelproxy/internal/transport"
)

// ServeServer 在 listenAddr（通常是 0.0.0.0:53）监听，接受客户端隧道连接。
// 对每个隧道连接启动会话分发，对每个会话执行 SOCKS5 服务端逻辑。
func ServeServer(listenAddr, password string) error {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	defer ln.Close()

	log.Printf("[server] 隧道监听于 %s（注意：端口 <1024 需管理员/root 权限）", listenAddr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[server] accept 错误: %v", err)
			continue
		}
		go handleTunnelConn(conn, password)
	}
}

// handleTunnelConn 处理一个客户端隧道连接。
func handleTunnelConn(conn net.Conn, password string) {
	t, err := transport.Accept(conn, password, func(s *transport.ServerSession) {
		HandleSession(s)
	})
	if err != nil {
		log.Printf("[server] 隧道握手失败: %v", err)
		conn.Close()
		return
	}
	log.Printf("[server] 客户端隧道已接入: %s", conn.RemoteAddr())
	defer t.Close()

	// 保持隧道存活直到关闭
	<-t.Done()
}

// HandleSession 处理服务端的一个隧道会话：
// 执行完整 SOCKS5 服务端逻辑（握手、CONNECT、建立真实出站连接、回发响应、双向转发）。
// s 是 transport.ServerSession，承载来自客户端的原始 SOCKS5 字节流。
func HandleSession(s net.Conn) {
	defer s.Close()

	// 1. SOCKS5 握手
	if err := handshake(s); err != nil {
		log.Printf("[server] SOCKS5 握手失败: %v", err)
		return
	}
	// 2. 解析 CONNECT 请求
	addr, err := readRequest(s)
	if err != nil {
		log.Printf("[server] SOCKS5 请求解析失败: %v", err)
		return
	}
	// 3. 建立真实出站连接
	target, err := net.Dial("tcp", addr)
	if err != nil {
		log.Printf("[server] 连接目标 %s 失败: %v", addr, err)
		_ = writeReply(s, repHostUnreach)
		return
	}
	defer target.Close()

	// 4. 回发成功应答
	if err := writeReply(s, repSucceeded); err != nil {
		return
	}
	log.Printf("[server] 已连接目标 %s", addr)

	// 5. 双向转发（客户端<->目标）
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(target, s)
		close(done)
	}()
	_, _ = io.Copy(s, target)
	<-done
}
