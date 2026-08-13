package socks

import (
	"io"
	"log"
	"net"

	"tunnelproxy/internal/transport"
)

// ServeLocal 在本机监听 listenAddr，接受浏览器/系统代理的 SOCKS5 连接，
// 并将每个连接的原始字节流经隧道透传到跳板机。
func ServeLocal(listenAddr string, tunnel *transport.ClientTunnel) error {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	defer ln.Close()

	log.Printf("[client] SOCKS5 本地监听于 %s，浏览器/系统代理请指向该地址", listenAddr)
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("[client] accept 错误: %v", err)
			continue
		}
		go handleLocalConn(c, tunnel)
	}
}

// handleLocalConn 处理一个本地浏览器连接：开一个隧道会话，双向透传字节流。
func handleLocalConn(c net.Conn, tunnel *transport.ClientTunnel) {
	defer c.Close()

	s := tunnel.Open()
	defer s.Close()

	// 浏览器 -> 隧道 -> 跳板机
	go func() {
		_, _ = io.Copy(s, c)
		_ = s.Close()
	}()
	// 跳板机 -> 隧道 -> 浏览器
	_, _ = io.Copy(c, s)
}
