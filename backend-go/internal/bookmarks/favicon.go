package bookmarks

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	faviconMaxBytes = 2 * 1024 * 1024
	faviconUA       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

	// faviconSrcPrefix 是本地图标在书签 icon_src 里的存储前缀,
	// 与落盘返回值及 serveFaviconFile 的路由保持一致。
	faviconSrcPrefix = "/api/bookmarks/favicons/"

	// faviconGCGrace 是孤儿回收的宽限期:下载是「先原子落盘、再写回数据库」
	// 两步,两步之间文件已存在但尚无任何书签引用。刚写入的文件先放过,
	// 避免把正在写回的图标当成孤儿删掉。
	faviconGCGrace = 10 * time.Minute
)

// validateFetchURL 校验抓取目标：仅允许 http/https，且必须是可公开访问的主机。
// 此前完全没有校验，用户可控的 url 可被用来探测内网、云元数据端点
// （169.254.169.254）、localhost 等，即 SSRF。
//
// 这里做的是「解析期」校验：把域名解析成 IP 并逐个检查。
// 它与拨号期校验是互补的 —— 走代理时 DialContext 拿到的是代理地址，
// 拨号期校验只能看到代理，因此必须靠这一层拦住内网目标。
func validateFetchURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("only http/https is allowed, got %q", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("url has no host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := checkPublicIP(ip); err != nil {
			return nil, err
		}
		return parsed, nil
	}
	// 域名：解析后逐 IP 校验，避免「域名指向内网」绕过。
	// 解析失败不算致命（可能是临时 DNS 问题），交由后续请求报错。
	if ips, err := net.LookupIP(host); err == nil {
		for _, ip := range ips {
			if err := checkPublicIP(ip); err != nil {
				return nil, fmt.Errorf("host %s resolves to a non-public address: %w", host, err)
			}
		}
	}
	return parsed, nil
}

// checkPublicIP 拒绝对回环、私网、链路本地、ULA 等地址的访问。
func checkPublicIP(ip net.IP) error {
	switch {
	case ip.IsLoopback():
		return fmt.Errorf("loopback address is not allowed")
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		return fmt.Errorf("link-local address is not allowed")
	case ip.IsPrivate():
		return fmt.Errorf("private address is not allowed")
	case ip.IsUnspecified():
		return fmt.Errorf("unspecified address is not allowed")
	case ip.IsMulticast():
		return fmt.Errorf("multicast address is not allowed")
	}
	return nil
}

// isConfiguredProxyHost 判断某个拨号目标是否是环境变量里配置的代理。
// 代理通常就在本机（127.0.0.1:7897），不能对它做内网校验，否则代理会被自己拒掉。
//
// 注意不能借 ProxyFromEnvironment 来判断：Go 对 localhost/127.0.0.1 有内置放行，
// 用它探测时对「代理本身就是回环地址」这种情况会返回 nil，判断必然失败。
// 因此这里直接读环境变量。
func isConfiguredProxyHost(host, address string) bool {
	proxyEnvs := []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"}
	hosts := map[string]bool{host: true}
	if host == "localhost" {
		hosts["127.0.0.1"] = true
		hosts["::1"] = true
	}
	for _, key := range proxyEnvs {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			// 允许 "127.0.0.1:7897" 这种不带 scheme 的写法
			parsed, err = url.Parse("http://" + raw)
			if err != nil {
				continue
			}
		}
		proxyHost := parsed.Hostname()
		if proxyHost == "" {
			continue
		}
		if hosts[proxyHost] {
			return true
		}
		if proxyIP := net.ParseIP(proxyHost); proxyIP != nil {
			for h := range hosts {
				if hostIP := net.ParseIP(h); hostIP != nil && proxyIP.Equal(hostIP) {
					return true
				}
			}
		}
	}
	return false
}

