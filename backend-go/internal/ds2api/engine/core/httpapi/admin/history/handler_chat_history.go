package history

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/usagestats"
)

func (h *Handler) getUsage(w http.ResponseWriter, _ *http.Request) {
	store := usagestats.Global()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "usage stats store is not configured"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": store.Summary(),
		"path":  store.Path(),
	})
}

func (h *Handler) clearUsage(w http.ResponseWriter, _ *http.Request) {
	store := usagestats.Global()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "usage stats store is not configured"})
		return
	}
	if err := store.Clear(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) getUsageSettings(w http.ResponseWriter, _ *http.Request) {
	store := usagestats.Global()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "usage stats store is not configured"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": store.Settings()})
}

func (h *Handler) updateUsageSettings(w http.ResponseWriter, r *http.Request) {
	store := usagestats.Global()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "usage stats store is not configured"})
		return
	}
	var body struct {
		Settings usagestats.UsageSettings `json:"settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid JSON body"})
		return
	}
	defer func() {
		if err := r.Body.Close(); err != nil {
			log.Printf("[admin_usage] close request body: %v", err)
		}
	}()
	if err := store.SaveSettings(body.Settings); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, usagestats.ErrInvalidUsageSettings) {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": store.Settings()})
}

func (h *Handler) getChatHistory(w http.ResponseWriter, r *http.Request) {
	store := h.ChatHistory
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "chat history store is not configured"})
		return
	}
	ifNoneMatch := strings.TrimSpace(r.Header.Get("If-None-Match"))
	if ifNoneMatch != "" {
		revision, err := store.Revision()
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"detail": err.Error(),
				"path":   store.Path(),
			})
			return
		}
		etag := chathistory.ListETag(revision)
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if ifNoneMatch == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"detail": err.Error(),
			"path":   store.Path(),
		})
		return
	}
	etag := chathistory.ListETag(snapshot.Revision)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if ifNoneMatch == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":  snapshot.Version,
		"limit":    snapshot.Limit,
		"revision": snapshot.Revision,
		"items":    snapshot.Items,
		"path":     store.Path(),
	})
}

func (h *Handler) getChatHistoryItem(w http.ResponseWriter, r *http.Request) {
	store := h.ChatHistory
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "chat history store is not configured"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "history id is required"})
		return
	}
	ifNoneMatch := strings.TrimSpace(r.Header.Get("If-None-Match"))
	if ifNoneMatch != "" {
		revision, err := store.DetailRevision(id)
		if err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]any{"detail": err.Error()})
			return
		}
		etag := chathistory.DetailETag(id, revision)
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if ifNoneMatch == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	item, err := store.Get(id)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"detail": err.Error()})
		return
	}
	etag := chathistory.DetailETag(item.ID, item.Revision)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if ifNoneMatch == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item": item,
	})
}

func (h *Handler) clearChatHistory(w http.ResponseWriter, _ *http.Request) {
	store := h.ChatHistory
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "chat history store is not configured"})
		return
	}
	if err := store.Clear(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": err.Error(), "path": store.Path()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) deleteChatHistoryItem(w http.ResponseWriter, r *http.Request) {
	store := h.ChatHistory
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "chat history store is not configured"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "history id is required"})
		return
	}
	if err := store.Delete(id); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) updateChatHistorySettings(w http.ResponseWriter, r *http.Request) {
	store := h.ChatHistory
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "chat history store is not configured"})
		return
	}
	var body struct {
		Limit int `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}
	snapshot, err := store.SetLimit(body.Limit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"limit":    snapshot.Limit,
		"revision": snapshot.Revision,
		"items":    snapshot.Items,
	})
}
