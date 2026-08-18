package github

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS github_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			type TEXT DEFAULT 'fine_grained',
			token_encrypted TEXT NOT NULL,
			enabled INTEGER DEFAULT 1,
			default_token INTEGER DEFAULT 0,
			note TEXT DEFAULT '',
			account_login TEXT DEFAULT '',
			scopes TEXT DEFAULT '',
			permissions_json TEXT DEFAULT '{}',
			last_test_status TEXT DEFAULT 'unknown',
			last_test_error TEXT DEFAULT '',
			last_test_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS github_repositories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token_id INTEGER,
			owner TEXT NOT NULL,
			name TEXT NOT NULL,
			full_name TEXT NOT NULL UNIQUE,
			html_url TEXT DEFAULT '',
			description TEXT DEFAULT '',
			private INTEGER DEFAULT 0,
			owned_by_token INTEGER DEFAULT 0,
			can_operate_actions INTEGER DEFAULT 0,
			default_branch TEXT DEFAULT '',
			language TEXT DEFAULT '',
			tags TEXT DEFAULT '',
			note TEXT DEFAULT '',
			enabled INTEGER DEFAULT 1,
			notify_enabled INTEGER DEFAULT 1,
			webhook_enabled INTEGER DEFAULT 0,
			webhook_secret TEXT DEFAULT '',
			collect_interval_seconds INTEGER DEFAULT 900,
			retention_days INTEGER DEFAULT 90,
			last_status TEXT DEFAULT 'pending',
			last_error TEXT DEFAULT '',
			last_collected_at DATETIME,
			last_event_fingerprint TEXT DEFAULT '',
			stars INTEGER DEFAULT 0,
			forks INTEGER DEFAULT 0,
			watchers INTEGER DEFAULT 0,
			open_issues INTEGER DEFAULT 0,
			open_pull_requests INTEGER DEFAULT 0,
			latest_release TEXT DEFAULT '',
			latest_release_url TEXT DEFAULT '',
			latest_action_status TEXT DEFAULT '',
			latest_action_conclusion TEXT DEFAULT '',
			rate_limit_remaining INTEGER DEFAULT -1,
			rate_limit_reset DATETIME,
			display_order INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS github_repository_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repository_id INTEGER NOT NULL,
			stars INTEGER DEFAULT 0,
			forks INTEGER DEFAULT 0,
			watchers INTEGER DEFAULT 0,
			open_issues INTEGER DEFAULT 0,
			open_pull_requests INTEGER DEFAULT 0,
			commit_count INTEGER DEFAULT 0,
			release_count INTEGER DEFAULT 0,
			contributor_count INTEGER DEFAULT 0,
			actions_total INTEGER DEFAULT 0,
			actions_success INTEGER DEFAULT 0,
			actions_failed INTEGER DEFAULT 0,
			traffic_views INTEGER DEFAULT 0,
			traffic_uniques INTEGER DEFAULT 0,
			traffic_clones INTEGER DEFAULT 0,
			traffic_clone_uniques INTEGER DEFAULT 0,
			collected_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS github_action_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repository_id INTEGER NOT NULL,
			run_id INTEGER NOT NULL,
			workflow_name TEXT DEFAULT '',
			display_title TEXT DEFAULT '',
			status TEXT DEFAULT '',
			conclusion TEXT DEFAULT '',
			event TEXT DEFAULT '',
					branch TEXT DEFAULT '',
					commit_sha TEXT DEFAULT '',
					commit_message TEXT DEFAULT '',
					actor TEXT DEFAULT '',
			html_url TEXT DEFAULT '',
			run_started_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			collected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(repository_id, run_id)
		)`,
		`CREATE TABLE IF NOT EXISTS github_traffic_samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repository_id INTEGER NOT NULL,
			views INTEGER DEFAULT 0,
			view_uniques INTEGER DEFAULT 0,
			clones INTEGER DEFAULT 0,
			clone_uniques INTEGER DEFAULT 0,
			collected_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS github_contributors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repository_id INTEGER NOT NULL,
			login TEXT NOT NULL,
			avatar_url TEXT DEFAULT '',
			html_url TEXT DEFAULT '',
			contributions INTEGER DEFAULT 0,
			collected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(repository_id, login)
		)`,
		`CREATE TABLE IF NOT EXISTS github_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repository_id INTEGER,
			event_type TEXT NOT NULL,
			severity TEXT DEFAULT 'info',
			title TEXT DEFAULT '',
			message TEXT DEFAULT '',
			payload_json TEXT DEFAULT '{}',
			fingerprint TEXT DEFAULT '',
			source TEXT DEFAULT 'collector',
			notified INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS github_webhook_deliveries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repository_id INTEGER,
			delivery_id TEXT DEFAULT '',
			event_type TEXT DEFAULT '',
			signature_valid INTEGER DEFAULT 0,
			duplicate INTEGER DEFAULT 0,
			payload_json TEXT DEFAULT '{}',
			error_message TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(delivery_id)
		)`,
		`CREATE TABLE IF NOT EXISTS github_operation_audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repository_id INTEGER,
			operation TEXT NOT NULL,
			target TEXT DEFAULT '',
			status TEXT DEFAULT '',
			request_json TEXT DEFAULT '{}',
			response_json TEXT DEFAULT '{}',
			error_message TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS github_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			enabled INTEGER DEFAULT 1,
			default_collect_interval_seconds INTEGER DEFAULT 900,
			default_retention_days INTEGER DEFAULT 90,
			max_concurrent_collectors INTEGER DEFAULT 2,
			rate_limit_low_threshold INTEGER DEFAULT 100,
			star_spike_threshold INTEGER DEFAULT 10,
			auto_create_webhook_secret INTEGER DEFAULT 1,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS github_public_pages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			slug TEXT UNIQUE NOT NULL,
			domain TEXT,
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			public INTEGER DEFAULT 1,
			cache_seconds INTEGER DEFAULT 300,
			config_json TEXT DEFAULT '{}',
			repository_ids_json TEXT DEFAULT '[]',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_github_snapshots_repo_time ON github_repository_snapshots(repository_id, collected_at)`,
		`CREATE INDEX IF NOT EXISTS idx_github_action_runs_repo_time ON github_action_runs(repository_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_github_events_repo_time ON github_events(repository_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_github_events_type ON github_events(event_type, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_github_repositories_enabled ON github_repositories(enabled, last_collected_at)`,
		`CREATE INDEX IF NOT EXISTS idx_github_public_pages_slug ON github_public_pages(slug, public)`,
		`CREATE INDEX IF NOT EXISTS idx_github_public_pages_domain ON github_public_pages(domain, public)`,
		`INSERT OR IGNORE INTO github_settings (id) VALUES (1)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure github schema: %w", err)
		}
	}
	for _, column := range []struct {
		table      string
		name       string
		definition string
	}{
		{"github_tokens", "account_login", "TEXT DEFAULT ''"},
		{"github_repositories", "owned_by_token", "INTEGER DEFAULT 0"},
		{"github_repositories", "can_operate_actions", "INTEGER DEFAULT 0"},
		{"github_repositories", "display_order", "INTEGER DEFAULT 0"},
		{"github_action_runs", "commit_message", "TEXT DEFAULT ''"},
	} {
		if err := ensureGitHubColumn(ctx, db, column.table, column.name, column.definition); err != nil {
			return err
		}
	}
	return nil
}

