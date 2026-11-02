package cloudflare

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func (s *Service) allZones(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	accounts, err := loadAccounts(r.Context(), db)
	_ = db.Close()
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	allZones := []interface{}{}
	for _, account := range accounts {
		token := secure.SecureDecrypt(stringValue(account["api_token"], ""))
		if token == "" {
			continue
		}
		zones, _, err := s.listZones(r.Context(), cloudflareAuthForAccount(token, account), nil)
		if err != nil {
			continue
		}
		allZones = append(allZones, zones...)
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": allZones})
}

func (s *Service) accountZones(w http.ResponseWriter, r *http.Request, accountID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		zones, pagination, err := s.listZones(r.Context(), auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		mapped := make([]map[string]interface{}, 0, len(zones))
		for _, item := range zones {
			mapped = append(mapped, mapZone(objectValue(item)))
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"zones": mapped, "pagination": pagination})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		name := strings.TrimSpace(stringValue(payload["name"], ""))
		if name == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "域名不能为空"})
			return
		}
		cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		body := map[string]interface{}{
			"name":       name,
			"jump_start": boolValue(payload["jumpStart"]),
			"type":       "full",
			"account":    map[string]interface{}{"id": cfAccountID},
		}
		apiPayload, err := s.cfRequest(r.Context(), http.MethodPost, "/zones", auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		zone := objectValue(apiPayload["result"])
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"zone": map[string]interface{}{
				"id":          zone["id"],
				"name":        zone["name"],
				"status":      zone["status"],
				"nameServers": zone["name_servers"],
				"createdOn":   zone["created_on"],
			},
		})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deleteZone(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	payload, err := s.cfRequest(r.Context(), http.MethodDelete, "/zones/"+url.PathEscape(zoneID), auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": payload["result"]})
}

func (s *Service) zoneRecords(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		records, pagination, err := s.listDNSRecords(r.Context(), auth, zoneID, r.URL.Query())
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		mapped := make([]map[string]interface{}, 0, len(records))
		for _, item := range records {
			mapped = append(mapped, mapDNSRecord(objectValue(item), true))
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"records": mapped, "pagination": pagination})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		if errors := validateDNSRecord(payload); len(errors) > 0 {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": strings.Join(errors, ", ")})
			return
		}
		record, err := s.createDNSRecord(r.Context(), auth, zoneID, payload)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "record": mapDNSRecord(record, false)})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) recordMutation(w http.ResponseWriter, r *http.Request, accountID, zoneID, recordID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodDelete:
		if _, err := s.cfRequest(r.Context(), http.MethodDelete, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), auth, nil); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	case http.MethodPut:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		apiPayload, err := s.cfRequest(r.Context(), http.MethodPatch, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), auth, dnsRecordBody(payload, true))
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "record": mapDNSRecord(objectValue(apiPayload["result"]), false)})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) batchCreateRecords(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	records := arrayValue(payload["records"])
	if records == nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "需要提供 records 数组"})
		return
	}
	results := []map[string]interface{}{}
	errorsOut := []map[string]interface{}{}
	for _, item := range records {
		recordPayload := objectValue(item)
		created, err := s.createDNSRecord(r.Context(), auth, zoneID, recordPayload)
		if err != nil {
			errorsOut = append(errorsOut, map[string]interface{}{"success": false, "record": recordPayload, "error": err.Error()})
			continue
		}
		results = append(results, map[string]interface{}{"success": true, "record": created})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": len(errorsOut) == 0,
		"created": len(results),
		"failed":  len(errorsOut),
		"results": results,
		"errors":  errorsOut,
	})
}

func (s *Service) switchDNSContent(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	recordType := strings.TrimSpace(stringValue(payload["type"], ""))
	name := strings.TrimSpace(stringValue(payload["name"], ""))
	newContent := strings.TrimSpace(stringValue(payload["newContent"], ""))
	if recordType == "" || name == "" || newContent == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "type, name, newContent 必填"})
		return
	}
	params := url.Values{}
	params.Set("type", recordType)
	params.Set("name", name)
	records, _, err := s.listDNSRecords(r.Context(), auth, zoneID, params)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if len(records) == 0 {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": fmt.Sprintf("No %s record found for %s", recordType, name)})
		return
	}
	updated := []map[string]interface{}{}
	for _, item := range records {
		record := objectValue(item)
		recordID := stringValue(record["id"], "")
		apiPayload, err := s.cfRequest(r.Context(), http.MethodPatch, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), auth, map[string]interface{}{"content": newContent})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		mapped := objectValue(apiPayload["result"])
		updated = append(updated, map[string]interface{}{"id": mapped["id"], "name": mapped["name"], "content": mapped["content"]})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "updated": len(updated), "records": updated})
}

