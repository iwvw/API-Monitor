package workbuddy

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"io"
	"strings"
	"testing"
)

// 压缩后的字节必须能被标准 gzip 解压回原文：这是「内容零丢失」的基本保证。
func TestEncodeUpstreamBodyRoundTrip(t *testing.T) {
	body := []byte(strings.Repeat("请分析这段代码的边界条件。func main() { println(\"hi\") }\n", 200))
	out, compressed := encodeUpstreamBody(body, true)
	if !compressed {
		t.Fatalf("超过阈值的文本应被压缩（原始 %d 字节）", len(body))
	}
	if len(out) >= len(body) {
		t.Fatalf("压缩后应更小：%d -> %d", len(body), len(out))
	}
	zr, err := gzip.NewReader(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("压缩产物不是合法 gzip: %v", err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("解压内容与原文不一致（原 %d 字节，解出 %d 字节）", len(body), len(got))
	}
}

// 开关关闭时必须原样发明文，且不设置 Content-Encoding。
func TestEncodeUpstreamBodyDisabled(t *testing.T) {
	body := []byte(strings.Repeat("x", 8192))
	out, compressed := encodeUpstreamBody(body, false)
	if compressed {
		t.Fatal("开关关闭时不应压缩")
	}
	if !bytes.Equal(out, body) {
		t.Fatal("开关关闭时应原样返回明文")
	}
}

// 小于阈值的请求体不压缩：省不了字节，还多一次 CPU 往返。
func TestEncodeUpstreamBodyBelowThreshold(t *testing.T) {
	body := []byte(`{"model":"hy3","messages":[]}`)
	out, compressed := encodeUpstreamBody(body, true)
	if compressed {
		t.Fatalf("%d 字节低于阈值，不应压缩", len(body))
	}
	if !bytes.Equal(out, body) {
		t.Fatal("未压缩时应原样返回")
	}
}

// 高熵内容（压缩后反而变大）必须退回明文，避免负收益。
func TestEncodeUpstreamBodySkipsWhenNotSmaller(t *testing.T) {
	// 密码学随机字节不可压缩：gzip 只会有头尾开销，压后必然更大。
	inner := make([]byte, 8192)
	if _, err := rand.Read(inner); err != nil {
		t.Fatalf("生成随机数据失败: %v", err)
	}
	out, compressed := encodeUpstreamBody(inner, true)
	if compressed {
		t.Fatalf("压缩未见收益时不应发压缩体（原 %d，压后 %d）", len(inner), len(out))
	}
	if !bytes.Equal(out, inner) {
		t.Fatal("退回明文时应原样返回原始字节")
	}
}

// newUpstreamRequest 必须让 Content-Length 等于实际发送的（压缩后）长度，
// 否则上游按旧长度读取会截断成 invalid character。
func TestNewUpstreamRequestSetsCompressedLength(t *testing.T) {
	body := []byte(strings.Repeat("alpha beta gamma delta. ", 2000))
	req, err := newUpstreamRequest(t.Context(), "https://example.invalid/v2/chat/completions", body, true)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	if got := req.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("应设置 Content-Encoding: gzip，得到 %q", got)
	}
	if req.ContentLength <= 0 || req.ContentLength >= int64(len(body)) {
		t.Fatalf("Content-Length 应等于压缩后长度且小于原文：%d vs 原文 %d", req.ContentLength, len(body))
	}
	sent, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("读请求体失败: %v", err)
	}
	if int64(len(sent)) != req.ContentLength {
		t.Fatalf("实际字节数 %d 与 Content-Length %d 不一致", len(sent), req.ContentLength)
	}
	zr, err := gzip.NewReader(bytes.NewReader(sent))
	if err != nil {
		t.Fatalf("发送体不是合法 gzip: %v", err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("发送的压缩体解压后与原文不一致")
	}
}

// 关闭压缩时不得出现 Content-Encoding，且长度为原文长度。
func TestNewUpstreamRequestPlain(t *testing.T) {
	body := []byte(strings.Repeat("alpha beta. ", 2000))
	req, err := newUpstreamRequest(t.Context(), "https://example.invalid/v2/chat/completions", body, false)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	if got := req.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("未压缩时不应设置 Content-Encoding，得到 %q", got)
	}
	if req.ContentLength != int64(len(body)) {
		t.Fatalf("Content-Length 应为原文长度 %d，得到 %d", len(body), req.ContentLength)
	}
}
