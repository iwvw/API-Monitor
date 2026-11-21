package totp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

type customBrandIconRecord struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Issuer      string `json:"issuer,omitempty"`
	Color       string `json:"color,omitempty"`
	Ext         string `json:"ext"`
	ContentType string `json:"content_type"`
	CreatedAt   string `json:"created_at"`
}

type remoteBrandIconAsset struct {
	Data        []byte
	FileName    string
	ContentType string
}

type remoteBrandIconFetcher func(context.Context, string) (remoteBrandIconAsset, error)

var blockedRemoteBrandIconNetworks = mustParseCIDRs(
	"100.64.0.0/10",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"240.0.0.0/4",
	"2001:db8::/32",
)

var syntheticProxyBrandIconNetwork = mustParseCIDR("198.18.0.0/15")

func (s *Service) listCustomBrandIcons(w http.ResponseWriter, r *http.Request) {
	records, err := s.loadCustomBrandIconRecords()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt == records[j].CreatedAt {
			return records[i].Name < records[j].Name
		}
		return records[i].CreatedAt > records[j].CreatedAt
	})
	items := make([]map[string]string, 0, len(records))
	for _, item := range records {
		items = append(items, map[string]string{
			"id":         item.ID,
			"icon":       "custom:" + item.ID,
			"name":       item.Name,
			"issuer":     item.Issuer,
			"color":      item.Color,
			"source":     "custom",
			"license":    "User Upload",
			"url":        "/api/totp/icons/custom-" + item.ID,
			"created_at": item.CreatedAt,
		})
	}
	response.OK(w, items)
}

func (s *Service) uploadCustomBrandIcon(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 768*1024)
	if err := r.ParseMultipartForm(768 * 1024); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid upload payload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "missing upload file")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 600*1024))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "read upload failed")
		return
	}
	if len(data) == 0 {
		response.Error(w, http.StatusBadRequest, "empty upload file")
		return
	}

	ext, contentType, err := detectCustomBrandIconFormat(data, header.Header.Get("Content-Type"), header.Filename)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if ext == ".svg" && !isSafeSVG(data) {
		response.Error(w, http.StatusBadRequest, "unsupported svg")
		return
	}

	baseName := strings.TrimSpace(r.FormValue("name"))
	if baseName == "" {
		baseName = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}
	if baseName == "" {
		baseName = "自定义图标"
	}
	color := strings.TrimSpace(r.FormValue("color"))
	issuer := strings.TrimSpace(r.FormValue("issuer"))
	result, err := s.saveCustomBrandIcon(data, baseName, issuer, color, ext, contentType)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, result)
}

func (s *Service) importCustomBrandIconFromURL(w http.ResponseWriter, r *http.Request) {
	var input struct {
		URL    string `json:"url"`
		Name   string `json:"name"`
		Issuer string `json:"issuer"`
		Color  string `json:"color"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024))
	if err := decoder.Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid import payload")
		return
	}
	if err := validateRemoteBrandIconURL(input.URL); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	fetcher := s.remoteBrandIconFetcher
	if fetcher == nil {
		fetcher = downloadRemoteBrandIcon
	}
	asset, err := fetcher(r.Context(), strings.TrimSpace(input.URL))
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	ext, contentType, err := detectCustomBrandIconFormat(asset.Data, asset.ContentType, asset.FileName)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if ext == ".svg" && !isSafeSVG(asset.Data) {
		response.Error(w, http.StatusBadRequest, "unsupported svg")
		return
	}
	baseName := strings.TrimSpace(input.Name)
	if baseName == "" {
		baseName = strings.TrimSuffix(asset.FileName, filepath.Ext(asset.FileName))
	}
	if baseName == "" {
		baseName = "远程图标"
	}
	result, err := s.saveCustomBrandIcon(
		asset.Data,
		baseName,
		strings.TrimSpace(input.Issuer),
		strings.TrimSpace(input.Color),
		ext,
		contentType,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, result)
}

func (s *Service) saveCustomBrandIcon(data []byte, baseName, issuer, color, ext, contentType string) (map[string]string, error) {
	records, err := s.loadCustomBrandIconRecords()
	if err != nil {
		return nil, err
	}
	record := customBrandIconRecord{
		ID:          uniqueCustomBrandIconID(records, baseName),
		Name:        baseName,
		Issuer:      issuer,
		Color:       color,
		Ext:         ext,
		ContentType: contentType,
		CreatedAt:   nowISO(),
	}
	if err := os.MkdirAll(s.customBrandIconDir(), 0o755); err != nil {
		return nil, fmt.Errorf("prepare icon storage: %w", err)
	}
	path := recordPathForCustomBrandIcon(s.customBrandIconDir(), record)
	if err := writeFileAtomically(path+".tmp", path, data); err != nil {
		return nil, err
	}
	records = append(records, record)
	if err := s.saveCustomBrandIconRecords(records); err != nil {
		_ = os.Remove(path)
		return nil, err
	}

	return map[string]string{
		"id":      record.ID,
		"icon":    "custom:" + record.ID,
		"name":    record.Name,
		"issuer":  record.Issuer,
		"color":   record.Color,
		"source":  "custom",
		"license": "User Upload",
		"url":     "/api/totp/icons/custom-" + record.ID,
	}, nil
}

func (s *Service) deleteCustomBrandIcon(w http.ResponseWriter, r *http.Request, id string) {
	id = strings.TrimSpace(id)
	if !safeBrandIconKeyPattern.MatchString(id) {
		response.Error(w, http.StatusBadRequest, "invalid custom icon key")
		return
	}
	records, err := s.loadCustomBrandIconRecords()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	index := -1
	for i := range records {
		if records[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		response.Error(w, http.StatusNotFound, "icon not found")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE totp_accounts SET icon = NULL, updated_at = ? WHERE icon = ?
	`, nowISO(), "custom:"+id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	record := records[index]
	remaining := append([]customBrandIconRecord{}, records[:index]...)
	remaining = append(remaining, records[index+1:]...)
	if err := s.saveCustomBrandIconRecords(remaining); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		_ = s.saveCustomBrandIconRecords(records)
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	path := recordPathForCustomBrandIcon(s.customBrandIconDir(), record)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		// Metadata is authoritative; a failed cleanup only leaves an unserved orphan file.
	}
	response.OK(w, map[string]interface{}{"id": id, "deleted": true})
}

