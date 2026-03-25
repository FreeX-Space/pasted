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

// clipChange 表示一次剪贴板变更事件
type clipChange struct {
	IsImage bool
	Data    []byte
}

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

// Start 启动剪贴板监听（统一的单 goroutine 监听文本 + 图像）
func (m *Monitor) Start() {
	go m.watchAll()
	logger.Info("剪贴板监听已启动（文本 + 图像）")
}

// watchAll 统一监听剪贴板变更（文本 + 图像）
// 使用平台特定的 watchClipboardData 实现，macOS 上确保单线程访问 NSPasteboard
func (m *Monitor) watchAll() {
	ch := watchClipboardData(m.ctx)
	for change := range ch {
		if len(change.Data) == 0 {
			continue
		}
		hash := sha256.Sum256(change.Data)

		m.mu.Lock()
		if hash == m.lastWrittenHash {
			// 这是我们自己写入的回音，跳过
			m.mu.Unlock()
			continue
		}
		m.mu.Unlock()

		var frameType byte
		var dataType string
		if change.IsImage {
			frameType = network.TypeImage
			dataType = "图像"
		} else {
			frameType = network.TypeText
			dataType = "文本"
		}

		frame := network.NewFrame(frameType, change.Data)
		logger.Info("检测到本地%s变更, 大小: %d bytes", dataType, len(change.Data))
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
