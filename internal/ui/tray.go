package ui

import (
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmTrayMsg  = 0x0400 + 1
	cmdRestart = 1002
	cmdQuit    = 1003
)

type Tray struct {
	hwnd uintptr
	quit chan struct{}
	done func()
	port int
}

func NewTray(port int, quit chan struct{}, done func()) *Tray {
	return &Tray{port: port, quit: quit, done: done}
}

func (t *Tray) Run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	icoBytes := trayIconBytes()
	u32 := windows.NewLazySystemDLL("user32.dll")
	k32 := windows.NewLazySystemDLL("kernel32.dll")
	sh32 := windows.NewLazySystemDLL("shell32.dll")

	hInst, _, _ := k32.NewProc("GetModuleHandleW").Call(0)

	className, _ := windows.UTF16PtrFromString("OneLLMRouterTray")
	var wc wndClassEx
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = windows.NewCallback(t.wndProc)
	wc.hInstance = hInst
	wc.lpszClassName = className
	u32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc)))

	cs := uintptr(unsafe.Pointer(className))
	t.hwnd, _, _ = u32.NewProc("CreateWindowExW").Call(0, cs, cs, 0, 0, 0, 0, 0, 0, 0, hInst, 0)

	hicon, _, _ := u32.NewProc("CreateIconFromResourceEx").Call(
		uintptr(unsafe.Pointer(&icoBytes[22])), uintptr(len(icoBytes)-22),
		1, 0x00030000, 16, 16, 0x00000001,
	)

	var nid nidStruct
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = t.hwnd
	nid.uID = 1
	nid.uFlags = 0x00000001 | 0x00000002 | 0x00000004
	nid.uCallbackMessage = wmTrayMsg
	nid.hIcon = hicon
	copyUTF16(nid.szTip[:], "OneLLMRouter")

	shellNI := sh32.NewProc("Shell_NotifyIconW")
	shellNI.Call(0, uintptr(unsafe.Pointer(&nid))) // NIM_ADD

	// Startup balloon only — one notification on launch
	nid.uFlags = 0x10
	nid.dwInfoFlags = 0x00000001
	nid.uTimeoutOrVer = 3000
	copyUTF16(nid.szInfo[:], "OneLLMRouter 已启动")
	copyUTF16(nid.szInfoTitle[:], fmt.Sprintf("localhost:%d", t.port))
	shellNI.Call(1, uintptr(unsafe.Pointer(&nid))) // NIM_MODIFY

	var msg msgStruct
	getMsg := u32.NewProc("GetMessageW")
	transMsg := u32.NewProc("TranslateMessage")
	dispMsg := u32.NewProc("DispatchMessageW")

	for {
		ret, _, _ := getMsg.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 {
			break
		}
		select {
		case <-t.quit:
			shellNI.Call(2, uintptr(unsafe.Pointer(&nid)))
			u32.NewProc("DestroyWindow").Call(t.hwnd)
			return
		default:
		}
		transMsg.Call(uintptr(unsafe.Pointer(&msg)))
		dispMsg.Call(uintptr(unsafe.Pointer(&msg)))
	}
	shellNI.Call(2, uintptr(unsafe.Pointer(&nid)))
	u32.NewProc("DestroyWindow").Call(t.hwnd)
}

func copyUTF16(dst []uint16, src string) {
	s, _ := windows.UTF16FromString(src)
	copy(dst, s)
}

func (t *Tray) wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmTrayMsg:
		if lParam == 0x0205 { // WM_RBUTTONUP
			t.showMenu()
		}
	}
	u32 := windows.NewLazySystemDLL("user32.dll")
	r, _, _ := u32.NewProc("DefWindowProcW").Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func (t *Tray) showMenu() {
	u32 := windows.NewLazySystemDLL("user32.dll")
	menu, _, _ := u32.NewProc("CreatePopupMenu").Call()
	appendM := u32.NewProc("AppendMenuW")

	s0, _ := windows.UTF16PtrFromString(fmt.Sprintf("localhost:%d", t.port))
	s1, _ := windows.UTF16PtrFromString("重启")
	s2, _ := windows.UTF16PtrFromString("退出")

	appendM.Call(menu, 0x001, 0, uintptr(unsafe.Pointer(s0))) // grayed text, no action
	appendM.Call(menu, 0x800, 0, 0)                            // separator
	appendM.Call(menu, 0, cmdRestart, uintptr(unsafe.Pointer(s1)))
	appendM.Call(menu, 0, cmdQuit, uintptr(unsafe.Pointer(s2)))

	var pt struct{ x, y int32 }
	u32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&pt)))
	ret, _, _ := u32.NewProc("TrackPopupMenu").Call(menu, 0x0100, uintptr(pt.x), uintptr(pt.y), 0, t.hwnd, 0)
	u32.NewProc("DestroyMenu").Call(menu)

	switch uint32(ret) {
	case cmdRestart:
		t.doRestart()
	case cmdQuit:
		t.doQuit()
	}
}

func (t *Tray) doRestart() {
	exePath, _ := os.Executable()
	exeW, _ := windows.UTF16PtrFromString(exePath)
	argsW, _ := windows.UTF16PtrFromString("--daemon")
	sh32 := windows.NewLazySystemDLL("shell32.dll")
	sh32.NewProc("ShellExecuteW").Call(0, 0, uintptr(unsafe.Pointer(exeW)), uintptr(unsafe.Pointer(argsW)), 0, 0)
	t.doQuit()
}

func (t *Tray) doQuit() {
	t.done()
	u32 := windows.NewLazySystemDLL("user32.dll")
	u32.NewProc("PostQuitMessage").Call(0)
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type msgStruct struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
}

type nidStruct struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uTimeoutOrVer    uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}
