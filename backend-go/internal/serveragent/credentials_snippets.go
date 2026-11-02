package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func (s *Service) handleCredentials(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 0 {
		if r.Method == http.MethodGet {
			s.listCredentials(w, r, db)
		} else if r.Method == http.MethodPost {
			s.createCredential(w, r, db)
		} else {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(subparts) == 1 && subparts[0] == "default" && r.Method == http.MethodGet {
		s.getDefaultCredential(w, r, db)
		return
	}

	if len(subparts) == 1 && r.Method == http.MethodDelete {
		s.deleteCredential(w, r, db, subparts[0])
		return
	}

	// PUT /api/server/credentials/{id} - 更新凭据
	if len(subparts) == 1 && r.Method == http.MethodPut {
		s.updateCredential(w, r, db, subparts[0])
		return
	}

	if len(subparts) == 2 && subparts[1] == "default" && (r.Method == http.MethodPut || r.Method == http.MethodPost) {
		s.setDefaultCredential(w, r, db, subparts[0])
		return
	}

	response.Error(w, http.StatusNotFound, "credentials sub-route not found")
}

func (s *Service) listCredentials(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), "SELECT id, name, username, password, is_default, created_at, updated_at FROM server_credentials ORDER BY is_default DESC, created_at DESC")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id int
		var name, username string
		var password sql.NullString
		var isDefault int
		var createdAt, updatedAt string
		if err := rows.Scan(&id, &name, &username, &password, &isDefault, &createdAt, &updatedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		plainPassword := ""
		if password.Valid {
			plainPassword = secure.SecureDecrypt(password.String)
		}

		list = append(list, map[string]interface{}{
			"id":         id,
			"name":       name,
			"username":   username,
			"password":   plainPassword,
			"is_default": isDefault == 1,
			"created_at": createdAt,
			"updated_at": updatedAt,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	response.OK(w, list)
}

func (s *Service) getDefaultCredential(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var id int
	var name, username string
	var password sql.NullString
	var isDefault int
	var createdAt, updatedAt string

	err := db.QueryRowContext(r.Context(), "SELECT id, name, username, password, is_default, created_at, updated_at FROM server_credentials WHERE is_default = 1 LIMIT 1").
		Scan(&id, &name, &username, &password, &isDefault, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		response.OK(w, nil)
		return
	} else if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	plainPassword := ""
	if password.Valid {
		plainPassword = secure.SecureDecrypt(password.String)
	}

	response.OK(w, map[string]interface{}{
		"id":         id,
		"name":       name,
		"username":   username,
		"password":   plainPassword,
		"is_default": true,
		"created_at": createdAt,
		"updated_at": updatedAt,
	})
}

func (s *Service) createCredential(w http.ResponseWriter, r *http.Request, db *sql.DB) {
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
		response.Error(w, http.StatusBadRequest, "缺少必填字段")
		return
	}

	encPassword := ""
	if req.Password != "" {
		var err error
		encPassword, err = secure.SecureEncrypt(req.Password)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "encryption failed: "+err.Error())
			return
		}
	}

	res, err := db.ExecContext(r.Context(), "INSERT INTO server_credentials (name, username, password) VALUES (?, ?, ?)", req.Name, req.Username, encPassword)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	lastID, _ := res.LastInsertId()
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "凭据添加成功",
		"data": map[string]interface{}{
			"id":       lastID,
			"name":     req.Name,
			"username": req.Username,
			"password": req.Password,
		},
	})
}

func (s *Service) setDefaultCredential(w http.ResponseWriter, r *http.Request, db *sql.DB, idStr string) {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid credential ID")
		return
	}

	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var exists int
	if err := tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM server_credentials WHERE id = ?", id).Scan(&exists); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if exists == 0 {
		response.Error(w, http.StatusNotFound, "凭据不存在")
		return
	}

	if _, err := tx.ExecContext(r.Context(), "UPDATE server_credentials SET is_default = 0"); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if _, err := tx.ExecContext(r.Context(), "UPDATE server_credentials SET is_default = 1 WHERE id = ?", id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	committed = true
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.OK(w, map[string]interface{}{
		"success": true,
		"message": "已设置为默认凭据",
	})
}

