package flclash

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const serverName = "tunnel.local"

// Config 是 FlClash 直连服务端的 JSON 配置。
type Config struct {
	Listen           string `json:"listen"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	CertFile         string `json:"cert_file"`
	KeyFile          string `json:"key_file"`
	HandshakeTimeout string `json:"handshake_timeout"`
	IdleTimeout      string `json:"idle_timeout"`
	MaxConnections   int    `json:"max_connections"`

	handshakeTimeout time.Duration
	idleTimeout      time.Duration
}

// DefaultDataDir 返回证书和服务端配置的默认持久化目录。
func DefaultDataDir() string {
	if runtime.GOOS == "windows" {
		if programData := os.Getenv("ProgramData"); programData != "" {
			return filepath.Join(programData, "TunnelProxy")
		}
	}
	if configDir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(configDir, "TunnelProxy")
	}
	return filepath.Join(".", "TunnelProxy")
}

// DefaultConfigPath 返回默认 JSON 配置路径。
func DefaultConfigPath() string { return filepath.Join(DefaultDataDir(), "server.json") }

// LoadConfig 读取、补齐并校验服务端配置。
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("读取配置 %s: %w", path, err)
	}
	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("解析配置 %s: %w", path, err)
	}
	if err := cfg.normalize(filepath.Dir(path)); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) normalize(baseDir string) error {
	if c.Listen == "" {
		c.Listen = "0.0.0.0:53"
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("flclash: listen 必须是 host:port 格式: %w", err)
	}
	if c.Username == "" || c.Password == "" {
		return errors.New("flclash: username 和 password 不能为空")
	}
	if len(c.Username) > 255 || len(c.Password) > 255 {
		return errors.New("flclash: username 和 password 不能超过 255 字节")
	}
	if c.CertFile == "" {
		c.CertFile = filepath.Join(baseDir, "server.crt")
	} else if !filepath.IsAbs(c.CertFile) {
		c.CertFile = filepath.Join(baseDir, c.CertFile)
	}
	if c.KeyFile == "" {
		c.KeyFile = filepath.Join(baseDir, "server.key")
	} else if !filepath.IsAbs(c.KeyFile) {
		c.KeyFile = filepath.Join(baseDir, c.KeyFile)
	}
	if c.HandshakeTimeout == "" {
		c.HandshakeTimeout = "15s"
	}
	if c.IdleTimeout == "" {
		c.IdleTimeout = "10m"
	}
	if c.MaxConnections == 0 {
		c.MaxConnections = 512
	}
	if c.MaxConnections < 1 || c.MaxConnections > 100000 {
		return errors.New("flclash: max_connections 必须在 1 到 100000 之间")
	}
	var err error
	c.handshakeTimeout, err = time.ParseDuration(c.HandshakeTimeout)
	if err != nil || c.handshakeTimeout <= 0 {
		return fmt.Errorf("flclash: handshake_timeout 无效: %q", c.HandshakeTimeout)
	}
	c.idleTimeout, err = time.ParseDuration(c.IdleTimeout)
	if err != nil || c.idleTimeout <= 0 {
		return fmt.Errorf("flclash: idle_timeout 无效: %q", c.IdleTimeout)
	}
	return nil
}
