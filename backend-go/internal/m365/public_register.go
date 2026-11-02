package m365

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) newPublicInvite(w http.ResponseWriter, r *http.Request, code string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	record, err := loadInviteCodeByCode(r.Context(), db, code)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "invite code not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, publicInviteCodePayload(r.Context(), db, record, time.Now().UTC()))
}

func (s *Service) newPublicRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.newPublicRegisterDescriptor(w, r)
		return
	}

	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	code := strings.TrimSpace(stringValue(payload["code"], ""))
	batchID := strings.TrimSpace(stringValue(payload["batch"], ""))
	mailNickname := strings.TrimSpace(stringValue(payload["mailNickname"], ""))
	displayName := strings.TrimSpace(stringValue(payload["displayName"], mailNickname))
	if (code == "" && batchID == "") || mailNickname == "" {
		response.Error(w, http.StatusBadRequest, "code or batch and mailNickname are required")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	now := time.Now().UTC()
	inviteCode := inviteCodeRecord{}
	switch {
	case code != "":
		inviteCode, err = loadInviteCodeByCode(r.Context(), db, code)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.Error(w, http.StatusNotFound, "invite code not found")
				return
			}
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if available, reason := evaluateInviteCodeAvailability(inviteCode, now); !available {
			response.Error(w, http.StatusBadRequest, "invite code unavailable: "+reason)
			return
		}
	case batchID != "":
		items, batchErr := loadInviteCodesByBatch(r.Context(), db, batchID)
		if batchErr != nil {
			if errors.Is(batchErr, sql.ErrNoRows) {
				response.Error(w, http.StatusNotFound, "invite batch not found")
				return
			}
			response.Error(w, http.StatusInternalServerError, batchErr.Error())
			return
		}
		selected, available, reason := pickInviteCodeFromBatch(items, now)
		if !available {
			response.Error(w, http.StatusBadRequest, "invite batch unavailable: "+reason)
			return
		}
		inviteCode = selected
	}

	target, err := resolveInviteRegistrationTarget(r.Context(), db, inviteCodeToInviteRecord(inviteCode), payload)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	account, err := s.loadDecryptedAccount(r.Context(), strconv.FormatInt(target.ID, 10))
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	if displayName == "" {
		displayName = mailNickname
	}
	userPrincipalName := mailNickname + "@" + target.Domain
	const maxPasswordAttempts = 5
	generatedPassword := ""
	created := map[string]interface{}{}
	var createErr error
	for attempt := 0; attempt < maxPasswordAttempts; attempt++ {
		generatedPassword, err = generateTemporaryPassword()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "generate temporary password: "+err.Error())
			return
		}
		created = map[string]interface{}{}
		body := map[string]interface{}{
			"accountEnabled":    true,
			"displayName":       displayName,
			"mailNickname":      mailNickname,
			"userPrincipalName": userPrincipalName,
			"passwordProfile": map[string]interface{}{
				"password":                      generatedPassword,
				"forceChangePasswordNextSignIn": inviteCode.ForceChangePasswordNextSignIn,
			},
		}
		body["usageLocation"] = resolvedInviteUsageLocation(inviteCode.UsageLocation)
		createErr = s.graphJSON(r.Context(), account, http.MethodPost, "/users", body, nil, &created)
		if createErr == nil || !isPasswordComplexityError(createErr) {
			break
		}
	}
	graphUserID := strings.TrimSpace(stringValue(created["id"], ""))
	status := "success"
	warning := ""
	errorMessage := ""
	if createErr != nil {
		status = "failed"
		errorMessage = createErr.Error()
	} else if len(inviteCode.SKUIDs) > 0 && graphUserID != "" {
		assignBody := map[string]interface{}{
			"addLicenses":    licenseAssignmentsFromIDs(inviteCode.SKUIDs),
			"removeLicenses": []string{},
		}
		if err := s.graphJSON(r.Context(), account, http.MethodPost, "/users/"+url.PathEscape(graphUserID)+"/assignLicense", assignBody, nil, nil); err != nil {
			status = "partial"
			errorMessage = err.Error()
			warning = err.Error()
		}
	}

	if persistErr := persistPublicPageRegistration(r.Context(), db, inviteCode, target.ID, target.Name, displayName, userPrincipalName, graphUserID, status, errorMessage); persistErr != nil {
		if errors.Is(persistErr, errInviteCodeExhausted) {
			// 并发竞争的第二个注册：邀请码额度已被对方占用
			response.Error(w, http.StatusConflict, "邀请码已被使用")
			return
		}
		if warning != "" {
			warning += "; "
		}
		warning += "local record save failed"
	}

	if status == "failed" {
		response.Error(w, http.StatusBadGateway, errorMessage)
		return
	}

	response.OK(w, map[string]interface{}{
		"id":                            graphUserID,
		"status":                        status,
		"accountId":                     target.ID,
		"domain":                        target.Domain,
		"userPrincipalName":             userPrincipalName,
		"initialPassword":               generatedPassword,
		"forceChangePasswordNextSignIn": inviteCode.ForceChangePasswordNextSignIn,
		"warning":                       emptyToNil(warning),
	})
}

