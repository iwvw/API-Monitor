package protocol

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSharedConstantsLoaded(t *testing.T) {
	cfg := sharedConstants{}
	if err := json.Unmarshal(sharedConstantsJSON, &cfg); err != nil {
		t.Fatalf("failed to parse shared constants: %v", err)
	}
	client := normalizeClientConstants(cfg.Client)
	if ClientVersion != client.Version {
		t.Fatalf("unexpected client version=%q", ClientVersion)
	}
	if _, ok := BaseHeaders["accept-charset"]; ok {
		t.Fatal("unexpected accept-charset header present")
	}
	if BaseHeaders["x-client-platform"] != "web" {
		t.Fatalf("unexpected base header x-client-platform=%q", BaseHeaders["x-client-platform"])
	}
	if BaseHeaders["x-client-version"] != ClientVersion {
		t.Fatalf("unexpected base header x-client-version=%q", BaseHeaders["x-client-version"])
	}
	if BaseHeaders["Content-Type"] != "application/json" {
		t.Fatalf("unexpected base header Content-Type=%q", BaseHeaders["Content-Type"])
	}
	// 真实网页版抓包确认 platform=web 也会发 x-client-bundle-id，
	// 早前把它当 App 专属头移除是误判。
	if BaseHeaders["x-client-bundle-id"] != "com.deepseek.chat" {
		t.Fatalf("unexpected x-client-bundle-id=%q", BaseHeaders["x-client-bundle-id"])
	}
	// platform=web 时 BaseHeaders 应出现完整浏览器头。
	for _, h := range []string{"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform", "sec-fetch-site", "sec-fetch-mode", "sec-fetch-dest", "Referer", "Origin", "Accept-Language"} {
		if _, ok := BaseHeaders[h]; !ok {
			t.Fatalf("expected browser header missing: %s", h)
		}
	}
	if !strings.Contains(BaseHeaders["User-Agent"], "Chrome") {
		t.Fatalf("expected Chrome User-Agent for web platform, got %q", BaseHeaders["User-Agent"])
	}
	if BaseHeaders["x-client-timezone-offset"] != "28800" {
		t.Fatalf("unexpected timezone offset for zh_CN: %q", BaseHeaders["x-client-timezone-offset"])
	}
	if len(SkipContainsPatterns) == 0 {
		t.Fatal("expected skip contains patterns to be loaded")
	}
	if _, ok := SkipExactPathSet["response/search_status"]; !ok {
		t.Fatal("expected response/search_status in exact skip path set")
	}
}

func TestClientHeadersDerivedFromSharedVersion(t *testing.T) {
	client := normalizeClientConstants(clientConstants{
		Name:            "DeepSeek",
		Platform:        "web",
		Version:         "9.8.7",
		AndroidAPILevel: "35",
		Locale:          "zh_CN",
	})
	headers := BuildBaseHeaders(client, map[string]string{
		"x-client-version": "stale",
	})
	if !strings.Contains(headers["User-Agent"], "Chrome") {
		t.Fatalf("expected Chrome User-Agent for web platform, got %q", headers["User-Agent"])
	}
	if headers["x-client-version"] != "9.8.7" {
		t.Fatalf("unexpected derived client version=%q", headers["x-client-version"])
	}
	if headers["Accept-Language"] != AcceptLanguageFor("zh_CN") {
		t.Fatalf("unexpected accept-language=%q", headers["Accept-Language"])
	}
	// 浏览器 fetch 发 */*，不是 application/json。
	if headers["Accept"] != "*/*" {
		t.Fatalf("web platform should send Accept=*/*, got %q", headers["Accept"])
	}
}

// Accept-Language 取决于用户的语言设置，用默认安装的短形式。
// 长形式（带 en-US;q=0.8,en;q=0.7 兜底链）只出现在额外添加了英语的配置文件，
// 无痕/默认对照组发的是短形式。
func TestAcceptLanguageUsesDefaultProfileForm(t *testing.T) {
	if got := AcceptLanguageFor("zh_CN"); got != "zh-CN,zh;q=0.9" {
		t.Fatalf("zh_CN accept-language = %q", got)
	}
	if got := AcceptLanguageFor("en_US"); got != "en-US,en;q=0.9" {
		t.Fatalf("en_US accept-language = %q", got)
	}
	if got := AcceptLanguageFor("xx_YY"); got != AcceptLanguageFor("zh_CN") {
		t.Fatalf("unknown locale should fall back to zh_CN, got %q", got)
	}
}

// 不要伪造 x-hif-dliq / x-hif-leim，理由见 constants.go 的长注释。
func TestNoDeviceFingerprintHeadersAreForged(t *testing.T) {
	headers := BaseHeadersFor("zh_CN")
	for name := range headers {
		if strings.HasPrefix(strings.ToLower(name), "x-hif-") {
			t.Fatalf("不应伪造设备指纹头 %q：它是设备级标识，"+
				"所有账号共用一份会造成「N 个账号同一台设备」的关联信号，"+
				"而真实浏览器在无痕模式下本来就不发它", name)
		}
	}
}

