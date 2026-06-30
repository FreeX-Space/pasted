// pasted — 跨平台局域网剪贴板同步 CLI 工具
//
// 功能：
// - 服务端通过 -L 监听 TLS 连接，作为剪贴板中继
// - 客户端通过 -F 连接服务端，同步文本 + 图像剪贴板
// - SHA-256 防回音风暴
package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	cb "github.com/babafeng/pasted/clipboard"
	"github.com/babafeng/pasted/logger"
	"github.com/babafeng/pasted/network"
	"github.com/babafeng/pasted/peer"
)

const (
	Version = "v1.0"
	// ListenPort 默认监听端口
	ListenPort = 48217
	// HeartbeatInterval 空闲连接保活间隔
	HeartbeatInterval = 5 * time.Second
)

type runConfig struct {
	mode     string
	endpoint string
}

func main() {
	fmt.Printf("pasted - %s\n\n", Version)

	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		logger.Error("%v", err)
		printUsage()
		os.Exit(2)
	}

	switch cfg.mode {
	case "listen":
		addr, err := parseTLSEndpoint(cfg.endpoint, "0.0.0.0")
		if err != nil {
			logger.Error("解析监听地址失败: %v", err)
			os.Exit(2)
		}
		if err := runRelayServer(addr); err != nil {
			logger.Error("服务端退出: %v", err)
			os.Exit(1)
		}
	case "forward":
		addr, err := parseTLSEndpoint(cfg.endpoint, "")
		if err != nil {
			logger.Error("解析服务端地址失败: %v", err)
			os.Exit(2)
		}
		if err := runRelayClient(addr); err != nil {
			logger.Error("客户端退出: %v", err)
			os.Exit(1)
		}
	default:
		printUsage()
		os.Exit(2)
	}
}

func parseArgs(args []string) (runConfig, error) {
	var cfg runConfig
	if len(args) == 0 {
		return cfg, fmt.Errorf("缺少运行模式")
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			printUsage()
			os.Exit(0)
		case arg == "-L":
			if cfg.mode != "" {
				return cfg, fmt.Errorf("只能指定一个运行模式")
			}
			cfg.mode = "listen"
			cfg.endpoint = "tls://0.0.0.0:48217"
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				cfg.endpoint = args[i]
			}
		case strings.HasPrefix(arg, "-L="):
			if cfg.mode != "" {
				return cfg, fmt.Errorf("只能指定一个运行模式")
			}
			cfg.mode = "listen"
			cfg.endpoint = strings.TrimPrefix(arg, "-L=")
			if cfg.endpoint == "" {
				cfg.endpoint = "tls://0.0.0.0:48217"
			}
		case arg == "-F":
			if cfg.mode != "" {
				return cfg, fmt.Errorf("只能指定一个运行模式")
			}
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return cfg, fmt.Errorf("-F 需要指定服务端地址，例如 tls://1.1.1.1:48217")
			}
			i++
			cfg.mode = "forward"
			cfg.endpoint = args[i]
		case strings.HasPrefix(arg, "-F="):
			if cfg.mode != "" {
				return cfg, fmt.Errorf("只能指定一个运行模式")
			}
			cfg.mode = "forward"
			cfg.endpoint = strings.TrimPrefix(arg, "-F=")
			if cfg.endpoint == "" {
				return cfg, fmt.Errorf("-F 需要指定服务端地址，例如 tls://1.1.1.1:48217")
			}
		default:
			return cfg, fmt.Errorf("未知参数: %s", arg)
		}
	}

	if cfg.mode == "" {
		return cfg, fmt.Errorf("缺少运行模式")
	}
	return cfg, nil
}

func printUsage() {
	fmt.Println("用法:")
	fmt.Println("  服务端: ./pasted -L tls://0.0.0.0:48217")
	fmt.Println("  客户端: ./pasted -F tls://1.1.1.1:48217")
	fmt.Println()
	fmt.Println("说明:")
	fmt.Println("  -L 可省略地址，默认 tls://0.0.0.0:48217")
	fmt.Println("  -F 地址可省略端口，默认 48217；当前仅支持 tls://")
}

func parseTLSEndpoint(raw, defaultHost string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if defaultHost == "" {
			return "", fmt.Errorf("地址不能为空")
		}
		raw = defaultHost
	}

	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", err
		}
		if parsed.Scheme != "tls" {
			return "", fmt.Errorf("不支持的协议 %q，仅支持 tls://", parsed.Scheme)
		}
		raw = parsed.Host
	}

	if raw == "" {
		if defaultHost == "" {
			return "", fmt.Errorf("地址不能为空")
		}
		raw = defaultHost
	}

	if strings.HasPrefix(raw, ":") {
		if defaultHost == "" {
			return "", fmt.Errorf("缺少主机名: %s", raw)
		}
		return net.JoinHostPort(defaultHost, strings.TrimPrefix(raw, ":")), nil
	}

	host, port, err := net.SplitHostPort(raw)
	if err == nil {
		if host == "" {
			host = defaultHost
		}
		if host == "" {
			return "", fmt.Errorf("缺少主机名: %s", raw)
		}
		if port == "" {
			port = strconv.Itoa(ListenPort)
		}
		return net.JoinHostPort(host, port), nil
	}

	return net.JoinHostPort(raw, strconv.Itoa(ListenPort)), nil
}

