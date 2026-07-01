package network

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestNewFrame(t *testing.T) {
	payload := []byte("Hello pasted!")
	frame := NewFrame(TypeText, payload)

	if frame.Type != TypeText {
		t.Errorf("期望 Type=0x01, 实际=%02x", frame.Type)
	}

	expectedHash := sha256.Sum256(payload)
	if frame.Hash != expectedHash {
		t.Errorf("Hash 不匹配")
	}

	if !bytes.Equal(frame.Payload, payload) {
		t.Errorf("Payload 不匹配")
	}
}

func TestFrameEncodeDecodeText(t *testing.T) {
	payload := []byte("测试中文文本 + emoji 🎉")
	original := NewFrame(TypeText, payload)

	var buf bytes.Buffer
	if err := original.Encode(&buf); err != nil {
		t.Fatalf("编码失败: %v", err)
	}

	decoded, err := DecodeFrame(&buf)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type 不匹配: 期望=%02x, 实际=%02x", original.Type, decoded.Type)
	}
	if decoded.Hash != original.Hash {
		t.Errorf("Hash 不匹配")
	}
	if !bytes.Equal(decoded.Payload, original.Payload) {
		t.Errorf("Payload 不匹配")
	}
}

func TestFrameEncodeDecodeImage(t *testing.T) {
	// 模拟一个 PNG 图像数据（伪数据）
	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = byte(i % 256)
	}
	original := NewFrame(TypeImage, payload)

	var buf bytes.Buffer
	if err := original.Encode(&buf); err != nil {
		t.Fatalf("编码失败: %v", err)
	}

	decoded, err := DecodeFrame(&buf)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}

	if decoded.Type != TypeImage {
		t.Errorf("Type 不匹配")
	}
	if !bytes.Equal(decoded.Payload, payload) {
		t.Errorf("Payload 不匹配, 期望长度=%d, 实际长度=%d", len(payload), len(decoded.Payload))
	}
}

func TestFrameEncodeDecodeEmpty(t *testing.T) {
	original := NewFrame(TypeText, []byte{})

	var buf bytes.Buffer
	if err := original.Encode(&buf); err != nil {
		t.Fatalf("编码失败: %v", err)
	}

	decoded, err := DecodeFrame(&buf)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}

	if len(decoded.Payload) != 0 {
		t.Errorf("空 Payload 应为 0 长度, 实际=%d", len(decoded.Payload))
	}
}

func TestDecodeInvalidType(t *testing.T) {
	// 构造一个无效类型的帧
	var buf bytes.Buffer
	buf.WriteByte(0xFF)           // 无效类型
	buf.Write(make([]byte, 32))   // fake hash
	buf.Write([]byte{0, 0, 0, 0}) // length=0

	_, err := DecodeFrame(&buf)
	if err == nil {
		t.Error("应返回未知帧类型错误")
	}
}

func TestDecodeHashMismatch(t *testing.T) {
	payload := []byte("test data")
	frame := NewFrame(TypeText, payload)

	var buf bytes.Buffer
	if err := frame.Encode(&buf); err != nil {
		t.Fatalf("编码失败: %v", err)
	}

	// 篡改 Payload（修改最后一个字节）
	data := buf.Bytes()
	data[len(data)-1] ^= 0xFF

	_, err := DecodeFrame(bytes.NewReader(data))
	if err == nil {
		t.Error("篡改数据应导致 Hash 校验失败")
	}
}

func TestMultipleFramesDecode(t *testing.T) {
	frames := []Frame{
		NewFrame(TypeText, []byte("第一条消息")),
		NewFrame(TypeImage, []byte{0x89, 0x50, 0x4E, 0x47}),
		NewFrame(TypeText, []byte("第三条消息")),
	}

	var buf bytes.Buffer
	for _, f := range frames {
		if err := f.Encode(&buf); err != nil {
			t.Fatalf("编码失败: %v", err)
		}
	}

	for i, expected := range frames {
		decoded, err := DecodeFrame(&buf)
		if err != nil {
			t.Fatalf("解码第 %d 帧失败: %v", i, err)
		}
		if decoded.Type != expected.Type {
			t.Errorf("第 %d 帧 Type 不匹配", i)
		}
		if !bytes.Equal(decoded.Payload, expected.Payload) {
			t.Errorf("第 %d 帧 Payload 不匹配", i)
		}
	}
}

func TestHelloFrame(t *testing.T) {
	frame := NewHelloFrame("L1102", "192.168.31.23")

	var buf bytes.Buffer
	if err := frame.Encode(&buf); err != nil {
		t.Fatalf("编码失败: %v", err)
	}

	decoded, err := DecodeFrame(&buf)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}

	source, err := ParseHelloFrame(decoded)
	if err != nil {
		t.Fatalf("解析 Hello 失败: %v", err)
	}
	if source.Hostname != "L1102" || source.IP != "192.168.31.23" {
		t.Fatalf("身份不匹配: %#v", source)
	}
}

func TestRelayFrameRoundTrip(t *testing.T) {
	original := NewFrame(TypeText, []byte("from another client"))
	relay, err := NewRelayFrame("L1102", "192.168.31.23", &original)
	if err != nil {
		t.Fatalf("创建中继帧失败: %v", err)
	}
	if relay.Type != TypeRelayText {
		t.Fatalf("中继帧类型=%02x, want %02x", relay.Type, TypeRelayText)
	}

	source, unwrapped, err := UnwrapRelayFrame(&relay)
	if err != nil {
		t.Fatalf("解开中继帧失败: %v", err)
	}
	if source.Hostname != "L1102" || source.IP != "192.168.31.23" {
		t.Fatalf("来源不匹配: %#v", source)
	}
	if unwrapped.Type != TypeText {
		t.Fatalf("原始帧类型=%02x, want %02x", unwrapped.Type, TypeText)
	}
	if !bytes.Equal(unwrapped.Payload, original.Payload) {
		t.Fatalf("原始 Payload 不匹配")
	}
	if unwrapped.Hash != original.Hash {
		t.Fatalf("原始 Hash 不匹配")
	}
}

func TestNewRelayFrameRejectsUnsupportedType(t *testing.T) {
	heartbeat := NewFrame(TypeHeartbeat, nil)
	if _, err := NewRelayFrame("node", "192.168.31.23", &heartbeat); err == nil {
		t.Fatal("心跳帧不应能创建中继帧")
	}
}
