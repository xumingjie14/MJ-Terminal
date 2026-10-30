//go:build windows

package main

// Windows implementation of the Stage Manager preview capture:
// PrintWindow with PW_RENDERFULLCONTENT renders the target
// window into a memory bitmap (it works for occluded windows;
// minimized windows are excluded by the caller), then the
// pixels are read back with GetDIBits, downscaled to the
// strip's width and PNG-encoded. Pure stdlib + LazyDLL, like
// the rest of the window layer.

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"syscall"
	"unsafe"
)

// thumbSupported: this platform can capture window previews
// (the !windows stub in winmgr_other.go declares it false).
const thumbSupported = true

var (
	gdi32 = syscall.NewLazyDLL("gdi32.dll")

	procPrintWindow         = user32.NewProc("PrintWindow")
	procGetWindowDC         = user32.NewProc("GetWindowDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBmp = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procGetDIBits           = gdi32.NewProc("GetDIBits")
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func platformCaptureWindowPNG(hwnd uint64, maxW int) ([]byte, error) {
	h := uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(h); r == 0 {
		return nil, fmt.Errorf("窗口已不存在")
	}
	var rc wmRect
	if r, _, _ := procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&rc))); r == 0 {
		return nil, fmt.Errorf("读取窗口大小失败")
	}
	w, ht := int(rc.Right-rc.Left), int(rc.Bottom-rc.Top)
	if w < 8 || ht < 8 {
		return nil, fmt.Errorf("窗口太小（%dx%d）", w, ht)
	}
	winDC, _, _ := procGetWindowDC.Call(h)
	if winDC == 0 {
		return nil, fmt.Errorf("GetWindowDC 失败")
	}
	defer procReleaseDC.Call(h, winDC)
	memDC, _, _ := procCreateCompatibleDC.Call(winDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC 失败")
	}
	defer procDeleteDC.Call(memDC)
	bmp, _, _ := procCreateCompatibleBmp.Call(winDC, uintptr(w), uintptr(ht))
	if bmp == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap 失败")
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(memDC, bmp)
	defer procSelectObject.Call(memDC, old)
	// PW_RENDERFULLCONTENT (0x2): asks the window (DWM-composed
	// apps included) to render its full content into the DC.
	if r, _, _ := procPrintWindow.Call(h, memDC, 2); r == 0 {
		return nil, fmt.Errorf("PrintWindow 拒绝了这个窗口")
	}
	// Read the pixels back as a top-down 32bpp DIB.
	bi := bitmapInfoHeader{
		Size:     uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:    int32(w),
		Height:   -int32(ht), // negative = top-down rows
		Planes:   1,
		BitCount: 32,
	}
	buf := make([]byte, w*ht*4)
	if r, _, _ := procGetDIBits.Call(memDC, bmp, 0, uintptr(ht),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0); r == 0 {
		return nil, fmt.Errorf("GetDIBits 失败")
	}
	img := image.NewRGBA(image.Rect(0, 0, w, ht))
	opaque := false
	for i := 0; i < w*ht; i++ {
		b, g, r, a := buf[i*4+0], buf[i*4+1], buf[i*4+2], buf[i*4+3]
		if a != 0 {
			opaque = true
		}
		img.Pix[i*4+0] = r
		img.Pix[i*4+1] = g
		img.Pix[i*4+2] = b
		img.Pix[i*4+3] = 255
	}
	if !opaque {
		// Some windows render with a fully-zero alpha channel
		// (their content is still in RGB) -- handled above by
		// forcing 255. Nothing else to do; the check exists so
		// a truly blank capture (all-zero RGB too) is honest.
		blank := true
		for _, px := range img.Pix {
			if px != 0 && px != 255 {
				blank = false
				break
			}
		}
		if blank {
			return nil, fmt.Errorf("抓到的画面是空白")
		}
	}
	out := image.Image(img)
	if w > maxW && maxW > 0 {
		out = downscaleRGBA(img, maxW)
	}
	var bb bytes.Buffer
	if err := png.Encode(&bb, out); err != nil {
		return nil, err
	}
	return bb.Bytes(), nil
}

// downscaleRGBA shrinks src to width maxW with nearest-neighbor
// sampling (previews do not need filtering quality, and this
// keeps the capture path dependency-free).
func downscaleRGBA(src *image.RGBA, maxW int) image.Image {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	dw := maxW
	dh := sh * dw / sw
	if dh < 1 {
		dh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		sy := y * sh / dh
		for x := 0; x < dw; x++ {
			sx := x * sw / dw
			si := src.PixOffset(sx, sy)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}