func ensureGitHubColumn(ctx context.Context, db *sql.DB, table, name, definition string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var columnName, columnType string
		var defaultValue interface{}
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if columnName == name {
			return nil
		}
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+name+` `+definition); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, name, err)
	}
	return nil
}

func loadSettings(ctx context.Context, db *sql.DB) (Settings, error) {
	var s Settings
	var enabled, autoSecret int
	err := db.QueryRowContext(ctx, `SELECT enabled, default_collect_interval_seconds, default_retention_days,
		max_concurrent_collectors, rate_limit_low_threshold, star_spike_threshold, auto_create_webhook_secret
		FROM github_settings WHERE id = 1`).Scan(&enabled, &s.DefaultCollectInterval, &s.DefaultRetentionDays, &s.MaxConcurrentCollectors, &s.RateLimitLowThreshold, &s.StarSpikeThreshold, &autoSecret)
	if err != nil {
		return s, err
	}
	s.Enabled = enabled == 1
	s.AutoCreateWebhookSecret = autoSecret == 1
	return s, nil
}

func updateSettings(ctx context.Context, db *sql.DB, payload map[string]interface{}) (Settings, error) {
	current, err := loadSettings(ctx, db)
	if err != nil {
		return current, err
	}
	settings := Settings{
		Enabled:                 current.Enabled,
		DefaultCollectInterval:  intValue(payload, "default_collect_interval_seconds", current.DefaultCollectInterval),
		DefaultRetentionDays:    intValue(payload, "default_retention_days", current.DefaultRetentionDays),
		MaxConcurrentCollectors: intValue(payload, "max_concurrent_collectors", current.MaxConcurrentCollectors),
		RateLimitLowThreshold:   intValue(payload, "rate_limit_low_threshold", current.RateLimitLowThreshold),
		StarSpikeThreshold:      intValue(payload, "star_spike_threshold", current.StarSpikeThreshold),
		AutoCreateWebhookSecret: current.AutoCreateWebhookSecret,
	}
	if value, ok := payload["enabled"]; ok {
		settings.Enabled = boolValue(value, current.Enabled)
	}
	if value, ok := payload["auto_create_webhook_secret"]; ok {
		settings.AutoCreateWebhookSecret = boolValue(value, current.AutoCreateWebhookSecret)
	}
	_, err = db.ExecContext(ctx, `UPDATE github_settings SET enabled = ?, default_collect_interval_seconds = ?,
		default_retention_days = ?, max_concurrent_collectors = ?, rate_limit_low_threshold = ?,
		star_spike_threshold = ?, auto_create_webhook_secret = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1`,
		boolInt(settings.Enabled), clamp(settings.DefaultCollectInterval, 60, 86400), clamp(settings.DefaultRetentionDays, 1, 3650),
		clamp(settings.MaxConcurrentCollectors, 1, 10), clamp(settings.RateLimitLowThreshold, 0, 5000), clamp(settings.StarSpikeThreshold, 1, 100000),
		boolInt(settings.AutoCreateWebhookSecret))
	if err != nil {
		return current, err
	}
	return loadSettings(ctx, db)
}

