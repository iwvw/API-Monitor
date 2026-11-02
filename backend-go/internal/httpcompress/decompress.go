package httpcompress

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// DefaultMaxRequestBodyBytes 是未显式配置时「解压后」请求体的上限（32MB）。
// 与 new-api 的 MAX_REQUEST_BODY_MB 默认值一致：既能容纳长上下文对话，
// 又能挡住解压炸弹（压缩比可轻易上千倍）。
const DefaultMaxRequestBodyBytes int64 = 32 << 20

// DecompressRequest 解压带 Content-Encoding 的请求体，让客户端可以把大请求体
// （典型是几十万 tokens 的对话上下文，压缩比常达 5~10 倍）压好再上传，
// 省掉「客户端 → 面板」这一腿的上行流量。
//
// 处理规则：
//   - 支持 gzip / br / zstd，可叠加（`Content-Encoding: gzip, br` 按逆序解压）；
//   - identity 或空值视为不压缩，原样透传；
//   - 不认识的编码直接 415，而不是把压缩字节丢给 JSON 解析器（那会报出
//     误导性的 `invalid character '('`，zstd 帧首字节正是 0x28）；
//   - 解压后按 maxBytes 限流，超限由下游 handler 读 body 时得到
//     *http.MaxBytesError（流式读取，不在中间件里缓冲整个 body）；
//   - 解压成功后删除 Content-Encoding / Content-Length 并把 ContentLength 置 -1，
//     否则下游会按压缩后的长度理解请求体。
//
// maxBytes 是「解压后」请求体的上限。超过时由下游 handler 读 body 时收到
// *http.MaxBytesError；handler 若能自行处理更大体积（如数据库导入），
// 可在中间件之后用 http.MaxBytesReader 重新放宽。传 0 或负数时使用
// DefaultMaxRequestBodyBytes。
//
// skipPaths 命中的路径（精确匹配）完全跳过解压：这类接口要么自己管理
// 上传体积（如 2GB 的数据库导入），要么不支持压缩上传。
//
// 只对可能带 body 的方法生效；GET/HEAD/OPTIONS 与 WebSocket 升级直接跳过。
func DecompressRequest(maxBytes int64, skipPaths ...string) func(http.Handler) http.Handler {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxRequestBodyBytes
	}
	skip := make(map[string]struct{}, len(skipPaths))
	for _, p := range skipPaths {
		if p = strings.TrimSpace(p); p != "" {
			skip[p] = struct{}{}
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !requestCanHaveBody(r) {
				next.ServeHTTP(w, r)
				return
			}
			if len(skip) > 0 {
				if _, ok := skip[r.URL.Path]; ok {
					next.ServeHTTP(w, r)
					return
				}
			}
			encodings := parseContentEncodings(r.Header.Get("Content-Encoding"))
			if len(encodings) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			// 没有 body 却声明了编码：删掉误导性的头，交给下游按空 body 处理。
			if r.Body == nil || r.ContentLength == 0 {
				r.Header.Del("Content-Encoding")
				next.ServeHTTP(w, r)
				return
			}

			body, err := decompressBody(r.Body, encodings)
			if err != nil {
				_ = r.Body.Close()
				if err == errUnsupportedEncoding {
					http.Error(w, "unsupported request Content-Encoding: "+r.Header.Get("Content-Encoding"), http.StatusUnsupportedMediaType)
					return
				}
				http.Error(w, "invalid compressed request body: "+err.Error(), http.StatusBadRequest)
				return
			}

			r.Body = http.MaxBytesReader(w, body, maxBytes)
			r.Header.Del("Content-Encoding")
			r.Header.Del("Content-Length")
			r.ContentLength = -1
			next.ServeHTTP(w, r)
		})
	}
}

// requestCanHaveBody 判断该请求是否值得尝试解压。
// WebSocket 升级请求会 Hijack 连接，改 body 没有意义。
func requestCanHaveBody(r *http.Request) bool {
	if r.Header.Get("Upgrade") != "" {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// parseContentEncodings 把 `gzip, br` 这类列表拆成按应用顺序排列的编码名。
func parseContentEncodings(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" || name == "identity" {
			continue
		}
		out = append(out, name)
	}
	return out
}

// decompressBody 按逆序逐层解开 Content-Encoding（HTTP 规范：后声明的先解）。
func decompressBody(orig io.ReadCloser, encodings []string) (io.ReadCloser, error) {
	var (
		reader   io.Reader = orig
		closeFns           = []func() error{orig.Close}
	)
	for i := len(encodings) - 1; i >= 0; i-- {
		switch encodings[i] {
		case "gzip", "x-gzip":
			gr, err := gzip.NewReader(reader)
			if err != nil {
				return nil, err
			}
			closeFns = append(closeFns, gr.Close)
			reader = gr
		case "br":
			// brotli.Reader 没有 Close，底层 body 由 orig.Close 负责。
			reader = brotli.NewReader(reader)
		case "zstd":
			zr, err := zstd.NewReader(reader)
			if err != nil {
				return nil, err
			}
			rc := zr.IOReadCloser()
			// 注意：zstd.Reader 内部持有解码 goroutine，必须 Close 释放；
			// 即使下游从未读取 body，也要保证 closeFns 被执行（见 decompressedBody.Close）。
			closeFns = append(closeFns, rc.Close)
			reader = rc
		default:
			return nil, errUnsupportedEncoding
		}
	}
	return &decompressedBody{Reader: reader, closeFns: closeFns}, nil
}

// decompressedBody 把解压链的关闭动作合并成一次 Close：先关解码器再关底层。
type decompressedBody struct {
	io.Reader
	closeFns []func() error
}

func (b *decompressedBody) Close() error {
	var firstErr error
	for _, fn := range b.closeFns {
		if err := fn(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// errUnsupportedEncoding 标记客户端声明了本中间件不支持的 Content-Encoding。
var errUnsupportedEncoding = &unsupportedEncodingError{}

type unsupportedEncodingError struct{}

func (*unsupportedEncodingError) Error() string { return "unsupported content encoding" }
