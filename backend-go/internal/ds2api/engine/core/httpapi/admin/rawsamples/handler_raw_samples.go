package rawsamples

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/devcapture"
	adminshared "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/shared"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/rawsample"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/util"
)

type captureChain struct {
	Key     string
	Entries []devcapture.Entry
}

func (h *Handler) captureRawSample(w http.ResponseWriter, r *http.Request) {
	if h.OpenAI == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "OpenAI handler is not configured"})
		return
	}

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}

	payload, sampleID, apiKey, err := prepareRawSampleCaptureRequest(h.Store, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}

	body, err := json.Marshal(payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to encode capture request"})
		return
	}

	traceID := rawsample.NormalizeSampleID(sampleID)
	if traceID == "" {
		traceID = rawsample.DefaultSampleID("capture")
	}

	before := devcapture.Global().Snapshot()
	rec := httptest.NewRecorder()
	captureReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__trace_id="+url.QueryEscape(traceID), bytes.NewReader(body))
	captureReq.Header.Set("Authorization", "Bearer "+apiKey)
	captureReq.Header.Set("Content-Type", "application/json")
	h.OpenAI.ChatCompletions(rec, captureReq)
	after := devcapture.Global().Snapshot()

	if rec.Code >= http.StatusBadRequest {
		copyHeader(w.Header(), rec.Header())
		w.WriteHeader(rec.Code)
		_, _ = io.Copy(w, bytes.NewReader(rec.Body.Bytes()))
		return
	}

	captureEntries, err := collectNewCaptureEntries(before, after)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
		return
	}

	saved, err := rawsample.Persist(rawsample.PersistOptions{
		RootDir:      config.RawStreamSampleRoot(),
		SampleID:     sampleID,
		Source:       "admin/dev/raw-samples/capture",
		Request:      payload,
		Capture:      captureSummaryFromEntries(captureEntries),
		UpstreamBody: combineCaptureBodies(captureEntries),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
		return
	}

	copyHeader(w.Header(), rec.Header())
	w.Header().Set("X-Ds2-Sample-Id", saved.SampleID)
	w.Header().Set("X-Ds2-Sample-Dir", saved.Dir)
	w.Header().Set("X-Ds2-Sample-Meta", saved.MetaPath)
	w.Header().Set("X-Ds2-Sample-Upstream", saved.UpstreamPath)
	w.WriteHeader(rec.Code)
	_, _ = io.Copy(w, bytes.NewReader(rec.Body.Bytes()))
}

func prepareRawSampleCaptureRequest(store adminshared.ConfigStore, req map[string]any) (map[string]any, string, string, error) {
	payload := cloneMap(req)
	sampleID := strings.TrimSpace(fieldString(payload, "sample_id"))
	apiKey := strings.TrimSpace(fieldString(payload, "api_key"))

	for _, k := range []string{"sample_id", "api_key", "promote_default", "persist", "source"} {
		delete(payload, k)
	}

	if apiKey == "" {
		if store == nil {
			return nil, "", "", fmt.Errorf("no api key provided")
		}
		keys := store.Keys()
		if len(keys) == 0 {
			return nil, "", "", fmt.Errorf("no api key available")
		}
		apiKey = strings.TrimSpace(keys[0])
	}

	if model := strings.TrimSpace(fieldString(payload, "model")); model == "" {
		payload["model"] = "deepseek-v4-flash"
	}
	if _, ok := payload["stream"]; !ok {
		payload["stream"] = true
	}

	if messagesRaw, ok := payload["messages"].([]any); !ok || len(messagesRaw) == 0 {
		message := strings.TrimSpace(fieldString(payload, "message"))
		if message == "" {
			message = "你好"
		}
		payload["messages"] = []map[string]any{{"role": "user", "content": message}}
	}
	delete(payload, "message")

	if sampleID == "" {
		model := strings.TrimSpace(fieldString(payload, "model"))
		if model == "" {
			model = "capture"
		}
		sampleID = rawsample.DefaultSampleID(model)
	}

	return payload, sampleID, apiKey, nil
}