func (s *Service) purgeZoneCache(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	body := map[string]interface{}{"purge_everything": true}
	if boolValue(payload["purge_everything"]) {
		body = map[string]interface{}{"purge_everything": true}
	} else if files := arrayValue(payload["files"]); files != nil {
		body = map[string]interface{}{"files": files}
	} else if tags := arrayValue(payload["tags"]); tags != nil {
		body = map[string]interface{}{"tags": tags}
	}
	apiPayload, err := s.cfRequest(r.Context(), http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/purge_cache", auth, body)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "缓存已清除",
		"result":  apiPayload["result"],
	})
}

func (s *Service) zoneSSL(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		settings, err := s.cfRequest(r.Context(), http.MethodGet, "/zones/"+url.PathEscape(zoneID)+"/settings/ssl", auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		certificates := []map[string]interface{}{}
		if payload, err := s.cfRequest(r.Context(), http.MethodGet, "/zones/"+url.PathEscape(zoneID)+"/ssl/certificate_packs", auth, nil); err == nil {
			for _, item := range arrayValue(payload["result"]) {
				cert := objectValue(item)
				certificates = append(certificates, map[string]interface{}{
					"id":                   cert["id"],
					"type":                 cert["type"],
					"hosts":                cert["hosts"],
					"status":               cert["status"],
					"validityDays":         cert["validity_days"],
					"certificateAuthority": cert["certificate_authority"],
					"primary":              cert["primary"],
				})
			}
		}
		verification := []interface{}{}
		if payload, err := s.cfRequest(r.Context(), http.MethodGet, "/zones/"+url.PathEscape(zoneID)+"/ssl/verification", auth, nil); err == nil {
			verification = arrayValue(payload["result"])
			if verification == nil {
				verification = []interface{}{}
			}
		}
		result := objectValue(settings["result"])
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"ssl": map[string]interface{}{
				"mode":         result["value"],
				"modifiedOn":   result["modified_on"],
				"editable":     result["editable"],
				"certificates": certificates,
				"verification": verification,
			},
		})
	case http.MethodPatch:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		mode := strings.TrimSpace(stringValue(payload["mode"], ""))
		if !containsString([]string{"off", "flexible", "full", "strict"}, mode) {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "无效的 SSL 模式"})
			return
		}
		apiPayload, err := s.cfRequest(r.Context(), http.MethodPatch, "/zones/"+url.PathEscape(zoneID)+"/settings/ssl", auth, map[string]interface{}{"value": mode})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		result := objectValue(apiPayload["result"])
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"ssl": map[string]interface{}{
				"mode":       result["value"],
				"modifiedOn": result["modified_on"],
			},
		})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) listZones(ctx context.Context, auth map[string]string, params url.Values) ([]interface{}, interface{}, error) {
	query := url.Values{}
	for key, values := range params {
		for _, value := range values {
			if value != "" {
				query.Add(key, value)
			}
		}
	}
	if query.Get("per_page") == "" {
		query.Set("per_page", "50")
	}
	path := "/zones"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	payload, err := s.cfRequest(ctx, http.MethodGet, path, auth, nil)
	if err != nil {
		return nil, nil, err
	}
	results := arrayValue(payload["result"])
	firstInfo := payload["result_info"]
	// per_page 默认 50 会静默截断，按 result_info.total_pages 翻页拉全
	totalPages := intValue(objectValue(firstInfo)["total_pages"], 1)
	for page := 2; page <= totalPages; page++ {
		query.Set("page", strconv.Itoa(page))
		pagePayload, perr := s.cfRequest(ctx, http.MethodGet, "/zones?"+query.Encode(), auth, nil)
		if perr != nil {
			applog.Warn(ctx, "cloudflare", "failed to list zones page", "page", page, "error", perr.Error())
			break
		}
		before := len(results)
		results = append(results, arrayValue(pagePayload["result"])...)
		if len(results) == before {
			// 防御：服务端返回空页时终止，避免异常响应下死循环
			break
		}
	}
	return results, firstInfo, nil
}

