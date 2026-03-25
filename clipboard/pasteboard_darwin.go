// macOS 平台：通过 cgo 直接读取 NSPasteboard，支持 TIFF（截图）→ PNG 转换
//
// 解决 golang.design/x/clipboard 只读 NSPasteboardTypePNG 导致
// macOS 截图（TIFF 格式）无法被检测的问题。

//go:build darwin && !ios

package clipboard

/*
#cgo LDFLAGS: -framework Foundation -framework Cocoa
#include <stdlib.h>

// 在 pasteboard_darwin.m 中实现
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

// watchImageData 轮询 macOS 剪贴板变更，检测图像数据（支持 TIFF 截图）
func watchImageData(ctx context.Context) <-chan []byte {
	recv := make(chan []byte, 1)

	go func() {
		// 锁定到固定 OS 线程，确保 ObjC 运行时状态一致
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
					b := readNativeImage()
					if b == nil {
						// 不是图像变更（如文本），更新计数避免重复读取
						lastCount = cur
						continue
					}
					recv <- b
					lastCount = cur
				}
			}
		}
	}()

	return recv
}
