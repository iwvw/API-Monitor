package subscription

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func (s *Service) servePublicSubscription(w http.ResponseWriter, r *http.Request) {
	token := strings.Trim(strings.TrimPrefix(r.URL.Path, "/sub/"), "/")
	format := r.URL.Query().Get("format")
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	statusCode := http.StatusOK
	success := false
	errMsg := ""
	nodeCount := 0
	var sub Subscription
	var traffic TrafficInfo
	defer func() {
		s.logAccess(r.Context(), db, sub.ID, token, clientIP(r), r.UserAgent(), format, success, statusCode, errMsg, nodeCount, traffic)
	}()

	subs, err := loadSubscriptionByToken(r.Context(), db, token)
	if err != nil || len(subs) == 0 {
		statusCode = http.StatusNotFound
		errMsg = "subscription not found"
		response.Error(w, statusCode, errMsg)
		return
	}
	sub = subs[0]
	if !sub.Enabled || !sub.PlanEnabled {
		statusCode = http.StatusForbidden
		errMsg = "subscription or plan disabled"
		response.Error(w, statusCode, errMsg)
		return
	}
	if sub.RateLimitEnabled && isRateLimited(r.Context(), db, sub.ID, clientIP(r), sub.RateLimitPerMinute) {
		statusCode = http.StatusTooManyRequests
		errMsg = "rate limited"
		response.Error(w, statusCode, errMsg)
		return
	}
	traffic = sub.Traffic
	nodes := []Node{}
	renderBlocked := traffic.Status == "expired" || traffic.Status == "exhausted"
	if !renderBlocked {
		nodes, err = loadPublishedNodesForSubscription(r.Context(), db, sub)
		if err != nil {
			statusCode = http.StatusInternalServerError
			errMsg = "load subscription nodes: " + err.Error()
			response.Error(w, statusCode, errMsg)
			return
		}
	}
	explicitFormat := r.URL.Query().Get("format")
	showInfoPage := explicitFormat == "info" || (explicitFormat == "" && wantsBrowserInfoPage(r))
	if showInfoPage {
		format = "info"
		nodeCount = len(nodes)
		s.serveSubscriptionInfoSPA(w, r)
		success = true
		return
	}
	if format == "" {
		format = subscriptionFormatFromUA(r.UserAgent())
	}
	if format == "" {
		format = templateFormat(r.Context(), db, sub.TemplateID)
	}
	body, contentType, err := renderOutput(r.Context(), db, sub, nodes, format, renderBlocked)
	if err != nil {
		statusCode = http.StatusInternalServerError
		errMsg = err.Error()
		response.Error(w, statusCode, errMsg)
		return
	}
	nodeCount = len(nodes)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": subscriptionOutputFilename(sub)}))
	w.Header().Set("Profile-Title", subscriptionProfileTitle(sub))
	w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", traffic.Upload, traffic.Download, traffic.Total, traffic.Expire))
	w.Header().Set("Profile-Update-Interval", "12")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
	success = true
}

// subscriptionRequestURL reconstructs the absolute subscription URL for the
// current request, honoring the X-Forwarded-Proto set by reverse proxies.
func subscriptionRequestURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded == "https" || forwarded == "http" {
		scheme = forwarded
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + host + r.URL.Path
}

func loadManagedSubscriptionNodes(ctx context.Context, db *sql.DB, sub Subscription) ([]Node, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,server_id,name,protocol,public_host,assigned_port,client_uri_encrypted,COALESCE(access_mode,'direct'),COALESCE(preferred_address_id,''),COALESCE(connect_address,''),COALESCE(connect_port,0),COALESCE(tunnel_hostname,''),COALESCE(stable,0),created_at,updated_at FROM managed_proxy_nodes WHERE enabled=1 AND publishable=1 AND apply_status='running' ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type managedRow struct {
		id, serverID, name, protocol, host, encrypted, accessMode, preferredID, connectAddress, tunnelHostname, createdAt, updatedAt string
		port, connectPort, stable                                                                                                    int
	}
	managedRows := []managedRow{}
	for rows.Next() {
		var item managedRow
		if err := rows.Scan(&item.id, &item.serverID, &item.name, &item.protocol, &item.host, &item.port, &item.encrypted, &item.accessMode, &item.preferredID, &item.connectAddress, &item.connectPort, &item.tunnelHostname, &item.stable, &item.createdAt, &item.updatedAt); err != nil {
			return nil, err
		}
		managedRows = append(managedRows, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, len(managedRows))
	for _, item := range managedRows {
		raw := secure.SecureDecrypt(item.encrypted)
		raw = bindSubscriptionCredential(raw, item.protocol, sub)
		if item.accessMode == "cloudflare_tunnel" {
			address, port := strings.TrimSpace(item.connectAddress), item.connectPort
			if address == "" && item.preferredID != "" {
				_ = db.QueryRowContext(ctx, `SELECT address,port FROM managed_proxy_preferences WHERE id=? AND enabled=1`, item.preferredID).Scan(&address, &port)
			}
			if address == "" {
				_ = db.QueryRowContext(ctx, `SELECT address,port FROM managed_proxy_preferences WHERE enabled=1 AND is_default=1 ORDER BY sort_order ASC,created_at ASC LIMIT 1`).Scan(&address, &port)
			}
			if address == "" {
				address = item.tunnelHostname
			}
			if port == 0 {
				port = 443
			}
			raw = replacePublishedURIAddress(raw, address, port)
		}
		node := parseURI(raw, len(nodes)+1)
		node.ID = item.id
		node.Name = item.name
		node.Server = item.host
		node.Port = item.port
		node.TrafficServerID = item.serverID
		node.Raw = raw
		node.Enabled = true
		// A running managed node is publishable, but that alone is not evidence
		// that it belongs in the operator-curated stable group.
		node.Stable = item.stable == 1
		node.Ownership = "self"
		node.Management = "agent"
		node.TrafficReporting = "trusted"
		node.Source = "internal"
		node.CreatedAt = item.createdAt
		node.UpdatedAt = item.updatedAt
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func bindSubscriptionCredential(raw, protocol string, sub Subscription) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" {
		return raw
	}
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vless-reality", "vless-ws-tunnel", "vless":
		if strings.TrimSpace(sub.VLESSUUID) == "" {
			return raw
		}
		parsed.User = url.User(sub.VLESSUUID)
	case "hysteria2", "hy2":
		if strings.TrimSpace(sub.Hysteria2Password) == "" {
			return raw
		}
		parsed.User = url.User(sub.Hysteria2Password)
	case "socks", "http":
		if strings.TrimSpace(sub.VLESSUUID) == "" || strings.TrimSpace(sub.Hysteria2Password) == "" {
			return raw
		}
		parsed.User = url.UserPassword(sub.VLESSUUID, sub.Hysteria2Password)
	}
	return parsed.String()
}

