package emailcode

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// 提取状态。
const (
	ExtractOK        = "ok"
	ExtractNone      = "none"
	ExtractAmbiguous = "ambiguous"
)

// 置信度（越大越可信）。
const (
	ConfidenceLow    = 1
	ConfidenceMedium = 2
	ConfidenceHigh   = 3
)

// Candidate 是一个验证码候选及其来源说明。
type Candidate struct {
	Code       string `json:"code"`
	Confidence int    `json:"confidence"`
	Source     string `json:"source"`
}

// ExtractResult 是提取结果：主码 + 全部候选 + 状态。
type ExtractResult struct {
	Code       string      `json:"code"`
	Status     string      `json:"status"`
	Candidates []Candidate `json:"candidates"`
	Link       string      `json:"link"`
}

// Template 是某发件人域名的提取模板，用于把「猜」收敛为「按规则取」。
// 零值字段表示不约束。Keywords 命中后取其后 Length 位数字，是最精确的一层。
type Template struct {
	FromDomain string
	Lengths    []int
	Kind       string // digits（默认）| alnum
	Keywords   *regexp.Regexp
	Subject    *regexp.Regexp
}

var (
	templateMu sync.RWMutex
	templates  = map[string]Template{}
)

// RegisterTemplate 注册或覆盖某发件人域名的提取模板。
func RegisterTemplate(t Template) {
	if t.FromDomain == "" {
		return
	}
	templateMu.Lock()
	defer templateMu.Unlock()
	templates[strings.ToLower(t.FromDomain)] = t
}

// TemplateFor 返回某发件人域名的模板。
func TemplateFor(fromDomain string) (Template, bool) {
	templateMu.RLock()
	defer templateMu.RUnlock()
	t, ok := templates[strings.ToLower(strings.TrimSpace(fromDomain))]
	return t, ok
}

var (
	// 关键词邻近：code/验证码/verification 之后 40 字符内的数字串。
	keywordForwardRe  = regexp.MustCompile(`(?i)(?:verification\s+code|your\s+code|security\s+code|one[-\s]?time\s+code|code|验证码|校验码|动态码)[^0-9]{0,40}([0-9]{4,8})`)
	keywordBackwardRe = regexp.MustCompile(`(?i)([0-9]{4,8})[^0-9]{0,40}(?:is\s+your\s+(?:code|verification\s+code|one[-\s]?time\s+code)|为您的?验证码|是你的?验证码)`)
	// 裸数字：两侧非数字边界。
	bareNumberRe = regexp.MustCompile(`(?:^|[^0-9])([0-9]{4,8})(?:[^0-9]|$)`)
	linkRe       = regexp.MustCompile(`https?://[^\s"'<>)\]]+`)
)

// Extract 按分层策略从邮件原文提取验证码：
//  1. 注册模板（发件人命中时按 Keywords/Lengths 精确提取）
//  2. 关键词上下文（code / 验证码 附近）
//  3. 裸数字兜底
//
// 返回全部候选；同置信度出现多个不同码时判为 ambiguous，交由调用方决定是否重试。
func Extract(raw string) ExtractResult {
	parsed, err := ParseRaw(raw)
	if err != nil {
		// 解析失败时退回对原文做启发式提取，尽力而为。
		parsed.Text = raw
	}
	body := parsed.Text
	if body == "" {
		body = raw
	}
	return ExtractFromParsed(parsed, body)
}

// ExtractFromParsed 在已解析的邮件上执行分层提取。
func ExtractFromParsed(parsed ParsedMail, body string) ExtractResult {
	res := ExtractResult{Status: ExtractNone}
	res.Link = firstMatch(linkRe, body)

	if tmpl, ok := TemplateFor(parsed.FromDomain); ok {
		res.Candidates = append(res.Candidates, applyTemplate(tmpl, parsed, body)...)
	}
	if len(res.Candidates) == 0 {
		res.Candidates = append(res.Candidates, keywordCandidates(body)...)
	}
	if len(res.Candidates) == 0 {
		res.Candidates = append(res.Candidates, bareCandidates(body)...)
	}

	res.Candidates = dedupeCandidates(res.Candidates)
	if len(res.Candidates) == 0 {
		return res
	}
	best := res.Candidates[0]
	for _, c := range res.Candidates[1:] {
		if c.Confidence > best.Confidence {
			best = c
		}
	}
	// 同置信度多个不同码：判为模糊，不轻易给出错误的主码。
	differing := 0
	for _, c := range res.Candidates {
		if c.Confidence == best.Confidence && c.Code != best.Code {
			differing++
		}
	}
	if differing > 0 {
		res.Status = ExtractAmbiguous
		res.Code = ""
		return res
	}
	res.Code = best.Code
	res.Status = ExtractOK
	return res
}

