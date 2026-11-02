package mihomo

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

// bridgeOrFail 统一处理 Bridge 未装配的场景（如部分测试构造），避免空指针。
func (h *Handler) bridgeOrFail(w http.ResponseWriter) Bridge {
	if h == nil || h.Bridge == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "Mihomo 代理桥未装配"})
		return nil
	}
	return h.Bridge
}

func (h *Handler) getStatus(w http.ResponseWriter, _ *http.Request) {
	b := h.bridgeOrFail(w)
	if b == nil {
		return
	}
	writeJSON(w, http.StatusOK, b.Status())
}

func (h *Handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	b := h.bridgeOrFail(w)
	if b == nil {
		return
	}
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	enabled := false
	if v, ok := req["enabled"].(bool); ok {
		enabled = v
	}
	err := b.UpdateSettings(r.Context(), enabled, fieldString(req, "binary_path"), fieldInt(req, "base_port"), fieldInt(req, "api_port"), fieldBool(req, "auto_bind"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "status": b.Status()})
}

func (h *Handler) applyNow(w http.ResponseWriter, r *http.Request) {
	b := h.bridgeOrFail(w)
	if b == nil {
		return
	}
	if err := b.Apply(r.Context()); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "status": b.Status()})
}

// getBinary 返回二进制探测结果与下载任务状态（前端据此决定“下载mihomo”按钮可用性）。
func (h *Handler) getBinary(w http.ResponseWriter, r *http.Request) {
	b := h.bridgeOrFail(w)
	if b == nil {
		return
	}
	writeJSON(w, http.StatusOK, b.DownloadInfo())
}

// downloadBinary 触发后台下载 mihomo 内核到根目录，返回后前端轮询进度。
func (h *Handler) downloadBinary(w http.ResponseWriter, r *http.Request) {
	b := h.bridgeOrFail(w)
	if b == nil {
		return
	}
	if err := b.StartBinaryDownload(r.Context()); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "info": b.DownloadInfo()})
}

func (h *Handler) listSubscriptions(w http.ResponseWriter, _ *http.Request) {
	snap := h.Store.Snapshot()
	items := make([]map[string]any, 0, len(snap.Mihomo.Subscriptions))
	for _, sub := range snap.Mihomo.Subscriptions {
		items = append(items, map[string]any{
			"id":         sub.ID,
			"name":       sub.Name,
			"url":        sub.URL,
			"updated_at": sub.UpdatedAt,
			"node_count": len(sub.Nodes),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (h *Handler) addSubscription(w http.ResponseWriter, r *http.Request) {
	b := h.bridgeOrFail(w)
	if b == nil {
		return
	}
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	sub, err := b.AddSubscription(r.Context(), fieldString(req, "name"), fieldString(req, "url"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"subscription": map[string]any{
			"id":         sub.ID,
			"name":       sub.Name,
			"url":        sub.URL,
			"updated_at": sub.UpdatedAt,
			"node_count": len(sub.Nodes),
		},
	})
}

func (h *Handler) refreshSubscription(w http.ResponseWriter, r *http.Request) {
	b := h.bridgeOrFail(w)
	if b == nil {
		return
	}
	subID := urlParam(r, "subID")
	count, err := b.RefreshSubscription(r.Context(), subID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "node_count": count})
}

func (h *Handler) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	b := h.bridgeOrFail(w)
	if b == nil {
		return
	}
	subID := urlParam(r, "subID")
	if err := b.DeleteSubscription(r.Context(), subID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) listNodes(w http.ResponseWriter, _ *http.Request) {
	b := h.bridgeOrFail(w)
	if b == nil {
		return
	}
	nodes := b.ListNodes()
	writeJSON(w, http.StatusOK, map[string]any{"items": nodes, "total": len(nodes)})
}

// testLatency 批量测试全部订阅节点延迟（每批最多 60 并发），返回已按延迟排序的结果。
func (h *Handler) testLatency(w http.ResponseWriter, r *http.Request) {
	b := h.bridgeOrFail(w)
	if b == nil {
		return
	}
	items, err := b.TestLatency(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

// assignAccounts 一键为全部账号分配节点绑定。body: {"node_keys": ["<nodeKey>", ...]}，
// node_keys 为按顺序排列的可分配节点（已测延迟时前端只传测试成功的节点）。
func (h *Handler) assignAccounts(w http.ResponseWriter, r *http.Request) {
	b := h.bridgeOrFail(w)
	if b == nil {
		return
	}
	var req struct {
		NodeKeys []string `json:"node_keys"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	bound, err := b.AssignAccounts(r.Context(), req.NodeKeys)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "bound": bound})
}

// bindAccount 处理 PUT /mihomo/bindings/{identifier}，body: {"node": "<nodeKey>"}。
// node 为空字符串表示解绑（账号回落到直连/手动代理）。
func (h *Handler) bindAccount(w http.ResponseWriter, r *http.Request) {
	b := h.bridgeOrFail(w)
	if b == nil {
		return
	}
	identifier := urlParam(r, "identifier")
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	nodeKey := strings.TrimSpace(fieldString(req, "node"))
	if err := b.BindAccount(r.Context(), identifier, nodeKey); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "node": nodeKey})
}

func urlParam(r *http.Request, name string) string {
	v := chi.URLParam(r, name)
	if decoded, err := url.PathUnescape(v); err == nil {
		return decoded
	}
	return v
}
