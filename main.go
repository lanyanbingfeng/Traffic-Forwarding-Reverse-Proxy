// 局域网 53 端口反向代理隧道工具
//
// 用法（客户端）：
//
//	tunnel-client -server 192.168.1.100:53 [-listen 127.0.0.1:1080] -key 口令
//
// 用法（服务端）：
//
//	tunnel-server -listen 0.0.0.0:53 -key 口令
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"tunnelproxy/internal/flclash"
	"tunnelproxy/internal/socks"
)

// defaultMode 由编译期 -ldflags -X 注入，用于生成默认角色的二进制。
var defaultMode string

func main() {
	mode := flag.String("mode", defaultMode, "运行模式: client、server 或 flclash-server")
	serverAddr := flag.String("server", "", "跳板机地址 (client 模式, 如 192.168.1.100:53)")
	listenAddr := flag.String("listen", "", "监听地址 (client: 127.0.0.1:1080, server: 0.0.0.0:53)")
	key := flag.String("key", "", "隧道加密和认证口令 (两端必须一致)")
	allowInsecure := flag.Bool("allow-insecure", false, "允许服务端无口令运行（不推荐，仅限可信网络）")
	retry := flag.Int("retry", 5, "client 模式连接失败后的重试间隔秒数")
	configPath := flag.String("config", "", "flclash-server JSON 配置路径")
	initOnly := flag.Bool("init-only", false, "仅初始化 FlClash TLS 证书并输出指纹")
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
		runServer(*listenAddr, *key, *allowInsecure)
	case "flclash-server":
		runFlClashServer(*configPath, *initOnly)
	default:
		fmt.Fprintf(os.Stderr, "错误: 未知模式 %q，仅支持 client/server/flclash-server\n", *mode)
		os.Exit(1)
	}
}

func runFlClashServer(configPath string, initOnly bool) {
	if configPath == "" {
		configPath = flclash.DefaultConfigPath()
	}
	cfg, err := flclash.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("[flclash-server] 配置错误: %v", err)
	}
	if initOnly {
		fingerprint, err := flclash.EnsureCertificate(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			log.Fatalf("[flclash-server] 初始化证书失败: %v", err)
		}
		fmt.Printf("SNI=tunnel.local\nCERT_SHA256=%s\n", fingerprint)
		return
	}
	if err := flclash.Serve(cfg); err != nil {
		log.Fatalf("[flclash-server] 启动失败: %v", err)
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

	if retry <= 0 {
		fmt.Fprintln(os.Stderr, "错误: -retry 必须大于 0")
		os.Exit(1)
	}

	if err := socks.ServeLocal(listenAddr, serverAddr, key, time.Duration(retry)*time.Second); err != nil {
		log.Fatalf("[client] 本地监听失败: %v", err)
	}
}

// runServer 运行服务端：监听 53 端口并处理隧道会话。
func runServer(listenAddr, key string, allowInsecure bool) {
	if listenAddr == "" {
		listenAddr = "0.0.0.0:53"
	}
	if key == "" && !allowInsecure {
		log.Fatal("[server] 为防止成为局域网开放代理，必须设置 -key；如确需明文模式请显式添加 -allow-insecure")
	}
	if key == "" {
		log.Printf("[server] 警告：当前为无认证明文模式，仅应在完全可信的隔离网络使用")
	}
	if err := socks.ServeServer(listenAddr, key); err != nil {
		log.Fatalf("[server] 启动失败: %v", err)
	}
}
