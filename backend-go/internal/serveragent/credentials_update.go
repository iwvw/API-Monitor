package serveragent

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

// updateCredential handles PUT /api/server/credentials/{id}
func (s *Service) updateCredential(w http.ResponseWriter, r *http.Request, db *sql.DB, idStr string) {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid credential ID")
		return
	}

	var req struct {
		Name     string `json:"name"`
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.Username == "" {
		response.Error(w, http.StatusBadRequest, "name and username are required")
		return
	}

	// 如果提供了密码，加密它
	var encPassword string
	if req.Password != "" {
		encPassword, err = secure.SecureEncrypt(req.Password)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "encryption failed: "+err.Error())
			return
		}
	}

	// 更新凭据
	var res sql.Result
	if req.Password != "" {
		res, err = db.ExecContext(r.Context(),
			"UPDATE server_credentials SET name = ?, username = ?, password = ?, updated_at = datetime('now') WHERE id = ?",
			req.Name, req.Username, encPassword, id)
	} else {
		// 如果没有提供密码，不更新密码字段
		res, err = db.ExecContext(r.Context(),
			"UPDATE server_credentials SET name = ?, username = ?, updated_at = datetime('now') WHERE id = ?",
			req.Name, req.Username, id)
	}

	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "credential not found")
		return
	}

	response.OK(w, map[string]interface{}{
		"success": true,
		"message": "Credential updated successfully",
	})
}
