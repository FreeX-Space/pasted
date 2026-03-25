// macOS 平台剪贴板图像读取（支持 PNG + TIFF）
// 解决 golang.design/x/clipboard 只读 NSPasteboardTypePNG 导致截图不被检测的问题

//go:build darwin && !ios

#import <Foundation/Foundation.h>
#import <Cocoa/Cocoa.h>

// pasteboard_read_image 从剪贴板读取图像数据（优先 PNG，回退 TIFF→PNG）
unsigned int pasteboard_read_image(void **out) {
    @autoreleasepool {
        NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];

        // 优先尝试 PNG 格式
        NSData *data = [pasteboard dataForType:NSPasteboardTypePNG];
        if (data != nil) {
            NSUInteger siz = [data length];
            *out = malloc(siz);
            [data getBytes:*out length:siz];
            return siz;
        }

        // 回退：读取 TIFF（macOS 截图格式）并转换为 PNG
        data = [pasteboard dataForType:NSPasteboardTypeTIFF];
        if (data != nil) {
            NSBitmapImageRep *imageRep = [NSBitmapImageRep imageRepWithData:data];
            if (imageRep != nil) {
                NSData *pngData = [imageRep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
                if (pngData != nil) {
                    NSUInteger siz = [pngData length];
                    *out = malloc(siz);
                    [pngData getBytes:*out length:siz];
                    return siz;
                }
            }
        }

        return 0;
    }
}

// pasteboard_change_count 返回剪贴板变更计数
long pasteboard_change_count() {
    @autoreleasepool {
        return (long)[[NSPasteboard generalPasteboard] changeCount];
    }
}
