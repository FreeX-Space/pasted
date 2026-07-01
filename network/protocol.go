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
	TypeText       byte = 0x01
	TypeImage      byte = 0x02
	TypeHeartbeat  byte = 0x03
	TypeHello      byte = 0x04
	TypeRelayText  byte = 0x11
	TypeRelayImage byte = 0x12
)

const maxIdentityFieldLen = 1024

// Frame 表示一个传输帧
// 线上格式: Type(1B) + Hash(32B) + Length(4B, BigEndian) + Payload(NB)
type Frame struct {
	Type    byte     // 0x01=Text, 0x02=Image, 0x03=Heartbeat, 0x04=Hello, 0x11/0x12=Relay
	Hash    [32]byte // SHA-256 of Payload
	Payload []byte   // 实际数据 (文本 UTF-8 / 图像 PNG)
}

// RelaySource 表示 relay 转发帧中的原始发送客户端身份。
type RelaySource struct {
	Hostname string
	IP       string
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

// NewHelloFrame 创建客户端身份注册帧。
func NewHelloFrame(hostname, ip string) Frame {
	return NewFrame(TypeHello, encodeIdentityPayload(hostname, ip, nil))
}

// ParseHelloFrame 解析客户端身份注册帧。
func ParseHelloFrame(frame *Frame) (RelaySource, error) {
	if frame.Type != TypeHello {
		return RelaySource{}, fmt.Errorf("不是 Hello 帧: 0x%02x", frame.Type)
	}
	source, _, err := decodeIdentityPayload(frame.Payload)
	return source, err
}

// NewRelayFrame 创建 relay 转发帧，payload 中包含原始发送客户端身份。
func NewRelayFrame(sourceHost, sourceIP string, frame *Frame) (Frame, error) {
	var relayType byte
	switch frame.Type {
	case TypeText:
		relayType = TypeRelayText
	case TypeImage:
		relayType = TypeRelayImage
	default:
		return Frame{}, fmt.Errorf("不支持中继帧类型: 0x%02x", frame.Type)
	}
	return NewFrame(relayType, encodeIdentityPayload(sourceHost, sourceIP, frame.Payload)), nil
}

// UnwrapRelayFrame 解析 relay 转发帧，返回原始客户端身份和原始文本/图像帧。
func UnwrapRelayFrame(frame *Frame) (RelaySource, *Frame, error) {
	var originalType byte
	switch frame.Type {
	case TypeRelayText:
		originalType = TypeText
	case TypeRelayImage:
		originalType = TypeImage
	default:
		return RelaySource{}, nil, fmt.Errorf("不是中继帧: 0x%02x", frame.Type)
	}

	source, payload, err := decodeIdentityPayload(frame.Payload)
	if err != nil {
		return RelaySource{}, nil, err
	}
	original := NewFrame(originalType, payload)
	return source, &original, nil
}

// IsRelayFrame 判断是否为 relay 转发帧。
func IsRelayFrame(typ byte) bool {
	return typ == TypeRelayText || typ == TypeRelayImage
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
	if !isKnownType(f.Type) {
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

func isKnownType(typ byte) bool {
	switch typ {
	case TypeText, TypeImage, TypeHeartbeat, TypeHello, TypeRelayText, TypeRelayImage:
		return true
	default:
		return false
	}
}

func encodeIdentityPayload(hostname, ip string, payload []byte) []byte {
	hostBytes := []byte(limitIdentityField(hostname))
	ipBytes := []byte(limitIdentityField(ip))

	out := make([]byte, 4+len(hostBytes)+len(ipBytes)+len(payload))
	binary.BigEndian.PutUint16(out[0:2], uint16(len(hostBytes)))
	binary.BigEndian.PutUint16(out[2:4], uint16(len(ipBytes)))
	copy(out[4:], hostBytes)
	copy(out[4+len(hostBytes):], ipBytes)
	copy(out[4+len(hostBytes)+len(ipBytes):], payload)
	return out
}

func decodeIdentityPayload(payload []byte) (RelaySource, []byte, error) {
	if len(payload) < 4 {
		return RelaySource{}, nil, fmt.Errorf("身份载荷过短: %d bytes", len(payload))
	}

	hostLen := int(binary.BigEndian.Uint16(payload[0:2]))
	ipLen := int(binary.BigEndian.Uint16(payload[2:4]))
	if hostLen > maxIdentityFieldLen || ipLen > maxIdentityFieldLen {
		return RelaySource{}, nil, fmt.Errorf("身份字段过长: hostname=%d ip=%d", hostLen, ipLen)
	}
	endIdentity := 4 + hostLen + ipLen
	if len(payload) < endIdentity {
		return RelaySource{}, nil, fmt.Errorf("身份载荷长度不匹配")
	}

	source := RelaySource{
		Hostname: string(payload[4 : 4+hostLen]),
		IP:       string(payload[4+hostLen : endIdentity]),
	}
	return source, payload[endIdentity:], nil
}

func limitIdentityField(value string) string {
	if len(value) <= maxIdentityFieldLen {
		return value
	}
	return value[:maxIdentityFieldLen]
}
