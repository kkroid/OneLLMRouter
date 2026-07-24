package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmTrayMsg    = 0x0400 + 1
	wmUpdateTray = 0x0400 + 2 // posted by health poller to refresh icon/menu
	cmdRestart   = 1002
	cmdQuit      = 1003
)

var (
	trayNid          *nidStruct
	blueIcon         uintptr
	yellowIcon       uintptr
	greenIcon        uintptr
	redIcon          uintptr
	warnTimer        *time.Timer
	warnMu           sync.Mutex
	wmTaskbarCreated uintptr // registered "TaskbarCreated" message
)

// healthData is the latest snapshot from the /health endpoint.
type healthData struct {
	healthy      bool
	modelCount   int
	copilotToken bool
	errorCount   int64
	statusText   string
}

// FlashWarning turns the tray icon yellow for 3 seconds.
// Consecutive calls reset the timer (icon stays yellow).
// Safe to call when no tray exists (no-op).
func FlashWarning() {
	if trayNid == nil {
		return
	}
	warnMu.Lock()
	defer warnMu.Unlock()

	// Swap to yellow
	setTrayIconRaw(yellowIcon)

	// System notification beep (MB_ICONASTERISK)
	if bellEnabled {
		user32 := windows.NewLazySystemDLL("user32.dll")
		user32.NewProc("MessageBeep").Call(0x00000040)
	}

	// Reset debounce timer
	if warnTimer != nil {
		warnTimer.Stop()
	}
	warnTimer = time.AfterFunc(3*time.Second, func() {
		warnMu.Lock()
		defer warnMu.Unlock()
		if trayNid == nil {
			return
		}
		// Restore correct status icon
		current := GetTrayStatus()
		var icon uintptr
		switch current {
		case StatusHealthy:
			icon = greenIcon
		case StatusDegraded:
			icon = yellowIcon
		case StatusError:
			icon = redIcon
		default:
			icon = blueIcon
		}
		setTrayIconRaw(icon)
	})
}

// setTrayIconRaw sets the tray icon immediately (low-level, no status tracking).
func setTrayIconRaw(icon uintptr) {
	sh32 := windows.NewLazySystemDLL("shell32.dll")
	shellNI := sh32.NewProc("Shell_NotifyIconW")
	trayNid.hIcon = icon
	trayNid.uFlags = 0x00000001                       // NIF_ICON
	shellNI.Call(1, uintptr(unsafe.Pointer(trayNid))) // NIM_MODIFY
}

type Tray struct {
	hwnd     uintptr
	quit     chan struct{}
	done     func()
	port     int
	version  string
	healthMu sync.Mutex
	health   healthData
}

func NewTray(port int, version string, quit chan struct{}, done func()) *Tray {
	return &Tray{port: port, version: version, quit: quit, done: done}
}

