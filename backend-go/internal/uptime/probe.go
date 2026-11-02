package uptime

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Service) probe(ctx context.Context, db *sql.DB, monitor map[string]interface{}) (probeResult, error) {
	switch stringValue(monitor["type"], "http") {
	case "http", "keyword":
		return s.httpProbe(ctx, monitor, stringValue(monitor["type"], "http") == "keyword")
	case "json", "json-query":
		return s.jsonProbe(ctx, monitor)
	case "tcp":
		return tcpProbe(ctx, monitor)
	case "ping":
		return pingProbe(ctx, monitor)
	case "dns":
		return dnsProbe(ctx, monitor)
	case "push":
		return pushProbe(ctx, db, monitor)
	default:
		return probeResult{}, fmt.Errorf("Unsupported monitor type: %s", stringValue(monitor["type"], ""))
	}
}

func (s *Service) httpProbe(ctx context.Context, monitor map[string]interface{}, keyword bool) (probeResult, error) {
	started := time.Now()
	bodyReader := bytes.NewReader([]byte(stringValue(monitor["body"], "")))
	req, err := http.NewRequestWithContext(ctx, stringValue(monitor["method"], "GET"), stringValue(monitor["url"], ""), bodyReader)
	if err != nil {
		return probeResult{}, err
	}
	for key, value := range parseJSONMap(monitor["headers"]) {
		req.Header.Set(key, stringValue(value, ""))
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: boolValue(monitor["ignoreTls"], false)}
	client := http.Client{Timeout: time.Duration(intValue(monitor["timeout"], defaultTimeoutSeconds)) * time.Second, Transport: transport}
	res, err := client.Do(req)
	if err != nil {
		return probeResult{}, err
	}
	defer res.Body.Close()
	content, _ := io.ReadAll(io.LimitReader(res.Body, maxProbeBodyBytes))
	statusCode := res.StatusCode
	if !acceptedStatus(stringValue(monitor["accepted_status_codes"], ""), statusCode) {
		return probeResult{}, fmt.Errorf("HTTP %d not accepted", statusCode)
	}
	if keyword {
		expected := stringValue(monitor["keyword"], "")
		if expected != "" && !strings.Contains(string(content), expected) {
			return probeResult{}, fmt.Errorf("Keyword not found: %s", expected)
		}
	}
	result := probeResult{OK: true, Status: stateUp, LatencyMS: time.Since(started).Milliseconds(), Message: "OK", StatusCode: &statusCode, Details: map[string]interface{}{"contentLength": len(content)}}
	// Extract TLS certificate expiry
	if res.TLS != nil && len(res.TLS.PeerCertificates) > 0 {
		expiry := res.TLS.PeerCertificates[0].NotAfter
		result.SslExpiry = &expiry
	}
	return result, nil
}

func (s *Service) jsonProbe(ctx context.Context, monitor map[string]interface{}) (probeResult, error) {
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, stringValue(monitor["method"], "GET"), stringValue(monitor["url"], ""), strings.NewReader(stringValue(monitor["body"], "")))
	if err != nil {
		return probeResult{}, err
	}
	for key, value := range parseJSONMap(monitor["headers"]) {
		req.Header.Set(key, stringValue(value, ""))
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: boolValue(monitor["ignoreTls"], false)}
	client := http.Client{Timeout: time.Duration(intValue(monitor["timeout"], defaultTimeoutSeconds)) * time.Second, Transport: transport}
	res, err := client.Do(req)
	if err != nil {
		return probeResult{}, err
	}
	defer res.Body.Close()
	content, _ := io.ReadAll(io.LimitReader(res.Body, maxProbeBodyBytes))
	statusCode := res.StatusCode
	if !acceptedStatus(stringValue(monitor["accepted_status_codes"], ""), statusCode) {
		return probeResult{}, fmt.Errorf("HTTP %d not accepted", statusCode)
	}
	var parsed interface{}
	if err := json.Unmarshal(content, &parsed); err != nil {
		return probeResult{}, err
	}
	configMap := objectValue(monitor["config"])
	path := stringValue(firstNonNil(configMap["jsonQueryPath"], configMap["jsonPath"], configMap["query"], monitor["keyword"]), "")
	operator := stringValue(firstNonNil(configMap["jsonQueryOperator"], configMap["operator"]), "equals")
	expected := firstNonNil(configMap["jsonExpectedValue"], configMap["expectedValue"], monitor["expectedValue"])
	actual := jsonPathValue(parsed, path)
	if !compareValues(actual, expected, operator) {
		return probeResult{}, fmt.Errorf("JSON Query mismatch at %s: expected %s %v", stringFallback(path, "$"), operator, expected)
	}
	result := probeResult{OK: true, Status: stateUp, LatencyMS: time.Since(started).Milliseconds(), Message: "OK", StatusCode: &statusCode, Details: map[string]interface{}{"contentLength": len(content), "jsonQueryPath": stringFallback(path, "$"), "jsonQueryOperator": operator, "jsonQueryActual": actual}}
	// Extract TLS certificate expiry
	if res.TLS != nil && len(res.TLS.PeerCertificates) > 0 {
		expiry := res.TLS.PeerCertificates[0].NotAfter
		result.SslExpiry = &expiry
	}
	return result, nil
}

