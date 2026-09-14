package subscription

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func renderOutput(ctx context.Context, db *sql.DB, sub Subscription, nodes []Node, format string, blocked bool) (string, string, error) {
	nodes = preparePublishedNodes(nodes)
	if blocked {
		nodes = nil
	}
	if format == "" || format == "clash" || format == "mihomo" {
		tpl := loadDefaultMihomoTemplate()
		usingCustomTemplate := false
		if sub.TemplateID != "" {
			var customTemplate string
			var builtin int
			_ = db.QueryRowContext(ctx, `SELECT content,builtin FROM subscription_templates WHERE id = ?`, sub.TemplateID).Scan(&customTemplate, &builtin)
			if strings.TrimSpace(customTemplate) != "" {
				tpl = customTemplate
				usingCustomTemplate = builtin == 0
			}
		}
		body := renderTemplate(tpl, sub, nodes)
		if err := validateMihomoOutput(body); err != nil {
			if !usingCustomTemplate {
				// Built-in templates are persisted for stable IDs. An older
				// record can retain removed node names, so fall back to the
				// current embedded template instead of breaking the feed.
				body = renderTemplate(loadDefaultMihomoTemplate(), sub, nodes)
				if fallbackErr := validateMihomoOutput(body); fallbackErr != nil {
					return "", "", fmt.Errorf("内置 Mihomo 模板输出无效，且当前模板回退失败: %w", fallbackErr)
				}
				return body, "text/yaml; charset=utf-8", nil
			}
			body = renderTemplate(loadDefaultMihomoTemplate(), sub, nodes)
			if fallbackErr := validateMihomoOutput(body); fallbackErr != nil {
				return "", "", fmt.Errorf("Mihomo 模板输出无效，且内置模板回退失败: %w", fallbackErr)
			}
		}
		return body, "text/yaml; charset=utf-8", nil
	}
	raw := rawURIList(nodes)
	if format == "base64" {
		return base64.StdEncoding.EncodeToString([]byte(raw)), "text/plain; charset=utf-8", nil
	}
	return raw, "text/plain; charset=utf-8", nil
}

func renderTemplate(tpl string, sub Subscription, nodes []Node) string {
	replacements := map[string]string{
		"{{ subscription.name }}":                  sub.Name,
		"{{ subscription.expire_at }}":             sub.ExpireAt,
		"{{ traffic.upload }}":                     strconv.FormatInt(sub.Traffic.Upload, 10),
		"{{ traffic.download }}":                   strconv.FormatInt(sub.Traffic.Download, 10),
		"{{ traffic.total }}":                      strconv.FormatInt(sub.Traffic.Total, 10),
		"{{ proxies_yaml }}":                       proxiesYAML(nodes, 2),
		"{{ proxy_names_yaml | indent 6 }}":        proxyNamesYAML(nodes, false, 6),
		"{{ stable_proxy_names_yaml | indent 6 }}": stableProxyNamesYAML(nodes, 6),
		"{{ raw_uri_list }}":                       rawURIList(nodes),
	}
	out := tpl
	for key, value := range replacements {
		out = strings.ReplaceAll(out, key, value)
	}
	return out
}

func normalizeTemplateFormat(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "clash", "mihomo", "yaml", "yml":
		return "clash"
	case "raw":
		return "raw"
	case "base64":
		return "base64"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

// subscriptionFormatFromUA maps known proxy client User-Agents to a concrete
// subscription format. Unknown clients return "" so the caller falls back to
// the subscription's default template format (usually Mihomo/Clash YAML).
func subscriptionFormatFromUA(userAgent string) string {
	lower := strings.ToLower(userAgent)
	switch {
	case strings.Contains(lower, "clash"), strings.Contains(lower, "mihomo"), strings.Contains(lower, "stash"):
		return "clash"
	case strings.Contains(lower, "v2rayn"), strings.Contains(lower, "nekobox"), strings.Contains(lower, "quantumult"), strings.Contains(lower, "shadowrocket"):
		return "base64"
	case strings.Contains(lower, "sing-box"), strings.Contains(lower, "singbox"), strings.Contains(lower, "sfi"), strings.Contains(lower, "sfm"), strings.Contains(lower, "sfa"):
		return "base64"
	default:
		return ""
	}
}

func (s *Service) serveSubscriptionInfoSPA(w http.ResponseWriter, r *http.Request) {
	indexPath := filepath.Join(s.cfg.DistDir, "index.html")
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "subscription info page unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Service) servePublicSubscriptionInfo(w http.ResponseWriter, r *http.Request, db *sql.DB, token string) {
	subs, err := loadSubscriptionByToken(r.Context(), db, token)
	if err != nil || len(subs) == 0 {
		response.Error(w, http.StatusNotFound, "订阅不存在")
		return
	}
	sub := subs[0]
	if !sub.Enabled || !sub.PlanEnabled {
		response.Error(w, http.StatusForbidden, "订阅或套餐已停用")
		return
	}
	info := publicSubscriptionInfo{
		ID:          sub.ID,
		Name:        sub.Name,
		Status:      sub.Traffic.Status,
		Upload:      sub.Traffic.Upload,
		Download:    sub.Traffic.Download,
		Total:       sub.Traffic.Total,
		Percent:     sub.Traffic.Percent,
		Expire:      sub.Traffic.Expire,
		CycleStart:  sub.Traffic.CycleStart,
		CycleEnd:    sub.Traffic.CycleEnd,
		PublicToken: sub.PublicToken,
		Formats:     []string{"clash", "base64", "raw", "info"},
	}
	if info.Status == "" {
		info.Status = "active"
	}
	nodes, err := loadPublishedNodesForSubscription(r.Context(), db, sub)
	if err == nil {
		info.NodeCount = len(nodes)
	}
	response.OK(w, info)
}

func templateFormat(ctx context.Context, db *sql.DB, id string) string {
	var format string
	_ = db.QueryRowContext(ctx, `SELECT format FROM subscription_templates WHERE id = ?`, id).Scan(&format)
	return format
}

func loadDefaultMihomoTemplate() string {
	if strings.TrimSpace(defaultMihomoTemplateEmbedded) != "" {
		return defaultMihomoTemplateEmbedded
	}
	for _, candidate := range []string{
		filepath.Join("backend-go", "internal", "subscription", "templates", "default-mihomo.yaml"),
		filepath.Join("internal", "subscription", "templates", "default-mihomo.yaml"),
		filepath.Join("data", "subscription", "default-mihomo.yaml"),
	} {
		if data, err := os.ReadFile(candidate); err == nil {
			return string(data)
		}
	}
	return ""
}

var unresolvedTemplatePattern = regexp.MustCompile(`\{\{[^{}]+\}\}`)
