package filebox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

type fakeAuth struct {
	ok bool
}

func (f fakeAuth) IsAuthenticated(context.Context, *http.Request) (bool, error) {
	return f.ok, nil
}

func TestTextShareRetrieveVerifyDownloadAndExpire(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})

	body, contentType := multipartBody(t, map[string]string{
		"type":               "text",
		"text":               "hello filebox",
		"expiry":             "1",
		"max_downloads":      "1",
		"access_password":    "secret",
		"burn_after_reading": "false",
	}, nil)
	res := performFileboxRequest(service, http.MethodPost, "/api/filebox/share", body, contentType)
	if res.Code != http.StatusOK {
		t.Fatalf("create text status = %d body=%s", res.Code, res.Body.String())
	}
	var createPayload struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Data    struct {
			RequiresPassword bool   `json:"requiresPassword"`
			Preview          string `json:"preview"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &createPayload)
	if !createPayload.Success || createPayload.Code == "" || !createPayload.Data.RequiresPassword || createPayload.Data.Preview != "" {
		t.Fatalf("unexpected create payload: %#v", createPayload)
	}

	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/retrieve/"+createPayload.Code, nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("retrieve status = %d body=%s", res.Code, res.Body.String())
	}
	var metadata struct {
		Success bool `json:"success"`
		Data    struct {
			Code             string `json:"code"`
			RequiresPassword bool   `json:"requiresPassword"`
			Preview          string `json:"preview"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &metadata)
	if !metadata.Success || metadata.Data.Code != createPayload.Code || !metadata.Data.RequiresPassword || metadata.Data.Preview != "" {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}

	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/public/"+createPayload.Code+"/verify", strings.NewReader(`{"password":"bad"}`), "application/json")
	if res.Code != http.StatusForbidden {
		t.Fatalf("bad verify status = %d body=%s", res.Code, res.Body.String())
	}
	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/public/"+createPayload.Code+"/verify", strings.NewReader(`{"password":"secret"}`), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("good verify status = %d body=%s", res.Code, res.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/filebox/download/"+createPayload.Code, nil)
	req.Header.Set("X-Filebox-Password", "secret")
	res = httptest.NewRecorder()
	service.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != "hello filebox" {
		t.Fatalf("download status=%d body=%q", res.Code, res.Body.String())
	}

	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/retrieve/"+createPayload.Code, nil, "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("retrieve after max downloads status = %d body=%s", res.Code, res.Body.String())
	}

	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/access-logs", nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("access logs status = %d body=%s", res.Code, res.Body.String())
	}
	var logs struct {
		Success bool        `json:"success"`
		Data    []AccessLog `json:"data"`
	}
	mustDecodeFilebox(t, res, &logs)
	if !logs.Success || len(logs.Data) < 2 {
		t.Fatalf("expected access logs, got %#v", logs)
	}
}

func TestMarkdownTextSharePreservesFormatAndDownloadMetadata(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})
	markdown := "# Release notes\n\n- [x] GFM task\n\n| Name | Value |\n| --- | --- |\n| API | Ready |"
	body, contentType := multipartBody(t, map[string]string{
		"type":   "text",
		"text":   markdown,
		"expiry": "1",
	}, nil)
	res := performFileboxRequest(service, http.MethodPost, "/api/filebox/share", body, contentType)
	if res.Code != http.StatusOK {
		t.Fatalf("create markdown status = %d body=%s", res.Code, res.Body.String())
	}
	var created struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Data    struct {
			Filename   string `json:"filename"`
			MIMEType   string `json:"mimetype"`
			TextFormat string `json:"textFormat"`
			Preview    string `json:"preview"`
			Size       int64  `json:"size"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &created)
	if !created.Success || created.Code == "" || created.Data.TextFormat != textFormatMarkdown || !strings.HasSuffix(created.Data.Filename, ".md") || created.Data.MIMEType != "text/markdown; charset=utf-8" || created.Data.Preview == "" || created.Data.Size != int64(len([]byte(markdown))) {
		t.Fatalf("unexpected markdown payload: %#v", created)
	}

	entry, err := service.GetEntry(context.Background(), created.Code, false)
	if err != nil || entry == nil {
		t.Fatalf("load markdown entry: entry=%#v err=%v", entry, err)
	}
	if entry.Metadata["textFormat"] != textFormatMarkdown {
		t.Fatalf("markdown metadata = %#v", entry.Metadata)
	}

	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/public/"+created.Code, nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("markdown metadata status = %d body=%s", res.Code, res.Body.String())
	}
	var metadata struct {
		Data struct {
			TextFormat string `json:"textFormat"`
			Filename   string `json:"filename"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &metadata)
	if metadata.Data.TextFormat != textFormatMarkdown || !strings.HasSuffix(metadata.Data.Filename, ".md") {
		t.Fatalf("unexpected public markdown metadata: %#v", metadata)
	}

	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/public/"+created.Code+"/download", nil, "")
	if res.Code != http.StatusOK || res.Body.String() != markdown {
		t.Fatalf("markdown download status=%d body=%q", res.Code, res.Body.String())
	}

	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/d/"+created.Code, nil, "")
	if res.Code != http.StatusOK || res.Body.String() != markdown {
		t.Fatalf("markdown short direct link status=%d body=%q", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Fatalf("markdown short direct link content type = %q", got)
	}
}