// 浏览器发消息时 Referer 是所在会话页，不是站点根路径。
func TestChatSessionReferer(t *testing.T) {
	if got := ChatSessionReferer("9b4da123"); got != "https://chat.deepseek.com/a/chat/s/9b4da123" {
		t.Fatalf("session referer = %q", got)
	}
	if got := ChatSessionReferer("  "); got != "https://chat.deepseek.com/" {
		t.Fatalf("empty session should fall back to site root, got %q", got)
	}
}

func TestAppPlatformUsesAppUserAgent(t *testing.T) {
	client := normalizeClientConstants(clientConstants{
		Name:            "DeepSeek",
		Platform:        "android",
		Version:         "2.2.2",
		AndroidAPILevel: "35",
		Locale:          "zh_CN",
	})
	headers := BuildBaseHeaders(client, nil)
	if headers["User-Agent"] != "DeepSeek/2.2.2" {
		t.Fatalf("expected App User-Agent, got %q", headers["User-Agent"])
	}
	if _, ok := headers["sec-ch-ua"]; ok {
		t.Fatal("unexpected browser header on android platform")
	}
}

// TestTimezoneOffsetIsRealAndDSTAware pins the offsets that never move and
// checks the DST-sensitive ones against the live zone data.
//
// Regression guard: en_US used to be hardcoded to "-420", a minutes value in a
// field everything else expressed in seconds. It claimed UTC-00:07 — a
// timezone that does not exist, and a stronger anomaly than the hardcoded
// UTC+8 it was meant to replace.
func TestTimezoneOffsetIsRealAndDSTAware(t *testing.T) {
	// Neither China nor Japan observes daylight saving, so these are fixed.
	for locale, want := range map[string]string{"zh_CN": "28800", "ja_JP": "32400"} {
		if got := TimezoneOffsetFor(locale); got != want {
			t.Errorf("locale %s: expected timezone %s, got %s", locale, want, got)
		}
	}

	// US Pacific is -28800 in winter and -25200 under daylight saving.
	switch got := TimezoneOffsetFor("en_US"); got {
	case "-28800", "-25200":
	default:
		t.Errorf("locale en_US: expected a US Pacific offset, got %s", got)
	}

	// Every mapped locale must agree with the IANA database right now.
	for locale, zone := range localeTimezones {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatalf("locale %s: embedded tzdata missing zone %s: %v", locale, zone, err)
		}
		_, offset := time.Now().In(loc).Zone()
		if got, want := TimezoneOffsetFor(locale), strconv.Itoa(offset); got != want {
			t.Errorf("locale %s (%s): got %s, want %s", locale, zone, got, want)
		}
		// A real UTC offset is a whole number of minutes within +/- 14h.
		if offset%60 != 0 || offset < -14*3600 || offset > 14*3600 {
			t.Errorf("locale %s (%s): implausible offset %d seconds", locale, zone, offset)
		}
	}
}

func TestTimezoneAndLanguageDerivedFromLocale(t *testing.T) {
	cases := []struct {
		locale   string
		tz       string
		language string
	}{
		{"zh_CN", "28800", AcceptLanguageFor("zh_CN")},
		{"en_US", TimezoneOffsetFor("en_US"), "en-US,en;q=0.9"},
		{"ja_JP", "32400", AcceptLanguageFor("ja_JP")},
	}
	for _, tc := range cases {
		tz := TimezoneOffsetFor(tc.locale)
		if tz != tc.tz {
			t.Fatalf("locale %s: expected timezone %s, got %s", tc.locale, tc.tz, tz)
		}
		lang := AcceptLanguageFor(tc.locale)
		if lang != tc.language {
			t.Fatalf("locale %s: expected language %s, got %s", tc.locale, tc.language, lang)
		}
		headers := BuildBaseHeaders(clientConstants{Name: "DeepSeek", Platform: "web", Version: "2.2.2", Locale: tc.locale}, nil)
		if headers["x-client-timezone-offset"] != tc.tz {
			t.Fatalf("locale %s: expected header timezone %s, got %s", tc.locale, tc.tz, headers["x-client-timezone-offset"])
		}
		if headers["Accept-Language"] != tc.language {
			t.Fatalf("locale %s: expected header language %s, got %s", tc.locale, tc.language, headers["Accept-Language"])
		}
	}
}

func TestLoginHeadersUseConservativeAppStyle(t *testing.T) {
	headers := LoginHeaders("zh_CN")
	if headers["User-Agent"] != "DeepSeek/"+ClientVersion {
		t.Fatalf("expected App User-Agent for login, got %q", headers["User-Agent"])
	}
	for _, h := range []string{"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform", "sec-fetch-site", "sec-fetch-mode", "sec-fetch-dest", "Referer", "Origin", "Accept-Language"} {
		if _, ok := headers[h]; ok {
			t.Fatalf("unexpected browser header in login headers: %s", h)
		}
	}
	if headers["x-client-timezone-offset"] != "28800" {
		t.Fatalf("unexpected login timezone offset: %q", headers["x-client-timezone-offset"])
	}
}
