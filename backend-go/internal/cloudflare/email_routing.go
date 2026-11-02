package cloudflare

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"golang.org/x/sync/errgroup"
)

// emailRoutingZones 返回可管理 Email Routing 的域名列表，附带各域名的启用状态，供前端列表用。
func (s *Service) emailRoutingZones(w http.ResponseWriter, r *http.Request, accountID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	zones, _, err := s.listZones(r.Context(), auth, url.Values{})
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	out := make([]map[string]interface{}, len(zones))
	g, gctx := errgroup.WithContext(r.Context())
	g.SetLimit(6)
	for i, item := range zones {
		z := objectValue(item)
		id := stringValue(z["id"], "")
		name := stringValue(z["name"], "")
		entry := map[string]interface{}{"id": id, "name": name, "enabled": false, "status": ""}
		out[i] = entry
		if id == "" {
			continue
		}
		g.Go(func() error {
			payload, err := s.cfRequest(gctx, http.MethodGet, "zones/"+id+"/email/routing", auth, nil)
			if err != nil {
				return nil
			}
			result := objectValue(payload["result"])
			entry["enabled"] = boolValue(result["enabled"])
			entry["status"] = stringValue(result["status"], "")
			return nil
		})
	}
	_ = g.Wait()
	response.JSON(w, http.StatusOK, map[string]interface{}{"zones": out})
}

func (s *Service) emailRoutingSettings(w http.ResponseWriter, r *http.Request, accountID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	zoneID, ok := s.resolveZoneID(w, r, auth)
	if !ok {
		return
	}
	zones, _, err := s.listZones(r.Context(), auth, url.Values{})
	zoneName := ""
	if err == nil {
		zoneName = zoneNameByID(zones, zoneID)
	}
	payload, err := s.cfRequest(r.Context(), http.MethodGet, "zones/"+zoneID+"/email/routing", auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	result := objectValue(payload["result"])
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"enabled":    boolValue(result["enabled"]),
		"status":     stringValue(result["status"], ""),
		"name":       stringValue(result["name"], zoneName),
		"createdAt":  stringValue(result["created"], ""),
		"modifiedAt": stringValue(result["modified"], ""),
		"zoneId":     zoneID,
		"zoneName":   zoneName,
		"raw":        result,
	})
}

// zoneNameByID 从 zone 列表中按 id 查域名，未命中返回空串。
func zoneNameByID(zones []interface{}, zoneID string) string {
	for _, item := range zones {
		z := objectValue(item)
		if stringValue(z["id"], "") == zoneID {
			return stringValue(z["name"], "")
		}
	}
	return ""
}

// emailRoutingAddresses 列出或新增目的地地址。
func (s *Service) emailRoutingAddresses(w http.ResponseWriter, r *http.Request, accountID string) {
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
		payload, err := s.cfRequest(r.Context(), http.MethodGet, "accounts/"+cfAccountID+"/email/routing/addresses", auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		result := arrayValue(payload["result"])
		out := make([]map[string]interface{}, 0, len(result))
		for _, item := range result {
			o := objectValue(item)
			out = append(out, mapAddress(o))
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"addresses": out})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		email := strings.TrimSpace(stringValue(payload["email"], ""))
		if email == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "邮箱不能为空"})
			return
		}
		respPayload, err := s.cfRequest(r.Context(), http.MethodPost, "accounts/"+cfAccountID+"/email/routing/addresses", auth,
			map[string]interface{}{"email": email})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"address": mapAddress(objectValue(respPayload["result"])),
		})
	default:
		response.JSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed"})
	}
}

// mapAddress 把 Cloudflare Email Routing 目的地地址映射为前端结构。
// CF 的 verified 字段不同版本形态不一：旧版为字符串已验证时间戳 / "pending"，
// 新版可能直接返回布尔 true/false，这里两种形态都兼容。
func mapAddress(o map[string]interface{}) map[string]interface{} {
	verified := false
	verifiedAt := ""
	switch v := o["verified"].(type) {
	case bool:
		verified = v
	case string:
		raw := v
		if raw == "verified" {
			verified = true
			verifiedAt = raw
		} else if raw != "" && raw != "pending" {
			// 兼容旧版时间戳形态：非 pending 即已认证。
			verified = true
			verifiedAt = raw
		}
	}
	return map[string]interface{}{
		"id":         stringValue(o["id"], ""),
		"email":      stringValue(o["email"], ""),
		"verified":   verified,
		"verifiedAt": verifiedAt,
		"createdAt":  stringValue(o["created"], ""),
	}
}

