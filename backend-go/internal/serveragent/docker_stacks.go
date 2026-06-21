package serveragent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) handleDockerStacks(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	rows, err := db.QueryContext(r.Context(), `
		SELECT id, server_id, name, type, source, COALESCE(working_dir, ''), COALESCE(config_files, '[]'),
		       status, COALESCE(last_error, ''), COALESCE(config_hash, ''), created_at, updated_at
		FROM docker_stacks
		WHERE server_id = ?
		ORDER BY updated_at DESC, name ASC`, serverID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	stacks := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var sid, name, typ, source, workingDir, configFiles, status, lastError, configHash, createdAt, updatedAt string
		if err := rows.Scan(&id, &sid, &name, &typ, &source, &workingDir, &configFiles, &status, &lastError, &configHash, &createdAt, &updatedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		var files interface{} = []interface{}{}
		_ = json.Unmarshal([]byte(configFiles), &files)
		stacks = append(stacks, map[string]interface{}{
			"id":          id,
			"serverId":    sid,
			"name":        name,
			"type":        typ,
			"source":      source,
			"workingDir":  workingDir,
			"configFiles": files,
			"status":      status,
			"lastError":   lastError,
			"configHash":  configHash,
			"createdAt":   createdAt,
			"updatedAt":   updatedAt,
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": stacks})
}

func upsertDockerStackSnapshot(ctx context.Context, db *sql.DB, serverID string, composeProjects interface{}) {
	projects := asMapList(composeProjects)
	if len(projects) == 0 {
		return
	}
	now := time.Now().Format(time.RFC3339Nano)
	for _, project := range projects {
		name := stackString(firstNonNil(project["Name"], project["name"], project["Project"], project["project"]))
		if name == "" {
			continue
		}
		status := stackString(firstNonNil(project["Status"], project["status"]))
		if status == "" {
			status = "unknown"
		}
		workingDir := stackString(firstNonNil(project["WorkingDir"], project["workingDir"], project["working_dir"], project["ProjectDir"], project["projectDir"]))
		configFilesJSON := stackConfigFilesJSON(firstNonNil(project["ConfigFiles"], project["configFiles"], project["config_files"], project["Files"], project["files"]))
		configHash := stackConfigHash(name, workingDir, configFilesJSON)

		_, _ = db.ExecContext(ctx, `
			INSERT INTO docker_stacks (server_id, name, type, source, working_dir, config_files, status, config_hash, updated_at)
			VALUES (?, ?, 'compose', 'agent', ?, ?, ?, ?, ?)
			ON CONFLICT(server_id, name) DO UPDATE SET
				working_dir = excluded.working_dir,
				config_files = excluded.config_files,
				status = excluded.status,
				config_hash = excluded.config_hash,
				updated_at = excluded.updated_at`,
			serverID, name, workingDir, configFilesJSON, status, configHash, now)
	}
}

func updateDockerStackActionState(ctx context.Context, db *sql.DB, serverID, name, action, actionError string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	now := time.Now().Format(time.RFC3339Nano)
	status := dockerStackStatusAfterAction(action, actionError)
	configHash := stackConfigHash(name)
	_, err := db.ExecContext(ctx, `
		INSERT INTO docker_stacks (server_id, name, type, source, status, last_error, config_hash, updated_at)
		VALUES (?, ?, 'compose', 'agent', ?, ?, ?, ?)
		ON CONFLICT(server_id, name) DO UPDATE SET
			status = excluded.status,
			last_error = excluded.last_error,
			updated_at = excluded.updated_at`,
		serverID, name, status, actionError, configHash, now)
	return err
}

func deleteDockerStackRecord(ctx context.Context, db *sql.DB, serverID, name string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM docker_stacks WHERE server_id = ? AND name = ?`, serverID, name)
	return err
}

func dockerStackStatusAfterAction(action, actionError string) string {
	if strings.TrimSpace(actionError) != "" {
		return "error"
	}
	switch action {
	case "up", "start", "restart":
		return "running"
	case "down", "stop":
		return "stopped"
	case "pull":
		return "image_pulled"
	default:
		return "unknown"
	}
}

func stackConfigFilesJSON(value interface{}) string {
	switch v := value.(type) {
	case []interface{}, []string:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	case string:
		parts := []string{}
		for _, part := range strings.Split(v, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
		if b, err := json.Marshal(parts); err == nil {
			return string(b)
		}
	}
	return "[]"
}

func stackString(value interface{}) string {
	return strings.TrimSpace(stringFromAny(value))
}

func stackConfigHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}
