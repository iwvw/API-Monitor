package vercel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

func TestParseVercelSyncOptionsFallsBackToSavedConfig(t *testing.T) {
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")

	opts, err := parseVercelSyncOptions(map[string]any{
		"vercel_token": "__USE_PRECONFIG__",
	}, config.VercelConfig{
		Token:     " saved-token ",
		ProjectID: " saved-project ",
		TeamID:    " saved-team ",
	})
	if err != nil {
		t.Fatalf("parse options error: %v", err)
	}
	if opts.VercelToken != "saved-token" || opts.ProjectID != "saved-project" || opts.TeamID != "saved-team" {
		t.Fatalf("unexpected options: %#v", opts)
	}
	if !opts.UsePreconfig {
		t.Fatal("expected preconfig mode")
	}
}

func TestSaveLocalVercelCredentialsStoresExplicitInput(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"]}`)
	store := config.LoadStore()
	h := &Handler{Store: store}

	saved, err := h.saveLocalVercelCredentials(vercelSyncOptions{
		VercelToken: " token ",
		ProjectID:   " project ",
		TeamID:      " team ",
		SaveCreds:   true,
	})
	if err != nil {
		t.Fatalf("save local credentials error: %v", err)
	}
	if !saved {
		t.Fatal("expected credentials to be saved")
	}
	got := store.Snapshot().Vercel
	if got.Token != "token" || got.ProjectID != "project" || got.TeamID != "team" {
		t.Fatalf("unexpected saved credentials: %#v", got)
	}
}

func TestSaveLocalVercelCredentialsPreservesPreconfiguredTokenAndUpdatesProject(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"vercel":{"token":"saved-token","project_id":"old-project","team_id":"old-team"}}`)
	store := config.LoadStore()
	h := &Handler{Store: store}

	saved, err := h.saveLocalVercelCredentials(vercelSyncOptions{
		VercelToken:  "resolved-token",
		ProjectID:    "new-project",
		TeamID:       "new-team",
		SaveCreds:    true,
		UsePreconfig: true,
	})
	if err != nil {
		t.Fatalf("save local credentials error: %v", err)
	}
	if !saved {
		t.Fatal("expected project/team updates to be saved")
	}
	got := store.Snapshot().Vercel
	if got.Token != "saved-token" || got.ProjectID != "new-project" || got.TeamID != "new-team" {
		t.Fatalf("unexpected saved credentials: %#v", got)
	}
}

func TestExportSyncConfigStripsSavedVercelCredentials(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"vercel":{"token":"secret-token","project_id":"project","team_id":"team"}}`)
	store := config.LoadStore()
	h := &Handler{Store: store}

	jsonStr, _, err := h.exportSyncConfig(map[string]any{})
	if err != nil {
		t.Fatalf("export sync config error: %v", err)
	}
	if strings.Contains(jsonStr, "secret-token") || strings.Contains(jsonStr, `"vercel"`) {
		t.Fatalf("expected sync export to strip Vercel credentials, got %s", jsonStr)
	}
	var exported config.Config
	if err := json.Unmarshal([]byte(jsonStr), &exported); err != nil {
		t.Fatalf("exported config is invalid JSON: %v", err)
	}
	if len(exported.Keys) != 1 || exported.Keys[0] != "k1" {
		t.Fatalf("unexpected exported config: %#v", exported)
	}
}
