package pow

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"testing"
)

// 测试向量来自直接调用 DeepSeek 官方 WASM。
func TestDeepSeekHashV1(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "e594808bc5b7151ac160c6d39a02e0a8e261ed588578403099e3561dc40c26b3"},
		{"testsalt_1700000000_42", "d4a2ea58c89e40887c933484868380c6f803eaa8dc53a3b9df8e431b921a4f09"},
		{"testsalt_1700000000_100000", "abea2f35796b65486e9be1b36f7878c66cab021e96faa473fdf4decd31f9ba30"},
		{"abc123salt_1700000000_12345", "74b3b7452745b70e85eb32ee7f0a9ec0381d42dd5137b695da915e104fc390e1"},
	} {
		h := DeepSeekHashV1([]byte(tc.in))
		got := hex.EncodeToString(h[:])
		if got != tc.want {
			t.Errorf("hash(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestSolvePow(t *testing.T) {
	for _, tc := range []struct {
		salt   string
		expire int64
		answer int64
		diff   int64
	}{
		{"testsalt", 1700000000, 42, 1000},
		{"testsalt", 1700000000, 500, 2000},
		{"abc123salt", 1700000000, 12345, 20000},
	} {
		h := DeepSeekHashV1([]byte(BuildPrefix(tc.salt, tc.expire) + strconv.FormatInt(tc.answer, 10)))
		got, err := SolvePow(context.Background(), hex.EncodeToString(h[:]), tc.salt, tc.expire, tc.diff)
		if err != nil || got != tc.answer {
			t.Errorf("salt=%q answer=%d: got=%d err=%v", tc.salt, tc.answer, got, err)
		}
	}
}

func TestSolveAndBuildHeader(t *testing.T) {
	t0 := DeepSeekHashV1([]byte("salt_1712345678_777"))
	header, err := SolveAndBuildHeader(context.Background(), &Challenge{
		Algorithm: "DeepSeekHashV1", Challenge: hex.EncodeToString(t0[:]),
		Salt: "salt", ExpireAt: 1712345678, Difficulty: 2000,
		Signature: "sig", TargetPath: "/api/v0/chat/completion",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(header)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if int64(m["answer"].(float64)) != 777 {
		t.Errorf("answer = %v, want 777", m["answer"])
	}
}

func BenchmarkHash(b *testing.B) {
	d := []byte("realisticsalt_1712345678_12345")
	for i := 0; i < b.N; i++ {
		DeepSeekHashV1(d)
	}
}

func BenchmarkSolve(b *testing.B) {
	h := DeepSeekHashV1([]byte("realisticsalt_1712345678_72000"))
	ch := hex.EncodeToString(h[:])
	for i := 0; i < b.N; i++ {
		_, _ = SolvePow(context.Background(), ch, "realisticsalt", 1712345678, 144000)
	}
}
