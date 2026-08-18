package drawio

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
)

type Store struct {
	cfg config.Config
	db  *database.Store
}

func NewStore(cfg config.Config) *Store {
	s := &Store{
		cfg: cfg,
		db:  database.New(cfg),
	}
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

// --- Documents ---

func (s *Store) ListDocuments(ctx context.Context, q, tag string, archived bool, sort string) ([]DocumentSummary, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	query := `SELECT id, title, description, tags_json, archived, page_count, cover_page_name,
		current_draft_rev, latest_version_no, thumbnail_path, thumbnail_status, updated_at
		FROM drawio_documents WHERE 1=1`
	args := []interface{}{}

	if q != "" {
		query += ` AND (title LIKE ? OR description LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if tag != "" {
		query += ` AND tags_json LIKE ?`
		args = append(args, "%\""+tag+"\"%")
	}
	if !archived {
		query += ` AND archived = 0`
	}

	switch sort {
	case "title":
		query += ` ORDER BY title ASC`
	case "created":
		query += ` ORDER BY created_at DESC`
	default:
		query += ` ORDER BY updated_at DESC`
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []DocumentSummary
	for rows.Next() {
		var d DocumentSummary
		var archived int
		if err := rows.Scan(&d.ID, &d.Title, &d.Description, &d.TagsJSON, &archived,
			&d.PageCount, &d.CoverPageName, &d.DraftRev, &d.LatestVersionNo,
			&d.ThumbnailPath, &d.ThumbnailStatus, &d.UpdatedAt); err != nil {
			return nil, err
		}
		d.Archived = archived != 0
		docs = append(docs, d)
	}
	if docs == nil {
		docs = []DocumentSummary{}
	}
	return docs, nil
}

func (s *Store) GetDocument(ctx context.Context, id int64) (*DocumentDetail, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var d DocumentDetail
	var archived int
	var latestVersionID sql.NullInt64
	err = db.QueryRowContext(ctx,
		`SELECT id, title, description, tags_json, archived, page_count, page_names_json,
		cover_page_id, cover_page_name, current_draft_rev, latest_version_id, latest_version_no,
		thumbnail_path, thumbnail_status, last_external_asset_scan_at, created_at, updated_at
		FROM drawio_documents WHERE id = ?`, id).
		Scan(&d.ID, &d.Title, &d.Description, &d.TagsJSON, &archived,
			&d.PageCount, &d.PageNamesJSON, &d.CoverPageID, &d.CoverPageName,
			&d.DraftRev, &latestVersionID, &d.LatestVersionNo,
			&d.ThumbnailPath, &d.ThumbnailStatus, &d.LastExternalAssetScanAt,
			&d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Archived = archived != 0
	if latestVersionID.Valid {
		d.LatestVersionID = &latestVersionID.Int64
	}
	return &d, nil
}

func (s *Store) CreateDocument(ctx context.Context, title, description, tagsJSON, xmlContent string) (*DocumentDetail, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	pages, coverID, coverName := ParsePageInfo(xmlContent)

	res, err := db.ExecContext(ctx,
		`INSERT INTO drawio_documents (title, description, tags_json, page_count, page_names_json,
		cover_page_id, cover_page_name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		title, description, tagsJSON, len(pages), pagesToJSON(pages), coverID, coverName, now, now)
	if err != nil {
		return nil, err
	}

	id, _ := res.LastInsertId()

	// 创建草稿
	xmlHash := ComputeXMLHash(xmlContent)
	_, err = db.ExecContext(ctx,
		`INSERT INTO drawio_drafts (document_id, xml_content, xml_hash, updated_at) VALUES (?, ?, ?, ?)`,
		id, xmlContent, xmlHash, now)
	if err != nil {
		return nil, err
	}

	return s.GetDocument(ctx, id)
}

func (s *Store) UpdateDocument(ctx context.Context, id int64, req UpdateDocumentRequest) (*DocumentDetail, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx,
		`UPDATE drawio_documents SET title = ?, description = ?, tags_json = ?,
		archived = CASE WHEN ? THEN 1 ELSE 0 END, updated_at = ?
		WHERE id = ?`,
		req.Title, req.Description, req.TagsJSON,
		req.Archived != nil && *req.Archived,
		now, id)
	if err != nil {
		return nil, err
	}

	return s.GetDocument(ctx, id)
}

func (s *Store) SaveDocumentThumbnail(ctx context.Context, documentID int64, thumbnailPath string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.ExecContext(ctx,
		`UPDATE drawio_documents SET
		thumbnail_path = ?, thumbnail_status = 'ready', thumbnail_error = '',
		thumbnail_updated_at = ?
		WHERE id = ?`,
		thumbnailPath, now, documentID)
	if err != nil {
		return err
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteDocument(ctx context.Context, id int64) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `DELETE FROM drawio_drafts WHERE document_id = ?`, id)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM drawio_versions WHERE document_id = ?`, id)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM drawio_assets WHERE document_id = ?`, id)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM drawio_render_jobs WHERE document_id = ?`, id)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM drawio_documents WHERE id = ?`, id)
	return err
}

