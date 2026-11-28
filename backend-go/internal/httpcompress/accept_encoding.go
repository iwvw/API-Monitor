package httpcompress

import (
	"strconv"
	"strings"
)

// AcceptedEncodings 解析 Accept-Encoding,返回 br / gzip 是否可接受。
// 处理 `q=0`(显式拒绝)与 `*` 通配:显式列出的编码优先于通配,
// 避免把客户端用 `br;q=0, *` 明确拒绝的编码发出去。
func AcceptedEncodings(header string) (brOK, gzipOK bool) {
	if strings.TrimSpace(header) == "" {
		return false, false
	}
	var (
		brSet, gzipSet bool
		brQ, gzipQ     bool
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
