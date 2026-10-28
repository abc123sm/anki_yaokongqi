//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"
	"unsafe"
)

var procMessageBoxW = user32.NewProc("MessageBoxW")

func errorLogPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "pc_remote_error.log"
	}
	return filepath.Join(filepath.Dir(exe), "pc_remote_error.log")
}

func recoverAndLog(where string) {
	if r := recover(); r != nil {
		stack := string(debug.Stack())
		msg := fmt.Sprintf("[%s] %s 发生异常: %v\r\n%s\r\n", time.Now().Format("2006-01-02 15:04:05"), where, r, stack)
		_ = os.WriteFile(errorLogPath(), []byte(msg), 0644)
		titlePtr, _ := syscall.UTF16PtrFromString("Anki 遥控器异常提示")
		textPtr, _ := syscall.UTF16PtrFromString(fmt.Sprintf("程序捕获到异常 (%s): %v\n详细日志已写入 pc_remote_error.log", where, r))
		procMessageBoxW.Call(0, uintptr(unsafe.Pointer(textPtr)), uintptr(unsafe.Pointer(titlePtr)), 0x10)
	}
}

func main() {
	defer recoverAndLog("main")
	if err := RunApp(); err != nil {
		msg := fmt.Sprintf("[%s] 启动失败: %v\r\n", time.Now().Format("2006-01-02 15:04:05"), err)
		_ = os.WriteFile(errorLogPath(), []byte(msg), 0644)
	}
}