func collectNewCaptureEntries(before, after []devcapture.Entry) ([]devcapture.Entry, error) {
	beforeIDs := make(map[string]struct{}, len(before))
	for _, entry := range before {
		beforeIDs[entry.ID] = struct{}{}
	}

	entries := make([]devcapture.Entry, 0, len(after))
	for _, entry := range after {
		if _, ok := beforeIDs[entry.ID]; ok {
			continue
		}
		if strings.TrimSpace(entry.ResponseBody) == "" {
			continue
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no upstream capture was recorded")
	}

	// Snapshot order is newest-first; reverse to preserve the actual request order.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

func captureSummaryFromEntries(entries []devcapture.Entry) rawsample.CaptureSummary {
	if len(entries) == 0 {
		return rawsample.CaptureSummary{}
	}

	// Primary metadata comes from the first (initial) capture.
	summary := rawsample.CaptureSummary{
		Label:      strings.TrimSpace(entries[0].Label),
		URL:        strings.TrimSpace(entries[0].URL),
		StatusCode: entries[0].StatusCode,
	}

	// Record every round (initial + continuations) so replay/debug
	// can reconstruct the full multi-round interaction.
	totalBytes := 0
	rounds := make([]rawsample.CaptureRound, 0, len(entries))
	for _, entry := range entries {
		n := len(entry.ResponseBody)
		totalBytes += n
		rounds = append(rounds, rawsample.CaptureRound{
			Label:         strings.TrimSpace(entry.Label),
			URL:           strings.TrimSpace(entry.URL),
			StatusCode:    entry.StatusCode,
			ResponseBytes: n,
		})
	}
	summary.ResponseBytes = totalBytes
	if len(rounds) > 1 {
		summary.Rounds = rounds
	}
	return summary
}

func combineCaptureBodies(entries []devcapture.Entry) []byte {
	if len(entries) == 0 {
		return nil
	}

	var buf bytes.Buffer
	for _, entry := range entries {
		if buf.Len() > 0 {
			last := buf.Bytes()[buf.Len()-1]
			if last != '\n' {
				buf.WriteByte('\n')
			}
		}
		buf.WriteString(entry.ResponseBody)
	}
	return buf.Bytes()
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		dst.Del(k)
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (h *Handler) queryRawSampleCaptures(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := intFromQuery(r, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	chains := buildCaptureChains(devcapture.Global().Snapshot())
	items := make([]map[string]any, 0, len(chains))
	for _, chain := range chains {
		if query != "" && !captureChainMatchesQuery(chain, query) {
			continue
		}
		items = append(items, buildCaptureChainQueryItem(chain, query))
		if len(items) >= limit {
			break
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"query": query,
		"limit": limit,
		"count": len(items),
		"items": items,
	})
}

func (h *Handler) saveRawSampleFromCaptures(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}

	snapshot := devcapture.Global().Snapshot()
	if len(snapshot) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "no capture logs available"})
		return
	}

	chain, err := resolveCaptureChainSelection(snapshot, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}

	sampleID := strings.TrimSpace(fieldString(req, "sample_id"))
	source := strings.TrimSpace(fieldString(req, "source"))
	if source == "" {
		source = "admin/dev/raw-samples/save"
	}
	requestPayload := captureChainRequestPayload(chain)

	saved, err := rawsample.Persist(rawsample.PersistOptions{
		RootDir:      config.RawStreamSampleRoot(),
		SampleID:     sampleID,
		Source:       source,
		Request:      requestPayload,
		Capture:      captureSummaryFromEntries(chain.Entries),
		UpstreamBody: combineCaptureBodies(chain.Entries),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"sample_id":     saved.SampleID,
		"sample_dir":    saved.Dir,
		"meta_path":     saved.MetaPath,
		"upstream_path": saved.UpstreamPath,
		"chain_key":     chain.Key,
		"capture_ids":   captureChainIDs(chain),
		"round_count":   len(chain.Entries),
	})
}

func buildCaptureChains(snapshot []devcapture.Entry) []captureChain {
	if len(snapshot) == 0 {
		return nil
	}
	ordered := make([]devcapture.Entry, len(snapshot))
	// devcapture snapshots are newest-first because the store prepends entries.
	// Reverse once so equal-second timestamps can preserve the actual capture
	// order (completion before continue) under the stable CreatedAt sort below.
	for i := range snapshot {
		ordered[len(snapshot)-1-i] = snapshot[i]
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].CreatedAt < ordered[j].CreatedAt
	})

	byKey := make(map[string]*captureChain, len(ordered))
	keys := make([]string, 0, len(ordered))
	for _, entry := range ordered {
		key := captureChainKey(entry)
		if key == "" {
			key = "capture:" + entry.ID
		}
		if _, ok := byKey[key]; !ok {
			byKey[key] = &captureChain{Key: key}
			keys = append(keys, key)
		}
		byKey[key].Entries = append(byKey[key].Entries, entry)
	}

	chains := make([]captureChain, 0, len(keys))
	for _, key := range keys {
		chains = append(chains, *byKey[key])
	}
	sort.SliceStable(chains, func(i, j int) bool {
		return latestCreatedAt(chains[i]) > latestCreatedAt(chains[j])
	})
	return chains
}

func captureChainKey(entry devcapture.Entry) string {
	req := parseCaptureRequestBody(entry.RequestBody)
	if sessionID := strings.TrimSpace(fieldString(req, "chat_session_id")); sessionID != "" {
		return "session:" + sessionID
	}
	return "capture:" + entry.ID
}

