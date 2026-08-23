package drawio

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

type Service struct {
	cfg      config.Config
	store    *Store
	renderer *Renderer
}

func New(cfg config.Config) *Service {
	store := NewStore(cfg)
	renderer := NewRenderer(store)

	s := &Service{
		cfg:      cfg,
		store:    store,
		renderer: renderer,
	}

	// 启动后台渲染器
	renderer.Start(context.Background())
	return s
}

func (s *Service) Stop() {
	s.renderer.Stop()
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/drawio")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	ctx := r.Context()

	switch {
	// Document list
	case path == "documents" && r.Method == http.MethodGet:
		s.listDocuments(ctx, w, r)
	case path == "documents" && r.Method == http.MethodPost:
		s.createDocument(ctx, w, r)

	// Import
	case path == "import" && r.Method == http.MethodPost:
		s.importDocument(ctx, w, r)

	// Settings
	case path == "settings" && r.Method == http.MethodGet:
		s.getSettings(ctx, w)
	case path == "settings" && r.Method == http.MethodPut:
		s.updateSettings(ctx, w, r)

	// Thumbnails rebuild all
	case path == "thumbnails/rebuild" && r.Method == http.MethodPost:
		s.rebuildAllThumbnails(ctx, w)

	// Render jobs
	case path == "render-jobs" && r.Method == http.MethodGet:
		s.listRenderJobs(ctx, w)

	// Document-specific routes
	case len(parts) >= 2 && parts[0] == "documents":
		docID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "Invalid document ID")
			return
		}

		switch {
		case len(parts) == 2 && r.Method == http.MethodGet:
			s.getDocument(ctx, w, docID)
		case len(parts) == 2 && r.Method == http.MethodPut:
			s.updateDocument(ctx, w, r, docID)
		case len(parts) == 2 && r.Method == http.MethodDelete:
			s.deleteDocument(ctx, w, docID)

		case len(parts) == 3 && parts[2] == "clone" && r.Method == http.MethodPost:
			s.cloneDocument(ctx, w, docID)
		case len(parts) == 3 && parts[2] == "draft" && r.Method == http.MethodGet:
			s.getDraft(ctx, w, docID)
		case len(parts) == 3 && parts[2] == "draft" && r.Method == http.MethodPut:
			s.saveDraft(ctx, w, r, docID)
		case len(parts) == 3 && parts[2] == "thumbnail" && r.Method == http.MethodPut:
			s.saveThumbnail(ctx, w, r, docID)
		case len(parts) == 3 && parts[2] == "versions" && r.Method == http.MethodGet:
			s.listVersions(ctx, w, docID)
		case len(parts) == 3 && parts[2] == "versions" && r.Method == http.MethodPost:
			s.saveVersion(ctx, w, r, docID)
		case len(parts) == 3 && parts[2] == "export" && r.Method == http.MethodGet:
			s.exportDocument(ctx, w, r, docID)
		case len(parts) == 4 && parts[2] == "thumbnails" && parts[3] == "rebuild" && r.Method == http.MethodPost:
			s.rebuildThumbnail(ctx, w, docID)

		case len(parts) == 4 && parts[2] == "versions":
			versionID, verr := strconv.ParseInt(parts[3], 10, 64)
			if verr != nil {
				response.Error(w, http.StatusBadRequest, "Invalid version ID")
				return
			}
			switch r.Method {
			case http.MethodGet:
				s.getVersion(ctx, w, docID, versionID)
			default:
				response.Error(w, http.StatusMethodNotAllowed, "Method not allowed")
			}

		case len(parts) == 5 && parts[2] == "versions" && parts[4] == "restore" && r.Method == http.MethodPost:
			versionID, verr := strconv.ParseInt(parts[3], 10, 64)
			if verr != nil {
				response.Error(w, http.StatusBadRequest, "Invalid version ID")
				return
			}
			s.restoreVersion(ctx, w, docID, versionID)

		default:
			response.Error(w, http.StatusNotFound, "Not found")
		}

	default:
		response.Error(w, http.StatusNotFound, "Not found")
	}
}

// --- Document handlers ---