func (s *Store) CloneDocument(ctx context.Context, id int64) (*DocumentDetail, error) {
	doc, err := s.GetDocument(ctx, id)
	if err != nil || doc == nil {
		return nil, err
	}

	draft, err := s.GetDraft(ctx, id)
	if err != nil {
		return nil, err
	}

	xmlContent := DefaultBlankMXFile()
	if draft != nil {
		xmlContent = draft.XMLContent
	}

	return s.CreateDocument(ctx,
		doc.Title+" (副本)",
		doc.Description,
		doc.TagsJSON,
		xmlContent,
	)
}

// --- Drafts ---

func (s *Store) GetDraft(ctx context.Context, documentID int64) (*DraftPayload, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var d DraftPayload
	var baseVersionID sql.NullInt64
	err = db.QueryRowContext(ctx,
		`SELECT document_id, xml_content, xml_hash, base_version_id, editor_state_json,
		external_assets_json, updated_at
		FROM drawio_drafts WHERE document_id = ?`, documentID).
		Scan(&d.DocumentID, &d.XMLContent, &d.XMLHash, &baseVersionID,
			&d.EditorStateJSON, &d.ExternalAssetsJSON, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if baseVersionID.Valid {
		d.BaseVersionID = &baseVersionID.Int64
	}
	return &d, nil
}

func (s *Store) GetDraftRevision(ctx context.Context, documentID int64) (int, error) {
	db, err := s.open(ctx)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var revision int
	err = db.QueryRowContext(ctx, `SELECT current_draft_rev FROM drawio_documents WHERE id = ?`, documentID).Scan(&revision)
	return revision, err
}

func (s *Store) SaveDraft(ctx context.Context, documentID int64, req SaveDraftRequest) (*DraftPayload, int, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	xmlHash := ComputeXMLHash(req.XMLContent)
	pages, coverID, coverName := ParsePageInfo(req.XMLContent)
	externalAssets := ExtractExternalAssets(req.XMLContent)
	assetsJSON := assetsToJSON(externalAssets)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	// 乐观锁：检查 draft_rev
	result, err := tx.ExecContext(ctx,
		`UPDATE drawio_documents SET current_draft_rev = current_draft_rev + 1,
		page_count = ?, page_names_json = ?, cover_page_id = ?, cover_page_name = ?,
		thumbnail_path = '', thumbnail_status = 'missing', thumbnail_error = '',
		thumbnail_updated_at = '', updated_at = ?
		WHERE id = ? AND current_draft_rev = ?`,
		len(pages), pagesToJSON(pages), coverID, coverName,
		now, documentID, req.ExpectedDraftRev)
	if err != nil {
		return nil, 0, err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// 冲突：获取当前 rev
		var currentRev int
		tx.QueryRowContext(ctx, `SELECT current_draft_rev FROM drawio_documents WHERE id = ?`, documentID).Scan(&currentRev)
		return nil, currentRev, fmt.Errorf("conflict")
	}

	// Upsert 草稿
	_, err = tx.ExecContext(ctx,
		`INSERT INTO drawio_drafts (document_id, xml_content, xml_hash, editor_state_json, external_assets_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(document_id) DO UPDATE SET
		xml_content = excluded.xml_content, xml_hash = excluded.xml_hash,
		editor_state_json = excluded.editor_state_json,
		external_assets_json = excluded.external_assets_json,
		updated_at = excluded.updated_at`,
		documentID, req.XMLContent, xmlHash, req.EditorStateJSON, assetsJSON, now)
	if err != nil {
		return nil, 0, err
	}

	// 获取更新后的 draft_rev
	var newRev int
	tx.QueryRowContext(ctx, `SELECT current_draft_rev FROM drawio_documents WHERE id = ?`, documentID).Scan(&newRev)
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}

	draft := &DraftPayload{
		DocumentID:         documentID,
		XMLContent:         req.XMLContent,
		XMLHash:            xmlHash,
		CurrentDraftRev:    newRev,
		EditorStateJSON:    req.EditorStateJSON,
		ExternalAssetsJSON: assetsJSON,
		UpdatedAt:          now,
	}
	return draft, newRev, nil
}

