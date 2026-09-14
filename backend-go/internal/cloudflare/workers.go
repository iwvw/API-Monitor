package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) workers(w http.ResponseWriter, r *http.Request, accountID string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/workers/scripts", auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	subdomainPayload, _ := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/workers/subdomain", auth, nil)
	subdomain := objectValue(subdomainPayload["result"])
	workers := []map[string]interface{}{}
	for _, item := range arrayValue(payload["result"]) {
		worker := objectValue(item)
		workers = append(workers, map[string]interface{}{
			"id":         worker["id"],
			"name":       stringValue(worker["id"], stringValue(worker["name"], "")),
			"createdOn":  worker["created_on"],
			"modifiedOn": worker["modified_on"],
			"etag":       worker["etag"],
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"workers":     workers,
		"subdomain":   nullableString(stringValue(subdomain["subdomain"], "")),
		"cfAccountId": cfAccountID,
	})
}

func (s *Service) workerScript(w http.ResponseWriter, r *http.Request, accountID, scriptName string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	switch r.Method {
	case http.MethodGet:
		script, err := s.getWorkerScript(r.Context(), auth, cfAccountID, scriptName)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"worker":  map[string]interface{}{"name": scriptName, "script": script, "meta": nil},
		})
	case http.MethodPut:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		script := stringValue(payload["script"], "")
		if strings.TrimSpace(script) == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "脚本内容不能为空"})
			return
		}
		result, err := s.putWorkerScript(r.Context(), auth, cfAccountID, scriptName, script, payload)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "worker": result})
	case http.MethodDelete:
		if _, err := s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+url.PathEscape(cfAccountID)+"/workers/scripts/"+url.PathEscape(scriptName), auth, nil); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) toggleWorker(w http.ResponseWriter, r *http.Request, accountID, scriptName string) {
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	result, err := s.cfRequest(r.Context(), http.MethodPost, "/accounts/"+url.PathEscape(cfAccountID)+"/workers/scripts/"+url.PathEscape(scriptName)+"/subdomain", auth, map[string]interface{}{"enabled": boolValue(payload["enabled"])})
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result["result"]})
}

func (s *Service) workerRoutes(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		payload, err := s.cfRequest(r.Context(), http.MethodGet, "/zones/"+url.PathEscape(zoneID)+"/workers/routes", auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		routes := []map[string]interface{}{}
		for _, item := range arrayValue(payload["result"]) {
			routes = append(routes, mapWorkerRoute(objectValue(item)))
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"routes": routes})
	case http.MethodPost:
		body, ok := s.workerRouteBody(w, r)
		if !ok {
			return
		}
		payload, err := s.cfRequest(r.Context(), http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/workers/routes", auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "route": mapWorkerRoute(objectValue(payload["result"]))})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) workerRouteMutation(w http.ResponseWriter, r *http.Request, accountID, zoneID, routeID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	path := "/zones/" + url.PathEscape(zoneID) + "/workers/routes/" + url.PathEscape(routeID)
	switch r.Method {
	case http.MethodPut:
		body, ok := s.workerRouteBody(w, r)
		if !ok {
			return
		}
		payload, err := s.cfRequest(r.Context(), http.MethodPut, path, auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "route": mapWorkerRoute(objectValue(payload["result"]))})
	case http.MethodDelete:
		if _, err := s.cfRequest(r.Context(), http.MethodDelete, path, auth, nil); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) workerRouteBody(w http.ResponseWriter, r *http.Request) (map[string]interface{}, bool) {
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return nil, false
	}
	pattern := strings.TrimSpace(stringValue(payload["pattern"], ""))
	script := strings.TrimSpace(stringValue(payload["script"], ""))
	if pattern == "" || script == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "pattern 和 script 必填"})
		return nil, false
	}
	return map[string]interface{}{"pattern": pattern, "script": script}, true
}

func (s *Service) workerAnalytics(w http.ResponseWriter, r *http.Request, accountID, scriptName string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/workers/scripts/" + url.PathEscape(scriptName) + "/analytics"
	if since := strings.TrimSpace(r.URL.Query().Get("since")); since != "" {
		path += "?since=" + url.QueryEscape(since)
	}
	payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "analytics": nil})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "analytics": payload["result"]})
}

func (s *Service) workerDomains(w http.ResponseWriter, r *http.Request, accountID, scriptName string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	switch r.Method {
	case http.MethodGet:
		domains, err := s.listWorkerDomains(r.Context(), auth, cfAccountID, scriptName)
		if err != nil {
			domains = []map[string]interface{}{}
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "domains": domains})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		hostname := strings.TrimSpace(stringValue(payload["hostname"], ""))
		if hostname == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "请输入域名"})
			return
		}
		domain, err := s.addWorkerDomain(r.Context(), auth, cfAccountID, scriptName, hostname, stringValue(payload["environment"], "production"))
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "domain": domain})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deleteWorkerDomain(w http.ResponseWriter, r *http.Request, accountID, scriptName, domainID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	_ = scriptName
	if _, err := s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+url.PathEscape(cfAccountID)+"/workers/domains/"+url.PathEscape(domainID), auth, nil); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) getWorkerScript(ctx context.Context, auth map[string]string, accountID, scriptName string) (string, error) {
	raw, contentType, err := s.cfRawRequest(ctx, http.MethodGet, "/accounts/"+url.PathEscape(accountID)+"/workers/scripts/"+url.PathEscape(scriptName), auth, "application/javascript", "", nil)
	if err != nil {
		return "", err
	}
	if strings.Contains(strings.ToLower(contentType), "multipart/") {
		if script := extractMultipartScript(raw, contentType); script != "" {
			return script, nil
		}
	}
	return strings.TrimSpace(string(raw)), nil
}

