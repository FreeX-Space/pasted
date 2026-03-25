package network

import (
	"crypto/tls"
	"fmt"
	"net"
	"testing"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	cert, err := GenerateSelfSignedCert()
	if err != nil {
		t.Fatalf("证书生成失败: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("证书链为空")
	}
	if cert.PrivateKey == nil {
		t.Fatal("私钥为空")
	}
}

func TestTLSHandshake(t *testing.T) {
	// 生成证书
	cert, err := GenerateSelfSignedCert()
	if err != nil {
		t.Fatalf("证书生成失败: %v", err)
	}

	// 启动 TLS 服务端
	serverConfig := NewServerTLSConfig(cert)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverConfig)
	if err != nil {
		t.Fatalf("TLS 监听失败: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)

	// 服务端 goroutine
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()

		buf := make([]byte, 5)
		n, err := conn.Read(buf)
		if err != nil {
			serverDone <- err
			return
		}
		if string(buf[:n]) != "hello" {
			serverDone <- fmt.Errorf("期望 'hello', 实际 '%s'", string(buf[:n]))
			return
		}
		serverDone <- nil
	}()

	// 客户端连接
	clientConfig := NewClientTLSConfig()
	conn, err := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", addr.Port), clientConfig)
	if err != nil {
		t.Fatalf("TLS 拨号失败: %v", err)
	}
	defer conn.Close()

	// 验证 TLS 1.3
	state := conn.ConnectionState()
	if state.Version != tls.VersionTLS13 {
		t.Errorf("期望 TLS 1.3, 实际版本: 0x%04x", state.Version)
	}

	// 发送数据
	_, err = conn.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("发送数据失败: %v", err)
	}

	// 等待服务端确认
	if err := <-serverDone; err != nil {
		t.Fatalf("服务端错误: %v", err)
	}
}
