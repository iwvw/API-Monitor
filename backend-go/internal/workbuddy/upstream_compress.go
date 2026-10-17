package workbuddy

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
)

// compressMinBytes 是启用压缩的最小请求体阈值（4 KiB）。
// 小请求压缩后往往更大（gzip 头尾固定开销约 20B，且要建字典），多一次
// CPU 往返没有收益；真实对话请求体普遍在几十 KB ~ 数 MB，远高于此阈值。
const compressMinBytes = 4 << 10

// encodeUpstreamBody 按上游实测行为对请求体做 gzip 压缩。
// 返回实际应发送的字节与是否压缩。
//
// 为什么可以这样做（实测依据，勿凭猜改）：
//   - 上游 copilot.tencent.com 对 Content-Encoding: gzip 的请求体**不报错也不
//     静默丢弃**：用 49,829B 长文本藏一个随机码发送，压缩到 475B 后模型仍能
//     正确复述该码，证明上游确实解压并读到了完整内容；
//   - deflate 不被接受（HTTP 400 / code 11101：JSON 解析器直接看到压缩字节），
//     所以只能用 gzip；
//   - 上游读的是解压后的 JSON。Content-Length 必须按压缩后的长度重算，
//     否则会被截断成 invalid character；
//   - 压缩收益极大：代码/日志/JSON 混合对话实测 10~37 倍，而 WorkBuddy 上行
//     占本站总上行九成以上，这是最直接的降流量手段。
//
// enabled=false（设置页开关关闭）或体积过小时原样返回明文。
func encodeUpstreamBody(body []byte, enabled bool) ([]byte, bool) {
	if !enabled || len(body) < compressMinBytes {
		return body, false
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return body, false // BestSpeed 是合法常量，理论上不会失败；失败退明文更安全
	}
	if _, err := zw.Write(body); err != nil {
		return body, false
	}
	if err := zw.Close(); err != nil {
		return body, false
	}
	// 压缩反而变大（已压缩内容 / 极高熵）：发明文，免得多付字节与 CPU。
	if buf.Len() >= len(body) {
		return body, false
	}
	return buf.Bytes(), true
}

// newUpstreamRequest 构造发往上游的 POST 请求；body 需要压缩时自动施加
// Content-Encoding: gzip 并以压缩后长度作为 Content-Length。
func newUpstreamRequest(ctx context.Context, url string, body []byte, compress bool) (*http.Request, error) {
	send, compressed := encodeUpstreamBody(body, compress)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(send))
	if err != nil {
		return nil, err
	}
	if compressed {
		req.Header.Set("Content-Encoding", "gzip")
	}
	req.ContentLength = int64(len(send))
	return req, nil
}
