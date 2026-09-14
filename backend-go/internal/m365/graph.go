package m365

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) organization(w http.ResponseWriter, r *http.Request, idText string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	org, err := s.fetchOrganization(r.Context(), account)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, org)
}

func (s *Service) permissions(w http.ResponseWriter, r *http.Request, idText string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	token, err := s.token(r.Context(), account)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	roleSet, err := tokenRoleSet(token)
	if err != nil {
		response.Error(w, http.StatusBadGateway, fmt.Sprintf("permission detection failed: %v", err))
		return
	}
	items := make([]map[string]interface{}, 0, len(requiredPermissions))
	grantedCount := 0
	for _, permission := range requiredPermissions {
		granted := roleSet[permission.Name]
		if granted {
			grantedCount++
		}
		items = append(items, map[string]interface{}{
			"name":    permission.Name,
			"note":    permission.Note,
			"granted": granted,
		})
	}
	response.OK(w, map[string]interface{}{
		"items":          items,
		"grantedCount":   grantedCount,
		"missingCount":   len(requiredPermissions) - grantedCount,
		"tokenRoleCount": len(roleSet),
	})
}

func (s *Service) fetchOrganization(ctx context.Context, account accountRecord) (map[string]interface{}, error) {
	result := map[string]interface{}{}
	if err := s.graphJSON(ctx, account, http.MethodGet, "/organization", nil, nil, &result); err != nil {
		return nil, err
	}
	items := objectArray(result["value"])
	if len(items) == 0 {
		return map[string]interface{}{}, nil
	}
	return items[0], nil
}

func (s *Service) graphJSON(ctx context.Context, account accountRecord, method, path string, body interface{}, extraHeaders map[string]string, target interface{}) error {
	token, err := s.token(ctx, account)
	if err != nil {
		return err
	}
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, joinURL(s.graphBase, path), bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range extraHeaders {
		req.Header.Set(key, value)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeGraphError(resp)
	}
	if target == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (s *Service) token(ctx context.Context, account accountRecord) (string, error) {
	form := url.Values{}
	form.Set("client_id", account.ClientID)
	form.Set("client_secret", account.ClientSecret)
	form.Set("scope", "https://graph.microsoft.com/.default")
	form.Set("grant_type", "client_credentials")
	endpoint := strings.TrimRight(s.loginBase, "/") + "/" + account.TenantID + "/oauth2/v2.0/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", decodeGraphError(resp)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.AccessToken == "" {
		return "", errors.New("empty access token")
	}
	return payload.AccessToken, nil
}

func tokenRoleSet(token string) (map[string]bool, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, errors.New("access token is not a JWT")
	}
	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode token payload: %w", err)
	}
	var payload struct {
		Roles []string `json:"roles"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, fmt.Errorf("parse token payload: %w", err)
	}
	roleSet := map[string]bool{}
	for _, role := range payload.Roles {
		role = strings.TrimSpace(role)
		if role != "" {
			roleSet[role] = true
		}
	}
	return roleSet, nil
}

func decodeGraphError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var envelope graphErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Message != "" {
		return fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	return fmt.Errorf("graph request failed: %s", message)
}
