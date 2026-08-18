package prompts

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
)

var (
	// errVersionNotFound / errVersionNotBelong 是版本操作的资源不存在哨兵错误，
	// handler 据此返回 404 而不是 500。
	errVersionNotFound  = errors.New("version not found")
	errVersionNotBelong = errors.New("version does not belong to entry")
)

type Store struct {
	cfg config.Config
	db  *database.Store
}

func NewStore(cfg config.Config) *Store {
	s := &Store{cfg: cfg, db: database.New(cfg)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := s.db.Open(ctx)
	if err == nil {
		ensureSchema(ctx, db)
		db.Close()
	}
	return s
}

func (s *Store) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.db.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := ensureSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func generatePublicID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)[:10]
}

// --- Collections ---

func (s *Store) ListCollections(ctx context.Context) ([]Collection, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx,
		`SELECT id, parent_id, name, description, icon, color_token, sort_order, archived, created_at, updated_at
		FROM prompt_collections WHERE archived = 0 ORDER BY sort_order ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var collections []Collection
	for rows.Next() {
		var c Collection
		var parentID sql.NullInt64
		var archived int
		if err := rows.Scan(&c.ID, &parentID, &c.Name, &c.Description, &c.Icon, &c.ColorToken,
			&c.SortOrder, &archived, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		if parentID.Valid {
			c.ParentID = &parentID.Int64
		}
		c.Archived = archived != 0
		collections = append(collections, c)
	}
	if collections == nil {
		collections = []Collection{}
	}
	return collections, nil
}

func (s *Store) CreateCollection(ctx context.Context, name string, parentID *int64, description string) (*Collection, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	var maxSort int
	db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) FROM prompt_collections`).Scan(&maxSort)

	result, err := db.ExecContext(ctx,
		`INSERT INTO prompt_collections (parent_id, name, description, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		parentID, name, description, maxSort+1, now, now)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return &Collection{
		ID:          id,
		ParentID:    parentID,
		Name:        name,
		Description: description,
		SortOrder:   maxSort + 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func (s *Store) UpdateCollection(ctx context.Context, id int64, req UpdateCollectionRequest) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx,
		`UPDATE prompt_collections SET name = ?, description = ?, parent_id = ?,
		archived = CASE WHEN ? THEN 1 ELSE 0 END, updated_at = ? WHERE id = ?`,
		req.Name, req.Description, req.ParentID, req.Archived != nil && *req.Archived, now, id)
	return err
}

func (s *Store) DeleteCollection(ctx context.Context, id int64) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	// Unlink entries
	db.ExecContext(ctx, `UPDATE prompt_entries SET collection_id = NULL WHERE collection_id = ?`, id)
	_, err = db.ExecContext(ctx, `DELETE FROM prompt_collections WHERE id = ?`, id)
	return err
}

// --- Entries ---

func (s *Store) ListEntries(ctx context.Context, collectionID *int64, q, tag string, starred, published bool) ([]EntrySummary, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	query := `SELECT id, public_id, collection_id, title, summary, tags_json, starred, visibility,
		latest_published_version_no, latest_published_at, created_at, updated_at
		FROM prompt_entries WHERE archived = 0`
	args := []interface{}{}

	if collectionID != nil {
		query += ` AND collection_id = ?`
		args = append(args, *collectionID)
	}
	if q != "" {
		query += ` AND (title LIKE ? OR summary LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if tag != "" {
		query += ` AND tags_json LIKE ?`
		args = append(args, "%\""+tag+"\"%")
	}
	if starred {
		query += ` AND starred = 1`
	}
	if published {
		query += ` AND latest_published_version_no > 0`
	}

	query += ` ORDER BY updated_at DESC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []EntrySummary
	for rows.Next() {
		var e EntrySummary
		var collID sql.NullInt64
		var starred int
		if err := rows.Scan(&e.ID, &e.PublicID, &collID, &e.Title, &e.Summary, &e.TagsJSON, &starred,
			&e.Visibility, &e.LatestPublishedVersionNo, &e.LatestPublishedAt,
			&e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		if collID.Valid {
			e.CollectionID = &collID.Int64
		}
		e.Starred = starred != 0
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []EntrySummary{}
	}
	return entries, nil
}

func (s *Store) CreateEntry(ctx context.Context, req CreateEntryRequest) (*EntryDetail, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	slug := GenerateSlug(req.Title)
	baseSlug := slug
	for suffix := 2; ; suffix++ {
		var exists int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM prompt_entries WHERE internal_slug = ?`, slug).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			break
		}
		slug = fmt.Sprintf("%s-%d", baseSlug, suffix)
	}
	publicID := GeneratePublicID()

	visibility := req.Visibility
	if visibility == "" {
		visibility = "unlisted"
	}

	result, err := db.ExecContext(ctx,
		`INSERT INTO prompt_entries (collection_id, title, internal_slug, public_id, tags_json, visibility, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		req.CollectionID, req.Title, slug, publicID, req.TagsJSON, visibility, now, now)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()

	// Create initial draft
	db.ExecContext(ctx,
		`INSERT INTO prompt_drafts (entry_id, content_md, content_text, updated_at) VALUES (?, '', '', ?)`,
		id, now)

	detail, _ := s.GetEntry(ctx, id)
	return detail, nil
}

func (s *Store) GetEntry(ctx context.Context, id int64) (*EntryDetail, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var e EntryDetail
	var collID, latestVersionID sql.NullInt64
	var starred, archived int
	err = db.QueryRowContext(ctx,
		`SELECT id, collection_id, title, internal_slug, public_id, summary, tags_json,
		starred, archived, visibility, current_draft_rev, latest_published_version_id,
		latest_published_version_no, latest_published_at, published_char_count,
		published_word_count, created_at, updated_at
		FROM prompt_entries WHERE id = ?`, id).
		Scan(&e.ID, &collID, &e.Title, &e.InternalSlug, &e.PublicID, &e.Summary,
			&e.TagsJSON, &starred, &archived, &e.Visibility, &e.CurrentDraftRev,
			&latestVersionID, &e.LatestPublishedVersionNo, &e.LatestPublishedAt,
			&e.PublishedCharCount, &e.PublishedWordCount, &e.CreatedAt, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if collID.Valid {
		e.CollectionID = &collID.Int64
	}
	if latestVersionID.Valid {
		e.LatestPublishedVersionID = &latestVersionID.Int64
	}
	e.Starred = starred != 0
	e.Archived = archived != 0
	return &e, nil
}

func (s *Store) UpdateEntry(ctx context.Context, id int64, req UpdateEntryRequest) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx,
		`UPDATE prompt_entries SET title = ?, collection_id = ?, tags_json = ?,
		starred = CASE WHEN ? THEN 1 ELSE 0 END,
		archived = CASE WHEN ? THEN 1 ELSE 0 END,
		visibility = ?, updated_at = ? WHERE id = ?`,
		req.Title, req.CollectionID, req.TagsJSON,
		req.Starred != nil && *req.Starred,
		req.Archived != nil && *req.Archived,
		req.Visibility, now, id)
	return err
}

func (s *Store) DeleteEntry(ctx context.Context, id int64) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	db.ExecContext(ctx, `DELETE FROM prompt_drafts WHERE entry_id = ?`, id)
	db.ExecContext(ctx, `DELETE FROM prompt_versions WHERE entry_id = ?`, id)
	db.ExecContext(ctx, `DELETE FROM prompt_access_logs WHERE entry_id = ?`, id)
	_, err = db.ExecContext(ctx, `DELETE FROM prompt_entries WHERE id = ?`, id)
	return err
}

func (s *Store) DuplicateEntry(ctx context.Context, id int64) (*EntryDetail, error) {
	entry, err := s.GetEntry(ctx, id)
	if err != nil || entry == nil {
		return nil, err
	}

	draft, _ := s.GetDraft(ctx, id)
	contentMD := ""
	if draft != nil {
		contentMD = draft.ContentMD
	}

	newEntry, err := s.CreateEntry(ctx, CreateEntryRequest{
		CollectionID: entry.CollectionID,
		Title:        entry.Title + " (副本)",
		TagsJSON:     entry.TagsJSON,
		Visibility:   entry.Visibility,
	})
	if err != nil {
		return nil, err
	}

	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	text := StripMarkdown(contentMD)
	db.ExecContext(ctx,
		`UPDATE prompt_drafts SET content_md = ?, content_text = ?, updated_at = ? WHERE entry_id = ?`,
		contentMD, text, now, newEntry.ID)

	return newEntry, nil
}

// --- Drafts ---

func (s *Store) GetDraft(ctx context.Context, entryID int64) (*DraftPayload, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var d DraftPayload
	err = db.QueryRowContext(ctx,
		`SELECT entry_id, content_md, content_text, outline_json, variables_json, excerpt_text, updated_at
		FROM prompt_drafts WHERE entry_id = ?`, entryID).
		Scan(&d.EntryID, &d.ContentMD, &d.ContentText, &d.OutlineJSON, &d.VariablesJSON,
			&d.ExcerptText, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) SaveDraft(ctx context.Context, entryID int64, req SaveDraftRequest) (*DraftPayload, int, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	text := StripMarkdown(req.ContentMD)
	outline := ExtractOutline(req.ContentMD)
	outlineJSON, _ := json.Marshal(outline)
	variables := ExtractVariables(req.ContentMD)
	variablesJSON, _ := json.Marshal(variables)
	excerpt := ExtractExcerpt(req.ContentMD, 200)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	// Optimistic lock
	result, err := tx.ExecContext(ctx,
		`UPDATE prompt_entries SET current_draft_rev = current_draft_rev + 1, updated_at = ?,
		summary = ? WHERE id = ? AND current_draft_rev = ?`,
		now, excerpt, entryID, req.ExpectedDraftRev)
	if err != nil {
		return nil, 0, err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		var currentRev int
		tx.QueryRowContext(ctx, `SELECT current_draft_rev FROM prompt_entries WHERE id = ?`, entryID).Scan(&currentRev)
		return nil, currentRev, fmt.Errorf("conflict")
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO prompt_drafts (entry_id, content_md, content_text, outline_json, variables_json, excerpt_text, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(entry_id) DO UPDATE SET
		content_md = excluded.content_md, content_text = excluded.content_text,
		outline_json = excluded.outline_json, variables_json = excluded.variables_json,
		excerpt_text = excluded.excerpt_text, updated_at = excluded.updated_at`,
		entryID, req.ContentMD, text, string(outlineJSON), string(variablesJSON), excerpt, now)
	if err != nil {
		return nil, 0, err
	}

	var newRev int
	tx.QueryRowContext(ctx, `SELECT current_draft_rev FROM prompt_entries WHERE id = ?`, entryID).Scan(&newRev)
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}

	draft := &DraftPayload{
		EntryID:       entryID,
		ContentMD:     req.ContentMD,
		ContentText:   text,
		OutlineJSON:   string(outlineJSON),
		VariablesJSON: string(variablesJSON),
		ExcerptText:   excerpt,
		UpdatedAt:     now,
	}
	return draft, newRev, nil
}