func scanToken(scanner interface{ Scan(...interface{}) error }) (Token, error) {
	var t Token
	var enabled, def int
	var lastTestAt sql.NullString
	err := scanner.Scan(&t.ID, &t.Name, &t.Type, &t.TokenEncrypted, &enabled, &def, &t.Note, &t.AccountLogin, &t.Scopes, &t.PermissionsJSON, &t.LastTestStatus, &t.LastTestError, &lastTestAt, &t.CreatedAt, &t.UpdatedAt)
	t.Enabled = enabled == 1
	t.DefaultToken = def == 1
	if lastTestAt.Valid {
		t.LastTestAt = &lastTestAt.String
	}
	return t, err
}

func listTokens(ctx context.Context, db *sql.DB) ([]Token, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, type, token_encrypted, enabled, default_token, note, account_login, scopes,
		permissions_json, last_test_status, last_test_error, last_test_at, created_at, updated_at
		FROM github_tokens ORDER BY default_token DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Token
	for rows.Next() {
		token, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, token)
	}
	return result, rows.Err()
}

func getToken(ctx context.Context, db *sql.DB, id int64) (Token, bool, error) {
	token, err := scanToken(db.QueryRowContext(ctx, `SELECT id, name, type, token_encrypted, enabled, default_token, note, account_login, scopes,
		permissions_json, last_test_status, last_test_error, last_test_at, created_at, updated_at
		FROM github_tokens WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return Token{}, false, nil
	}
	if err != nil {
		return Token{}, false, err
	}
	return token, true, nil
}

func getDefaultToken(ctx context.Context, db *sql.DB) (Token, bool, error) {
	token, err := scanToken(db.QueryRowContext(ctx, `SELECT id, name, type, token_encrypted, enabled, default_token, note, account_login, scopes,
		permissions_json, last_test_status, last_test_error, last_test_at, created_at, updated_at
		FROM github_tokens WHERE enabled = 1 ORDER BY default_token DESC, created_at ASC LIMIT 1`))
	if err == sql.ErrNoRows {
		return Token{}, false, nil
	}
	if err != nil {
		return Token{}, false, err
	}
	return token, true, nil
}

func scanRepository(scanner interface{ Scan(...interface{}) error }) (Repository, error) {
	var r Repository
	var tokenID sql.NullInt64
	var private, ownedByToken, canOperateActions, enabled, notifyEnabled, webhookEnabled int
	var lastCollectedAt, rateLimitReset sql.NullString
	err := scanner.Scan(&r.ID, &tokenID, &r.Owner, &r.Name, &r.FullName, &r.HTMLURL, &r.Description, &private, &ownedByToken, &canOperateActions, &r.DefaultBranch, &r.Language,
		&r.Tags, &r.Note, &enabled, &notifyEnabled, &webhookEnabled, &r.WebhookSecret, &r.CollectInterval, &r.RetentionDays,
		&r.LastStatus, &r.LastError, &lastCollectedAt, &r.LastEventFingerprint, &r.Stars, &r.Forks, &r.Watchers, &r.OpenIssues,
		&r.OpenPullRequests, &r.LatestRelease, &r.LatestReleaseURL, &r.LatestActionStatus, &r.LatestActionConclusion, &r.RateLimitRemaining,
		&rateLimitReset, &r.DisplayOrder, &r.CreatedAt, &r.UpdatedAt)
	if tokenID.Valid {
		id := tokenID.Int64
		r.TokenID = &id
	}
	r.Private = private == 1
	r.OwnedByToken = ownedByToken == 1
	r.CanOperateActions = canOperateActions == 1
	r.Enabled = enabled == 1
	r.NotifyEnabled = notifyEnabled == 1
	r.WebhookEnabled = webhookEnabled == 1
	if lastCollectedAt.Valid {
		r.LastCollectedAt = &lastCollectedAt.String
	}
	if rateLimitReset.Valid {
		r.RateLimitReset = rateLimitReset.String
	}
	return r, err
}

const repoSelect = `SELECT id, token_id, owner, name, full_name, html_url, description, private, owned_by_token, can_operate_actions, default_branch, language,
	tags, note, enabled, notify_enabled, webhook_enabled, webhook_secret, collect_interval_seconds, retention_days,
	last_status, last_error, last_collected_at, last_event_fingerprint, stars, forks, watchers, open_issues,
	open_pull_requests, latest_release, latest_release_url, latest_action_status, latest_action_conclusion,
	rate_limit_remaining, rate_limit_reset, display_order, created_at, updated_at FROM github_repositories`

const publicPageSelect = `SELECT id, slug, domain, title, description, public, cache_seconds, config_json, repository_ids_json, created_at, updated_at FROM github_public_pages`

func listRepositories(ctx context.Context, db *sql.DB, onlyEnabled bool) ([]Repository, error) {
	query := repoSelect
	if onlyEnabled {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY CASE WHEN display_order > 0 THEN display_order ELSE id END ASC, id ASC`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Repository
	for rows.Next() {
		repo, err := scanRepository(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, repo)
	}
	return result, rows.Err()
}

func attachLatestActionTiming(ctx context.Context, db *sql.DB, repositories []Repository) {
	for i := range repositories {
		var startedAt, createdAt, updatedAt sql.NullString
		err := db.QueryRowContext(ctx, `SELECT run_started_at, created_at, updated_at
			FROM github_action_runs WHERE repository_id = ?
			ORDER BY COALESCE(run_started_at, created_at, collected_at) DESC LIMIT 1`, repositories[i].ID).
			Scan(&startedAt, &createdAt, &updatedAt)
		if err != nil {
			continue
		}
		if startedAt.Valid {
			repositories[i].LatestActionStartedAt = startedAt.String
		}
		if createdAt.Valid {
			repositories[i].LatestActionCreatedAt = createdAt.String
		}
		if updatedAt.Valid {
			repositories[i].LatestActionUpdatedAt = updatedAt.String
		}
	}
}

func getRepository(ctx context.Context, db *sql.DB, id int64) (Repository, bool, error) {
	repo, err := scanRepository(db.QueryRowContext(ctx, repoSelect+` WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return Repository{}, false, nil
	}
	if err != nil {
		return Repository{}, false, err
	}
	return repo, true, nil
}

func latestActionRunForRepository(ctx context.Context, db *sql.DB, repositoryID int64) (map[string]interface{}, bool, error) {
	row := db.QueryRowContext(ctx, `SELECT run_id, workflow_name, display_title, status, conclusion, event, branch, commit_sha, commit_message, actor, html_url,
		run_started_at, created_at, updated_at, collected_at
		FROM github_action_runs
		WHERE repository_id = ?
		ORDER BY COALESCE(run_started_at, created_at, collected_at) DESC
		LIMIT 1`, repositoryID)

	var runID int64
	var workflowName, displayTitle, status, conclusion, event, branch, commitSHA, commitMessage, actor, htmlURL string
	var startedAt, createdAt, updatedAt, collectedAt sql.NullString
	if err := row.Scan(&runID, &workflowName, &displayTitle, &status, &conclusion, &event, &branch, &commitSHA, &commitMessage, &actor, &htmlURL, &startedAt, &createdAt, &updatedAt, &collectedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}

	return map[string]interface{}{
		"run_id":         runID,
		"workflow_name":  workflowName,
		"display_title":  displayTitle,
		"status":         status,
		"conclusion":     conclusion,
		"event":          event,
		"branch":         branch,
		"commit_sha":     commitSHA,
		"commit_message": commitMessage,
		"actor":          actor,
		"html_url":       htmlURL,
		"run_started_at": nullString(startedAt),
		"created_at":     nullString(createdAt),
		"updated_at":     nullString(updatedAt),
		"collected_at":   nullString(collectedAt),
	}, true, nil
}

func recentActionRunsForRepository(ctx context.Context, db *sql.DB, repositoryID int64, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.QueryContext(ctx, `SELECT run_id, workflow_name, display_title, status, conclusion, event, branch, commit_sha, commit_message, actor, html_url,
		run_started_at, created_at, updated_at, collected_at
		FROM github_action_runs
		WHERE repository_id = ?
		ORDER BY COALESCE(run_started_at, created_at, collected_at) DESC
		LIMIT ?`, repositoryID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var runID int64
		var workflowName, displayTitle, status, conclusion, event, branch, commitSHA, commitMessage, actor, htmlURL string
		var startedAt, createdAt, updatedAt, collectedAt sql.NullString
		if err := rows.Scan(&runID, &workflowName, &displayTitle, &status, &conclusion, &event, &branch, &commitSHA, &commitMessage, &actor, &htmlURL, &startedAt, &createdAt, &updatedAt, &collectedAt); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{
			"run_id":         runID,
			"workflow_name":  workflowName,
			"display_title":  displayTitle,
			"status":         status,
			"conclusion":     conclusion,
			"event":          event,
			"branch":         branch,
			"commit_sha":     commitSHA,
			"commit_message": commitMessage,
			"actor":          actor,
			"html_url":       htmlURL,
			"run_started_at": nullString(startedAt),
			"created_at":     nullString(createdAt),
			"updated_at":     nullString(updatedAt),
			"collected_at":   nullString(collectedAt),
		})
	}
	return result, rows.Err()
}

func getActionRunByRunID(ctx context.Context, db *sql.DB, repositoryID int64, runID int64) (map[string]interface{}, bool, error) {
	row := db.QueryRowContext(ctx, `SELECT run_id, workflow_name, display_title, status, conclusion, event, branch, commit_sha, commit_message, actor, html_url,
		run_started_at, created_at, updated_at, collected_at
		FROM github_action_runs
		WHERE repository_id = ? AND run_id = ?
		LIMIT 1`, repositoryID, runID)

	var rID int64
	var workflowName, displayTitle, status, conclusion, event, branch, commitSHA, commitMessage, actor, htmlURL string
	var startedAt, createdAt, updatedAt, collectedAt sql.NullString
	if err := row.Scan(&rID, &workflowName, &displayTitle, &status, &conclusion, &event, &branch, &commitSHA, &commitMessage, &actor, &htmlURL, &startedAt, &createdAt, &updatedAt, &collectedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}

	return map[string]interface{}{
		"run_id":         rID,
		"workflow_name":  workflowName,
		"display_title":  displayTitle,
		"status":         status,
		"conclusion":     conclusion,
		"event":          event,
		"branch":         branch,
		"commit_sha":     commitSHA,
		"commit_message": commitMessage,
		"actor":          actor,
		"html_url":       htmlURL,
		"run_started_at": nullString(startedAt),
		"created_at":     nullString(createdAt),
		"updated_at":     nullString(updatedAt),
		"collected_at":   nullString(collectedAt),
	}, true, nil
}

func getRepositoryByFullName(ctx context.Context, db *sql.DB, fullName string) (Repository, bool, error) {
	repo, err := scanRepository(db.QueryRowContext(ctx, repoSelect+` WHERE lower(full_name) = lower(?)`, fullName))
	if err == sql.ErrNoRows {
		return Repository{}, false, nil
	}
	if err != nil {
		return Repository{}, false, err
	}
	return repo, true, nil
}

func insertSnapshot(ctx context.Context, db *sql.DB, repoID int64, snapshot Snapshot) error {
	_, err := db.ExecContext(ctx, `INSERT INTO github_repository_snapshots (
		repository_id, stars, forks, watchers, open_issues, open_pull_requests, commit_count,
		release_count, contributor_count, actions_total, actions_success, actions_failed,
		traffic_views, traffic_uniques, traffic_clones, traffic_clone_uniques
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		repoID, snapshot.Stars, snapshot.Forks, snapshot.Watchers, snapshot.OpenIssues, snapshot.OpenPullRequests, snapshot.CommitCount,
		snapshot.ReleaseCount, snapshot.ContributorCount, snapshot.ActionsTotal, snapshot.ActionsSuccess, snapshot.ActionsFailed,
		snapshot.TrafficViews, snapshot.TrafficUniques, snapshot.TrafficClones, snapshot.TrafficCloneUniques)
	return err
}

func insertEvent(ctx context.Context, db *sql.DB, repoID *int64, eventType, severity, title, message, source string, payload map[string]interface{}, fingerprint string, notified bool) error {
	body, _ := json.Marshal(compactStoredEventPayload(eventType, payload))
	var id interface{}
	if repoID != nil {
		id = *repoID
	}
	_, err := db.ExecContext(ctx, `INSERT INTO github_events (repository_id, event_type, severity, title, message, payload_json, fingerprint, source, notified)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, eventType, severity, title, message, string(body), fingerprint, source, boolInt(notified))
	return err
}

func listEvents(ctx context.Context, db *sql.DB, repoID int64, limit int) ([]map[string]interface{}, error) {
	query := `SELECT id, repository_id, event_type, severity, title, message, payload_json, source, notified, created_at FROM github_events`
	args := []interface{}{}
	if repoID > 0 {
		query += ` WHERE repository_id = ?`
		args = append(args, repoID)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, clamp(limit, 1, 500))
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]interface{}
	for rows.Next() {
		var id, notified int
		var repositoryID sql.NullInt64
		var eventType, severity, title, message, raw, source, createdAt string
		if err := rows.Scan(&id, &repositoryID, &eventType, &severity, &title, &message, &raw, &source, &notified, &createdAt); err != nil {
			return nil, err
		}
		payload := map[string]interface{}{}
		_ = json.Unmarshal([]byte(raw), &payload)
		item := map[string]interface{}{"id": id, "event_type": eventType, "severity": severity, "title": title, "message": message, "payload": payload, "source": source, "notified": notified == 1, "created_at": createdAt}
		if repositoryID.Valid {
			item["repository_id"] = repositoryID.Int64
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func cleanupHistory(ctx context.Context, db *sql.DB, repoID int64, days int) (map[string]int64, error) {
	if days <= 0 {
		days = 90
	}
	tables := []string{"github_repository_snapshots", "github_action_runs", "github_traffic_samples", "github_events", "github_webhook_deliveries", "github_operation_audit"}
	result := map[string]int64{}
	cutoff := time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339)
	for _, table := range tables {
		column := "created_at"
		if table == "github_repository_snapshots" || table == "github_traffic_samples" {
			column = "collected_at"
		}
		// 分批删除：单批上限内循环，避免大表单次 DELETE 长时间持有写锁
		// （collector 每分钟与手动清理都会走到这里）。LIMIT 放在子查询中
		// （modernc 驱动不支持 DELETE 主语句 LIMIT）。
		const batchSize = 2000
		for {
			query := fmt.Sprintf("DELETE FROM %s WHERE rowid IN (SELECT rowid FROM %s WHERE %s < ?", table, table, column)
			args := []interface{}{cutoff}
			if repoID > 0 && table != "github_webhook_deliveries" {
				query += " AND repository_id = ?"
				args = append(args, repoID)
			}
			query += fmt.Sprintf(" LIMIT %d)", batchSize)
			res, err := db.ExecContext(ctx, query, args...)
			if err != nil {
				return result, err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return result, err
			}
			result[table] += n
			if n < batchSize {
				break
			}
		}
	}
	return result, nil
}

func compactHistoryPayloads(ctx context.Context, db *sql.DB, repoID int64) (map[string]int64, error) {
	result := map[string]int64{
		"github_events":             0,
		"github_webhook_deliveries": 0,
		"bytes_before":              0,
		"bytes_after":               0,
		"bytes_saved":               0,
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	updatedEvents, beforeEvents, afterEvents, err := compactEventRows(ctx, tx, repoID)
	if err != nil {
		return result, err
	}
	updatedDeliveries, beforeDeliveries, afterDeliveries, err := compactWebhookDeliveryRows(ctx, tx, repoID)
	if err != nil {
		return result, err
	}

	if err := tx.Commit(); err != nil {
		return result, err
	}

	result["github_events"] = updatedEvents
	result["github_webhook_deliveries"] = updatedDeliveries
	result["bytes_before"] = beforeEvents + beforeDeliveries
	result["bytes_after"] = afterEvents + afterDeliveries
	result["bytes_saved"] = result["bytes_before"] - result["bytes_after"]
	return result, nil
}

func compactEventRows(ctx context.Context, tx *sql.Tx, repoID int64) (updated, beforeBytes, afterBytes int64, err error) {
	query := `SELECT id, event_type, payload_json FROM github_events`
	args := []interface{}{}
	if repoID > 0 {
		query += ` WHERE repository_id = ?`
		args = append(args, repoID)
	}
	query += ` ORDER BY id ASC`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()

	stmt, err := tx.PrepareContext(ctx, `UPDATE github_events SET payload_json = ? WHERE id = ?`)
	if err != nil {
		return 0, 0, 0, err
	}
	defer stmt.Close()

	for rows.Next() {
		var id int64
		var eventType string
		var raw string
		if err := rows.Scan(&id, &eventType, &raw); err != nil {
			return 0, 0, 0, err
		}
		compact, ok := compactEventPayloadString(eventType, raw)
		if !ok || compact == raw {
			continue
		}
		if _, err := stmt.ExecContext(ctx, compact, id); err != nil {
			return 0, 0, 0, err
		}
		updated++
		beforeBytes += int64(len(raw))
		afterBytes += int64(len(compact))
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, err
	}
	return updated, beforeBytes, afterBytes, nil
}

func compactWebhookDeliveryRows(ctx context.Context, tx *sql.Tx, repoID int64) (updated, beforeBytes, afterBytes int64, err error) {
	query := `SELECT id, event_type, payload_json FROM github_webhook_deliveries`
	args := []interface{}{}
	if repoID > 0 {
		query += ` WHERE repository_id = ?`
		args = append(args, repoID)
	}
	query += ` ORDER BY id ASC`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()

	stmt, err := tx.PrepareContext(ctx, `UPDATE github_webhook_deliveries SET payload_json = ? WHERE id = ?`)
	if err != nil {
		return 0, 0, 0, err
	}
	defer stmt.Close()

	for rows.Next() {
		var id int64
		var eventType string
		var raw string
		if err := rows.Scan(&id, &eventType, &raw); err != nil {
			return 0, 0, 0, err
		}
		compact, ok := compactWebhookPayloadString(eventType, raw)
		if !ok || compact == raw {
			continue
		}
		if _, err := stmt.ExecContext(ctx, compact, id); err != nil {
			return 0, 0, 0, err
		}
		updated++
		beforeBytes += int64(len(raw))
		afterBytes += int64(len(compact))
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, err
	}
	return updated, beforeBytes, afterBytes, nil
}

func compactEventPayloadString(eventType, raw string) (string, bool) {
	payload := map[string]interface{}{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", false
	}
	body, err := json.Marshal(compactStoredEventPayload(eventType, payload))
	if err != nil {
		return "", false
	}
	return string(body), true
}

func compactWebhookPayloadString(eventType, raw string) (string, bool) {
	payload := map[string]interface{}{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", false
	}
	body, err := json.Marshal(compactWebhookDeliveryPayload(eventType, payload, []byte(raw)))
	if err != nil {
		return "", false
	}
	return string(body), true
}

func jsonString(value interface{}) string {
	body, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(body)
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func nullableInt64(value interface{}) interface{} {
	switch v := value.(type) {
	case nil:
		return nil
	case int64:
		if v == 0 {
			return nil
		}
		return v
	case *int64:
		if v == nil || *v == 0 {
			return nil
		}
		return *v
	default:
		return v
	}
}

func parseTags(raw interface{}) string {
	switch value := raw.(type) {
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ",")
	case []string:
		return strings.Join(value, ",")
	default:
		return strings.TrimSpace(fmt.Sprint(raw))
	}
}

func scanPublicPage(scanner interface{ Scan(...interface{}) error }) (PublicPage, error) {
	var page PublicPage
	var domain sql.NullString
	var isPublic int
	var configJSON, repositoryIDsJSON string
	if err := scanner.Scan(
		&page.ID,
		&page.Slug,
		&domain,
		&page.Title,
		&page.Description,
		&isPublic,
		&page.CacheSeconds,
		&configJSON,
		&repositoryIDsJSON,
		&page.CreatedAt,
		&page.UpdatedAt,
	); err != nil {
		return PublicPage{}, err
	}
	page.Public = isPublic == 1
	if domain.Valid {
		page.Domain = domain.String
	}
	page.Config = map[string]interface{}{}
	if strings.TrimSpace(configJSON) != "" {
		_ = json.Unmarshal([]byte(configJSON), &page.Config)
	}
	_ = json.Unmarshal([]byte(firstNonEmpty(repositoryIDsJSON, "[]")), &page.RepositoryIDs)
	if page.RepositoryIDs == nil {
		page.RepositoryIDs = []int64{}
	}
	return page, nil
}

func listPublicPages(ctx context.Context, db *sql.DB) ([]PublicPage, error) {
	rows, err := db.QueryContext(ctx, publicPageSelect+` ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PublicPage, 0)
	for rows.Next() {
		page, err := scanPublicPage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, page)
	}
	return result, rows.Err()
}

func getPublicPage(ctx context.Context, db *sql.DB, id int64) (PublicPage, bool, error) {
	page, err := scanPublicPage(db.QueryRowContext(ctx, publicPageSelect+` WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return PublicPage{}, false, nil
	}
	if err != nil {
		return PublicPage{}, false, err
	}
	return page, true, nil
}

func getPublicPageBySlug(ctx context.Context, db *sql.DB, slug string, onlyPublic bool) (PublicPage, bool, error) {
	query := publicPageSelect + ` WHERE slug = ?`
	args := []interface{}{normalizeGitHubPublicSlug(slug, "github")}
	if onlyPublic {
		query += ` AND public = 1`
	}
	page, err := scanPublicPage(db.QueryRowContext(ctx, query, args...))
	if err == sql.ErrNoRows {
		return PublicPage{}, false, nil
	}
	if err != nil {
		return PublicPage{}, false, err
	}
	return page, true, nil
}

func getPublicPageByDomain(ctx context.Context, db *sql.DB, domain string) (PublicPage, bool, error) {
	page, err := scanPublicPage(db.QueryRowContext(ctx, publicPageSelect+` WHERE public = 1 AND lower(domain) = lower(?)`, normalizeGitHubPublicDomain(domain)))
	if err == sql.ErrNoRows {
		return PublicPage{}, false, nil
	}
	if err != nil {
		return PublicPage{}, false, err
	}
	return page, true, nil
}

func savePublicPage(ctx context.Context, db *sql.DB, id int64, payload map[string]interface{}) (PublicPage, bool, error) {
	current := PublicPage{
		Public:        true,
		CacheSeconds:  300,
		Config:        map[string]interface{}{},
		RepositoryIDs: []int64{},
	}
	if id > 0 {
		var ok bool
		var err error
		current, ok, err = getPublicPage(ctx, db, id)
		if err != nil {
			return PublicPage{}, false, err
		}
		if !ok {
			return PublicPage{}, false, nil
		}
	}

	title := strings.TrimSpace(current.Title)
	if _, exists := payload["title"]; exists || id == 0 {
		title = strings.TrimSpace(firstNonEmpty(stringValue(payload, "title", ""), title, "GitHub 动态"))
	}
	slugSource := current.Slug
	if _, exists := payload["slug"]; exists || id == 0 {
		slugSource = stringValue(payload, "slug", "")
	}
	slug := normalizeGitHubPublicSlug(firstNonEmpty(slugSource, title, "github"), "github")
	domain := normalizeGitHubPublicDomain(current.Domain)
	if _, exists := payload["domain"]; exists || id == 0 {
		domain = normalizeGitHubPublicDomain(stringValue(payload, "domain", ""))
	}
	description := strings.TrimSpace(current.Description)
	if _, exists := payload["description"]; exists || id == 0 {
		description = strings.TrimSpace(stringValue(payload, "description", ""))
	}
	cacheSeconds := clamp(intValue(payload, "cacheSeconds", current.CacheSeconds), 30, 86400)
	isPublic := current.Public
	if value, exists := payload["public"]; exists {
		isPublic = boolValue(value, current.Public)
	}

	config := current.Config
	if payloadConfig, ok := payload["config"].(map[string]interface{}); ok {
		config = payloadConfig
	}
	if config == nil {
		config = map[string]interface{}{}
	}

	repositoryIDs := current.RepositoryIDs
	if ids := int64SliceValue(payload["repositoryIds"]); len(ids) > 0 {
		repositoryIDs = ids
	}
	if len(repositoryIDs) == 0 {
		return PublicPage{}, true, errors.New("repositoryIds is required")
	}

	configBody, err := json.Marshal(config)
	if err != nil {
		return PublicPage{}, true, err
	}
	repositoryBody, err := json.Marshal(repositoryIDs)
	if err != nil {
		return PublicPage{}, true, err
	}

	if id > 0 {
		if _, err := db.ExecContext(ctx, `UPDATE github_public_pages
			SET slug = ?, domain = ?, title = ?, description = ?, public = ?, cache_seconds = ?, config_json = ?, repository_ids_json = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?`,
			slug, nullEmpty(domain), title, description, boolInt(isPublic), cacheSeconds, string(configBody), string(repositoryBody), id); err != nil {
			return PublicPage{}, true, err
		}
		page, ok, err := getPublicPage(ctx, db, id)
		return page, ok, err
	}

	result, err := db.ExecContext(ctx, `INSERT INTO github_public_pages
		(slug, domain, title, description, public, cache_seconds, config_json, repository_ids_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		slug, nullEmpty(domain), title, description, boolInt(isPublic), cacheSeconds, string(configBody), string(repositoryBody))
	if err != nil {
		return PublicPage{}, true, err
	}
	insertID, err := result.LastInsertId()
	if err != nil {
		return PublicPage{}, true, err
	}
	page, ok, err := getPublicPage(ctx, db, insertID)
	return page, ok, err
}