func tcpProbe(ctx context.Context, monitor map[string]interface{}) (probeResult, error) {
	started := time.Now()
	dialer := net.Dialer{Timeout: time.Duration(intValue(monitor["timeout"], 10)) * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(stringValue(monitor["hostname"], ""), strconv.Itoa(intValue(monitor["port"], 0))))
	if err != nil {
		return probeResult{}, err
	}
	_ = conn.Close()
	return probeResult{OK: true, Status: stateUp, LatencyMS: time.Since(started).Milliseconds(), Message: "OK"}, nil
}

func pingProbe(ctx context.Context, monitor map[string]interface{}) (probeResult, error) {
	var last error
	for _, port := range []int{80, 443, 53} {
		probeMonitor := copyMap(monitor)
		probeMonitor["port"] = port
		probeMonitor["timeout"] = math.Min(float64(intValue(monitor["timeout"], 2)), 2)
		result, err := tcpProbe(ctx, probeMonitor)
		if err == nil {
			return result, nil
		}
		last = err
	}
	return probeResult{}, fmt.Errorf("Ping TCP fallback failed: %v", last)
}

func dnsProbe(ctx context.Context, monitor map[string]interface{}) (probeResult, error) {
	started := time.Now()
	hostname := strings.TrimSpace(stringValue(monitor["hostname"], ""))
	resolveType := strings.ToUpper(stringValue(firstNonNil(monitor["dns_resolve_type"], monitor["dnsResolveType"]), "A"))
	server := strings.TrimSpace(stringValue(firstNonNil(monitor["dns_resolve_server"], monitor["dnsResolveServer"]), ""))
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(intValue(monitor["timeout"], 10))*time.Second)
	defer cancel()
	resolver := net.DefaultResolver
	if server != "" {
		resolver = customDNSResolver(server)
	}
	records := []string{}
	var err error
	switch resolveType {
	case "AAAA":
		var ips []net.IP
		ips, err = resolver.LookupIP(timeoutCtx, "ip6", hostname)
		for _, ip := range ips {
			records = append(records, ip.String())
		}
	case "MX":
		var mxs []*net.MX
		mxs, err = resolver.LookupMX(timeoutCtx, hostname)
		for _, mx := range mxs {
			records = append(records, fmt.Sprintf("%s %d", mx.Host, mx.Pref))
		}
	case "TXT":
		records, err = resolver.LookupTXT(timeoutCtx, hostname)
	case "NS":
		var nss []*net.NS
		nss, err = resolver.LookupNS(timeoutCtx, hostname)
		for _, ns := range nss {
			records = append(records, ns.Host)
		}
	case "CNAME":
		var cname string
		cname, err = resolver.LookupCNAME(timeoutCtx, hostname)
		if cname != "" {
			records = append(records, cname)
		}
	default:
		records, err = resolver.LookupHost(timeoutCtx, hostname)
	}
	if err != nil {
		return probeResult{}, err
	}
	expected := stringValue(firstNonNil(monitor["keyword"], monitor["expectedValue"], objectValue(monitor["config"])["expectedValue"]), "")
	if expected != "" && !strings.Contains(strings.Join(records, ","), expected) {
		return probeResult{}, fmt.Errorf("DNS %s expected value not found: %s", resolveType, expected)
	}
	return probeResult{OK: true, Status: stateUp, LatencyMS: time.Since(started).Milliseconds(), Message: "OK", Details: map[string]interface{}{"records": records, "type": resolveType}}, nil
}

// customDNSResolver 返回将 DNS 查询发往指定服务器（支持 "IP" 或 "IP:端口"，
// 缺省端口为 53）的解析器。
func customDNSResolver(server string) *net.Resolver {
	serverAddr := net.JoinHostPort(server, "53")
	if _, _, err := net.SplitHostPort(server); err == nil {
		serverAddr = server
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", serverAddr)
		},
	}
}

func pushProbe(ctx context.Context, db *sql.DB, monitor map[string]interface{}) (probeResult, error) {
	started := time.Now()
	configMap := objectValue(monitor["config"])
	grace := intValue(firstNonNil(monitor["pushGraceSeconds"], monitor["push_grace_seconds"], configMap["graceSeconds"]), 120)
	last, err := getLastHeartbeat(ctx, db, int64Value(monitor["id"], 0))
	if err != nil {
		return probeResult{}, err
	}
	if last == nil || intValue(last["status"], 0) != 1 {
		return probeResult{}, errors.New("Push heartbeat missing")
	}
	lastTime := parseTimeFallbackAny(firstNonNil(last["time"], last["created_at"]), time.Time{})
	if lastTime.IsZero() {
		return probeResult{}, errors.New("Push heartbeat missing")
	}
	age := time.Since(lastTime)
	if age > time.Duration(grace)*time.Second {
		return probeResult{}, fmt.Errorf("Push heartbeat overdue (%ds > %ds)", int(age.Seconds()), grace)
	}
	return probeResult{OK: true, Status: stateUp, LatencyMS: time.Since(started).Milliseconds(), Message: "Push heartbeat received", Details: map[string]interface{}{"lastPushAt": lastTime.UTC().Format(time.RFC3339), "ageSeconds": int(age.Seconds()), "graceSeconds": grace}}, nil
}

func acceptedStatus(raw string, status int) bool {
	if strings.TrimSpace(raw) == "" {
		return status >= 200 && status < 300
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			pieces := strings.SplitN(part, "-", 2)
			min, _ := strconv.Atoi(strings.TrimSpace(pieces[0]))
			max, _ := strconv.Atoi(strings.TrimSpace(pieces[1]))
			if status >= min && status <= max {
				return true
			}
			continue
		}
		exact, err := strconv.Atoi(part)
		if err == nil && status == exact {
			return true
		}
	}
	return false
}
