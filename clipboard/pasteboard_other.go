// 非 macOS 平台：使用 golang.design/x/clipboard 库的 FmtImage Watch 作为回退

//go:build !darwin

package clipboard

import (
	"context"

	"golang.design/x/clipboard"
)

// watchImageData 非 macOS 平台直接使用库的 Watch
func watchImageData(ctx context.Context) <-chan []byte {
	return clipboard.Watch(ctx, clipboard.FmtImage)
}
