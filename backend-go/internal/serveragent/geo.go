package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const ipInfoEndpoint = "https://uapis.cn/api/v1/network/ipinfo"

var geoHTTPClient = http.DefaultClient

type ipInfoResult struct {
	IP      string `json:"ip"`
	Region  string `json:"region"`
	ISP     string `json:"isp"`
	LLC     string `json:"llc"`
	ASN     string `json:"asn"`
	Message string `json:"message"`
}

func normalizeLookupHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || host == "0.0.0.0" {
		return ""
	}
	if strings.Contains(host, "://") {
		if parsed, err := url.Parse(host); err == nil {
			host = parsed.Host
		}
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	parsedIP := net.ParseIP(host)
	if parsedIP != nil && !parsedIP.IsGlobalUnicast() {
		return ""
	}
	if parsedIP != nil && (parsedIP.IsPrivate() || parsedIP.IsLoopback() || parsedIP.IsLinkLocalUnicast()) {
		return ""
	}
	if parsedIP == nil && (!strings.Contains(host, ".") || strings.EqualFold(host, "localhost")) {
		return ""
	}
	return host
}

func (s *Service) lookupHostLocation(ctx context.Context, host string) (map[string]interface{}, bool) {
	lookupHost := normalizeLookupHost(host)
	if lookupHost == "" {
		return nil, false
	}

	providers := []func(context.Context, string) (map[string]interface{}, bool){
		s.lookupHostLocationFromIPSB,
		s.lookupHostLocationFromUapis,
		s.lookupHostLocationFromIPAPI,
		s.lookupHostLocationFromIPAPICo,
	}
	for _, provider := range providers {
		if info, ok := provider(ctx, lookupHost); ok {
			return info, true
		}
	}
	return nil, false
}

func (s *Service) lookupHostLocationFromUapis(ctx context.Context, lookupHost string) (map[string]interface{}, bool) {
	reqCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("ip", lookupHost)
	params.Set("source", "commercial")
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, ipInfoEndpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, false
	}

	resp, err := geoHTTPClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}

	var result ipInfoResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, false
	}
	region := strings.TrimSpace(result.Region)
	if region == "" {
		return nil, false
	}

	info := map[string]interface{}{
		"ip":       firstNonEmpty(strings.TrimSpace(result.IP), lookupHost),
		"region":   region,
		"location": region,
	}
	if result.ISP != "" {
		info["isp"] = result.ISP
	}
	if result.LLC != "" {
		info["llc"] = result.LLC
	}
	if result.ASN != "" {
		info["asn"] = result.ASN
	}
	return info, true
}

func (s *Service) lookupHostLocationFromIPSB(ctx context.Context, lookupHost string) (map[string]interface{}, bool) {
	var raw map[string]interface{}
	if !fetchGeoJSON(ctx, "https://api.ip.sb/geoip/"+url.PathEscape(lookupHost), &raw) {
		return nil, false
	}
	region := strings.Join(compactStrings(
		geoString(raw, "country"),
		geoString(raw, "region"),
		geoString(raw, "city"),
	), " ")
	countryCode := strings.ToLower(geoString(raw, "country_code"))
	return buildGeoInfo(firstNonEmpty(geoString(raw, "ip"), lookupHost), region, countryCode, map[string]interface{}{
		"isp":       geoString(raw, "isp"),
		"llc":       geoString(raw, "organization"),
		"asn":       geoString(raw, "asn"),
		"timezone":  geoString(raw, "timezone"),
		"latitude":  raw["latitude"],
		"longitude": raw["longitude"],
	})
}