func (s *Service) newPublicRegisterDescriptor(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	payload := map[string]interface{}{
		"method":       "POST",
		"fields":       []string{"code", "batch", "mailNickname", "displayName", "accountId", "domain"},
		"passwordMode": "generated",
	}
	now := time.Now().UTC()
	if code := strings.TrimSpace(r.URL.Query().Get("code")); code != "" {
		record, err := loadInviteCodeByCode(r.Context(), db, code)
		if err == nil {
			payload["invite"] = publicInviteCodePayload(r.Context(), db, record, now)
		}
	} else if batchID := strings.TrimSpace(r.URL.Query().Get("batch")); batchID != "" {
		items, err := loadInviteCodesByBatch(r.Context(), db, batchID)
		if err == nil {
			payload["invite"] = publicInviteBatchPayload(r.Context(), db, items, now)
		}
	}
	response.OK(w, payload)
}

func publicInviteCodePayload(ctx context.Context, db *sql.DB, record inviteCodeRecord, now time.Time) map[string]interface{} {
	payload := inviteCodeToMap(record, now)
	targets := resolveInviteTargetsFromAccounts(inviteCodeToInviteRecord(record), mustLoadAccounts(ctx, db))
	targetItems := make([]map[string]interface{}, 0, len(targets))
	for _, target := range targets {
		targetItems = append(targetItems, map[string]interface{}{
			"accountId":   target.ID,
			"accountName": target.Name,
			"domain":      target.Domain,
		})
	}
	payload["targets"] = targetItems
	payload["targetCount"] = len(targetItems)
	return payload
}

func publicInviteBatchPayload(ctx context.Context, db *sql.DB, items []inviteCodeRecord, now time.Time) map[string]interface{} {
	if len(items) == 0 {
		return map[string]interface{}{
			"mode":           "batch",
			"inviteCount":    0,
			"usedCount":      0,
			"availableCount": 0,
			"available":      false,
		}
	}

	primary := items[0]
	payload := publicInviteCodePayload(ctx, db, primary, now)
	usedCount := int64(0)
	availableCount := int64(0)
	availabilityReason := ""
	for _, item := range items {
		if item.UsedCount > 0 {
			usedCount++
		}
		available, reason := evaluateInviteCodeAvailability(item, now)
		if available {
			availableCount++
		} else if availabilityReason == "" {
			availabilityReason = reason
		}
	}

	payload["mode"] = "batch"
	payload["batchId"] = emptyToNil(primary.BatchID)
	payload["inviteCount"] = len(items)
	payload["usedCount"] = usedCount
	payload["availableCount"] = availableCount
	payload["used"] = availableCount == 0
	payload["available"] = availableCount > 0
	payload["availabilityReason"] = emptyToNil(availabilityReason)
	delete(payload, "code")
	delete(payload, "id")
	return payload
}

// errInviteCodeExhausted 表示并发注册竞争下邀请码额度已被另一方占用。
var errInviteCodeExhausted = errors.New("invite code exhausted")