func (s *Service) listDNSRecords(ctx context.Context, auth map[string]string, zoneID string, params url.Values) ([]interface{}, interface{}, error) {
	query := url.Values{}
	for key, values := range params {
		for _, value := range values {
			if value != "" {
				query.Add(key, value)
			}
		}
	}
	if query.Get("per_page") == "" {
		query.Set("per_page", "100")
	}
	path := "/zones/" + url.PathEscape(zoneID) + "/dns_records"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	payload, err := s.cfRequest(ctx, http.MethodGet, path, auth, nil)
	if err != nil {
		return nil, nil, err
	}
	return arrayValue(payload["result"]), payload["result_info"], nil
}

func (s *Service) createDNSRecord(ctx context.Context, auth map[string]string, zoneID string, payload map[string]interface{}) (map[string]interface{}, error) {
	apiPayload, err := s.cfRequest(ctx, http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records", auth, dnsRecordBody(payload, false))
	if err != nil {
		return nil, err
	}
	return objectValue(apiPayload["result"]), nil
}

func mapZone(zone map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":          zone["id"],
		"name":        zone["name"],
		"status":      zone["status"],
		"paused":      zone["paused"],
		"type":        zone["type"],
		"nameServers": zone["name_servers"],
		"createdOn":   zone["created_on"],
		"modifiedOn":  zone["modified_on"],
	}
}

func mapDNSRecord(record map[string]interface{}, includeTimes bool) map[string]interface{} {
	mapped := map[string]interface{}{
		"id":       record["id"],
		"type":     record["type"],
		"name":     record["name"],
		"content":  record["content"],
		"proxied":  record["proxied"],
		"ttl":      record["ttl"],
		"priority": record["priority"],
	}
	if includeTimes {
		mapped["createdOn"] = record["created_on"]
		mapped["modifiedOn"] = record["modified_on"]
	}
	return mapped
}

func dnsRecordBody(payload map[string]interface{}, partial bool) map[string]interface{} {
	body := map[string]interface{}{}
	for _, key := range []string{"type", "name", "content"} {
		if value := strings.TrimSpace(stringValue(payload[key], "")); value != "" {
			body[key] = value
		}
	}
	if value, ok := payload["ttl"]; ok {
		body["ttl"] = intValue(value, 1)
	} else if !partial {
		body["ttl"] = 1
	}
	if value, ok := payload["proxied"]; ok {
		body["proxied"] = boolValue(value)
	} else if !partial {
		body["proxied"] = true
	}
	if value, ok := payload["priority"]; ok {
		body["priority"] = intValue(value, 0)
	}
	return body
}

func supportedRecordTypes() []string {
	return []string{"A", "AAAA", "CNAME", "TXT", "MX", "NS", "SRV", "CAA", "PTR"}
}

func validateDNSRecord(record map[string]interface{}) []string {
	errorsOut := []string{}
	recordType := strings.ToUpper(strings.TrimSpace(stringValue(record["type"], "")))
	name := strings.TrimSpace(stringValue(record["name"], ""))
	content := strings.TrimSpace(stringValue(record["content"], ""))
	if recordType == "" {
		errorsOut = append(errorsOut, "Type is required")
	} else if !containsString(supportedRecordTypes(), recordType) {
		errorsOut = append(errorsOut, "Invalid type: "+recordType)
	}
	if name == "" {
		errorsOut = append(errorsOut, "Name is required")
	}
	if content == "" {
		errorsOut = append(errorsOut, "Content is required")
	}
	if recordType == "A" {
		parts := strings.Split(content, ".")
		if len(parts) != 4 {
			errorsOut = append(errorsOut, "Invalid IPv4 address")
		} else {
			for _, part := range parts {
				value := intValue(part, -1)
				if value < 0 || value > 255 || part == "" {
					errorsOut = append(errorsOut, "Invalid IPv4 address")
					break
				}
			}
		}
	}
	if recordType == "MX" {
		if _, ok := record["priority"]; !ok {
			errorsOut = append(errorsOut, "MX record requires priority")
		}
	}
	return errorsOut
}