func downloadRemoteBrandIcon(ctx context.Context, rawURL string) (remoteBrandIconAsset, error) {
	if err := validateRemoteBrandIconURL(rawURL); err != nil {
		return remoteBrandIconAsset{}, err
	}
	client := newRemoteBrandIconHTTPClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(rawURL), nil)
	if err != nil {
		return remoteBrandIconAsset{}, fmt.Errorf("invalid image URL")
	}
	req.Header.Set("Accept", "image/svg+xml,image/png,image/jpeg,image/webp,image/gif;q=0.9,*/*;q=0.1")
	res, err := client.Do(req)
	if err != nil {
		return remoteBrandIconAsset{}, fmt.Errorf("download image failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return remoteBrandIconAsset{}, fmt.Errorf("download image failed with status %d", res.StatusCode)
	}
	const maxIconBytes = 600 * 1024
	if res.ContentLength > maxIconBytes {
		return remoteBrandIconAsset{}, fmt.Errorf("remote image exceeds 600 KB")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxIconBytes+1))
	if err != nil {
		return remoteBrandIconAsset{}, fmt.Errorf("read remote image failed: %w", err)
	}
	if len(data) == 0 {
		return remoteBrandIconAsset{}, fmt.Errorf("remote image is empty")
	}
	if len(data) > maxIconBytes {
		return remoteBrandIconAsset{}, fmt.Errorf("remote image exceeds 600 KB")
	}
	parsed, _ := url.Parse(res.Request.URL.String())
	fileName := filepath.Base(parsed.Path)
	if fileName == "." || fileName == "/" {
		fileName = "remote-icon"
	}
	return remoteBrandIconAsset{
		Data:        data,
		FileName:    fileName,
		ContentType: res.Header.Get("Content-Type"),
	}, nil
}

func newRemoteBrandIconHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 15 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid remote address")
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, fmt.Errorf("resolve remote host failed")
		}
		publicAddresses := filterPublicRemoteBrandIconAddresses(addresses)
		if len(publicAddresses) == 0 {
			return nil, fmt.Errorf("private or reserved image host is not allowed")
		}
		var lastErr error
		for _, address := range publicAddresses {
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return validateRemoteBrandIconURL(req.URL.String())
		},
	}
}

func validateRemoteBrandIconURL(rawURL string) error {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("invalid image URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("only http/https image URLs are supported")
	}
	if parsed.User != nil {
		return fmt.Errorf("image URL credentials are not allowed")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil {
		if syntheticProxyBrandIconNetwork.Contains(ip) || !isPublicRemoteBrandIconIP(ip) {
			return fmt.Errorf("private or reserved image host is not allowed")
		}
	}
	return nil
}

func isPublicRemoteBrandIconIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, network := range blockedRemoteBrandIconNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

func filterPublicRemoteBrandIconAddresses(addresses []net.IPAddr) []net.IPAddr {
	publicAddresses := make([]net.IPAddr, 0, len(addresses))
	for _, address := range addresses {
		if isPublicRemoteBrandIconIP(address.IP) {
			publicAddresses = append(publicAddresses, address)
		}
	}
	return publicAddresses
}

func mustParseCIDRs(values ...string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic(err)
		}
		networks = append(networks, network)
	}
	return networks
}

