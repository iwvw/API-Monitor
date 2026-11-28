package prompts

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetVersionRejectsForeignEntry(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	entryA, err := service.store.CreateEntry(ctx, CreateEntryRequest{Title: "A", TagsJSON: "[]", Visibility: "unlisted"})
	if err != nil {
		t.Fatal(err)
	}
	entryB, err := service.store.CreateEntry(ctx, CreateEntryRequest{Title: "B", TagsJSON: "[]", Visibility: "unlisted"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.store.SaveDraft(ctx, entryA.ID, SaveDraftRequest{ContentMD: "# A", ExpectedDraftRev: 1}); err != nil {
		t.Fatal(err)
	}
	version, err := service.store.PublishVersion(ctx, entryA.ID, PublishRequest{ExpectedDraftRev: 2})
	if err != nil {
		t.Fatal(err)
	}

	own := httptest.NewRecorder()
	service.ServeHTTP(own, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/prompts/entries/%d/versions/%d", entryA.ID, version.ID), nil))
	if own.Code != http.StatusOK {
		t.Fatalf("own version status=%d body=%s", own.Code, own.Body.String())
	}

	other := httptest.NewRecorder()
	service.ServeHTTP(other, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/prompts/entries/%d/versions/%d", entryB.ID, version.ID), nil))
	if other.Code != http.StatusNotFound {
		t.Fatalf("cross-entry version status=%d body=%s", other.Code, other.Body.String())
	}
}