func (s *Service) lookupHostLocationFromIPAPI(ctx context.Context, lookupHost string) (map[string]interface{}, bool) {
	params := url.Values{}
	params.Set("fields", "status,message,query,country,countryCode,regionName,city,isp,as,lat,lon,timezone")
	params.Set("lang", "zh-CN")
	var raw map[string]interface{}
	endpoint := "http://ip-api.com/json/" + url.PathEscape(lookupHost) + "?" + params.Encode()
	if !fetchGeoJSON(ctx, endpoint, &raw) || geoString(raw, "status") == "fail" {
		return nil, false
	}
	region := strings.Join(compactStrings(
		geoString(raw, "country"),
		geoString(raw, "regionName"),
		geoString(raw, "city"),
	), " ")
	return buildGeoInfo(firstNonEmpty(geoString(raw, "query"), lookupHost), region, strings.ToLower(geoString(raw, "countryCode")), map[string]interface{}{
		"isp":       geoString(raw, "isp"),
		"asn":       geoString(raw, "as"),
		"timezone":  geoString(raw, "timezone"),
		"latitude":  raw["lat"],
		"longitude": raw["lon"],
	})
}

func (s *Service) lookupHostLocationFromIPAPICo(ctx context.Context, lookupHost string) (map[string]interface{}, bool) {
	var raw map[string]interface{}
	if !fetchGeoJSON(ctx, "https://ipapi.co/"+url.PathEscape(lookupHost)+"/json/", &raw) || raw["error"] == true {
		return nil, false
	}
	region := strings.Join(compactStrings(
		geoString(raw, "country_name"),
		geoString(raw, "region"),
		geoString(raw, "city"),
	), " ")
	return buildGeoInfo(firstNonEmpty(geoString(raw, "ip"), lookupHost), region, strings.ToLower(geoString(raw, "country_code")), map[string]interface{}{
		"isp":       firstNonEmpty(geoString(raw, "org"), geoString(raw, "network")),
		"asn":       geoString(raw, "asn"),
		"timezone":  geoString(raw, "timezone"),
		"latitude":  raw["latitude"],
		"longitude": raw["longitude"],
	})
}

func fetchGeoJSON(ctx context.Context, endpoint string, target interface{}) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "API-Monitor/1.0")
	resp, err := geoHTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return json.NewDecoder(resp.Body).Decode(target) == nil
}

func buildGeoInfo(ip, region, countryCode string, extra map[string]interface{}) (map[string]interface{}, bool) {
	region = strings.TrimSpace(region)
	countryCode = strings.ToLower(strings.TrimSpace(countryCode))
	if region == "" && countryCode == "" {
		return nil, false
	}
	info := map[string]interface{}{
		"ip":       ip,
		"region":   firstNonEmpty(region, strings.ToUpper(countryCode)),
		"location": firstNonEmpty(region, strings.ToUpper(countryCode)),
	}
	if countryCode != "" {
		info["country_code"] = countryCode
	}
	for key, value := range extra {
		if text, ok := value.(string); ok {
			value = strings.TrimSpace(text)
		}
		if value != nil && value != "" {
			info[key] = value
		}
	}
	return info, true
}

func geoString(m map[string]interface{}, key string) string {
	value, ok := m[key]
	if !ok || value == nil {
		return ""
	}
	switch val := value.(type) {
	case string:
		return strings.TrimSpace(val)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case json.Number:
		return val.String()
	default:
		return strings.TrimSpace(fmt.Sprint(val))
	}
}

func compactStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func mergeCachedInfo(raw sql.NullString, updates map[string]interface{}) string {
	cached := map[string]interface{}{}
	if raw.Valid && raw.String != "" {
		_ = json.Unmarshal([]byte(raw.String), &cached)
	}
	for key, value := range updates {
		if value != nil {
			cached[key] = value
		}
	}
	encoded, _ := json.Marshal(cached)
	return string(encoded)
}

func accountNeedsLocation(country, resolvedCountry sql.NullString, cachedInfo sql.NullString) bool {
	if country.Valid && country.String != "" && country.String != "auto" {
		return false
	}
	if resolvedCountry.Valid && resolvedCountry.String != "" {
		return false
	}
	if cachedInfo.Valid && cachedInfo.String != "" {
		var cached map[string]interface{}
		if err := json.Unmarshal([]byte(cachedInfo.String), &cached); err == nil {
			return firstNonEmpty(
				getString(cached, "resolved_country"),
				getString(cached, "country_code"),
				getString(cached, "location"),
				getString(cached, "region"),
			) == ""
		}
	}
	return true
}