func mustParseCIDR(value string) *net.IPNet {
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		panic(err)
	}
	return network
}

func (s *Service) serveCustomBrandIcon(w http.ResponseWriter, r *http.Request, id string) {
	if !safeBrandIconKeyPattern.MatchString(id) {
		response.Error(w, http.StatusBadRequest, "invalid custom icon key")
		return
	}
	records, err := s.loadCustomBrandIconRecords()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	var found *customBrandIconRecord
	for i := range records {
		if records[i].ID == id {
			found = &records[i]
			break
		}
	}
	if found == nil {
		response.Error(w, http.StatusNotFound, "icon not found")
		return
	}
	path := recordPathForCustomBrandIcon(s.customBrandIconDir(), *found)
	if _, err := os.Stat(path); err != nil {
		response.Error(w, http.StatusNotFound, "icon file missing")
		return
	}
	w.Header().Set("Content-Type", defaultString(found.ContentType, "image/svg+xml; charset=utf-8"))
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	http.ServeFile(w, r, path)
}

func (s *Service) loadCustomBrandIconRecords() ([]customBrandIconRecord, error) {
	path := s.customBrandIconMetaPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []customBrandIconRecord{}, nil
		}
		return nil, fmt.Errorf("read custom icon metadata: %w", err)
	}
	var records []customBrandIconRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parse custom icon metadata: %w", err)
	}
	filtered := make([]customBrandIconRecord, 0, len(records))
	for _, item := range records {
		if !safeBrandIconKeyPattern.MatchString(item.ID) {
			continue
		}
		if item.Ext == "" || item.ContentType == "" {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func (s *Service) saveCustomBrandIconRecords(records []customBrandIconRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.customBrandIconMetaPath()), 0o755); err != nil {
		return fmt.Errorf("prepare custom icon metadata: %w", err)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode custom icon metadata: %w", err)
	}
	return writeFileAtomically(s.customBrandIconMetaPath()+".tmp", s.customBrandIconMetaPath(), data)
}

func (s *Service) customBrandIconMetaPath() string {
	return filepath.Join(s.cfg.DataDir, "totp-icons", "custom-icons.json")
}

func (s *Service) customBrandIconDir() string {
	return filepath.Join(s.cfg.DataDir, "totp-icons", "custom")
}

func detectCustomBrandIconFormat(data []byte, declaredContentType string, fileName string) (string, string, error) {
	text := strings.ToLower(strings.TrimSpace(string(data)))
	if strings.Contains(text, "<svg") && strings.Contains(text, "</svg>") {
		return ".svg", "image/svg+xml; charset=utf-8", nil
	}
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/png":
		return ".png", contentType, nil
	case "image/jpeg":
		return ".jpg", contentType, nil
	case "image/gif":
		return ".gif", contentType, nil
	case "image/webp":
		return ".webp", contentType, nil
	}
	lowerName := strings.ToLower(fileName)
	switch {
	case strings.HasSuffix(lowerName, ".svg"):
		return ".svg", "image/svg+xml; charset=utf-8", nil
	case strings.HasSuffix(lowerName, ".png"):
		return ".png", "image/png", nil
	case strings.HasSuffix(lowerName, ".jpg"), strings.HasSuffix(lowerName, ".jpeg"):
		return ".jpg", "image/jpeg", nil
	case strings.HasSuffix(lowerName, ".gif"):
		return ".gif", "image/gif", nil
	case strings.HasSuffix(lowerName, ".webp"):
		return ".webp", "image/webp", nil
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(declaredContentType)), "image/") {
		return "", "", fmt.Errorf("unsupported image type")
	}
	return "", "", fmt.Errorf("unsupported file type, only svg/png/jpg/webp/gif are allowed")
}

func uniqueCustomBrandIconID(records []customBrandIconRecord, name string) string {
	base := slugifyCustomBrandIconID(name)
	if base == "" {
		base = randomID("icon")
		base = strings.TrimPrefix(base, "icon_")
		base = strings.ReplaceAll(base, "_", "-")
	}
	exists := map[string]bool{}
	for _, item := range records {
		exists[item.ID] = true
	}
	if !exists[base] {
		return base
	}
	for i := 2; i < 1000; i += 1 {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !exists[candidate] {
			return candidate
		}
	}
	return base + "-" + strings.ReplaceAll(randomID("x"), "_", "-")
}

func slugifyCustomBrandIconID(value string) string {
	var builder strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	id := strings.Trim(builder.String(), "-")
	if len(id) > 72 {
		id = strings.Trim(id[:72], "-")
	}
	if !safeBrandIconKeyPattern.MatchString(id) {
		return ""
	}
	return id
}

func recordPathForCustomBrandIcon(dir string, record customBrandIconRecord) string {
	return filepath.Join(dir, record.ID+record.Ext)
}
