package prompts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

type Service struct {
	cfg   config.Config
	store *Store
}

func New(cfg config.Config) *Service {
	store := NewStore(cfg)
	return &Service{cfg: cfg, store: store}
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/prompts")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	ctx := r.Context()

	switch {
	// Collections
	case path == "collections" && r.Method == http.MethodGet:
		s.listCollections(ctx, w, r)
	case path == "collections" && r.Method == http.MethodPost:
		s.createCollection(ctx, w, r)

	// Entries
	case path == "entries" && r.Method == http.MethodGet:
		s.listEntries(ctx, w, r)
	case path == "entries" && r.Method == http.MethodPost:
		s.createEntry(ctx, w, r)

	// Settings
	case path == "settings" && r.Method == http.MethodGet:
		s.getSettings(ctx, w)
	case path == "settings" && r.Method == http.MethodPut:
		s.updateSettings(ctx, w, r)

	default:
		// Routes with IDs
		if len(parts) >= 2 {
			switch {
			case parts[0] == "collections":
				id, err := strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					response.Error(w, http.StatusBadRequest, "Invalid ID")
					return
				}
				switch r.Method {
				case http.MethodPut:
					s.updateCollection(ctx, w, r, id)
				case http.MethodDelete:
					s.deleteCollection(ctx, w, id)
				default:
					response.Error(w, http.StatusMethodNotAllowed, "")
				}

			case parts[0] == "entries":
				id, err := strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					response.Error(w, http.StatusBadRequest, "Invalid ID")
					return
				}
				switch {
				case len(parts) == 2 && r.Method == http.MethodGet:
					s.getEntry(ctx, w, id)
				case len(parts) == 2 && r.Method == http.MethodPut:
					s.updateEntry(ctx, w, r, id)
				case len(parts) == 2 && r.Method == http.MethodDelete:
					s.deleteEntry(ctx, w, id)
				case len(parts) == 3 && parts[2] == "duplicate" && r.Method == http.MethodPost:
					s.duplicateEntry(ctx, w, id)
				case len(parts) == 3 && parts[2] == "draft" && r.Method == http.MethodGet:
					s.getDraft(ctx, w, id)
				case len(parts) == 3 && parts[2] == "draft" && r.Method == http.MethodPut:
					s.saveDraft(ctx, w, r, id)
				case len(parts) == 3 && parts[2] == "publish" && r.Method == http.MethodPost:
					s.publishEntry(ctx, w, r, id)
				case len(parts) == 3 && parts[2] == "versions" && r.Method == http.MethodGet:
					s.listVersions(ctx, w, id)
				case len(parts) == 4 && parts[2] == "public" && parts[3] == "regenerate" && r.Method == http.MethodPost:
					s.regeneratePublicID(ctx, w, id)
				case len(parts) == 4 && parts[2] == "versions":
					versionID, verr := strconv.ParseInt(parts[3], 10, 64)
					if verr != nil {
						response.Error(w, http.StatusBadRequest, "Invalid version ID")
						return
					}
					s.getVersion(ctx, w, versionID)
				case len(parts) == 5 && parts[2] == "versions" && parts[4] == "restore" && r.Method == http.MethodPost:
					versionID, verr := strconv.ParseInt(parts[3], 10, 64)
					if verr != nil {
						response.Error(w, http.StatusBadRequest, "Invalid version ID")
						return
					}
					s.restoreVersion(ctx, w, id, versionID)
				default:
					response.Error(w, http.StatusNotFound, "Not found")
				}

			default:
				response.Error(w, http.StatusNotFound, "Not found")
			}
		} else {
			response.Error(w, http.StatusNotFound, "Not found")
		}
	}
}

// --- Collections ---

func (s *Service) listCollections(ctx context.Context, w http.ResponseWriter, _ *http.Request) {
	collections, err := s.store.ListCollections(ctx)
	if err != nil {
		log.Printf("[prompts] list collections: %v", err)
		response.Error(w, http.StatusInternalServerError, "Failed to list collections")
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"collections": collections})
}

func (s *Service) createCollection(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var req CreateCollectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Name == "" {
		response.Error(w, http.StatusBadRequest, "Name is required")
		return
	}
	col, err := s.store.CreateCollection(ctx, req.Name, req.ParentID, req.Description)
	if err != nil {
		log.Printf("[prompts] create collection: %v", err)
		response.Error(w, http.StatusInternalServerError, "Failed to create collection")
		return
	}
	response.JSON(w, http.StatusCreated, map[string]interface{}{"collection": col})
}

func (s *Service) updateCollection(ctx context.Context, w http.ResponseWriter, r *http.Request, id int64) {
	var req UpdateCollectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := s.store.UpdateCollection(ctx, id, req); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to update collection")
		return
	}
	response.OK(w, map[string]interface{}{"updated": true})
}

func (s *Service) deleteCollection(ctx context.Context, w http.ResponseWriter, id int64) {
	if err := s.store.DeleteCollection(ctx, id); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to delete collection")
		return
	}
	response.OK(w, map[string]interface{}{"deleted": true})
}

// --- Entries ---

func (s *Service) listEntries(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	tag := r.URL.Query().Get("tag")
	starred := r.URL.Query().Get("starred") == "true"
	published := r.URL.Query().Get("published") == "true"
	var collectionID *int64
	if collStr := r.URL.Query().Get("collection_id"); collStr != "" {
		id, err := strconv.ParseInt(collStr, 10, 64)
		if err == nil {
			collectionID = &id
		}
	}

	entries, err := s.store.ListEntries(ctx, collectionID, q, tag, starred, published)
	if err != nil {
		log.Printf("[prompts] list entries: %v", err)
		response.Error(w, http.StatusInternalServerError, "Failed to list entries")
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"entries": entries})
}

