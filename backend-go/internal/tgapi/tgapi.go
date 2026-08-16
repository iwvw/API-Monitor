// Package tgapi 是 Telegram Bot API 的共享调用层。
// 通知中心（notification）与管理 AI 频道（adminai/channel）都通过本包收发，
// 避免两套 HTTP 调用实现漂移（此前通知侧修全角冒号乱码、频道侧不受益）。
package tgapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Envelope 是 Telegram Bot API 响应的通用信封：ok / description / result。
// Result 保持原始 JSON，由调用方按方法解码（sendMessage 为对象、getUpdates 为数组）。
type Envelope struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

// Client 是 Telegram Bot API 客户端。
type Client struct {
	Token  string
	HTTP   *http.Client
	Base   string
}

// NewClient 构造客户端。base 为空时使用官方端点；httpClient 为空时使用默认客户端。
func NewClient(token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{Token: token, HTTP: httpClient, Base: "https://api.telegram.org"}
}

// Call 调用任意 Bot API 方法（POST + JSON），返回通用信封。
// 网络失败统一包装为 "telegram API request failed: %w"，便于调用方识别。
func (c *Client) Call(ctx context.Context, method string, payload map[string]interface{}) (Envelope, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal telegram payload: %w", err)
	}
	endpoint := c.Base + "/bot" + c.Token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Envelope{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		var requestErr *url.Error
		if errors.As(err, &requestErr) {
			err = requestErr.Err
		}
		return Envelope{}, fmt.Errorf("telegram API request failed: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return Envelope{}, fmt.Errorf("read telegram response: %w", err)
	}

	var envelope Envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return Envelope{}, fmt.Errorf("telegram API status %d", res.StatusCode)
		}
		return Envelope{}, fmt.Errorf("decode telegram response: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if envelope.Description != "" {
			return Envelope{}, fmt.Errorf("telegram API status %d: %s", res.StatusCode, envelope.Description)
		}
		return Envelope{}, fmt.Errorf("telegram API status %d", res.StatusCode)
	}
	if !envelope.OK {
		return Envelope{}, fmt.Errorf("telegram API error: %s", envelope.Description)
	}
	return envelope, nil
}

// DecodeObject 把 Envelope.Result 解码为对象（sendMessage / sendRichMessage 等）。
func DecodeObject[T any](env Envelope) (T, error) {
	var out T
	if err := json.Unmarshal(env.Result, &out); err != nil {
		return out, fmt.Errorf("decode telegram result: %w", err)
	}
	return out, nil
}

// DecodeArray 把 Envelope.Result 解码为数组（getUpdates 等）。
func DecodeArray[T any](env Envelope) ([]T, error) {
	var out []T
	if err := json.Unmarshal(env.Result, &out); err != nil {
		return nil, fmt.Errorf("decode telegram result: %w", err)
	}
	return out, nil
}