func (s *Service) listDocuments(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	tag := r.URL.Query().Get("tag")
	archived := r.URL.Query().Get("archived") == "true"
	sort := r.URL.Query().Get("sort")

	docs, err := s.store.ListDocuments(ctx, q, tag, archived, sort)
	if err != nil {
		log.Printf("[drawio] list documents: %v", err)
		response.Error(w, http.StatusInternalServerError, "Failed to list documents")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"documents": docs})
}

func (s *Service) getDocument(ctx context.Context, w http.ResponseWriter, docID int64) {
	doc, err := s.store.GetDocument(ctx, docID)
	if err != nil {
		log.Printf("[drawio] get document %d: %v", docID, err)
		response.Error(w, http.StatusInternalServerError, "Failed to get document")
		return
	}
	if doc == nil {
		response.Error(w, http.StatusNotFound, "Document not found")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"document": doc})
}

func (s *Service) createDocument(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var req CreateDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Title == "" {
		req.Title = time.Now().Format("01021504")
	}
	if req.TagsJSON == "" {
		req.TagsJSON = "[]"
	}

	xmlContent := DefaultBlankMXFile()
	doc, err := s.store.CreateDocument(ctx, req.Title, req.Description, req.TagsJSON, xmlContent)
	if err != nil {
		log.Printf("[drawio] create document: %v", err)
		response.Error(w, http.StatusInternalServerError, "Failed to create document")
		return
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{"document": doc})
}

func (s *Service) updateDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, docID int64) {
	var req UpdateDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	doc, err := s.store.UpdateDocument(ctx, docID, req)
	if err != nil {
		log.Printf("[drawio] update document %d: %v", docID, err)
		response.Error(w, http.StatusInternalServerError, "Failed to update document")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"document": doc})
}

func (s *Service) deleteDocument(ctx context.Context, w http.ResponseWriter, docID int64) {
	if err := s.store.DeleteDocument(ctx, docID); err != nil {
		log.Printf("[drawio] delete document %d: %v", docID, err)
		response.Error(w, http.StatusInternalServerError, "Failed to delete document")
		return
	}

	response.OK(w, map[string]interface{}{"deleted": true})
}

func (s *Service) cloneDocument(ctx context.Context, w http.ResponseWriter, docID int64) {
	doc, err := s.store.CloneDocument(ctx, docID)
	if err != nil {
		log.Printf("[drawio] clone document %d: %v", docID, err)
		response.Error(w, http.StatusInternalServerError, "Failed to clone document")
		return
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{"document": doc})
}

func (s *Service) importDocument(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	// 限制上传大小
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		response.Error(w, http.StatusBadRequest, "File too large or invalid form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "No file provided")
		return
	}
	defer file.Close()

	raw, err := io.ReadAll(file)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to read file")
		return
	}

	xmlContent, err := NormalizeXML(string(raw))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "Unrecognized file format: "+err.Error())
		return
	}

	// 从文件名提取标题
	title := strings.TrimSuffix(header.Filename, ".drawio")
	title = strings.TrimSuffix(title, ".xml")

	doc, err := s.store.CreateDocument(ctx, title, "", "[]", xmlContent)
	if err != nil {
		log.Printf("[drawio] import document: %v", err)
		response.Error(w, http.StatusInternalServerError, "Failed to import document")
		return
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{"document": doc})
}

// --- Draft handlers ---

func (s *Service) getDraft(ctx context.Context, w http.ResponseWriter, docID int64) {
	draft, err := s.store.GetDraft(ctx, docID)
	if err != nil {
		log.Printf("[drawio] get draft %d: %v", docID, err)
		response.Error(w, http.StatusInternalServerError, "Failed to get draft")
		return
	}

	if draft == nil {
		response.Error(w, http.StatusNotFound, "Draft not found")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"draft": draft})
}

