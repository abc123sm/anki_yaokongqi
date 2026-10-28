//go:build windows

package main

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procSetWindowTextW   = user32.NewProc("SetWindowTextW")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procFindWindowW      = user32.NewProc("FindWindowW")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procCreateFontW      = gdi32.NewProc("CreateFontW")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetBkColor       = gdi32.NewProc("SetBkColor")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procInvalidateRect   = user32.NewProc("InvalidateRect")
	procRegisterRawInput = user32.NewProc("RegisterRawInputDevices")
	procGetRawInputData  = user32.NewProc("GetRawInputData")
	procCreateMutexW     = kernel32.NewProc("CreateMutexW")
)

const (
	WS_OVERLAPPED = 0x00000000
	WS_CAPTION    = 0x00C00000
	WS_SYSMENU    = 0x00080000
	WS_MINIMIZEBOX = 0x00020000
	WS_VISIBLE    = 0x10000000
	WS_CHILD      = 0x40000000
	BS_PUSHBUTTON = 0x00000000
	SS_LEFT       = 0x00000000
	SW_SHOW       = 5
	SW_RESTORE    = 9

	WM_DESTROY         = 0x0002
	WM_COMMAND         = 0x0111
	WM_SETFONT         = 0x0030
	WM_CTLCOLORSTATIC  = 0x0138
	WM_INPUT           = 0x00FF
	WM_APP_UPDATE_TEXT = 0x8001

	IDC_ARROW            = 32512
	OPAQUE               = 2
	ERROR_ALREADY_EXISTS = 183

	BTN_ID_MODE = 2001

	RIDEV_INPUTSINK = 0x00000100
	RID_INPUT       = 0x10000003
	RIM_TYPEHID     = 2
)

type wndClassExW struct {
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

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type rawInputDevice struct {
	usUsagePage uint16
	usUsage     uint16
	dwFlags     uint32
	hwndTarget  uintptr
}

type rawInputHeader struct {
	dwType  uint32
	dwSize  uint32
	hDevice uintptr
	wParam  uintptr
}

type rawHIDHeader struct {
	dwSizeHid uint32
	dwCount   uint32
}

type AppUI struct {
	mu            sync.Mutex
	cfg           AppConfig
	hook          *HookEngine
	ctrl          *AnkiController
	tracker       *TouchStrokeTracker
	throttler     *GestureThrottler
	mainHwnd      uintptr
	lblStatus     uintptr
	btnMode       uintptr
	bgBrush       uintptr
	pendingStatus string
}

var (
	globalUI      *AppUI
	mainWndProcCB = syscall.NewCallback(mainWndProc)
)

func setWindowText(hwnd uintptr, text string) {
	ptr, _ := syscall.UTF16PtrFromString(text)
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(ptr)))
}

func (ui *AppUI) UpdateStatusAsync(text string) {
	ui.mu.Lock()
	ui.pendingStatus = text
	hwnd := ui.mainHwnd
	ui.mu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, WM_APP_UPDATE_TEXT, 0, 0)
	}
}

func (ui *AppUI) switchPortMode() {
	ui.cfg.NextPortMode()
	ui.ctrl.SetEndpoints(ui.cfg.AnkiURL, ui.cfg.AIExtURL)
	setWindowText(ui.btnMode, ui.cfg.ModeLabel())
	ui.UpdateStatusAsync(fmt.Sprintf("已切换至 %s", ui.cfg.ModeLabel()))
}