func (t *Tray) Run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	u32 := windows.NewLazySystemDLL("user32.dll")
	k32 := windows.NewLazySystemDLL("kernel32.dll")
	sh32 := windows.NewLazySystemDLL("shell32.dll")

	hInst, _, _ := k32.NewProc("GetModuleHandleW").Call(0)

	// Register for the TaskbarCreated message — when explorer.exe restarts,
	// Windows broadcasts this message so we can re-add our tray icon.
	tcName, _ := windows.UTF16PtrFromString("TaskbarCreated")
	wmTaskbarCreated, _, _ = u32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(tcName)))

	className, _ := windows.UTF16PtrFromString("OneLLMRouterTray")
	var wc wndClassEx
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = windows.NewCallback(t.wndProc)
	wc.hInstance = hInst
	wc.lpszClassName = className
	u32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc)))

	cs := uintptr(unsafe.Pointer(className))
	t.hwnd, _, _ = u32.NewProc("CreateWindowExW").Call(0, cs, cs, 0, 0, 0, 0, 0, 0, 0, hInst, 0)

	// loadIcon creates an HICON from raw BMP resource data (no ICO header).
	loadIcon := func(data []byte) uintptr {
		h, _, _ := u32.NewProc("CreateIconFromResourceEx").Call(
			uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)),
			1, 0x00030000, 16, 16, 0x00000001,
		)
		return h
	}

	blueIcon = loadIcon(blueIconBytes())
	yellowIcon = loadIcon(yellowIconBytes)
	greenIcon = loadIcon(greenIconBytes)
	redIcon = loadIcon(redIconBytes)

	var nid nidStruct
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = t.hwnd
	nid.uID = 1
	nid.uFlags = 0x00000001 | 0x00000002 | 0x00000004
	nid.uCallbackMessage = wmTrayMsg
	nid.hIcon = greenIcon // start green — poller will correct if needed
	copyUTF16(nid.szTip[:], "OneLLMRouter")

	shellNI := sh32.NewProc("Shell_NotifyIconW")
	shellNI.Call(0, uintptr(unsafe.Pointer(&nid))) // NIM_ADD
	trayNid = &nid

	// Startup balloon only — one notification on launch
	nid.uFlags = 0x10
	nid.dwInfoFlags = 0x00000001
	nid.uTimeoutOrVer = 3000
	copyUTF16(nid.szInfo[:], "OneLLMRouter 已启动")
	copyUTF16(nid.szInfoTitle[:], fmt.Sprintf("localhost:%d", t.port))
	shellNI.Call(1, uintptr(unsafe.Pointer(&nid))) // NIM_MODIFY

	// Start health polling goroutine
	go t.healthPoller()

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
	case wmUpdateTray:
		status := GetTrayStatus()
		t.setTrayIconFromStatus(status)
		// Update tooltip
		t.healthMu.Lock()
		tip := fmt.Sprintf("OneLLMRouter — %s", t.health.statusText)
		t.healthMu.Unlock()
		if trayNid != nil {
			copyUTF16(trayNid.szTip[:], tip)
			trayNid.uFlags = 0x00000004 // NIF_TIP
			sh32 := windows.NewLazySystemDLL("shell32.dll")
			sh32.NewProc("Shell_NotifyIconW").Call(1, uintptr(unsafe.Pointer(trayNid)))
		}
	default:
		if uintptr(msg) == wmTaskbarCreated && wmTaskbarCreated != 0 && trayNid != nil {
			// Explorer.exe restarted — re-add the tray icon
			sh32 := windows.NewLazySystemDLL("shell32.dll")
			sh32.NewProc("Shell_NotifyIconW").Call(0, uintptr(unsafe.Pointer(trayNid))) // NIM_ADD
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

	t.healthMu.Lock()
	h := t.health
	t.healthMu.Unlock()

	// Status header
	statusLabel := fmt.Sprintf("Status: %s", h.statusText)
	sStatus, _ := windows.UTF16PtrFromString(statusLabel)
	appendM.Call(menu, 0x001, 0, uintptr(unsafe.Pointer(sStatus))) // MF_GRAYED

	// Model count
	modelsLabel := fmt.Sprintf("Models: %d", h.modelCount)
	sModels, _ := windows.UTF16PtrFromString(modelsLabel)
	appendM.Call(menu, 0x001, 0, uintptr(unsafe.Pointer(sModels)))

	// Port + version
	versionLabel := t.version
	if versionLabel != "" && versionLabel != "dev" && versionLabel[0] != 'v' {
		versionLabel = "v" + versionLabel
	}
	if versionLabel == "" {
		versionLabel = "dev"
	}
	infoLabel := fmt.Sprintf("localhost:%d  |  %s", t.port, versionLabel)
	sInfo, _ := windows.UTF16PtrFromString(infoLabel)
	appendM.Call(menu, 0x001, 0, uintptr(unsafe.Pointer(sInfo)))

	// Error count if degraded
	if h.errorCount > 0 {
		errLabel := fmt.Sprintf("Recent errors: %d", h.errorCount)
		sErr, _ := windows.UTF16PtrFromString(errLabel)
		appendM.Call(menu, 0x001, 0, uintptr(unsafe.Pointer(sErr)))
	}

	appendM.Call(menu, 0x800, 0, 0) // separator

	s1, _ := windows.UTF16PtrFromString("Restart")
	s2, _ := windows.UTF16PtrFromString("Quit")
	appendM.Call(menu, 0, cmdRestart, uintptr(unsafe.Pointer(s1)))
	appendM.Call(menu, 0, cmdQuit, uintptr(unsafe.Pointer(s2)))

	var pt struct{ x, y int32 }
	u32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&pt)))
	// SetForegroundWindow is needed so the menu dismisses on outside click.
	u32.NewProc("SetForegroundWindow").Call(t.hwnd)
	ret, _, _ := u32.NewProc("TrackPopupMenu").Call(menu, 0x0100, uintptr(pt.x), uintptr(pt.y), 0, t.hwnd, 0)
	u32.NewProc("PostMessageW").Call(t.hwnd, 0, 0, 0)
	u32.NewProc("DestroyMenu").Call(menu)

	switch uint32(ret) {
	case cmdRestart:
		t.doRestart()
	case cmdQuit:
		t.doQuit()
	}
}