// applyTemplate 用模板精确提取。
func applyTemplate(t Template, parsed ParsedMail, body string) []Candidate {
	if t.Subject != nil && !t.Subject.MatchString(parsed.Subject) {
		return nil
	}
	pattern := t.Keywords
	if pattern == nil && t.Subject != nil {
		pattern = t.Subject
	}
	if pattern == nil {
		return nil
	}
	return scanLengths(pattern, body, t.Lengths, t.Kind, ConfidenceHigh, "template")
}

// keywordCandidates 用通用关键词上下文提取。
func keywordCandidates(body string) []Candidate {
	out := []Candidate{}
	for _, re := range []*regexp.Regexp{keywordForwardRe, keywordBackwardRe} {
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			if len(m) > 1 && isPlausibleCode(m[1]) {
				out = append(out, Candidate{Code: m[1], Confidence: ConfidenceMedium, Source: "keyword"})
			}
		}
	}
	return out
}

// bareCandidates 裸数字兜底（排除年份等常见误报）。
func bareCandidates(body string) []Candidate {
	out := []Candidate{}
	for _, m := range bareNumberRe.FindAllStringSubmatch(body, -1) {
		if len(m) > 1 && isPlausibleCode(m[1]) {
			out = append(out, Candidate{Code: m[1], Confidence: ConfidenceLow, Source: "bare"})
		}
	}
	return out
}

// scanLengths 在关键词命中处按长度抓取验证码。
func scanLengths(pattern *regexp.Regexp, text string, lengths []int, kind string, confidence int, source string) []Candidate {
	if len(lengths) == 0 {
		lengths = []int{6}
	}
	out := []Candidate{}
	alnum := strings.EqualFold(kind, "alnum")
	for _, length := range lengths {
		var re *regexp.Regexp
		if alnum {
			re = regexp.MustCompile(`(?i)` + pattern.String() + `[^A-Za-z0-9]{0,40}([A-Za-z0-9]{` + strconv.Itoa(length) + `})`)
		} else {
			re = regexp.MustCompile(`(?i)` + pattern.String() + `[^0-9]{0,40}([0-9]{` + strconv.Itoa(length) + `})`)
		}
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if len(m) > 1 && m[1] != "" {
				out = append(out, Candidate{Code: m[1], Confidence: confidence, Source: source})
			}
		}
	}
	return out
}

// isPlausibleCode 排除年份、超长数字等常见误报。
func isPlausibleCode(code string) bool {
	if len(code) < 4 || len(code) > 8 {
		return false
	}
	if len(code) == 4 && (strings.HasPrefix(code, "19") || strings.HasPrefix(code, "20")) {
		return false
	}
	if n, err := strconv.Atoi(code); err == nil {
		// 全同一数字（如 000000）通常不是验证码。
		if code == strings.Repeat(code[:1], len(code)) {
			return false
		}
		_ = n
	}
	return true
}

func dedupeCandidates(in []Candidate) []Candidate {
	best := map[string]Candidate{}
	order := []string{}
	for _, c := range in {
		prev, ok := best[c.Code]
		if !ok {
			best[c.Code] = c
			order = append(order, c.Code)
			continue
		}
		if c.Confidence > prev.Confidence {
			best[c.Code] = c
		}
	}
	out := make([]Candidate, 0, len(order))
	for _, code := range order {
		out = append(out, best[code])
	}
	// 高置信度优先。
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Confidence > out[i].Confidence {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func firstMatch(re *regexp.Regexp, text string) string {
	m := re.FindString(text)
	return strings.TrimRight(m, ".,;:)")
}