func (ui *AppUI) handleRawInput(lParam uintptr) {
	var size uint32
	ret1, _, _ := procGetRawInputData.Call(
		lParam,
		RID_INPUT,
		0,
		uintptr(unsafe.Pointer(&size)),
		unsafe.Sizeof(rawInputHeader{}),
	)
	if uint32(ret1) == ^uint32(0) || size == 0 || size > 4096 {
		return
	}
	hdrSize := uint32(unsafe.Sizeof(rawInputHeader{}))
	hidHdrSize := uint32(unsafe.Sizeof(rawHIDHeader{}))
	minBytes := hdrSize + hidHdrSize + 5
	if size < minBytes {
		return
	}

	buf := make([]byte, size)
	ret2, _, _ := procGetRawInputData.Call(
		lParam,
		RID_INPUT,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		unsafe.Sizeof(rawInputHeader{}),
	)
	if !IsValidRawInputReturn(ret2, uint32(len(buf)), minBytes) {
		return
	}

	hdr := (*rawInputHeader)(unsafe.Pointer(&buf[0]))
	if hdr.dwType != RIM_TYPEHID {
		return
	}

	hidHdr := (*rawHIDHeader)(unsafe.Pointer(&buf[hdrSize]))
	if hidHdr.dwSizeHid < 5 || hidHdr.dwCount == 0 {
		return
	}

	dataOffset := hdrSize + hidHdrSize
	for i := uint32(0); i < hidHdr.dwCount; i++ {
		start := dataOffset + i*hidHdr.dwSizeHid
		end := start + hidHdr.dwSizeHid
		if end > uint32(len(buf)) {
			break
		}
		report := buf[start:end]
		key, dx, dy, done := ui.tracker.FeedReport(report)
		if !done {
			continue
		}
		if key == KeyNone {
			ui.UpdateStatusAsync(fmt.Sprintf("滑动过小 (dx:%d, dy:%d)", dx, dy))
			continue
		}
		if !ui.throttler.Allow(time.Now()) {
			continue
		}

		go func(k RemoteKey, dX, dY int32) {
			defer recoverAndLog("onKeyTriggered")
			if k == KeyUp && ui.ctrl.CurrentSide() == SideAnswer {
				ui.UpdateStatusAsync(fmt.Sprintf("%s (dx:%d, dy:%d) -> AI 生成中...", k.String(), dX, dY))
			}
			resMsg, err := ui.ctrl.HandleKey(k)
			if err != nil {
				ui.UpdateStatusAsync(fmt.Sprintf("%s (dx:%d, dy:%d) -> 失败: %v", k.String(), dX, dY, err))
				return
			}
			ui.UpdateStatusAsync(fmt.Sprintf("%s (dx:%d, dy:%d) -> %s", k.String(), dX, dY, resMsg))
		}(key, dx, dy)
	}
}

func mainWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if globalUI != nil {
		switch uint32(msg) {
		case WM_INPUT:
			globalUI.handleRawInput(lParam)
			return 0
		case WM_COMMAND:
			id := int(wParam & 0xFFFF)
			if id == BTN_ID_MODE {
				globalUI.switchPortMode()
				return 0
			}
		case WM_APP_UPDATE_TEXT:
			globalUI.mu.Lock()
			txt := globalUI.pendingStatus
			globalUI.mu.Unlock()
			setWindowText(globalUI.lblStatus, txt)
			procInvalidateRect.Call(globalUI.lblStatus, 0, 1)
			return 0
		case WM_CTLCOLORSTATIC:
			procSetBkMode.Call(wParam, OPAQUE)
			procSetBkColor.Call(wParam, 0x00222222)
			procSetTextColor.Call(wParam, 0x00E0E0E0)
			return globalUI.bgBrush
		case WM_DESTROY:
			globalUI.hook.Uninstall()
			procPostQuitMessage.Call(0)
			return 0
		}
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}

func createFont(size int32, bold bool) uintptr {
	weight := int32(400)
	if bold {
		weight = 700
	}
	fontName, _ := syscall.UTF16PtrFromString("Microsoft YaHei UI")
	hFont, _, _ := procCreateFontW.Call(
		uintptr(size), 0, 0, 0,
		uintptr(weight),
		0, 0, 0, 1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(fontName)),
	)
	return hFont
}

func ensureSingleInstance() bool {
	mutexName, _ := syscall.UTF16PtrFromString("Local\\AnkiRemotePCSingleInstanceMutex")
	hMutex, _, err := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(mutexName)))
	if hMutex != 0 && err == syscall.Errno(ERROR_ALREADY_EXISTS) {
		clsName, _ := syscall.UTF16PtrFromString("AnkiRemoteMainWnd")
		existingHwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(clsName)), 0)
		if existingHwnd != 0 {
			procShowWindow.Call(existingHwnd, SW_RESTORE)
			procSetForegroundWindow.Call(existingHwnd)
		}
		return false
	}
	return true
}

