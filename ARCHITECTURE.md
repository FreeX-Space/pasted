# Pasted — 跨平台局域网剪贴板同步工具 架构设计

## 1. 概述

`pasted` 是一个纯 CLI 工具，在 macOS 和 Windows 之间通过局域网实现剪贴板实时同步。支持纯文本和图像（含图片文件自动提取像素数据），使用 TLS 1.3 加密通信，通过 mDNS 自动发现对端节点。

---

## 2. 服务发现方案

### 选型：mDNS (Multicast DNS)

使用 `github.com/hashicorp/mdns` 库实现局域网内零配置服务发现。

**工作流程：**

```
启动 pasted
  ├── 注册 mDNS 服务: _pasted._tcp, 端口 48217
  │     ServiceName = "<hostname>(<ip>)"
  └── 启动 mDNS 浏览器，持续监听同类型服务
        ├── 发现新节点 → 终端提示用户确认(Y/n)
        │     ├── Y → 发起 TLS 连接，加入已连接节点列表
        │     └── n → 忽略该节点
        └── 节点离线 → 自动从已连接列表移除
```

**选择 mDNS 而非 UDP 广播的理由：**
- mDNS 是标准化的服务发现协议（RFC 6762），macOS/Windows 原生支持
- `hashicorp/mdns` 成熟稳定，API 简洁
- 比原始 UDP 广播更可靠，自带服务注册/注销生命周期管理

---

## 3. 通信架构

### TLS 1.3 加密通信

```
┌──────────────┐        TLS 1.3         ┌──────────────┐
│   Node A     │◄──────────────────────►│   Node B     │
│  :48217      │   自签证书 / 跳过校验   │  :48217      │
└──────────────┘                        └──────────────┘
```

- 每个节点启动时在内存中使用 `crypto/tls` + `crypto/x509` 自签发 TLS 证书
- TLS 配置中设置 `InsecureSkipVerify: true`，局域网内互信
- 强制 `MinVersion: tls.VersionTLS13`

### 通信协议（自定义简单帧协议）

```
┌─────────────┬──────────────┬──────────────┬───────────────┐
│  Type (1B)  │ Hash (32B)   │ Length (4B)  │  Payload (NB) │
│  0x01=Text  │ SHA-256      │ uint32 BE    │  实际数据      │
│  0x02=Image │              │              │               │
└─────────────┴──────────────┴──────────────┴───────────────┘
```

- `Type`: 1 字节，标识文本 (0x01) 或图像 (0x02)
- `Hash`: 32 字节 SHA-256 摘要，用于去重和日志记录
- `Length`: 4 字节大端序，Payload 长度
- `Payload`: 实际剪贴板数据（文本 UTF-8 / 图像 PNG）

---

## 4. 并发模型

```
main goroutine
  │
  ├── [goroutine] mDNS 服务注册（后台保持）
  ├── [goroutine] mDNS 浏览器（发现节点 → 写入 channel）
  ├── [goroutine] TLS Server（Accept 连接 → 每连接一个 goroutine 处理接收）
  ├── [goroutine] 剪贴板文本监听 (clipboard.Watch FmtText)
  ├── [goroutine] 剪贴板图像监听 (clipboard.Watch FmtImage)
  └── [goroutine] 用户交互（读取 stdin，处理连接确认）
```

**关键数据流：**

```
剪贴板变更 → Watch goroutine 检测到
  → 计算 SHA-256
  → 与 lastSentHash 比较（防自回音）
  → 不同 → 向所有已连接节点发送
  → 更新 lastSentHash

收到远端数据 → TLS 接收 goroutine
  → 解析帧：Type + Hash + Payload
  → 与当前剪贴板 SHA-256 比较（防重复写入）
  → 不同 → 写入本地剪贴板
  → 更新 lastReceivedHash
  → 输出结构化日志
```

---

## 5. 剪贴板防无限循环（Anti-Echo Storm）

这是核心难点。采用**双重 Hash 守卫**机制：

```go
var (
    mu               sync.Mutex
    lastWrittenHash  [32]byte  // 最近一次写入本地剪贴板的数据 Hash
    lastSentHash     [32]byte  // 最近一次发送给远端的数据 Hash
)
```

### 防循环流程

```
场景：用户在 A 复制文本 "Hello"

A 端：
  1. Watch 检测到剪贴板变更，数据 = "Hello"
  2. hash = SHA256("Hello")
  3. 检查 hash != lastWrittenHash → 非回音，继续
  4. 发送给 B，更新 lastSentHash = hash

B 端：
  5. 收到 "Hello"，hash 已在帧中
  6. 检查 hash != 当前剪贴板 hash → 不同，写入
  7. 更新 lastWrittenHash = hash
  8. Watch 检测到剪贴板变更（由步骤 6 触发）
  9. hash = SHA256("Hello")
  10. 检查 hash == lastWrittenHash → 是回音！跳过发送 ✓
```

---

## 6. 图片文件特殊处理

当用户复制的是一个图片**文件**（而非截图）时：
- `clipboard.Read(clipboard.FmtImage)` 在部分平台可能返回空
- 需要检查系统剪贴板中是否有文件路径类型数据
- 若检测到图片文件路径，读取文件内容，转为 PNG 格式后作为图像类型同步

> **第一版简化策略**：依赖 `golang.design/x/clipboard` 的 `FmtImage` 格式读取。如果操作系统将复制的图片文件自动转为图像数据放入剪贴板，则可以直接获取；否则此场景在 v1 中可能需要平台特定代码补充。

---

## 7. 日志格式

```
[时间] [发送方主机名](发送方IP) --> [接收方主机名](接收方IP) [SHA256][数据类型][数据长度]
```

示例：
```
2026-03-24 10:00:05 MMac(192.168.0.10) --> WinPC(192.168.0.11) [e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855][TEXT][1024 bytes]
```

使用 Go 标准库 `log` 包，自定义格式输出到 stdout。

---

## 8. 第三方依赖清单

| 库                          | 用途                   | 选择理由                                                       |
| --------------------------- | ---------------------- | -------------------------------------------------------------- |
| `golang.design/x/clipboard` | 跨平台剪贴板读写与监听 | 唯一成熟的 Go 跨平台剪贴板库，支持文本+PNG图像，提供 Watch API |
| `github.com/hashicorp/mdns` | mDNS 局域网服务发现    | HashiCorp 出品，API 简洁，支持服务注册和浏览                   |

其余全部使用 Go 标准库：`crypto/tls`, `crypto/x509`, `crypto/sha256`, `net`, `encoding/binary`, `os`, `fmt`, `sync` 等。

---

## 9. 项目目录结构

```
pasted/
├── main.go              # 入口：解析参数、初始化各模块、阻塞等待
├── clipboard/
│   └── monitor.go       # 剪贴板监听、读写、防回音逻辑
├── discovery/
│   └── mdns.go          # mDNS 服务注册与节点发现
├── network/
│   ├── tls.go           # TLS 证书自签发、配置生成
│   ├── server.go        # TLS Server，监听入站连接
│   ├── client.go        # TLS Client，拨号连接远端节点
│   └── protocol.go      # 帧协议编解码（Type+Hash+Length+Payload）
├── peer/
│   └── manager.go       # 已连接节点管理（增/删/广播发送）
├── logger/
│   └── logger.go        # 结构化日志格式化输出
├── go.mod
└── go.sum
```
