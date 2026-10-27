package serveragent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestAgentUploadRequiresValidator 锁住二进制直传端点的鉴权边界：未注入
// AI 接入校验器时一律拒绝，注入后由校验器决定放行。
func TestAgentUploadRequiresValidator(t *testing.T) {
	s := &Service{}
	req := httptest.NewRequest(http.MethodPost, "/api/server/agent/upload", nil)
	if s.authorizeAIAgent(req) {
		t.Fatalf("未注入校验器时不应放行")
	}

	called := false
	s.SetAIAgentValidator(func(r *http.Request) bool {
		called = true
		return true
	})
	if !s.authorizeAIAgent(req) {
		t.Fatalf("校验器放行时 authorizeAIAgent 应返回 true")
	}
	if !called {
		t.Fatalf("校验器应被调用")
	}
}

// TestAgentFetchURLValidatesParams 验证 fetch-url 在缺少必填字段时直接返回 400，
// 不会触达 Agent 下发路径。
func TestAgentFetchURLValidatesParams(t *testing.T) {
	s := &Service{}
	body := strings.NewReader(`{"serverId":"host-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/server/agent/fetch-url", body)
	rec := httptest.NewRecorder()
	s.handleAgentFetchURL(rec, req, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺少 url/path 应返回 400，实际 %d", rec.Code)
	}
}

// TestWriteAgentFileBytesValidatesParams 验证字节写入在缺少必填字段时直接失败，
// 不会静默丢弃。
func TestWriteAgentFileBytesValidatesParams(t *testing.T) {
	s := &Service{}
	if _, err := s.WriteAgentFileBytes(context.Background(), "", "/tmp/a.bin", []byte("data"), 0); err == nil {
		t.Fatal("空 serverID 写入应返回错误")
	}
}

// TestAgentFileWriteResultParsing 覆盖真实路径的解析：新版 Agent 返回 JSON，
// 旧版只回纯文本，两种都必须能解析且不 panic。
func TestAgentFileWriteResultParsing(t *testing.T) {
	modern := parseAgentFileWriteResult(
		`{"message":"已写入 12 字节","realPath":"/var/lib/api-monitor/agent/files/opt/a.sh","size":12}`)
	if modern.RealPath != "/var/lib/api-monitor/agent/files/opt/a.sh" {
		t.Fatalf("realPath 解析失败: %#v", modern)
	}
	if modern.Size != 12 {
		t.Fatalf("size 解析失败: %#v", modern)
	}

	legacy := parseAgentFileWriteResult("已写入 12 字节")
	if legacy.RealPath != "" {
		t.Fatalf("旧版 Agent 不应伪造 realPath: %#v", legacy)
	}
	if legacy.Message != "已写入 12 字节" {
		t.Fatalf("旧版消息应保留: %#v", legacy)
	}

	empty := parseAgentFileWriteResult("")
	if empty.Message != "" || empty.RealPath != "" {
		t.Fatalf("空输入不应产生内容: %#v", empty)
	}
}

// captureWriteSocket 记录下发给 Agent 的 task payload，并模拟 Agent 回报结果，
// 用于验证二进制字节走的是 base64 分块而不是 string 直塞。
type captureWriteSocket struct {
	t        *testing.T
	service  *Service
	mu       sync.Mutex
	payloads []map[string]interface{}
}

func (s *captureWriteSocket) WriteMessage(_ int, data []byte) error {
	raw := string(data)
	var frame []interface{}
	if err := json.Unmarshal([]byte(raw[2:]), &frame); err != nil {
		s.t.Fatalf("decode socket frame: %v frame=%s", err, raw)
	}
	payload, _ := frame[1].(map[string]interface{})
	s.mu.Lock()
	s.payloads = append(s.payloads, payload)
	s.mu.Unlock()

	taskID, _ := payload["id"].(string)
	taskType, _ := payload["type"].(float64)
	dataStr, _ := payload["data"].(string)
	var result string
	if int(taskType) == agentFileWriteBase64Task {
		var req struct {
			Path string `json:"path"`
			Data string `json:"data"`
		}
		if err := json.Unmarshal([]byte(dataStr), &req); err == nil {
			result = `{"message":"已写入 1 块","realPath":"/var/lib/api-monitor/agent/files` + req.Path + `","size":10}`
		}
	}
	go s.service.taskRegistry.Complete(taskID, result)
	return nil
}

func (s *captureWriteSocket) snapshot() []map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]interface{}, len(s.payloads))
	copy(out, s.payloads)
	return out
}

// TestAgentFileWritePreservesBinaryBytes 锁住二进制损坏回归：
// SFTP 上传的 Agent 分支曾把原始字节强转 string 再塞进单个 JSON 帧，
// 非法 UTF-8 序列在 json.Marshal 时被替换成 U+FFFD（实测 zip/图片会静默损坏）。
// 现在必须走 base64 分块写入。
func TestAgentFileWritePreservesBinaryBytes(t *testing.T) {
	service, _ := testService(t)
	socket := &captureWriteSocket{t: t, service: service}
	service.registry.Register("server-1", socket)

	// 含非法 UTF-8 序列（0xff 0xd8 0x00 0x80 0xfe）；走 string 会被破坏。
	original := []byte{0x50, 0x4b, 0x03, 0x04, 0xff, 0xd8, 0x00, 0x80, 0xfe, 0x01}
	if _, err := service.WriteAgentFileBytes(context.Background(), "server-1", "/opt/a.bin", original, 0); err != nil {
		t.Fatalf("WriteAgentFileBytes err: %v", err)
	}

	var found bool
	for _, payload := range socket.snapshot() {
		taskType, _ := payload["type"].(float64)
		if int(taskType) != agentFileWriteBase64Task {
			continue
		}
		found = true
		dataStr, _ := payload["data"].(string)
		var req struct {
			Path     string `json:"path"`
			Data     string `json:"data"`
			Truncate bool   `json:"truncate"`
		}
		if err := json.Unmarshal([]byte(dataStr), &req); err != nil {
			t.Fatalf("分块写入 payload 解析失败: %v", err)
		}
		if !req.Truncate {
			t.Fatal("首块应要求 truncate")
		}
		decoded, err := base64.StdEncoding.DecodeString(req.Data)
		if err != nil {
			t.Fatalf("分块数据应为合法 base64: %v", err)
		}
		if !bytes.Equal(decoded, original) {
			t.Fatalf("字节被改写: %x != %x", decoded, original)
		}
	}
	if !found {
		t.Fatal("未下发 base64 分块写入任务，仍可能走了 string 直塞路径")
	}
}
