package cloudflare

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

// ManagedTunnelAPI is the narrow Cloudflare control-plane surface used by the
// server Agent module. Account API tokens never leave the panel.
type ManagedTunnelAPI interface {
	PreflightManagedTunnel(context.Context, string, string, string) (ManagedTunnelPreflight, error)
	CreateManagedTunnel(context.Context, string, string) (ManagedTunnel, error)
	ManagedTunnelExists(context.Context, string, string) (bool, error)
	ConfigureManagedTunnel(context.Context, string, string, []ManagedTunnelIngress) error
	EnsureManagedTunnelDNS(context.Context, string, string, string, string) (ManagedTunnelDNS, error)
	ManagedTunnelToken(context.Context, string, string) (string, error)
	ManagedTunnelConnections(context.Context, string, string) (int, error)
	DeleteManagedTunnelDNS(context.Context, string, string, string) error
	DeleteManagedTunnel(context.Context, string, string) error
}

type ManagedTunnelPreflight struct {
	AccountID   string `json:"account_id"`
	ZoneID      string `json:"zone_id"`
	ZoneName    string `json:"zone_name"`
	Hostname    string `json:"hostname"`
	TunnelWrite bool   `json:"tunnel_write"`
	DNSWrite    bool   `json:"dns_write"`
}

type ManagedTunnel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ManagedTunnelDNS struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type ManagedTunnelIngress struct {
	Hostname string
	Path     string
	Service  string
}

func (s *Service) managedTunnelAuth(ctx context.Context, accountID string) (map[string]string, string, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, "", err
	}
	defer db.Close()
	account, err := loadAccount(ctx, db, strings.TrimSpace(accountID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", errors.New("Cloudflare account not found")
		}
		return nil, "", err
	}
	token := secure.SecureDecrypt(stringValue(account["api_token"], ""))
	if token == "" {
		return nil, "", errors.New("Cloudflare account token is empty")
	}
	auth := cloudflareAuthForAccount(token, account)
	cfAccountID, err := s.cloudflareAccountIDStrict(ctx, auth)
	if err != nil {
		return nil, "", fmt.Errorf("resolve Cloudflare account ID: %w", err)
	}
	return auth, cfAccountID, nil
}

func (s *Service) PreflightManagedTunnel(ctx context.Context, accountID, zoneID, hostname string) (ManagedTunnelPreflight, error) {
	auth, cfAccountID, err := s.managedTunnelAuth(ctx, accountID)
	if err != nil {
		return ManagedTunnelPreflight{}, err
	}
	zoneID, hostname = strings.TrimSpace(zoneID), strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	if zoneID == "" || hostname == "" {
		return ManagedTunnelPreflight{}, errors.New("zone_id and hostname are required")
	}
	zonePayload, err := s.cfRequest(ctx, http.MethodGet, "/zones/"+url.PathEscape(zoneID), auth, nil)
	if err != nil {
		return ManagedTunnelPreflight{}, fmt.Errorf("verify DNS zone access: %w", err)
	}
	zoneName := strings.ToLower(strings.TrimSuffix(stringValue(objectValue(zonePayload["result"])["name"], ""), "."))
	if zoneName == "" || (hostname != zoneName && !strings.HasSuffix(hostname, "."+zoneName)) {
		return ManagedTunnelPreflight{}, fmt.Errorf("hostname %s does not belong to zone %s", hostname, zoneName)
	}
	if _, err := s.cfRequest(ctx, http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel?is_deleted=false&per_page=1", auth, nil); err != nil {
		return ManagedTunnelPreflight{}, fmt.Errorf("verify Cloudflare Tunnel permission: %w", err)
	}
	// Cloudflare has no non-mutating DNS Write probe. Zone access is verified
	// here; the first idempotent DNS upsert is the authoritative write check.
	return ManagedTunnelPreflight{AccountID: cfAccountID, ZoneID: zoneID, ZoneName: zoneName, Hostname: hostname, TunnelWrite: true, DNSWrite: true}, nil
}

