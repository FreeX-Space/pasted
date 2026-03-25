package network

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"

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

// Close 关闭连接
func (c *Client) Close() error {
	return c.conn.Close()
}

// RemoteAddr 返回远端地址
func (c *Client) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}