func (t *Tray) doRestart() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
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

// setTrayIconFromStatus sets the tray icon based on TrayStatus.
func (t *Tray) setTrayIconFromStatus(status TrayStatus) {
	if trayNid == nil {
		return
	}
	sh32 := windows.NewLazySystemDLL("shell32.dll")
	shellNI := sh32.NewProc("Shell_NotifyIconW")

	var icon uintptr
	switch status {
	case StatusHealthy:
		icon = greenIcon
	case StatusDegraded:
		icon = yellowIcon
	case StatusError:
		icon = redIcon
	default:
		icon = blueIcon
	}
	trayNid.hIcon = icon
	trayNid.uFlags = 0x00000001                       // NIF_ICON
	shellNI.Call(1, uintptr(unsafe.Pointer(trayNid))) // NIM_MODIFY
}

// healthPoller periodically checks the health endpoint and updates tray status.
func (t *Tray) healthPoller() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Initial poll after a short delay
	time.Sleep(500 * time.Millisecond)
	t.pollHealth()

	// When t.quit is nil (default), the goroutine runs until the process exits.
	// A nil channel in select is never ready — safe and intentional.
	for {
		select {
		case <-ticker.C:
			t.pollHealth()
		case <-t.quit:
			return
		}
	}
}

func (t *Tray) pollHealth() {
	client := &http.Client{Timeout: 2 * time.Second}
	t.pollHealthWithClient(client)

	u32 := windows.NewLazySystemDLL("user32.dll")
	u32.NewProc("PostMessageW").Call(t.hwnd, wmUpdateTray, 0, 0)
}

func (t *Tray) pollHealthWithClient(client *http.Client) {
	url := fmt.Sprintf("http://localhost:%d/health", t.port)
	resp, err := client.Get(url)
	errorCount := GetAndResetErrors()
	if resp != nil {
		defer resp.Body.Close()
	}

	t.healthMu.Lock()
	defer t.healthMu.Unlock()

	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		t.health.healthy = false
		t.health.errorCount = 0
		t.health.statusText = "down"
		if err != nil {
			t.health.statusText = "down — " + err.Error()
		}
		SetTrayStatus(StatusError)
		return
	}

	var healthResponse struct {
		Models       int  `json:"models"`
		CopilotToken bool `json:"copilot_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&healthResponse); err != nil {
		t.health.healthy = false
		t.health.errorCount = 0
		t.health.statusText = "down"
		SetTrayStatus(StatusError)
		return
	}

	t.health.healthy = true
	t.health.modelCount = healthResponse.Models
	t.health.copilotToken = healthResponse.CopilotToken
	if errorCount > 0 {
		t.health.errorCount = errorCount
		t.health.statusText = "degraded"
		SetTrayStatus(StatusDegraded)
		return
	}
	t.health.errorCount = 0
	t.health.statusText = "healthy"
	SetTrayStatus(StatusHealthy)
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
