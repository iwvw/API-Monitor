package config

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// ─── GetModelConfig edge cases ───────────────────────────────────────

func TestGetModelConfigDeepSeekChat(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-v4-flash")
	if !ok {
		t.Fatal("expected ok for deepseek-v4-flash")
	}
	if !thinking || search {
		t.Fatalf("expected thinking=true search=false for deepseek-v4-flash, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigDeepSeekChatNoThinking(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-v4-flash-nothinking")
	if !ok {
		t.Fatal("expected ok for deepseek-v4-flash-nothinking")
	}
	if thinking || search {
		t.Fatalf("expected thinking=false search=false for deepseek-v4-flash-nothinking, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigDeepSeekReasoner(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-v4-pro")
	if !ok {
		t.Fatal("expected ok for deepseek-v4-pro")
	}
	if !thinking || search {
		t.Fatalf("expected thinking=true search=false, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigDeepSeekChatSearch(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-v4-flash-search")
	if !ok {
		t.Fatal("expected ok for deepseek-v4-flash-search")
	}
	if !thinking || !search {
		t.Fatalf("expected thinking=true search=true, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigDeepSeekExpertChat(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-v4-pro")
	if !ok {
		t.Fatal("expected ok for deepseek-v4-pro")
	}
	if !thinking || search {
		t.Fatalf("expected thinking=true search=false for deepseek-v4-pro, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigDeepSeekVision(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-v4-vision")
	if !ok {
		t.Fatal("expected ok for deepseek-v4-vision")
	}
	if !thinking || search {
		t.Fatalf("expected thinking=true search=false, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigDeepSeekVisionSearch(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-v4-vision-search")
	if !ok {
		t.Fatal("expected ok for deepseek-v4-vision-search")
	}
	if !thinking || !search {
		t.Fatalf("expected thinking=true search=true, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelTypeDefaultExpertAndVision(t *testing.T) {
	defaultType, ok := GetModelType("deepseek-v4-flash")
	if !ok || defaultType != "default" {
		t.Fatalf("expected default model_type, got ok=%v model_type=%q", ok, defaultType)
	}
	defaultNoThinkingType, ok := GetModelType("deepseek-v4-flash-nothinking")
	if !ok || defaultNoThinkingType != "default" {
		t.Fatalf("expected default model_type for nothinking, got ok=%v model_type=%q", ok, defaultNoThinkingType)
	}
	expertType, ok := GetModelType("deepseek-v4-pro")
	if !ok || expertType != "expert" {
		t.Fatalf("expected expert model_type, got ok=%v model_type=%q", ok, expertType)
	}
	visionType, ok := GetModelType("deepseek-v4-vision")
	if !ok || visionType != "vision" {
		t.Fatalf("expected vision model_type, got ok=%v model_type=%q", ok, visionType)
	}
}

func TestGetModelConfigCaseInsensitive(t *testing.T) {
	thinking, search, ok := GetModelConfig("DeepSeek-V4-Flash")
	if !ok {
		t.Fatal("expected ok for case-insensitive deepseek-v4-flash")
	}
	if !thinking || search {
		t.Fatalf("expected thinking=true search=false for case-insensitive deepseek-v4-flash")
	}
}

func TestGetModelConfigUnknownModel(t *testing.T) {
	_, _, ok := GetModelConfig("gpt-4")
	if ok {
		t.Fatal("expected not ok for unknown model")
	}
}

func TestGetModelConfigEmpty(t *testing.T) {
	_, _, ok := GetModelConfig("")
	if ok {
		t.Fatal("expected not ok for empty model")
	}
}

// ─── lower function ──────────────────────────────────────────────────

func TestLowerFunction(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello", "hello"},
		{"ALLCAPS", "allcaps"},
		{"already-lower", "already-lower"},
		{"Mixed-CASE-123", "mixed-case-123"},
		{"", ""},
	}
	for _, tc := range tests {
		got := lower(tc.input)
		if got != tc.expected {
			t.Errorf("lower(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// ─── Config.MarshalJSON / UnmarshalJSON roundtrip ────────────────────

func TestConfigJSONRoundtrip(t *testing.T) {
	cfg := Config{
		Keys:         []string{"key1", "key2"},
		Accounts:     []Account{{Email: "user@example.com", Password: "pass", Token: "tok"}},
		ModelAliases: map[string]string{"Claude-Sonnet-4-6": "DeepSeek-V4-Flash"},
		AutoDelete: AutoDeleteConfig{
			Mode: "single",
		},
		Runtime: RuntimeConfig{
			TokenRefreshIntervalHours: 12,
		},
		Vercel: VercelConfig{
			Token:     " vercel-token ",
			ProjectID: " prj_123 ",
			TeamID:    " team_123 ",
		},
		VercelSyncHash: "hash123",
		VercelSyncTime: 1234567890,
		AdditionalFields: map[string]any{
			"custom_field": "custom_value",
		},
	}

	data, err := cfg.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(decoded.Keys) != 2 || decoded.Keys[0] != "key1" {
		t.Fatalf("unexpected keys: %#v", decoded.Keys)
	}
	if len(decoded.Accounts) != 1 || decoded.Accounts[0].Email != "user@example.com" {
		t.Fatalf("unexpected accounts: %#v", decoded.Accounts)
	}
	if decoded.ModelAliases["claude-sonnet-4-6"] != "deepseek-v4-flash" {
		t.Fatalf("unexpected normalized model aliases: %#v", decoded.ModelAliases)
	}
	if decoded.Runtime.TokenRefreshIntervalHours != 12 {
		t.Fatalf("unexpected runtime refresh interval: %#v", decoded.Runtime.TokenRefreshIntervalHours)
	}
	if decoded.AutoDelete.Mode != "single" {
		t.Fatalf("unexpected auto delete mode: %#v", decoded.AutoDelete.Mode)
	}
	if decoded.Vercel.Token != "vercel-token" || decoded.Vercel.ProjectID != "prj_123" || decoded.Vercel.TeamID != "team_123" {
		t.Fatalf("unexpected vercel config: %#v", decoded.Vercel)
	}
	if decoded.VercelSyncHash != "hash123" {
		t.Fatalf("unexpected vercel sync hash: %q", decoded.VercelSyncHash)
	}
	if decoded.AdditionalFields["custom_field"] != "custom_value" {
		t.Fatalf("unexpected additional fields: %#v", decoded.AdditionalFields)
	}
}

func TestAutoDeleteModeResolution(t *testing.T) {
	tests := []struct {
		name string
		cfg  AutoDeleteConfig
		want string
	}{
		{name: "default", cfg: AutoDeleteConfig{}, want: "none"},
		{name: "legacy all", cfg: AutoDeleteConfig{Sessions: true}, want: "all"},
		{name: "single", cfg: AutoDeleteConfig{Mode: "single"}, want: "single"},
		{name: "all", cfg: AutoDeleteConfig{Mode: "all"}, want: "all"},
		{name: "none", cfg: AutoDeleteConfig{Mode: "none"}, want: "none"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &Store{cfg: Config{AutoDelete: tc.cfg}}
			if got := store.AutoDeleteMode(); got != tc.want {
				t.Fatalf("AutoDeleteMode()=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestConfigUnmarshalJSONPreservesUnknownFields(t *testing.T) {
	raw := `{"keys":["k1"],"accounts":[],"my_custom_field":"hello","number_field":42}`
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if cfg.AdditionalFields["my_custom_field"] != "hello" {
		t.Fatalf("expected custom field preserved, got %#v", cfg.AdditionalFields)
	}
	// number_field should also be preserved
	if cfg.AdditionalFields["number_field"] != float64(42) {
		t.Fatalf("expected number field preserved, got %#v", cfg.AdditionalFields["number_field"])
	}
}

func TestConfigUnmarshalJSONIgnoresRemovedLegacyModelMappings(t *testing.T) {
	raw := `{"keys":["k1"],"accounts":[],"claude_mapping":{"fast":"deepseek-v4-pro"},"claude_model_mapping":{"slow":"deepseek-v4-pro"}}`
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(cfg.ModelAliases) != 0 {
		t.Fatalf("expected removed legacy mappings to be ignored, got %#v", cfg.ModelAliases)
	}
	if _, ok := cfg.AdditionalFields["claude_mapping"]; ok {
		t.Fatalf("expected removed legacy field not to persist in additional fields: %#v", cfg.AdditionalFields)
	}
	if _, ok := cfg.AdditionalFields["claude_model_mapping"]; ok {
		t.Fatalf("expected removed legacy field not to persist in additional fields: %#v", cfg.AdditionalFields)
	}
}

func TestConfigUnmarshalJSONIgnoresRemovedHistorySplit(t *testing.T) {
	raw := `{"keys":["k1"],"history_split":{"enabled":true,"trigger_after_turns":2}}`
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if _, ok := cfg.AdditionalFields["history_split"]; ok {
		t.Fatalf("expected removed legacy field not to persist in additional fields: %#v", cfg.AdditionalFields)
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if strings.Contains(string(out), "history_split") {
		t.Fatalf("expected removed history_split field not to marshal, got %s", out)
	}
}

// ─── Config.Clone ────────────────────────────────────────────────────

func TestConfigCloneIsDeepCopy(t *testing.T) {
	cfg := Config{
		Keys:             []string{"key1"},
		Accounts:         []Account{{Email: "user@test.com", Token: "token"}},
		ModelAliases:     map[string]string{"claude-sonnet-4-6": "deepseek-v4-flash"},
		AdditionalFields: map[string]any{"custom": "value"},
	}

	cloned := cfg.Clone()

	// Modify original
	cfg.Keys[0] = "modified"
	cfg.Accounts[0].Email = "modified@test.com"
	cfg.ModelAliases["claude-sonnet-4-6"] = "modified-model"

	// Cloned should not be affected
	if cloned.Keys[0] != "key1" {
		t.Fatalf("clone keys was affected by original change: %#v", cloned.Keys)
	}
	if cloned.Accounts[0].Email != "user@test.com" {
		t.Fatalf("clone accounts was affected: %#v", cloned.Accounts)
	}
	if cloned.ModelAliases["claude-sonnet-4-6"] != "deepseek-v4-flash" {
		t.Fatalf("clone model aliases was affected: %#v", cloned.ModelAliases)
	}
}

func TestConfigCloneNilMaps(t *testing.T) {
	cfg := Config{
		Keys:     []string{"k"},
		Accounts: nil,
	}
	cloned := cfg.Clone()
	if len(cloned.Keys) != 1 {
		t.Fatalf("unexpected keys length: %d", len(cloned.Keys))
	}
	if cloned.Accounts != nil {
		t.Fatalf("expected nil accounts in clone, got %#v", cloned.Accounts)
	}
}

// ─── Account.Identifier edge cases ───────────────────────────────────

func TestAccountIdentifierPreferenceMobileOverToken(t *testing.T) {
	acc := Account{Mobile: "13800138000", Token: "tok"}
	if acc.Identifier() != "+8613800138000" {
		t.Fatalf("expected mobile identifier, got %q", acc.Identifier())
	}
}

func TestAccountIdentifierPreferenceEmailOverMobile(t *testing.T) {
	acc := Account{Email: "user@test.com", Mobile: "13800138000"}
	if acc.Identifier() != "user@test.com" {
		t.Fatalf("expected email identifier, got %q", acc.Identifier())
	}
}

func TestAccountIdentifierEmptyAccount(t *testing.T) {
	acc := Account{}
	if acc.Identifier() != "" {
		t.Fatalf("expected empty identifier for empty account, got %q", acc.Identifier())
	}
}

// ─── normalizeConfigInput ────────────────────────────────────────────

func TestNormalizeConfigInputStripsQuotes(t *testing.T) {
	got := normalizeConfigInput(`"base64:abc"`)
	if strings.HasPrefix(got, `"`) || strings.HasSuffix(got, `"`) {
		t.Fatalf("expected quotes stripped, got %q", got)
	}
}

func TestNormalizeConfigInputStripsSingleQuotes(t *testing.T) {
	got := normalizeConfigInput("'some-value'")
	if strings.HasPrefix(got, "'") || strings.HasSuffix(got, "'") {
		t.Fatalf("expected single quotes stripped, got %q", got)
	}
}

func TestNormalizeConfigInputTrimsWhitespace(t *testing.T) {
	got := normalizeConfigInput("  hello  ")
	if got != "hello" {
		t.Fatalf("expected trimmed, got %q", got)
	}
}

// ─── parseConfigString edge cases ────────────────────────────────────

func TestParseConfigStringPlainJSON(t *testing.T) {
	cfg, err := parseConfigString(`{"keys":["k1"],"accounts":[]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Keys) != 1 || cfg.Keys[0] != "k1" {
		t.Fatalf("unexpected keys: %#v", cfg.Keys)
	}
}

func TestParseConfigStringBase64Prefix(t *testing.T) {
	rawJSON := `{"keys":["base64-key"],"accounts":[]}`
	b64 := base64.StdEncoding.EncodeToString([]byte(rawJSON))
	cfg, err := parseConfigString("base64:" + b64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Keys) != 1 || cfg.Keys[0] != "base64-key" {
		t.Fatalf("unexpected keys: %#v", cfg.Keys)
	}
}

func TestParseConfigStringInvalidBase64(t *testing.T) {
	_, err := parseConfigString("base64:!!!invalid!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestParseConfigStringEmptyString(t *testing.T) {
	_, err := parseConfigString("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
}

// ─── Store methods ───────────────────────────────────────────────────

func TestStoreSnapshotReturnsClone(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[{"email":"u@test.com","token":"t1"}]}`)
	store := LoadStore()
	snap := store.Snapshot()
	snap.Keys[0] = "modified"
	if store.Keys()[0] != "k1" {
		t.Fatal("snapshot modification should not affect store")
	}
}

func TestStoreHasAPIKeyMultipleKeys(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["key1","key2","key3"],"accounts":[]}`)
	store := LoadStore()
	if !store.HasAPIKey("key1") {
		t.Fatal("expected key1 found")
	}
	if !store.HasAPIKey("key2") {
		t.Fatal("expected key2 found")
	}
	if !store.HasAPIKey("key3") {
		t.Fatal("expected key3 found")
	}
	if store.HasAPIKey("nonexistent") {
		t.Fatal("expected nonexistent key not found")
	}
}

func TestStoreFindAccountNotFound(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[{"email":"u@test.com"}]}`)
	store := LoadStore()
	_, ok := store.FindAccount("nonexistent@test.com")
	if ok {
		t.Fatal("expected account not found")
	}
}

func TestStoreIgnoresRemovedCompatConfig(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[],"compat":{"strip_reference_markers":false}}`)
	store := LoadStore()

	snap := store.Snapshot()
	data, err := snap.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if _, ok := out["compat"]; ok {
		t.Fatalf("expected removed compat field not to marshal, got %#v", out)
	}
}

func TestStoreIsEnvBacked(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[]}`)
	store := LoadStore()
	if !store.IsEnvBacked() {
		t.Fatal("expected env-backed store")
	}
}

func TestStoreReplace(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[]}`)
	store := LoadStore()
	newCfg := Config{
		Keys:     []string{"new-key"},
		Accounts: []Account{{Email: "new@test.com"}},
	}
	if err := store.Replace(newCfg); err != nil {
		t.Fatalf("replace error: %v", err)
	}
	if !store.HasAPIKey("new-key") {
		t.Fatal("expected new key after replace")
	}
	if store.HasAPIKey("k1") {
		t.Fatal("expected old key removed after replace")
	}
}

func TestStoreUpdate(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[]}`)
	store := LoadStore()
	err := store.Update(func(cfg *Config) error {
		cfg.Keys = append(cfg.Keys, "k2")
		return nil
	})
	if err != nil {
		t.Fatalf("update error: %v", err)
	}
	if !store.HasAPIKey("k2") {
		t.Fatal("expected k2 after update")
	}
}

func TestStoreUpdateReconcilesAPIKeyMutations(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["k1"],
		"api_keys":[{"key":"k1","name":"primary","remark":"prod"}],
		"accounts":[]
	}`)
	store := LoadStore()

	if err := store.Update(func(cfg *Config) error {
		cfg.APIKeys = append(cfg.APIKeys, APIKey{Key: "k2", Name: "secondary", Remark: "staging"})
		return nil
	}); err != nil {
		t.Fatalf("add api key failed: %v", err)
	}

	snap := store.Snapshot()
	if len(snap.Keys) != 2 || snap.Keys[0] != "k1" || snap.Keys[1] != "k2" {
		t.Fatalf("unexpected keys after api key add: %#v", snap.Keys)
	}
	if len(snap.APIKeys) != 2 {
		t.Fatalf("unexpected api keys length after add: %#v", snap.APIKeys)
	}
	if snap.APIKeys[0].Name != "primary" || snap.APIKeys[0].Remark != "prod" {
		t.Fatalf("metadata for existing key was lost: %#v", snap.APIKeys[0])
	}
	if snap.APIKeys[1].Name != "secondary" || snap.APIKeys[1].Remark != "staging" {
		t.Fatalf("metadata for new key was lost: %#v", snap.APIKeys[1])
	}

	if err := store.Update(func(cfg *Config) error {
		cfg.APIKeys = append([]APIKey(nil), cfg.APIKeys[1:]...)
		return nil
	}); err != nil {
		t.Fatalf("delete api key failed: %v", err)
	}

	snap = store.Snapshot()
	if len(snap.Keys) != 1 || snap.Keys[0] != "k2" {
		t.Fatalf("unexpected keys after api key delete: %#v", snap.Keys)
	}
	if len(snap.APIKeys) != 1 || snap.APIKeys[0].Key != "k2" {
		t.Fatalf("unexpected api keys after delete: %#v", snap.APIKeys)
	}
}

func TestStoreUpdateReconcilesLegacyKeyMutations(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["k1"],
		"api_keys":[{"key":"k1","name":"primary","remark":"prod"}],
		"accounts":[]
	}`)
	store := LoadStore()

	if err := store.Update(func(cfg *Config) error {
		cfg.Keys = append(cfg.Keys, "k2")
		return nil
	}); err != nil {
		t.Fatalf("legacy key update failed: %v", err)
	}

	snap := store.Snapshot()
	if len(snap.Keys) != 2 || snap.Keys[0] != "k1" || snap.Keys[1] != "k2" {
		t.Fatalf("unexpected keys after legacy update: %#v", snap.Keys)
	}
	if len(snap.APIKeys) != 2 {
		t.Fatalf("unexpected api keys after legacy update: %#v", snap.APIKeys)
	}
	if snap.APIKeys[0].Name != "primary" || snap.APIKeys[0].Remark != "prod" {
		t.Fatalf("metadata for preserved key was lost: %#v", snap.APIKeys[0])
	}
	if snap.APIKeys[1].Key != "k2" || snap.APIKeys[1].Name != "" || snap.APIKeys[1].Remark != "" {
		t.Fatalf("new legacy key should stay metadata-free: %#v", snap.APIKeys[1])
	}
}

func TestNormalizeCredentialsPrefersStructuredAPIKeys(t *testing.T) {
	cfg := Config{
		Keys: []string{"legacy-key"},
		APIKeys: []APIKey{
			{Key: "structured-key", Name: "primary", Remark: "prod"},
		},
	}
	cfg.NormalizeCredentials()

	if len(cfg.Keys) != 1 || cfg.Keys[0] != "structured-key" {
		t.Fatalf("unexpected normalized keys: %#v", cfg.Keys)
	}
	if len(cfg.APIKeys) != 1 {
		t.Fatalf("unexpected normalized api keys: %#v", cfg.APIKeys)
	}
	if cfg.APIKeys[0].Key != "structured-key" || cfg.APIKeys[0].Name != "primary" || cfg.APIKeys[0].Remark != "prod" {
		t.Fatalf("unexpected structured api key metadata: %#v", cfg.APIKeys[0])
	}
}

func TestStoreModelAliasesIncludesDefaultsAndOverrides(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":[],"accounts":[],"model_aliases":{"claude-opus-4-6":"deepseek-v4-flash-search"}}`)
	store := LoadStore()
	aliases := store.ModelAliases()
	if aliases["claude-sonnet-4-6"] != "deepseek-v4-flash" {
		t.Fatalf("expected default alias to remain available, got %q", aliases["claude-sonnet-4-6"])
	}
	if aliases["claude-opus-4-6"] != "deepseek-v4-flash-search" {
		t.Fatalf("expected custom alias override, got %q", aliases["claude-opus-4-6"])
	}
}

func TestStoreModelAliasesDefault(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":[],"accounts":[]}`)
	store := LoadStore()
	aliases := store.ModelAliases()
	if aliases == nil {
		t.Fatal("expected non-nil aliases")
	}
	if aliases["claude-sonnet-4-6"] != "deepseek-v4-flash" {
		t.Fatalf("expected built-in alias, got %q", aliases["claude-sonnet-4-6"])
	}
}

func TestStoreSetVercelSync(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":[],"accounts":[]}`)
	store := LoadStore()
	if err := store.SetVercelSync("hash123", 1234567890); err != nil {
		t.Fatalf("setVercelSync error: %v", err)
	}
	snap := store.Snapshot()
	if snap.VercelSyncHash != "hash123" || snap.VercelSyncTime != 1234567890 {
		t.Fatalf("unexpected vercel sync: hash=%q time=%d", snap.VercelSyncHash, snap.VercelSyncTime)
	}
}

func TestStoreExportJSONAndBase64(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["export-key"],"accounts":[]}`)
	store := LoadStore()
	jsonStr, b64Str, err := store.ExportJSONAndBase64()
	if err != nil {
		t.Fatalf("export error: %v", err)
	}
	if !strings.Contains(jsonStr, "export-key") {
		t.Fatalf("expected JSON to contain key: %q", jsonStr)
	}
	decoded, err := base64.StdEncoding.DecodeString(b64Str)
	if err != nil {
		t.Fatalf("base64 decode error: %v", err)
	}
	if !strings.Contains(string(decoded), "export-key") {
		t.Fatalf("expected base64-decoded to contain key: %q", string(decoded))
	}
}

// ─── OpenAIModelsResponse / ClaudeModelsResponse ─────────────────────

func TestOpenAIModelsResponse(t *testing.T) {
	resp := OpenAIModelsResponse()
	if resp["object"] != "list" {
		t.Fatalf("unexpected object: %v", resp["object"])
	}
	data, ok := resp["data"].([]ModelInfo)
	if !ok {
		t.Fatalf("unexpected data type: %T", resp["data"])
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty models list")
	}
	expected := map[string]bool{
		"deepseek-v4-flash":                   false,
		"deepseek-v4-flash-nothinking":        false,
		"deepseek-v4-pro":                     false,
		"deepseek-v4-pro-nothinking":          false,
		"deepseek-v4-flash-search":            false,
		"deepseek-v4-flash-search-nothinking": false,
		"deepseek-v4-vision":                  false,
		"deepseek-v4-vision-nothinking":       false,
	}
	for _, model := range data {
		if _, ok := expected[model.ID]; ok {
			expected[model.ID] = true
		}
	}
	for id, seen := range expected {
		if !seen {
			t.Fatalf("expected OpenAI model list to include %s", id)
		}
	}
}

func TestClaudeModelsResponse(t *testing.T) {
	resp := ClaudeModelsResponse()
	if resp["object"] != "list" {
		t.Fatalf("unexpected object: %v", resp["object"])
	}
	data, ok := resp["data"].([]ModelInfo)
	if !ok {
		t.Fatalf("unexpected data type: %T", resp["data"])
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty models list")
	}
}

func TestNormalizeAPIKeysPreservesMetadata(t *testing.T) {
	cfg := Config{
		APIKeys: []APIKey{
			{Key: "k1", Name: "a", Remark: "r1"},
			{Key: "k2", Name: "b"},
		},
	}
	cfg.NormalizeCredentials()

	if len(cfg.APIKeys) != 2 {
		t.Fatalf("unexpected api keys: %#v", cfg.APIKeys)
	}
	if cfg.APIKeys[0].Name != "a" || cfg.APIKeys[0].Remark != "r1" {
		t.Fatalf("metadata was lost during normalize: %#v", cfg.APIKeys[0])
	}
	if cfg.APIKeys[1].Name != "b" {
		t.Fatalf("name was lost during normalize: %#v", cfg.APIKeys[1])
	}
}

func TestEqualAPIKeysDetectsMetadataDifference(t *testing.T) {
	a := []APIKey{{Key: "k1", Name: "a"}}
	b := []APIKey{{Key: "k1", Name: "b"}}
	if equalAPIKeys(a, b) {
		t.Fatalf("equalAPIKeys should detect metadata difference")
	}
	if !equalAPIKeys(a, a) {
		t.Fatalf("equalAPIKeys should return true for identical slices")
	}
}