func TestSettingsRequireAuthAndCleanupExpired(t *testing.T) {
	unauthenticated := newTestService(t, fakeAuth{ok: false})
	res := performFileboxRequest(unauthenticated, http.MethodGet, "/api/filebox/settings", nil, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated settings status = %d body=%s", res.Code, res.Body.String())
	}

	service := newTestService(t, fakeAuth{ok: true})
	res = performFileboxRequest(service, http.MethodPut, "/api/filebox/settings", strings.NewReader(`{"max_file_size":1024,"allowed_mime_types":["text/*"],"default_expiry_hours":2,"public_upload_enabled":true}`), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("update settings status = %d body=%s", res.Code, res.Body.String())
	}
	var settingsPayload struct {
		Success bool     `json:"success"`
		Data    Settings `json:"data"`
	}
	mustDecodeFilebox(t, res, &settingsPayload)
	if !settingsPayload.Success || settingsPayload.Data.MaxFileSize != 1024 || !settingsPayload.Data.PublicUploadEnabled || len(settingsPayload.Data.AllowedMIMETypes) != 1 {
		t.Fatalf("unexpected settings payload: %#v", settingsPayload)
	}

	entry, err := service.AddText(context.Background(), "expired", 1, false, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil || entry.Code == "" {
		t.Fatalf("expected expired entry, got %#v", entry)
	}
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `UPDATE filebox_entries SET expiry = ? WHERE code = ?`, time.Now().Add(-time.Hour).UnixMilli(), entry.Code)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	permanent, err := service.AddText(context.Background(), "permanent", 0, false, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if permanent == nil || permanent.Expiry != 0 {
		t.Fatalf("expected permanent entry, got %#v", permanent)
	}
	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/jobs/cleanup", strings.NewReader(`{}`), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("cleanup status = %d body=%s", res.Code, res.Body.String())
	}
	var cleanup struct {
		Success bool `json:"success"`
		Data    struct {
			Deleted int `json:"deleted"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &cleanup)
	if !cleanup.Success || cleanup.Data.Deleted != 1 {
		t.Fatalf("unexpected cleanup payload: %#v", cleanup)
	}
	if found, err := service.GetEntry(context.Background(), permanent.Code, false); err != nil || found == nil {
		t.Fatalf("expected permanent entry to survive cleanup, found=%#v err=%v", found, err)
	}
}

func TestVoidRoomSignalsRequireTokensAndDoNotTouchFileboxStorage(t *testing.T) {
	unauthenticated := newTestService(t, fakeAuth{ok: false})
	res := performFileboxRequest(unauthenticated, http.MethodPost, "/api/filebox/void/rooms", strings.NewReader(`{}`), "application/json")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create room status = %d body=%s", res.Code, res.Body.String())
	}

	service := newTestService(t, fakeAuth{ok: true})
	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/void/rooms", strings.NewReader(`{}`), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("create room status = %d body=%s", res.Code, res.Body.String())
	}
	var createPayload struct {
		Success bool `json:"success"`
		Data    struct {
			RoomID             string `json:"roomId"`
			OwnerToken         string `json:"ownerToken"`
			OwnerParticipantID string `json:"ownerParticipantId"`
			Mode               string `json:"mode"`
			ExpiresAt          int64  `json:"expiresAt"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &createPayload)
	if !createPayload.Success || createPayload.Data.RoomID == "" || createPayload.Data.OwnerToken == "" || createPayload.Data.OwnerParticipantID != "owner" || createPayload.Data.Mode != voidRoomModeTemporary || createPayload.Data.ExpiresAt == 0 {
		t.Fatalf("unexpected create room payload: %#v", createPayload)
	}

	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/void/rooms/"+createPayload.Data.RoomID, nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("get room status = %d body=%s", res.Code, res.Body.String())
	}
	var roomPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Participants []publicVoidParticipant `json:"participants"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &roomPayload)
	if len(roomPayload.Data.Participants) != 1 || roomPayload.Data.Participants[0].Role != "owner" {
		t.Fatalf("expected owner first, got %#v", roomPayload.Data.Participants)
	}

	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/void/rooms/"+createPayload.Data.RoomID+"/participants", strings.NewReader(`{"name":"phone"}`), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("join room status = %d body=%s", res.Code, res.Body.String())
	}
	var joinPayload struct {
		Success bool `json:"success"`
		Data    struct {
			ParticipantID    string `json:"participantId"`
			ParticipantToken string `json:"participantToken"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &joinPayload)
	if !joinPayload.Success || joinPayload.Data.ParticipantID == "" || joinPayload.Data.ParticipantToken == "" {
		t.Fatalf("unexpected join payload: %#v", joinPayload)
	}
	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/void/rooms/"+createPayload.Data.RoomID, nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("get joined room status = %d body=%s", res.Code, res.Body.String())
	}
	mustDecodeFilebox(t, res, &roomPayload)
	if len(roomPayload.Data.Participants) < 2 || roomPayload.Data.Participants[0].Role != "owner" || roomPayload.Data.Participants[1].ID != joinPayload.Data.ParticipantID {
		t.Fatalf("unexpected participant order: %#v", roomPayload.Data.Participants)
	}

	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/void/rooms/"+createPayload.Data.RoomID+"/signals", strings.NewReader(`{"participantId":"`+joinPayload.Data.ParticipantID+`","type":"chat.text"}`), "application/json")
	if res.Code != http.StatusBadRequest {
		t.Fatalf("missing token signal status = %d body=%s", res.Code, res.Body.String())
	}

	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/void/rooms/"+createPayload.Data.RoomID+"/signals", strings.NewReader(`{"participantId":"`+joinPayload.Data.ParticipantID+`","participantToken":"bad","type":"participant.ready"}`), "application/json")
	if res.Code != http.StatusForbidden {
		t.Fatalf("bad token signal status = %d body=%s", res.Code, res.Body.String())
	}

	chatSignalBody := `{"participantId":"` + joinPayload.Data.ParticipantID + `","participantToken":"` + joinPayload.Data.ParticipantToken + `","to":"owner","type":"chat.text","payload":{"text":"hello"}}`
	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/void/rooms/"+createPayload.Data.RoomID+"/signals", strings.NewReader(chatSignalBody), "application/json")
	if res.Code != http.StatusBadRequest {
		t.Fatalf("payload signal status = %d body=%s", res.Code, res.Body.String())
	}

	signalBody := `{"participantId":"` + joinPayload.Data.ParticipantID + `","participantToken":"` + joinPayload.Data.ParticipantToken + `","to":"owner","type":"participant.ready","payload":{"name":"phone"}}`
	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/void/rooms/"+createPayload.Data.RoomID+"/signals", strings.NewReader(signalBody), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("post signal status = %d body=%s", res.Code, res.Body.String())
	}

	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/void/rooms/"+createPayload.Data.RoomID+"/signals?participantId=owner&participantToken="+createPayload.Data.OwnerToken+"&since=0", nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("get signals status = %d body=%s", res.Code, res.Body.String())
	}
	var signalsPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Signals []voidSignal `json:"signals"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &signalsPayload)
	if !signalsPayload.Success || len(signalsPayload.Data.Signals) < 2 {
		t.Fatalf("expected join and ready signals, got %#v", signalsPayload)
	}

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var entries int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM filebox_entries`).Scan(&entries); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()
	if entries != 0 {
		t.Fatalf("void room should not create filebox entries, got %d", entries)
	}
	files, err := os.ReadDir(service.uploadsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("void room should not write uploads, got %d files", len(files))
	}
}

