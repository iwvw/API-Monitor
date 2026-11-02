package antigravity

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseManifestVersion(t *testing.T) {
	manifest := "version: 2.26.0\nfiles:\n  - url: https://example.com/a.zip\n"
	if got := parseManifestVersion(manifest); got != "2.26.0" {
		t.Fatalf("parseManifestVersion = %q, want 2.26.0", got)
	}

	quoted := "path: a.zip\nversion: \"2.24.1\"\n"
	if got := parseManifestVersion(quoted); got != "2.24.1" {
		t.Fatalf("parseManifestVersion quoted = %q, want 2.24.1", got)
	}

	if got := parseManifestVersion("files:\n  - url: x\n"); got != "" {
		t.Fatalf("manifest without version should be empty, got %q", got)
	}
}

func TestIsValidSemVersion(t *testing.T) {
	valid := []string{"2.24.0", "1.0.0", "10.20.30"}
	for _, v := range valid {
		if !isValidSemVersion(v) {
			t.Errorf("isValidSemVersion(%q) = false, want true", v)
		}
	}
	invalid := []string{"", "2.24", "2.24.0.1", "2.a.0", "2.24.-1", "v2.24.0"}
	for _, v := range invalid {
		if isValidSemVersion(v) {
			t.Errorf("isValidSemVersion(%q) = true, want false", v)
		}
	}
}

func TestHubUserAgentIncludesChangelist(t *testing.T) {
	// 回退版本必须带已知 changelist，且必须 >= 2.23.0（上游对新模型的门槛）。
	if got := hubUserAgentForVersion(AntigravityHubFallbackVersion); got !=
		"antigravity/hub/2.24.0 (aidev_client; os_type=darwin; arch=arm64; cl=1000335682)" {
		t.Fatalf("fallback UA = %q", got)
	}

	// 未知版本不套用别人的 changelist。
	if got := hubUserAgentForVersion("9.9.9"); got !=
		"antigravity/hub/9.9.9 (aidev_client; os_type=darwin; arch=arm64)" {
		t.Fatalf("unknown version UA = %q", got)
	}
}

func TestFallbackVersionMeetsCloudCodeFloor(t *testing.T) {
	// Cloud Code 拒绝 2.23.0 以下客户端访问新模型。兜底版本一旦下调会静默
	// 让所有新模型请求失败，这里用测试钉住。
	if AntigravityHubFallbackVersion < "2.23.0" {
		t.Fatalf("fallback version %q is below the Cloud Code floor 2.23.0", AntigravityHubFallbackVersion)
	}
}

func TestHubVersionRefreshedFromManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "electron-builder" {
			t.Errorf("manifest UA = %q, want electron-builder", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte("version: 2.30.1\nfiles: []\n"))
	}))
	defer server.Close()

	origURL := antigravityHubLatestManifestURL
	antigravityHubLatestManifestURL = server.URL
	defer func() {
		antigravityHubLatestManifestURL = origURL
		resetHubVersionForTest()
	}()

	resetHubVersionForTest()
	refreshHubVersion(context.Background())

	if got := HubLatestVersion(); got != "2.30.1" {
		t.Fatalf("HubLatestVersion = %q, want 2.30.1", got)
	}
	if !strings.Contains(GetUserAgent(), "antigravity/hub/2.30.1") {
		t.Fatalf("GetUserAgent should follow refreshed version, got %q", GetUserAgent())
	}
}

func TestHubVersionKeepsFallbackOnFetchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	origURL := antigravityHubLatestManifestURL
	antigravityHubLatestManifestURL = server.URL
	defer func() {
		antigravityHubLatestManifestURL = origURL
		resetHubVersionForTest()
	}()

	resetHubVersionForTest()
	refreshHubVersion(context.Background())

	if got := HubLatestVersion(); got != AntigravityHubFallbackVersion {
		t.Fatalf("HubLatestVersion = %q, want fallback %q", got, AntigravityHubFallbackVersion)
	}
}

