// Package network 提供 pasted 的网络通信功能，包括帧协议编解码、TLS 和 TCP 连接管理。
package network

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
)

// 帧类型常量
const (
	TypeText  byte = 0x01
	TypeImage byte = 0x02
)

// Frame 表示一个传输帧
// 线上格式: Type(1B) + Hash(32B) + Length(4B, BigEndian) + Payload(NB)
type Frame struct {
	Type    byte     // 0x01=Text, 0x02=Image
	Hash    [32]byte // SHA-256 of Payload
	Payload []byte   // 实际数据 (文本 UTF-8 / 图像 PNG)
}

// MaxPayloadSize 最大载荷大小，50MB，防止恶意数据
const MaxPayloadSize = 50 * 1024 * 1024

// NewFrame 根据类型和数据创建一个 Frame，自动计算 SHA-256 Hash
func NewFrame(typ byte, payload []byte) Frame {
	return Frame{
		Type:    typ,
		Hash:    sha256.Sum256(payload),
		Payload: payload,
	}
}

// Encode 将 Frame 编码写入 writer
// 格式: Type(1B) + Hash(32B) + Length(4B) + Payload(NB)
func (f *Frame) Encode(w io.Writer) error {
	// Type
	if _, err := w.Write([]byte{f.Type}); err != nil {
		return fmt.Errorf("写入 Type 失败: %w", err)
	}
	// Hash
	if _, err := w.Write(f.Hash[:]); err != nil {
		return fmt.Errorf("写入 Hash 失败: %w", err)
	}
	// Length (BigEndian uint32)
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(f.Payload)))
	if _, err := w.Write(length); err != nil {
		return fmt.Errorf("写入 Length 失败: %w", err)
	}
	// Payload
	if _, err := w.Write(f.Payload); err != nil {
		return fmt.Errorf("写入 Payload 失败: %w", err)
	}
	return nil
}

// DecodeFrame 从 reader 中解码一个 Frame
func DecodeFrame(r io.Reader) (*Frame, error) {
	f := &Frame{}

	// Type (1 byte)
	typeBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, typeBuf); err != nil {
		return nil, fmt.Errorf("读取 Type 失败: %w", err)
	}
	f.Type = typeBuf[0]
	if f.Type != TypeText && f.Type != TypeImage {
		return nil, fmt.Errorf("未知帧类型: 0x%02x", f.Type)
	}

	// Hash (32 bytes)
	if _, err := io.ReadFull(r, f.Hash[:]); err != nil {
		return nil, fmt.Errorf("读取 Hash 失败: %w", err)
	}

	// Length (4 bytes, BigEndian)
	lengthBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lengthBuf); err != nil {
		return nil, fmt.Errorf("读取 Length 失败: %w", err)
	}
	payloadLen := binary.BigEndian.Uint32(lengthBuf)
	if payloadLen > MaxPayloadSize {
		return nil, fmt.Errorf("载荷过大: %d bytes (最大 %d)", payloadLen, MaxPayloadSize)
	}

	// Payload
	f.Payload = make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return nil, fmt.Errorf("读取 Payload 失败: %w", err)
		}
	}

	// 校验 Hash
	computed := sha256.Sum256(f.Payload)
	if computed != f.Hash {
		return nil, fmt.Errorf("Hash 校验失败: 期望 %x, 实际 %x", f.Hash, computed)
	}

	return f, nil
}