func TestPersistentVoidRoomPersistsMetadataOnly(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})
	res := performFileboxRequest(service, http.MethodPost, "/api/filebox/void/rooms", strings.NewReader(`{"mode":"persistent"}`), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("create persistent room status = %d body=%s", res.Code, res.Body.String())
	}
	var createPayload struct {
		Success bool `json:"success"`
		Data    struct {
			RoomID     string `json:"roomId"`
			OwnerToken string `json:"ownerToken"`
			Mode       string `json:"mode"`
			ExpiresAt  int64  `json:"expiresAt"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &createPayload)
	if !createPayload.Success || createPayload.Data.RoomID == "" || createPayload.Data.OwnerToken == "" || createPayload.Data.Mode != voidRoomModePersistent || createPayload.Data.ExpiresAt != 0 {
		t.Fatalf("unexpected persistent room payload: %#v", createPayload)
	}

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var persistentRooms, entries int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM filebox_void_rooms WHERE deleted_at IS NULL`).Scan(&persistentRooms); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM filebox_entries`).Scan(&entries); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()
	if persistentRooms != 1 || entries != 0 {
		t.Fatalf("expected one persistent room and no filebox entries, rooms=%d entries=%d", persistentRooms, entries)
	}
	files, err := os.ReadDir(service.uploadsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("persistent void room should not write uploads, got %d files", len(files))
	}

	restarted := New(service.cfg, fakeAuth{ok: true})
	res = performFileboxRequest(restarted, http.MethodGet, "/api/filebox/void/rooms/"+createPayload.Data.RoomID, nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("get persisted room status = %d body=%s", res.Code, res.Body.String())
	}
	var roomPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Mode       string `json:"mode"`
			Persistent bool   `json:"persistent"`
			ExpiresAt  int64  `json:"expiresAt"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &roomPayload)
	if !roomPayload.Success || roomPayload.Data.Mode != voidRoomModePersistent || !roomPayload.Data.Persistent || roomPayload.Data.ExpiresAt != 0 {
		t.Fatalf("unexpected persisted room: %#v", roomPayload)
	}

	res = performFileboxRequest(restarted, http.MethodGet, "/api/filebox/void/rooms", nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("list rooms status = %d body=%s", res.Code, res.Body.String())
	}
	var listPayload struct {
		Success bool `json:"success"`
		Data    []struct {
			RoomID     string `json:"roomId"`
			OwnerToken string `json:"ownerToken"`
			Mode       string `json:"mode"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &listPayload)
	if !listPayload.Success || len(listPayload.Data) != 1 || listPayload.Data[0].RoomID != createPayload.Data.RoomID || listPayload.Data[0].OwnerToken == "" || listPayload.Data[0].Mode != voidRoomModePersistent {
		t.Fatalf("unexpected room list: %#v", listPayload)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/filebox/void/rooms/"+createPayload.Data.RoomID, nil)
	req.Header.Set("X-Void-Owner-Token", createPayload.Data.OwnerToken)
	res = httptest.NewRecorder()
	restarted.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("delete persistent room status = %d body=%s", res.Code, res.Body.String())
	}
	afterDelete := New(service.cfg, fakeAuth{ok: true})
	res = performFileboxRequest(afterDelete, http.MethodGet, "/api/filebox/void/rooms/"+createPayload.Data.RoomID, nil, "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("deleted persistent room status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestVoidRoomMergesRepeatedClientDevice(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})
	res := performFileboxRequest(service, http.MethodPost, "/api/filebox/void/rooms", strings.NewReader(`{}`), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("create room status = %d body=%s", res.Code, res.Body.String())
	}
	var createPayload struct {
		Data struct {
			RoomID string `json:"roomId"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &createPayload)

	joinBody := `{"name":"phone","clientId":"phone-client-1"}`
	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/void/rooms/"+createPayload.Data.RoomID+"/participants", strings.NewReader(joinBody), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("first join status = %d body=%s", res.Code, res.Body.String())
	}
	var firstJoin struct {
		Data struct {
			ParticipantID string `json:"participantId"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &firstJoin)

	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/void/rooms/"+createPayload.Data.RoomID+"/participants", strings.NewReader(joinBody), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("second join status = %d body=%s", res.Code, res.Body.String())
	}
	var secondJoin struct {
		Data struct {
			ParticipantID string `json:"participantId"`
			Room          struct {
				Participants []publicVoidParticipant `json:"participants"`
			} `json:"room"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &secondJoin)
	if secondJoin.Data.ParticipantID != firstJoin.Data.ParticipantID {
		t.Fatalf("expected same participant id, first=%s second=%s", firstJoin.Data.ParticipantID, secondJoin.Data.ParticipantID)
	}
	if len(secondJoin.Data.Room.Participants) != 2 {
		t.Fatalf("expected owner plus one phone, got %#v", secondJoin.Data.Room.Participants)
	}
}

