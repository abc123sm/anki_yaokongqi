package main

import (
	"testing"
)

func TestIsTouchSignature(t *testing.T) {
	if !IsTouchSignature(0xFF515700) {
		t.Errorf("0xFF515700 应当被识别为 Touch 签名")
	}
	if !IsTouchSignature(0xFF515783) {
		t.Errorf("0xFF515783 应当被识别为 Touch 签名")
	}
	if IsTouchSignature(0) {
		t.Errorf("普通鼠标 dwExtraInfo=0 不应被识别为 Touch 签名")
	}
}

func TestIsValidRawInputReturn(t *testing.T) {
	minBytes := uint32(48)
	// Win32 GetRawInputData 失败时返回 32 位 (UINT)-1，在 64 位 uintptr 中为 0x00000000FFFFFFFF
	win32ErrRet := uintptr(0x00000000FFFFFFFF)
	if IsValidRawInputReturn(win32ErrRet, 48, minBytes) {
		t.Fatalf("32位错误返回值 0x00000000FFFFFFFF 必须被判定为无效")
	}
	if IsValidRawInputReturn(^uintptr(0), 48, minBytes) {
		t.Fatalf("全1错误返回值必须被判定为无效")
	}
	if IsValidRawInputReturn(0, 48, minBytes) {
		t.Fatalf("0字节返回值必须被判定为无效")
	}
	if IsValidRawInputReturn(24, 48, minBytes) {
		t.Fatalf("小于最小结构体长度的返回值必须被判定为无效")
	}
	if !IsValidRawInputReturn(48, 48, minBytes) {
		t.Fatalf("正常 48 字节返回值应当有效")
	}
}

