package bookmarks

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	faviconMaxBytes = 2 * 1024 * 1024
	faviconUA       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
)

// resolveFaviconURL 抓取页面中 <link rel="icon"> 指向的图标地址，未命中时回退到 /favicon.ico。
func resolveFaviconURL(pageURL string) (string, error) {
	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
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
		return "", fmt.Errorf("page returned HTTP %d", resp.StatusCode)
	}

	base, err := url.Parse(pageURL)
	if err != nil {
		return "", err
	}

	var href string
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", err
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err == nil {
		href = firstIconHref(doc)
	}

	if href != "" {
		ref, perr := url.Parse(href)
		if perr == nil {
			resolved := base.ResolveReference(ref)
			if resolved != nil {
				return resolved.String(), nil
			}
		}
	}

	fallback := *base
	fallback.Path = "/favicon.ico"
	return fallback.String(), nil
}

func firstIconHref(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "link" {
		rel := ""
		href := ""
		for _, attr := range n.Attr {
			switch attr.Key {
			case "rel":
				rel = strings.ToLower(attr.Val)
			case "href":
				href = attr.Val
			}
		}
		if strings.Contains(rel, "icon") && href != "" {
			return href
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := firstIconHref(c); found != "" {
			return found
		}
	}
	return ""
}

// downloadFavicon 下载图标到本地 favicon 目录，返回 HTTP 可访问的相对路径。
func (s *Service) downloadFavicon(iconURL string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, iconURL, nil)
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, faviconMaxBytes))
	if err != nil {
		return "", err
	}
	if len(body) == 0 {
		return "", fmt.Errorf("favicon body empty")
	}

	sum := md5.Sum([]byte(iconURL))
	filename := hex.EncodeToString(sum[:]) + ext
	dest := s.faviconDir() + string(os.PathSeparator) + filename
	if _, err := os.Stat(dest); err == nil {
		return "/api/bookmarks/favicons/" + filename, nil
	}

	if err := os.MkdirAll(s.faviconDir(), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		return "", err
	}
	return "/api/bookmarks/favicons/" + filename, nil
}

func extensionFor(contentType, iconURL string) (string, error) {
	ext := strings.ToLower(path.Ext(iconURL))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp":
		return ext, nil
	case ".svg":
		return "", fmt.Errorf("svg favicon rejected")
	}
	switch {
	case strings.Contains(contentType, "image/png"):
		return ".png", nil
	case strings.Contains(contentType, "image/jpeg"):
		return ".jpg", nil
	case strings.Contains(contentType, "image/gif"):
		return ".gif", nil
	case strings.Contains(contentType, "image/svg"):
		return "", fmt.Errorf("svg favicon rejected")
	case strings.Contains(contentType, "image/webp"):
		return ".webp", nil
	case strings.Contains(contentType, "image/vnd.microsoft.icon"):
		return ".ico", nil
	default:
		return ".png", nil
	}
}