func runRelayServer(listenAddr string) error {
	peerMgr := peer.NewManager()

	cert, err := network.GenerateSelfSignedCert()
	if err != nil {
		return fmt.Errorf("生成 TLS 证书失败: %w", err)
	}

	onRecv := func(peerAddr string, frame *network.Frame) {
		dataType := frameDataType(frame)
		sender := peerFromAddr(peerAddr, nil)
		if p, ok := peerMgr.Get(peerAddr); ok {
			sender = p
		}

		sent := peerMgr.BroadcastExcept(peerAddr, frame)
		for _, p := range sent {
			logger.LogSync(
				sender.Hostname, sender.IP,
				p.Hostname, p.IP,
				frame.Hash, dataType, len(frame.Payload),
			)
		}
		logger.Info("已从 %s 转发 [%s][%d bytes] 给 %d 个客户端", peerAddr, dataType, len(frame.Payload), len(sent))
	}

	onConnect := func(peerAddr string, client *network.Client) {
		peerMgr.Add(peerAddr, peerFromAddr(peerAddr, client))
		logger.Info("客户端已连接: %s，当前客户端数: %d", peerAddr, peerMgr.Count())
	}

	onDisconnect := func(peerAddr string) {
		peerMgr.Remove(peerAddr)
		logger.Info("客户端已断开: %s，当前客户端数: %d", peerAddr, peerMgr.Count())
	}

	serverTLSConfig := network.NewServerTLSConfig(cert)
	server, err := network.NewServerAddr(listenAddr, serverTLSConfig, onRecv, onConnect, onDisconnect)
	if err != nil {
		return err
	}
	defer server.Close()

	done := make(chan struct{})
	go sendHeartbeats(done, peerMgr)

	logger.Info("中继服务端已就绪: tls://%s", listenAddr)
	waitForSignal()
	close(done)
	return nil
}

func runRelayClient(serverAddr string) error {
	localHost, localIP := localIdentity()
	peerMgr := peer.NewManager()

	clipMon, err := cb.NewMonitor(func(frame *network.Frame) {
		sent := peerMgr.Broadcast(frame)
		dataType := frameDataType(frame)
		for _, p := range sent {
			logger.LogSync(localHost, localIP, p.Hostname, p.IP, frame.Hash, dataType, len(frame.Payload))
		}
		if len(sent) == 0 {
			logger.Warn("当前未连接服务端，跳过本地剪贴板变更 [%s][%d bytes]", dataType, len(frame.Payload))
		}
	})
	if err != nil {
		return fmt.Errorf("初始化剪贴板失败: %w", err)
	}
	clipMon.Start()
	defer clipMon.Stop()

	done := make(chan struct{})
	go sendHeartbeats(done, peerMgr)

	go func() {
		clientTLSConfig := network.NewClientTLSConfig()
		for {
			select {
			case <-done:
				return
			default:
			}

			logger.Info("正在连接中继服务端 tls://%s ...", serverAddr)
			client, err := network.NewClient(serverAddr, clientTLSConfig)
			if err != nil {
				logger.Error("连接中继服务端失败: %v，3 秒后重试...", err)
				if !sleepOrDone(3*time.Second, done) {
					return
				}
				continue
			}

			serverPeer := &peer.Peer{
				Hostname: "relay",
				IP:       serverAddr,
				Client:   client,
			}
			peerMgr.Add(serverAddr, serverPeer)

			disconnected := make(chan struct{})
			go client.ReadLoop(func(peerAddr string, frame *network.Frame) {
				dataType := frameDataType(frame)
				if clipMon.WriteFromRemote(frame) {
					logger.LogSync("relay", peerAddr, localHost, localIP, frame.Hash, dataType, len(frame.Payload))
					logger.Info("远端数据已写入本地剪贴板 [%s][%d bytes]", dataType, len(frame.Payload))
				}
			}, func() {
				peerMgr.Remove(serverAddr)
				close(disconnected)
			})

			logger.Info("已连接中继服务端 tls://%s", serverAddr)
			select {
			case <-done:
				peerMgr.Remove(serverAddr)
				return
			case <-disconnected:
				logger.Warn("与中继服务端断开，3 秒后重连...")
				if !sleepOrDone(3*time.Second, done) {
					return
				}
			}
		}
	}()

	logger.Info("客户端已就绪，服务端: tls://%s", serverAddr)
	waitForSignal()
	close(done)
	return nil
}

func sendHeartbeats(done <-chan struct{}, peerMgr *peer.Manager) {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			heartbeatFrame := network.NewFrame(network.TypeHeartbeat, nil)
			peerMgr.Broadcast(&heartbeatFrame)
		}
	}
}

func frameDataType(frame *network.Frame) logger.DataType {
	if frame.Type == network.TypeImage {
		return logger.DataTypeImage
	}
	return logger.DataTypeText
}

func peerFromAddr(addr string, client *network.Client) *peer.Peer {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	return &peer.Peer{
		Hostname: host,
		IP:       addr,
		Client:   client,
	}
}

func localIdentity() (string, string) {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "localhost"
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return hostname, "127.0.0.1"
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		return hostname, ipNet.IP.String()
	}
	return hostname, "127.0.0.1"
}

func waitForSignal() {
	logger.Info("按 Ctrl+C 退出")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Println("\n再见！")
}

func sleepOrDone(d time.Duration, done <-chan struct{}) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-done:
		return false
	case <-timer.C:
		return true
	}
}
