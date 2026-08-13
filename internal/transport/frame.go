// Package transport 实现了在单个 TCP 连接上复用多个并发会话的隧道帧协议。
//
// 帧格式（所有字段均为网络字节序 BigEndian）：
//
//	+--------+------+--------+----------+
//	| length | type | connID | payload  |
//	| 4 bytes|2bytes| 4 bytes| length - 6 |
//	+--------+------+--------+----------+
//
//   - length: 帧体长度 = 6 + len(payload)（即 type+connID+payload 的字节数）
//   - type:   帧类型（见 FrameType 常量）
//   - connID: 会话（连接）ID，用于在一个 TCP 隧道上区分多个并发 SOCKS5 会话
//   - payload: 帧负载
//
// 可选加密：通过 XORKey 派生密钥对帧头之后（含 type/connID/payload）做 XOR 混淆，
// 用于抵御同网段简单嗅探，不提供强加密保证。
package transport

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// 帧类型
const (
	// FrameHandshake 认证握手帧：建立隧道时客户端发送认证信息，服务端验证后回发确认。
	FrameHandshake = 0x01
	// FrameSocks SOCKS5 数据帧：承载一个会话的 SOCKS5 字节流。
	FrameSocks = 0x02
	// FrameClose 关闭帧：关闭指定会话。
	FrameClose = 0x03
)

// frameHeaderSize 帧头固定大小：type(2) + connID(4)。
const frameHeaderSize = 6

// MaxFrameSize 单帧负载上限，防止恶意超长帧拖垮内存。
const MaxFrameSize = 16 * 1024 * 1024

// Frame 表示一个隧道帧。
type Frame struct {
	Type    uint16
	ConnID  uint32
	Payload []byte
}

// Cipher 提供可选的帧负载 XOR 混淆。
type Cipher struct {
	key []byte
}

// NewCipher 根据口令派生 XOR 密钥。key 为空时返回 nil（明文模式）。
func NewCipher(password string) *Cipher {
	if password == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(password))
	return &Cipher{key: sum[:]}
}

// xor 对 data 就地做 XOR 混淆。
func (c *Cipher) xor(data []byte) {
	if c == nil {
		return
	}
	k := len(c.key)
	for i := range data {
		data[i] ^= c.key[i%k]
	}
}

// ReadFrame 从 bufio.Reader 读取一个完整帧。
func ReadFrame(br *bufio.Reader, cipher *Cipher) (Frame, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(br, lenBuf[:]); err != nil {
		return Frame{}, err
	}
	bodyLen := binary.BigEndian.Uint32(lenBuf[:])
	if bodyLen < frameHeaderSize {
		return Frame{}, fmt.Errorf("transport: 非法帧长度 %d", bodyLen)
	}
	if bodyLen > MaxFrameSize {
		return Frame{}, fmt.Errorf("transport: 帧长度 %d 超过上限 %d", bodyLen, MaxFrameSize)
	}

	body := make([]byte, bodyLen)
	if _, err := io.ReadFull(br, body); err != nil {
		return Frame{}, err
	}
	if cipher != nil {
		cipher.xor(body)
	}

	return Frame{
		Type:    binary.BigEndian.Uint16(body[0:2]),
		ConnID:  binary.BigEndian.Uint32(body[2:6]),
		Payload: body[6:],
	}, nil
}

// WriteFrame 将一个帧写入 writer。
func WriteFrame(w io.Writer, cipher *Cipher, f Frame) error {
	bodyLen := frameHeaderSize + len(f.Payload)
	var head [4]byte
	binary.BigEndian.PutUint32(head[:], uint32(bodyLen))
	if _, err := w.Write(head[:]); err != nil {
		return err
	}

	body := make([]byte, bodyLen)
	binary.BigEndian.PutUint16(body[0:2], f.Type)
	binary.BigEndian.PutUint32(body[2:6], f.ConnID)
	copy(body[6:], f.Payload)
	if cipher != nil {
		cipher.xor(body)
	}
	_, err := w.Write(body)
	return err
}

// ErrTunnelClosed 表示隧道连接已关闭。
var ErrTunnelClosed = errors.New("transport: 隧道连接已关闭")
