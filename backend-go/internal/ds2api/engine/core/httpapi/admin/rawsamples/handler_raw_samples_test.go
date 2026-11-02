package rawsamples

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/devcapture"
)

type stubOpenAIChatCaller struct{}

func (stubOpenAIChatCaller) ChatCompletions(w http.ResponseWriter, _ *http.Request) {
	store := devcapture.Global()
	session := store.Start("deepseek_completion", "https://chat.deepseek.com/api/v0/chat/completion", "acct-test", map[string]any{"model": "deepseek-v4-flash"})
	raw := io.NopCloser(strings.NewReader(
		"data: {\"v\":\"hello [reference:1]\"}\n\n" +
			"data: {\"v\":\"FINISHED\",\"p\":\"response/status\"}\n\n",
	))
	if session != nil {
		raw = session.WrapBody(raw, http.StatusOK)
	}
	_, _ = io.ReadAll(raw)
	_ = raw.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"index\":0}],\"created\":1,\"id\":\"id\",\"model\":\"m\",\"object\":\"chat.completion.chunk\"}\n\n")
}

type stubOpenAIChatCallerWithContinuations struct{}

func (stubOpenAIChatCallerWithContinuations) ChatCompletions(w http.ResponseWriter, _ *http.Request) {
	recordCapturedResponse("deepseek_completion", "https://chat.deepseek.com/api/v0/chat/completion", http.StatusOK, map[string]any{"model": "deepseek-v4-flash"}, "data: {\"v\":\"hello [reference:1]\"}\n\n"+"data: [DONE]\n\n")
	recordCapturedResponse("deepseek_continue", "https://chat.deepseek.com/api/v0/chat/continue", http.StatusOK, map[string]any{"chat_session_id": "session-1", "message_id": 2}, "data: {\"v\":\"continued\"}\n\n"+"data: [DONE]\n\n")

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello continued\"},\"index\":0}],\"created\":1,\"id\":\"id\",\"model\":\"m\",\"object\":\"chat.completion.chunk\"}\n\n")
}

type stubOpenAIChatCallerWithoutCapture struct{}

func (stubOpenAIChatCallerWithoutCapture) ChatCompletions(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"index\":0}],\"created\":1,\"id\":\"id\",\"model\":\"m\",\"object\":\"chat.completion.chunk\"}\n\n")
}

func recordCapturedResponse(label, rawURL string, statusCode int, request any, body string) {
	store := devcapture.Global()
	session := store.Start(label, rawURL, "acct-test", request)
	raw := io.NopCloser(strings.NewReader(body))
	if session != nil {
		raw = session.WrapBody(raw, statusCode)
	}
	_, _ = io.ReadAll(raw)
	_ = raw.Close()
}

