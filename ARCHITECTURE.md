# Pasted — 中继式局域网/公网剪贴板同步工具 架构设计

## 1. 概述

`pasted` 是一个 CLI 剪贴板同步工具。当前运行模型改为“一个中继服务端 + 多个客户端”：

- 服务端只负责接收客户端上传的剪贴板帧，并转发给除发送者之外的其他客户端。
- 客户端负责监听本机剪贴板，把本地变更发送到服务端，并接收服务端转发的数据写入本地剪贴板。
- 支持文本和图像数据，传输层使用 TLS 1.3。

## 2. 启动方式

服务端：

```bash
./pasted -L tls://0.0.0.0:48217
```

`-L` 可以省略地址，默认监听 `tls://0.0.0.0:48217`：

```bash
./pasted -L
```

客户端：

```bash
./pasted -F tls://1.1.1.1:48217
```

客户端地址可以省略端口，默认端口为 `48217`：

```bash
./pasted -F tls://1.1.1.1
```

## 3. 中继数据流

```
Client A 剪贴板变更
  -> Client A 发送 Frame 到 Relay Server
  -> Relay Server 按连接 key 排除 Client A
  -> Relay Server 转发给 Client B / Client C / ...
  -> 其他客户端写入本机剪贴板
```

服务端不写本机剪贴板，也不依赖图形环境。

## 4. 通信协议

自定义帧协议保持不变：

```
Type(1B) + Hash(32B) + Length(4B, BigEndian) + Payload(NB)
```

帧类型：

- `0x01`: 文本
- `0x02`: 图像
- `0x03`: 心跳

`Hash` 是 `Payload` 的 SHA-256，用于完整性校验和剪贴板回音抑制。最大 payload 为 50MB。

## 5. 连接与转发

服务端按完整远端地址作为连接 key，例如 `203.0.113.10:53122`。这样同一个 NAT/IP 后面的多个客户端可以同时连接。

转发逻辑：

```go
peerMgr.BroadcastExcept(senderAddr, frame)
```

发送失败的连接按连接 key 移除，避免同 IP 的其他客户端被误删。

## 6. 保活

客户端和服务端都会每 5 秒发送一次心跳帧。读循环设置 15 秒 read deadline，长期无数据或连接断开会触发清理/重连。

客户端断开后会每 3 秒重试连接服务端。

## 7. TLS

每次启动时在内存中生成 ECDSA P-256 自签证书，并强制 TLS 1.3。

当前客户端仍使用 `InsecureSkipVerify: true`，因此具备传输加密，但没有服务端身份认证。跨不可信网络使用时，应增加证书指纹确认、共享口令配对或证书 pinning。

## 8. 目录结构

```
pasted/
├── main.go              # -L/-F CLI，服务端/客户端主流程
├── clipboard/           # 剪贴板监听、写入、防回音
├── network/             # TLS、连接读写、帧协议
├── peer/                # 连接管理与排除来源广播
├── logger/              # 日志格式
├── discovery/           # 旧 mDNS 发现模块，当前中继模式未使用
├── go.mod
└── go.sum
```
