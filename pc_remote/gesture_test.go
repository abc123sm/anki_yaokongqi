package main

import (
	"testing"
	"time"
)

func TestClassifySwipe(t *testing.T) {
	tests := []struct {
		name           string
		startX, startY int32
		endX, endY     int32
		wantKey        RemoteKey
	}{
		{
			name:   "原地单击 (dx=0, dy=0) -> OK键",
			startX: 500, startY: 500,
			endX: 500, endY: 500,
			wantKey: KeyOK,
		},
		{
			name:   "微小偏移单击 (|dx|<10, |dy|<10) -> OK键",
			startX: 500, startY: 500,
			endX: 509, endY: 492,
			wantKey: KeyOK,
		},
		{
			name:   "向右滑动 (dx > 30, |dx| > |dy|) -> 物理左键 (KeyLeft)",
			startX: 500, startY: 500,
			endX: 620, endY: 510,
			wantKey: KeyLeft,
		},
		{
			name:   "向左滑动 (dx < -30, |dx| > |dy|) -> 物理右键 (KeyRight)",
			startX: 500, startY: 500,
			endX: 380, endY: 495,
			wantKey: KeyRight,
		},
		{
			name:   "向下滑动 (dy > 30, |dy| >= |dx|) -> 物理上键 (KeyUp)",
			startX: 500, startY: 500,
			endX: 505, endY: 650,
			wantKey: KeyUp,
		},
		{
			name:   "向上滑动 (dy < -30, |dy| >= |dx|) -> 物理下键 (KeyDown)",
			startX: 500, startY: 500,
			endX: 495, endY: 350,
			wantKey: KeyDown,
		},
		{
			name:   "处于死区的微弱抖动 (10 <= |dx| <= 30, |dy| <= 30) -> 忽略 (KeyNone)",
			startX: 500, startY: 500,
			endX: 520, endY: 515,
			wantKey: KeyNone,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifySwipe(tc.startX, tc.startY, tc.endX, tc.endY)
			if got != tc.wantKey {
				t.Errorf("ClassifySwipe(%d,%d -> %d,%d) = %v, want %v",
					tc.startX, tc.startY, tc.endX, tc.endY, got, tc.wantKey)
			}
		})
	}
}

func TestGestureThrottler(t *testing.T) {
	throttler := NewGestureThrottler(500 * time.Millisecond)
	baseTime := time.Date(2026, 10, 26, 20, 0, 0, 0, time.UTC)

	// 第一次按键应当放行
	if !throttler.Allow(baseTime) {
		t.Fatalf("首次触发应当被允许")
	}

	// 200ms 内的连点应当被拦截
	if throttler.Allow(baseTime.Add(200 * time.Millisecond)) {
		t.Fatalf("200ms 内的重复触发应当被 500ms 节流器拦截")
	}

	// 499ms 内依然拦截
	if throttler.Allow(baseTime.Add(499 * time.Millisecond)) {
		t.Fatalf("499ms 内的重复触发应当被拦截")
	}

	// 500ms 整应当放行
	if !throttler.Allow(baseTime.Add(500 * time.Millisecond)) {
		t.Fatalf("满 500ms 后的触发应当被允许")
	}
}

func makeS2Packet(tip bool, x, y int32) []byte {
	var b1 byte
	if tip {
		b1 = 0x07 // TipSwitch=1, InRange=1, Confidence=1
	}
	ux := uint16(x & 0x0FFF)
	uy := uint16(y & 0x0FFF)
	return []byte{
		0x01, // ReportID = 1
		b1,
		byte(ux & 0xFF),
		byte(((ux >> 8) & 0x0F) | ((uy & 0x0F) << 4)),
		byte((uy >> 4) & 0xFF),
	}
}

func TestParseS2TouchReportAndTracker(t *testing.T) {
	// 1. 验证 12-bit X/Y 编解码精确性
	pkt := makeS2Packet(true, 500, 720)
	tip, x, y, ok := ParseS2TouchReport(pkt)
	if !ok || !tip || x != 500 || y != 720 {
		t.Fatalf("ParseS2TouchReport 解码错误: got tip=%v x=%d y=%d ok=%v", tip, x, y, ok)
	}

	tracker := NewTouchStrokeTracker()

	// 2. 模拟按下【下键】(向上滑动: Y 从 600 滑到 320, dy = -280 < -30)
	if k, _, _, done := tracker.FeedReport(makeS2Packet(true, 500, 600)); done || k != KeyNone {
		t.Fatalf("按下首帧不应直接触发动作")
	}
	if k, _, _, done := tracker.FeedReport(makeS2Packet(true, 500, 450)); done || k != KeyNone {
		t.Fatalf("滑动中不应触发动作")
	}
	if k, _, _, done := tracker.FeedReport(makeS2Packet(true, 500, 320)); done || k != KeyNone {
		t.Fatalf("滑动中不应触发动作")
	}
	// 抬起帧 (即使抬起帧坐标归零 0,0，也应保留最后有效滑动坐标 500,320)
	key, dx, dy, done := tracker.FeedReport(makeS2Packet(false, 0, 0))
	if !done || key != KeyDown || dx != 0 || dy != -280 {
		t.Fatalf("向上滑动抬起后应识别为 KeyDown (dx=0, dy=-280), 实际为 done=%v key=%v dx=%d dy=%d", done, key, dx, dy)
	}

	// 3. 模拟按下【OK键】(原地点击: 500,500 -> 抬起)
	tracker.FeedReport(makeS2Packet(true, 500, 500))
	key, dx, dy, done = tracker.FeedReport(makeS2Packet(false, 500, 500))
	if !done || key != KeyOK || dx != 0 || dy != 0 {
		t.Fatalf("原地点击抬起后应识别为 KeyOK, 实际为 done=%v key=%v dx=%d dy=%d", done, key, dx, dy)
	}
}

