package drawio

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func testService(t *testing.T) *Service {
	t.Helper()
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	t.Cleanup(service.Stop)
	return service
}

func TestDocumentRoutesAndDraftConflict(t *testing.T) {
	service := testService(t)
	create := httptest.NewRequest(http.MethodPost, "/api/drawio/documents", strings.NewReader(`{"title":"架构图","tags_json":"[]"}`))
	created := httptest.NewRecorder()
	service.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var payload struct {
		Document DocumentDetail `json:"document"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}

	get := httptest.NewRecorder()
	service.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/drawio/documents/1", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", get.Code, get.Body.String())
	}

	firstXML := strings.Replace(DefaultBlankMXFile(), "Page-1", "第一版", 1)
	first := httptest.NewRecorder()
	service.ServeHTTP(first, httptest.NewRequest(http.MethodPut, "/api/drawio/documents/1/draft", strings.NewReader(`{"xml_content":`+strconvQuote(firstXML)+`,"expected_draft_rev":1}`)))
	if first.Code != http.StatusOK {
		t.Fatalf("first save status = %d, body = %s", first.Code, first.Body.String())
	}

	conflict := httptest.NewRecorder()
	service.ServeHTTP(conflict, httptest.NewRequest(http.MethodPut, "/api/drawio/documents/1/draft", strings.NewReader(`{"xml_content":"bad","expected_draft_rev":1}`)))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, body = %s", conflict.Code, conflict.Body.String())
	}
	draft, err := service.store.GetDraft(context.Background(), payload.Document.ID)
	if err != nil || draft.XMLContent != firstXML {
		t.Fatalf("conflict changed draft: err=%v xml=%q", err, draft.XMLContent)
	}
}

func TestCreateDocumentUsesAutomaticTitle(t *testing.T) {
	service := testService(t)
	create := httptest.NewRequest(http.MethodPost, "/api/drawio/documents", strings.NewReader(`{"tags_json":"[]"}`))
	created := httptest.NewRecorder()
	service.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var payload struct {
		Document DocumentDetail `json:"document"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Document.Title) != 8 {
		t.Fatalf("automatic title = %q", payload.Document.Title)
	}
	if _, err := time.Parse("01021504", payload.Document.Title); err != nil {
		t.Fatalf("automatic title is not MMDDHHmm: %q", payload.Document.Title)
	}
}

func TestDeleteDocumentRemovesRelatedRecords(t *testing.T) {
	service := testService(t)
	create := httptest.NewRequest(http.MethodPost, "/api/drawio/documents", strings.NewReader(`{"title":"待删除图表","tags_json":"[]"}`))
	created := httptest.NewRecorder()
	service.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}

	deleted := httptest.NewRecorder()
	service.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/api/drawio/documents/1", nil))
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleted.Code, deleted.Body.String())
	}

	get := httptest.NewRecorder()
	service.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/drawio/documents/1", nil))
	if get.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d, body = %s", get.Code, get.Body.String())
	}
}

func TestNormalizeCompressedDrawio(t *testing.T) {
	page := `<mxGraphModel><root><mxCell id="0"/></root></mxGraphModel>`
	var compressed bytes.Buffer
	w, _ := flate.NewWriter(&compressed, flate.DefaultCompression)
	w.Write([]byte(url.QueryEscape(page)))
	w.Close()
	raw := `<mxfile><diagram id="p1" name="Page-1">` + base64.StdEncoding.EncodeToString(compressed.Bytes()) + `</diagram></mxfile>`
	normalized, err := NormalizeXML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(normalized, page) {
		t.Fatalf("normalized XML did not contain page model: %s", normalized)
	}
	pages, _, name := ParsePageInfo(normalized)
	if len(pages) != 1 || name != "Page-1" {
		t.Fatalf("unexpected page info: %#v %q", pages, name)
	}
}

