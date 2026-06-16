package totp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

const backupVersion = 1

var base32SecretPattern = regexp.MustCompile(`^[A-Z2-7]+=*$`)

type Service struct {
	cfg   config.Config
	store *database.Store
}

type Account struct {
	ID                 string  `json:"id"`
	OTPType            string  `json:"otp_type"`
	Issuer             string  `json:"issuer"`
	Account            string  `json:"account"`
	Secret             string  `json:"secret,omitempty"`
	Algorithm          string  `json:"algorithm"`
	Digits             int     `json:"digits"`
	Period             int     `json:"period"`
	Counter            int64   `json:"counter"`
	GroupID            *string `json:"group_id"`
	Icon               *string `json:"icon"`
	Color              *string `json:"color"`
	SortOrder          int     `json:"sort_order"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at,omitempty"`
	SecretEncryptedAt  *string `json:"secret_encrypted_at,omitempty"`
	LastRevealedAt     *string `json:"last_revealed_at,omitempty"`
	HasSecret          bool    `json:"hasSecret"`
	HasEncryptedSecret bool    `json:"hasEncryptedSecret,omitempty"`
	CurrentCode        string  `json:"currentCode,omitempty"`
	Remaining          int     `json:"remaining,omitempty"`
}

type Group struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Icon      *string `json:"icon"`
	Color     *string `json:"color"`
	SortOrder int     `json:"sort_order"`
	CreatedAt string  `json:"created_at"`
}

type accountInput struct {
	ID                string      `json:"id"`
	OTPType           string      `json:"otp_type"`
	Issuer            string      `json:"issuer"`
	Account           string      `json:"account"`
	Secret            string      `json:"secret"`
	Algorithm         string      `json:"algorithm"`
	Digits            int         `json:"digits"`
	Period            int         `json:"period"`
	Counter           int64       `json:"counter"`
	GroupID           interface{} `json:"group_id"`
	Icon              interface{} `json:"icon"`
	Color             interface{} `json:"color"`
	SortOrder         int         `json:"sort_order"`
	CreatedAt         string      `json:"created_at"`
	SecretEncryptedAt string      `json:"secret_encrypted_at"`
	LastRevealedAt    interface{} `json:"last_revealed_at"`
}

type groupInput struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Icon      interface{} `json:"icon"`
	Color     interface{} `json:"color"`
	SortOrder int         `json:"sort_order"`
}

type backupPayload struct {
	Type       string         `json:"type"`
	Version    int            `json:"version"`
	ExportedAt string         `json:"exportedAt"`
	Accounts   []accountInput `json:"accounts"`
	Groups     []groupInput   `json:"groups"`
}