func (s *Service) saveDraft(ctx context.Context, w http.ResponseWriter, r *http.Request, docID int64) {
	var req SaveDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// 大小限制检查
	settings, _ := s.store.GetSettings(ctx)
	maxSize := int64(5 << 20) // default 5MB
	if settings != nil && settings.DocumentSizeLimitBytes > 0 {
		maxSize = settings.DocumentSizeLimitBytes
	}
	if int64(len(req.XMLContent)) > maxSize {
		response.Error(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("XML content exceeds size limit (%d bytes)", maxSize))
		return
	}

	draft, newRev, err := s.store.SaveDraft(ctx, docID, req)
	if err != nil {
		if strings.Contains(err.Error(), "conflict") {
			currentRev, _ := s.store.GetDraftRevision(ctx, docID)
			conflict := ConflictResponse{
				CurrentDraftRev: currentRev,
				Message:         "Draft has been modified by another session",
			}
			response.JSON(w, http.StatusConflict, map[string]interface{}{"error": "conflict", "conflict": conflict})
			return
		}
		log.Printf("[drawio] save draft %d: %v", docID, err)
		response.Error(w, http.StatusInternalServerError, "Failed to save draft")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"draft":    draft,
		"draftRev": newRev,
	})
}

func (s *Service) saveThumbnail(ctx context.Context, w http.ResponseWriter, r *http.Request, docID int64) {
	var req UpdateThumbnailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.ThumbnailPath = strings.TrimSpace(req.ThumbnailPath)
	if req.ThumbnailPath == "" {
		response.Error(w, http.StatusBadRequest, "Thumbnail content is required")
		return
	}

	if err := s.store.SaveDocumentThumbnail(ctx, docID, req.ThumbnailPath); err != nil {
		if err == sql.ErrNoRows {
			response.Error(w, http.StatusNotFound, "Document not found")
			return
		}
		log.Printf("[drawio] save thumbnail %d: %v", docID, err)
		response.Error(w, http.StatusInternalServerError, "Failed to save thumbnail")
		return
	}

	response.OK(w, map[string]interface{}{"saved": true})
}

// --- Version handlers ---

func (s *Service) listVersions(ctx context.Context, w http.ResponseWriter, docID int64) {
	versions, err := s.store.ListVersions(ctx, docID)
	if err != nil {
		log.Printf("[drawio] list versions %d: %v", docID, err)
		response.Error(w, http.StatusInternalServerError, "Failed to list versions")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"versions": versions})
}

func (s *Service) getVersion(ctx context.Context, w http.ResponseWriter, docID, versionID int64) {
	version, err := s.store.GetVersion(ctx, versionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "Version not found")
			return
		}
		log.Printf("[drawio] get version %d: %v", versionID, err)
		response.Error(w, http.StatusInternalServerError, "Failed to get version")
		return
	}
	if version == nil {
		response.Error(w, http.StatusNotFound, "Version not found")
		return
	}
	// 版本必须属于当前文档，防止枚举 versionId 读取他人文档（与 export/restore 一致）
	if version.DocumentID != docID {
		response.Error(w, http.StatusNotFound, "Version not found")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"version": version})
}

func (s *Service) saveVersion(ctx context.Context, w http.ResponseWriter, r *http.Request, docID int64) {
	var req SaveVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	version, err := s.store.SaveVersion(ctx, docID, req)
	if err != nil {
		if strings.Contains(err.Error(), "conflict") {
			response.Error(w, http.StatusConflict, "Draft revision mismatch")
			return
		}
		log.Printf("[drawio] save version %d: %v", docID, err)
		response.Error(w, http.StatusInternalServerError, "Failed to save version")
		return
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{"version": version})
}

func (s *Service) restoreVersion(ctx context.Context, w http.ResponseWriter, docID, versionID int64) {
	if err := s.store.RestoreVersion(ctx, docID, versionID); err != nil {
		if errors.Is(err, errVersionNotFound) || errors.Is(err, errVersionNotBelong) {
			response.Error(w, http.StatusNotFound, "Version not found")
			return
		}
		log.Printf("[drawio] restore version %d -> %d: %v", versionID, docID, err)
		response.Error(w, http.StatusInternalServerError, "Failed to restore version")
		return
	}

	response.OK(w, map[string]interface{}{"restored": true})
}

// --- Export handler ---