func parseCaptureRequestBody(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func latestCreatedAt(chain captureChain) int64 {
	var latest int64
	for _, entry := range chain.Entries {
		if entry.CreatedAt > latest {
			latest = entry.CreatedAt
		}
	}
	return latest
}

func captureChainMatchesQuery(chain captureChain, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	for _, entry := range chain.Entries {
		hay := strings.ToLower(strings.Join([]string{
			entry.Label,
			entry.URL,
			entry.AccountID,
			entry.RequestBody,
			entry.ResponseBody,
		}, "\n"))
		if strings.Contains(hay, query) {
			return true
		}
	}
	return false
}

func buildCaptureChainQueryItem(chain captureChain, query string) map[string]any {
	first := chain.Entries[0]
	last := chain.Entries[len(chain.Entries)-1]
	requestPreview := previewCaptureChainRequest(chain)
	responsePreview := previewCaptureChainResponse(chain)

	return map[string]any{
		"chain_key":          chain.Key,
		"capture_ids":        captureChainIDs(chain),
		"created_at":         latestCreatedAt(chain),
		"round_count":        len(chain.Entries),
		"account_id":         nilIfEmpty(strings.TrimSpace(first.AccountID)),
		"initial_label":      first.Label,
		"initial_url":        first.URL,
		"latest_label":       last.Label,
		"latest_url":         last.URL,
		"request_preview":    requestPreview,
		"response_preview":   responsePreview,
		"query":              query,
		"response_truncated": captureChainHasTruncatedResponse(chain),
	}
}

func captureChainIDs(chain captureChain) []string {
	out := make([]string, 0, len(chain.Entries))
	for _, entry := range chain.Entries {
		out = append(out, entry.ID)
	}
	return out
}

func previewCaptureChainRequest(chain captureChain) string {
	for _, entry := range chain.Entries {
		req := parseCaptureRequestBody(entry.RequestBody)
		if prompt := strings.TrimSpace(fieldString(req, "prompt")); prompt != "" {
			return previewText(prompt, 280)
		}
		if messages, ok := req["messages"].([]any); ok {
			var parts []string
			for _, item := range messages {
				m, _ := item.(map[string]any)
				content := strings.TrimSpace(fieldString(m, "content"))
				if content != "" {
					parts = append(parts, content)
				}
			}
			if len(parts) > 0 {
				return previewText(strings.Join(parts, "\n"), 280)
			}
		}
	}
	return previewText(strings.TrimSpace(chain.Entries[0].RequestBody), 280)
}

func previewCaptureChainResponse(chain captureChain) string {
	var b strings.Builder
	for _, entry := range chain.Entries {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimSpace(entry.ResponseBody))
		if b.Len() >= 280 {
			break
		}
	}
	return previewText(b.String(), 280)
}

func previewText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 {
		return text
	}
	if truncated, ok := util.TruncateRunes(text, limit); ok {
		return truncated + "..."
	}
	return text
}

func captureChainHasTruncatedResponse(chain captureChain) bool {
	for _, entry := range chain.Entries {
		if entry.ResponseTruncated {
			return true
		}
	}
	return false
}

func resolveCaptureChainSelection(snapshot []devcapture.Entry, req map[string]any) (captureChain, error) {
	chains := buildCaptureChains(snapshot)
	if len(chains) == 0 {
		return captureChain{}, fmt.Errorf("no capture logs available")
	}

	if chainKey := strings.TrimSpace(fieldString(req, "chain_key")); chainKey != "" {
		for _, chain := range chains {
			if chain.Key == chainKey {
				return chain, nil
			}
		}
		return captureChain{}, fmt.Errorf("capture chain not found")
	}

	captureID := strings.TrimSpace(fieldString(req, "capture_id"))
	if captureID == "" {
		if ids, ok := toStringSlice(req["capture_ids"]); ok && len(ids) > 0 {
			captureID = strings.TrimSpace(ids[0])
		}
	}
	if captureID != "" {
		for _, chain := range chains {
			for _, entry := range chain.Entries {
				if entry.ID == captureID {
					return chain, nil
				}
			}
		}
		return captureChain{}, fmt.Errorf("capture id not found")
	}

	query := strings.TrimSpace(fieldString(req, "query"))
	if query != "" {
		for _, chain := range chains {
			if captureChainMatchesQuery(chain, query) {
				return chain, nil
			}
		}
		return captureChain{}, fmt.Errorf("no capture chain matched query")
	}

	return captureChain{}, fmt.Errorf("capture_id, chain_key, or query is required")
}

func captureChainRequestPayload(chain captureChain) any {
	for _, entry := range chain.Entries {
		if req := parseCaptureRequestBody(entry.RequestBody); req != nil {
			return req
		}
	}
	return strings.TrimSpace(chain.Entries[0].RequestBody)
}
