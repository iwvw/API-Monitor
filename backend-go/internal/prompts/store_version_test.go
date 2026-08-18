package prompts

import (
	"context"
	"errors"
	"testing"
)

func TestPublishVersionLifecycle(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	entry, err := service.store.CreateEntry(ctx, CreateEntryRequest{Title: "版本化", TagsJSON: "[]", Visibility: "unlisted"})
	if err != nil {
		t.Fatal(err)
	}

	draft, revision, err := service.store.SaveDraft(ctx, entry.ID, SaveDraftRequest{ContentMD: "# v1\n\nHello **world**", ExpectedDraftRev: 1})
	if err != nil || revision != 2 {
		t.Fatalf("save draft: revision=%d err=%v", revision, err)
	}

	if _, err := service.store.PublishVersion(ctx, entry.ID, PublishRequest{ExpectedDraftRev: 1}); err == nil {
		t.Fatal("expected publish conflict on stale revision")
	}

	v1, err := service.store.PublishVersion(ctx, entry.ID, PublishRequest{ExpectedDraftRev: revision})
	if err != nil {
		t.Fatal(err)
	}
	if v1.VersionNo != 1 {
		t.Fatalf("version no = %d, want 1", v1.VersionNo)
	}
	if v1.Checksum != ComputeChecksum(draft.ContentMD) {
		t.Fatalf("checksum = %q, want %q", v1.Checksum, ComputeChecksum(draft.ContentMD))
	}
	if v1.CharCount != len(draft.ContentText) || v1.WordCount != CountWords(draft.ContentText) {
		t.Fatalf("counts = %d/%d, want %d/%d", v1.CharCount, v1.WordCount, len(draft.ContentText), CountWords(draft.ContentText))
	}

	detail, err := service.store.GetEntry(ctx, entry.ID)
	if err != nil || detail.LatestPublishedVersionNo != 1 || detail.LatestPublishedVersionID == nil {
		t.Fatalf("entry publish state = %#v err=%v", detail, err)
	}

	published, err := service.store.GetPublishedVersion(ctx, entry.ID)
	if err != nil || published == nil || published.VersionNo != 1 {
		t.Fatalf("published version = %#v err=%v", published, err)
	}

	_, revision2, err := service.store.SaveDraft(ctx, entry.ID, SaveDraftRequest{ContentMD: "# v2\n\nNew content", ExpectedDraftRev: revision})
	if err != nil {
		t.Fatal(err)
	}
	v2, err := service.store.PublishVersion(ctx, entry.ID, PublishRequest{ExpectedDraftRev: revision2})
	if err != nil {
		t.Fatal(err)
	}
	if v2.VersionNo != 2 {
		t.Fatalf("second version no = %d, want 2", v2.VersionNo)
	}

	published2, err := service.store.GetPublishedVersion(ctx, entry.ID)
	if err != nil || published2.VersionNo != 2 {
		t.Fatalf("latest published = %#v err=%v", published2, err)
	}

	byNo, err := service.store.GetPublishedVersionByNo(ctx, entry.ID, 1)
	if err != nil || byNo == nil || byNo.VersionNo != 1 {
		t.Fatalf("version by no = %#v err=%v", byNo, err)
	}
	if missing, err := service.store.GetPublishedVersionByNo(ctx, entry.ID, 99); err != nil || missing != nil {
		t.Fatalf("missing version by no = %#v err=%v", missing, err)
	}

	versions, err := service.store.ListVersions(ctx, entry.ID)
	if err != nil || len(versions) != 2 || versions[0].VersionNo != 2 || versions[1].VersionNo != 1 {
		t.Fatalf("versions = %#v err=%v", versions, err)
	}
}

func TestRestoreVersion(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	entry, err := service.store.CreateEntry(ctx, CreateEntryRequest{Title: "恢复", TagsJSON: "[]"})
	if err != nil {
		t.Fatal(err)
	}
	_, rev1, err := service.store.SaveDraft(ctx, entry.ID, SaveDraftRequest{ContentMD: "# 初版\n\n内容A", ExpectedDraftRev: 1})
	if err != nil {
		t.Fatal(err)
	}
	v1, err := service.store.PublishVersion(ctx, entry.ID, PublishRequest{ExpectedDraftRev: rev1})
	if err != nil {
		t.Fatal(err)
	}
	_, rev2, err := service.store.SaveDraft(ctx, entry.ID, SaveDraftRequest{ContentMD: "# 新版\n\n内容B", ExpectedDraftRev: rev1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.PublishVersion(ctx, entry.ID, PublishRequest{ExpectedDraftRev: rev2}); err != nil {
		t.Fatal(err)
	}

	if err := service.store.RestoreVersion(ctx, entry.ID, v1.ID); err != nil {
		t.Fatal(err)
	}
	draft, err := service.store.GetDraft(ctx, entry.ID)
	if err != nil || draft.ContentMD != v1.ContentMD || draft.ContentText != v1.ContentText {
		t.Fatalf("restored draft = %#v err=%v", draft, err)
	}

	detail, err := service.store.GetEntry(ctx, entry.ID)
	if err != nil || detail.CurrentDraftRev != rev2+1 {
		t.Fatalf("draft rev after restore = %d, want %d", detail.CurrentDraftRev, rev2+1)
	}

	if err := service.store.RestoreVersion(ctx, entry.ID, 999999); !errors.Is(err, errVersionNotFound) {
		t.Fatalf("restore missing version err = %v, want errVersionNotFound", err)
	}

	other, err := service.store.CreateEntry(ctx, CreateEntryRequest{Title: "其他条目", TagsJSON: "[]"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.RestoreVersion(ctx, other.ID, v1.ID); !errors.Is(err, errVersionNotBelong) {
		t.Fatalf("restore foreign version err = %v, want errVersionNotBelong", err)
	}
}

func TestGetVersionNonexistent(t *testing.T) {
	service := newTestService(t)
	if v, err := service.store.GetVersion(context.Background(), 123456); err == nil || v != nil {
		t.Fatalf("GetVersion(missing) = %#v err=%v, want error", v, err)
	}
}