// Package clipboard 提供跨平台剪贴板监听和写入，包含防回音风暴的 Hash 守卫机制。
package clipboard

import (
	"context"
	"crypto/sha256"
	"sync"

	"github.com/babafeng/pasted/logger"
	"github.com/babafeng/pasted/network"

	"golang.design/x/clipboard"
)

// Monitor 剪贴板监控器
type Monitor struct {
	mu              sync.Mutex
	lastWrittenHash [32]byte // 最近一次由远端数据写入本地剪贴板的 Hash
	onChanged       func(frame *network.Frame)

	ctx    context.Context
	cancel context.CancelFunc
}

// NewMonitor 创建剪贴板监控器
// onChanged: 检测到本地剪贴板变更时的回调（已排除回音）
func NewMonitor(onChanged func(frame *network.Frame)) (*Monitor, error) {
	// 初始化剪贴板
	if err := clipboard.Init(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Monitor{
		onChanged: onChanged,
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

// Start 启动剪贴板监听（文本 + 图像各一个 goroutine）
func (m *Monitor) Start() {
	go m.watchText()
	go m.watchImage()
	logger.Info("剪贴板监听已启动（文本 + 图像）")
}

// watchText 监听文本剪贴板变更
func (m *Monitor) watchText() {
	ch := clipboard.Watch(m.ctx, clipboard.FmtText)
	for data := range ch {
		if len(data) == 0 {
			continue
		}
		hash := sha256.Sum256(data)

		m.mu.Lock()
		if hash == m.lastWrittenHash {
			// 这是我们自己写入的回音，跳过
			m.mu.Unlock()
			continue
		}
		m.mu.Unlock()

		// 用户在本机复制了新的文本，通知上层广播
		frame := network.NewFrame(network.TypeText, data)
		logger.Info("检测到本地文本变更, 大小: %d bytes", len(data))
		if m.onChanged != nil {
			m.onChanged(&frame)
		}
	}
}

// watchImage 监听图像剪贴板变更
func (m *Monitor) watchImage() {
	ch := clipboard.Watch(m.ctx, clipboard.FmtImage)
	for data := range ch {
		if len(data) == 0 {
			continue
		}
		hash := sha256.Sum256(data)

		m.mu.Lock()
		if hash == m.lastWrittenHash {
			m.mu.Unlock()
			continue
		}
		m.mu.Unlock()

		// 用户在本机复制/截图了新的图像，通知上层广播
		frame := network.NewFrame(network.TypeImage, data)
		logger.Info("检测到本地图像变更, 大小: %d bytes", len(data))
		if m.onChanged != nil {
			m.onChanged(&frame)
		}
	}
}

// WriteFromRemote 将远端同步来的数据写入本地剪贴板
// 写入前比对 Hash, 避免重复写入
func (m *Monitor) WriteFromRemote(frame *network.Frame) bool {
	// 读取当前剪贴板内容并计算 Hash
	var currentHash [32]byte
	switch frame.Type {
	case network.TypeText:
		cur := clipboard.Read(clipboard.FmtText)
		if cur != nil {
			currentHash = sha256.Sum256(cur)
		}
	case network.TypeImage:
		cur := clipboard.Read(clipboard.FmtImage)
		if cur != nil {
			currentHash = sha256.Sum256(cur)
		}
	}

	// 如果当前剪贴板内容与要写入的相同，跳过
	if currentHash == frame.Hash {
		return false
	}

	// 设置 lastWrittenHash，防止 Watch 触发回音
	m.mu.Lock()
	m.lastWrittenHash = frame.Hash
	m.mu.Unlock()

	// 写入剪贴板
	switch frame.Type {
	case network.TypeText:
		clipboard.Write(clipboard.FmtText, frame.Payload)
	case network.TypeImage:
		clipboard.Write(clipboard.FmtImage, frame.Payload)
	}

	return true
}

// Stop 停止剪贴板监听
func (m *Monitor) Stop() {
	m.cancel()
}
