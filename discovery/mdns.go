// Package discovery 提供基于 mDNS 的局域网节点自动发现功能。
package discovery

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/babafeng/pasted/logger"
	"github.com/hashicorp/mdns"
)

const (
	// ServiceName mDNS 服务名称
	ServiceName = "_pasted._tcp"
	// Domain mDNS 域名
	Domain = "local."
)

// PeerInfo 发现的节点信息
type PeerInfo struct {
	Hostname string
	IP       string
	Port     int
}

// ID 返回节点标识: 主机名(IP)
func (p PeerInfo) ID() string {
	return p.Hostname + "(" + p.IP + ")"
}

// Service 封装 mDNS 服务注册与节点发现
type Service struct {
	server   *mdns.Server
	localIP  string
	localIPs map[string]bool // 所有本机 IP，用于排除自发现
	hostname string
	port     int
}

// NewService 创建 mDNS 服务发现实例
func NewService(port int) (*Service, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	localIP := getLocalIP()

	// 收集所有本机 IP，用于排除自发现
	localIPs := getAllLocalIPs()

	return &Service{
		hostname: hostname,
		localIP:  localIP,
		localIPs: localIPs,
		port:     port,
	}, nil
}

// Register 注册 mDNS 服务到局域网
func (s *Service) Register() error {
	// 服务实例名 = 主机名
	info := []string{
		fmt.Sprintf("hostname=%s", s.hostname),
		fmt.Sprintf("ip=%s", s.localIP),
	}

	service, err := mdns.NewMDNSService(
		s.hostname,   // 实例名
		ServiceName,  // 服务类型
		Domain,       // 域名
		"",           // 主机名（空=自动）
		s.port,       // 端口
		nil,          // IPs（nil=自动）
		info,         // TXT 记录
	)
	if err != nil {
		return fmt.Errorf("创建 mDNS 服务失败: %w", err)
	}

	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		return fmt.Errorf("启动 mDNS 服务器失败: %w", err)
	}

	s.server = server
	logger.Info("mDNS 服务已注册: %s(%s) 端口 %d", s.hostname, s.localIP, s.port)
	return nil
}

// Browse 浏览局域网中的其他 pasted 节点。
// 发现的节点通过 channel 返回（排除自身）。
// 该函数每隔 interval 执行一次查询，直到 done channel 关闭。
func (s *Service) Browse(interval time.Duration, done <-chan struct{}) <-chan PeerInfo {
	found := make(chan PeerInfo, 16)

	go func() {
		defer close(found)

		// 静默 hashicorp/mdns 库内部的 IPv6 噪音日志
		origWriter := log.Writer()
		origFlags := log.Flags()
		log.SetOutput(io.Discard)
		defer func() {
			log.SetOutput(origWriter)
			log.SetFlags(origFlags)
		}()

		for {
			select {
			case <-done:
				return
			default:
			}

			entryCh := make(chan *mdns.ServiceEntry, 16)
			go func() {
				for entry := range entryCh {
					peer := parsePeerInfo(entry)
					if peer == nil {
						continue
					}
					// 排除自身（检查所有本机 IP，含 127.0.0.1）
					if s.isLocalIP(peer.IP) {
						continue
					}
					found <- *peer
				}
			}()

			params := &mdns.QueryParam{
				Service:             ServiceName,
				Domain:              Domain,
				Timeout:             3 * time.Second,
				Entries:             entryCh,
				DisableIPv6:         true,
				WantUnicastResponse: false,
			}

			if err := mdns.Query(params); err != nil {
				logger.Error("mDNS 查询失败: %v", err)
			}

			select {
			case <-done:
				return
			case <-time.After(interval):
			}
		}
	}()

	return found
}

// parsePeerInfo 从 mDNS 条目中解析节点信息
func parsePeerInfo(entry *mdns.ServiceEntry) *PeerInfo {
	if entry == nil {
		return nil
	}

	ip := ""
	if entry.AddrV4 != nil {
		ip = entry.AddrV4.String()
	}
	if ip == "" {
		return nil
	}

	hostname := ""
	for _, txt := range entry.InfoFields {
		if strings.HasPrefix(txt, "hostname=") {
			hostname = strings.TrimPrefix(txt, "hostname=")
		}
	}
	if hostname == "" {
		// 从服务名解析
		hostname = entry.Name
		// 移除服务后缀
		if idx := strings.Index(hostname, "."+ServiceName); idx > 0 {
			hostname = hostname[:idx]
		}
	}

	return &PeerInfo{
		Hostname: hostname,
		IP:       ip,
		Port:     entry.Port,
	}
}

// Close 关闭 mDNS 服务
func (s *Service) Close() error {
	if s.server != nil {
		return s.server.Shutdown()
	}
	return nil
}

// LocalID 返回本机节点标识
func (s *Service) LocalID() string {
	return s.hostname + "(" + s.localIP + ")"
}

// LocalHostname 返回本机主机名
func (s *Service) LocalHostname() string {
	return s.hostname
}

// LocalIP 返回本机 IP
func (s *Service) LocalIP() string {
	return s.localIP
}

// isLocalIP 检查给定 IP 是否为本机 IP（含 127.0.0.1 和所有网卡 IP）
func (s *Service) isLocalIP(ip string) bool {
	if ip == "127.0.0.1" || ip == "::1" {
		return true
	}
	return s.localIPs[ip]
}

// getAllLocalIPs 收集本机所有 IPv4 地址
func getAllLocalIPs() map[string]bool {
	ips := map[string]bool{
		"127.0.0.1": true,
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok {
			if ipNet.IP.To4() != nil {
				ips[ipNet.IP.String()] = true
			}
		}
	}
	return ips
}

// getLocalIP 获取本机局域网 IP 地址（优先非回环）
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}
