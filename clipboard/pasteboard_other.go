// 非 macOS 平台：使用 golang.design/x/clipboard 库的 Watch

//go:build !darwin

package clipboard

import (
	"context"

	"golang.design/x/clipboard"
)

// watchClipboardData 非 macOS 平台使用库的 Watch，合并文本和图像通道
func watchClipboardData(ctx context.Context) <-chan clipChange {
	recv := make(chan clipChange, 1)

	textCh := clipboard.Watch(ctx, clipboard.FmtText)
	imgCh := clipboard.Watch(ctx, clipboard.FmtImage)

	go func() {
		defer close(recv)
		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-textCh:
				if !ok {
					return
				}
				if len(data) > 0 {
					recv <- clipChange{IsImage: false, Data: data}
				}
			case data, ok := <-imgCh:
				if !ok {
					return
				}
				if len(data) > 0 {
					recv <- clipChange{IsImage: true, Data: data}
				}
			}
		}
	}()

	return recv
}
