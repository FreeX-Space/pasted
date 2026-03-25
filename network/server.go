package network

import (
	"crypto/tls"
	"fmt"
	"net"

	"github.com/babafeng/pasted/logger"
)

// OnReceiveFunc 接收到帧数据时的回调函数类型
// peerAddr 为远端地址（ip:port 格式）
type OnReceiveFunc func(peerAddr string, frame *Frame)

// Server TLS 服务端，监听入站连接
type Server struct {
	listener net.Listener
	onRecv   OnReceiveFunc
}

// NewServer 创建并启动 TLS 服务端，监听指定端口
func NewServer(port int, tlsConfig *tls.Config, onRecv OnReceiveFunc) (*Server, error) {
	addr := fmt.Sprintf(":%d", port)
	listener, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("TLS 监听失败 %s: %w", addr, err)
	}

	s := &Server{
		listener: listener,
		onRecv:   onRecv,
	}
	go s.acceptLoop()
	logger.Info("TLS 服务端已启动，监听 %s", addr)
	return s, nil
}

// acceptLoop 循环接受新连接
func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// listener 关闭时退出
			logger.Error("Accept 失败: %v", err)
			return
		}
		go s.handleConn(conn)
	}
}

// handleConn 处理单个入站连接，持续读取帧数据
func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	peerAddr := conn.RemoteAddr().String()
	logger.Info("入站连接: %s", peerAddr)

	for {
		frame, err := DecodeFrame(conn)
		if err != nil {
			logger.Warn("连接 %s 读取帧失败: %v", peerAddr, err)
			return
		}
		if s.onRecv != nil {
			s.onRecv(peerAddr, frame)
		}
	}
}

// Close 关闭服务端
func (s *Server) Close() error {
	return s.listener.Close()
}

// Addr 返回监听地址
func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}
