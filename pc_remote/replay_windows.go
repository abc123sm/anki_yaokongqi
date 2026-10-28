//go:build windows

package main

import (
	"errors"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	gdi32                        = syscall.NewLazyDLL("gdi32.dll")
	iphlpapi                     = syscall.NewLazyDLL("iphlpapi.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procSendInput                = user32.NewProc("SendInput")
	procGetExtendedTcpTable      = iphlpapi.NewProc("GetExtendedTcpTable")
)

const (
	INPUT_KEYBOARD               = 1
	KEYEVENTF_KEYUP              = 0x0002
	VK_R                         = 0x52
	ANKI_INJECT_MAGIC            = 0x414E4B49
	AF_INET                      = 2
	TCP_TABLE_OWNER_PID_LISTENER = 3
)

type keyboardInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

type inputUnion struct {
	inputType uint32
	ki        keyboardInput
	padding   uint64
}

type mibTcpRowOwnerPid struct {
	dwState      uint32
	dwLocalAddr  uint32
	dwLocalPort  uint32
	dwRemoteAddr uint32
	dwRemotePort uint32
	dwOwningPid  uint32
}

func getWindowText(hwnd uintptr) string {
	buf := make([]uint16, 256)
	ret, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if ret == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:ret])
}

func getClassName(hwnd uintptr) string {
	buf := make([]uint16, 256)
	ret, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if ret == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:ret])
}

// findListeningPID 通过 Windows TCP 监听表反查指定端口 (如 8765 / 18765 / 28765) 所属的 Anki 进程 PID
func findListeningPID(port int) uint32 {
	if port <= 0 {
		return 0
	}
	var size uint32
	procGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, AF_INET, TCP_TABLE_OWNER_PID_LISTENER, 0)
	if size == 0 || size > 4*1024*1024 {
		return 0
	}
	buf := make([]byte, size)
	ret, _, _ := procGetExtendedTcpTable.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
		AF_INET,
		TCP_TABLE_OWNER_PID_LISTENER,
		0,
	)
	if uint32(ret) != 0 || len(buf) < 4 {
		return 0
	}
	numEntries := *(*uint32)(unsafe.Pointer(&buf[0]))
	rowSize := unsafe.Sizeof(mibTcpRowOwnerPid{})
	for i := uint32(0); i < numEntries; i++ {
		offset := uintptr(4) + uintptr(i)*rowSize
		if offset+rowSize > uintptr(len(buf)) {
			break
		}
		row := (*mibTcpRowOwnerPid)(unsafe.Pointer(&buf[offset]))
		localPort := int(((row.dwLocalPort & 0xFF) << 8) | ((row.dwLocalPort >> 8) & 0xFF))
		if localPort == port && row.dwOwningPid != 0 {
			return row.dwOwningPid
		}
	}
	return 0
}

// 包级全局单次注册 EnumWindows 回调，杜绝重复分配 callback 槽位与栈变量捕获风险
var (
	enumMu           sync.Mutex
	enumTargetPID    uint32
	enumExactHwnd    uintptr
	enumFallbackHwnd uintptr
	enumWindowsCB    = syscall.NewCallback(enumWindowsProc)
)

func enumWindowsProc(hwnd, lParam uintptr) uintptr {
	vis, _, _ := procIsWindowVisible.Call(hwnd)
	if vis == 0 {
		return 1
	}
	cls := getClassName(hwnd)
	if !strings.HasPrefix(cls, "Qt") {
		return 1
	}
	title := getWindowText(hwnd)
	if !strings.HasSuffix(title, " - Anki") && title != "Anki" {
		return 1
	}

	if enumFallbackHwnd == 0 {
		enumFallbackHwnd = hwnd
	}
	if enumTargetPID != 0 {
		var winPID uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&winPID)))
		if winPID == enumTargetPID {
			enumExactHwnd = hwnd
			return 0
		}
	}
	return 1
}

// findAnkiMainWindowByPort 根据当前模式的端口精准查找对应的 Anki 主窗口句柄
func findAnkiMainWindowByPort(ankiPort int) uintptr {
	targetPID := findListeningPID(ankiPort)

	enumMu.Lock()
	defer enumMu.Unlock()

	enumTargetPID = targetPID
	enumExactHwnd = 0
	enumFallbackHwnd = 0

	procEnumWindows.Call(enumWindowsCB, 0)
	if enumExactHwnd != 0 {
		return enumExactHwnd
	}
	return enumFallbackHwnd
}

// ReplayAudioInAnkiByPort 触发指定端口对应的电脑端 Anki 原生音频重播（发送 R 键）
func ReplayAudioInAnkiByPort(ankiPort int) error {
	hwnd := findAnkiMainWindowByPort(ankiPort)
	if hwnd == 0 {
		return errors.New("未找到打开的 Anki 窗口")
	}

	fg, _, _ := procGetForegroundWindow.Call()
	if fg != hwnd {
		procSetForegroundWindow.Call(hwnd)
		time.Sleep(30 * time.Millisecond)
	}

	inputs := [2]inputUnion{
		{
			inputType: INPUT_KEYBOARD,
			ki: keyboardInput{
				wVk:         VK_R,
				dwFlags:     0,
				dwExtraInfo: ANKI_INJECT_MAGIC,
			},
		},
		{
			inputType: INPUT_KEYBOARD,
			ki: keyboardInput{
				wVk:         VK_R,
				dwFlags:     KEYEVENTF_KEYUP,
				dwExtraInfo: ANKI_INJECT_MAGIC,
			},
		},
	}

	ret, _, _ := procSendInput.Call(
		2,
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
	if uint32(ret) == 0 {
		return errors.New("发送 R 键重播指令失败")
	}
	return nil
}