// --- Versions ---

func (s *Store) PublishVersion(ctx context.Context, entryID int64, req PublishRequest) (*VersionPayload, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Verify draft rev
	var currentRev int
	db.QueryRowContext(ctx, `SELECT current_draft_rev FROM prompt_entries WHERE id = ?`, entryID).Scan(&currentRev)
	if req.ExpectedDraftRev != 0 && currentRev != req.ExpectedDraftRev {
		return nil, fmt.Errorf("conflict: draft revision mismatch")
	}

	draft, err := s.GetDraft(ctx, entryID)
	if err != nil || draft == nil {
		return nil, fmt.Errorf("draft not found")
	}

	var lastVersionNo int
	db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_no), 0) FROM prompt_versions WHERE entry_id = ?`, entryID).Scan(&lastVersionNo)
	nextVersionNo := lastVersionNo + 1

	now := time.Now().UTC().Format(time.RFC3339)
	checksum := ComputeChecksum(draft.ContentMD)

	result, err := db.ExecContext(ctx,
		`INSERT INTO prompt_versions (entry_id, version_no, content_md, content_text, outline_json,
		variables_json, excerpt_text, checksum, char_count, word_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entryID, nextVersionNo, draft.ContentMD, draft.ContentText, draft.OutlineJSON,
		draft.VariablesJSON, draft.ExcerptText, checksum,
		len(draft.ContentText), CountWords(draft.ContentText), now)
	if err != nil {
		return nil, err
	}

	versionID, _ := result.LastInsertId()

	db.ExecContext(ctx,
		`UPDATE prompt_entries SET latest_published_version_id = ?, latest_published_version_no = ?,
		latest_published_at = ?, published_char_count = ?, published_word_count = ?, updated_at = ?
		WHERE id = ?`,
		versionID, nextVersionNo, now, len(draft.ContentText), CountWords(draft.ContentText), now, entryID)

	return &VersionPayload{
		ID:          versionID,
		EntryID:     entryID,
		VersionNo:   nextVersionNo,
		ContentMD:   draft.ContentMD,
		ContentText: draft.ContentText,
		OutlineJSON: draft.OutlineJSON,
		ExcerptText: draft.ExcerptText,
		Checksum:    checksum,
		CharCount:   len(draft.ContentText),
		WordCount:   CountWords(draft.ContentText),
		CreatedAt:   now,
	}, nil
}

