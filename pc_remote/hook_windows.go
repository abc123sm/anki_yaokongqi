//go:build windows

package main

import (
	"sync"
	"syscall"
	"unsafe"
)

var (
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
)

const (
	WH_MOUSE_LL = 14
	SM_CXSCREEN = 0
	SM_CYSCREEN = 1

	WS_EX_TOPMOST    = 0x00000008
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_NOACTIVATE = 0x08000000
	WS_EX_LAYERED    = 0x00080000
	LWA_ALPHA        = 0x00000002
)

type point struct {
	X int32
	Y int32
}

type msllHookStruct struct {
	Pt          point
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

// HookEngine 仅负责在 WH_MOUSE_LL 中静默吞掉带 Windows Touch 签名 (0xFF515700) 的合成鼠标点击，
// 防止 S2 遥控器在桌面上乱点；真正的方向解析完全由 RawInput HID Touch (0x0D/0x04) 原生 5 字节包负责。
type HookEngine struct {
	mu        sync.Mutex
	enabled   bool
	mouseHook uintptr
}

var (
	globalHookEngine *HookEngine
	lowLevelMouseCB  = syscall.NewCallback(lowLevelMouseProc)
)

func NewHookEngine() *HookEngine {
	eng := &HookEngine{
		enabled: true,
	}
	globalHookEngine = eng
	return eng
}

// lowLevelMouseProc 严格使用全 uintptr 参数签名
func lowLevelMouseProc(nCode, wParam, lParam uintptr) uintptr {
	if int32(nCode) >= 0 && globalHookEngine != nil && lParam != 0 {
		info := (*msllHookStruct)(unsafe.Pointer(lParam))
		if IsTouchSignature(info.DwExtraInfo) {
			return 1 // 吞掉系统由触控合成的假鼠标事件，绝不干扰真实物理鼠标
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
	return ret
}

func (e *HookEngine) Install() error {
	hMod, _, _ := procGetModuleHandleW.Call(0)
	hook, _, err := procSetWindowsHookExW.Call(WH_MOUSE_LL, lowLevelMouseCB, hMod, 0)
	if hook == 0 {
		return err
	}
	e.mu.Lock()
	e.mouseHook = hook
	e.mu.Unlock()
	return nil
}

func (e *HookEngine) Uninstall() {
	e.mu.Lock()
	hook := e.mouseHook
	e.mouseHook = 0
	e.mu.Unlock()
	if hook != 0 {
		procUnhookWindowsHookEx.Call(hook)
	}
}
