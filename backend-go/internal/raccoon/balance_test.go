package raccoon

import "testing"

// TestBalanceUnmarshal 回归：上游 balance 响应里混有布尔/字符串字段
// （topup_frozen / owner_type 等），旧的 map[string]*int64 会整体反序列化失败，
// 导致可用积分永远显示为空。这里断言只取数值字段且 0 是合法值。
func TestBalanceUnmarshal(t *testing.T) {
	raw := []byte(`{
		"available_points": 7794,
		"daily_points": 294,
		"monthly_points": 0,
		"reward_points": 7500,
		"topup_points": 0,
		"enterprise_topup_points": 0,
		"total_chargeable_points": 7794,
		"enterprise_reserved": false,
		"topup_frozen": false,
		"owner_type": "users",
		"owner_id": 7838004
	}`)
	var b Balance
	if err := b.UnmarshalJSON(raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if b.AvailablePoints == nil || *b.AvailablePoints != 7794 {
		t.Errorf("available = %v, want 7794", b.AvailablePoints)
	}
	if b.DailyPoints == nil || *b.DailyPoints != 294 {
		t.Errorf("daily = %v, want 294", b.DailyPoints)
	}
	if b.MonthlyPoints == nil || *b.MonthlyPoints != 0 {
		t.Errorf("monthly = %v, want 0 (zero is valid)", b.MonthlyPoints)
	}
	if b.RewardPoints == nil || *b.RewardPoints != 7500 {
		t.Errorf("reward = %v, want 7500", b.RewardPoints)
	}
}

// TestBalanceFlexInt 验证数字字符串兼容与缺失字段。
func TestBalanceFlexInt(t *testing.T) {
	var b Balance
	if err := b.UnmarshalJSON([]byte(`{"available_points":"1234"}`)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if b.AvailablePoints == nil || *b.AvailablePoints != 1234 {
		t.Errorf("string number = %v, want 1234", b.AvailablePoints)
	}
	if b.DailyPoints != nil {
		t.Errorf("missing field should be nil, got %v", b.DailyPoints)
	}
}
