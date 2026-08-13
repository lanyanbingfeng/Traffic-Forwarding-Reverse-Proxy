// Package socks 实现了 SOCKS5 协议的相关逻辑。
//
// 架构说明：
//   - 本机客户端仅做"原始字节流透传"，把浏览器发来的 SOCKS5 握手/请求字节
//     原样经隧道发给跳板机，不做任何 SOCKS5 解析。
//   - 跳板机服务端对每个隧道会话执行完整的 SOCKS5 服务端逻辑：
//     握手、CONNECT 解析、建立真实出站连接并回发响应，随后双向转发。
package socks

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
)

// SOCKS5 协议常量
const (
	version = 0x05

	// 认证方法
	methodNoAuth = 0x00

	// 命令
	cmdConnect = 0x01

	// 地址类型
	atypIPv4   = 0x01
	atypDomain = 0x03
	atypIPv6   = 0x04

	// 应答码
	repSucceeded   = 0x00
	repGeneralFail = 0x01
	repHostUnreach = 0x04
	repCmdNotSup   = 0x07
)

// handshake 读取客户端握手并回复无认证方法。
func handshake(rw io.ReadWriter) error {
	var hdr [2]byte
	if _, err := io.ReadFull(rw, hdr[:]); err != nil {
		return err
	}
	if hdr[0] != version {
		return fmt.Errorf("socks: 不支持的 SOCKS 版本 %d", hdr[0])
	}
	nmethods := int(hdr[1])
	if nmethods <= 0 {
		return errors.New("socks: 无效的 NMETHODS")
	}
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(rw, methods); err != nil {
		return err
	}
	// 选择无认证方法
	_, err := rw.Write([]byte{version, methodNoAuth})
	return err
}

// readRequest 解析 CONNECT 请求，返回目标地址与端口。
func readRequest(rw io.ReadWriter) (addr string, err error) {
	var hdr [4]byte
	if _, err = io.ReadFull(rw, hdr[:]); err != nil {
		return "", err
	}
	if hdr[0] != version {
		return "", fmt.Errorf("socks: 不支持的 SOCKS 版本 %d", hdr[0])
	}
	if hdr[1] != cmdConnect {
		_ = writeReply(rw, repCmdNotSup)
		return "", errors.New("socks: 仅支持 CONNECT 命令")
	}
	// hdr[2] = RSV，忽略

	var host string
	switch hdr[3] {
	case atypIPv4:
		var b [4]byte
		if _, err = io.ReadFull(rw, b[:]); err != nil {
			return "", err
		}
		host = net.IP(b[:]).String()
	case atypIPv6:
		var b [16]byte
		if _, err = io.ReadFull(rw, b[:]); err != nil {
			return "", err
		}
		host = net.IP(b[:]).String()
	case atypDomain:
		var lb [1]byte
		if _, err = io.ReadFull(rw, lb[:]); err != nil {
			return "", err
		}
		ln := int(lb[0])
		if ln <= 0 {
			return "", errors.New("socks: 无效的域名长度")
		}
		db := make([]byte, ln)
		if _, err = io.ReadFull(rw, db); err != nil {
			return "", err
		}
		host = string(db)
	default:
		return "", fmt.Errorf("socks: 不支持的地址类型 %d", hdr[3])
	}

	var pb [2]byte
	if _, err = io.ReadFull(rw, pb[:]); err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(pb[:])
	return net.JoinHostPort(host, fmt.Sprintf("%d", port)), nil
}

// writeReply 向客户端发送 SOCKS5 应答。
func writeReply(w io.Writer, rep byte) error {
	// 绑定地址置为 0.0.0.0:0
	resp := []byte{version, rep, 0x00, atypIPv4, 0, 0, 0, 0, 0, 0}
	_, err := w.Write(resp)
	return err
}
