package main

import (
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// api-monitor-auth-proxy：CF 隧道 token 转发的入口侧鉴权反代。
// cloudflared ingress 指向本代理（127.0.0.1:<listen>），校验通过后反代到本地服务
// （-upstream）。校验来源：Authorization: Bearer、?token= 查询参数、am_token cookie。
// 与 relay 一样由 agent 下载并托管在源主机上。

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "监听地址")
	upstream := flag.String("upstream", "", "上游本地服务，如 http://127.0.0.1:3000")
	token := flag.String("token", "", "访问令牌")
	flag.Parse()

	if *upstream == "" || *token == "" {
		log.Fatalf("auth-proxy: -upstream 与 -token 必填")
	}
	target, err := url.Parse(*upstream)
	if err != nil {
		log.Fatalf("auth-proxy: invalid upstream: %v", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	handler := authHandler(proxy, *token)

	srv := &http.Server{Addr: *listen, Handler: handler}
	log.Printf("api-monitor-auth-proxy listening on %s -> %s", srv.Addr, *upstream)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("auth-proxy: %v", err)
	}
}

func authHandler(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="api-monitor-forward"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authorized(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") && strings.TrimSpace(h[len("Bearer "):]) == token {
		return true
	}
	if q := r.URL.Query().Get("token"); q == token {
		return true
	}
	if c, err := r.Cookie("am_token"); err == nil && c.Value == token {
		return true
	}
	return false
}
