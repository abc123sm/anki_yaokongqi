package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type mockAnkiServer struct {
	mu              sync.Mutex
	currentCardID   int64
	reviewActive    bool
	actionsCalled   []string
	lastAnswerEase  int
	aiTriggerCount  int
	autoContCount   int
	ankiHandler     *httptest.Server
	aiExtHandler    *httptest.Server
}

func newMockAnkiServer(cardID int64, active bool) *mockAnkiServer {
	m := &mockAnkiServer{
		currentCardID: cardID,
		reviewActive:  active,
	}

	m.ankiHandler = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action  string          `json:"action"`
			Version int             `json:"version"`
			Params  json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		m.mu.Lock()
		defer m.mu.Unlock()
		m.actionsCalled = append(m.actionsCalled, req.Action)

		switch req.Action {
		case "guiCurrentCard":
			if !m.reviewActive {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"result": nil,
					"error":  "Gui review is not currently active",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"cardId": m.currentCardID,
				},
				"error": nil,
			})
		case "guiShowAnswer":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": true,
				"error":  nil,
			})
		case "guiAnswerCard":
			var p struct {
				Ease int `json:"ease"`
			}
			_ = json.Unmarshal(req.Params, &p)
			m.lastAnswerEase = p.Ease
			m.currentCardID++ // 模拟自动切到下一张卡
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": true,
				"error":  nil,
			})
		}
	}))

	m.aiExtHandler = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		switch r.URL.Path {
		case "/trigger-ai":
			m.aiTriggerCount++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": "success",
			})
		case "/auto-continue":
			m.autoContCount++
			m.reviewActive = true
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result":  "success",
				"success": true,
			})
		}
	}))

	return m
}

func (m *mockAnkiServer) close() {
	m.ankiHandler.Close()
	m.aiExtHandler.Close()
}

func TestController_FrontAndBackFlow(t *testing.T) {
	mock := newMockAnkiServer(1001, true)
	defer mock.close()

	replayed := 0
	ctrl := NewAnkiController(mock.ankiHandler.URL, mock.aiExtHandler.URL+"/trigger-ai", func() error {
		replayed++
		return nil
	})

	// 1. 初始在正面 (question)，按左键 (KeyLeft) 应当执行翻面 (guiShowAnswer)
	msg, err := ctrl.HandleKey(KeyLeft)
	if err != nil {
		t.Fatalf("正面按左键出错: %v", err)
	}
	if ctrl.CurrentSide() != SideAnswer {
		t.Fatalf("正面按左键后应当进入背面 SideAnswer, 实际为 %v (msg=%s)", ctrl.CurrentSide(), msg)
	}

	// 2. 在背面 (answer)，按上键 (KeyUp) 应当触发 AI 提问 (/trigger-ai) 且仍保持在背面
	_, err = ctrl.HandleKey(KeyUp)
	if err != nil {
		t.Fatalf("背面按上键触发AI出错: %v", err)
	}
	if mock.aiTriggerCount != 1 {
		t.Fatalf("背面按上键应当触发 1 次 AI, 实际为 %d", mock.aiTriggerCount)
	}
	if ctrl.CurrentSide() != SideAnswer {
		t.Fatalf("触发 AI 后应当仍在背面 SideAnswer")
	}

	// 3. 在背面，按下键 (KeyDown) 和 OK 键 (KeyOK) 均应触发音频重播
	_, _ = ctrl.HandleKey(KeyDown)
	_, _ = ctrl.HandleKey(KeyOK)
	if replayed != 2 {
		t.Fatalf("下键与OK键应当触发 2 次音频重播, 实际为 %d", replayed)
	}

	// 4. 在背面，按左键 (KeyLeft) 应当评分 Good (ease=3)，并切入下一张卡的正面 (SideQuestion)
	_, err = ctrl.HandleKey(KeyLeft)
	if err != nil {
		t.Fatalf("背面按左键评分出错: %v", err)
	}
	if mock.lastAnswerEase != 3 {
		t.Fatalf("背面按左键应当评分 ease=3, 实际为 %d", mock.lastAnswerEase)
	}
	if ctrl.CurrentSide() != SideQuestion {
		t.Fatalf("评分后应当回到正面 SideQuestion")
	}

	// 5. 此时在新卡 (1002) 正面，按上键 (KeyUp) 应当翻面而非触发 AI
	_, err = ctrl.HandleKey(KeyUp)
	if err != nil {
		t.Fatalf("新卡正面按上键出错: %v", err)
	}
	if mock.aiTriggerCount != 1 {
		t.Fatalf("正面按上键不应触发 AI")
	}
	if ctrl.CurrentSide() != SideAnswer {
		t.Fatalf("正面按上键后应进入背面 SideAnswer")
	}

	// 6. 在背面，按右键 (KeyRight) 应当评分 Again (ease=1)
	_, err = ctrl.HandleKey(KeyRight)
	if err != nil {
		t.Fatalf("背面按右键评分出错: %v", err)
	}
	if mock.lastAnswerEase != 1 {
		t.Fatalf("背面按右键应当评分 ease=1, 实际为 %d", mock.lastAnswerEase)
	}
}

