package serveragent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicGlobeNodesHideIdentityAndCoarsenCoordinates(t *testing.T) {
	service, db := testService(t)

	_, err := db.ExecContext(context.Background(), `
		INSERT INTO server_accounts (id, name, host, username, auth_type, cached_info)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "node-1", "家里的笔电", "192.0.2.10", "root", "password", `{"lat":30.58333,"lon":114.26667}`)
	if err != nil {
		t.Fatalf("seed server account: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/server/public/globe-nodes", nil)
	rec := httptest.NewRecorder()
	service.handlePublicGlobeNodes(rec, req, db)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Nodes []map[string]interface{} `json:"nodes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Success || len(payload.Data.Nodes) != 1 {
		t.Fatalf("unexpected payload: %s", rec.Body.String())
	}

	node := payload.Data.Nodes[0]
	if _, ok := node["name"]; ok {
		t.Fatalf("public globe node must not expose internal name: %#v", node)
	}
	if node["latitude"] != 30.6 || node["longitude"] != 114.3 {
		t.Fatalf("coordinates not coarsened: %#v", node)
	}
}
