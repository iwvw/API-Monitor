package origin

import (
	"net"
	"net/url"
	"strings"
)

// IsLoopbackClient 判断请求来源是否为回环地址。
func IsLoopbackClient(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil || host == "" {
		host = strings.TrimSpace(remoteAddr)
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// IsDevelopmentOriginHost 判断 Origin 主机是否为开发环境豁免
// （localhost、host.docker.internal、回环或内网地址）。
func IsDevelopmentOriginHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") || strings.EqualFold(host, "host.docker.internal") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

// IsEmbeddedWrapperOrigin 判断 Origin 是否为本机包装环境来源
// （Origin: null、自定义 scheme 如 app://、无主机名等）。这类来源只可能由
// 用户自己的嵌入容器产生，公网站点无法伪造，放行不构成 CSRF 风险。
func IsEmbeddedWrapperOrigin(origin string) bool {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return true
	}
	host := parsed.Hostname()
	return host == "" || strings.EqualFold(host, "null")
}

// AllowedByConfig 判断 Origin 是否命中 CORS_ALLOWED_ORIGINS 白名单（忽略尾部斜杠）。
func AllowedByConfig(allowedOrigins []string, origin string) bool {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	for _, allowed := range allowedOrigins {
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(allowed), "/"), origin) {
			return true
		}
	}
	return false
}
