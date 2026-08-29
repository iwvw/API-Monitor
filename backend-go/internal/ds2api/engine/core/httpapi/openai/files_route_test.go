package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	dsclient "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/client"
)

type managedFilesAuthStub struct{}

func (managedFilesAuthStub) Determine(_ *http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{
		UseConfigToken: true,
		DeepSeekToken:  "managed-token",
		CallerID:       "caller:test",
		AccountID:      "acct-123",
		TriedAccounts:  map[string]bool{},
	}, nil
}

func (managedFilesAuthStub) DetermineCaller(_ *http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{
		UseConfigToken: true,
		DeepSeekToken:  "managed-token",
		CallerID:       "caller:test",
		AccountID:      "acct-123",
		TriedAccounts:  map[string]bool{},
	}, nil
}

func (managedFilesAuthStub) Release(_ *auth.RequestAuth) {}

func (managedFilesAuthStub) SetAccountMutedUntil(_ *auth.RequestAuth, _ float64) {}

func (managedFilesAuthStub) SetAccountBanned(_ *auth.RequestAuth, _ string) {}

type filesRouteDSStub struct {
	lastReq dsclient.UploadFileRequest
	upload  *dsclient.UploadFileResult
	fetched *dsclient.UploadFileResult
	err     error
}

func (m *filesRouteDSStub) CreateSession(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "", nil
}

func (m *filesRouteDSStub) GetPow(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "", nil
}

func (m *filesRouteDSStub) UploadFile(_ context.Context, _ *auth.RequestAuth, req dsclient.UploadFileRequest, _ int) (*dsclient.UploadFileResult, error) {
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	if m.upload != nil {
		return m.upload, nil
	}
	return &dsclient.UploadFileResult{ID: "file-123", Filename: req.Filename, Bytes: int64(len(req.Data)), Purpose: req.Purpose, Status: "uploaded"}, nil
}

func (m *filesRouteDSStub) FetchUploadedFile(_ context.Context, _ *auth.RequestAuth, fileID string) (*dsclient.UploadFileResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.fetched != nil {
		return m.fetched, nil
	}
	return &dsclient.UploadFileResult{ID: fileID, Filename: "notes.txt", Bytes: 11, Purpose: "assistants", Status: "processed"}, nil
}

func (m *filesRouteDSStub) CallCompletion(_ context.Context, _ *auth.RequestAuth, _ map[string]any, _ string, _ int) (*http.Response, error) {
	return nil, errors.New("not implemented")
}

func (m *filesRouteDSStub) StopStream(_ context.Context, _ *auth.RequestAuth, _ string, _ int) error {
	return nil
}

func (m *filesRouteDSStub) FireCompletionAndStop(_ context.Context, _ *auth.RequestAuth, _ map[string]any, _ string) (int, error) {
	return 0, nil
}

func (m *filesRouteDSStub) DeleteSessionForToken(_ context.Context, _ string, _ string) (*dsclient.DeleteSessionResult, error) {
	return &dsclient.DeleteSessionResult{Success: true}, nil
}

func (m *filesRouteDSStub) DeleteAllSessionsForToken(_ context.Context, _ string) error {
	return nil
}

func newMultipartUploadRequest(t *testing.T, purpose string, filename string, data []byte, model string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if purpose != "" {
		if err := writer.WriteField("purpose", purpose); err != nil {
			t.Fatalf("write purpose failed: %v", err)
		}
	}
	if model != "" {
		if err := writer.WriteField("model", model); err != nil {
			t.Fatalf("write model failed: %v", err)
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file failed: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/files", &body)
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestFilesRouteUploadSuccess(t *testing.T) {
	ds := &filesRouteDSStub{}
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: streamStatusAuthStub{}, DS: ds}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	req := newMultipartUploadRequest(t, "assistants", "notes.txt", []byte("hello world"), "deepseek-v4-vision")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if ds.lastReq.Filename != "notes.txt" {
		t.Fatalf("expected filename notes.txt, got %q", ds.lastReq.Filename)
	}
	if ds.lastReq.Purpose != "assistants" {
		t.Fatalf("expected purpose assistants, got %q", ds.lastReq.Purpose)
	}
	if ds.lastReq.ModelType != "vision" {
		t.Fatalf("expected vision model type, got %q", ds.lastReq.ModelType)
	}
	if string(ds.lastReq.Data) != "hello world" {
		t.Fatalf("unexpected uploaded data: %q", string(ds.lastReq.Data))
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, rec.Body.String())
	}
	if out["object"] != "file" {
		t.Fatalf("expected file object, got %#v", out)
	}
	if out["id"] != "file-123" {
		t.Fatalf("expected file id file-123, got %#v", out["id"])
	}
	if out["filename"] != "notes.txt" {
		t.Fatalf("expected filename notes.txt, got %#v", out["filename"])
	}
}

func TestFilesRouteUploadIncludesAccountIDForManagedAccount(t *testing.T) {
	ds := &filesRouteDSStub{}
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: managedFilesAuthStub{}, DS: ds}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	req := newMultipartUploadRequest(t, "assistants", "notes.txt", []byte("hello world"), "deepseek-v4-vision")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, rec.Body.String())
	}
	if out["account_id"] != "acct-123" {
		t.Fatalf("expected account_id acct-123, got %#v", out["account_id"])
	}
}

func TestFilesRouteRetrieveSuccess(t *testing.T) {
	ds := &filesRouteDSStub{fetched: &dsclient.UploadFileResult{
		ID:       "file-123",
		Filename: "notes.txt",
		Bytes:    11,
		Purpose:  "assistants",
		Status:   "processed",
	}}
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: managedFilesAuthStub{}, DS: ds}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	req := httptest.NewRequest(http.MethodGet, "/v1/files/file-123", nil)
	req.Header.Set("Authorization", "Bearer direct-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, rec.Body.String())
	}
	if out["id"] != "file-123" || out["filename"] != "notes.txt" || out["status"] != "processed" {
		t.Fatalf("unexpected file object: %#v", out)
	}
	if out["account_id"] != "acct-123" {
		t.Fatalf("expected account_id acct-123, got %#v", out["account_id"])
	}
}

func TestFilesRouteRetrieveNotFound(t *testing.T) {
	ds := &filesRouteDSStub{err: dsclient.ErrUploadFileNotFound}
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: streamStatusAuthStub{}, DS: ds}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	req := httptest.NewRequest(http.MethodGet, "/v1/files/missing-file", nil)
	req.Header.Set("Authorization", "Bearer direct-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFilesRouteRejectsNonMultipart(t *testing.T) {
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: streamStatusAuthStub{}, DS: &filesRouteDSStub{}}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	req := httptest.NewRequest(http.MethodPost, "/v1/files", bytes.NewBufferString(`{"purpose":"assistants"}`))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFilesRouteRequiresFileField(t *testing.T) {
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: streamStatusAuthStub{}, DS: &filesRouteDSStub{}}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("purpose", "assistants"); err != nil {
		t.Fatalf("write field failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/files", &body)
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}