func TestRequestUserAgentFamilies(t *testing.T) {
	base := hubBaseUserAgent("")
	if base != HubUserAgent() {
		t.Fatalf("empty UA should resolve to Hub UA, got %q", base)
	}

	// onboardUser 需要长 UA：Hub UA + google-api-nodejs-client。
	long := OnboardUserUserAgent("")
	if !strings.HasSuffix(long, " "+AntigravityNodeAPIClientUA) {
		t.Fatalf("onboard UA should carry node client suffix, got %q", long)
	}
	// 重复调用不应叠加后缀。
	if got := OnboardUserUserAgent(long); got != long {
		t.Fatalf("OnboardUserUserAgent should be idempotent, got %q", got)
	}
	// 请求 UA 要剥掉 node 后缀。
	if got := RequestUserAgent(long); strings.Contains(got, AntigravityNodeAPIClientUA) {
		t.Fatalf("RequestUserAgent should strip node suffix, got %q", got)
	}
}

func TestVersionFromUserAgent(t *testing.T) {
	long := OnboardUserUserAgent("")
	if got := VersionFromUserAgent(long); got != HubLatestVersion() {
		t.Fatalf("VersionFromUserAgent(long) = %q, want %q", got, HubLatestVersion())
	}
	// 显式版本要能被识别出来。
	if got := VersionFromUserAgent("antigravity/hub/2.30.1 (aidev_client)"); got != "2.30.1" {
		t.Fatalf("VersionFromUserAgent = %q, want 2.30.1", got)
	}
	// 非 Antigravity UA 回落到当前 Hub 版本。
	if got := VersionFromUserAgent("curl/8.0"); got != HubLatestVersion() {
		t.Fatalf("foreign UA version = %q, want %q", got, HubLatestVersion())
	}
}

func TestOnboardUserRequestShape(t *testing.T) {
	var captured []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("User-Agent"), AntigravityNodeAPIClientUA) {
			t.Errorf("onboardUser UA should carry node client, got %q", r.Header.Get("User-Agent"))
		}
		if got := r.Header.Get("X-Goog-Api-Client"); got != AntigravityGoogAPIClientUA {
			t.Errorf("X-Goog-Api-Client = %q, want %q", got, AntigravityGoogAPIClientUA)
		}
		buf := new(strings.Builder)
		raw, errRead := io.ReadAll(r.Body)
		if errRead != nil {
			t.Errorf("读取 onboardUser 请求体失败: %v", errRead)
		}
		buf.Write(raw)
		captured = []byte(buf.String())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"done":true,"response":{"cloudaicompanionProject":"proj-1"}}`))
	}))
	defer server.Close()

	withMockBaseURLs(t, []string{server.URL})

	client := mustNewClient(t, "")
	projectID, err := client.OnboardUser(context.Background(), "token", "free-tier")
	if err != nil {
		t.Fatalf("OnboardUser 失败: %v", err)
	}
	if projectID != "proj-1" {
		t.Fatalf("projectID = %q, want proj-1", projectID)
	}

	var body struct {
		TierID   string `json:"tier_id"`
		Metadata struct {
			IDEType    string `json:"ide_type"`
			IDEVersion string `json:"ide_version"`
			IDEName    string `json:"ide_name"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("解析 onboardUser 请求体失败: %v (%s)", err, captured)
	}
	if body.TierID != "free-tier" {
		t.Errorf("tier_id = %q, want free-tier", body.TierID)
	}
	if body.Metadata.IDEType != "ANTIGRAVITY" || body.Metadata.IDEName != "antigravity" {
		t.Errorf("metadata ide_type/ide_name = %q/%q", body.Metadata.IDEType, body.Metadata.IDEName)
	}
	if body.Metadata.IDEVersion != HubLatestVersion() {
		t.Errorf("ide_version = %q, want %q", body.Metadata.IDEVersion, HubLatestVersion())
	}
	// 旧字段不应再出现。
	if strings.Contains(string(captured), "tierId") || strings.Contains(string(captured), "pluginType") {
		t.Errorf("onboardUser 请求体仍包含旧字段: %s", captured)
	}
}