func TestVoidRoomExpiryAndNetworkCandidates(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})
	res := performFileboxRequest(service, http.MethodPost, "/api/filebox/void/rooms", strings.NewReader(`{}`), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("create room status = %d body=%s", res.Code, res.Body.String())
	}
	var createPayload struct {
		Data struct {
			RoomID string `json:"roomId"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &createPayload)
	service.voidMu.Lock()
	service.voidRooms[createPayload.Data.RoomID].ExpiresAt = time.Now().Add(-time.Minute).UnixMilli()
	service.voidMu.Unlock()

	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/void/rooms/"+createPayload.Data.RoomID, nil, "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("expired room status = %d body=%s", res.Code, res.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/filebox/void/network-candidates", nil)
	req.Host = "localhost:3000"
	res = httptest.NewRecorder()
	service.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("network candidates status = %d body=%s", res.Code, res.Body.String())
	}
	var candidatesPayload struct {
		Success bool `json:"success"`
		Data    struct {
			CurrentOrigin string                 `json:"currentOrigin"`
			Candidates    []voidNetworkCandidate `json:"candidates"`
			Warnings      []string               `json:"warnings"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &candidatesPayload)
	if !candidatesPayload.Success || candidatesPayload.Data.CurrentOrigin != "http://localhost:3000" || len(candidatesPayload.Data.Candidates) == 0 || len(candidatesPayload.Data.Warnings) == 0 {
		t.Fatalf("unexpected network candidates payload: %#v", candidatesPayload)
	}
}

