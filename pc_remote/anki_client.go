package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type CardSide string

const (
	SideQuestion CardSide = "question"
	SideAnswer   CardSide = "answer"
)

type AnkiController struct {
	mu            sync.Mutex
	ankiURL       string
	aiExtURL      string
	httpClient    *http.Client
	aiClient      *http.Client
	currentCardID int64
	currentSide   CardSide
	replayAudioFn func() error
}

func NewAnkiController(ankiURL, aiExtURL string, replayAudioFn func() error) *AnkiController {
	return &AnkiController{
		ankiURL:       ankiURL,
		aiExtURL:      aiExtURL,
		httpClient:    &http.Client{Timeout: 5 * time.Second},
		aiClient:      &http.Client{Timeout: 65 * time.Second},
		currentSide:   SideQuestion,
		replayAudioFn: replayAudioFn,
	}
}

func (c *AnkiController) SetEndpoints(ankiURL, aiExtURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ankiURL = ankiURL
	c.aiExtURL = aiExtURL
	c.currentCardID = 0
	c.currentSide = SideQuestion
}

func (c *AnkiController) CurrentSide() CardSide {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentSide
}

func (c *AnkiController) CurrentCardID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentCardID
}

type ankiRequest struct {
	Action  string      `json:"action"`
	Version int         `json:"version"`
	Params  interface{} `json:"params,omitempty"`
}

type ankiResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *string         `json:"error"`
}

func (c *AnkiController) callAnki(action string, params interface{}) (json.RawMessage, error) {
	reqBody, err := json.Marshal(ankiRequest{
		Action:  action,
		Version: 6,
		Params:  params,
	})
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Post(c.ankiURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("无法连接 AnkiConnect: %w", err)
	}
	defer resp.Body.Close()

	var ankiResp ankiResponse
	if err := json.NewDecoder(resp.Body).Decode(&ankiResp); err != nil {
		return nil, fmt.Errorf("解析 AnkiConnect 响应失败: %w", err)
	}
	if ankiResp.Error != nil && *ankiResp.Error != "" {
		return nil, errors.New(*ankiResp.Error)
	}
	return ankiResp.Result, nil
}

func (c *AnkiController) resumeReviewIfNeeded() bool {
	if c.aiExtURL == "" {
		return false
	}
	continueURL := strings.Replace(c.aiExtURL, "/trigger-ai", "/auto-continue", 1)
	resp, err := c.httpClient.Post(continueURL, "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var res struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return false
	}
	return res.Success
}

// syncCurrentCard 拉取当前正在学习的卡片 ID，若复习未激活则自动调用 /auto-continue 唤醒续航
func (c *AnkiController) syncCurrentCard() (int64, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		raw, err := c.callAnki("guiCurrentCard", nil)
		if err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "Gui review is not currently active") {
				if c.resumeReviewIfNeeded() {
					time.Sleep(150 * time.Millisecond)
					rawRetry, errRetry := c.callAnki("guiCurrentCard", nil)
					if errRetry == nil {
						raw = rawRetry
						lastErr = nil
					} else {
						lastErr = errRetry
						continue
					}
				} else {
					break
				}
			} else {
				return 0, err
			}
		}

		if len(raw) == 0 || string(raw) == "null" {
			return 0, errors.New("当前无待复习卡片")
		}

		var card struct {
			CardID int64 `json:"cardId"`
		}
		if err := json.Unmarshal(raw, &card); err != nil {
			return 0, err
		}
		if card.CardID == 0 {
			return 0, errors.New("当前无有效卡片")
		}

		// 若发现外部切换了卡片（cardId 改变），自动重置为正面状态
		if c.currentCardID != card.CardID {
			c.currentCardID = card.CardID
			c.currentSide = SideQuestion
		}
		return card.CardID, nil
	}
	return 0, lastErr
}

func (c *AnkiController) showAnswerLocked() (string, error) {
	if _, err := c.callAnki("guiShowAnswer", nil); err != nil {
		return "", fmt.Errorf("显示答案失败: %w", err)
	}
	c.currentSide = SideAnswer
	return "显示答案", nil
}

func (c *AnkiController) rateLocked(ease int, label string) (string, error) {
	params := map[string]int{"ease": ease}
	if _, err := c.callAnki("guiAnswerCard", params); err != nil {
		return "", fmt.Errorf("评分失败: %w", err)
	}
	c.currentSide = SideQuestion
	// 同步下一张卡的 ID
	_, _ = c.syncCurrentCard()
	return label, nil
}

func (c *AnkiController) triggerAILocked() (string, error) {
	if c.aiExtURL == "" {
		return "", errors.New("未配置 AI 触发器地址")
	}
	resp, err := c.aiClient.Post(c.aiExtURL, "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", fmt.Errorf("AI 触发失败: %w", err)
	}
	defer resp.Body.Close()
	return "AI 注释生成完成", nil
}

// HandleKey 执行遥控器物理按键对应的 Anki 动作
func (c *AnkiController) HandleKey(key RemoteKey) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if key == KeyNone {
		return "", nil
	}

	// 1. 同步当前卡片与自动续航唤醒
	if _, err := c.syncCurrentCard(); err != nil {
		return "", err
	}

	// 2. 下键 / OK键：无论正背面均执行重播音频
	if key == KeyDown || key == KeyOK {
		if c.replayAudioFn != nil {
			if err := c.replayAudioFn(); err != nil {
				return "", fmt.Errorf("重播音频失败: %w", err)
			}
		}
		return "重播音频", nil
	}

	// 3. 正面状态：左键 / 右键 / 上键 全部执行翻面 (guiShowAnswer)
	if c.currentSide == SideQuestion {
		return c.showAnswerLocked()
	}

	// 4. 背面状态：
	switch key {
	case KeyLeft:
		return c.rateLocked(3, "良好")
	case KeyRight:
		return c.rateLocked(1, "重来")
	case KeyUp:
		return c.triggerAILocked()
	default:
		return "", nil
	}
}