func (s *Service) createEntry(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var req CreateEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Title == "" {
		response.Error(w, http.StatusBadRequest, "Title is required")
		return
	}
	entry, err := s.store.CreateEntry(ctx, req)
	if err != nil {
		log.Printf("[prompts] create entry: %v", err)
		response.Error(w, http.StatusInternalServerError, "Failed to create entry")
		return
	}
	response.JSON(w, http.StatusCreated, map[string]interface{}{"entry": entry})
}

func (s *Service) getEntry(ctx context.Context, w http.ResponseWriter, id int64) {
	entry, err := s.store.GetEntry(ctx, id)
	if err != nil || entry == nil {
		response.Error(w, http.StatusNotFound, "Entry not found")
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"entry": entry})
}

func (s *Service) updateEntry(ctx context.Context, w http.ResponseWriter, r *http.Request, id int64) {
	var req UpdateEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := s.store.UpdateEntry(ctx, id, req); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to update entry")
		return
	}
	response.OK(w, map[string]interface{}{"updated": true})
}

func (s *Service) deleteEntry(ctx context.Context, w http.ResponseWriter, id int64) {
	if err := s.store.DeleteEntry(ctx, id); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to delete entry")
		return
	}
	response.OK(w, map[string]interface{}{"deleted": true})
}

func (s *Service) duplicateEntry(ctx context.Context, w http.ResponseWriter, id int64) {
	entry, err := s.store.DuplicateEntry(ctx, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to duplicate entry")
		return
	}
	response.JSON(w, http.StatusCreated, map[string]interface{}{"entry": entry})
}

// --- Drafts ---

func (s *Service) getDraft(ctx context.Context, w http.ResponseWriter, id int64) {
	draft, err := s.store.GetDraft(ctx, id)
	if err != nil || draft == nil {
		response.Error(w, http.StatusNotFound, "Draft not found")
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"draft": draft})
}

func (s *Service) saveDraft(ctx context.Context, w http.ResponseWriter, r *http.Request, id int64) {
	var req SaveDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	draft, newRev, err := s.store.SaveDraft(ctx, id, req)
	if err != nil {
		if strings.Contains(err.Error(), "conflict") {
			response.JSON(w, http.StatusConflict, map[string]interface{}{
				"error":             "conflict",
				"current_draft_rev": newRev,
				"message":           "Draft has been modified by another session",
			})
			return
		}
		log.Printf("[prompts] save draft %d: %v", id, err)
		response.Error(w, http.StatusInternalServerError, "Failed to save draft")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"draft":    draft,
		"draftRev": newRev,
	})
}

// --- Publishing ---

func (s *Service) publishEntry(ctx context.Context, w http.ResponseWriter, r *http.Request, id int64) {
	var req PublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	version, err := s.store.PublishVersion(ctx, id, req)
	if err != nil {
		if strings.Contains(err.Error(), "conflict") {
			response.Error(w, http.StatusConflict, "Draft revision mismatch")
			return
		}
		log.Printf("[prompts] publish entry %d: %v", id, err)
		response.Error(w, http.StatusInternalServerError, "Failed to publish")
		return
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{"version": version})
}

func (s *Service) listVersions(ctx context.Context, w http.ResponseWriter, id int64) {
	versions, err := s.store.ListVersions(ctx, id)
	if err != nil {
		log.Printf("[prompts] list versions %d: %v", id, err)
		response.Error(w, http.StatusInternalServerError, "Failed to list versions")
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"versions": versions})
}

func (s *Service) getVersion(ctx context.Context, w http.ResponseWriter, versionID int64) {
	version, err := s.store.GetVersion(ctx, versionID)
	if err != nil || version == nil {
		response.Error(w, http.StatusNotFound, "Version not found")
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"version": version})
}

func (s *Service) restoreVersion(ctx context.Context, w http.ResponseWriter, entryID, versionID int64) {
	if err := s.store.RestoreVersion(ctx, entryID, versionID); err != nil {
		if errors.Is(err, errVersionNotFound) || errors.Is(err, errVersionNotBelong) {
			response.Error(w, http.StatusNotFound, "Version not found")
			return
		}
		log.Printf("[prompts] restore version: %v", err)
		response.Error(w, http.StatusInternalServerError, "Failed to restore version")
		return
	}
	response.OK(w, map[string]interface{}{"restored": true})
}

func (s *Service) regeneratePublicID(ctx context.Context, w http.ResponseWriter, id int64) {
	db, err := s.store.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer db.Close()

	publicID := GeneratePublicID()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx,
		`UPDATE prompt_entries SET public_id = ?, updated_at = ? WHERE id = ?`,
		publicID, now, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to regenerate public ID")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"public_id": publicID})
}

// --- Settings ---

func (s *Service) getSettings(ctx context.Context, w http.ResponseWriter) {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		log.Printf("[prompts] get settings: %v", err)
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
		response.Error(w, http.StatusInternalServerError, "Failed to update settings")
		return
	}
	response.OK(w, map[string]interface{}{"updated": true})
}

// EnsureSchema 在 Server 初始化时调用以确保 schema 存在
func (s *Service) EnsureSchema(ctx context.Context) error {
	db, err := s.store.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	return ensureSchema(ctx, db)
}

// unused imports are okay; sql may be used
var _ = fmt.Sprintf("%v", sql.NullInt64{})
