package manifest

import (
	"strings"
	"testing"
)

func TestMatchPrefersMostSpecificPrefix(t *testing.T) {
	route, ok := Match("/api/server/agent/quick-install")
	if !ok {
		t.Fatal("expected a route match")
	}
	if route.Module != "server-agent" || route.Owner != OwnerGo {
		t.Fatalf("expected server-agent go route, got module=%s owner=%s", route.Module, route.Owner)
	}
}

func TestMatchAgentLinuxInstallWithKey(t *testing.T) {
	route, ok := Match("/api/server/agent/install/linux/server-1/secret-key")
	if !ok {
		t.Fatal("expected a route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthPublic || route.Prefix != "/api/server/agent/install/linux/{id}/{key}" {
		t.Fatalf("expected public keyed linux install route, got prefix=%s owner=%s auth=%s", route.Prefix, route.Owner, route.Auth)
	}
}

func TestGitHubPublicRealtimeStreamIsPublicAndStreaming(t *testing.T) {
	route, ok := Match("/api/github/public/pages/status/stream")
	if !ok {
		t.Fatal("expected GitHub public realtime stream route")
	}
	if route.Owner != OwnerGo || route.Auth != AuthPublic || route.ResponseMode != ResponseStream {
		t.Fatalf("unexpected public stream governance: owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}
}

func TestManifestDoesNotExposeRetiredRoutes(t *testing.T) {
	for _, route := range Routes() {
		if route.Owner == OwnerRetired {
			t.Fatalf("retired route must be removed instead of exposed: prefix=%s module=%s", route.Prefix, route.Module)
		}
	}
}

func TestServerInventoryRoutesRequireSession(t *testing.T) {
	for _, path := range []string{
		"/api/server/accounts",
		"/api/server/s",
		"/api/server/s/server-1",
		"/api/server/s/server-1/history",
	} {
		route, ok := Match(path)
		if !ok {
			t.Fatalf("expected server inventory route match for %s", path)
		}
		if route.Owner != OwnerGo || route.Auth != AuthSession || route.ResponseMode != ResponseJSON {
			t.Fatalf("expected session JSON go owner for %s, got owner=%s auth=%s response=%s", path, route.Owner, route.Auth, route.ResponseMode)
		}
	}
}

func TestMusicRouteIsRemoved(t *testing.T) {
	if route, ok := Match("/api/music/search"); ok {
		t.Fatalf("expected no removed music route match, got prefix=%s owner=%s", route.Prefix, route.Owner)
	}
}

func TestAuthRoutesAreSplitDuringMigration(t *testing.T) {
	route, ok := Match("/api/auth/login")
	if !ok {
		t.Fatal("expected auth route match")
	}
	if route.Owner != OwnerGo {
		t.Fatalf("expected go owner for auth login, got %s", route.Owner)
	}

	route, ok = Match("/api/auth/2fa/status")
	if !ok {
		t.Fatal("expected auth 2fa route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthPublic {
		t.Fatalf("expected public go owner for auth 2fa status, got owner=%s auth=%s", route.Owner, route.Auth)
	}

	route, ok = Match("/api/auth/2fa/setup")
	if !ok {
		t.Fatal("expected auth 2fa setup route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession {
		t.Fatalf("expected session go owner for auth 2fa management, got owner=%s auth=%s", route.Owner, route.Auth)
	}
}

func TestSettingsRoutesOwnDatabaseMaintenanceSlices(t *testing.T) {
	route, ok := Match("/api/settings")
	if !ok {
		t.Fatal("expected settings route match")
	}
	if route.Owner != OwnerGo {
		t.Fatalf("expected go owner for core settings, got %s", route.Owner)
	}

	route, ok = Match("/api/settings/database-stats")
	if !ok {
		t.Fatal("expected settings database route match")
	}
	if route.Owner != OwnerGo {
		t.Fatalf("expected go owner for database stats route, got %s", route.Owner)
	}

	route, ok = Match("/api/settings/operation-logs")
	if !ok {
		t.Fatal("expected settings operation logs route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession {
		t.Fatalf("expected session go owner for operation logs route, got owner=%s auth=%s", route.Owner, route.Auth)
	}

	route, ok = Match("/api/settings/log-settings")
	if !ok {
		t.Fatal("expected settings log settings route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession {
		t.Fatalf("expected session go owner for log settings route, got owner=%s auth=%s", route.Owner, route.Auth)
	}

	route, ok = Match("/api/settings/export-database")
	if !ok {
		t.Fatal("expected settings database export route match")
	}
	if route.Owner != OwnerGo {
		t.Fatalf("expected go owner for database export route, got %s", route.Owner)
	}

	route, ok = Match("/api/settings/database/import/preview")
	if !ok {
		t.Fatal("expected settings database import preview route match")
	}
	if route.Owner != OwnerGo || route.Prefix != "/api/settings/database/import" {
		t.Fatalf("expected go owner for database import route, got owner=%s prefix=%s", route.Owner, route.Prefix)
	}

	route, ok = Match("/api/settings/deprecated-tables")
	if !ok {
		t.Fatal("expected settings deprecated tables route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession {
		t.Fatalf("expected session go owner for deprecated tables route, got owner=%s auth=%s", route.Owner, route.Auth)
	}

	route, ok = Match("/api/settings/cleanup-deprecated-tables")
	if !ok {
		t.Fatal("expected settings deprecated cleanup route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession {
		t.Fatalf("expected session go owner for deprecated cleanup route, got owner=%s auth=%s", route.Owner, route.Auth)
	}

	if route, ok := Match("/api/settings/database/legacy"); ok {
		t.Fatalf("expected no removed settings database fallback route, got prefix=%s owner=%s", route.Prefix, route.Owner)
	}

	route, ok = Match("/api/settings/enforce-log-limits")
	if !ok {
		t.Fatal("expected settings log enforcement route match")
	}
	if route.Owner != OwnerGo {
		t.Fatalf("expected go owner for log enforcement route, got %s", route.Owner)
	}

	route, ok = Match("/api/settings/vacuum-database")
	if !ok {
		t.Fatal("expected settings vacuum route match")
	}
	if route.Owner != OwnerGo {
		t.Fatalf("expected go owner for vacuum route, got %s", route.Owner)
	}
}

func TestSystemHostMetricsRouteIsGoOwned(t *testing.T) {
	route, ok := Match("/api/system/host-metrics")
	if !ok {
		t.Fatal("expected system host metrics route match")
	}
	if route.Owner != OwnerGo {
		t.Fatalf("expected go owner for host metrics route, got %s", route.Owner)
	}

	if route, ok := Match("/api/system/unknown"); ok {
		t.Fatalf("expected no removed system fallback route, got prefix=%s owner=%s", route.Prefix, route.Owner)
	}
}

func TestTOTPRoutesAreGoOwned(t *testing.T) {
	route, ok := Match("/api/totp/accounts")
	if !ok {
		t.Fatal("expected totp route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession {
		t.Fatalf("expected session go owner for totp route, got owner=%s auth=%s", route.Owner, route.Auth)
	}
}

func TestM365RoutesAreGoOwned(t *testing.T) {
	route, ok := Match("/api/m365/accounts")
	if !ok {
		t.Fatal("expected m365 route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession || route.ResponseMode != ResponseJSON {
		t.Fatalf("expected session JSON go owner for m365 route, got owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}

	route, ok = Match("/api/m365/public/register")
	if !ok {
		t.Fatal("expected m365 public register route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthPublic || route.ResponseMode != ResponseJSON || route.Prefix != "/api/m365/public/register" {
		t.Fatalf("expected public exact go owner for m365 public register, got prefix=%s owner=%s auth=%s response=%s", route.Prefix, route.Owner, route.Auth, route.ResponseMode)
	}

	route, ok = Match("/api/m365/public/invites/demo-code")
	if !ok {
		t.Fatal("expected m365 public invite route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthPublic || route.ResponseMode != ResponseJSON || route.Prefix != "/api/m365/public/invites/{code}" {
		t.Fatalf("expected public pattern go owner for m365 public invite, got prefix=%s owner=%s auth=%s response=%s", route.Prefix, route.Owner, route.Auth, route.ResponseMode)
	}
}

func TestCronRoutesAreGoOwned(t *testing.T) {
	route, ok := Match("/api/cron/tasks")
	if !ok {
		t.Fatal("expected cron route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession || route.ResponseMode != ResponseJSON {
		t.Fatalf("expected session JSON go owner for cron route, got owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}
}

func TestFileboxRoutesAreGoOwnedWithMixedAuthInsideModule(t *testing.T) {
	route, ok := Match("/api/filebox/download/ABCDE")
	if !ok {
		t.Fatal("expected filebox route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthPublic || route.ResponseMode != ResponseJSON {
		t.Fatalf("expected public JSON go owner for filebox prefix, got owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}
}

func TestBookmarksRoutesAreGoOwnedWithMixedAuthInsideModule(t *testing.T) {
	// 内部路由走 session
	route, ok := Match("/api/bookmarks/groups")
	if !ok {
		t.Fatal("expected bookmarks groups route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession || route.ResponseMode != ResponseJSON {
		t.Fatalf("expected session JSON go owner for bookmarks groups, got owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}
	// 公开页路由走 public
	route, ok = Match("/api/bookmarks/public/groups/my-slug")
	if !ok {
		t.Fatal("expected bookmarks public group route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthPublic || route.ResponseMode != ResponseJSON {
		t.Fatalf("expected public JSON go owner for bookmarks public group, got owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}
	route, ok = Match("/api/bookmarks/public/page-by-domain")
	if !ok {
		t.Fatal("expected bookmarks page-by-domain route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthPublic || route.ResponseMode != ResponseJSON {
		t.Fatalf("expected public JSON go owner for bookmarks page-by-domain, got owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}
	// 本地下载的站点图标文件公开可访问（匿名公开页 img 需要），文件名 md5 不可枚举
	route, ok = Match("/api/bookmarks/favicons/abcd1234.png")
	if !ok {
		t.Fatal("expected bookmarks favicon file route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthPublic || route.ResponseMode != ResponseJSON {
		t.Fatalf("expected public JSON go owner for bookmarks favicon file, got owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}
}

func TestNotificationRoutesAreGoOwned(t *testing.T) {
	route, ok := Match("/api/notification/channels")
	if !ok {
		t.Fatal("expected notification route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession || route.ResponseMode != ResponseJSON {
		t.Fatalf("expected session JSON go owner for notification route, got owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}
}

func TestUptimeRoutesAreGoOwnedWithMixedAuthInsideModule(t *testing.T) {
	route, ok := Match("/api/uptime/monitors")
	if !ok {
		t.Fatal("expected uptime route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthPublic || route.ResponseMode != ResponseJSON {
		t.Fatalf("expected public JSON go owner for uptime prefix, got owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}
}

func TestKoyebRoutesAreGoOwned(t *testing.T) {
	route, ok := Match("/api/koyeb/accounts")
	if !ok {
		t.Fatal("expected koyeb route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession || route.ResponseMode != ResponseJSON {
		t.Fatalf("expected session JSON go owner for koyeb route, got owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}
}

func TestFlyioRoutesAreGoOwned(t *testing.T) {
	route, ok := Match("/api/flyio/accounts")
	if !ok {
		t.Fatal("expected flyio route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession || route.ResponseMode != ResponseJSON {
		t.Fatalf("expected session JSON go owner for flyio route, got owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}
}

func TestAliyunRoutesAreGoOwned(t *testing.T) {
	route, ok := Match("/api/aliyun/accounts")
	if !ok {
		t.Fatal("expected aliyun route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession || route.ResponseMode != ResponseJSON {
		t.Fatalf("expected session JSON go owner for aliyun route, got owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}
}

func TestCloudflareAccountDNSAndZoneResourceRoutesAreGoOwnedWithoutSwallowingOtherCloudflareRoutes(t *testing.T) {
	for _, path := range []string{
		"/api/cloudflare/accounts",
		"/api/cloudflare/accounts/export",
		"/api/cloudflare/export/accounts",
		"/api/cloudflare/import/accounts",
		"/api/cloudflare/templates",
		"/api/cloudflare/templates/tpl_123",
		"/api/cloudflare/templates/tpl_123/apply",
		"/api/cloudflare/import/templates",
		"/api/cloudflare/accounts/cf_123",
		"/api/cloudflare/accounts/cf_123/verify",
		"/api/cloudflare/accounts/cf_123/token",
		"/api/cloudflare/accounts/cf_123/cf-account-id",
		"/api/cloudflare/accounts/cf_123/pages",
		"/api/cloudflare/accounts/cf_123/pages/project_123",
		"/api/cloudflare/accounts/cf_123/pages/project_123/deployments",
		"/api/cloudflare/accounts/cf_123/pages/project_123/deployments/deploy_123",
		"/api/cloudflare/accounts/cf_123/pages/project_123/domains",
		"/api/cloudflare/accounts/cf_123/pages/project_123/domains/www.example.com",
		"/api/cloudflare/accounts/cf_123/workers",
		"/api/cloudflare/accounts/cf_123/workers/worker_123",
		"/api/cloudflare/accounts/cf_123/workers/worker_123/toggle",
		"/api/cloudflare/accounts/cf_123/workers/worker_123/analytics",
		"/api/cloudflare/accounts/cf_123/workers/worker_123/domains",
		"/api/cloudflare/accounts/cf_123/workers/worker_123/domains/domain_123",
		"/api/cloudflare/record-types",
		"/api/cloudflare/zones",
		"/api/cloudflare/accounts/cf_123/zones",
		"/api/cloudflare/accounts/cf_123/zones/zone_123",
		"/api/cloudflare/accounts/cf_123/zones/zone_123/workers/routes",
		"/api/cloudflare/accounts/cf_123/zones/zone_123/workers/routes/route_123",
		"/api/cloudflare/accounts/cf_123/zones/zone_123/records",
		"/api/cloudflare/accounts/cf_123/zones/zone_123/records/rec_123",
		"/api/cloudflare/accounts/cf_123/zones/zone_123/ssl",
		"/api/cloudflare/accounts/cf_123/zones/zone_123/analytics",
		"/api/cloudflare/accounts/cf_123/zones/zone_123/purge",
		"/api/cloudflare/accounts/cf_123/zones/zone_123/switch",
		"/api/cloudflare/accounts/cf_123/zones/zone_123/batch",
		"/api/cloudflare/accounts/cf_123/r2/buckets",
		"/api/cloudflare/accounts/cf_123/r2/buckets/bucket_name",
		"/api/cloudflare/accounts/cf_123/r2/buckets/bucket_name/objects",
		"/api/cloudflare/accounts/cf_123/r2/buckets/bucket_name/objects/key_123",
		"/api/cloudflare/accounts/cf_123/r2/buckets/bucket_name/objects/key_123/download-info",
		"/api/cloudflare/accounts/cf_123/r2/buckets/bucket_name/objects/key_123/preview",
		"/api/cloudflare/accounts/cf_123/tunnels",
		"/api/cloudflare/accounts/cf_123/tunnels/tunnel_123",
		"/api/cloudflare/accounts/cf_123/tunnels/tunnel_123/configuration",
		"/api/cloudflare/accounts/cf_123/tunnels/tunnel_123/token",
		"/api/cloudflare/accounts/cf_123/tunnels/tunnel_123/connections",
	} {
		route, ok := Match(path)
		if !ok {
			t.Fatalf("expected cloudflare go route match for %s", path)
		}
		expectedResponse := ResponseJSON
		if strings.HasSuffix(path, "/preview") {
			expectedResponse = ResponseProxy
		}
		if route.Owner != OwnerGo || route.Auth != AuthSession || route.ResponseMode != expectedResponse {
			t.Fatalf("expected session %s go owner for %s, got owner=%s auth=%s response=%s", expectedResponse, path, route.Owner, route.Auth, route.ResponseMode)
		}
	}

	for _, path := range []string{
		"/api/cloudflare/unimplemented-path-fallback",
	} {
		if route, ok := Match(path); ok {
			t.Fatalf("expected no removed cloudflare fallback route for %s, got prefix=%s owner=%s", path, route.Prefix, route.Owner)
		}
	}
}

func TestTencentRoutesAreGoOwned(t *testing.T) {
	route, ok := Match("/api/tencent/accounts")
	if !ok {
		t.Fatal("expected tencent route match")
	}
	if route.Owner != OwnerGo || route.Auth != AuthSession || route.ResponseMode != ResponseJSON {
		t.Fatalf("expected session JSON go owner for tencent route, got owner=%s auth=%s response=%s", route.Owner, route.Auth, route.ResponseMode)
	}
}

func TestOpenListRouteIsRemoved(t *testing.T) {
	if route, ok := Match("/api/openlist/manage-accounts"); ok {
		t.Fatalf("expected no removed openlist route match, got prefix=%s owner=%s", route.Prefix, route.Owner)
	}
}

func TestOpenAIRoutesAreGoOwned(t *testing.T) {
	for _, path := range []string{
		"/api/openai/endpoints",
		"/api/openai/endpoints/oai_123",
		"/api/openai/endpoints/oai_123/verify",
		"/api/openai/export",
		"/api/openai/import",
		"/v1/chat/completions",
		"/v1/responses",
		"/v1/models",
	} {
		route, ok := Match(path)
		if !ok {
			t.Fatalf("expected route match for %s", path)
		}
		if route.Owner != OwnerGo {
			t.Fatalf("expected go owner for %s, got %s", path, route.Owner)
		}
	}
}

func TestSummaryCoversAllRoutes(t *testing.T) {
	total := 0
	for _, count := range Summary() {
		total += count
	}
	if total != len(Routes()) {
		t.Fatalf("summary counted %d routes, want %d", total, len(Routes()))
	}
}

func TestCompareRouteSpecificityPrefersLiteralOverParam(t *testing.T) {
	cases := []struct {
		name      string
		literal   string
		param     string
	}{
		{
			name:    "prune beats imageRef placeholder",
			literal: "/api/server/v2/docker/{serverId}/images/prune",
			param:   "/api/server/v2/docker/{serverId}/images/{imageRef}",
		},
		{
			name:    "sync beats project placeholder",
			literal: "/api/server/v2/docker/{serverId}/stacks/sync",
			param:   "/api/server/v2/docker/{serverId}/stacks/{project}",
		},
		{
			name:    "exact beats pattern for same path",
			literal: "/api/x/foo",
			param:   "/api/x/{id}",
		},
	}
	for _, tc := range cases {
		if got := CompareRouteSpecificity(tc.literal, tc.param); got != 1 {
			t.Errorf("%s: CompareRouteSpecificity(literal, param) = %d, want 1", tc.name, got)
		}
		if got := CompareRouteSpecificity(tc.param, tc.literal); got != -1 {
			t.Errorf("%s: CompareRouteSpecificity(param, literal) = %d, want -1", tc.name, got)
		}
	}
	if got := CompareRouteSpecificity("/api/a/b", "/api/a/b"); got != 0 {
		t.Errorf("equal prefixes should compare 0, got %d", got)
	}
}
