package emailcode

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"

	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

// ParsedMail 是从邮件原文解析出的结构化信息。
type ParsedMail struct {
	MessageID  string
	From       string
	FromDomain string
	To         string
	Subject    string
	Text       string
	HTML       string
}

// ParseRaw 解析 MIME 原文，返回结构化字段。
// 处理 multipart 树、charset 解码、base64/quoted-printable 传输编码；
// 优先 text/plain，同时保留 text/html 供正文回退。
func ParseRaw(raw string) (ParsedMail, error) {
	out := ParsedMail{}
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		return out, err
	}
	out.Subject = decodeHeader(msg.Header.Get("Subject"))
	out.MessageID = strings.TrimSpace(msg.Header.Get("Message-Id"))
	from := msg.Header.Get("From")
	if addr, err := mail.ParseAddress(from); err == nil {
		out.From = addr.Address
		out.FromDomain = domainOfAddress(addr.Address)
	} else {
		out.From = strings.TrimSpace(from)
		out.FromDomain = domainOfAddress(out.From)
	}
	to := msg.Header.Get("To")
	if addr, err := mail.ParseAddress(to); err == nil {
		out.To = addr.Address
	} else {
		out.To = strings.TrimSpace(to)
	}

	contentType := msg.Header.Get("Content-Type")
	body, _ := io.ReadAll(io.LimitReader(msg.Body, 4<<20))
	walkPart(mediaType(contentType), headerParams(contentType), msg.Header.Get("Content-Transfer-Encoding"), body, &out)

	if out.Text == "" && out.HTML != "" {
		out.Text = htmlToText(out.HTML)
	}
	return out, nil
}

// walkPart 递归遍历 MIME 部件，把文本内容累积到 out。
func walkPart(partType string, params map[string]string, encoding string, body []byte, out *ParsedMail) {
	switch {
	case strings.HasPrefix(partType, "multipart/"):
		boundary := params["boundary"]
		if boundary == "" {
			return
		}
		reader := multipart.NewReader(bytes.NewReader(body), boundary)
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			partBody, _ := io.ReadAll(io.LimitReader(part, 4<<20))
			partCT := part.Header.Get("Content-Type")
			walkPart(mediaType(partCT), headerParams(partCT), part.Header.Get("Content-Transfer-Encoding"), partBody, out)
			if err := part.Close(); err != nil {
				break
			}
		}
	case partType == "text/plain":
		out.Text += decodeText(body, encoding, params["charset"]) + "\n"
	case partType == "text/html":
		out.HTML += decodeText(body, encoding, params["charset"]) + "\n"
	}
}

// decodeText 按传输编码与字符集解码文本部件。
func decodeText(body []byte, encoding, charset string) string {
	raw := body
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		cleaned := stripNonBase64(string(raw))
		if decoded, err := base64.StdEncoding.DecodeString(cleaned); err == nil {
			raw = decoded
		}
	case "quoted-printable":
		if decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(raw))); err == nil {
			raw = decoded
		}
	}
	return decodeCharset(raw, charset)
}

// decodeCharset 把任意字符集转为 UTF-8（未知或 UTF-8 时原样返回）。
func decodeCharset(raw []byte, charset string) string {
	charset = strings.ToLower(strings.TrimSpace(charset))
	if charset == "" || charset == "utf-8" || charset == "us-ascii" || charset == "ascii" {
		return string(raw)
	}
	enc, err := htmlindex.Get(charset)
	if err != nil || enc == nil {
		return string(raw)
	}
	if decoded, _, err := transform.Bytes(enc.NewDecoder(), raw); err == nil {
		return string(decoded)
	}
	return string(raw)
}

// decodeHeader 解码 MIME 编码的头部（如 =?UTF-8?B?...?=）。
func decodeHeader(value string) string {
	if value == "" {
		return ""
	}
	decoded, err := new(mime.WordDecoder).DecodeHeader(value)
	if err != nil {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(decoded)
}

func mediaType(contentType string) string {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	return strings.ToLower(mt)
}

func headerParams(contentType string) map[string]string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return map[string]string{}
	}
	return params
}

func stripNonBase64(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var (
	htmlTagRe    = regexp.MustCompile(`(?s)<[^>]*>`)
	htmlEntityRe = regexp.MustCompile(`&[a-zA-Z#0-9]+;`)
	spaceRe      = regexp.MustCompile(`[ \t\r\n]+`)
)

// htmlToText 把 HTML 转为纯文本（去标签、去实体、压空白）。
func htmlToText(html string) string {
	text := htmlTagRe.ReplaceAllString(html, " ")
	text = htmlEntityRe.ReplaceAllString(text, " ")
	return strings.TrimSpace(spaceRe.ReplaceAllString(text, " "))
}

func domainOfAddress(address string) string {
	addr := strings.TrimSpace(address)
	if at := strings.LastIndex(addr, "@"); at >= 0 {
		addr = addr[at+1:]
	}
	addr = strings.Trim(addr, "<>")
	return strings.ToLower(strings.TrimSpace(addr))
}