// --- Versions ---

func (s *Store) ListVersions(ctx context.Context, documentID int64) ([]VersionPayload, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx,
		`SELECT id, document_id, version_no, summary, xml_hash, page_count,
		cover_page_name, thumbnail_path, thumbnail_status, created_at
		FROM drawio_versions WHERE document_id = ? ORDER BY version_no DESC`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []VersionPayload
	for rows.Next() {
		var v VersionPayload
		if err := rows.Scan(&v.ID, &v.DocumentID, &v.VersionNo, &v.Summary, &v.XMLHash,
			&v.PageCount, &v.CoverPageName, &v.ThumbnailPath, &v.ThumbnailStatus, &v.CreatedAt); err != nil {
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
		`SELECT id, document_id, version_no, xml_content, summary, xml_hash,
		page_count, cover_page_name, thumbnail_path, thumbnail_status, created_at
		FROM drawio_versions WHERE id = ?`, versionID).
		Scan(&v.ID, &v.DocumentID, &v.VersionNo, &v.XMLContent, &v.Summary, &v.XMLHash,
			&v.PageCount, &v.CoverPageName, &v.ThumbnailPath, &v.ThumbnailStatus, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Store) SaveVersion(ctx context.Context, documentID int64, req SaveVersionRequest) (*VersionPayload, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// 获取当前草稿
	draft, err := s.GetDraft(ctx, documentID)
	if err != nil || draft == nil {
		return nil, fmt.Errorf("draft not found")
	}

	// 乐观锁校验
	var currentRev int
	db.QueryRowContext(ctx, `SELECT current_draft_rev FROM drawio_documents WHERE id = ?`, documentID).Scan(&currentRev)
	if currentRev != 0 && req.ExpectedDraftRev != 0 && currentRev != req.ExpectedDraftRev {
		return nil, fmt.Errorf("conflict: draft revision mismatch")
	}

	// 获取下一个版本号
	var lastVersionNo int
	db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_no), 0) FROM drawio_versions WHERE document_id = ?`, documentID).Scan(&lastVersionNo)
	nextVersionNo := lastVersionNo + 1

	now := time.Now().UTC().Format(time.RFC3339)
	pages, _, coverName := ParsePageInfo(draft.XMLContent)

	result, err := db.ExecContext(ctx,
		`INSERT INTO drawio_versions (document_id, version_no, xml_content, xml_hash, summary, page_count, cover_page_name, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		documentID, nextVersionNo, draft.XMLContent, draft.XMLHash, req.Summary, len(pages), coverName, now)
	if err != nil {
		return nil, err
	}

	versionID, _ := result.LastInsertId()

	// 更新文档的最新版本信息
	db.ExecContext(ctx,
		`UPDATE drawio_documents SET latest_version_id = ?, latest_version_no = ?, updated_at = ? WHERE id = ?`,
		versionID, nextVersionNo, now, documentID)

	return &VersionPayload{
		ID:            versionID,
		DocumentID:    documentID,
		VersionNo:     nextVersionNo,
		Summary:       req.Summary,
		XMLHash:       draft.XMLHash,
		PageCount:     len(pages),
		CoverPageName: coverName,
		CreatedAt:     now,
	}, nil
}

var (
	// errVersionNotFound / errVersionNotBelong 是版本操作的资源不存在哨兵错误，
	// handler 据此返回 404 而不是 500。
	errVersionNotFound  = errors.New("version not found")
	errVersionNotBelong = errors.New("version does not belong to document")
)