func TestRandomCodeUsesRejectionSamplingWithoutBias(t *testing.T) {
	for _, length := range []int{1, 8, defaultCodeLength} {
		code, err := randomCode(length)
		if err != nil {
			t.Fatalf("randomCode(%d): %v", length, err)
		}
		if len(code) != length {
			t.Fatalf("randomCode(%d) len = %d", length, len(code))
		}
		for i := 0; i < len(code); i++ {
			if !strings.ContainsRune(codeAlphabet, rune(code[i])) {
				t.Fatalf("randomCode(%d) produced invalid char %q in %q", length, code[i], code)
			}
		}
	}

	// 统计分布：每个字符出现次数不应偏离均匀期望过远，
	// 若直接取模（无拒绝采样）会系统性偏向字母表前段。
	const samples = 4000
	counts := make(map[byte]int)
	for i := 0; i < samples; i++ {
		code, err := randomCode(2)
		if err != nil {
			t.Fatal(err)
		}
		for j := 0; j < len(code); j++ {
			counts[code[j]]++
		}
	}
	total := samples * 2
	expect := float64(total) / float64(len(codeAlphabet))
	for index := 0; index < len(codeAlphabet); index++ {
		char := codeAlphabet[index]
		got := float64(counts[char])
		if got < expect*0.7 || got > expect*1.3 {
			t.Fatalf("char %q count %.0f deviates from expectation %.0f beyond ±30%%", char, got, expect)
		}
	}
}

func TestGenerateCodeDefaultsToLongerLength(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})
	code, err := service.GenerateCode(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != defaultCodeLength {
		t.Fatalf("default code length = %d, want %d", len(code), defaultCodeLength)
	}
}

func TestAccessEntryCountsDownloadsAtomicallyUntilMaxReached(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})
	ctx := context.Background()

	entry, err := service.AddText(ctx, "limited", 0, false, 2, "")
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 2; i++ {
		if err := service.AccessEntry(ctx, entry.Code, requestMeta{}); err != nil {
			t.Fatalf("access #%d: %v", i, err)
		}
		found, err := service.GetEntry(ctx, entry.Code, true)
		if err != nil || found == nil {
			t.Fatalf("entry should survive download #%d: found=%#v err=%v", i, found, err)
		}
		if found.Downloads != int64(i) {
			t.Fatalf("download count after access #%d = %d, want %d", i, found.Downloads, i)
		}
	}

	// 达到 max_downloads 后再访问不得继续计数，条目应被清理
	if err := service.AccessEntry(ctx, entry.Code, requestMeta{}); err != nil {
		t.Fatalf("over-limit access: %v", err)
	}
	found, err := service.GetEntry(ctx, entry.Code, true)
	if err != nil || found != nil {
		t.Fatalf("entry should be gone after exceeding max downloads, found=%#v err=%v", found, err)
	}
}

func TestAccessEntryBurnAfterReadingDeletesExactlyOnce(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})
	ctx := context.Background()

	entry, err := service.AddText(ctx, "burn me", 0, true, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AccessEntry(ctx, entry.Code, requestMeta{}); err != nil {
		t.Fatalf("burn access: %v", err)
	}
	found, err := service.GetEntry(ctx, entry.Code, true)
	if err != nil || found != nil {
		t.Fatalf("burned entry should be deleted, found=%#v err=%v", found, err)
	}
	var logs []AccessLog
	db, openErr := service.open(ctx)
	if openErr != nil {
		t.Fatal(openErr)
	}
	rows, qerr := db.QueryContext(ctx, `SELECT action FROM filebox_access_logs WHERE code = ?`, entry.Code)
	if qerr != nil {
		_ = db.Close()
		t.Fatal(qerr)
	}
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			rows.Close()
			_ = db.Close()
			t.Fatal(err)
		}
		logs = append(logs, AccessLog{Action: action})
	}
	rows.Close()
	if cerr := db.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	downloads, deletes := 0, 0
	for _, item := range logs {
		switch item.Action {
		case "download":
			downloads++
		case "delete":
			deletes++
		}
	}
	if downloads != 1 || deletes != 1 {
		t.Fatalf("expected exactly one download log and one delete log, got %d/%d", downloads, deletes)
	}
}

