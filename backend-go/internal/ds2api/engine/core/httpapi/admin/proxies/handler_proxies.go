package proxies

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	dsclient "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/client"
)

var proxyConnectivityTester = func(ctx context.Context, proxy config.Proxy) map[string]any {
	return dsclient.TestProxyConnectivity(ctx, proxy)
}

func validateProxyMutation(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	if err := config.ValidateProxyConfig(cfg.Proxies); err != nil {
		return err
	}
	return config.ValidateAccountProxyReferences(cfg.Accounts, cfg.Proxies)
}

func proxyResponse(proxy config.Proxy) map[string]any {
	proxy = config.NormalizeProxy(proxy)
	return map[string]any{
		"id":           proxy.ID,
		"name":         proxy.Name,
		"type":         proxy.Type,
		"host":         proxy.Host,
		"port":         proxy.Port,
		"username":     proxy.Username,
		"has_password": strings.TrimSpace(proxy.Password) != "",
	}
}

func (h *Handler) listProxies(w http.ResponseWriter, _ *http.Request) {
	proxies := h.Store.Snapshot().Proxies
	items := make([]map[string]any, 0, len(proxies))
	for _, proxy := range proxies {
		proxy = config.NormalizeProxy(proxy)
		items = append(items, map[string]any{
			"id":           proxy.ID,
			"name":         proxy.Name,
			"type":         proxy.Type,
			"host":         proxy.Host,
			"port":         proxy.Port,
			"username":     proxy.Username,
			"has_password": strings.TrimSpace(proxy.Password) != "",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (h *Handler) addProxy(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	proxy := toProxy(req)
	err := h.Store.Update(func(c *config.Config) error {
		c.Proxies = append(c.Proxies, proxy)
		return validateProxyMutation(c)
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	h.ProxyChanged()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "proxy": proxyResponse(proxy)})
}

// ProxyChanged 在代理新增/变更后刷新运行侧缓存（账号池 + DeepSeek client 代理连接池）。
func (h *Handler) ProxyChanged() {
	if h == nil {
		return
	}
	if h.Pool != nil {
		h.Pool.Reset()
	}
	if h.ResetProxyClients != nil {
		h.ResetProxyClients()
	}
}

func (h *Handler) updateProxy(w http.ResponseWriter, r *http.Request) {
	proxyID := chi.URLParam(r, "proxyID")
	if decoded, err := url.PathUnescape(proxyID); err == nil {
		proxyID = decoded
	}
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	proxy := toProxy(req)
	proxy.ID = strings.TrimSpace(proxyID)

	err := h.Store.Update(func(c *config.Config) error {
		for i, existing := range c.Proxies {
			existing = config.NormalizeProxy(existing)
			if existing.ID != proxy.ID {
				continue
			}
			if proxy.Password == "" {
				proxy.Password = existing.Password
			}
			c.Proxies[i] = proxy
			return validateProxyMutation(c)
		}
		return newRequestError("代理不存在")
	})
	if err != nil {
		if detail, ok := requestErrorDetail(err); ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"detail": detail})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	h.ProxyChanged()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "proxy": proxyResponse(proxy)})
}

func (h *Handler) deleteProxy(w http.ResponseWriter, r *http.Request) {
	proxyID := chi.URLParam(r, "proxyID")
	if decoded, err := url.PathUnescape(proxyID); err == nil {
		proxyID = decoded
	}
	err := h.Store.Update(func(c *config.Config) error {
		idx := -1
		for i, existing := range c.Proxies {
			existing = config.NormalizeProxy(existing)
			if existing.ID == strings.TrimSpace(proxyID) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return newRequestError("代理不存在")
		}
		c.Proxies = append(c.Proxies[:idx], c.Proxies[idx+1:]...)
		for i := range c.Accounts {
			if strings.TrimSpace(c.Accounts[i].ProxyID) == strings.TrimSpace(proxyID) {
				c.Accounts[i].ProxyID = ""
			}
		}
		return validateProxyMutation(c)
	})
	if err != nil {
		if detail, ok := requestErrorDetail(err); ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"detail": detail})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	h.ProxyChanged()
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) testProxy(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	proxyID := fieldString(req, "proxy_id")

	var proxy config.Proxy
	if proxyID != "" {
		var ok bool
		proxy, ok = findProxyByID(h.Store.Snapshot(), proxyID)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"detail": "代理不存在"})
			return
		}
	} else {
		proxy = toProxy(req)
	}

	result := proxyConnectivityTester(r.Context(), proxy)
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) updateAccountProxy(w http.ResponseWriter, r *http.Request) {
	identifier := chi.URLParam(r, "identifier")
	if decoded, err := url.PathUnescape(identifier); err == nil {
		identifier = decoded
	}
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	proxyID := fieldString(req, "proxy_id")

	err := h.Store.Update(func(c *config.Config) error {
		if proxyID != "" {
			if _, ok := findProxyByID(*c, proxyID); !ok {
				return newRequestError("代理不存在")
			}
		}
		for i, acc := range c.Accounts {
			if !accountMatchesIdentifier(acc, identifier) {
				continue
			}
			c.Accounts[i].ProxyID = proxyID
			// 显式选择"不代理"（proxy_id 为空）后，自动调度不再给该账号分配节点。
			c.Accounts[i].NoProxy = strings.TrimSpace(proxyID) == ""
			return validateProxyMutation(c)
		}
		return newRequestError("账号不存在")
	})
	if err != nil {
		if detail, ok := requestErrorDetail(err); ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"detail": detail})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	h.ProxyChanged()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "proxy_id": proxyID})
}