func (s *Store) RestoreVersion(ctx context.Context, documentID, versionID int64) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	version, err := s.GetVersion(ctx, versionID)
	if err != nil || version == nil {
		return errVersionNotFound
	}

	if version.DocumentID != documentID {
		return errVersionNotBelong
	}

	// 将版本 XML 覆盖到草稿
	now := time.Now().UTC().Format(time.RFC3339)
	xmlHash := ComputeXMLHash(version.XMLContent)

	_, err = db.ExecContext(ctx,
		`UPDATE drawio_documents SET current_draft_rev = current_draft_rev + 1, updated_at = ? WHERE id = ?`,
		now, documentID)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO drawio_drafts (document_id, xml_content, xml_hash, base_version_id, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(document_id) DO UPDATE SET
		xml_content = excluded.xml_content, xml_hash = excluded.xml_hash,
		base_version_id = excluded.base_version_id, updated_at = excluded.updated_at`,
		documentID, version.XMLContent, xmlHash, versionID, now)
	return err
}

// --- Settings ---

func (s *Store) GetSettings(ctx context.Context) (*SettingsPayload, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var st SettingsPayload
	var autosaveEnabled, allowExternal, blockPrivate int
	err = db.QueryRowContext(ctx,
		`SELECT default_export_format, default_theme_mode, autosave_enabled, autosave_debounce_ms,
		document_size_limit_bytes, version_soft_limit, allow_external_assets,
		block_private_network_assets, thumbnail_format, thumbnail_max_width, thumbnail_max_height
		FROM drawio_settings WHERE id = 1`).
		Scan(&st.DefaultExportFormat, &st.DefaultThemeMode, &autosaveEnabled,
			&st.AutosaveDebounceMs, &st.DocumentSizeLimitBytes, &st.VersionSoftLimit,
			&allowExternal, &blockPrivate, &st.ThumbnailFormat,
			&st.ThumbnailMaxWidth, &st.ThumbnailMaxHeight)
	if err != nil {
		return nil, err
	}
	st.AutosaveEnabled = autosaveEnabled != 0
	st.AllowExternalAssets = allowExternal != 0
	st.BlockPrivateNetworkAssets = blockPrivate != 0
	return &st, nil
}

func (s *Store) UpdateSettings(ctx context.Context, settings SettingsPayload) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.ExecContext(ctx,
		`UPDATE drawio_settings SET
		default_export_format = ?, default_theme_mode = ?,
		autosave_enabled = ?, autosave_debounce_ms = ?,
		document_size_limit_bytes = ?, version_soft_limit = ?,
		allow_external_assets = ?, block_private_network_assets = ?,
		thumbnail_format = ?, thumbnail_max_width = ?, thumbnail_max_height = ?,
		updated_at = ?
		WHERE id = 1`,
		settings.DefaultExportFormat, settings.DefaultThemeMode,
		boolToInt(settings.AutosaveEnabled), settings.AutosaveDebounceMs,
		settings.DocumentSizeLimitBytes, settings.VersionSoftLimit,
		boolToInt(settings.AllowExternalAssets), boolToInt(settings.BlockPrivateNetworkAssets),
		settings.ThumbnailFormat, settings.ThumbnailMaxWidth, settings.ThumbnailMaxHeight,
		time.Now().UTC().Format(time.RFC3339))
	return err
}

// --- Helpers ---

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func pagesToJSON(pages []PageInfo) string {
	if len(pages) == 0 {
		return "[]"
	}
	type pageJSON struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	out := make([]pageJSON, len(pages))
	for i, p := range pages {
		out[i] = pageJSON{ID: p.ID, Name: p.Name}
	}
	if encoded, err := json.Marshal(out); err == nil {
		return string(encoded)
	}
	return "[]"
}

func assetsToJSON(assets []ExternalAsset) string {
	if len(assets) == 0 {
		return "[]"
	}
	type assetJSON struct {
		URL       string `json:"url"`
		Domain    string `json:"domain"`
		AssetType string `json:"asset_type"`
	}
	out := make([]assetJSON, len(assets))
	for i, a := range assets {
		out[i] = assetJSON{URL: a.URL, Domain: a.Domain, AssetType: a.AssetType}
	}
	if encoded, err := json.Marshal(out); err == nil {
		return string(encoded)
	}
	return "[]"
}

func joinStrings(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ","
		}
		result += p
	}
	return result
}
