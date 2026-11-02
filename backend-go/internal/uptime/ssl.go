package uptime

import (
	"crypto/tls"
	"database/sql"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) monitorSSL(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitor, found, err := loadMonitor(r.Context(), db, id)
	if err != nil || !found {
		response.Error(w, http.StatusNotFound, "monitor not found")
		return
	}
	monURL := stringValue(monitor["url"], "")
	if monURL == "" {
		response.JSON(w, http.StatusOK, map[string]interface{}{"ssl": false, "reason": "no URL configured"})
		return
	}
	if !strings.HasPrefix(monURL, "https://") {
		response.JSON(w, http.StatusOK, map[string]interface{}{"ssl": false, "reason": "not HTTPS"})
		return
	}
	// Parse hostname from URL for TLS dial
	host := strings.TrimPrefix(monURL, "https://")
	if idx := strings.Index(host, "/"); idx > 0 {
		host = host[:idx]
	}
	if !strings.Contains(host, ":") {
		host = host + ":443"
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", host, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"ssl": false, "error": err.Error()})
		return
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{"ssl": false, "reason": "no certificates"})
		return
	}
	leaf := certs[0]
	daysLeft := int(time.Until(leaf.NotAfter).Hours() / 24)
	chain := []map[string]interface{}{}
	for _, cert := range certs {
		chain = append(chain, map[string]interface{}{
			"subject":   cert.Subject.CommonName,
			"issuer":    cert.Issuer.CommonName,
			"notBefore": cert.NotBefore.UTC().Format(time.RFC3339),
			"notAfter":  cert.NotAfter.UTC().Format(time.RFC3339),
			"isCA":      cert.IsCA,
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"ssl":       true,
		"subject":   leaf.Subject.CommonName,
		"issuer":    leaf.Issuer.CommonName,
		"notBefore": leaf.NotBefore.UTC().Format(time.RFC3339),
		"notAfter":  leaf.NotAfter.UTC().Format(time.RFC3339),
		"daysLeft":  daysLeft,
		"dnsNames":  leaf.DNSNames,
		"chain":     chain,
	})
}

func (s *Service) sslStatus(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	rows, err := db.QueryContext(r.Context(), `
		SELECT m.id, m.name, m.url, m.active, s.ssl_expiry
		FROM uptime_monitors m
		LEFT JOIN uptime_monitor_states s ON s.monitor_id = m.id
		WHERE COALESCE(m.url, '') LIKE 'https://%'
		ORDER BY m.name ASC
	`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	now := time.Now()
	for rows.Next() {
		var id int64
		var name, url string
		var active int
		var expiry sql.NullString
		if err := rows.Scan(&id, &name, &url, &active, &expiry); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		item := map[string]interface{}{"monitorId": id, "name": name, "url": url, "active": active != 0, "sslExpiry": nil, "daysLeft": nil, "status": "unknown"}
		if expiry.Valid && strings.TrimSpace(expiry.String) != "" {
			if parsed, err := parseUptimeDBTime(expiry.String); err == nil {
				days := int(parsed.Sub(now).Hours() / 24)
				item["sslExpiry"] = parsed.UTC().Format(time.RFC3339)
				item["daysLeft"] = days
				switch {
				case days < 0:
					item["status"] = "expired"
				case days <= 7:
					item["status"] = "critical"
				case days <= 15:
					item["status"] = "warning"
				default:
					item["status"] = "ok"
				}
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, items)
}
