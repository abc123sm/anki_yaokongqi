package main

import (
	"sync"
	"time"
)

// RemoteKey 表示 S2 抖音蓝牙遥控器上的物理按键
type RemoteKey int

const (
	KeyNone  RemoteKey = iota
	KeyUp              // 物理上键（底层为向下滑动 dy > 30）
	KeyDown            // 物理下键（底层为向上滑动 dy < -30）
	KeyLeft            // 物理左键（底层为向右滑动 dx > 30）
	KeyRight           // 物理右键（底层为向左滑动 dx < -30）
	KeyOK              // 物理OK键（底层为原地单击 |dx| < 10 && |dy| < 10）
)

func (k RemoteKey) String() string {
	switch k {
	case KeyUp:
		return "上键"
	case KeyDown:
		return "下键"
	case KeyLeft:
		return "左键"
	case KeyRight:
		return "右键"
	case KeyOK:
		return "OK键"
	default:
		return "无动作"
	}
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// ClassifySwipe 根据触控按下与抬起的坐标偏移，严格按照 index.html 的算法与阈值还原为 S2 遥控器物理按键
func ClassifySwipe(startX, startY, endX, endY int32) RemoteKey {
	dx := endX - startX
	dy := endY - startY
	absDx := abs32(dx)
	absDy := abs32(dy)

	// 原地点击：|dx| < 10 && |dy| < 10 -> OK键
	if absDx < 10 && absDy < 10 {
		return KeyOK
	}

	// 滑动阈值：必须至少有一轴位移超过 30
	if absDx <= 30 && absDy <= 30 {
		return KeyNone
	}

	if absDx > absDy {
		// 水平滑动（注意 S2 遥控器的物理方向与屏幕滑动方向相反）
		if dx > 0 {
			// 向右滑动 -> 物理左方向键
			return KeyLeft
		}
		// 向左滑动 -> 物理右方向键
		return KeyRight
	}

	// 垂直滑动
	if dy > 0 {
		// 向下滑动 -> 物理上方向键
		return KeyUp
	}
	// 向上滑动 -> 物理下方向键
	return KeyDown
}

// ParseS2TouchReport 解析 S2 遥控器 Col01 (UsagePage 0x0D, Usage 0x04) 的 5 字节原生 HID 触控报告
// 报告结构:
//   Byte 0: ReportID (1)
//   Byte 1: Bit0=TipSwitch, Bit1=InRange, Bit2=Confidence, Bits3..7=ContactID
//   Bytes 2..4: 12-bit X (0..1000), 12-bit Y (0..1000)
func ParseS2TouchReport(raw []byte) (tipSwitch bool, x int32, y int32, ok bool) {
	if len(raw) < 5 {
		return false, 0, 0, false
	}
	if raw[0] != 1 {
		return false, 0, 0, false
	}
	tipSwitch = (raw[1] & 0x01) != 0
	ux := uint16(raw[2]) | (uint16(raw[3]&0x0F) << 8)
	uy := (uint16(raw[3]) >> 4) | (uint16(raw[4]) << 4)
	return tipSwitch, int32(ux), int32(uy), true
}

// TouchStrokeTracker 追踪 S2 原生 HID 触控从按下 (TipSwitch=1) 到抬起 (TipSwitch=0) 的完整轨迹
type TouchStrokeTracker struct {
	mu     sync.Mutex
	active bool
	startX int32
	startY int32
	lastX  int32
	lastY  int32
}

func NewTouchStrokeTracker() *TouchStrokeTracker {
	return &TouchStrokeTracker{}
}

// FeedReport 输入一帧 5 字节 HID 报告；当手指抬起完成一次手势时返回 done=true 及对应的按键与 (dx, dy)
func (t *TouchStrokeTracker) FeedReport(raw []byte) (key RemoteKey, dx int32, dy int32, done bool) {
	tip, x, y, ok := ParseS2TouchReport(raw)
	if !ok {
		return KeyNone, 0, 0, false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if tip {
		if !t.active {
			t.active = true
			t.startX = x
			t.startY = y
			t.lastX = x
			t.lastY = y
		} else {
			t.lastX = x
			t.lastY = y
		}
		return KeyNone, 0, 0, false
	}

	// tip == false (抬起)
	if !t.active {
		return KeyNone, 0, 0, false
	}
	t.active = false
	// 如果抬起帧带有非零有效坐标，更新终点坐标；若抬起帧归零 (0,0)，则沿用滑动中的最后一帧有效坐标
	if x != 0 || y != 0 {
		t.lastX = x
		t.lastY = y
	}
	dx = t.lastX - t.startX
	dy = t.lastY - t.startY
	key = ClassifySwipe(t.startX, t.startY, t.lastX, t.lastY)
	return key, dx, dy, true
}

// GestureThrottler 实现 500ms 硬件级防连点节流器
type GestureThrottler struct {
	mu       sync.Mutex
	interval time.Duration
	lastTime time.Time
}

func NewGestureThrottler(interval time.Duration) *GestureThrottler {
	return &GestureThrottler{
		interval: interval,
	}
}

func (gt *GestureThrottler) Allow(now time.Time) bool {
	gt.mu.Lock()
	defer gt.mu.Unlock()

	if !gt.lastTime.IsZero() && now.Sub(gt.lastTime) < gt.interval {
		return false
	}
	gt.lastTime = now
	return true
}
