//go:build windows

package main

// ---------- v0.37: notification-area (tray) icon ----------
//
// When the desktop view is closed under the close-to-tray rule,
// the daemon parks one icon in the host's notification area (the
// user expects it in the hidden overflow behind the ^ chevron --
// where Windows puts new icons by default). Left click (or the
// 打开桌面 menu item) restores the whole set and reopens the
// desktop view; 退出系统 quits through the same /api/shutdown
// path as the Settings app (see onTrayIconExit, winset.go).
//
// Implementation notes: pure stdlib syscalls like the rest of the
// window code. A dedicated OS thread runs a classic message loop
// behind a message-only window; all Shell_NotifyIcon calls happen
// on that thread. The icon artwork is generated in code (ICO
// container with PNG payloads, drawn pixel-wise below) so the
// single-file promise holds -- no external asset files.

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	procShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procAppendMenuW            = user32.NewProc("AppendMenuW")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procCreateIconFromResource = user32.NewProc("CreateIconFromResourceEx")
	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
)

const (
	trayCallbackMsg = 0x0401 // WM_USER + 1: shell -> us, icon events
	trayReconcile   = 0x0402 // WM_USER + 2: us -> us, visibility changed
	trayUID         = 1

	trayMenuOpen = 1
	trayMenuExit = 2

	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmContextMenu   = 0x007B
)

var (
	trayWanted  atomic.Bool    // the state machine's wish: icon parked or not
	trayHWND    atomic.Uintptr // the message window, once created
	trayStart   sync.Once
	trayIcon    uintptr // HICON, built lazily on the tray thread
	taskbarMsg  uint32  // "TaskbarCreated" broadcast id (explorer restarts)
	trayIconOn  bool    // the icon is currently added (tray thread only)
	trayClassOK bool
)

// platformTraySetVisible records the wish and wakes the tray
// thread (starting it on first use). Safe from any goroutine.
func platformTraySetVisible(v bool) {
	trayWanted.Store(v)
	if !v && trayHWND.Load() == 0 {
		return // never started and nothing to remove
	}
	trayStart.Do(func() { go trayThread() })
	if h := trayHWND.Load(); h != 0 {
		procPostMessageW.Call(h, trayReconcile, 0, 0)
	}
	// If the thread is still starting, its startup reconciles
	// against trayWanted once the window exists.
}

type trayWndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type trayMsg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	PtX     int32
	PtY     int32
}

type trayPoint struct {
	X, Y int32
}

// The window procedure is created lazily inside trayThread, not
// as a package-level variable: as a var initializer its reference
// chain (menu -> restore hook -> state machine -> tray) forms an
// initialization cycle the compiler rejects.
var trayWndProcPtr uintptr

func makeTrayWndProc() uintptr {
	return syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
		switch uint32(msg) {
		case trayCallbackMsg:
			switch lParam {
			case wmLButtonUp, wmLButtonDblClk:
				go onTrayIconRestore()
			case wmRButtonUp, wmContextMenu:
				trayShowMenu(hwnd)
			}
			return 0
		case trayReconcile:
			trayReconcileIcon(hwnd)
			return 0
		}
		if taskbarMsg != 0 && uint32(msg) == taskbarMsg {
			// Explorer restarted and rebuilt the tray: re-park.
			trayIconOn = false
			trayReconcileIcon(hwnd)
			return 0
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return r
	})
}

func trayThread() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("MJTerminalTrayWnd")
	if trayWndProcPtr == 0 {
		trayWndProcPtr = makeTrayWndProc()
	}
	if !trayClassOK {
		wc := trayWndClassEx{
			Style:         0,
			LpfnWndProc:   trayWndProcPtr,
			HInstance:     hInst,
			LpszClassName: className,
		}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		if atom, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom != 0 {
			trayClassOK = true
		}
	}
	const hwndMessage = ^uintptr(2) // (HWND)-3
	hwnd, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(className)), 0, 0,
		0, 0, 0, 0, hwndMessage, 0, hInst, 0)
	if hwnd == 0 {
		return
	}
	if id, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(mustUTF16Ptr("TaskbarCreated")))); id != 0 {
		taskbarMsg = uint32(id)
	}
	trayHWND.Store(hwnd)
	trayReconcileIcon(hwnd)

	var m trayMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func mustUTF16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

type notifyIconDataW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UTimeout         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

// trayReconcileIcon makes the shell's icon state match trayWanted.
// Tray thread only.
func trayReconcileIcon(hwnd uintptr) {
	want := trayWanted.Load()
	if want == trayIconOn {
		return
	}
	const (
		nimAdd     = 0
		nimDelete  = 1
		nifMessage = 0x00000001
		nifIcon    = 0x00000002
		nifTip     = 0x00000004
	)
	nid := notifyIconDataW{HWnd: hwnd, UID: trayUID}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	if want {
		if trayIcon == 0 {
			trayIcon = trayBuildHICON()
		}
		nid.UFlags = nifMessage | nifIcon | nifTip
		nid.UCallbackMessage = trayCallbackMsg
		nid.HIcon = trayIcon
		tip, _ := syscall.UTF16FromString("铭杰终端机 · 桌面已隐藏到托盘")
		copy(nid.SzTip[:], tip)
		if r, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r != 0 {
			trayIconOn = true
		}
	} else {
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
		trayIconOn = false
	}
}