func TestSaveThumbnailAndInvalidateOnDraftSave(t *testing.T) {
	service := testService(t)

	create := httptest.NewRequest(http.MethodPost, "/api/drawio/documents", strings.NewReader(`{"title":"缩略图测试","tags_json":"[]"}`))
	created := httptest.NewRecorder()
	service.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}

	thumbnailPath := "data:image/svg+xml,%3Csvg%20xmlns%3D%22http%3A//www.w3.org/2000/svg%22/%3E"
	saveThumbnail := httptest.NewRecorder()
	service.ServeHTTP(
		saveThumbnail,
		httptest.NewRequest(
			http.MethodPut,
			"/api/drawio/documents/1/thumbnail",
			strings.NewReader(`{"thumbnail_path":"`+thumbnailPath+`"}`),
		),
	)
	if saveThumbnail.Code != http.StatusOK {
		t.Fatalf("save thumbnail status = %d, body = %s", saveThumbnail.Code, saveThumbnail.Body.String())
	}

	doc, err := service.store.GetDocument(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if doc.ThumbnailStatus != "ready" || doc.ThumbnailPath != thumbnailPath {
		t.Fatalf("unexpected thumbnail state after save: %#v", doc)
	}

	nextXML := strings.Replace(DefaultBlankMXFile(), "Page-1", "缩略图更新", 1)
	saveDraft := httptest.NewRecorder()
	service.ServeHTTP(
		saveDraft,
		httptest.NewRequest(
			http.MethodPut,
			"/api/drawio/documents/1/draft",
			strings.NewReader(`{"xml_content":`+strconvQuote(nextXML)+`,"expected_draft_rev":1}`),
		),
	)
	if saveDraft.Code != http.StatusOK {
		t.Fatalf("save draft status = %d, body = %s", saveDraft.Code, saveDraft.Body.String())
	}

	doc, err = service.store.GetDocument(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if doc.ThumbnailStatus != "missing" || doc.ThumbnailPath != "" {
		t.Fatalf("thumbnail was not invalidated: %#v", doc)
	}
}

func strconvQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func TestGetVersionRejectsVersionFromOtherDocument(t *testing.T) {
	service := testService(t)
	for _, title := range []string{"文档一", "文档二"} {
		created := httptest.NewRecorder()
		service.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/api/drawio/documents", strings.NewReader(`{"title":"`+title+`","tags_json":"[]"}`)))
		if created.Code != http.StatusCreated {
			t.Fatalf("create %s status=%d body=%s", title, created.Code, created.Body.String())
		}
	}
	// 文档 1 保存一个版本
	saved := httptest.NewRecorder()
	service.ServeHTTP(saved, httptest.NewRequest(http.MethodPost, "/api/drawio/documents/1/versions", strings.NewReader(`{"summary":"v1","xml_content":"<mxfile/>"}`)))
	if saved.Code != http.StatusCreated {
		t.Fatalf("save version status=%d body=%s", saved.Code, saved.Body.String())
	}
	var payload struct {
		Version struct {
			ID int64 `json:"id"`
		} `json:"version"`
	}
	if err := json.Unmarshal(saved.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	// 归属文档读取成功
	ownDoc := httptest.NewRecorder()
	service.ServeHTTP(ownDoc, httptest.NewRequest(http.MethodGet, "/api/drawio/documents/1/versions/"+strconv.FormatInt(payload.Version.ID, 10), nil))
	if ownDoc.Code != http.StatusOK {
		t.Fatalf("own document version status=%d body=%s", ownDoc.Code, ownDoc.Body.String())
	}
	// 通过他人文档路径枚举 versionId 必须返回 404
	otherDoc := httptest.NewRecorder()
	service.ServeHTTP(otherDoc, httptest.NewRequest(http.MethodGet, "/api/drawio/documents/2/versions/"+strconv.FormatInt(payload.Version.ID, 10), nil))
	if otherDoc.Code != http.StatusNotFound {
		t.Fatalf("cross-document version status=%d body=%s", otherDoc.Code, otherDoc.Body.String())
	}
}