func (s *Service) putWorkerScript(ctx context.Context, auth map[string]string, accountID, scriptName, script string, payload map[string]interface{}) (map[string]interface{}, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	isModule := strings.Contains(script, "export default") || strings.Contains(script, "export {") || strings.Contains(script, "export async")
	meta := map[string]interface{}{
		"bindings":           arrayValue(payload["bindings"]),
		"compatibility_date": stringValue(payload["compatibility_date"], time.Now().UTC().Format("2006-01-02")),
	}
	if meta["bindings"] == nil {
		meta["bindings"] = []interface{}{}
	}
	if isModule {
		meta["main_module"] = "worker.js"
	} else {
		meta["body_part"] = "script"
	}
	metaPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": []string{`form-data; name="metadata"`},
		"Content-Type":        []string{"application/json"},
	})
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(metaPart).Encode(meta); err != nil {
		return nil, err
	}
	fieldName := "script"
	fileName := "script.js"
	contentType := "application/javascript"
	if isModule {
		fieldName = "worker.js"
		fileName = "worker.js"
		contentType = "application/javascript+module"
	}
	scriptPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": []string{fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, fileName)},
		"Content-Type":        []string{contentType},
	})
	if err != nil {
		return nil, err
	}
	if _, err := scriptPart.Write([]byte(script)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	raw, _, err := s.cfRawRequest(ctx, http.MethodPut, "/accounts/"+url.PathEscape(accountID)+"/workers/scripts/"+url.PathEscape(scriptName), auth, "application/json", writer.FormDataContentType(), body)
	if err != nil {
		return nil, err
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("invalid worker upload response")
	}
	return objectValue(decoded["result"]), nil
}

func (s *Service) listWorkerDomains(ctx context.Context, auth map[string]string, accountID, scriptName string) ([]map[string]interface{}, error) {
	payload, err := s.cfRequest(ctx, http.MethodGet, "/accounts/"+url.PathEscape(accountID)+"/workers/domains", auth, nil)
	if err != nil {
		return nil, err
	}
	domains := []map[string]interface{}{}
	for _, item := range arrayValue(payload["result"]) {
		domain := objectValue(item)
		if stringValue(domain["service"], "") != scriptName {
			continue
		}
		domains = append(domains, mapWorkerDomain(domain))
	}
	return domains, nil
}

func (s *Service) addWorkerDomain(ctx context.Context, auth map[string]string, accountID, scriptName, hostname, environment string) (map[string]interface{}, error) {
	zoneID := ""
	parts := strings.Split(hostname, ".")
	for i := 0; i < len(parts)-1; i++ {
		name := strings.Join(parts[i:], ".")
		zones, _, err := s.listZones(ctx, auth, url.Values{"name": []string{name}})
		if err != nil || len(zones) == 0 {
			continue
		}
		zoneID = stringValue(objectValue(zones[0])["id"], "")
		if zoneID != "" {
			break
		}
	}
	if zoneID == "" {
		return nil, fmt.Errorf("未找到域名 %s 对应的 Zone，请确保该域名已在 Cloudflare DNS 中托管", hostname)
	}
	payload, err := s.cfRequest(ctx, http.MethodPut, "/accounts/"+url.PathEscape(accountID)+"/workers/domains", auth, map[string]interface{}{
		"hostname":    hostname,
		"service":     scriptName,
		"environment": stringValue(environment, "production"),
		"zone_id":     zoneID,
	})
	if err != nil {
		return nil, err
	}
	return objectValue(payload["result"]), nil
}

func mapWorkerRoute(route map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":      route["id"],
		"pattern": route["pattern"],
		"script":  route["script"],
	}
}

func mapWorkerDomain(domain map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":          domain["id"],
		"hostname":    domain["hostname"],
		"service":     domain["service"],
		"environment": domain["environment"],
		"zoneId":      domain["zone_id"],
		"zoneName":    domain["zone_name"],
	}
}

func extractMultipartScript(raw []byte, contentType string) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	boundary := params["boundary"]
	if boundary == "" {
		return ""
	}
	reader := multipart.NewReader(bytes.NewReader(raw), boundary)
	for {
		part, err := reader.NextPart()
		if err != nil {
			return ""
		}
		partType := strings.ToLower(part.Header.Get("Content-Type"))
		fileName := strings.ToLower(part.FileName())
		if strings.Contains(partType, "javascript") || strings.HasSuffix(fileName, ".js") {
			body, _ := io.ReadAll(io.LimitReader(part, 16<<20))
			return strings.TrimSpace(string(body))
		}
	}
}