// safeHTTPClient 返回一个带 SSRF 防护的 HTTP 客户端：
// 在拨号阶段校验真实连接地址（防 DNS rebinding 与重定向绕过），并限制跳转次数。
//
// 代理：自定义 Transport 会关掉 Go 默认的 ProxyFromEnvironment 行为，
// 因此必须显式设置 Proxy，否则 HTTPS_PROXY/HTTP_PROXY 完全不生效。
//
// 注意拨号期校验与代理的交互：走代理时 DialContext 拿到的是「代理地址」
// （通常是 127.0.0.1:7897），若对它做内网校验就会把代理自己拒掉
// （实测报错 proxyconnect tcp: loopback address is not allowed）。
// 因此这里区分两种情况：
//   - 目标是代理本身：放行（代理是本机基础设施，不是被探测的目标）；
//   - 目标是远端主机：照常做内网校验。
//
// 目标站点的内网校验由 validateFetchURL 在解析期完成（含域名解析后的 IP 校验），
// 两层配合才能在启用代理时依然防住 SSRF。
func safeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	proxyFunc := http.ProxyFromEnvironment
	transport := &http.Transport{
		// 显式启用环境变量代理（HTTP_PROXY / HTTPS_PROXY / NO_PROXY）。
		Proxy: proxyFunc,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			// 该地址是不是环境变量里配置的代理？是则直接放行。
			if isConfiguredProxyHost(host, address) {
				return dialer.DialContext(ctx, network, address)
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, addr := range ips {
				if err := checkPublicIP(addr.IP); err != nil {
					lastErr = err
					continue
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), portOf(address)))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("no address for host %q", host)
			}
			return nil, lastErr
		},
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			// 跳转目标同样要过 scheme/host 校验。
			_, err := validateFetchURL(req.URL.String())
			return err
		},
	}
}

func portOf(address string) string {
	if _, port, err := net.SplitHostPort(address); err == nil {
		return port
	}
	return "80"
}

