package workbuddy

import (
	"encoding/json"
	"testing"
)

func TestParseBalanceAggregatesPackages(t *testing.T) {
	// 取自国际版真实响应结构（两个计费包，一个用 Cycle*、一个用 Capacity*）。
	raw := json.RawMessage(`{"Response":{"Data":{"TotalCount":2,"TotalDosage":350,"Accounts":[
		{"DealName":"pkg-a","CapacityType":1,"CapacitySize":250,"CapacityRemain":250,"CycleCapacitySize":250,"CycleCapacityRemain":250,"CycleEndTime":"2026-09-28 10:52:12"},
		{"DealName":"pkg-b","CapacityType":4,"CapacitySize":100,"CapacityRemain":60,"CycleCapacitySize":0,"CycleCapacityRemain":0,"CycleEndTime":"2026-09-30 23:59:59"}
	]}}}`)
	bal, err := parseBalance(raw)
	if err != nil {
		t.Fatal(err)
	}
	if bal.Size != 350 {
		t.Errorf("Size = %v, want 350", bal.Size)
	}
	if bal.Remain != 310 {
		t.Errorf("Remain = %v, want 310", bal.Remain)
	}
	if bal.Used != 40 {
		t.Errorf("Used = %v, want 40", bal.Used)
	}
	if len(bal.Packages) != 2 {
		t.Fatalf("Packages = %d, want 2", len(bal.Packages))
	}
	if bal.UpdatedAt == "" {
		t.Error("UpdatedAt 不应为空")
	}
}

func TestParseBalanceClampsNegativeAndEmpty(t *testing.T) {
	// 负剩余按 0；size 不小于 remain；空列表不报错。
	raw := json.RawMessage(`{"Response":{"Data":{"Accounts":[
		{"CapacitySize":10,"CapacityRemain":-5}
	]}}}`)
	bal, err := parseBalance(raw)
	if err != nil {
		t.Fatal(err)
	}
	if bal.Remain != 0 {
		t.Errorf("负剩余应夹到 0，得到 %v", bal.Remain)
	}
	if bal.Size != 10 {
		t.Errorf("Size = %v, want 10", bal.Size)
	}

	empty := json.RawMessage(`{"Response":{"Data":{"Accounts":[]}}}`)
	if _, err := parseBalance(empty); err != nil {
		t.Errorf("空账号列表不应报错: %v", err)
	}
}

func TestEndpointBillingPerRegion(t *testing.T) {
	if got := endpointBilling(regionCN); got != "https://www.codebuddy.cn/v2/billing/meter/get-user-resource" {
		t.Errorf("国内 billing URL = %q", got)
	}
	if got := endpointBilling(regionIntl); got != "https://www.workbuddy.ai/v2/billing/meter/get-user-resource" {
		t.Errorf("国际 billing URL = %q", got)
	}
}