func New(cfg config.Config) *Service {
	return &Service{cfg: cfg, store: database.New(cfg)}
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/totp")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	switch {
	case len(parts) == 1 && parts[0] == "accounts":
		switch r.Method {
		case http.MethodGet:
			s.listAccounts(w, r)
		case http.MethodPost:
			s.createAccount(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "increment":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.incrementHotp(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "code":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.getAccountCode(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "accounts":
		switch r.Method {
		case http.MethodGet:
			s.getAccount(w, r, parts[1])
		case http.MethodPut:
			s.updateAccount(w, r, parts[1])
		case http.MethodDelete:
			s.deleteAccount(w, r, parts[1])
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 1 && parts[0] == "codes":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.listCodes(w, r)
	case len(parts) == 1 && parts[0] == "verify":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.verifyCode(w, r)
	case len(parts) == 1 && parts[0] == "groups":
		switch r.Method {
		case http.MethodGet:
			s.listGroups(w, r)
		case http.MethodPost:
			s.createGroup(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 2 && parts[0] == "groups":
		switch r.Method {
		case http.MethodPut:
			s.updateGroup(w, r, parts[1])
		case http.MethodDelete:
			s.deleteGroup(w, r, parts[1])
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 1 && parts[0] == "export":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.exportBackup(w, r)
	case len(parts) == 2 && parts[0] == "import" && parts[1] == "preview":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.previewImport(w, r)
	case len(parts) == 1 && parts[0] == "import", len(parts) == 2 && parts[0] == "import" && parts[1] == "commit":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.importBackup(w, r)
	case len(parts) == 1 && parts[0] == "order":
		if r.Method != http.MethodPut {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.updateOrder(w, r)
	case len(parts) == 1 && parts[0] == "generate-secret":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		response.OK(w, map[string]string{"secret": generateSecret()})
	case len(parts) == 2 && parts[0] == "extension" && parts[1] == "download":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.downloadExtension(w, r)
	default:
		response.Error(w, http.StatusNotFound, "totp route not implemented")
	}
}

func (s *Service) listAccounts(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	accounts, err := loadAccounts(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if r.URL.Query().Get("withCodes") == "true" {
		codes := generateAllCodes(accounts, time.Now())
		for i := range accounts {
			if code, ok := codes[accounts[i].ID]; ok {
				if text, _ := code["code"].(string); text != "" {
					accounts[i].CurrentCode = text
				}
				if remaining, _ := code["remaining"].(int); remaining > 0 {
					accounts[i].Remaining = remaining
				}
			}
		}
	}
	for i := range accounts {
		accounts[i].Secret = ""
	}
	response.OK(w, accounts)
}

func (s *Service) getAccount(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	account, ok, err := findAccount(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	if r.URL.Query().Get("showSecret") == "true" {
		now := nowISO()
		_, _ = db.ExecContext(r.Context(), `UPDATE totp_accounts SET last_revealed_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
		account.LastRevealedAt = &now
	} else {
		account.Secret = ""
	}
	response.OK(w, account)
}

func (s *Service) createAccount(w http.ResponseWriter, r *http.Request) {
	var input accountInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Secret) == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "密钥不能为空"})
		return
	}
	input.Secret = cleanSecret(input.Secret)
	if !isValidBase32Secret(input.Secret) {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "无效的 Base32 密钥格式"})
		return
	}
	if _, err := generateCodeForInput(input, time.Now()); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "密钥无效，无法生成验证码"})
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	account, err := insertAccount(r.Context(), db, input)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	account.Secret = ""
	response.OK(w, account)
}

func (s *Service) updateAccount(w http.ResponseWriter, r *http.Request, id string) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	existing, ok, err := findAccount(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	if value, ok := payload["secret"]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
		secret := cleanSecret(fmt.Sprint(value))
		testInput := accountInput{
			OTPType:   existing.OTPType,
			Secret:    secret,
			Algorithm: existing.Algorithm,
			Digits:    existing.Digits,
			Period:    existing.Period,
			Counter:   existing.Counter,
		}
		if _, err := generateCodeForInput(testInput, time.Now()); err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "新密钥无效"})
			return
		}
		payload["secret"] = secret
	}
	if err := updateAccountFields(r.Context(), db, id, payload); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	_, ok, err := findAccount(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	if _, err := db.ExecContext(r.Context(), `DELETE FROM totp_accounts WHERE id = ?`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) incrementHotp(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	account, ok, err := findAccount(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	if account.OTPType != "hotp" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "仅 HOTP 账号支持递增"})
		return
	}
	newCounter := account.Counter + 1
	code, err := hotpCode(account.Secret, uint64(newCounter), account.Digits, account.Algorithm)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := nowISO()
	if _, err := db.ExecContext(r.Context(), `UPDATE totp_accounts SET counter = ?, updated_at = ? WHERE id = ?`, newCounter, now, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"code": code, "counter": newCounter})
}

func (s *Service) listCodes(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	accounts, err := loadAccounts(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, generateAllCodes(accounts, time.Now()))
}

func (s *Service) getAccountCode(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	account, ok, err := findAccount(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	code, err := generateCode(account, time.Now())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, code)
}

func (s *Service) verifyCode(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.ID) == "" || strings.TrimSpace(payload.Token) == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少参数"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	account, ok, err := findAccount(r.Context(), db, payload.ID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	valid := false
	if account.OTPType == "hotp" {
		hotpValid, newCounter := verifyHOTP(account.Secret, payload.Token, uint64(account.Counter), account.Digits, account.Algorithm)
		valid = hotpValid
		if valid {
			now := nowISO()
			_, _ = db.ExecContext(r.Context(), `UPDATE totp_accounts SET counter = ?, updated_at = ? WHERE id = ?`, int64(newCounter), now, account.ID)
		}
	} else {
		valid = verifyTOTP(account.Secret, payload.Token, account.Digits, account.Period, account.Algorithm, time.Now())
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "valid": valid})
}

func (s *Service) listGroups(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	groups, err := loadGroups(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, groups)
}

func (s *Service) createGroup(w http.ResponseWriter, r *http.Request) {
	var input groupInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "分组名称不能为空"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	group, err := insertGroup(r.Context(), db, input)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, group)
}

func (s *Service) updateGroup(w http.ResponseWriter, r *http.Request, id string) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	if err := updateGroupFields(r.Context(), db, id, payload); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) deleteGroup(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `UPDATE totp_accounts SET group_id = NULL WHERE group_id = ?`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM totp_groups WHERE id = ?`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) exportBackup(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	payload, err := s.buildBackupPayload(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if r.URL.Query().Get("format") == "uri" || r.URL.Query().Get("plaintext") == "true" {
		uris := make([]string, 0, len(payload.Accounts))
		for _, account := range payload.Accounts {
			uris = append(uris, generateURI(account))
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "format": "uri", "data": uris})
		return
	}
	encrypted, err := secure.EncryptJSON(payload)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"format":  "encrypted-backup",
		"data": map[string]interface{}{
			"version":      backupVersion,
			"exportedAt":   payload.ExportedAt,
			"accountCount": len(payload.Accounts),
			"groupCount":   len(payload.Groups),
			"payload":      encrypted,
		},
	})
}

func (s *Service) previewImport(w http.ResponseWriter, r *http.Request) {
	importItems, backup, err := decodeImportItems(r)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	existing, err := loadAccounts(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	existingKeys := map[string]bool{}
	for _, account := range existing {
		existingKeys[accountKey(account.Issuer, account.Account)] = true
	}
	seen := map[string]bool{}
	items := make([]map[string]interface{}, 0, len(importItems))
	for i, item := range importItems {
		key := accountKey(item.Issuer, item.Account)
		duplicateExisting := existingKeys[key]
		duplicateInBatch := seen[key]
		seen[key] = true
		valid := strings.TrimSpace(item.Secret) != ""
		errorText := interface{}(nil)
		if !valid {
			errorText = "缺少密钥"
		}
		items = append(items, map[string]interface{}{
			"index":             i,
			"issuer":            defaultString(item.Issuer, "未知"),
			"account":           item.Account,
			"otp_type":          defaultString(item.OTPType, "totp"),
			"valid":             valid,
			"duplicate":         duplicateExisting || duplicateInBatch,
			"duplicateExisting": duplicateExisting,
			"duplicateInBatch":  duplicateInBatch,
			"error":             errorText,
		})
	}
	backupMeta := interface{}(nil)
	if backup != nil {
		backupMeta = map[string]interface{}{
			"version":    backup.Version,
			"exportedAt": backup.ExportedAt,
			"groupCount": len(backup.Groups),
		}
	}
	validCount := 0
	duplicates := 0
	for _, item := range items {
		if item["duplicate"] == true {
			duplicates++
			continue
		}
		if item["valid"] == true {
			validCount++
		}
	}
	response.OK(w, map[string]interface{}{
		"total":      len(items),
		"valid":      validCount,
		"duplicates": duplicates,
		"errors":     []string{},
		"items":      items,
		"backup":     backupMeta,
	})
}

func (s *Service) importBackup(w http.ResponseWriter, r *http.Request) {
	items, backup, err := decodeImportItems(r)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(items) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "没有有效的导入数据"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	result := map[string]interface{}{"success": 0, "failed": 0, "errors": []string{}}
	errorsOut := []string{}
	groupsCreated := 0
	if backup != nil {
		for _, group := range backup.Groups {
			if strings.TrimSpace(group.Name) == "" {
				continue
			}
			exists, err := groupExists(r.Context(), db, group.ID)
			if err != nil {
				errorsOut = append(errorsOut, "group:"+group.Name+": "+err.Error())
				continue
			}
			if !exists {
				if _, err := insertGroup(r.Context(), db, group); err != nil {
					errorsOut = append(errorsOut, "group:"+group.Name+": "+err.Error())
					continue
				}
				groupsCreated++
			}
		}
		result["groups"] = groupsCreated
	}
	successCount := 0
	failedCount := 0
	for _, item := range items {
		if strings.TrimSpace(item.Secret) == "" {
			failedCount++
			errorsOut = append(errorsOut, "缺少密钥: "+defaultString(item.Issuer, "未知"))
			continue
		}
		if _, err := insertAccount(r.Context(), db, item); err != nil {
			failedCount++
			errorsOut = append(errorsOut, defaultString(item.Issuer, "未知")+": "+err.Error())
			continue
		}
		successCount++
	}
	result["success"] = successCount
	result["failed"] = failedCount
	result["errors"] = errorsOut
	response.OK(w, result)
}

func (s *Service) updateOrder(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		OrderedIDs []string `json:"orderedIds"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	if payload.OrderedIDs == nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "无效的排序数据"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	now := nowISO()
	for i, id := range payload.OrderedIDs {
		if _, err := tx.ExecContext(r.Context(), `UPDATE totp_accounts SET sort_order = ?, updated_at = ? WHERE id = ?`, i, now, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) downloadExtension(w http.ResponseWriter, r *http.Request) {
	pluginDir := filepath.Join(repoRoot(s.cfg), "plugin")
	info, err := os.Stat(pluginDir)
	if err != nil || !info.IsDir() {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "浏览器扩展目录不存在"})
		return
	}
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	err = filepath.WalkDir(pluginDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(pluginDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		writer, err := zipWriter.Create(rel)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(writer, file)
		return err
	})
	if closeErr := zipWriter.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "压缩失败: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="api-monitor-2fa-extension.zip"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := ensureSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS totp_groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			icon TEXT,
			color TEXT,
			sort_order INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS totp_accounts (
			id TEXT PRIMARY KEY,
			otp_type TEXT DEFAULT 'totp',
			issuer TEXT NOT NULL,
			account TEXT NOT NULL,
			secret TEXT NOT NULL,
			secret_encrypted_at DATETIME,
			last_revealed_at DATETIME,
			algorithm TEXT DEFAULT 'SHA1',
			digits INTEGER DEFAULT 6,
			period INTEGER DEFAULT 30,
			counter INTEGER DEFAULT 0,
			group_id TEXT,
			icon TEXT,
			color TEXT,
			sort_order INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_totp_sort ON totp_accounts(sort_order, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_totp_group ON totp_accounts(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_totp_groups_sort ON totp_groups(sort_order)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure totp schema: %w", err)
		}
	}
	return ensureColumns(ctx, db, "totp_accounts", []columnDef{
		{"otp_type", "ALTER TABLE totp_accounts ADD COLUMN otp_type TEXT DEFAULT 'totp'"},
		{"secret_encrypted_at", "ALTER TABLE totp_accounts ADD COLUMN secret_encrypted_at DATETIME"},
		{"last_revealed_at", "ALTER TABLE totp_accounts ADD COLUMN last_revealed_at DATETIME"},
		{"algorithm", "ALTER TABLE totp_accounts ADD COLUMN algorithm TEXT DEFAULT 'SHA1'"},
		{"digits", "ALTER TABLE totp_accounts ADD COLUMN digits INTEGER DEFAULT 6"},
		{"period", "ALTER TABLE totp_accounts ADD COLUMN period INTEGER DEFAULT 30"},
		{"counter", "ALTER TABLE totp_accounts ADD COLUMN counter INTEGER DEFAULT 0"},
		{"group_id", "ALTER TABLE totp_accounts ADD COLUMN group_id TEXT"},
		{"icon", "ALTER TABLE totp_accounts ADD COLUMN icon TEXT"},
		{"color", "ALTER TABLE totp_accounts ADD COLUMN color TEXT"},
		{"sort_order", "ALTER TABLE totp_accounts ADD COLUMN sort_order INTEGER DEFAULT 0"},
		{"updated_at", "ALTER TABLE totp_accounts ADD COLUMN updated_at DATETIME DEFAULT CURRENT_TIMESTAMP"},
	})
}

type columnDef struct {
	name string
	sql  string
}

func ensureColumns(ctx context.Context, db *sql.DB, table string, columns []columnDef) error {
	existing, err := tableColumns(ctx, db, table)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if !existing[column.name] {
			if _, err := db.ExecContext(ctx, column.sql); err != nil {
				return fmt.Errorf("add %s.%s: %w", table, column.name, err)
			}
		}
	}
	return nil
}

func loadAccounts(ctx context.Context, db *sql.DB) ([]Account, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, otp_type, issuer, account, secret, secret_encrypted_at, last_revealed_at,
			algorithm, digits, period, counter, group_id, icon, color, sort_order, created_at, updated_at
		FROM totp_accounts
		ORDER BY sort_order ASC, created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("load totp accounts: %w", err)
	}
	defer rows.Close()
	accounts := []Account{}
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return accounts, nil
}

func findAccount(ctx context.Context, db *sql.DB, id string) (Account, bool, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, otp_type, issuer, account, secret, secret_encrypted_at, last_revealed_at,
			algorithm, digits, period, counter, group_id, icon, color, sort_order, created_at, updated_at
		FROM totp_accounts
		WHERE id = ?
	`, id)
	account, err := scanAccount(row)
	if err == nil {
		return account, true, nil
	}
	if err == sql.ErrNoRows {
		return Account{}, false, nil
	}
	return Account{}, false, err
}

type accountScanner interface {
	Scan(dest ...interface{}) error
}

func scanAccount(scanner accountScanner) (Account, error) {
	var account Account
	var otpType, algorithm sql.NullString
	var secret string
	var secretEncryptedAt, lastRevealedAt, groupID, icon, color, createdAt, updatedAt sql.NullString
	var digits, period, counter, sortOrder sql.NullInt64
	if err := scanner.Scan(
		&account.ID,
		&otpType,
		&account.Issuer,
		&account.Account,
		&secret,
		&secretEncryptedAt,
		&lastRevealedAt,
		&algorithm,
		&digits,
		&period,
		&counter,
		&groupID,
		&icon,
		&color,
		&sortOrder,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Account{}, err
	}
	account.OTPType = defaultString(otpType.String, "totp")
	account.Secret = secure.SecureDecrypt(secret)
	account.Algorithm = defaultString(algorithm.String, "SHA1")
	account.Digits = intOrDefault(digits, 6)
	account.Period = intOrDefault(period, 30)
	account.Counter = int64OrDefault(counter, 0)
	account.GroupID = nullStringPtr(groupID)
	account.Icon = nullStringPtr(icon)
	account.Color = nullStringPtr(color)
	account.SortOrder = intOrDefault(sortOrder, 0)
	account.CreatedAt = nullString(createdAt, nowISO())
	account.UpdatedAt = nullString(updatedAt, "")
	account.SecretEncryptedAt = nullStringPtr(secretEncryptedAt)
	account.LastRevealedAt = nullStringPtr(lastRevealedAt)
	account.HasSecret = secret != ""
	account.HasEncryptedSecret = secure.IsEncrypted(secret)
	return account, nil
}

func loadGroups(ctx context.Context, db *sql.DB) ([]Group, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, icon, color, sort_order, created_at
		FROM totp_groups
		ORDER BY sort_order ASC, created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("load totp groups: %w", err)
	}
	defer rows.Close()
	groups := []Group{}
	for rows.Next() {
		var group Group
		var icon, color, createdAt sql.NullString
		var sortOrder sql.NullInt64
		if err := rows.Scan(&group.ID, &group.Name, &icon, &color, &sortOrder, &createdAt); err != nil {
			return nil, err
		}
		group.Icon = nullStringPtr(icon)
		group.Color = nullStringPtr(color)
		group.SortOrder = intOrDefault(sortOrder, 0)
		group.CreatedAt = nullString(createdAt, nowISO())
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

func insertAccount(ctx context.Context, db *sql.DB, input accountInput) (Account, error) {
	now := nowISO()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = randomID("totp")
	}
	otpType := defaultString(input.OTPType, "totp")
	issuer := defaultString(input.Issuer, "未知")
	accountName := input.Account
	secret := cleanSecret(input.Secret)
	encryptedSecret, err := secure.SecureEncrypt(secret)
	if err != nil {
		return Account{}, err
	}
	algorithm := defaultString(input.Algorithm, "SHA1")
	digits := input.Digits
	if digits == 0 {
		digits = 6
	}
	period := input.Period
	if period == 0 {
		period = 30
	}
	counter := input.Counter
	groupID := optionalString(input.GroupID)
	icon := optionalString(input.Icon)
	color := optionalString(input.Color)
	createdAt := input.CreatedAt
	if createdAt == "" {
		createdAt = now
	}
	secretEncryptedAt := input.SecretEncryptedAt
	if secretEncryptedAt == "" {
		secretEncryptedAt = now
	}
	lastRevealedAt := optionalString(input.LastRevealedAt)
	_, err = db.ExecContext(ctx, `
		INSERT INTO totp_accounts (
			id, otp_type, issuer, account, secret, secret_encrypted_at, last_revealed_at,
			algorithm, digits, period, counter, group_id, icon, color, sort_order, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, otpType, issuer, accountName, encryptedSecret, secretEncryptedAt, lastRevealedAt, algorithm, digits, period, counter, groupID, icon, color, input.SortOrder, createdAt, now)
	if err != nil {
		return Account{}, fmt.Errorf("create totp account: %w", err)
	}
	account, _, err := findAccount(ctx, db, id)
	return account, err
}

func updateAccountFields(ctx context.Context, db *sql.DB, id string, payload map[string]interface{}) error {
	allowed := map[string]string{
		"otp_type":            "otp_type",
		"issuer":              "issuer",
		"account":             "account",
		"secret":              "secret",
		"secret_encrypted_at": "secret_encrypted_at",
		"last_revealed_at":    "last_revealed_at",
		"algorithm":           "algorithm",
		"digits":              "digits",
		"period":              "period",
		"counter":             "counter",
		"group_id":            "group_id",
		"icon":                "icon",
		"color":               "color",
		"sort_order":          "sort_order",
	}
	sets := []string{"updated_at = ?"}
	args := []interface{}{nowISO()}
	for key, column := range allowed {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if key == "secret" {
			encrypted, err := secure.SecureEncrypt(cleanSecret(fmt.Sprint(value)))
			if err != nil {
				return err
			}
			value = encrypted
			if _, ok := payload["secret_encrypted_at"]; !ok {
				sets = append(sets, "secret_encrypted_at = ?")
				args = append(args, nowISO())
			}
		}
		if key == "group_id" || key == "icon" || key == "color" || key == "last_revealed_at" {
			value = optionalString(value)
		}
		sets = append(sets, column+" = ?")
		args = append(args, value)
	}
	if len(sets) == 1 {
		return nil
	}
	args = append(args, id)
	_, err := db.ExecContext(ctx, `UPDATE totp_accounts SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return fmt.Errorf("update totp account: %w", err)
	}
	return nil
}

func insertGroup(ctx context.Context, db *sql.DB, input groupInput) (Group, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = randomID("group")
	}
	now := nowISO()
	_, err := db.ExecContext(ctx, `
		INSERT INTO totp_groups (id, name, icon, color, sort_order, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, input.Name, optionalString(input.Icon), optionalString(input.Color), input.SortOrder, now)
	if err != nil {
		return Group{}, fmt.Errorf("create totp group: %w", err)
	}
	return Group{
		ID:        id,
		Name:      input.Name,
		Icon:      stringPtrFromInterface(input.Icon),
		Color:     stringPtrFromInterface(input.Color),
		SortOrder: input.SortOrder,
		CreatedAt: now,
	}, nil
}

func updateGroupFields(ctx context.Context, db *sql.DB, id string, payload map[string]interface{}) error {
	allowed := map[string]string{"name": "name", "icon": "icon", "color": "color", "sort_order": "sort_order"}
	sets := []string{}
	args := []interface{}{}
	for key, column := range allowed {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if key == "icon" || key == "color" {
			value = optionalString(value)
		}
		sets = append(sets, column+" = ?")
		args = append(args, value)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := db.ExecContext(ctx, `UPDATE totp_groups SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

func groupExists(ctx context.Context, db *sql.DB, id string) (bool, error) {
	if id == "" {
		return false, nil
	}
	var found string
	err := db.QueryRowContext(ctx, `SELECT id FROM totp_groups WHERE id = ?`, id).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (s *Service) buildBackupPayload(ctx context.Context, db *sql.DB) (backupPayload, error) {
	accounts, err := loadAccounts(ctx, db)
	if err != nil {
		return backupPayload{}, err
	}
	groups, err := loadGroups(ctx, db)
	if err != nil {
		return backupPayload{}, err
	}
	accountItems := make([]accountInput, 0, len(accounts))
	for _, account := range accounts {
		accountItems = append(accountItems, accountInput{
			ID:        account.ID,
			OTPType:   account.OTPType,
			Issuer:    account.Issuer,
			Account:   account.Account,
			Secret:    account.Secret,
			Algorithm: account.Algorithm,
			Digits:    account.Digits,
			Period:    account.Period,
			Counter:   account.Counter,
			GroupID:   account.GroupID,
			Icon:      account.Icon,
			Color:     account.Color,
			SortOrder: account.SortOrder,
			CreatedAt: account.CreatedAt,
		})
	}
	groupItems := make([]groupInput, 0, len(groups))
	for _, group := range groups {
		groupItems = append(groupItems, groupInput{
			ID:        group.ID,
			Name:      group.Name,
			Icon:      group.Icon,
			Color:     group.Color,
			SortOrder: group.SortOrder,
		})
	}
	return backupPayload{
		Type:       "api-monitor-totp-backup",
		Version:    backupVersion,
		ExportedAt: nowISO(),
		Accounts:   accountItems,
		Groups:     groupItems,
	}, nil
}

func decodeImportItems(r *http.Request) ([]accountInput, *backupPayload, error) {
	var raw map[string]json.RawMessage
	if !decodeJSON(nil, r, &raw) {
		return nil, nil, fmt.Errorf("request parameter validation failed")
	}
	items := []accountInput{}
	var backup *backupPayload
	if value, ok := raw["backup"]; ok {
		parsed, err := decodeBackupPayload(value)
		if err != nil {
			return nil, nil, err
		}
		backup = parsed
		items = append(items, parsed.Accounts...)
	} else if value, ok := raw["payload"]; ok {
		parsed, err := decodeBackupPayload(value)
		if err != nil {
			return nil, nil, err
		}
		backup = parsed
		items = append(items, parsed.Accounts...)
	}
	if value, ok := raw["uris"]; ok {
		var uris []string
		if err := json.Unmarshal(value, &uris); err == nil {
			for _, uri := range uris {
				if parsed, ok := parseURI(uri); ok {
					items = append(items, parsed)
				}
			}
		}
	}
	if value, ok := raw["accounts"]; ok {
		var accounts []accountInput
		if err := json.Unmarshal(value, &accounts); err == nil {
			items = append(items, accounts...)
		}
	}
	return items, backup, nil
}

func decodeBackupPayload(raw json.RawMessage) (*backupPayload, error) {
	var direct backupPayload
	if len(raw) > 0 && raw[0] == '"' {
		var encrypted string
		if err := json.Unmarshal(raw, &encrypted); err != nil {
			return nil, err
		}
		if err := secure.DecryptJSON(encrypted, &direct); err != nil {
			return nil, fmt.Errorf("Invalid encrypted TOTP backup")
		}
	} else if err := json.Unmarshal(raw, &direct); err != nil {
		return nil, err
	}
	if direct.Type != "" && direct.Type != "api-monitor-totp-backup" {
		return nil, fmt.Errorf("Invalid encrypted TOTP backup")
	}
	return &direct, nil
}

func generateAllCodes(accounts []Account, now time.Time) map[string]map[string]interface{} {
	result := map[string]map[string]interface{}{}
	for _, account := range accounts {
		code, err := generateCode(account, now)
		if err != nil {
			result[account.ID] = map[string]interface{}{"code": nil, "error": err.Error()}
			continue
		}
		result[account.ID] = code
	}
	return result
}

func generateCode(account Account, now time.Time) (map[string]interface{}, error) {
	if account.OTPType == "hotp" {
		code, err := hotpCode(account.Secret, uint64(account.Counter), account.Digits, account.Algorithm)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"code": code, "counter": account.Counter, "type": "hotp"}, nil
	}
	period := account.Period
	if period <= 0 {
		period = 30
	}
	code, err := totpCode(account.Secret, now, account.Digits, period, account.Algorithm)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"code": code, "remaining": period - int(now.Unix()%int64(period)), "type": "totp"}, nil
}

func generateCodeForInput(input accountInput, now time.Time) (string, error) {
	digits := input.Digits
	if digits == 0 {
		digits = 6
	}
	if input.OTPType == "hotp" {
		return hotpCode(input.Secret, uint64(input.Counter), digits, input.Algorithm)
	}
	period := input.Period
	if period == 0 {
		period = 30
	}
	return totpCode(input.Secret, now, digits, period, input.Algorithm)
}

func totpCode(secret string, now time.Time, digits, period int, algorithm string) (string, error) {
	if period <= 0 {
		period = 30
	}
	return hotpCode(secret, uint64(now.Unix()/int64(period)), digits, algorithm)
}

func hotpCode(secret string, counter uint64, digits int, algorithm string) (string, error) {
	key, err := decodeBase32(secret)
	if err != nil {
		return "", err
	}
	if digits <= 0 {
		digits = 6
	}
	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)
	mac := hmac.New(hashFactory(algorithm), key)
	mac.Write(counterBytes[:])
	sum := mac.Sum(nil)
	offset := int(sum[len(sum)-1] & 0x0f)
	binaryCode := (int(sum[offset]&0x7f) << 24) |
		(int(sum[offset+1]&0xff) << 16) |
		(int(sum[offset+2]&0xff) << 8) |
		int(sum[offset+3]&0xff)
	mod := 1
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, binaryCode%mod), nil
}

func verifyTOTP(secret, token string, digits, period int, algorithm string, now time.Time) bool {
	token = strings.ReplaceAll(strings.TrimSpace(token), " ", "")
	if period <= 0 {
		period = 30
	}
	baseCounter := now.Unix() / int64(period)
	for offset := int64(-1); offset <= 1; offset++ {
		code, err := hotpCode(secret, uint64(baseCounter+offset), digits, algorithm)
		if err == nil && subtleEqual(code, token) {
			return true
		}
	}
	return false
}

func verifyHOTP(secret, token string, counter uint64, digits int, algorithm string) (bool, uint64) {
	token = strings.ReplaceAll(strings.TrimSpace(token), " ", "")
	for i := uint64(0); i <= 10; i++ {
		code, err := hotpCode(secret, counter+i, digits, algorithm)
		if err == nil && subtleEqual(code, token) {
			return true, counter + i + 1
		}
	}
	return false, counter
}

func hashFactory(algorithm string) func() hash.Hash {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(algorithm, "-", ""), "_", ""))
	switch normalized {
	case "sha256":
		return sha256.New
	case "sha512":
		return sha512.New
	default:
		return sha1.New
	}
}

func decodeBase32(secret string) ([]byte, error) {
	clean := cleanSecret(secret)
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(clean, "="))
}

func generateSecret() string {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "JBSWY3DPEHPK3PXP"
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
}

func generateURI(account accountInput) string {
	otpType := defaultString(account.OTPType, "totp")
	issuer := defaultString(account.Issuer, "未知")
	params := url.Values{}
	params.Set("secret", account.Secret)
	params.Set("issuer", issuer)
	params.Set("algorithm", defaultString(account.Algorithm, "SHA1"))
	if account.Digits > 0 {
		params.Set("digits", strconv.Itoa(account.Digits))
	}
	if otpType == "hotp" {
		params.Set("counter", strconv.FormatInt(account.Counter, 10))
	} else if account.Period > 0 {
		params.Set("period", strconv.Itoa(account.Period))
	}
	return fmt.Sprintf("otpauth://%s/%s:%s?%s", otpType, url.PathEscape(issuer), url.PathEscape(account.Account), params.Encode())
}

func parseURI(value string) (accountInput, bool) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "otpauth" {
		return accountInput{}, false
	}
	otpType := parsed.Host
	if otpType == "" {
		otpType = "totp"
	}
	label, _ := url.PathUnescape(strings.TrimPrefix(parsed.Path, "/"))
	params := parsed.Query()
	issuer := params.Get("issuer")
	account := label
	if strings.Contains(label, ":") {
		parts := strings.Split(label, ":")
		if issuer == "" {
			issuer = parts[0]
		}
		account = strings.Join(parts[1:], ":")
	}
	digits, _ := strconv.Atoi(params.Get("digits"))
	if digits == 0 {
		digits = 6
	}
	period, _ := strconv.Atoi(params.Get("period"))
	if period == 0 {
		period = 30
	}
	counter, _ := strconv.ParseInt(params.Get("counter"), 10, 64)
	return accountInput{
		OTPType:   otpType,
		Issuer:    issuer,
		Account:   account,
		Secret:    cleanSecret(params.Get("secret")),
		Algorithm: defaultString(params.Get("algorithm"), "SHA1"),
		Digits:    digits,
		Period:    period,
		Counter:   counter,
	}, true
}

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+quoteIdentifier(table)+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		if w != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "request parameter validation failed"})
		}
		return false
	}
	return true
}