func TestController_ExternalCardChangeResetsToQuestion(t *testing.T) {
	mock := newMockAnkiServer(2001, true)
	defer mock.close()

	ctrl := NewAnkiController(mock.ankiHandler.URL, mock.aiExtHandler.URL+"/trigger-ai", func() error { return nil })

	// 先翻到背面
	_, _ = ctrl.HandleKey(KeyRight)
	if ctrl.CurrentSide() != SideAnswer {
		t.Fatalf("应当处于背面")
	}

	// 模拟用户在电脑上用键盘手动切到了下一张卡 2002（此时新卡在正面）
	mock.mu.Lock()
	mock.currentCardID = 2002
	mock.mu.Unlock()

	// 用户按左键：因为检测到 cardId 从 2001 变成了 2002，应当识别为新卡正面并执行翻面，绝不能直接对 2002 打分！
	_, err := ctrl.HandleKey(KeyLeft)
	if err != nil {
		t.Fatalf("按左键出错: %v", err)
	}
	if mock.lastAnswerEase != 0 {
		t.Fatalf("外部切换新卡后首次按键不应直接评分，应当先翻面！")
	}
	if ctrl.CurrentSide() != SideAnswer {
		t.Fatalf("翻面后应当处于新卡的背面")
	}
}

func TestController_AutoContinueWhenReviewInactive(t *testing.T) {
	mock := newMockAnkiServer(3001, false) // 初始未激活复习（如停留在恭喜页面）
	defer mock.close()

	ctrl := NewAnkiController(mock.ankiHandler.URL, mock.aiExtHandler.URL+"/trigger-ai", func() error { return nil })

	// 此时按任意键应当触发 /auto-continue 唤醒复习，并完成当前卡片加载
	_, err := ctrl.HandleKey(KeyLeft)
	if err != nil {
		t.Fatalf("自动唤醒复习出错: %v", err)
	}
	if mock.autoContCount == 0 {
		t.Fatalf("复习未激活时应当自动调用 /auto-continue")
	}
}

func TestPortModeSwitch(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.PortMode != 1 || cfg.AnkiPort() != 8765 || cfg.AIPort() != 8766 {
		t.Fatalf("默认模式应为模式1 (8765/8766), 实际为 mode=%d (%d/%d)", cfg.PortMode, cfg.AnkiPort(), cfg.AIPort())
	}
	if cfg.AnkiURL != "http://127.0.0.1:8765" || cfg.AIExtURL != "http://127.0.0.1:8766/trigger-ai" {
		t.Fatalf("模式1 URL 不匹配: %s / %s", cfg.AnkiURL, cfg.AIExtURL)
	}

	// 切换到模式 2 (18765 / 18766)
	cfg.NextPortMode()
	if cfg.PortMode != 2 || cfg.AnkiPort() != 18765 || cfg.AIPort() != 18766 {
		t.Fatalf("切换后应为模式2 (18765/18766), 实际为 mode=%d (%d/%d)", cfg.PortMode, cfg.AnkiPort(), cfg.AIPort())
	}
	if cfg.AnkiURL != "http://127.0.0.1:18765" || cfg.AIExtURL != "http://127.0.0.1:18766/trigger-ai" {
		t.Fatalf("模式2 URL 不匹配: %s / %s", cfg.AnkiURL, cfg.AIExtURL)
	}

	// 切换到模式 3 (28765 / 28766)
	cfg.NextPortMode()
	if cfg.PortMode != 3 || cfg.AnkiPort() != 28765 || cfg.AIPort() != 28766 {
		t.Fatalf("切换后应为模式3 (28765/28766), 实际为 mode=%d (%d/%d)", cfg.PortMode, cfg.AnkiPort(), cfg.AIPort())
	}
	if cfg.AnkiURL != "http://127.0.0.1:28765" || cfg.AIExtURL != "http://127.0.0.1:28766/trigger-ai" {
		t.Fatalf("模式3 URL 不匹配: %s / %s", cfg.AnkiURL, cfg.AIExtURL)
	}

	// 再次切换应回到模式 1 (8765 / 8766)
	cfg.NextPortMode()
	if cfg.PortMode != 1 || cfg.AnkiPort() != 8765 || cfg.AIPort() != 8766 {
		t.Fatalf("循环切换后应回到模式1 (8765/8766), 实际为 mode=%d", cfg.PortMode)
	}

	// 验证控制器切换端口时重置正背面状态机
	mock1 := newMockAnkiServer(1001, true)
	defer mock1.close()
	mock2 := newMockAnkiServer(2001, true)
	defer mock2.close()

	ctrl := NewAnkiController(mock1.ankiHandler.URL, mock1.aiExtHandler.URL+"/trigger-ai", func() error { return nil })
	_, _ = ctrl.HandleKey(KeyLeft) // 翻到模式1卡片的背面
	if ctrl.CurrentSide() != SideAnswer {
		t.Fatalf("应当在背面")
	}

	// 切换到实例2的端口后，状态应立即重置为正面 SideQuestion
	ctrl.SetEndpoints(mock2.ankiHandler.URL, mock2.aiExtHandler.URL+"/trigger-ai")
	if ctrl.CurrentSide() != SideQuestion || ctrl.CurrentCardID() != 0 {
		t.Fatalf("切换端口模式后应当重置为 SideQuestion 及 cardID=0")
	}
}

