package network

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/babafeng/pasted/logger"
)

// Client TLS 客户端，管理到远端节点的连接
type Client struct {
	conn net.Conn
	mu   sync.Mutex
}

// NewClient 建立到目标地址的 TLS 连接
func NewClient(addr string, tlsConfig *tls.Config) (*Client, error) {
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("TLS 拨号失败 %s: %w", addr, err)
	}
	logger.Info("已连接到远端节点: %s", addr)
	return &Client{conn: conn}, nil
}

// NewClientFromConn 从已有连接创建 Client（用于入站连接的反向发送）
func NewClientFromConn(conn net.Conn) *Client {
	return &Client{conn: conn}
}


// Send 向远端节点发送一个帧（线程安全）
func (c *Client) Send(frame *Frame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := frame.Encode(c.conn); err != nil {
		return fmt.Errorf("发送帧失败: %w", err)
	}
	return nil
}

// ReadLoop 持续从远端读取帧数据，实现全双工。
// onRecv: 接收到帧的回调
// onDisconnect: 读取出错（如连接断开）时的回调
func (c *Client) ReadLoop(onRecv func(peerAddr string, frame *Frame), onDisconnect func()) {
	peerAddr := c.conn.RemoteAddr().String()
	for {
		c.conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		frame, err := DecodeFrame(c.conn)
		if err != nil {
			logger.Warn("远端连接 %s 读取帧失败/已断开: %v", peerAddr, err)
			c.Close()
			if onDisconnect != nil {
				onDisconnect()
			}
			return
		}
		if frame.Type == TypeHeartbeat {
			continue // heartbeat only used to keep connection alive
		}
		if onRecv != nil {
			onRecv(peerAddr, frame)
		}
	}
}

// Close 关闭连接
func (c *Client) Close() error {
	return c.conn.Close()
}

// RemoteAddr 返回远端地址
func (c *Client) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}