func replacePublishedURIAddress(raw, address string, port int) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(address) == "" || port < 1 || port > 65535 {
		return raw
	}
	parsed.Host = net.JoinHostPort(strings.TrimSpace(address), strconv.Itoa(port))
	return parsed.String()
}

func ensureUniquePublishedNodeNames(nodes []Node) []Node {
	seen := make(map[string]int, len(nodes))
	for i := range nodes {
		base := strings.TrimSpace(nodes[i].Name)
		if base == "" {
			base = firstNonEmpty(nodes[i].Server, "未命名节点")
		}
		key := strings.ToLower(base)
		seen[key]++
		name := base
		if seen[key] > 1 {
			name = fmt.Sprintf("%s · %d", base, seen[key])
		}
		if nodes[i].Name == name {
			continue
		}
		nodes[i].Name = name
		if parsed, err := url.Parse(nodes[i].Raw); err == nil && parsed.Scheme != "" {
			parsed.Fragment = name
			nodes[i].Raw = parsed.String()
		}
		if strings.TrimSpace(nodes[i].ConfigJSON) != "" {
			var config map[string]interface{}
			if json.Unmarshal([]byte(nodes[i].ConfigJSON), &config) == nil {
				config["name"] = name
				if encoded, err := json.Marshal(config); err == nil {
					nodes[i].ConfigJSON = string(encoded)
				}
			}
		}
	}
	return nodes
}

func subscriptionProfileTitle(sub Subscription) string {
	title := strings.TrimSpace(sub.Name)
	if title == "" {
		title = strings.TrimSpace(sub.ID)
	}
	if title == "" {
		title = "subscription"
	}
	return "base64:" + base64.StdEncoding.EncodeToString([]byte(title))
}

func subscriptionOutputFilename(sub Subscription) string {
	name := strings.TrimSpace(sub.Name)
	if name == "" {
		name = strings.TrimSpace(sub.ID)
	}
	if name == "" {
		name = "subscription"
	}

	replacer := strings.NewReplacer(
		`"`, "",
		`\`, "-",
		"/", "-",
		":", "-",
		"*", "-",
		"?", "",
		"<", "",
		">", "",
		"|", "-",
		"\r", " ",
		"\n", " ",
		"\t", " ",
	)
	name = strings.Join(strings.Fields(replacer.Replace(name)), " ")
	if name == "" {
		name = "subscription"
	}
	return name
}

// wantsBrowserInfoPage decides whether a human opened the subscription URL in
// a browser and should get the readable info page instead of a raw config dump.
// Browsers are identified by a Mozilla-style UA plus an Accept header that asks
// for HTML; proxy clients send neither. Explicit ?format= is decided by the
// caller before this check so a browser can still force a config download.
func wantsBrowserInfoPage(r *http.Request) bool {
	ua := strings.ToLower(r.UserAgent())
	if !strings.Contains(ua, "mozilla") || strings.Contains(ua, "clash") || strings.Contains(ua, "mihomo") || strings.Contains(ua, "sing-box") || strings.Contains(ua, "singbox") || strings.Contains(ua, "v2rayn") || strings.Contains(ua, "nekobox") || strings.Contains(ua, "sfa") || strings.Contains(ua, "sfm") || strings.Contains(ua, "sfi") || strings.Contains(ua, "quantumult") || strings.Contains(ua, "shadowrocket") {
		return false
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	return strings.Contains(accept, "text/html")
}
