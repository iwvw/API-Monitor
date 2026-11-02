package m365

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) groups(w http.ResponseWriter, r *http.Request, idText string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		result := map[string]interface{}{}
		query := url.Values{}
		query.Set("$top", clampPositive(r.URL.Query().Get("top"), 50, 200))
		query.Set("$select", "id,displayName,mail,mailEnabled,securityEnabled,createdDateTime")
		search := strings.TrimSpace(r.URL.Query().Get("search"))
		if search != "" {
			query.Set("$search", fmt.Sprintf(`"displayName:%s"`, escapeSearch(search)))
		}
		headers := map[string]string{"ConsistencyLevel": "eventual"}
		if err := s.graphJSON(r.Context(), account, http.MethodGet, "/groups?"+query.Encode(), nil, headers, &result); err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"items": objectArray(result["value"])})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		name := strings.TrimSpace(stringValue(payload["displayName"], ""))
		mailNickname := strings.TrimSpace(stringValue(payload["mailNickname"], ""))
		if name == "" || mailNickname == "" {
			response.Error(w, http.StatusBadRequest, "displayName and mailNickname are required")
			return
		}
		body := map[string]interface{}{
			"displayName":     name,
			"mailEnabled":     boolValue(payload["mailEnabled"], false),
			"mailNickname":    mailNickname,
			"securityEnabled": boolValue(payload["securityEnabled"], true),
		}
		created := map[string]interface{}{}
		if err := s.graphJSON(r.Context(), account, http.MethodPost, "/groups", body, nil, &created); err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		response.OK(w, created)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) groupMembers(w http.ResponseWriter, r *http.Request, idText, groupID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	result := map[string]interface{}{}
	path := "/groups/" + url.PathEscape(groupID) + "/members?$top=100&$select=id,displayName,userPrincipalName,mail"
	if err := s.graphJSON(r.Context(), account, http.MethodGet, path, nil, nil, &result); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"items": objectArray(result["value"])})
}

func (s *Service) addGroupMember(w http.ResponseWriter, r *http.Request, idText, groupID, memberID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	body := map[string]interface{}{
		"@odata.id": strings.TrimRight(s.graphBase, "/") + "/directoryObjects/" + url.PathEscape(memberID),
	}
	if err := s.graphJSON(r.Context(), account, http.MethodPost, "/groups/"+url.PathEscape(groupID)+"/members/$ref", body, nil, nil); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"added": true})
}

func (s *Service) removeGroupMember(w http.ResponseWriter, r *http.Request, idText, groupID, memberID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	if err := s.graphJSON(r.Context(), account, http.MethodDelete, "/groups/"+url.PathEscape(groupID)+"/members/"+url.PathEscape(memberID)+"/$ref", nil, nil, nil); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"removed": true})
}

func (s *Service) assignGroupLicense(w http.ResponseWriter, r *http.Request, idText, groupID string) {
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
	if err := s.graphJSON(r.Context(), account, http.MethodPost, "/groups/"+url.PathEscape(groupID)+"/assignLicense", body, nil, &result); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, result)
}
