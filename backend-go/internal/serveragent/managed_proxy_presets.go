package serveragent

import (
	"database/sql"
	"net/http"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// Managed proxy presets are the reviewed, one-click tunings for a sing-box
// inbound. They exist so operators (and the MCP API) do not have to hand-edit
// the generated JSON to switch between the trade-offs we already validated:
//
//   - balanced:    defaults for both protocols (REALITY 1m clock tolerance,
//     Hysteria2 BBR + bing masquerade).
//   - low_latency: REALITY tightens clock tolerance for well-synced clients;
//     Hysteria2 keeps BBR and disables the masquerade page.
//   - stealth:     REALITY moves the handshake to a heavily-fronted site;
//     Hysteria2 masquerades as an innocuous static site.
//   - throughput:  Hysteria2 keeps BBR but advertises a large bandwidth hint;
//     REALITY stays on the balanced tuning.
//
// Presets only adjust parameters the panel already owns. They never change
// ports, protocols, credentials or the operator's REALITY handshake domain.
type managedProxyPreset struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Protocols   []string               `json:"protocols"`
	Options     map[string]interface{} `json:"options"`
}

// managedProxyPresetCatalog is the single source of truth shared by the panel
// UI and the MCP surface (GET /api/server/agent/proxy/presets).
var managedProxyPresetCatalog = []managedProxyPreset{
	{
		ID:          "balanced",
		Name:        "均衡（默认）",
		Description: "REALITY 容忍 1 分钟时钟偏差；Hysteria2 走 BBR 并伪装成普通站点。适合绝大多数主机。",
		Protocols:   []string{"vless-reality", "hysteria2"},
		Options: map[string]interface{}{
			"reality_max_time_difference":       "1m",
			"hysteria2_ignore_client_bandwidth": true,
			"hysteria2_masquerade":              "https://www.bing.com",
		},
	},
	{
		ID:          "low_latency",
		Name:        "低延迟",
		Description: "REALITY 收紧时钟容差（10s），客户端需校时准确；Hysteria2 关闭伪装页以减少握手开销。",
		Protocols:   []string{"vless-reality", "hysteria2"},
		Options: map[string]interface{}{
			"reality_max_time_difference":       "10s",
			"hysteria2_ignore_client_bandwidth": true,
			"hysteria2_masquerade":              "",
		},
	},
	{
		ID:          "stealth",
		Name:        "隐蔽优先",
		Description: "REALITY 握手落到微软主站；Hysteria2 伪装为静态站点，降低被主动探测识别的概率。",
		Protocols:   []string{"vless-reality", "hysteria2"},
		Options: map[string]interface{}{
			"reality_server_name":               "www.microsoft.com",
			"reality_max_time_difference":       "1m",
			"hysteria2_ignore_client_bandwidth": true,
			"hysteria2_masquerade":              "https://www.bing.com",
		},
	},
	{
		ID:          "throughput",
		Name:        "吞吐优先",
		Description: "Hysteria2 保留 BBR 但放开带宽提示，适合高带宽独享主机；REALITY 保持均衡参数。",
		Protocols:   []string{"vless-reality", "hysteria2"},
		Options: map[string]interface{}{
			"reality_max_time_difference":       "1m",
			"hysteria2_ignore_client_bandwidth": true,
			"hysteria2_masquerade":              "https://www.bing.com",
			"hysteria2_bandwidth_hint":          "200 mbps",
		},
	},
}

// managedProxyPresetByID resolves a preset id, falling back to balanced so an
// unknown/blank id never breaks node creation.
func managedProxyPresetByID(id string) managedProxyPreset {
	for _, preset := range managedProxyPresetCatalog {
		if preset.ID == id {
			return preset
		}
	}
	return managedProxyPresetCatalog[0]
}

// listManagedProxyPresets serves the preset catalog to the panel and MCP.
func (s *Service) listManagedProxyPresets(w http.ResponseWriter, r *http.Request, _ *sql.DB) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	response.OK(w, map[string]interface{}{"presets": managedProxyPresetCatalog, "default": managedProxyPresetCatalog[0].ID})
}

// presetString/presetBool/presetStringOr/presetBoolOr read optional preset
// options. The *Or variants fall back to the balanced default.
func presetString(options map[string]interface{}, key string) (string, bool) {
	value, ok := options[key].(string)
	return value, ok
}

func presetBool(options map[string]interface{}, key string) (bool, bool) {
	value, ok := options[key].(bool)
	return value, ok
}

func presetStringOr(options map[string]interface{}, key, fallback string) string {
	if value, ok := presetString(options, key); ok {
		return value
	}
	return fallback
}

func presetBoolOr(options map[string]interface{}, key string, fallback bool) bool {
	if value, ok := presetBool(options, key); ok {
		return value
	}
	return fallback
}