func (s *Store) ListVersions(ctx context.Context, entryID int64) ([]VersionPayload, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx,
		`SELECT id, entry_id, version_no, content_text, outline_json, variables_json,
		excerpt_text, checksum, char_count, word_count, created_at
		FROM prompt_versions WHERE entry_id = ? ORDER BY version_no DESC`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []VersionPayload
	for rows.Next() {
		var v VersionPayload
		if err := rows.Scan(&v.ID, &v.EntryID, &v.VersionNo, &v.ContentText, &v.OutlineJSON,
			&v.VariablesJSON, &v.ExcerptText, &v.Checksum, &v.CharCount, &v.WordCount, &v.CreatedAt); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	if versions == nil {
		versions = []VersionPayload{}
	}
	return versions, nil
}

func (s *Store) GetVersion(ctx context.Context, versionID int64) (*VersionPayload, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var v VersionPayload
	err = db.QueryRowContext(ctx,
		`SELECT id, entry_id, version_no, content_md, content_text, outline_json, variables_json,
		excerpt_text, checksum, char_count, word_count, created_at
		FROM prompt_versions WHERE id = ?`, versionID).
		Scan(&v.ID, &v.EntryID, &v.VersionNo, &v.ContentMD, &v.ContentText, &v.OutlineJSON,
			&v.VariablesJSON, &v.ExcerptText, &v.Checksum, &v.CharCount, &v.WordCount, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Store) RestoreVersion(ctx context.Context, entryID, versionID int64) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	version, err := s.GetVersion(ctx, versionID)
	if err != nil || version == nil {
		return errVersionNotFound
	}
	if version.EntryID != entryID {
		return errVersionNotBelong
	}

	now := time.Now().UTC().Format(time.RFC3339)
	db.ExecContext(ctx, `UPDATE prompt_entries SET current_draft_rev = current_draft_rev + 1, updated_at = ? WHERE id = ?`, now, entryID)

	_, err = db.ExecContext(ctx,
		`INSERT INTO prompt_drafts (entry_id, content_md, content_text, outline_json, variables_json, excerpt_text, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(entry_id) DO UPDATE SET
		content_md = excluded.content_md, content_text = excluded.content_text,
		outline_json = excluded.outline_json, variables_json = excluded.variables_json,
		excerpt_text = excluded.excerpt_text, updated_at = excluded.updated_at`,
		entryID, version.ContentMD, version.ContentText, version.OutlineJSON,
		version.VariablesJSON, version.ExcerptText, now)
	return err
}

// --- Public ---

func (s *Store) GetEntryByPublicID(ctx context.Context, publicID string) (*EntryDetail, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var e EntryDetail
	var collID, latestVersionID sql.NullInt64
	var starred, archived int
	err = db.QueryRowContext(ctx,
		`SELECT id, collection_id, title, internal_slug, public_id, summary, tags_json,
		starred, archived, visibility, current_draft_rev, latest_published_version_id,
		latest_published_version_no, latest_published_at, published_char_count,
		published_word_count, created_at, updated_at
		FROM prompt_entries WHERE public_id = ? AND visibility != 'private'`, publicID).
		Scan(&e.ID, &collID, &e.Title, &e.InternalSlug, &e.PublicID, &e.Summary,
			&e.TagsJSON, &starred, &archived, &e.Visibility, &e.CurrentDraftRev,
			&latestVersionID, &e.LatestPublishedVersionNo, &e.LatestPublishedAt,
			&e.PublishedCharCount, &e.PublishedWordCount, &e.CreatedAt, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if collID.Valid {
		e.CollectionID = &collID.Int64
	}
	if latestVersionID.Valid {
		e.LatestPublishedVersionID = &latestVersionID.Int64
	}
	e.Starred = starred != 0
	e.Archived = archived != 0
	return &e, nil
}

func (s *Store) GetPublishedVersion(ctx context.Context, entryID int64) (*VersionPayload, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var versionID sql.NullInt64
	db.QueryRowContext(ctx, `SELECT latest_published_version_id FROM prompt_entries WHERE id = ?`, entryID).Scan(&versionID)
	if !versionID.Valid {
		return nil, nil
	}

	return s.GetVersion(ctx, versionID.Int64)
}

func (s *Store) GetPublishedVersionByNo(ctx context.Context, entryID int64, versionNo int) (*VersionPayload, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var v VersionPayload
	err = db.QueryRowContext(ctx,
		`SELECT id, entry_id, version_no, content_md, content_text, outline_json, variables_json,
		excerpt_text, checksum, char_count, word_count, created_at
		FROM prompt_versions WHERE entry_id = ? AND version_no = ?`, entryID, versionNo).
		Scan(&v.ID, &v.EntryID, &v.VersionNo, &v.ContentMD, &v.ContentText, &v.OutlineJSON,
			&v.VariablesJSON, &v.ExcerptText, &v.Checksum, &v.CharCount, &v.WordCount, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Store) LogAccess(ctx context.Context, entryID int64, versionID *int64, routeKind, responseFormat, ipHash, userAgent string) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	db.ExecContext(ctx,
		`INSERT INTO prompt_access_logs (entry_id, version_id, route_kind, response_format, ip_hash, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entryID, versionID, routeKind, responseFormat, ipHash, userAgent, now)
}

// --- Settings ---

func (s *Store) GetSettings(ctx context.Context) (*SettingsPayload, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var st SettingsPayload
	var allowPublic, allowDirect int
	err = db.QueryRowContext(ctx,
		`SELECT default_visibility, default_direct_format, allow_public_pages, allow_direct_links, access_log_retention_days
		FROM prompt_settings WHERE id = 1`).
		Scan(&st.DefaultVisibility, &st.DefaultDirectFormat, &allowPublic, &allowDirect, &st.AccessLogRetentionDays)
	if err != nil {
		return nil, err
	}
	st.AllowPublicPages = allowPublic != 0
	st.AllowDirectLinks = allowDirect != 0
	return &st, nil
}

func (s *Store) UpdateSettings(ctx context.Context, settings SettingsPayload) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	allowPublic := 0
	if settings.AllowPublicPages {
		allowPublic = 1
	}
	allowDirect := 0
	if settings.AllowDirectLinks {
		allowDirect = 1
	}

	_, err = db.ExecContext(ctx,
		`UPDATE prompt_settings SET
		default_visibility = ?, default_direct_format = ?,
		allow_public_pages = ?, allow_direct_links = ?,
		access_log_retention_days = ?, updated_at = ?
		WHERE id = 1`,
		settings.DefaultVisibility, settings.DefaultDirectFormat,
		allowPublic, allowDirect, settings.AccessLogRetentionDays,
		time.Now().UTC().Format(time.RFC3339))
	return err
}
