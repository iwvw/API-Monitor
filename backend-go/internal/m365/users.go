package m365

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) users(w http.ResponseWriter, r *http.Request, idText string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		query := url.Values{}
		query.Set("$top", clampPositive(r.URL.Query().Get("top"), 50, 200))
		query.Set("$count", "true")
		query.Set("$select", "id,displayName,userPrincipalName,mail,jobTitle,department,officeLocation,usageLocation,accountEnabled,createdDateTime,assignedLicenses")
		search := strings.TrimSpace(r.URL.Query().Get("search"))
		if search != "" {
			query.Set("$search", fmt.Sprintf(`"displayName:%s" OR "userPrincipalName:%s"`, escapeSearch(search), escapeSearch(search)))
		}
		path := "/users?" + query.Encode()
		headers := map[string]string{"ConsistencyLevel": "eventual"}
		result := map[string]interface{}{}
		if err := s.graphJSON(r.Context(), account, http.MethodGet, path, nil, headers, &result); err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{
			"items":    objectArray(result["value"]),
			"count":    numberValue(result["@odata.count"]),
			"nextLink": stringValue(result["@odata.nextLink"], ""),
		})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		body, err := normalizeCreateUserPayload(payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		created := map[string]interface{}{}
		if err := s.graphJSON(r.Context(), account, http.MethodPost, "/users", body, nil, &created); err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		response.OK(w, created)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) userDetails(w http.ResponseWriter, r *http.Request, idText, userID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	result := map[string]interface{}{}
	path := "/users/" + url.PathEscape(userID) + "?$select=id,displayName,mailNickname,userPrincipalName,mail,jobTitle,department,officeLocation,usageLocation,accountEnabled,createdDateTime,assignedLicenses,proxyAddresses,businessPhones"
	if err := s.graphJSON(r.Context(), account, http.MethodGet, path, nil, nil, &result); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, result)
}

func (s *Service) userMutation(w http.ResponseWriter, r *http.Request, idText, userID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		body := normalizeUserPatchPayload(payload)
		if value, ok := payload["accountEnabled"]; ok {
			body["accountEnabled"] = boolValue(value, true)
		}
		if password := strings.TrimSpace(stringValue(payload["password"], "")); password != "" {
			body["passwordProfile"] = map[string]interface{}{
				"password":                      password,
				"forceChangePasswordNextSignIn": boolValue(payload["forceChangePasswordNextSignIn"], false),
			}
		}
		if len(body) == 0 {
			response.Error(w, http.StatusBadRequest, "no supported fields provided")
			return
		}
		if err := s.graphJSON(r.Context(), account, http.MethodPatch, "/users/"+url.PathEscape(userID), body, nil, nil); err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"updated": true})
	case http.MethodDelete:
		if err := s.graphJSON(r.Context(), account, http.MethodDelete, "/users/"+url.PathEscape(userID), nil, nil, nil); err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"deleted": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) userLicenseDetails(w http.ResponseWriter, r *http.Request, idText, userID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	result := map[string]interface{}{}
	if err := s.graphJSON(r.Context(), account, http.MethodGet, "/users/"+url.PathEscape(userID)+"/licenseDetails", nil, nil, &result); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"items": objectArray(result["value"])})
}

func (s *Service) assignUserLicense(w http.ResponseWriter, r *http.Request, idText, userID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	body := map[string]interface{}{
		"addLicenses":    normalizeLicenseAssignments(payload["addLicenses"]),
		"removeLicenses": stringArray(payload["removeLicenses"]),
	}
	result := map[string]interface{}{}
	if err := s.graphJSON(r.Context(), account, http.MethodPost, "/users/"+url.PathEscape(userID)+"/assignLicense", body, nil, &result); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, result)
}

func normalizeCreateUserPayload(payload map[string]interface{}) (map[string]interface{}, error) {
	displayName := strings.TrimSpace(stringValue(payload["displayName"], ""))
	mailNickname := strings.TrimSpace(stringValue(payload["mailNickname"], ""))
	userPrincipalName := strings.TrimSpace(stringValue(payload["userPrincipalName"], ""))
	password := strings.TrimSpace(stringValue(payload["password"], ""))
	if displayName == "" || mailNickname == "" || userPrincipalName == "" || password == "" {
		return nil, errors.New("displayName, mailNickname, userPrincipalName and password are required")
	}
	body := map[string]interface{}{
		"accountEnabled":    boolValue(payload["accountEnabled"], true),
		"displayName":       displayName,
		"mailNickname":      mailNickname,
		"userPrincipalName": userPrincipalName,
		"passwordProfile": map[string]interface{}{
			"password":                      password,
			"forceChangePasswordNextSignIn": boolValue(payload["forceChangePasswordNextSignIn"], true),
		},
	}
	for _, key := range []string{"department", "jobTitle", "officeLocation", "usageLocation"} {
		if value := strings.TrimSpace(stringValue(payload[key], "")); value != "" {
			body[key] = value
		}
	}
	return body, nil
}

func normalizeUserPatchPayload(payload map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{}
	for _, key := range []string{"displayName", "mailNickname", "userPrincipalName"} {
		if value, ok := payload[key]; ok {
			trimmed := strings.TrimSpace(stringValue(value, ""))
			if trimmed != "" {
				body[key] = trimmed
			}
		}
	}
	for _, key := range []string{"department", "jobTitle", "officeLocation", "usageLocation"} {
		if value, ok := payload[key]; ok {
			trimmed := strings.TrimSpace(stringValue(value, ""))
			if trimmed != "" {
				body[key] = trimmed
			}
		}
	}
	return body
}