func RunApp() error {
	runtime.LockOSThread()

	if !ensureSingleInstance() {
		return nil
	}

	cfg := DefaultConfig()
	ui := &AppUI{
		cfg:       cfg,
		hook:      NewHookEngine(),
		tracker:   NewTouchStrokeTracker(),
		throttler: NewGestureThrottler(time.Duration(cfg.ThrottleMs) * time.Millisecond),
	}
	globalUI = ui

	ui.ctrl = NewAnkiController(cfg.AnkiURL, cfg.AIExtURL, func() error {
		return ReplayAudioInAnkiByPort(ui.cfg.AnkiPort())
	})

	hInst, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, IDC_ARROW)
	ui.bgBrush, _, _ = procCreateSolidBrush.Call(0x00222222)

	mainClass, _ := syscall.UTF16PtrFromString("AnkiRemoteMainWnd")
	wcMain := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc:   mainWndProcCB,
		hInstance:     hInst,
		hCursor:       cursor,
		hbrBackground: ui.bgBrush,
		lpszClassName: mainClass,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcMain)))

	screenW, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	winW := int32(280)
	winH := int32(125)
	winX := int32(screenW) - winW - 30
	winY := int32(40)
	if winX < 0 {
		winX = 20
	}

	mainTitle, _ := syscall.UTF16PtrFromString("Anki 遥控器")
	mainHwnd, _, _ := procCreateWindowExW.Call(
		WS_EX_TOPMOST,
		uintptr(unsafe.Pointer(mainClass)),
		uintptr(unsafe.Pointer(mainTitle)),
		WS_OVERLAPPED|WS_CAPTION|WS_SYSMENU|WS_MINIMIZEBOX|WS_VISIBLE,
		uintptr(winX), uintptr(winY), uintptr(winW), uintptr(winH),
		0, 0, hInst, 0,
	)
	ui.mainHwnd = mainHwnd

	fontNormal := createFont(-14, false)
	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	btnClass, _ := syscall.UTF16PtrFromString("BUTTON")

	statusPtr, _ := syscall.UTF16PtrFromString("等待遥控器按键...")
	ui.lblStatus, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(statusPtr)),
		WS_CHILD|WS_VISIBLE|SS_LEFT,
		12, 12, 240, 24,
		mainHwnd, 0, hInst, 0,
	)
	procSendMessageW.Call(ui.lblStatus, WM_SETFONT, fontNormal, 1)

	modePtr, _ := syscall.UTF16PtrFromString(ui.cfg.ModeLabel())
	ui.btnMode, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(btnClass)),
		uintptr(unsafe.Pointer(modePtr)),
		WS_CHILD|WS_VISIBLE|BS_PUSHBUTTON,
		12, 42, 240, 34,
		mainHwnd, BTN_ID_MODE, hInst, 0,
	)
	procSendMessageW.Call(ui.btnMode, WM_SETFONT, fontNormal, 1)

	// 注册 RawInput 直接监听 HID Touch Screen (UsagePage 0x000D, Usage 0x0004)
	rid := rawInputDevice{
		usUsagePage: 0x000D,
		usUsage:     0x0004,
		dwFlags:     RIDEV_INPUTSINK,
		hwndTarget:  mainHwnd,
	}
	procRegisterRawInput.Call(uintptr(unsafe.Pointer(&rid)), 1, unsafe.Sizeof(rid))

	// 安装底层 WH_MOUSE_LL 钩子静默屏蔽触控合成的鼠标点击
	_ = ui.hook.Install()

	procShowWindow.Call(mainHwnd, SW_SHOW)
	procUpdateWindow.Call(mainHwnd)

	runtime.KeepAlive(mainClass)
	runtime.KeepAlive(staticClass)
	runtime.KeepAlive(btnClass)

	var m winMsg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}