// emailRoutingDeleteAddress 删除目的地地址。
func (s *Service) emailRoutingDeleteAddress(w http.ResponseWriter, r *http.Request, accountID, addressID string) {
	if r.Method != http.MethodDelete {
		response.JSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed"})
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
	if _, err := s.cfRequest(r.Context(), http.MethodDelete, "accounts/"+cfAccountID+"/email/routing/addresses/"+addressID, auth, nil); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// emailRoutingRules 列出或新增路由规则（zone 级接口）。
func (s *Service) emailRoutingRules(w http.ResponseWriter, r *http.Request, accountID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	zoneID, ok := s.resolveZoneID(w, r, auth)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		payload, err := s.cfRequest(r.Context(), http.MethodGet, "zones/"+zoneID+"/email/routing/rules", auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		result := arrayValue(payload["result"])
		out := make([]map[string]interface{}, 0, len(result))
		for _, item := range result {
			out = append(out, mapEmailRule(objectValue(item)))
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"rules": out})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		body := map[string]interface{}{
			"name":        strings.TrimSpace(stringValue(payload["name"], "")),
			"enabled":     boolValue(payload["enabled"]),
			"matchers":    arrayValue(payload["matchers"]),
			"actions":     arrayValue(payload["actions"]),
			"catch_all":   boolValue(payload["catchAll"]),
			"skip_wizard": boolValue(payload["skipWizard"]),
			"priority":    intValue(payload["priority"], 0),
			"tag":         stringValue(payload["tag"], ""),
			"stop":        boolValue(payload["stop"]),
		}
		if len(arrayValue(payload["matchers"])) == 0 {
			if m := objectValue(payload["matcher"]); len(m) > 0 {
				body["matchers"] = []interface{}{m}
			}
		}
		respPayload, err := s.cfRequest(r.Context(), http.MethodPost, "zones/"+zoneID+"/email/routing/rules", auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"rule":    mapEmailRule(objectValue(respPayload["result"])),
		})
	default:
		response.JSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed"})
	}
}

// emailRoutingDeleteRule 删除路由规则（zone 级）。
func (s *Service) emailRoutingDeleteRule(w http.ResponseWriter, r *http.Request, accountID, ruleID string) {
	if r.Method != http.MethodDelete {
		response.JSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed"})
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	zoneID, ok := s.resolveZoneID(w, r, auth)
	if !ok {
		return
	}
	if _, err := s.cfRequest(r.Context(), http.MethodDelete, "zones/"+zoneID+"/email/routing/rules/"+ruleID, auth, nil); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// resolveZoneID 解析要操作的域名：优先用 URL 查询参数 zoneId，
// 未指定时回退到账号下第一个 zone。失败时写入错误响应并返回 false。
func (s *Service) resolveZoneID(w http.ResponseWriter, r *http.Request, auth map[string]string) (string, bool) {
	if z := strings.TrimSpace(r.URL.Query().Get("zoneId")); z != "" {
		return z, true
	}
	return s.firstZoneID(w, r, auth)
}

// firstZoneID 返回账号下第一个 zone 的 id；失败时写入错误响应并返回 false。
func (s *Service) firstZoneID(w http.ResponseWriter, r *http.Request, auth map[string]string) (string, bool) {
	zones, _, err := s.listZones(r.Context(), auth, url.Values{})
	if err != nil || len(zones) == 0 {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "无法获取该账号下的域名列表"})
		return "", false
	}
	zoneID := stringValue(objectValue(zones[0])["id"], "")
	if zoneID == "" {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "域名缺少 zone id"})
		return "", false
	}
	return zoneID, true
}

// mapEmailRule 把 Cloudflare Email Routing rule 映射为前端友好结构。
func mapEmailRule(o map[string]interface{}) map[string]interface{} {
	// matchers 是数组，取第一个；actions 的 value 是 string 数组。
	matchers := arrayValue(o["matchers"])
	matcher := "全部匹配"
	matcherValue := ""
	if len(matchers) > 0 {
		mo := objectValue(matchers[0])
		switch stringValue(mo["type"], "") {
		case "all":
			matcher = "全部匹配"
		case "literal":
			matcher = "收件人精确匹配"
			matcherValue = stringValue(mo["value"], "")
		case "custom":
			matcher = "自定义匹配"
			matcherValue = stringValue(mo["value"], "")
		default:
			matcher = stringValue(mo["field"], "") + " " + stringValue(mo["type"], "")
			matcherValue = stringValue(mo["value"], "")
		}
	}
	actions := arrayValue(o["actions"])
	actionList := make([]string, 0, len(actions))
	for _, a := range actions {
		ao := objectValue(a)
		t := stringValue(ao["type"], "")
		value := joinStringArray(arrayValue(ao["value"]))
		switch t {
		case "forward":
			actionList = append(actionList, "转发至 "+value)
		case "worker":
			actionList = append(actionList, "发往 Worker "+value)
		case "drop":
			actionList = append(actionList, "丢弃")
		default:
			actionList = append(actionList, t)
		}
	}
	return map[string]interface{}{
		"id":           stringValue(o["id"], ""),
		"name":         stringValue(o["name"], ""),
		"enabled":      boolValue(o["enabled"]),
		"matcher":      matcher,
		"matcherValue": matcherValue,
		"catchAll":     boolValue(o["catch_all"]),
		"priority":     intValue(o["priority"], 0),
		"stop":         boolValue(o["stop"]),
		"actions":      actionList,
		"createdAt":    stringValue(o["created"], ""),
	}
}

// joinStringArray 把 []interface{}{"a","b"} 拼成 "a;b"。
func joinStringArray(values []interface{}) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ";")
}
