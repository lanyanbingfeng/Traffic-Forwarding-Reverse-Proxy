package socks

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"tunnelproxy/internal/transport"
)

// ServeLocal 在本机监听 listenAddr，接受浏览器/系统代理的 SOCKS5 连接，
// 并将每个连接的原始字节流经隧道透传到跳板机。
func ServeLocal(listenAddr, serverAddr, password string, retry time.Duration) error {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	defer ln.Close()

	manager := &tunnelManager{}
	go manager.reconnectLoop(serverAddr, password, retry)

	log.Printf("[client] SOCKS5 本地监听于 %s，浏览器/系统代理请指向该地址", listenAddr)
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("[client] accept 错误: %v", err)
			continue
		}
		tunnel := manager.current()
		if tunnel == nil {
			log.Printf("[client] 隧道暂不可用，拒绝本地连接 %s", c.RemoteAddr())
			_ = c.Close()
			continue
		}
		go handleLocalConn(c, tunnel)
	}
}

type tunnelManager struct {
	mu     sync.RWMutex
	tunnel *transport.ClientTunnel
}

func (m *tunnelManager) current() *transport.ClientTunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tunnel
}

func (m *tunnelManager) set(t *transport.ClientTunnel) {
	m.mu.Lock()
	m.tunnel = t
	m.mu.Unlock()
}

func (m *tunnelManager) clear(t *transport.ClientTunnel) {
	m.mu.Lock()
	if m.tunnel == t {
		m.tunnel = nil
	}
	m.mu.Unlock()
}

func (m *tunnelManager) reconnectLoop(serverAddr, password string, retry time.Duration) {
	if retry <= 0 {
		retry = 5 * time.Second
	}
	for {
		log.Printf("[client] 尝试连接跳板机 %s ...", serverAddr)
		tunnel, err := transport.Dial(serverAddr, password)
		if err != nil {
			log.Printf("[client] 连接失败: %v，%s 后重试...", err, retry)
			time.Sleep(retry)
			continue
		}
		m.set(tunnel)
		log.Printf("[client] 已连接跳板机 %s", serverAddr)
		<-tunnel.Done()
		m.clear(tunnel)
		log.Printf("[client] 隧道已断开，%s 后重连...", retry)
		time.Sleep(retry)
	}
}

// handleLocalConn 处理一个本地浏览器连接：开一个隧道会话，双向透传字节流。
func handleLocalConn(c net.Conn, tunnel *transport.ClientTunnel) {
	defer c.Close()

	s, err := tunnel.Open()
	if err != nil {
		return
	}
	defer s.Close()

	// 浏览器 -> 隧道 -> 跳板机
	go func() {
		_, _ = io.Copy(s, c)
		_ = s.Close()
	}()
	// 跳板机 -> 隧道 -> 浏览器
	_, _ = io.Copy(c, s)
}