func (s *Service) CreateManagedTunnel(ctx context.Context, accountID, name string) (ManagedTunnel, error) {
	auth, cfAccountID, err := s.managedTunnelAuth(ctx, accountID)
	if err != nil {
		return ManagedTunnel{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ManagedTunnel{}, errors.New("tunnel name is required")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return ManagedTunnel{}, fmt.Errorf("generate tunnel secret: %w", err)
	}
	payload, err := s.cfRequest(ctx, http.MethodPost, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel", auth, map[string]interface{}{
		"name": name, "tunnel_secret": base64.StdEncoding.EncodeToString(secret), "config_src": "cloudflare",
	})
	if err != nil {
		return ManagedTunnel{}, fmt.Errorf("create Cloudflare Tunnel: %w", err)
	}
	result := objectValue(payload["result"])
	tunnel := ManagedTunnel{ID: stringValue(result["id"], ""), Name: stringValue(result["name"], name)}
	if tunnel.ID == "" {
		return ManagedTunnel{}, errors.New("Cloudflare returned an empty Tunnel ID")
	}
	return tunnel, nil
}

func (s *Service) ConfigureManagedTunnel(ctx context.Context, accountID, tunnelID string, ingress []ManagedTunnelIngress) error {
	auth, cfAccountID, err := s.managedTunnelAuth(ctx, accountID)
	if err != nil {
		return err
	}
	rules := make([]interface{}, 0, len(ingress)+1)
	for _, item := range ingress {
		if strings.TrimSpace(item.Hostname) == "" || strings.TrimSpace(item.Service) == "" {
			return errors.New("managed Tunnel ingress requires hostname and service")
		}
		rule := map[string]interface{}{"hostname": item.Hostname, "service": item.Service}
		if strings.TrimSpace(item.Path) != "" {
			rule["path"] = item.Path
		}
		rules = append(rules, rule)
	}
	rules = append(rules, map[string]interface{}{"service": "http_status:404"})
	_, err = s.cfRequest(ctx, http.MethodPut, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelID)+"/configurations", auth, map[string]interface{}{"config": map[string]interface{}{"ingress": rules}})
	if err != nil {
		return fmt.Errorf("update Cloudflare Tunnel ingress: %w", err)
	}
	return nil
}

func (s *Service) EnsureManagedTunnelDNS(ctx context.Context, accountID, zoneID, hostname, tunnelID string) (ManagedTunnelDNS, error) {
	auth, _, err := s.managedTunnelAuth(ctx, accountID)
	if err != nil {
		return ManagedTunnelDNS{}, err
	}
	hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	content := strings.TrimSpace(tunnelID) + ".cfargotunnel.com"
	records, _, err := s.listDNSRecords(ctx, auth, zoneID, url.Values{"type": {"CNAME"}, "name": {hostname}, "per_page": {"100"}})
	if err != nil {
		return ManagedTunnelDNS{}, fmt.Errorf("read Tunnel DNS record: %w", err)
	}
	body := map[string]interface{}{"type": "CNAME", "name": hostname, "content": content, "ttl": 1, "proxied": true, "comment": "Managed by API Monitor"}
	var result map[string]interface{}
	if len(records) > 0 {
		existing := objectValue(records[0])
		if err := validateManagedTunnelDNSOwnership(existing, content); err != nil {
			return ManagedTunnelDNS{}, err
		}
		recordID := stringValue(existing["id"], "")
		payload, err := s.cfRequest(ctx, http.MethodPut, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), auth, dnsRecordBody(body, true))
		if err != nil {
			return ManagedTunnelDNS{}, fmt.Errorf("update Tunnel DNS record: %w", err)
		}
		result = objectValue(payload["result"])
	} else {
		result, err = s.createDNSRecord(ctx, auth, zoneID, body)
		if err != nil {
			return ManagedTunnelDNS{}, fmt.Errorf("create Tunnel DNS record: %w", err)
		}
	}
	return ManagedTunnelDNS{ID: stringValue(result["id"], ""), Name: stringValue(result["name"], hostname), Content: stringValue(result["content"], content)}, nil
}

