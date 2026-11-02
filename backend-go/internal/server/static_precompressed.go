package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// 构建期预压缩静态资源（gzip / brotli）的运行期发送端。
//
// 背景：静态资源原先由 httpcompress.Middleware 每个请求实时 gzip（BestSpeed），
// 486KB 的 CSS 从 50 并发就打满 2 核。改为构建期压缩一次、运行期直接发 `.br` / `.gz`。
// 构建期产物由 tools/precompress-dist.mjs 生成。
//
// 这里只负责「优先发预压缩文件，没有就退回原文件」，不负责生成产物：
// 开发环境没有预压缩步骤时自动退回原路径，行为与改造前一致（仍由中间件实时 gzip）。
var precompressedExtensions = map[string]struct{}{
	".js":          {},
	".mjs":         {},
	".css":         {},
	".html":        {},
	".svg":         {},
	".json":        {},
	".webmanifest": {},
	".map":         {},
	".txt":         {},
	".xml":         {},
	".wasm":        {},
}

// servePrecompressed 尝试发送 candidate 的预压缩变体。命中返回 true，
// 调用方不再继续处理响应。
//
// 注意 Content-Type：必须按原始文件扩展名推断，不能按 `.br` / `.gz` 推断，
// 所以这里把原始文件名交给 http.ServeContent，它会按 name 的扩展名设置 MIME。
func servePrecompressed(w http.ResponseWriter, r *http.Request, candidate string) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	// 带 Range 的请求交给原始文件处理：http.ServeContent 在 Content-Encoding 非空时
	// 不会自行设置 Content-Length，而我们希望预压缩路径带上准确的 Content-Length，
	// 两者无法同时满足。静态资源里只有 .wasm 可能被分片下载，回退代价可忽略。
	if r.Header.Get("Range") != "" {
		return false
	}
	if _, ok := precompressedExtensions[strings.ToLower(filepath.Ext(candidate))]; !ok {
		return false
	}

	encoding, variant := negotiatePrecompressed(r.Header.Get("Accept-Encoding"), candidate)
	if encoding == "" {
		return false
	}

	file, err := os.Open(variant)
	if err != nil {
		// 预压缩产物缺失（开发环境、或构建时跳过了该文件）→ 退回原始文件。
		return false
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return false
	}

	// 必须在写入前设置：httpcompress.Middleware 以 Content-Encoding 非空判定
	// 「已编码」并跳过二次压缩，这条路径正是靠它避免重复压缩。
	w.Header().Set("Content-Encoding", encoding)
	w.Header().Set("Vary", "Accept-Encoding")
	// ServeContent 只在 Content-Encoding 为空时自动补 Content-Length，这里自己补，
	// 让客户端能拿到真实传输体积（也避免 chunked 编码）。
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	// Content-Type 必须按原始文件扩展名推断，不能按 .br / .gz 推断，
	// 所以 name 传原始文件名而不是 variant。
	http.ServeContent(w, r, candidate, info.ModTime(), file)
	return true
}

// negotiatePrecompressed 按 Accept-Encoding 选变体，返回编码名与对应文件路径。
// brotli 优先（同内容体积更小），其次 gzip；都不接受返回空。
func negotiatePrecompressed(acceptEncoding, candidate string) (string, string) {
	brOK, gzipOK := acceptedEncodings(acceptEncoding)
	switch {
	case brOK:
		return "br", candidate + ".br"
	case gzipOK:
		return "gzip", candidate + ".gz"
	default:
		return "", ""
	}
}

// acceptedEncodings 解析 Accept-Encoding，返回 br / gzip 是否可接受。
// 处理 `q=0`（显式拒绝）与 `*` 通配：显式列出的编码优先于通配，
// 避免把客户端用 `br;q=0, *` 明确拒绝的编码发出去。
func acceptedEncodings(header string) (brOK, gzipOK bool) {
	if strings.TrimSpace(header) == "" {
		return false, false
	}
	var (
		brSet, gzipSet bool // 该编码是否被显式列出（含 q=0）
		brQ, gzipQ     bool // 显式列出的编码是否被接受
		wildcardQ      bool
	)
	for _, part := range strings.Split(header, ",") {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}
		name := token
		quality := 1.0
		if idx := strings.IndexByte(token, ';'); idx >= 0 {
			name = strings.TrimSpace(token[:idx])
			params := token[idx+1:]
			for _, param := range strings.Split(params, ";") {
				param = strings.TrimSpace(param)
				if !strings.HasPrefix(param, "q=") {
					continue
				}
				if q, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(param, "q=")), 64); err == nil {
					quality = q
				}
			}
		}
		accepted := quality > 0
		switch strings.ToLower(name) {
		case "br":
			brSet, brQ = true, accepted
		case "gzip", "x-gzip":
			gzipSet, gzipQ = true, accepted
		case "*":
			wildcardQ = accepted
		}
	}
	if brSet {
		brOK = brQ
	} else {
		brOK = wildcardQ
	}
	if gzipSet {
		gzipOK = gzipQ
	} else {
		gzipOK = wildcardQ
	}
	return brOK, gzipOK
}
