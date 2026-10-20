package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// AIChatInput 是一次对话的参数（对应 Rust AiChatInput）。
type AIChatInput struct {
	BaseURL string
	APIKey  string
	Model   string
	Prompt  string
}

var aiClient = &http.Client{
	Timeout:   90 * time.Second,
	Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 15 * time.Second}).DialContext, Proxy: http.ProxyFromEnvironment},
}

// AIChat 调用 OpenAI 兼容的 chat/completions，返回第一段输出。
func AIChat(in AIChatInput) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if base == "" {
		return "", errors.New("未配置 API 地址")
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" {
		return "", errors.New("未配置 API Key")
	}
	url := base + "/chat/completions"
	body, _ := json.Marshal(map[string]any{
		"model":       in.Model,
		"messages":    []map[string]string{{"role": "user", "content": in.Prompt}},
		"temperature": 0.2,
		"stream":      false,
	})
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := aiClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("无法连接 %s：%v", url, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		hint := map[int]string{401: "API Key 无效或已过期", 403: "没有权限（Key 未开通该模型？）",
			404: "接口地址不对（检查 API 地址是否为 OpenAI 兼容根路径）", 429: "请求过于频繁或额度不足"}[resp.StatusCode]
		if hint != "" {
			hint = "（" + hint + "）"
		}
		return "", fmt.Errorf("API 返回 %d%s：%s", resp.StatusCode, hint, BriefError(string(data)))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("响应不是合法 JSON：%v", err)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", errors.New("响应里没有模型输出（choices[0].message.content 为空）")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

// BriefError 从 API 错误体抽出最短的可读信息。
func BriefError(body string) string {
	var v map[string]any
	if json.Unmarshal([]byte(body), &v) != nil {
		return truncRunes(body, 300)
	}
	msg := body
	if e, ok := v["error"].(map[string]any); ok {
		if m, ok := e["message"].(string); ok {
			msg = m
		}
	} else if m, ok := v["message"].(string); ok {
		msg = m
	} else if m, ok := v["error"].(string); ok {
		msg = m
	}
	return truncRunes(msg, 300)
}

func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