// trayShowMenu pops the two-item menu at the cursor. Tray thread
// only; TrackPopupMenu with TPM_RETURNCMD hands the choice back
// synchronously.
func trayShowMenu(hwnd uintptr) {
	const (
		mfString       = 0x00000000
		tpmRightButton = 0x0002
		tpmReturnCmd   = 0x0100
		tpmNonotify    = 0x0080
	)
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	openTxt := mustUTF16Ptr("打开桌面")
	exitTxt := mustUTF16Ptr("退出系统")
	procAppendMenuW.Call(menu, mfString, trayMenuOpen, uintptr(unsafe.Pointer(openTxt)))
	procAppendMenuW.Call(menu, mfString, trayMenuExit, uintptr(unsafe.Pointer(exitTxt)))
	var pt trayPoint
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// TrackPopupMenu requires the owner to be foreground first.
	procSetForegroundWindow.Call(hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd|tpmNonotify,
		uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	switch cmd {
	case trayMenuOpen:
		go onTrayIconRestore()
	case trayMenuExit:
		go onTrayIconExit()
	}
}

// ---------- icon artwork (generated, no asset files) ----------

// trayBuildHICON draws the MJ terminal mark (a light ">_" prompt
// on a dark rounded square) at several sizes, packs the PNGs into
// an ICO container and creates an HICON from the bytes in memory.
func trayBuildHICON() uintptr {
	sizes := []int{16, 20, 24, 32, 48}
	var pngs [][]byte
	for _, s := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, trayDrawIcon(s)); err != nil {
			return 0
		}
		pngs = append(pngs, buf.Bytes())
	}
	var ico bytes.Buffer
	_ = binary.Write(&ico, binary.LittleEndian, uint16(0))
	_ = binary.Write(&ico, binary.LittleEndian, uint16(1)) // type: icon
	_ = binary.Write(&ico, binary.LittleEndian, uint16(len(sizes)))
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		ico.WriteByte(byte(s))                                  // width (all sizes < 256)
		ico.WriteByte(byte(s))                                  // height
		ico.WriteByte(0)                                        // palette colors
		ico.WriteByte(0)                                        // reserved
		_ = binary.Write(&ico, binary.LittleEndian, uint16(1))  // planes
		_ = binary.Write(&ico, binary.LittleEndian, uint16(32)) // bits
		_ = binary.Write(&ico, binary.LittleEndian, uint32(len(pngs[i])))
		_ = binary.Write(&ico, binary.LittleEndian, uint32(offset))
		offset += len(pngs[i])
	}
	for _, p := range pngs {
		ico.Write(p)
	}
	b := ico.Bytes()
	h, _, _ := procCreateIconFromResource.Call(
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)),
		1 /* fIcon */, 0x00030000 /* version */, 0, 0, 0)
	return h
}

// trayDrawIcon renders one size of the mark: vertical-gradient
// dark rounded square, then a bold ">_" prompt glyph in a light
// blue-white. Pixel work only, so any size stays crisp enough for
// the tray.
func trayDrawIcon(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	radius := s * 0.24
	top := color.RGBA{44, 52, 66, 255}
	bottom := color.RGBA{16, 19, 26, 255}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if !insideRounded(x, y, s, radius) {
				continue
			}
			t := float64(y) / s
			img.Set(x, y, color.RGBA{
				R: lerp8(top.R, bottom.R, t),
				G: lerp8(top.G, bottom.G, t),
				B: lerp8(top.B, bottom.B, t),
				A: 255,
			})
		}
	}
	glyph := color.RGBA{148, 226, 255, 255}
	thick := s * 0.085
	// ">" : two strokes meeting at the middle right.
	seg(img, 0.33*s, 0.28*s, 0.58*s, 0.50*s, thick, glyph)
	seg(img, 0.58*s, 0.50*s, 0.33*s, 0.72*s, thick, glyph)
	// "_" : the prompt's underscore.
	seg(img, 0.62*s, 0.70*s, 0.80*s, 0.70*s, thick, glyph)
	return img
}

func insideRounded(x, y int, s, r float64) bool {
	// Standard rounded-box test: clamp the point into the inner
	// rectangle; the point is inside iff it is within r of the
	// clamped point (in the central cross the clamped point IS
	// the point, so this also covers the straight edges).
	fx, fy := float64(x)+0.5, float64(y)+0.5
	cx := math.Max(r, math.Min(s-r, fx))
	cy := math.Max(r, math.Min(s-r, fy))
	dx, dy := fx-cx, fy-cy
	return dx*dx+dy*dy <= r*r
}

// seg draws a thick line segment (round caps) of the given color.
func seg(img *image.RGBA, x0, y0, x1, y1, thick float64, c color.Color) {
	b := img.Bounds()
	minX := int(math.Max(0, math.Min(x0, x1)-thick))
	maxX := int(math.Min(float64(b.Dx()-1), math.Max(x0, x1)+thick))
	minY := int(math.Max(0, math.Min(y0, y1)-thick))
	maxY := int(math.Min(float64(b.Dy()-1), math.Max(y0, y1)+thick))
	dx, dy := x1-x0, y1-y0
	len2 := dx*dx + dy*dy
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			t := 0.0
			if len2 > 0 {
				t = ((px-x0)*dx + (py-y0)*dy) / len2
				t = math.Max(0, math.Min(1, t))
			}
			qx, qy := x0+t*dx, y0+t*dy
			if math.Hypot(px-qx, py-qy) <= thick/2 {
				img.Set(x, y, c)
			}
		}
	}
}

func lerp8(a, b uint8, t float64) uint8 {
	return uint8(float64(a) + (float64(b)-float64(a))*t)
}