func newTestService(t *testing.T, auth Authenticator) *Service {
	t.Helper()
	return New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	}, auth)
}

func performFileboxRequest(service *Service, method, path string, body ioReader, contentType string) *httptest.ResponseRecorder {
	var reader ioReader = body
	if reader == nil {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

type ioReader interface {
	Read([]byte) (int, error)
}

func multipartBody(t *testing.T, fields map[string]string, file *multipartFile) (*bytes.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if file != nil {
		part, err := writer.CreateFormFile("file", file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(file.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes()), writer.FormDataContentType()
}

type multipartFile struct {
	name    string
	content string
}

func mustDecodeFilebox(t *testing.T, res *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), target); err != nil {
		t.Fatalf("decode %q: %v", res.Body.String(), err)
	}
}

type fakeNodeProvider struct {
	nodes []StorageNodeInfo
	keys  map[string]string
	err   error
}

func (f *fakeNodeProvider) ListEligibleStorageNodes(ctx context.Context) ([]StorageNodeInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.nodes, nil
}

func (f *fakeNodeProvider) GetEligibleStorageNode(ctx context.Context, serverID string) (*StorageNodeInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, n := range f.nodes {
		if n.ID == serverID {
			return &n, nil
		}
	}
	return nil, os.ErrNotExist
}

func (f *fakeNodeProvider) GetStorageNodeAgentKey(ctx context.Context, serverID string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if k, ok := f.keys[serverID]; ok {
		return k, nil
	}
	return "test-agent-key-secret", nil
}

func TestStorageNodesEndpoint(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})
	provider := &fakeNodeProvider{
		nodes: []StorageNodeInfo{
			{ID: "node-1", Name: "Node One", Host: "1.2.3.4", StoragePort: 61208, Platform: "linux", Online: true},
		},
		keys: map[string]string{"node-1": "key123"},
	}
	service.SetNodeProvider(provider)

	res := performFileboxRequest(service, http.MethodGet, "/api/filebox/storage-nodes", nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var resp struct {
		Success bool              `json:"success"`
		Data    []StorageNodeInfo `json:"data"`
	}
	mustDecodeFilebox(t, res, &resp)
	if !resp.Success || len(resp.Data) != 1 || resp.Data[0].ID != "node-1" {
		t.Fatalf("unexpected nodes: %#v", resp)
	}
}

func TestRemoteUploadFlowAndRedirect(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})
	provider := &fakeNodeProvider{
		nodes: []StorageNodeInfo{
			{ID: "node-1", Name: "Node One", Host: "1.2.3.4", StoragePort: 61208, Platform: "linux", Online: true},
		},
		keys: map[string]string{"node-1": "secret-key-123"},
	}
	service.SetNodeProvider(provider)

	// 1. Init remote upload
	initBody := `{"serverId":"node-1","filename":"backup.tar.gz","size":1048576}`
	res := performFileboxRequest(service, http.MethodPost, "/api/filebox/shares/init-upload", strings.NewReader(initBody), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var initResp struct {
		Success bool `json:"success"`
		Data    struct {
			Code      string `json:"code"`
			Filename  string `json:"filename"`
			UploadURL string `json:"uploadUrl"`
			Expires   int64  `json:"expires"`
		} `json:"data"`
	}
	mustDecodeFilebox(t, res, &initResp)
	if !initResp.Success || initResp.Data.Code == "" || !strings.Contains(initResp.Data.UploadURL, "1.2.3.4:61208") {
		t.Fatalf("unexpected init response: %#v", initResp)
	}

	// 2. Complete remote upload
	completeBody := fmt.Sprintf(`{
		"code": %q,
		"filename": "backup.tar.gz",
		"size": 1048576,
		"serverId": "node-1",
		"mimeType": "application/gzip",
		"expiry": "24",
		"max_downloads": "2"
	}`, initResp.Data.Code)
	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/shares/complete-upload", strings.NewReader(completeBody), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var completeResp struct {
		Success bool        `json:"success"`
		Data    PublicEntry `json:"data"`
	}
	mustDecodeFilebox(t, res, &completeResp)
	if completeResp.Data.StorageType != "remote" || completeResp.Data.ServerID == nil || *completeResp.Data.ServerID != "node-1" {
		t.Fatalf("unexpected complete entry: %#v", completeResp.Data)
	}

	// 3. Download remote entry -> should 302 redirect to signed URL
	res = performFileboxRequest(service, http.MethodGet, "/api/filebox/download/"+initResp.Data.Code, nil, "")
	if res.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", res.Code, res.Body.String())
	}
	location := res.Header().Get("Location")
	if !strings.Contains(location, "1.2.3.4:61208/storage/"+initResp.Data.Code+"/backup.tar.gz") {
		t.Fatalf("unexpected redirect location: %s", location)
	}

	// Check download count incremented
	entry, err := service.GetEntry(context.Background(), initResp.Data.Code, true)
	if err != nil || entry == nil {
		t.Fatalf("entry not found: %v", err)
	}
	if entry.Downloads != 1 {
		t.Fatalf("expected downloads=1, got %d", entry.Downloads)
	}

	// 4. Test HandleShareRedirect
	shareReq := httptest.NewRequest(http.MethodGet, "/share/"+initResp.Data.Code, nil)
	// 用不同 IP 模拟第二个独立逻辑下载，避免被同一 IP 的去重窗口合并
	shareReq.RemoteAddr = "10.0.0.99:5000"
	shareRec := httptest.NewRecorder()
	handled := service.HandleShareRedirect(shareRec, shareReq, initResp.Data.Code)
	if !handled {
		t.Fatalf("expected HandleShareRedirect to handle request")
	}
	if shareRec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", shareRec.Code)
	}

	// Now downloads reached max (2), entry should be expired/deleted
	entryAfter, _ := service.GetEntry(context.Background(), initResp.Data.Code, false)
	if entryAfter != nil {
		t.Fatalf("expected entry to be cleaned up after reaching max downloads")
	}
}

