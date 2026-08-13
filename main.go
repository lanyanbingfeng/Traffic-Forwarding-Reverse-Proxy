// 局域网 53 端口反向代理隧道工具
//
// 用法（客户端）：
//   tunnel-client -server 192.168.1.100:53 [-listen 127.0.0.1:1080] [-key 口令]
//
// 用法（服务端）：
//   tunnel-server -listen 0.0.0.0:53 [-key 口令]
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"tunnelproxy/internal/socks"
	"tunnelproxy/internal/transport"
)

// defaultMode 由编译期 -ldflags -X 注入，用于生成默认角色的二进制。
var defaultMode string

func main() {
	mode := flag.String("mode", defaultMode, "运行模式: client 或 server")
	serverAddr := flag.String("server", "", "跳板机地址 (client 模式, 如 192.168.1.100:53)")
	listenAddr := flag.String("listen", "", "监听地址 (client: 127.0.0.1:1080, server: 0.0.0.0:53)")
	key := flag.String("key", "", "加密口令 (可选, 两端必须一致)")
	retry := flag.Int("retry", 5, "client 模式连接失败后的重试间隔秒数")
	flag.Parse()

	if *mode == "" {
		fmt.Fprintln(os.Stderr, "错误: 请用 -mode client 或 -mode server 指定运行模式（或使用默认角色二进制）")
		flag.Usage()
		os.Exit(1)
	}

	switch *mode {
	case "client":
		runClient(*serverAddr, *listenAddr, *key, *retry)
	case "server":
		runServer(*listenAddr, *key)
	default:
		fmt.Fprintf(os.Stderr, "错误: 未知模式 %q，仅支持 client/server\n", *mode)
		os.Exit(1)
	}
}

// runClient 运行客户端：连接跳板机 53 端口，然后在本机启动 SOCKS5 监听。
func runClient(serverAddr, listenAddr, key string, retry int) {
	if serverAddr == "" {
		fmt.Fprintln(os.Stderr, "错误: client 模式必须指定 -server 跳板机地址 (如 192.168.1.100:53)")
		os.Exit(1)
	}
	if listenAddr == "" {
		listenAddr = "127.0.0.1:1080"
	}

	log.Printf("[client] 尝试连接跳板机 %s ...", serverAddr)
	tunnel, err := transport.Dial(serverAddr, key)
	if err != nil {
		if retry > 0 {
			log.Printf("[client] 连接失败: %v，%d 秒后重试...", err, retry)
			time.Sleep(time.Duration(retry) * time.Second)
			runClient(serverAddr, listenAddr, key, retry)
			return
		}
		log.Fatalf("[client] 连接跳板机失败: %v", err)
	}
	defer tunnel.Close()
	log.Printf("[client] 已连接跳板机 %s", serverAddr)

	if err := socks.ServeLocal(listenAddr, tunnel); err != nil {
		log.Fatalf("[client] 本地监听失败: %v", err)
	}
}

// runServer 运行服务端：监听 53 端口并处理隧道会话。
func runServer(listenAddr, key string) {
	if listenAddr == "" {
		listenAddr = "0.0.0.0:53"
	}
	if err := socks.ServeServer(listenAddr, key); err != nil {
		log.Fatalf("[server] 启动失败: %v", err)
	}
}
