package cloudflare

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) tunnels(w http.ResponseWriter, r *http.Request, accountID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if r.Method == http.MethodGet {
		path := "/accounts/" + url.PathEscape(cfAccountID) + "/cfd_tunnel?is_deleted=false&per_page=100"
		payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		tunnelsList := arrayValue(payload["result"])
		mapped := []map[string]interface{}{}
		for _, item := range tunnelsList {
			t := objectValue(item)
			conns := arrayValue(t["connections"])
			if conns == nil {
				conns = []interface{}{}
			}
			mapped = append(mapped, map[string]interface{}{
				"id":            t["id"],
				"name":          t["name"],
				"status":        t["status"],
				"createdAt":     t["created_at"],
				"deletedAt":     t["deleted_at"],
				"connections":   conns,
				"connsActiveAt": t["conns_active_at"],
				"connsPending":  t["conns_pending"],
				"remoteConfig":  t["remote_config"],
			})
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "tunnels": mapped})
	} else if r.Method == http.MethodPost {
		var reqBody struct {
			Name         string  `json:"name"`
			TunnelSecret *string `json:"tunnelSecret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if reqBody.Name == "" {
			response.Error(w, http.StatusBadRequest, "Tunnel 名称不能为空")
			return
		}
		var secret string
		if reqBody.TunnelSecret != nil && *reqBody.TunnelSecret != "" {
			secret = *reqBody.TunnelSecret
		} else {
			bytes := make([]byte, 32)
			if _, err := rand.Read(bytes); err != nil {
				response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "failed to generate tunnel secret"})
				return
			}
			secret = base64.StdEncoding.EncodeToString(bytes)
		}
		body := map[string]interface{}{
			"name":          reqBody.Name,
			"tunnel_secret": secret,
			"config_src":    "cloudflare",
		}
		payload, err := s.cfRequest(r.Context(), http.MethodPost, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel", auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		t := objectValue(payload["result"])
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"tunnel": map[string]interface{}{
				"id":        t["id"],
				"name":      t["name"],
				"status":    t["status"],
				"createdAt": t["created_at"],
			},
		})
	} else {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) tunnelMutation(w http.ResponseWriter, r *http.Request, accountID, tunnelId string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if r.Method == http.MethodGet {
		payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId), auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		t := objectValue(payload["result"])
		conns := arrayValue(t["connections"])
		if conns == nil {
			conns = []interface{}{}
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"tunnel": map[string]interface{}{
				"id":           t["id"],
				"name":         t["name"],
				"status":       t["status"],
				"createdAt":    t["created_at"],
				"connections":  conns,
				"remoteConfig": t["remote_config"],
			},
		})
	} else if r.Method == http.MethodDelete {
		// 删除前清理所有连接
		s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId)+"/connections", auth, nil)
		_, err = s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId), auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	} else if r.Method == http.MethodPatch {
		var reqBody struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if reqBody.Name == "" {
			response.Error(w, http.StatusBadRequest, "名称不能为空")
			return
		}
		body := map[string]interface{}{"name": reqBody.Name}
		payload, err := s.cfRequest(r.Context(), http.MethodPatch, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId), auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		t := objectValue(payload["result"])
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"tunnel": map[string]interface{}{
				"id":   t["id"],
				"name": t["name"],
			},
		})
	} else {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) tunnelConfiguration(w http.ResponseWriter, r *http.Request, accountID, tunnelId string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if r.Method == http.MethodGet {
		payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId)+"/configurations", auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		resVal := objectValue(payload["result"])
		config := resVal["config"]
		if config == nil {
			config = map[string]interface{}{"ingress": []interface{}{}}
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "config": config})
	} else if r.Method == http.MethodPut {
		var reqBody struct {
			Config map[string]interface{} `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if reqBody.Config == nil {
			response.Error(w, http.StatusBadRequest, "配置不能为空")
			return
		}
		body := map[string]interface{}{"config": reqBody.Config}
		payload, err := s.cfRequest(r.Context(), http.MethodPut, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId)+"/configurations", auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": payload["result"]})
	} else {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) tunnelToken(w http.ResponseWriter, r *http.Request, accountID, tunnelId string) {
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
	payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId)+"/token", auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "token": payload["result"]})
}

func (s *Service) tunnelConnections(w http.ResponseWriter, r *http.Request, accountID, tunnelId string) {
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
	payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/cfd_tunnel/"+url.PathEscape(tunnelId)+"/connections", auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	connectionsList := arrayValue(payload["result"])
	mapped := []map[string]interface{}{}
	for _, item := range connectionsList {
		c := objectValue(item)

		id := c["id"]
		if id == nil || id == "" {
			id = c["uuid"]
		}
		clientId := c["client_id"]
		if clientId == nil || clientId == "" {
			clientId = c["id"]
		}
		clientVersion := c["client_version"]
		if clientVersion == nil || clientVersion == "" {
			clientVersion = c["version"]
		}
		arch := c["arch"]
		if arch == nil || arch == "" {
			arch = c["platform"]
		}
		connectedAt := c["opened_at"]
		if connectedAt == nil || connectedAt == "" {
			connectedAt = c["connected_at"]
		}
		if connectedAt == nil || connectedAt == "" {
			connectedAt = c["created_at"]
		}
		originIp := c["origin_ip"]
		if originIp == nil || originIp == "" {
			originIp = c["origin"]
		}
		uuid := c["uuid"]
		if uuid == nil || uuid == "" {
			uuid = c["id"]
		}
		coloName := c["colo_name"]
		if coloName == nil || coloName == "" {
			coloName = c["colo"]
		}

		mapped = append(mapped, map[string]interface{}{
			"id":            id,
			"clientId":      clientId,
			"clientVersion": clientVersion,
			"arch":          arch,
			"connectedAt":   connectedAt,
			"originIp":      originIp,
			"uuid":          uuid,
			"coloName":      coloName,
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "connections": mapped})
}