// resolveFaviconCandidates 返回按优先级排序的全部候选图标地址（绝对 URL）。
// 优先用页面声明的 <link rel="icon">;没有(或全部失效)时回退到站点根的
// 常见图标路径(/favicon.ico、/favicon.png、/apple-touch-icon.png、/favicon.svg)。
// 候选已按「位图优先、SVG 最后」排好序，调用方逐个尝试即可
// （见 downloadFaviconFromCandidates）。
func resolveFaviconCandidates(pageURL string) ([]string, error) {
	base, err := validateFetchURL(pageURL)
	if err != nil {
		return nil, err
	}
	client := safeHTTPClient(12 * time.Second)
	req, err := http.NewRequest(http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", faviconUA)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("page returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	var hrefs []iconHref
	if doc, perr := html.Parse(strings.NewReader(string(body))); perr == nil {
		hrefs = sortedIconHrefs(doc)
	}

	out := make([]string, 0, len(hrefs)+1)
	for _, icon := range hrefs {
		ref, perr := url.Parse(icon.href)
		if perr != nil {
			continue
		}
		resolved := base.ResolveReference(ref)
		if resolved == nil {
			continue
		}
		if _, verr := validateFetchURL(resolved.String()); verr != nil {
			continue
		}
		out = append(out, resolved.String())
	}

	// 页面上没有可用图标时回退到站点根的常见图标路径(位图优先、SVG 最后)。
	out = appendFallbackCandidates(out, base)
	return out, nil
}

// commonFaviconPaths 是页面未声明图标时探测的站点根常见图标路径。
// 顺序保持「位图优先、SVG 最后」,与 iconPreference 一致。
var commonFaviconPaths = []string{
	"/favicon.ico",
	"/favicon.png",
	"/apple-touch-icon.png",
	"/favicon.svg",
}

// appendFallbackCandidates 把站点根的常见图标路径追加到 out,
// 去掉继承自页面 URL 的 query/fragment,并按完整地址去重。
//
// 这些候选只会在前面的候选都失败时才真正被下载(下载按顺序短路),
// 因此正常声明了图标的站点不会因为这里多出几条而多发请求。
func appendFallbackCandidates(out []string, base *url.URL) []string {
	if base == nil {
		return out
	}
	seen := make(map[string]bool, len(out)+len(commonFaviconPaths))
	for _, c := range out {
		seen[c] = true
	}
	for _, p := range commonFaviconPaths {
		u := *base
		u.Path = p
		u.RawPath = ""
		u.RawQuery = ""
		u.Fragment = ""
		s := u.String()
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// iconHref 是一个候选图标的地址与声明的类型（type 属性，可能为空）。
type iconHref struct {
	href string
	typ  string
}

// iconPreference 给候选图标排序：非 SVG 优先，SVG 最后。
//
// 为什么要排序：页面上常同时声明多种图标（如 favicon.ico / favicon.png /
// favicon.svg）。只取「第一个」会撞上声明顺序 —— 若 SVG 写在最前面，
// 就会放着更安全的 PNG 不用而去存 SVG。这里始终优先位图格式，
// 只有在确实没有位图候选时才回落到 SVG。
func iconPreference(icon iconHref) int {
	lower := strings.ToLower(icon.typ)
	href := strings.ToLower(icon.href)
	isSVG := strings.Contains(lower, "svg") || strings.HasSuffix(href, ".svg")
	if isSVG {
		return 10
	}
	// 明确声明为位图的排最前
	if strings.Contains(lower, "image/") || strings.Contains(lower, "icon") {
		return 0
	}
	// 没声明 type：按后缀推断，无后缀的当作普通候选
	switch path.Ext(href) {
	case ".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp":
		return 0
	case ".svg":
		return 10
	}
	return 1
}

// collectIconHrefs 收集页面里所有 rel 含 icon 的 <link>，按优先级排序：
// 位图在前、SVG 在后；同档位保持文档顺序（稳定排序）。
func collectIconHrefs(n *html.Node, out *[]iconHref) {
	if n.Type == html.ElementNode && n.Data == "link" {
		rel := ""
		href := ""
		typ := ""
		for _, attr := range n.Attr {
			switch attr.Key {
			case "rel":
				rel = strings.ToLower(attr.Val)
			case "href":
				href = attr.Val
			case "type":
				typ = attr.Val
			}
		}
		// rel 可能是 "icon"、"shortcut icon"、"apple-touch-icon" 等
		if strings.Contains(rel, "icon") && strings.TrimSpace(href) != "" {
			*out = append(*out, iconHref{href: strings.TrimSpace(href), typ: typ})
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectIconHrefs(c, out)
	}
}

// firstIconHref 保留旧签名（单值）供既有调用与测试使用：
// 返回优先级最高的候选，等价于排序后的第一个。
func firstIconHref(n *html.Node) string {
	candidates := sortedIconHrefs(n)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].href
}

// sortedIconHrefs 返回按优先级排好序的候选图标地址。
func sortedIconHrefs(n *html.Node) []iconHref {
	var candidates []iconHref
	collectIconHrefs(n, &candidates)
	// 稳定排序：同档位保持文档顺序，避免每次抓取结果漂移
	sort.SliceStable(candidates, func(i, j int) bool {
		return iconPreference(candidates[i]) < iconPreference(candidates[j])
	})
	return candidates
}

// downloadFaviconFromCandidates 按候选优先级依次尝试，返回第一个成功的本地路径。
//
// 为什么必须回退而不是只试第一个：页面声明的图标里第一个经常是坏的
// （404、非图片、体积超限），只试一个会让整站抓取直接判失败 ——
// 而 resolveFaviconCandidates 已经把可用候选按位图优先排好序了。
// force 为 true 时穿透「已存在即命中」的缓存短路,强制重新下载并覆盖
// (用于「重新抓取/覆盖图标」;否则同一图标 URL 永远返回首次的结果)。
func (s *Service) downloadFaviconFromCandidates(candidates []string, force bool) (string, error) {
	if len(candidates) == 0 {
		return "", fmt.Errorf("no favicon candidate found")
	}
	var lastErr error
	for _, candidate := range candidates {
		localPath, err := s.downloadFavicon(candidate, force)
		if err == nil {
			return localPath, nil
		}
		lastErr = err
	}

	// 所有候选都失败时的补充路径:有些静态服务器把图标放在 /favicon.ico/
	// 目录里(直接请求 /favicon.ico 会 301 到目录页),或 SPA 对 /favicon.ico
	// 回退成 index.html —— 两种情况拿到的都是 HTML 而不是图片。此时解析该
	// HTML 里指向图片的链接再试一次。只在失败路径触发,成功时不会多发请求。
	for _, candidate := range candidates {
		name := strings.TrimSuffix(candidate, "/")
		if !strings.HasSuffix(name, "/favicon.ico") {
			continue
		}
		for _, discovered := range discoverImageLinks(candidate) {
			localPath, err := s.downloadFavicon(discovered, force)
			if err == nil {
				return localPath, nil
			}
			lastErr = err
		}
	}
	return "", lastErr
}

// downloadFavicon 下载图标到本地 favicon 目录，返回 HTTP 可访问的相对路径。
func (s *Service) downloadFavicon(iconURL string, force bool) (string, error) {
	target, err := validateFetchURL(iconURL)
	if err != nil {
		return "", err
	}
	client := safeHTTPClient(15 * time.Second)
	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", faviconUA)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("favicon returned HTTP %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	ext, err := extensionFor(contentType, iconURL)
	if err != nil {
		return "", err
	}
	// 声明长度超限时直接拒绝，避免白读一遍再截断。
	if resp.ContentLength > faviconMaxBytes {
		return "", fmt.Errorf("favicon too large: %d bytes", resp.ContentLength)
	}
	// 多读 1 字节用于判定是否被截断：此前 LimitReader 静默截断，
	// 会把超限图片写成损坏的 2MB 文件并永久缓存。
	body, err := io.ReadAll(io.LimitReader(resp.Body, faviconMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) == 0 {
		return "", fmt.Errorf("favicon body empty")
	}
	if len(body) > faviconMaxBytes {
		return "", fmt.Errorf("favicon exceeds %d bytes", faviconMaxBytes)
	}
	// 校验真实内容确为图片，避免把 HTML 错误页当图标存成 .png。
	magicExt := sniffImageExt(body)
	if magicExt == "" {
		return "", fmt.Errorf("response is not a supported image")
	}
	if ext == "" {
		// Content-Type 与 URL 后缀都没给出格式时，以魔数为准。
		ext = magicExt
	}
	// 实际内容与声明的类型不一致时以魔数为准：
	// 例如声明 image/png 实际是 SVG（或反之），按声明存会写出扩展名与内容不符的文件。
	if magicExt != "" && magicExt != ext {
		ext = magicExt
	}
	if ext == ".svg" {
		// SVG 是唯一可携带脚本的图标格式，落盘前剥离脚本与事件处理器。
		sanitized, serr := sanitizeSVG(body)
		if serr != nil {
			return "", serr
		}
		body = sanitized
	}

	sum := md5.Sum([]byte(iconURL))
	filename := hex.EncodeToString(sum[:]) + ext
	dest := filepath.Join(s.faviconDir(), filename)
	if !force {
		if _, err := os.Stat(dest); err == nil {
			return faviconSrcPrefix + filename, nil
		}
	}
	// force(「重新抓取/覆盖」)时跳过上面这条缓存短路,让内容真的被刷新。

	if err := os.MkdirAll(s.faviconDir(), 0o755); err != nil {
		return "", err
	}
	// 原子写：先写临时文件再改名，避免崩溃/并发留下半截文件被永久当作命中缓存。
	tmp, err := os.CreateTemp(s.faviconDir(), "dl-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	// force 覆盖时目标文件可能已存在:Windows 上 os.Rename 拒绝覆盖,
	// 先移除旧文件再改名(缓存文件,短暂空窗可接受,下一次请求会重新命中)。
	if force {
		_ = os.Remove(dest)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return "", err
	}
	return faviconSrcPrefix + filename, nil
}

// sniffImageExt 通过魔数判断内容格式，返回对应扩展名（无法识别时返回空）。
func sniffImageExt(body []byte) string {
	if len(body) < 4 {
		return ""
	}
	switch {
	case bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")):
		return ".png"
	case bytes.HasPrefix(body, []byte("\xff\xd8\xff")):
		return ".jpg"
	case bytes.HasPrefix(body, []byte("GIF87a")), bytes.HasPrefix(body, []byte("GIF89a")):
		return ".gif"
	case bytes.HasPrefix(body, []byte("RIFF")) && len(body) >= 12 && bytes.Equal(body[8:12], []byte("WEBP")):
		return ".webp"
	case bytes.HasPrefix(body, []byte("\x00\x00\x01\x00")):
		return ".ico"
	case looksLikeSVG(body):
		return ".svg"
	}
	return ""
}

// looksLikeSVG 判断内容是否是 SVG。
// SVG 是纯文本、没有二进制魔数，因此跳过前导空白/BOM/XML 声明后再找 <svg。
func looksLikeSVG(body []byte) bool {
	// 只检查头部：图标文件很小，且避免把大段 HTML 误判成 SVG
	head := body
	if len(head) > 4096 {
		head = head[:4096]
	}
	trimmed := bytes.TrimLeft(head, " \t\r\n\ufeff")
	if len(trimmed) == 0 {
		return false
	}
	// 允许 BOM（UTF-8 BOM 已由 TrimLeft 处理，这里兜底 XML 声明）
	lower := bytes.ToLower(trimmed)
	return bytes.HasPrefix(lower, []byte("<svg")) || bytes.HasPrefix(lower, []byte("<?xml"))
}

// SVG 消毒用的正则。
//
// 为什么不用 XML 解析器重写：图标是很小的静态文件，而 encoding/xml 往返
// 会丢掉属性顺序、命名空间前缀与格式，可能把图标渲染坏。
// 这里改用「定向剥离」——只去掉可执行/可外联的部分，其余原样保留。
var (
	svgScriptRe     = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`)
	svgScriptSelfRe = regexp.MustCompile(`(?is)<script\b[^>]*/?>`)
	svgEventAttrRe  = regexp.MustCompile(`(?is)\son[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	svgHrefJSRe     = regexp.MustCompile(`(?is)(href|xlink:href|src)\s*=\s*("\s*javascript:[^"]*"|'\s*javascript:[^']*')`)
	svgForeignRe    = regexp.MustCompile(`(?is)<foreignObject\b[^>]*>.*?</foreignObject\s*>`)
	svgDoctypeRe    = regexp.MustCompile(`(?is)<!DOCTYPE[^>]*>`)
	svgEntityRe     = regexp.MustCompile(`(?is)<!ENTITY[^>]*>`)
)

// sanitizeSVG 剥离 SVG 中的可执行内容，返回可安全落盘的字节。
//
// 威胁模型：SVG 一旦被浏览器当作独立文档打开（直接访问 /api/bookmarks/favicons/xxx.svg），
// 其中的 <script> 就在本站源上执行，等价于存储型 XSS。
// 因此做最小必要剥离：
//  1. <script> 整段（含自闭合形式）
//  2. on* 事件属性（onload/onclick/onerror…）
//  3. href/src 为 javascript: 协议的引用
//  4. <foreignObject>（可内嵌任意 HTML）
//  5. DOCTYPE 与 ENTITY（防 XXE 与实体展开炸弹）
//
// 保留 <style>：图标常用内联样式上色，且 CSS 无法在本站源上执行 JS
// （老 IE 的 expression() 已无现实浏览器支持）。
func sanitizeSVG(body []byte) ([]byte, error) {
	text := string(body)
	if !looksLikeSVG(body) {
		return nil, fmt.Errorf("svg favicon rejected: content is not svg")
	}

	text = svgScriptRe.ReplaceAllString(text, "")
	text = svgScriptSelfRe.ReplaceAllString(text, "")
	text = svgForeignRe.ReplaceAllString(text, "")
	text = svgEventAttrRe.ReplaceAllString(text, "")
	text = svgHrefJSRe.ReplaceAllString(text, "$1=\"#\"")
	text = svgDoctypeRe.ReplaceAllString(text, "")
	text = svgEntityRe.ReplaceAllString(text, "")

	cleaned := []byte(text)
	if !looksLikeSVG(cleaned) {
		return nil, fmt.Errorf("svg favicon rejected: empty after sanitizing")
	}
	if len(cleaned) > faviconMaxBytes {
		return nil, fmt.Errorf("svg favicon exceeds %d bytes after sanitizing", faviconMaxBytes)
	}
	return cleaned, nil
}

func extensionFor(contentType, iconURL string) (string, error) {
	// 优先用响应声明的 Content-Type：URL 后缀经常缺失或与实际内容不符。
	lower := strings.ToLower(contentType)
	switch {
	case strings.Contains(lower, "image/png"):
		return ".png", nil
	case strings.Contains(lower, "image/jpeg"), strings.Contains(lower, "image/jpg"):
		return ".jpg", nil
	case strings.Contains(lower, "image/gif"):
		return ".gif", nil
	case strings.Contains(lower, "image/webp"):
		return ".webp", nil
	case strings.Contains(lower, "image/vnd.microsoft.icon"), strings.Contains(lower, "image/x-icon"), strings.Contains(lower, "image/ico"):
		return ".ico", nil
	case strings.Contains(lower, "image/svg"), strings.Contains(lower, "svg"):
		// SVG 允许存储，但下载后会做脚本剥离（见 sanitizeSVG）：
		// 页面上的 <link rel=icon> 常常只提供 SVG，一律拒绝会让这类站点拿不到图标。
		// 位图候选仍然优先，只有在没有 PNG/ICO 时才回落到 SVG。
		return ".svg", nil
	case strings.Contains(lower, "image/"):
		// 其他图片类型（avif/bmp/tiff 等）不支持存储。
		return "", fmt.Errorf("unsupported image type: %s", contentType)
	case strings.Contains(lower, "text/") || strings.Contains(lower, "html") || strings.Contains(lower, "json"):
		// 明确的非图片响应，直接拒绝，避免把错误页存成 .png。
		return "", fmt.Errorf("response is not an image: %s", contentType)
	}

	// Content-Type 缺失或不可判定时，回退到 URL 后缀。
	switch ext := strings.ToLower(path.Ext(iconURL)); ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp":
		return ext, nil
	case ".svg":
		// 同 image/svg 分支：允许，但后续会剥离脚本
		return ".svg", nil
	}
	// 不再默认 .png：调用方会再做魔数校验。
	return "", nil
}

// faviconIconMissing 判断图标引用是否指向本站本地 favicon 但文件已不存在。
//
// 仅当引用是本站本地路径且文件名合法时返回 true;远端 URL、文字图标、
// 非标准文件名一律返回 false(无从判定,保持原有「视为已有图标」的行为)。
//
// 用于两个场景:
//   - 导入:导出不含二进制(见 export.go 文件头),换实例导入后本地引用必然失效。
//     导入时清空这类引用,让它回归「缺少图标」,由抓取流程补齐。
//   - 批量抓取:把「引用存在但文件丢失」也算作缺少图标,避免 only_missing
//     把它们永久跳过。
func (s *Service) faviconIconMissing(iconSrc string) bool {
	raw := strings.TrimSpace(iconSrc)
	if !strings.HasPrefix(raw, faviconSrcPrefix) {
		return false
	}
	name := strings.TrimPrefix(raw, faviconSrcPrefix)
	if !faviconFilenameRe.MatchString(name) {
		return false
	}
	_, err := os.Stat(filepath.Join(s.faviconDir(), name))
	return os.IsNotExist(err)
}

// gcFaviconFiles 删除 favicon 目录里已不被任何书签引用的文件。
//
// 图标按 md5(iconURL) 命名并永久缓存,但删除书签、改网址后重抓、导入 replace
// 都会让旧文件失去引用。此前没有任何回收路径,data/bookmarks-favicons 只会
// 单调增长。删除类操作后调用本函数回收孤儿文件。
//
// 宽限期见 faviconGCGrace:刚写入、尚未写回数据库的文件不会被误删。
func (s *Service) gcFaviconFiles(ctx context.Context, db *sql.DB) {
	referenced, err := s.referencedFaviconNames(ctx, db)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(s.faviconDir())
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-faviconGCGrace)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !faviconFilenameRe.MatchString(name) {
			// 跳过 dl-*.tmp 等临时文件:它们可能在写入中。
			continue
		}
		if referenced[name] {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(s.faviconDir(), name))
	}
}

// referencedFaviconNames 返回仍被书签引用的本地 favicon 文件名集合。
func (s *Service) referencedFaviconNames(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT icon_src FROM bookmarks WHERE icon_src LIKE ?`, faviconSrcPrefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var src sql.NullString
		if err := rows.Scan(&src); err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(strings.TrimSpace(src.String), faviconSrcPrefix)
		if faviconFilenameRe.MatchString(name) {
			out[name] = true
		}
	}
	return out, rows.Err()
}

// maxIconDiscovery 是一次 HTML 发现最多采纳的图片链接数,避免把导航图当图标。
const maxIconDiscovery = 3

// discoverImageLinks 请求 rawURL;不是 HTML(或请求失败)时返回 nil。
// 若是 HTML(目录页/SPA 回退页),解析其中指向图片的 <a>/<link> 链接,
// 按重定向后的最终地址解析成绝对 URL,并逐个过 SSRF 校验。
func discoverImageLinks(rawURL string) []string {
	base, err := validateFetchURL(rawURL)
	if err != nil {
		return nil
	}
	client := safeHTTPClient(8 * time.Second)
	req, err := http.NewRequest(http.MethodGet, base.String(), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", faviconUA)
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}
	raw := imageLinksFromHTML(doc, resp.Request.URL)
	out := make([]string, 0, len(raw))
	for _, link := range raw {
		if _, verr := validateFetchURL(link); verr != nil {
			continue
		}
		out = append(out, link)
	}
	return out
}

// imageLinksFromHTML 收集文档里指向图片资源的 href,并基于 base 解析为绝对地址。
// 纯函数(不发请求、不做 SSRF 校验),便于单测;校验由调用方完成。
func imageLinksFromHTML(doc *html.Node, base *url.URL) []string {
	if doc == nil || base == nil {
		return nil
	}
	out := make([]string, 0, maxIconDiscovery)
	seen := map[string]bool{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(out) >= maxIconDiscovery {
			return
		}
		if n.Type == html.ElementNode && (n.Data == "a" || n.Data == "link") {
			for _, attr := range n.Attr {
				if attr.Key != "href" {
					continue
				}
				href := strings.TrimSpace(attr.Val)
				if !isImageHref(href) {
					continue
				}
				ref, perr := url.Parse(href)
				if perr != nil {
					continue
				}
				resolved := base.ResolveReference(ref)
				if resolved == nil {
					continue
				}
				s := resolved.String()
				if seen[s] {
					continue
				}
				seen[s] = true
				out = append(out, s)
				break
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

// isImageHref 判断 href 去掉 query/fragment 后是否以图片扩展名结尾。
func isImageHref(href string) bool {
	if href == "" || strings.HasPrefix(strings.ToLower(href), "javascript:") {
		return false
	}
	u, err := url.Parse(href)
	if err != nil {
		return false
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".svg":
		return true
	}
	return false
}