func (s *Service) exportDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, docID int64) {
	format := r.URL.Query().Get("format")
	source := r.URL.Query().Get("source")

	if format == "" {
		format = "drawio"
	}

	var xmlContent string
	if source == "version" {
		versionIDStr := r.URL.Query().Get("versionId")
		if versionIDStr != "" {
			versionID, _ := strconv.ParseInt(versionIDStr, 10, 64)
			version, err := s.store.GetVersion(ctx, versionID)
			if err != nil || version == nil {
				response.Error(w, http.StatusNotFound, "Version not found")
				return
			}
			// 版本必须属于当前文档，防止枚举 versionId 导出他人文档
			if version.DocumentID != docID {
				response.Error(w, http.StatusNotFound, "Version not found")
				return
			}
			xmlContent = version.XMLContent
		} else {
			response.Error(w, http.StatusBadRequest, "versionId required for version source")
			return
		}
	} else {
		draft, err := s.store.GetDraft(ctx, docID)
		if err != nil || draft == nil {
			response.Error(w, http.StatusNotFound, "Draft not found")
			return
		}
		xmlContent = draft.XMLContent
	}

	switch format {
	case "xml":
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="document-%d.xml"`, docID))
		w.Write([]byte(xmlContent))
	case "svg":
		// SVG export not implemented yet; return placeholder
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><rect width="100" height="100" fill="#eee"/><text x="10" y="50" fill="#999">SVG export not available</text></svg>`))
	default:
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="document-%d.drawio"`, docID))
		w.Write([]byte(xmlContent))
	}
}

// --- Thumbnail handlers ---

func (s *Service) rebuildThumbnail(ctx context.Context, w http.ResponseWriter, docID int64) {
	_, err := s.renderer.EnqueueThumbnailRender(ctx, docID, "draft")
	if err != nil {
		log.Printf("[drawio] enqueue thumbnail rebuild %d: %v", docID, err)
		response.Error(w, http.StatusInternalServerError, "Failed to enqueue thumbnail rebuild")
		return
	}

	response.OK(w, map[string]interface{}{"enqueued": true})
}

func (s *Service) rebuildAllThumbnails(ctx context.Context, w http.ResponseWriter) {
	docs, err := s.store.ListDocuments(ctx, "", "", false, "")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to list documents")
		return
	}

	count := 0
	for _, doc := range docs {
		s.renderer.EnqueueThumbnailRender(ctx, doc.ID, "draft")
		count++
	}

	response.OK(w, map[string]interface{}{"enqueued": count})
}

func (s *Service) listRenderJobs(ctx context.Context, w http.ResponseWriter) {
	db, err := s.store.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx,
		`SELECT id, document_id, version_id, source_kind, target_kind, format, trigger_source,
		status, attempt_count, COALESCE(last_error, ''), created_at, updated_at
		FROM drawio_render_jobs ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to query render jobs")
		return
	}
	defer rows.Close()

	var jobs []RenderJob
	for rows.Next() {
		var j RenderJob
		var versionID sql.NullInt64
		if err := rows.Scan(&j.ID, &j.DocumentID, &versionID, &j.SourceKind, &j.TargetKind,
			&j.Format, &j.TriggerSource, &j.Status, &j.AttemptCount, &j.LastError,
			&j.CreatedAt, &j.UpdatedAt); err != nil {
			continue
		}
		if versionID.Valid {
			j.VersionID = &versionID.Int64
		}
		jobs = append(jobs, j)
	}
	if jobs == nil {
		jobs = []RenderJob{}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"jobs": jobs})
}

// --- Settings handlers ---

func (s *Service) getSettings(ctx context.Context, w http.ResponseWriter) {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		log.Printf("[drawio] get settings: %v", err)
		response.Error(w, http.StatusInternalServerError, "Failed to get settings")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"settings": settings})
}

func (s *Service) updateSettings(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var settings SettingsPayload
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := s.store.UpdateSettings(ctx, settings); err != nil {
		log.Printf("[drawio] update settings: %v", err)
		response.Error(w, http.StatusInternalServerError, "Failed to update settings")
		return
	}

	response.OK(w, map[string]interface{}{"updated": true})
}
