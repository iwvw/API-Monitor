package huawei

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCanonicalURITrailingSlash(t *testing.T) {
	cases := map[string]string{
		"/v1/projects":    "/v1/projects/",
		"/v1/projects/":   "/v1/projects/",
		"/":               "/",
		"":                "/",
		"/v2/zones/abc":   "/v2/zones/abc/",
	}
	for input, want := range cases {
		if got := canonicalURI(input); got != want {
			t.Errorf("canonicalURI(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEscapePathSegment(t *testing.T) {
	if got := escapePathSegment("abc-_.~XYZ012"); got != "abc-_.~XYZ012" {
		t.Errorf("escapePathSegment reserved chars = %q", got)
	}
	if got := escapePathSegment("a/b c"); got != "a%2Fb%20c" {
		t.Errorf("escapePathSegment special chars = %q", got)
	}
	if got := escapePathSegment("项目"); got != "%E9%A1%B9%E7%9B%AE" {
		t.Errorf("escapePathSegment utf8 = %q", got)
	}
}

func TestCanonicalQueryStringSorted(t *testing.T) {
	values := url.Values{}
	values.Set("region_id", "cn-north-4")
	values.Set("type", "hcss.l-instance")
	values.Set("limit", "20")
	got := canonicalQueryString(values)
	want := "limit=20&region_id=cn-north-4&type=hcss.l-instance"
	if got != want {
		t.Errorf("canonicalQueryString = %q, want %q", got, want)
	}
}

func TestSignHeadersDeterministic(t *testing.T) {
	signer := &huaweiSigner{ak: "AK123456", sk: "SK-abcdef"}
	date := time.Date(2026, 9, 6, 5, 45, 36, 0, time.UTC)
	headers, err := signer.signHeadersAt(
		"iam.myhuaweicloud.cn",
		"GET",
		"/v3/projects",
		"",
		nil,
		date,
	)
	if err != nil {
		t.Fatalf("signHeadersAt: %v", err)
	}
	if headers["x-sdk-date"] != "20260906T054536Z" {
		t.Errorf("x-sdk-date = %q", headers["x-sdk-date"])
	}
	auth := headers["Authorization"]
	if !strings.HasPrefix(auth, "SDK-HMAC-SHA256 Access=AK123456, SignedHeaders=host;x-sdk-date, Signature=") {
		t.Errorf("Authorization format unexpected: %q", auth)
	}
	// 确定性：同一输入两次签名一致。
	headers2, err := signer.signHeadersAt(
		"iam.myhuaweicloud.cn",
		"GET",
		"/v3/projects",
		"",
		nil,
		date,
	)
	if err != nil {
		t.Fatalf("signHeadersAt second: %v", err)
	}
	if headers["Authorization"] != headers2["Authorization"] {
		t.Errorf("signature not deterministic")
	}
}

func TestSignHeadersContentHashBody(t *testing.T) {
	signer := &huaweiSigner{ak: "AK", sk: "SK"}
	date := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	empty, err := signer.signHeadersAt("ecs.cn-north-4.myhuaweicloud.cn", "POST", "/v1/p/action", "", nil, date)
	if err != nil {
		t.Fatal(err)
	}
	withBody, err := signer.signHeadersAt("ecs.cn-north-4.myhuaweicloud.cn", "POST", "/v1/p/action", "", []byte(`{}`), date)
	if err != nil {
		t.Fatal(err)
	}
	if empty["Authorization"] == withBody["Authorization"] {
		t.Errorf("body hash should change signature")
	}
}

func TestSiteOrDefaultAndSuffix(t *testing.T) {
	if got := siteOrDefault(""); got != "cn" {
		t.Errorf("siteOrDefault empty = %q", got)
	}
	if got := siteOrDefault("intl"); got != "intl" {
		t.Errorf("siteOrDefault intl = %q", got)
	}
	if got := siteSuffix("cn"); got != "cn" {
		t.Errorf("siteSuffix cn = %q", got)
	}
	if got := siteSuffix("intl"); got != "com" {
		t.Errorf("siteSuffix intl = %q", got)
	}
}

func TestServiceHostGlobal(t *testing.T) {
	if got := serviceHost("cn", "cn-north-4", "rms"); got != "rms.myhuaweicloud.cn" {
		t.Errorf("serviceHost rms = %q", got)
	}
	if got := serviceHost("cn", "cn-north-4", "ecs"); got != "ecs.cn-north-4.myhuaweicloud.cn" {
		t.Errorf("serviceHost ecs = %q", got)
	}
	if got := serviceHost("intl", "ap-southeast-1", "ecs"); got != "ecs.ap-southeast-1.myhuaweicloud.com" {
		t.Errorf("serviceHost intl ecs = %q", got)
	}
}

// TestNormalizeInstanceShowServer 锁定华为云 ECS ShowServer 响应解析：
// flavor.disk/vcpus/ram 与 addresses[].version 均为字符串，必须能解析出状态/IP/规格。
func TestNormalizeInstanceShowServer(t *testing.T) {
	raw := []byte(`{
		"server": {
			"id": "874e82c8-2848-4bab-bede-88c37d4d94a7",
			"name": "hcss_ecs_e176",
			"status": "ACTIVE",
			"flavor": {"id": "t7.large.1", "name": "t7.large.1", "vcpus": "2", "ram": "2048", "disk": "0"},
			"addresses": {
				"net": [
					{"addr": "192.168.5.170", "version": "4", "OS-EXT-IPS:type": "fixed"},
					{"addr": "189.1.217.109", "version": "4", "OS-EXT-IPS:type": "floating"}
				]
			},
			"metadata": {"charging_mode": "1"},
			"image": {"name": "Debian 11.1.0 64bit"},
			"created": "2024-09-23T07:48:18Z",
			"enterprise_project_id": "0"
		}
	}`)
	var envelope struct {
		Server json.RawMessage `json:"server"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	instance, err := normalizeInstance(envelope.Server, "ap-southeast-1")
	if err != nil {
		t.Fatalf("normalizeInstance: %v", err)
	}
	if instance.Status != "ACTIVE" {
		t.Errorf("status = %q, want ACTIVE", instance.Status)
	}
	if instance.PublicIP != "189.1.217.109" {
		t.Errorf("publicIp = %q", instance.PublicIP)
	}
	if instance.PrivateIP != "192.168.5.170" {
		t.Errorf("privateIp = %q", instance.PrivateIP)
	}
	if instance.VCPUs != 2 || instance.MemoryMB != 2048 {
		t.Errorf("vcpus/memory = %d/%d", instance.VCPUs, instance.MemoryMB)
	}
	if instance.FlavorName != "t7.large.1" {
		t.Errorf("flavorName = %q", instance.FlavorName)
	}
	if instance.ImageName != "Debian 11.1.0 64bit" {
		t.Errorf("imageName = %q", instance.ImageName)
	}
}