func TestRemoteDownloadNodeOfflineReturns503(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})
	provider := &fakeNodeProvider{
		nodes: []StorageNodeInfo{}, // empty, node offline
		keys:  map[string]string{},
	}
	service.SetNodeProvider(provider)

	entry, err := service.AddRemoteFile(context.Background(), "testoffline", "test.bin", 100, "application/octet-stream", "offline-node", 1, false, 10, "")
	if err != nil {
		t.Fatalf("create remote file: %v", err)
	}
	if entry == nil {
		t.Fatalf("entry is nil")
	}

	res := performFileboxRequest(service, http.MethodGet, "/api/filebox/download/"+entry.Code, nil, "")
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d: %s", res.Code, res.Body.String())
	}
}

func TestTransferStorageLocalToRemoteAndBack(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})

	// Setup fake HTTP server simulating remote agent storage node
	remoteStore := make(map[string][]byte)
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			remoteStore[r.URL.Path] = data
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			data, ok := remoteStore[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
		case http.MethodDelete:
			delete(remoteStore, r.URL.Path)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer remoteServer.Close()

	parts := strings.Split(strings.TrimPrefix(remoteServer.URL, "http://"), ":")
	nodeHost := parts[0]
	nodePort, _ := strconv.Atoi(parts[1])

	provider := &fakeNodeProvider{
		nodes: []StorageNodeInfo{
			{ID: "node-agent-1", Name: "Remote Node", Host: nodeHost, StoragePort: nodePort, Platform: "linux", Online: true},
		},
		keys: map[string]string{"node-agent-1": "test-key"},
	}
	service.SetNodeProvider(provider)

	// 1. Create local file share
	fileData := "hello file content for transfer test"
	body, contentType := multipartBody(t, map[string]string{
		"type":   "file",
		"expiry": "24",
	}, &multipartFile{name: "transfer_test.txt", content: fileData})
	res := performFileboxRequest(service, http.MethodPost, "/api/filebox/share", body, contentType)
	if res.Code != http.StatusOK {
		t.Fatalf("create file status = %d: %s", res.Code, res.Body.String())
	}
	var createResp struct {
		Success bool        `json:"success"`
		Code    string      `json:"code"`
		Data    PublicEntry `json:"data"`
	}
	mustDecodeFilebox(t, res, &createResp)
	if createResp.Data.StorageType != "local" {
		t.Fatalf("expected initial local storage, got %s", createResp.Data.StorageType)
	}

	// 2. Transfer from local to remote
	transferBody := `{"targetStorageType":"remote","targetServerId":"node-agent-1"}`
	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/shares/"+createResp.Code+"/transfer", strings.NewReader(transferBody), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("transfer to remote failed status=%d: %s", res.Code, res.Body.String())
	}
	var transferredResp struct {
		Success bool        `json:"success"`
		Data    PublicEntry `json:"data"`
	}
	mustDecodeFilebox(t, res, &transferredResp)
	if transferredResp.Data.StorageType != "remote" || transferredResp.Data.ServerID == nil || *transferredResp.Data.ServerID != "node-agent-1" {
		t.Fatalf("expected remote storage after transfer, got %#v", transferredResp.Data)
	}

	// Verify remote server got the file data
	foundOnRemote := false
	for path, data := range remoteStore {
		if strings.Contains(path, createResp.Code) && string(data) == fileData {
			foundOnRemote = true
			break
		}
	}
	if !foundOnRemote {
		t.Fatalf("file not found on remote storage after transfer")
	}

	// 3. Transfer back from remote to local
	transferLocalBody := `{"targetStorageType":"local"}`
	res = performFileboxRequest(service, http.MethodPost, "/api/filebox/shares/"+createResp.Code+"/transfer", strings.NewReader(transferLocalBody), "application/json")
	if res.Code != http.StatusOK {
		t.Fatalf("transfer back to local failed status=%d: %s", res.Code, res.Body.String())
	}
	mustDecodeFilebox(t, res, &transferredResp)
	if transferredResp.Data.StorageType != "local" {
		t.Fatalf("expected local storage after transferring back, got %s", transferredResp.Data.StorageType)
	}

	// Verify local content is readable and intact
	entry, err := service.GetEntry(context.Background(), createResp.Code, true)
	if err != nil || entry == nil || entry.Path == nil {
		t.Fatalf("entry not found or path nil: %v", err)
	}
	content, err := os.ReadFile(*entry.Path)
	if err != nil || string(content) != fileData {
		t.Fatalf("local file content mismatch after transfer back: %s vs %s", string(content), fileData)
	}
}

