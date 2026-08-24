package system

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestValidateAICallBody(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_monitor_contract_gate_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	service := New(config.Config{DataDir: tempDir, DBName: "data.db"})
	defer service.Shutdown()

	// 合法请求体：契约字段齐全且类型正确
	validBody, _ := json.Marshal(map[string]interface{}{
		"name":           "测试任务",
		"command":        "echo hello",
		"schedule":       "*/5 * * * *",
		"type":           "shell",
		"enabled":        true,
		"config":         `{"channelId":"abc"}`,
		"node_id":        "local",
		"timeout_seconds": 300,
	})
if err := service.ValidateAICallBody("POST", "/api/scheduler/tasks", validBody); err != nil {
		t.Fatalf("valid body should pass, got %v", err)
	}

	// boolean 兼容 0/1 整数（enabled 契约注明兼容）
	intEnabled, _ := json.Marshal(map[string]interface{}{
		"name": "x", "command": "echo", "enabled": 1, "schedule": "0 2 * * *",
	})
	if err := service.ValidateAICallBody("POST", "/api/scheduler/tasks", intEnabled); err != nil {
		t.Fatalf("enabled=1 should pass for boolean-compatible field, got %v", err)
	}
	badIntEnabled, _ := json.Marshal(map[string]interface{}{
		"name": "x", "command": "echo", "enabled": 2, "schedule": "0 2 * * *",
	})
	if err := service.ValidateAICallBody("POST", "/api/scheduler/tasks", badIntEnabled); err == nil {
		t.Fatal("enabled=2 should be rejected for boolean field")
	}

	// 缺少必填字段 name/command -> 报错
	missingName, _ := json.Marshal(map[string]interface{}{"command": "echo"})
	err = service.ValidateAICallBody("POST", "/api/scheduler/tasks", missingName)
	if err == nil || !strings.Contains(err.Error(), "缺少必填字段") {
		t.Fatalf("expected missing-required error, got %v", err)
	}

	// 类型错误：schedule 传入数组
	badType, _ := json.Marshal(map[string]interface{}{"name": "x", "command": "echo", "schedule": []string{"a"}})
	err = service.ValidateAICallBody("POST", "/api/scheduler/tasks", badType)
	if err == nil || !strings.Contains(err.Error(), "应为字符串") {
		t.Fatalf("expected type error, got %v", err)
	}

	// 枚举错误：confirm 不是 RESTORE
	badEnum, _ := json.Marshal(map[string]interface{}{"id": "1", "confirm": "NO"})
	err = service.ValidateAICallBody("POST", "/api/backup/restore", badEnum)
	if err == nil || !strings.Contains(err.Error(), "可选值") {
		t.Fatalf("expected enum error, got %v", err)
	}

	// 非 JSON -> 报错
	err = service.ValidateAICallBody("POST", "/api/scheduler/tasks", json.RawMessage(`not-json`))
	if err == nil || !strings.Contains(err.Error(), "合法 JSON") {
		t.Fatalf("expected invalid-json error, got %v", err)
	}

	// GET 不校验（只读无副作用）
	if err := service.ValidateAICallBody("GET", "/api/scheduler/tasks", missingName); err != nil {
		t.Fatalf("GET should skip validation, got %v", err)
	}

	// 无契约路由跳过校验（如纯 GET 数据接口的 POST 子路径）
	noContract, _ := json.Marshal(map[string]interface{}{"whatever": 1})
	if err := service.ValidateAICallBody("POST", "/api/system/api-docs", noContract); err != nil {
		t.Fatalf("route without contract should pass, got %v", err)
	}

	// 具体子路径也能命中模板契约（pattern 匹配）
	subPath, _ := json.Marshal(map[string]interface{}{"accountId": "acc-1", "image": "iwvw/api-monitor:dev"})
	if err := service.ValidateAICallBody("POST", "/api/flyio/apps/some-app/update-image", subPath); err != nil {
		t.Fatalf("pattern route should pass, got %v", err)
	}
badSubPath, _ := json.Marshal(map[string]interface{}{"accountId": "acc-1"})
	if err := service.ValidateAICallBody("POST", "/api/flyio/apps/some-app/update-image", badSubPath); err == nil {
		t.Fatal("expected missing image error on pattern route")
	}
}