func TestCaptureRawSampleWritesPersistentSample(t *testing.T) {
	t.Setenv("DS2API_DEV_PACKET_CAPTURE", "true")
	t.Setenv("DS2API_RAW_STREAM_SAMPLE_ROOT", t.TempDir())
	devcapture.Global().Clear()
	defer devcapture.Global().Clear()

	h := &Handler{OpenAI: stubOpenAIChatCaller{}}
	reqBody := `{
		"sample_id":"My Sample 01",
		"api_key":"local-key",
		"model":"deepseek-v4-flash",
		"message":"广州天气",
		"stream":true
	}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/dev/raw-samples/capture", strings.NewReader(reqBody))
	h.captureRawSample(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Ds2-Sample-Id"); got != "my-sample-01" {
		t.Fatalf("expected sample id header my-sample-01, got %q", got)
	}
	if got := rec.Header().Get("X-Ds2-Sample-Upstream"); got != filepath.Join(os.Getenv("DS2API_RAW_STREAM_SAMPLE_ROOT"), "my-sample-01", "upstream.stream.sse") {
		t.Fatalf("unexpected sample upstream header: %q", got)
	}
	if !strings.Contains(rec.Body.String(), `"content":"hello"`) {
		t.Fatalf("expected proxied openai output, got %s", rec.Body.String())
	}

	sampleDir := filepath.Join(os.Getenv("DS2API_RAW_STREAM_SAMPLE_ROOT"), "my-sample-01")
	if _, err := os.Stat(sampleDir); err != nil {
		t.Fatalf("sample dir missing: %v", err)
	}
	metaBytes, err := os.ReadFile(filepath.Join(sampleDir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if meta["sample_id"] != "my-sample-01" {
		t.Fatalf("unexpected meta sample_id: %#v", meta["sample_id"])
	}
	capture, _ := meta["capture"].(map[string]any)
	if capture == nil {
		t.Fatalf("missing capture meta: %#v", meta)
	}
	if got := int(capture["response_bytes"].(float64)); got == 0 {
		t.Fatalf("expected capture bytes to be recorded, got %#v", capture)
	}
	if _, ok := meta["processed"]; ok {
		t.Fatalf("unexpected processed meta: %#v", meta["processed"])
	}
}

func TestCaptureRawSampleCombinesContinuationCaptures(t *testing.T) {
	t.Setenv("DS2API_DEV_PACKET_CAPTURE", "true")
	t.Setenv("DS2API_RAW_STREAM_SAMPLE_ROOT", t.TempDir())
	devcapture.Global().Clear()
	defer devcapture.Global().Clear()

	h := &Handler{OpenAI: stubOpenAIChatCallerWithContinuations{}}
	reqBody := `{
		"sample_id":"My Sample 02",
		"api_key":"local-key",
		"model":"deepseek-v4-flash",
		"message":"广州天气",
		"stream":true
	}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/dev/raw-samples/capture", strings.NewReader(reqBody))
	h.captureRawSample(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	sampleDir := filepath.Join(os.Getenv("DS2API_RAW_STREAM_SAMPLE_ROOT"), "my-sample-02")
	upstreamBytes, err := os.ReadFile(filepath.Join(sampleDir, "upstream.stream.sse"))
	if err != nil {
		t.Fatalf("read upstream: %v", err)
	}
	upstream := string(upstreamBytes)
	if !strings.Contains(upstream, "hello [reference:1]") {
		t.Fatalf("expected initial capture in combined upstream, got %s", upstream)
	}
	if !strings.Contains(upstream, "continued") {
		t.Fatalf("expected continuation capture in combined upstream, got %s", upstream)
	}
	if strings.Index(upstream, "hello [reference:1]") > strings.Index(upstream, "continued") {
		t.Fatalf("expected initial capture before continuation, got %s", upstream)
	}

	metaBytes, err := os.ReadFile(filepath.Join(sampleDir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	capture, _ := meta["capture"].(map[string]any)
	if capture == nil {
		t.Fatalf("missing capture meta: %#v", meta)
	}
	if got := int(capture["response_bytes"].(float64)); got != len(upstreamBytes) {
		t.Fatalf("expected combined response_bytes %d, got %#v", len(upstreamBytes), capture["response_bytes"])
	}

	rounds, _ := capture["rounds"].([]any)
	if len(rounds) != 2 {
		t.Fatalf("expected 2 capture rounds, got %d: %#v", len(rounds), capture)
	}
	r0, _ := rounds[0].(map[string]any)
	r1, _ := rounds[1].(map[string]any)
	if r0["label"] != "deepseek_completion" {
		t.Fatalf("expected first round label deepseek_completion, got %v", r0["label"])
	}
	if r1["label"] != "deepseek_continue" {
		t.Fatalf("expected second round label deepseek_continue, got %v", r1["label"])
	}
}

func TestCaptureRawSampleReturnsErrorWhenNoNewCaptureRecorded(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DS2API_RAW_STREAM_SAMPLE_ROOT", root)
	devcapture.Global().Clear()
	defer devcapture.Global().Clear()

	recordCapturedResponse("preexisting", "https://chat.deepseek.com/api/v0/chat/completion", http.StatusOK, map[string]any{"model": "deepseek-v4-flash"}, "data: {\"v\":\"old\"}\n\n")

	h := &Handler{OpenAI: stubOpenAIChatCallerWithoutCapture{}}
	reqBody := `{
		"sample_id":"My Sample 03",
		"api_key":"local-key",
		"model":"deepseek-v4-flash",
		"message":"广州天气",
		"stream":true
	}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/dev/raw-samples/capture", strings.NewReader(reqBody))
	h.captureRawSample(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no upstream capture was recorded") {
		t.Fatalf("expected no-capture error, got %s", rec.Body.String())
	}

	if _, err := os.Stat(filepath.Join(root, "my-sample-03")); !os.IsNotExist(err) {
		t.Fatalf("expected no sample dir to be created, stat err=%v", err)
	}
}

func TestCombineCaptureBodiesPreservesOrderAndSeparators(t *testing.T) {
	entries := []devcapture.Entry{
		{ResponseBody: "first"},
		{ResponseBody: "second"},
	}
	got := combineCaptureBodies(entries)
	if !bytes.Equal(got, []byte("first\nsecond")) {
		t.Fatalf("unexpected combined body: %q", string(got))
	}
}

func TestPreviewTextPreservesUTF8MB4Characters(t *testing.T) {
	preview := previewText(strings.Repeat("😀", 281), 280)
	if !utf8.ValidString(preview) {
		t.Fatalf("expected valid utf-8 preview, got %q", preview)
	}
	if preview != strings.Repeat("😀", 280)+"..." {
		t.Fatalf("unexpected preview: %q", preview)
	}
}

func TestQueryRawSampleCapturesGroupsBySessionAndMatchesQuestion(t *testing.T) {
	t.Setenv("DS2API_DEV_PACKET_CAPTURE", "true")
	devcapture.Global().Clear()
	defer devcapture.Global().Clear()

	recordCapturedResponse(
		"deepseek_completion",
		"https://chat.deepseek.com/api/v0/chat/completion",
		http.StatusOK,
		map[string]any{
			"chat_session_id": "session-query-1",
			"prompt":          "用户问题：广州天气怎么样？",
		},
		"data: {\"v\":\"先看天气\"}\n\n",
	)
	recordCapturedResponse(
		"deepseek_continue",
		"https://chat.deepseek.com/api/v0/chat/continue",
		http.StatusOK,
		map[string]any{
			"chat_session_id": "session-query-1",
			"message_id":      2,
		},
		"data: {\"v\":\"再补充一点\"}\n\n",
	)

	h := &Handler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/dev/raw-samples/query?q=广州天气", nil)
	h.queryRawSampleCaptures(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	items, _ := out["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d body=%s", len(items), rec.Body.String())
	}
	item, _ := items[0].(map[string]any)
	if item["chain_key"] != "session:session-query-1" {
		t.Fatalf("unexpected chain key: %#v", item["chain_key"])
	}
	if int(item["round_count"].(float64)) != 2 {
		t.Fatalf("expected 2 rounds, got %#v", item["round_count"])
	}
	reqPreview, _ := item["request_preview"].(string)
	if !strings.Contains(reqPreview, "广州天气") {
		t.Fatalf("expected request preview to contain query, got %q", reqPreview)
	}
}

func TestBuildCaptureChainsPreservesCaptureOrderWhenTimestampsCollide(t *testing.T) {
	snapshot := []devcapture.Entry{
		{
			ID:           "cap_continue",
			CreatedAt:    1712365200,
			Label:        "deepseek_continue",
			RequestBody:  `{"chat_session_id":"session-collision","message_id":2}`,
			ResponseBody: "data: {\"v\":\"第二段\"}\n\n",
		},
		{
			ID:           "cap_completion",
			CreatedAt:    1712365200,
			Label:        "deepseek_completion",
			RequestBody:  `{"chat_session_id":"session-collision","prompt":"题目"}`,
			ResponseBody: "data: {\"v\":\"第一段\"}\n\n",
		},
	}

	chains := buildCaptureChains(snapshot)
	if len(chains) != 1 {
		t.Fatalf("expected 1 chain, got %d", len(chains))
	}
	if len(chains[0].Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(chains[0].Entries))
	}
	if chains[0].Entries[0].Label != "deepseek_completion" {
		t.Fatalf("expected completion first, got %#v", chains[0].Entries)
	}
	if chains[0].Entries[1].Label != "deepseek_continue" {
		t.Fatalf("expected continue second, got %#v", chains[0].Entries)
	}
}

func TestSaveRawSampleFromCapturesPersistsSelectedChain(t *testing.T) {
	t.Setenv("DS2API_DEV_PACKET_CAPTURE", "true")
	root := t.TempDir()
	t.Setenv("DS2API_RAW_STREAM_SAMPLE_ROOT", root)
	devcapture.Global().Clear()
	defer devcapture.Global().Clear()

	recordCapturedResponse(
		"deepseek_completion",
		"https://chat.deepseek.com/api/v0/chat/completion",
		http.StatusOK,
		map[string]any{
			"chat_session_id": "session-save-1",
			"prompt":          "请回答深圳天气",
		},
		"data: {\"v\":\"第一段\"}\n\n",
	)
	recordCapturedResponse(
		"deepseek_continue",
		"https://chat.deepseek.com/api/v0/chat/continue",
		http.StatusOK,
		map[string]any{
			"chat_session_id": "session-save-1",
			"message_id":      2,
		},
		"data: {\"v\":\"第二段\"}\n\n",
	)

	h := &Handler{}
	rec := httptest.NewRecorder()
	reqBody := `{"query":"深圳天气","sample_id":"saved-from-memory"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/dev/raw-samples/save", strings.NewReader(reqBody))
	h.saveRawSampleFromCaptures(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if out["sample_id"] != "saved-from-memory" {
		t.Fatalf("unexpected sample id: %#v", out["sample_id"])
	}
	if int(out["round_count"].(float64)) != 2 {
		t.Fatalf("expected round_count=2, got %#v", out["round_count"])
	}

	sampleDir := filepath.Join(root, "saved-from-memory")
	upstreamBytes, err := os.ReadFile(filepath.Join(sampleDir, "upstream.stream.sse"))
	if err != nil {
		t.Fatalf("read upstream: %v", err)
	}
	upstream := string(upstreamBytes)
	if !strings.Contains(upstream, "第一段") || !strings.Contains(upstream, "第二段") {
		t.Fatalf("expected combined upstream, got %q", upstream)
	}
	metaBytes, err := os.ReadFile(filepath.Join(sampleDir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	reqMeta, _ := meta["request"].(map[string]any)
	if fieldString(reqMeta, "chat_session_id") != "session-save-1" {
		t.Fatalf("expected request to come from selected chain, got %#v", meta["request"])
	}
}
