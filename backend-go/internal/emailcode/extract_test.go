package emailcode

import (
	"regexp"
	"strings"
	"testing"
)

func TestExtractPlainTextKeyword(t *testing.T) {
	raw := strings.Join([]string{
		"From: PostHog <hey@posthog.com>",
		"To: a@x.com",
		"Subject: Let's verify your email",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Enter this code to verify your email address:",
		"",
		"919941",
	}, "\r\n")
	res := Extract(raw)
	if res.Code != "919941" {
		t.Fatalf("应提取 919941，得到 %q (status=%s cands=%+v)", res.Code, res.Status, res.Candidates)
	}
	if res.Status != ExtractOK {
		t.Fatalf("status 应为 ok，得到 %s", res.Status)
	}
}

func TestExtractIgnoresHeaderTransactionID(t *testing.T) {
	// 邮件头里的长十六进制事务 ID 含连续 6 位数字，绝不能被当成验证码。
	raw := strings.Join([]string{
		"Received: by da72629123f14942ad25aa8d62edc9d72b0ebac656892fdbce43380da4e7c30b",
		"From: hey@posthog.com",
		"Subject: Your code",
		"Content-Type: text/plain",
		"",
		"Your verification code is 483920.",
	}, "\r\n")
	res := Extract(raw)
	if res.Code != "483920" {
		t.Fatalf("应从正文取码，得到 %q (cands=%+v)", res.Code, res.Candidates)
	}
}

func TestExtractBase64Multipart(t *testing.T) {
	plain := "Welcome to PostHog.\r\n\r\nYour code is 551234.\r\n"
	boundary := "BOUND123"
	raw := strings.Join([]string{
		"From: hey@posthog.com",
		"To: a@x.com",
		"Subject: Verify",
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"",
		"",
		"--" + boundary,
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: base64",
		"",
		b64(plain),
		"--" + boundary + "--",
		"",
	}, "\r\n")
	res := Extract(raw)
	if res.Code != "551234" {
		t.Fatalf("base64 multipart 应提取 551234，得到 %q (status=%s cands=%+v)", res.Code, res.Status, res.Candidates)
	}
}

func TestExtractQuotedPrintableSoftBreak(t *testing.T) {
	raw := strings.Join([]string{
		"From: hey@posthog.com",
		"Subject: code",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: quoted-printable",
		"",
		"Your code is 12=",
		"3456",
	}, "\r\n")
	res := Extract(raw)
	if res.Code != "123456" {
		t.Fatalf("软换行应还原，得到 %q (cands=%+v)", res.Code, res.Candidates)
	}
}

func TestExtractHTMLOnly(t *testing.T) {
	raw := strings.Join([]string{
		"From: hey@posthog.com",
		"Subject: verify",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<html><body><p>Your verification code is</p><h1>778899</h1></body></html>",
	}, "\r\n")
	res := Extract(raw)
	if res.Code != "778899" {
		t.Fatalf("HTML 邮件应转文本后提取，得到 %q (cands=%+v)", res.Code, res.Candidates)
	}
}

func TestExtractAmbiguousReturnsNoPrimary(t *testing.T) {
	// 两个同置信度候选：宁可不给主码，由调用方决定重试。
	raw := "verification code 111234 and backup verification code 222345"
	res := ExtractFromParsed(ParsedMail{}, raw)
	if res.Status != ExtractAmbiguous {
		t.Fatalf("应判为模糊，得到 status=%s code=%q cands=%+v", res.Status, res.Code, res.Candidates)
	}
	if res.Code != "" {
		t.Fatalf("模糊时不应给出主码，得到 %q", res.Code)
	}
}

func TestExtractNoCode(t *testing.T) {
	res := ExtractFromParsed(ParsedMail{}, "Hello, this is a plain message with no digits.")
	if res.Status != ExtractNone || res.Code != "" {
		t.Fatalf("无码应返回 none，得到 status=%s code=%q", res.Status, res.Code)
	}
}

func TestExtractTemplateTakesPrecedence(t *testing.T) {
	RegisterTemplate(Template{
		FromDomain: "tpl.example",
		Lengths:    []int{8},
		Keywords:   keywordsFor("passcode"),
	})
	defer func() {
		templateMu.Lock()
		delete(templates, "tpl.example")
		templateMu.Unlock()
	}()
	raw := strings.Join([]string{
		"From: no-reply@tpl.example",
		"Subject: Your passcode",
		"Content-Type: text/plain",
		"",
		"Your passcode is 12345678 and code is 999999.",
	}, "\r\n")
	res := Extract(raw)
	if res.Code != "12345678" {
		t.Fatalf("模板应优先取 8 位码，得到 %q (cands=%+v)", res.Code, res.Candidates)
	}
	if res.Candidates[0].Source != "template" || res.Candidates[0].Confidence != ConfidenceHigh {
		t.Fatalf("首个候选应来自模板且高置信度: %+v", res.Candidates[0])
	}
}

func TestExtractLink(t *testing.T) {
	parsed := ParsedMail{Text: "Click https://posthog.com/verify?t=abc to confirm."}
	res := ExtractFromParsed(parsed, parsed.Text)
	if res.Link != "https://posthog.com/verify?t=abc" {
		t.Fatalf("应提取验证链接，得到 %q", res.Link)
	}
}

func TestExtractRejectsYearAsCandidate(t *testing.T) {
	res := ExtractFromParsed(ParsedMail{}, "Copyright 2024 example. code 445566")
	if res.Code != "445566" {
		t.Fatalf("应跳过年份取真码，得到 %q (cands=%+v)", res.Code, res.Candidates)
	}
}

func TestParseRawDecodesCharset(t *testing.T) {
	// GBK 编码的「验证码」+ 数字，应正确解码后提取。
	body := []byte{0xd1, 0xe9, 0xd6, 0xa4, 0xc2, 0xeb, ':', ' ', '9', '8', '7', '6', '5', '4'}
	raw := string([]byte("From: a@b.com\r\nSubject: test\r\nContent-Type: text/plain; charset=gbk\r\n\r\n")) + string(body)
	parsed, err := ParseRaw(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parsed.Text, "验证码") {
		t.Fatalf("应正确解码 GBK 正文，得到 %q", parsed.Text)
	}
}

func keywordsFor(word string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)` + regexp.QuoteMeta(word))
}
