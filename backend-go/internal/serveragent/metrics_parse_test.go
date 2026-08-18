package serveragent

import (
	"encoding/json"
	"testing"
)

func TestParseByteValueUnits(t *testing.T) {
	kb := 1024.0
	mb := 1024 * kb
	gb := 1024 * mb
	tb := 1024 * gb
	cases := []struct {
		name  string
		value interface{}
		want  float64
	}{
		{"plain number", 512, 512},
		{"float64", 512.5, 512.5},
		{"negative float", -128.25, -128.25},
		{"zero", 0, 0},
		{"json number", json.Number("2048"), 2048},
		{"lowercase kb", "128KB", 128 * kb},
		{"lowercase mb string", "1MB", mb},
		{"uppercase gb", "2GB", 2 * gb},
		{"mixed case t", "1Tb", tb},
		{"decimal", "2.5GB", 2.5 * gb},
		{"thousand separator", "1,024 KB", 1024 * kb},
		{"surrounding whitespace", "  4 MB  ", 4 * mb},
		{"unitless string", "512", 512},
		{"negative string", "-64MB", -64 * mb},
		{"zero with unit", "0KB", 0},
		{"unknown suffix", "1024 ZiB", 1024},
	}
	for _, tc := range cases {
		if got := parseByteValue(tc.value); got != tc.want {
			t.Fatalf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseByteValueTrimsCommasInSuffixLookup(t *testing.T) {
	if got := parseByteValue(" 2,048 KB "); got != 2*1024*1024 {
		t.Fatalf("comma separated value = %v", got)
	}
}

func TestParseByteValueInvalidInputs(t *testing.T) {
	cases := []interface{}{
		nil,
		true,
		struct{}{},
		"",
		"  ",
		"abc",
		"KB",
		"-MB",
		[]string{"1GB"},
	}
	for _, value := range cases {
		if got := parseByteValue(value); got != 0 {
			t.Fatalf("parseByteValue(%#v) = %v, want 0", value, got)
		}
	}
}

func TestParsePairBytes(t *testing.T) {
	cases := []struct {
		name      string
		value     string
		wantUsed  int
		wantTotal int
	}{
		{"plain pair", "123/456", 123, 456},
		{"suffixed pair", "1GB/2GB", 1, 2},
		{"decimal pair", "1.5/3", 1, 3},
		{"leading slash", "/5", 0, 5},
		{"trailing slash", "5/", 5, 0},
		{"extra slash", "5/3/2", 5, 3},
		{"no slash", "1024", 0, 0},
		{"empty", "", 0, 0},
		{"whitespace pair", " 10 / 20 ", 10, 20},
		{"garbage", "abc/def", 0, 0},
	}
	for _, tc := range cases {
		used, total := parsePairBytes(tc.value)
		if used != tc.wantUsed || total != tc.wantTotal {
			t.Fatalf("%s = (%d, %d), want (%d, %d)", tc.name, used, total, tc.wantUsed, tc.wantTotal)
		}
	}
}

func TestParseMetricNumberWithCommasAndSigns(t *testing.T) {
	cases := []struct {
		value interface{}
		want  float64
	}{
		{"12,345", 12345},
		{"-7.5%", -7.5},
		{"herp 42 derp", 42},
		{int32(9), 9},
		{int64(10), 10},
		{uint(11), 0},
	}
	for _, tc := range cases {
		if got := parseMetricNumber(tc.value); got != tc.want {
			t.Fatalf("parseMetricNumber(%#v) = %v, want %v", tc.value, got, tc.want)
		}
	}
}