func validateManagedTunnelDNSOwnership(record map[string]interface{}, desiredContent string) error {
	comment := strings.TrimSpace(stringValue(record["comment"], ""))
	content := strings.TrimSpace(stringValue(record["content"], ""))
	if strings.EqualFold(content, strings.TrimSpace(desiredContent)) {
		// The CNAME already points at this managed Tunnel; adopt it even if a
		// legacy record predates the ownership comment marker.
		return nil
	}
	if comment != "Managed by API Monitor" {
		return fmt.Errorf("DNS hostname is already used by a record not owned by API Monitor")
	}
	return fmt.Errorf("DNS hostname is owned by another API Monitor Tunnel")
}

func (s *Service) ManagedTunnelToken(ctx context.Context, accountID, tunnelID string) (string, error) {
	auth, cfAccountID, err := s.managedTunnelAuth(ctx, accountID)
	if err != nil {
		return "", err
	}
	payload, err := s.cfRequest(ctx, http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelID)+"/token", auth, nil)
	if err != nil {
		return "", fmt.Errorf("retrieve scoped Tunnel token: %w", err)
	}
	token := strings.TrimSpace(stringValue(payload["result"], ""))
	if token == "" {
		return "", errors.New("Cloudflare returned an empty Tunnel token")
	}
	return token, nil
}

func (s *Service) ManagedTunnelConnections(ctx context.Context, accountID, tunnelID string) (int, error) {
	auth, cfAccountID, err := s.managedTunnelAuth(ctx, accountID)
	if err != nil {
		return 0, err
	}
	payload, err := s.cfRequest(ctx, http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelID)+"/connections", auth, nil)
	if err != nil {
		return 0, err
	}
	return len(arrayValue(payload["result"])), nil
}

func (s *Service) ManagedTunnelExists(ctx context.Context, accountID, tunnelID string) (bool, error) {
	if strings.TrimSpace(tunnelID) == "" {
		return false, nil
	}
	auth, cfAccountID, err := s.managedTunnelAuth(ctx, accountID)
	if err != nil {
		return false, err
	}
	_, err = s.cfRequest(ctx, http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelID), auth, nil)
	if err == nil {
		return true, nil
	}
	if cfIsResourceMissing(err) {
		return false, nil
	}
	return false, fmt.Errorf("read Cloudflare Tunnel: %w", err)
}

func cfIsResourceMissing(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not found") || strings.Contains(message, "does not exist")
}

func (s *Service) DeleteManagedTunnelDNS(ctx context.Context, accountID, zoneID, recordID string) error {
	if strings.TrimSpace(recordID) == "" {
		return nil
	}
	auth, _, err := s.managedTunnelAuth(ctx, accountID)
	if err != nil {
		return err
	}
	_, err = s.cfRequest(ctx, http.MethodDelete, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), auth, nil)
	if err != nil && !cfIsResourceMissing(err) {
		return fmt.Errorf("delete Tunnel DNS record: %w", err)
	}
	return nil
}

func (s *Service) DeleteManagedTunnel(ctx context.Context, accountID, tunnelID string) error {
	if strings.TrimSpace(tunnelID) == "" {
		return nil
	}
	auth, cfAccountID, err := s.managedTunnelAuth(ctx, accountID)
	if err != nil {
		return err
	}
	_, err = s.cfRequest(ctx, http.MethodDelete, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelID)+"?cascade=true", auth, nil)
	if err != nil && !cfIsResourceMissing(err) {
		return fmt.Errorf("delete Cloudflare Tunnel: %w", err)
	}
	return nil
}
