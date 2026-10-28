package main

// IsTouchSignature 判断 MSLLHOOKSTRUCT.dwExtraInfo 是否带有 Windows Touch / Pen 注入签名 (0xFF515700)
func IsTouchSignature(dwExtraInfo uintptr) bool {
	return (dwExtraInfo & 0xFFFFFF00) == 0xFF515700
}

// IsValidRawInputReturn 校验 Win32 GetRawInputData 的 32 位 UINT 返回值，过滤 (UINT)-1 (0xFFFFFFFF) 及长度不足的情况
func IsValidRawInputReturn(ret uintptr, bufLen uint32, minBytes uint32) bool {
	r32 := uint32(ret)
	if r32 == ^uint32(0) || r32 == 0 {
		return false
	}
	if r32 > bufLen || r32 < minBytes {
		return false
	}
	return true
}
