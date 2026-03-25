// macOS 平台：统一的剪贴板监听（文本 + 图像）
//
// 核心原则：所有 NSPasteboard 访问必须在同一个 goroutine/线程中，
// 不可并发调用。否则 NSPasteboard 内部断言会触发 SIGTRAP 崩溃。
//
// 因此不使用 golang.design/x/clipboard 的 Watch 函数（它会创建独立 goroutine），
// 而是自行实现统一的 changeCount 轮询 + 数据读取。

//go:build darwin && !ios

package clipboard

/*
#cgo LDFLAGS: -framework Foundation -framework Cocoa
#include <stdlib.h>

extern unsigned int pasteboard_read_string(void **out);
extern unsigned int pasteboard_read_image(void **out);
extern long pasteboard_change_count();
*/
import "C"
import (
	"context"
	"runtime"
	"time"
	"unsafe"
)

// readNativeText 从 macOS 剪贴板读取文本数据
func readNativeText() []byte {
	var data unsafe.Pointer
	n := C.pasteboard_read_string(&data)
	if data == nil || n == 0 {
		return nil
	}
	defer C.free(data)
	return C.GoBytes(data, C.int(n))
}

// readNativeImage 从 macOS 剪贴板读取图像数据（PNG 或 TIFF→PNG）
func readNativeImage() []byte {
	var data unsafe.Pointer
	n := C.pasteboard_read_image(&data)
	if data == nil || n == 0 {
		return nil
	}
	defer C.free(data)
	return C.GoBytes(data, C.int(n))
}

// watchClipboardData 统一监听剪贴板变更（文本 + 图像），单 goroutine 避免并发访问 NSPasteboard
func watchClipboardData(ctx context.Context) <-chan clipChange {
	recv := make(chan clipChange, 1)

	go func() {
		// 锁定到固定 OS 线程，确保所有 NSPasteboard 调用在同一线程
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(recv)

		ti := time.NewTicker(time.Second)
		defer ti.Stop()

		lastCount := int64(C.pasteboard_change_count())

		for {
			select {
			case <-ctx.Done():
				return
			case <-ti.C:
				cur := int64(C.pasteboard_change_count())
				if lastCount != cur {
					lastCount = cur

					// 优先检查图像（PNG + TIFF 截图）
					if imgData := readNativeImage(); imgData != nil {
						recv <- clipChange{IsImage: true, Data: imgData}
						continue
					}

					// 再检查文本
					if textData := readNativeText(); textData != nil {
						recv <- clipChange{IsImage: false, Data: textData}
					}
				}
			}
		}
	}()

	return recv
}
