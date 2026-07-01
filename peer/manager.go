// Package peer 管理已连接的对端节点列表，提供广播发送能力。
package peer

import (
	"sync"

	"github.com/babafeng/pasted/logger"
	"github.com/babafeng/pasted/network"
)

// Peer 表示一个已连接的对端节点
type Peer struct {
	Hostname string
	IP       string
	Client   *network.Client
}

// ID 返回节点标识: 主机名(IP)
func (p *Peer) ID() string {
	return p.Hostname + "(" + p.IP + ")"
}

// Manager 管理已连接的对端节点列表（线程安全）
type Manager struct {
	mu    sync.RWMutex
	peers map[string]*Peer // key: "ip:port" 或 "ip"
}

type peerEntry struct {
	key  string
	peer *Peer
}

// NewManager 创建节点管理器
func NewManager() *Manager {
	return &Manager{
		peers: make(map[string]*Peer),
	}
}

// Has 检查指定 key 的节点是否已存在
func (m *Manager) Has(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.peers[key]
	return ok
}

// Get 返回指定节点快照。
func (m *Manager) Get(key string) (*Peer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.peers[key]
	return p, ok
}

// Add 添加一个已连接节点
func (m *Manager) Add(key string, p *Peer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers[key] = p
	logger.Info("节点已加入: %s", p.ID())
}

// UpdateIdentity 更新已连接节点的展示身份。
func (m *Manager) UpdateIdentity(key, hostname, ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.peers[key]
	if !ok {
		return false
	}
	if hostname != "" {
		p.Hostname = hostname
	}
	if ip != "" {
		p.IP = ip
	}
	return true
}

// Remove 移除一个节点
func (m *Manager) Remove(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.peers[key]; ok {
		_ = p.Client.Close()
		logger.Info("节点已移除: %s", p.ID())
		delete(m.peers, key)
	}
}

// Broadcast 向所有已连接节点广播一个帧
// 返回发送成功的节点列表
func (m *Manager) Broadcast(frame *network.Frame) []*Peer {
	return m.BroadcastExcept("", frame)
}

// BroadcastExcept 向除 excludeKey 外的所有已连接节点广播一个帧。
func (m *Manager) BroadcastExcept(excludeKey string, frame *network.Frame) []*Peer {
	m.mu.RLock()
	// 复制列表，避免长时间持锁
	snapshot := make([]peerEntry, 0, len(m.peers))
	for key, p := range m.peers {
		if key == excludeKey {
			continue
		}
		snapshot = append(snapshot, peerEntry{key: key, peer: p})
	}
	m.mu.RUnlock()

	var sent []*Peer
	var failed []string
	for _, entry := range snapshot {
		p := entry.peer
		if err := p.Client.Send(frame); err != nil {
			logger.Error("发送到 %s 失败: %v", p.ID(), err)
			failed = append(failed, entry.key)
		} else {
			sent = append(sent, p)
		}
	}

	// 移除失败的节点
	for _, key := range failed {
		m.Remove(key)
	}

	return sent
}

// Count 返回已连接节点数量
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

// List 返回所有节点快照
func (m *Manager) List() []*Peer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Peer, 0, len(m.peers))
	for _, p := range m.peers {
		list = append(list, p)
	}
	return list
}
