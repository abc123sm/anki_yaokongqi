package main

import (
	"fmt"
)

// AppConfig 保存电脑端遥控器运行期状态
type AppConfig struct {
	PortMode   int // 1: 8765/8766, 2: 18765/18766, 3: 28765/28766
	AnkiURL    string
	AIExtURL   string
	ThrottleMs int
}

func DefaultConfig() AppConfig {
	cfg := AppConfig{
		PortMode:   1,
		ThrottleMs: 500,
	}
	cfg.ApplyPortMode(1)
	return cfg
}

// ApplyPortMode 根据模式 1/2/3 设置对应的端口与请求地址
func (c *AppConfig) ApplyPortMode(mode int) {
	if mode < 1 || mode > 3 {
		mode = 1
	}
	c.PortMode = mode
	c.AnkiURL = fmt.Sprintf("http://127.0.0.1:%d", c.AnkiPort())
	c.AIExtURL = fmt.Sprintf("http://127.0.0.1:%d/trigger-ai", c.AIPort())
}

// NextPortMode 在模式 1 -> 模式 2 -> 模式 3 -> 模式 1 之间循环切换
func (c *AppConfig) NextPortMode() int {
	next := c.PortMode + 1
	if next > 3 {
		next = 1
	}
	c.ApplyPortMode(next)
	return c.PortMode
}

// AnkiPort 返回当前模式对应的 AnkiConnect 端口 (8765 / 18765 / 28765)
func (c *AppConfig) AnkiPort() int {
	switch c.PortMode {
	case 2:
		return 18765
	case 3:
		return 28765
	default:
		return 8765
	}
}

// AIPort 返回当前模式对应的 AI 扩展服务端口 (8766 / 18766 / 28766)
func (c *AppConfig) AIPort() int {
	switch c.PortMode {
	case 2:
		return 18766
	case 3:
		return 28766
	default:
		return 8766
	}
}

// ModeLabel 返回用于 UI 显示的模式名称
func (c *AppConfig) ModeLabel() string {
	return fmt.Sprintf("模式 %d", c.PortMode)
}
