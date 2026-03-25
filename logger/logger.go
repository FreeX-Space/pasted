// Package logger 提供 pasted 的结构化日志输出。
// 日志格式: [Time] Sender(SenderIP) --> Receiver(ReceiverIP) [SHA256][Type][Size]
package logger

import (
	"encoding/hex"
	"fmt"
	"time"
)

// DataType 表示同步的数据类型
type DataType string

const (
	DataTypeText  DataType = "TEXT"
	DataTypeImage DataType = "IMAGE"
)

// LogSync 输出一条同步日志，格式为:
// 2026-03-24 10:00:05 MMac(192.168.0.10) --> WinPC(192.168.0.11) [sha256][TEXT][1024 bytes]
func LogSync(senderHost, senderIP, receiverHost, receiverIP string, hash [32]byte, dataType DataType, size int) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	hashStr := hex.EncodeToString(hash[:])
	fmt.Printf("%s [INFO] %s(%s) --> %s(%s) [%s][%s][%d bytes]\n",
		ts, senderHost, senderIP, receiverHost, receiverIP, hashStr, dataType, size)
}

// Info 输出普通信息日志
func Info(format string, args ...any) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("%s [INFO] %s\n", ts, fmt.Sprintf(format, args...))
}

// Error 输出错误日志
func Error(format string, args ...any) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("%s [ERROR] %s\n", ts, fmt.Sprintf(format, args...))
}

// Warn 输出警告日志
func Warn(format string, args ...any) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("%s [WARN] %s\n", ts, fmt.Sprintf(format, args...))
}
