package accountpick

import (
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":              First,
		"first":         First,
		"FIRST":         First,
		" round-robin ": RoundRobin,
		"Round-Robin":   RoundRobin,
		"least-used":    LeastUsed,
		"bogus":         First,
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Fatalf("Normalize(%q)=%q want %q", in, got, want)
		}
	}
}

// 轮询：从上次位置向后推进，候选集缩短时不回绕导致重复选中。
func TestRoundRobinNoRewind(t *testing.T) {
	var rr Cursor
	all := []Candidate{{ID: "a", Index: 0}, {ID: "b", Index: 1}, {ID: "c", Index: 2}}

	got := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		c, ok := rr.Pick(all)
		if !ok {
			t.Fatal("应能选中")
		}
		got = append(got, c.ID)
	}
	want := []string{"a", "b", "c", "a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("轮询序列错误：%v want %v", got, want)
		}
	}

	// 第 2 个候选消失后，不应回绕到 a：应从 c 之后找，落到 a（正确）还是 b（错误）
	// 取决于锚定方式。这里显式验证：去掉 b 后，游标停在 c(2)，下一个是 a(0)。
	reduced := []Candidate{{ID: "a", Index: 0}, {ID: "c", Index: 2}}
	c, _ := rr.Pick(reduced)
	if c.ID != "a" {
		t.Fatalf("候选集缩短后应回绕到 a，得到 %s", c.ID)
	}
}

func TestRoundRobinEmpty(t *testing.T) {
	var rr Cursor
	if _, ok := rr.Pick(nil); ok {
		t.Fatal("空候选应返回 false")
	}
}

// Best 取权重最大者，相同取列表序靠前。
func TestBest(t *testing.T) {
	cands := []Candidate{{ID: "a", Index: 0}, {ID: "b", Index: 1}, {ID: "c", Index: 2}}
	weights := map[string]float64{"a": 1, "b": 5, "c": 5}
	c, ok := Best(cands, func(id string) float64 { return weights[id] })
	if !ok || c.ID != "b" {
		t.Fatalf("应取权重最大的 b（相同取靠前），得到 %s/%v", c.ID, ok)
	}
	if _, ok := Best(nil, func(string) float64 { return 0 }); ok {
		t.Fatal("空候选应返回 false")
	}
}

func TestSnapshotTTLAndClaim(t *testing.T) {
	s := NewSnapshot()
	if got := s.Get("a", time.Hour); got != 0 {
		t.Fatalf("未写入应返回 0，得到 %v", got)
	}
	s.Set("a", 42)
	if got := s.Get("a", time.Hour); got != 42 {
		t.Fatalf("应读到 42，得到 %v", got)
	}
	// TTL 为负：视为已失效。
	if got := s.Get("a", -time.Second); got != 0 {
		t.Fatalf("TTL 过期应返回 0，得到 %v", got)
	}

	if !s.ClaimRefresh("a", time.Minute) {
		t.Fatal("首次应可刷新")
	}
	if s.ClaimRefresh("a", time.Minute) {
		t.Fatal("节流窗口内不应再刷新")
	}
	if !s.ClaimRefresh("a", -time.Second) {
		t.Fatal("节流窗口已过应可刷新")
	}
}

func TestSnapshotNilSafe(t *testing.T) {
	var s *Snapshot
	s.Set("a", 1)
	if got := s.Get("a", time.Hour); got != 0 {
		t.Fatalf("nil 快照应返回 0，得到 %v", got)
	}
	if s.ClaimRefresh("a", time.Minute) {
		t.Fatal("nil 快照不应可刷新")
	}
}

func TestThrottle(t *testing.T) {
	var th Throttle
	if !th.Allow(time.Minute) {
		t.Fatal("首次应放行")
	}
	if th.Allow(time.Minute) {
		t.Fatal("节流窗口内不应放行")
	}
	if !th.Allow(-time.Second) {
		t.Fatal("窗口已过应放行")
	}
	var nilTh *Throttle
	if nilTh.Allow(time.Minute) {
		t.Fatal("nil 节流器不应放行")
	}
}