func optionalString(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case *string:
		if typed == nil || strings.TrimSpace(*typed) == "" {
			return nil
		}
		return *typed
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return typed
	case json.Number:
		return typed.String()
	case float64:
		if typed == 0 {
			return nil
		}
		return strconv.FormatInt(int64(typed), 10)
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			return nil
		}
		return text
	}
}

func stringPtrFromInterface(value interface{}) *string {
	optional := optionalString(value)
	if optional == nil {
		return nil
	}
	text := fmt.Sprint(optional)
	return &text
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullString(value sql.NullString, fallback string) string {
	if value.Valid {
		return value.String
	}
	return fallback
}

func intOrDefault(value sql.NullInt64, fallback int) int {
	if value.Valid {
		return int(value.Int64)
	}
	return fallback
}

func int64OrDefault(value sql.NullInt64, fallback int64) int64 {
	if value.Valid {
		return value.Int64
	}
	return fallback
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func cleanSecret(value string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
}

func isValidBase32Secret(value string) bool {
	clean := cleanSecret(value)
	if !base32SecretPattern.MatchString(clean) {
		return false
	}
	_, err := decodeBase32(clean)
	return err == nil
}

func accountKey(issuer, account string) string {
	return strings.ToLower(issuer) + "|" + strings.ToLower(account)
}

func randomID(prefix string) string {
	raw := make([]byte, 5)
	_, _ = rand.Read(raw)
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(raw))
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func subtleEqual(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

func repoRoot(cfg config.Config) string {
	dataDir := filepath.Clean(cfg.DataDir)
	if strings.EqualFold(filepath.Base(dataDir), "data") {
		return filepath.Dir(dataDir)
	}
	wd, err := os.Getwd()
	if err == nil {
		for {
			if exists(filepath.Join(wd, "package.json")) && exists(filepath.Join(wd, "plugin")) {
				return wd
			}
			parent := filepath.Dir(wd)
			if parent == wd {
				break
			}
			wd = parent
		}
	}
	return filepath.Dir(dataDir)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