func (s *Service) deleteCredential(w http.ResponseWriter, r *http.Request, db *sql.DB, idStr string) {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid credential ID")
		return
	}

	res, err := db.ExecContext(r.Context(), "DELETE FROM server_credentials WHERE id = ?", id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "凭据不存在")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "凭据删除成功",
	})
}

// ==========================================
// SNIPPETS HANDLERS
// ==========================================

func (s *Service) handleSnippets(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 0 {
		if r.Method == http.MethodGet {
			s.listSnippets(w, r, db)
		} else if r.Method == http.MethodPost {
			s.createSnippet(w, r, db)
		} else {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(subparts) == 1 {
		if subparts[0] == "preview" && r.Method == http.MethodPost {
			s.previewSnippet(w, r, db)
			return
		}
		if subparts[0] == "history" {
			if r.Method == http.MethodGet {
				s.getSnippetHistory(w, r, db)
			} else if r.Method == http.MethodPost {
				s.addSnippetHistory(w, r, db)
			} else {
				response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			}
			return
		}
	}

	if len(subparts) == 1 {
		if r.Method == http.MethodPut {
			s.updateSnippet(w, r, db, subparts[0])
			return
		}
		if r.Method == http.MethodDelete {
			s.deleteSnippet(w, r, db, subparts[0])
			return
		}
	}

	response.Error(w, http.StatusNotFound, "snippets sub-route not found")
}

func (s *Service) listSnippets(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	q := r.URL.Query()
	category := q.Get("category")
	platform := q.Get("platform")
	favoriteStr := q.Get("favorite")
	searchQ := q.Get("q")

	sqlQuery := "SELECT id, title, content, category, platform, tags, favorite, run_count, last_used_at, is_builtin, description, created_at, updated_at FROM server_snippets WHERE 1=1"
	var params []interface{}

	if category != "" {
		sqlQuery += " AND category = ?"
		params = append(params, category)
	}
	if platform != "" && platform != "all" {
		sqlQuery += " AND (platform = 'all' OR platform = ?)"
		params = append(params, platform)
	}
	if favoriteStr != "" {
		favVal := 0
		if favoriteStr == "true" || favoriteStr == "1" {
			favVal = 1
		}
		sqlQuery += " AND favorite = ?"
		params = append(params, favVal)
	}
	if searchQ != "" {
		sqlQuery += " AND (title LIKE ? OR content LIKE ? OR description LIKE ? OR tags LIKE ?)"
		likeVal := "%" + searchQ + "%"
		params = append(params, likeVal, likeVal, likeVal, likeVal)
	}

	sqlQuery += " ORDER BY favorite DESC, category ASC, run_count DESC, title ASC"

	rows, err := db.QueryContext(r.Context(), sqlQuery, params...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var snippets []map[string]interface{}
	for rows.Next() {
		var id, favorite, runCount, isBuiltin int
		var title, content, category, platform, tagsStr string
		var lastUsedAt, description sql.NullString
		var createdAt, updatedAt string
		err := rows.Scan(&id, &title, &content, &category, &platform, &tagsStr, &favorite, &runCount, &lastUsedAt, &isBuiltin, &description, &createdAt, &updatedAt)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		descVal := ""
		if description.Valid {
			descVal = description.String
		}
		lastUsedVal := interface{}(nil)
		if lastUsedAt.Valid {
			lastUsedVal = lastUsedAt.String
		}

		dangerRes := DetectDangerousCommand(content)

		snippets = append(snippets, map[string]interface{}{
			"id":                id,
			"title":             title,
			"content":           content,
			"category":          category,
			"platform":          platform,
			"tags":              parseJSONTags(tagsStr),
			"favorite":          favorite == 1,
			"run_count":         runCount,
			"last_used_at":      lastUsedVal,
			"is_builtin":        isBuiltin == 1,
			"description":       descVal,
			"created_at":        createdAt,
			"updated_at":        updatedAt,
			"dangerous":         dangerRes.Dangerous,
			"dangerous_reasons": dangerRes.Reasons,
		})
	}
	if snippets == nil {
		snippets = []map[string]interface{}{}
	}
	response.OK(w, snippets)
}

func (s *Service) createSnippet(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		Title       string      `json:"title"`
		Content     string      `json:"content"`
		Category    string      `json:"category"`
		Platform    string      `json:"platform"`
		Tags        interface{} `json:"tags"`
		Favorite    bool        `json:"favorite"`
		Description string      `json:"description"`
		IsBuiltin   bool        `json:"is_builtin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title == "" || req.Content == "" {
		response.Error(w, http.StatusBadRequest, "缺少必填字段")
		return
	}

	now := time.Now().Format(time.RFC3339)
	favoriteVal := 0
	if req.Favorite {
		favoriteVal = 1
	}
	builtinVal := 0
	if req.IsBuiltin {
		builtinVal = 1
	}

	res, err := db.ExecContext(r.Context(), `
		INSERT INTO server_snippets (
			title, content, category, platform, tags, favorite, description, is_builtin, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Title, req.Content,
		coalesceStr(req.Category, "common"),
		coalesceStr(req.Platform, "all"),
		SerializeList(req.Tags),
		favoriteVal,
		req.Description,
		builtinVal,
		now, now,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	lastID, _ := res.LastInsertId()
	snippetObj, err := s.querySnippetByID(r.Context(), db, int(lastID))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, snippetObj)
}

func (s *Service) updateSnippet(w http.ResponseWriter, r *http.Request, db *sql.DB, idStr string) {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid snippet ID")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch existing snippet
	existing, err := s.querySnippetByID(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusNotFound, "snippet not found")
		return
	}

	now := time.Now().Format(time.RFC3339)
	title := getStringVal(req, "title", existing["title"].(string))
	content := getStringVal(req, "content", existing["content"].(string))
	category := getStringVal(req, "category", existing["category"].(string))
	platform := getStringVal(req, "platform", existing["platform"].(string))
	description := getStringVal(req, "description", existing["description"].(string))

	favorite := existing["favorite"].(bool)
	if val, ok := req["favorite"].(bool); ok {
		favorite = val
	}
	favoriteVal := 0
	if favorite {
		favoriteVal = 1
	}

	tagsStr := SerializeList(existing["tags"])
	if val, ok := req["tags"]; ok {
		tagsStr = SerializeList(val)
	}

	_, err = db.ExecContext(r.Context(), `
		UPDATE server_snippets
		SET title = ?, content = ?, category = ?, platform = ?, tags = ?, favorite = ?, description = ?, updated_at = ?
		WHERE id = ?`,
		title, content, category, platform, tagsStr, favoriteVal, description, now, id,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.OK(w, true)
}

func (s *Service) deleteSnippet(w http.ResponseWriter, r *http.Request, db *sql.DB, idStr string) {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid snippet ID")
		return
	}

	res, err := db.ExecContext(r.Context(), "DELETE FROM server_snippets WHERE id = ?", id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "代码片段不存在")
		return
	}

	response.OK(w, true)
}

func (s *Service) previewSnippet(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		Command   string                 `json:"command"`
		SnippetID int                    `json:"snippetId"`
		ServerID  string                 `json:"serverId"`
		Cwd       string                 `json:"cwd"`
		Variables map[string]interface{} `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	commandText := req.Command
	if req.SnippetID > 0 {
		var content string
		err := db.QueryRowContext(r.Context(), "SELECT content FROM server_snippets WHERE id = ?", req.SnippetID).Scan(&content)
		if err == nil {
			commandText = content
		}
	}

	if commandText == "" {
		response.Error(w, http.StatusBadRequest, "缺少命令内容")
		return
	}

	// Fetch server if serverId is provided
	serverMap := make(map[string]interface{})
	if req.ServerID != "" {
		var host, name, username string
		var port int
		err := db.QueryRowContext(r.Context(), "SELECT host, name, port, username FROM server_accounts WHERE id = ?", req.ServerID).
			Scan(&host, &name, &port, &username)
		if err == nil {
			serverMap["host"] = host
			serverMap["name"] = name
			serverMap["port"] = port
			serverMap["username"] = username
		}
	}

	extraVars := make(map[string]interface{})
	for k, v := range req.Variables {
		extraVars[k] = v
	}
	extraVars["cwd"] = req.Cwd

	resolvedVars := BuildCommandVariables(serverMap, extraVars)
	rendered := RenderCommandTemplate(commandText, resolvedVars)
	danger := DetectDangerousCommand(rendered)

	response.OK(w, map[string]interface{}{
		"command":       commandText,
		"rendered":      rendered,
		"dangerous":     danger.Dangerous,
		"dangerReasons": danger.Reasons,
	})
}

func (s *Service) addSnippetHistory(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		SnippetID       *int    `json:"snippetId"`
		ServerID        *string `json:"serverId"`
		Command         string  `json:"command"`
		RenderedCommand string  `json:"renderedCommand"`
		ExecutionMode   string  `json:"executionMode"`
		Status          string  `json:"status"`
		ResultSummary   *string `json:"resultSummary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	lastID, serverName, err := s.insertSnippetHistory(r.Context(), db, req.SnippetID, req.ServerID, req.Command, req.RenderedCommand, req.ExecutionMode, req.Status, req.ResultSummary)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().Format(time.RFC3339)
	response.OK(w, map[string]interface{}{
		"id":               lastID,
		"snippet_id":       req.SnippetID,
		"server_id":        req.ServerID,
		"server_name":      serverName,
		"command":          firstNonEmpty(req.Command, req.RenderedCommand),
		"rendered_command": firstNonEmpty(req.RenderedCommand, req.Command),
		"execution_mode":   req.ExecutionMode,
		"status":           req.Status,
		"created_at":       now,
	})
}

// insertSnippetHistory 写入一条命令历史记录（片段执行与 API 命令执行共用），
// 并更新片段 run_count。返回新记录 ID 与解析出的服务器名。
func (s *Service) insertSnippetHistory(ctx context.Context, db *sql.DB, snippetID *int, serverID *string, command, renderedCommand, executionMode, status string, resultSummary *string) (int64, string, error) {
	var serverName sql.NullString
	if serverID != nil && *serverID != "" {
		var name string
		err := db.QueryRowContext(ctx, "SELECT name FROM server_accounts WHERE id = ?", *serverID).Scan(&name)
		if err == nil {
			serverName = sql.NullString{String: name, Valid: true}
		}
	}

	commandStr := firstNonEmpty(command, renderedCommand)
	renderedStr := firstNonEmpty(renderedCommand, command)

	danger := DetectDangerousCommand(renderedStr)
	dangerReasonsJSON, _ := json.Marshal(danger.Reasons)

	now := time.Now().Format(time.RFC3339)

	res, err := db.ExecContext(ctx, `
		INSERT INTO server_command_history (
			snippet_id, server_id, server_name, command, rendered_command, execution_mode, status, dangerous, danger_reasons, result_summary, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snippetID, serverID, serverName, commandStr, renderedStr,
		coalesceStr(executionMode, "terminal"),
		coalesceStr(status, "sent"),
		boolToInt(danger.Dangerous),
		string(dangerReasonsJSON),
		resultSummary,
		now,
	)
	if err != nil {
		return 0, "", err
	}

	// Update run count
	if snippetID != nil && *snippetID > 0 {
		_, _ = db.ExecContext(ctx, "UPDATE server_snippets SET run_count = COALESCE(run_count, 0) + 1, last_used_at = ?, updated_at = ? WHERE id = ?", now, now, *snippetID)
	}

	lastID, _ := res.LastInsertId()
	return lastID, serverName.String, nil
}

func (s *Service) getSnippetHistory(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	q := r.URL.Query()
	serverId := q.Get("serverId")
	limitStr := q.Get("limit")

	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}
	if limit > 200 {
		limit = 200
	}

	sqlQuery := "SELECT id, snippet_id, server_id, server_name, command, rendered_command, execution_mode, status, dangerous, danger_reasons, result_summary, created_at FROM server_command_history"
	var params []interface{}
	if serverId != "" {
		sqlQuery += " WHERE server_id = ?"
		params = append(params, serverId)
	}
	sqlQuery += " ORDER BY created_at DESC LIMIT ?"
	params = append(params, limit)

	rows, err := db.QueryContext(r.Context(), sqlQuery, params...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, dangerous int
		var snippetID sql.NullInt64
		var serverID, serverName, resultSummary sql.NullString
		var command, renderedCommand, executionMode, status, dangerReasonsJSON, createdAt string
		err := rows.Scan(&id, &snippetID, &serverID, &serverName, &command, &renderedCommand, &executionMode, &status, &dangerous, &dangerReasonsJSON, &resultSummary, &createdAt)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		var snippetVal interface{} = nil
		if snippetID.Valid {
			snippetVal = snippetID.Int64
		}
		var serverIDVal interface{} = nil
		if serverID.Valid {
			serverIDVal = serverID.String
		}
		var serverNameVal interface{} = nil
		if serverName.Valid {
			serverNameVal = serverName.String
		}
		var resultVal interface{} = nil
		if resultSummary.Valid {
			resultVal = resultSummary.String
		}

		var reasons []string
		_ = json.Unmarshal([]byte(dangerReasonsJSON), &reasons)
		if reasons == nil {
			reasons = []string{}
		}

		list = append(list, map[string]interface{}{
			"id":               id,
			"snippet_id":       snippetVal,
			"server_id":        serverIDVal,
			"server_name":      serverNameVal,
			"command":          command,
			"rendered_command": renderedCommand,
			"execution_mode":   executionMode,
			"status":           status,
			"dangerous":        dangerous == 1,
			"danger_reasons":   reasons,
			"result_summary":   resultVal,
			"created_at":       createdAt,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	response.OK(w, list)
}

func (s *Service) querySnippetByID(ctx context.Context, db *sql.DB, id int) (map[string]interface{}, error) {
	var favorite, runCount, isBuiltin int
	var title, content, category, platform, tagsStr string
	var lastUsedAt, description sql.NullString
	var createdAt, updatedAt string
	err := db.QueryRowContext(ctx, "SELECT id, title, content, category, platform, tags, favorite, run_count, last_used_at, is_builtin, description, created_at, updated_at FROM server_snippets WHERE id = ?", id).
		Scan(&id, &title, &content, &category, &platform, &tagsStr, &favorite, &runCount, &lastUsedAt, &isBuiltin, &description, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	descVal := ""
	if description.Valid {
		descVal = description.String
	}
	lastUsedVal := interface{}(nil)
	if lastUsedAt.Valid {
		lastUsedVal = lastUsedAt.String
	}

	dangerRes := DetectDangerousCommand(content)

	return map[string]interface{}{
		"id":                id,
		"title":             title,
		"content":           content,
		"category":          category,
		"platform":          platform,
		"tags":              parseJSONTags(tagsStr),
		"favorite":          favorite == 1,
		"run_count":         runCount,
		"last_used_at":      lastUsedVal,
		"is_builtin":        isBuiltin == 1,
		"description":       descVal,
		"created_at":        createdAt,
		"updated_at":        updatedAt,
		"dangerous":         dangerRes.Dangerous,
		"dangerous_reasons": dangerRes.Reasons,
	}, nil
}

// ==========================================
// MONITOR CONFIG & LOGS HANDLERS
// ==========================================
