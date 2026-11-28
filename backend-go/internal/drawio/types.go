package drawio

// DocumentSummary 图库列表项，不携带 XML 正文
type DocumentSummary struct {
	ID               int64  `json:"id"`
	Title            string `json:"title"`
	Description      string `json:"description"`
	TagsJSON         string `json:"tags_json"`
	Archived         bool   `json:"archived"`
	PageCount        int    `json:"page_count"`
	CoverPageName    string `json:"cover_page_name"`
	DraftRev         int    `json:"draft_rev"`
	LatestVersionNo  int    `json:"latest_version_no"`
	ThumbnailPath    string `json:"thumbnail_path"`
	ThumbnailStatus  string `json:"thumbnail_status"`
	UpdatedAt        string `json:"updated_at"`
}

// DocumentDetail 文档详情
type DocumentDetail struct {
	ID                     int64  `json:"id"`
	Title                  string `json:"title"`
	Description            string `json:"description"`
	TagsJSON               string `json:"tags_json"`
	Archived               bool   `json:"archived"`
	PageCount              int    `json:"page_count"`
	PageNamesJSON          string `json:"page_names_json"`
	CoverPageID            string `json:"cover_page_id"`
	CoverPageName          string `json:"cover_page_name"`
	DraftRev               int    `json:"draft_rev"`
	LatestVersionID        *int64 `json:"latest_version_id"`
	LatestVersionNo        int    `json:"latest_version_no"`
	ThumbnailPath          string `json:"thumbnail_path"`
	ThumbnailStatus        string `json:"thumbnail_status"`
	LastExternalAssetScanAt string `json:"last_external_asset_scan_at"`
	CreatedAt              string `json:"created_at"`
	UpdatedAt              string `json:"updated_at"`
}

// DraftPayload 草稿保存/读取
type DraftPayload struct {
	DocumentID       int64  `json:"document_id"`
	XMLContent       string `json:"xml_content"`
	XMLHash          string `json:"xml_hash"`
	ExpectedDraftRev int    `json:"expected_draft_rev"`
	CurrentDraftRev  int    `json:"current_draft_rev"`
	BaseVersionID    *int64 `json:"base_version_id"`
	EditorStateJSON  string `json:"editor_state_json"`
	ExternalAssetsJSON string `json:"external_assets_json"`
	UpdatedAt        string `json:"updated_at"`
}

// VersionPayload 版本快照
type VersionPayload struct {
	ID              int64  `json:"id"`
	DocumentID      int64  `json:"document_id"`
	VersionNo       int    `json:"version_no"`
	Summary         string `json:"summary"`
	XMLContent       string `json:"xml_content,omitempty"`
	XMLHash         string `json:"xml_hash"`
	PageCount       int    `json:"page_count"`
	CoverPageName   string `json:"cover_page_name"`
	ThumbnailPath   string `json:"thumbnail_path"`
	ThumbnailStatus string `json:"thumbnail_status"`
	CreatedAt       string `json:"created_at"`
}

// SettingsPayload 模块设置
type SettingsPayload struct {
	DefaultExportFormat        string `json:"default_export_format"`
	DefaultThemeMode           string `json:"default_theme_mode"`
	AutosaveEnabled            bool   `json:"autosave_enabled"`
	AutosaveDebounceMs         int    `json:"autosave_debounce_ms"`
	DocumentSizeLimitBytes     int64  `json:"document_size_limit_bytes"`
	VersionSoftLimit           int    `json:"version_soft_limit"`
	AllowExternalAssets        bool   `json:"allow_external_assets"`
	BlockPrivateNetworkAssets  bool   `json:"block_private_network_assets"`
	ThumbnailFormat            string `json:"thumbnail_format"`
	ThumbnailMaxWidth          int    `json:"thumbnail_max_width"`
	ThumbnailMaxHeight         int    `json:"thumbnail_max_height"`
}

// CreateDocumentRequest 新建文档
type CreateDocumentRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	TagsJSON    string `json:"tags_json"`
}

// UpdateDocumentRequest 更新文档元数据
type UpdateDocumentRequest struct {
	Title             string  `json:"title"`
	Description       string  `json:"description"`
	TagsJSON          string  `json:"tags_json"`
	Archived          *bool   `json:"archived"`
	ExpectedUpdatedAt *string `json:"expected_updated_at"`
}

// SaveDraftRequest 保存草稿
type SaveDraftRequest struct {
	XMLContent        string `json:"xml_content"`
	ExpectedDraftRev  int    `json:"expected_draft_rev"`
	EditorStateJSON   string `json:"editor_state_json"`
}

// SaveVersionRequest 保存版本
type SaveVersionRequest struct {
	Summary          string `json:"summary"`
	ExpectedDraftRev int    `json:"expected_draft_rev"`
}

// UpdateThumbnailRequest 更新文档缩略图
type UpdateThumbnailRequest struct {
	ThumbnailPath string `json:"thumbnail_path"`
}

// ConflictResponse 冲突响应
type ConflictResponse struct {
	CurrentDraftRev int    `json:"current_draft_rev"`
	ServerUpdatedAt string `json:"server_updated_at"`
	LatestVersionID *int64 `json:"latest_version_id"`
	Message         string `json:"message"`
}

// RenderJob 渲染任务
type RenderJob struct {
	ID            int64  `json:"id"`
	DocumentID    int64  `json:"document_id"`
	VersionID     *int64 `json:"version_id"`
	SourceKind    string `json:"source_kind"`
	TargetKind    string `json:"target_kind"`
	Format        string `json:"format"`
	TriggerSource string `json:"trigger_source"`
	Status        string `json:"status"`
	AttemptCount  int    `json:"attempt_count"`
	LastError     string `json:"last_error"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// ImportResult 导入结果
type ImportResult struct {
	Document  DocumentDetail `json:"document"`
	DraftXML  string         `json:"draft_xml"`
}
