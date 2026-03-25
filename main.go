// pasted — 跨平台局域网剪贴板同步 CLI 工具
//
// 功能：
// - 通过 mDNS 自动发现局域网内其他 pasted 节点
// - TLS 1.3 加密传输剪贴板内容（文本 + 图像）
// - SHA-256 防回音风暴
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	cb "github.com/babafeng/pasted/clipboard"
	"github.com/babafeng/pasted/discovery"
	"github.com/babafeng/pasted/logger"
	"github.com/babafeng/pasted/network"
	"github.com/babafeng/pasted/peer"

	"time"
)

const (
	Version = "v1.0"
	// ListenPort 默认监听端口
	ListenPort = 48217
	// BrowseInterval mDNS 浏览间隔
	BrowseInterval = 5 * time.Second
)

func main() {
	fmt.Printf("pasted - %s\n", Version)
	fmt.Println()

	// 1. 初始化节点管理器
	peerMgr := peer.NewManager()

	// 2. 初始化 mDNS 服务
	mdnsSvc, err := discovery.NewService(ListenPort)
	if err != nil {
		logger.Error("初始化 mDNS 失败: %v", err)
		os.Exit(1)
	}
	logger.Info("本机标识: %s", mdnsSvc.LocalID())

	// 3. 初始化剪贴板监控（检测到本地变更 → 广播给所有已连接节点）
	clipMon, err := cb.NewMonitor(func(frame *network.Frame) {
		sent := peerMgr.Broadcast(frame)
		dataType := logger.DataTypeText
		if frame.Type == network.TypeImage {
			dataType = logger.DataTypeImage
		}
		for _, p := range sent {
			logger.LogSync(
				mdnsSvc.LocalHostname(), mdnsSvc.LocalIP(),
				p.Hostname, p.IP,
				frame.Hash, dataType, len(frame.Payload),
			)
		}
	})
	if err != nil {
		logger.Error("初始化剪贴板失败: %v", err)
		os.Exit(1)
	}

	// 4. 生成自签 TLS 证书
	cert, err := network.GenerateSelfSignedCert()
	if err != nil {
		logger.Error("生成 TLS 证书失败: %v", err)
		os.Exit(1)
	}
	logger.Info("TLS 自签证书已生成（内存中，ECDSA P-256）")

	// 5. 启动 TLS 服务端（接收远端同步数据 → 写入本地剪贴板）
	serverTLSConfig := network.NewServerTLSConfig(cert)
	_, err = network.NewServer(ListenPort, serverTLSConfig, func(peerAddr string, frame *network.Frame) {
		// 从 peerAddr 解析 IP（去掉端口）
		peerIP := peerAddr
		if idx := strings.LastIndex(peerAddr, ":"); idx > 0 {
			peerIP = peerAddr[:idx]
		}

		dataType := logger.DataTypeText
		if frame.Type == network.TypeImage {
			dataType = logger.DataTypeImage
		}

		// 尝试找到发送方节点的 Hostname
		senderHostname := peerIP
		for _, p := range peerMgr.List() {
			if p.IP == peerIP {
				senderHostname = p.Hostname
				break
			}
		}

		if clipMon.WriteFromRemote(frame) {
			logger.LogSync(
				senderHostname, peerIP,
				mdnsSvc.LocalHostname(), mdnsSvc.LocalIP(),
				frame.Hash, dataType, len(frame.Payload),
			)
			logger.Info("远端数据已写入本地剪贴板 [%s][%d bytes]", dataType, len(frame.Payload))
		}
	})
	if err != nil {
		logger.Error("启动 TLS 服务端失败: %v", err)
		os.Exit(1)
	}

	// 6. 注册 mDNS 服务
	if err := mdnsSvc.Register(); err != nil {
		logger.Error("注册 mDNS 服务失败: %v", err)
		os.Exit(1)
	}
	defer mdnsSvc.Close()

	// 7. 启动剪贴板监听
	clipMon.Start()
	defer clipMon.Stop()

	// 8. 启动 mDNS 浏览，异步发现节点
	done := make(chan struct{})
	foundCh := mdnsSvc.Browse(BrowseInterval, done)

	// 8.5 如果指定了目标 IP，启动主动连接 goroutine
	if len(os.Args) > 1 {
		targetIP := os.Args[1]
		go func() {
			addr := fmt.Sprintf("%s:%d", targetIP, ListenPort)
			clientTLSConfig := network.NewClientTLSConfig()
			for {
				logger.Info("正在主动连接 %s ...", addr)
				client, err := network.NewClient(addr, clientTLSConfig)
				if err != nil {
					logger.Error("连接 %s 失败: %v，1 秒后重试...", addr, err)
					time.Sleep(1 * time.Second)
					continue
				}
				peerMgr.Add(targetIP, &peer.Peer{
					Hostname: targetIP,
					IP:       targetIP,
					Client:   client,
				})
				logger.Info("✅ 已主动连接到 %s", addr)
				break
			}
		}()
	}

	// 9. 用户交互 goroutine：发现新节点后询问是否连接
	go func() {
		reader := bufio.NewReader(os.Stdin)
		clientTLSConfig := network.NewClientTLSConfig()

		for peerInfo := range foundCh {
			fmt.Printf("\n发现新节点: %s — 是否建立连接? (Y/n): ", peerInfo.ID())
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))

			if answer == "n" || answer == "no" {
				logger.Info("已跳过节点: %s", peerInfo.ID())
				continue
			}

			// 建立 TLS 连接
			addr := fmt.Sprintf("%s:%d", peerInfo.IP, peerInfo.Port)
			client, err := network.NewClient(addr, clientTLSConfig)
			if err != nil {
				logger.Error("连接节点 %s 失败: %v", peerInfo.ID(), err)
				continue
			}

			peerMgr.Add(peerInfo.IP, &peer.Peer{
				Hostname: peerInfo.Hostname,
				IP:       peerInfo.IP,
				Client:   client,
			})

			logger.Info("✅ 已与 %s 建立加密连接", peerInfo.ID())
		}
	}()

	// 10. 等待退出信号
	logger.Info("pasted 已就绪，等待节点发现和剪贴板同步...")
	logger.Info("按 Ctrl+C 退出")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	close(done)
	fmt.Println("\n再见！")
}