func downloadWithIP(service *Service, code, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/filebox/download/"+code, nil)
	req.RemoteAddr = ip
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

func TestDownloadDedupSameIPWithinWindow(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})
	entry, err := service.AddText(context.Background(), "dedup test", 24, false, 0, "")
	if err != nil {
		t.Fatalf("create text share: %v", err)
	}

	// 同一 IP 的并发/重复请求只计一次（模拟多线程 Range 分块）
	for i := 0; i < 4; i++ {
		res := downloadWithIP(service, entry.Code, "192.0.2.10:5555")
		if res.Code != http.StatusOK {
			t.Fatalf("download attempt %d expected 200, got %d", i, res.Code)
		}
	}
	after, err := service.GetEntry(context.Background(), entry.Code, true)
	if err != nil || after == nil {
		t.Fatalf("entry not found: %v", err)
	}
	if after.Downloads != 1 {
		t.Fatalf("expected downloads=1 after 4 same-IP requests, got %d", after.Downloads)
	}
}

func TestDownloadDedupDifferentIPCountsEach(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})
	entry, err := service.AddText(context.Background(), "dedup ip", 24, false, 0, "")
	if err != nil {
		t.Fatalf("create text share: %v", err)
	}

	for _, ip := range []string{"192.0.2.10:1", "192.0.2.20:2", "192.0.2.30:3"} {
		res := downloadWithIP(service, entry.Code, ip)
		if res.Code != http.StatusOK {
			t.Fatalf("expected 200 for ip %s, got %d", ip, res.Code)
		}
	}
	after, err := service.GetEntry(context.Background(), entry.Code, true)
	if err != nil || after == nil {
		t.Fatalf("entry not found: %v", err)
	}
	if after.Downloads != 3 {
		t.Fatalf("expected downloads=3 for 3 distinct IPs, got %d", after.Downloads)
	}
}

func TestDownloadDedupExpiredByWindow(t *testing.T) {
	service := newTestService(t, fakeAuth{ok: true})
	entry, err := service.AddText(context.Background(), "dedup window", 24, false, 0, "")
	if err != nil {
		t.Fatalf("create text share: %v", err)
	}

	// 首个请求计数
	if res := downloadWithIP(service, entry.Code, "192.0.2.10:1"); res.Code != http.StatusOK {
		t.Fatalf("first download expected 200, got %d", res.Code)
	}
	// 手动把去重时间戳拨回窗口之前，模拟窗口过期后的另一次独立下载
	// 注：claimDownloadSlot 的 key 用的是剥离端口后的纯 IP
	service.dedupMu.Lock()
	key := "192.0.2.10|" + entry.Code
	service.downloadDedup[key] = time.Now().Add(-(downloadDedupWindow + time.Second)).UnixMilli()
	service.dedupMu.Unlock()

	if res := downloadWithIP(service, entry.Code, "192.0.2.10:1"); res.Code != http.StatusOK {
		t.Fatalf("download after window expected 200, got %d", res.Code)
	}
	after, err := service.GetEntry(context.Background(), entry.Code, true)
	if err != nil || after == nil {
		t.Fatalf("entry not found: %v", err)
	}
	if after.Downloads != 2 {
		t.Fatalf("expected downloads=2 across window boundary, got %d", after.Downloads)
	}
}


