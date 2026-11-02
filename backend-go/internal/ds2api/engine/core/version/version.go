package version

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// BuildVersion can be injected at build time via -ldflags.
// In release builds it should come from Git tag (e.g. v2.3.5).
var BuildVersion = ""

var (
	currentOnce sync.Once
	currentVal  string
	sourceVal   string
)

func Current() (value string, source string) {
	currentOnce.Do(func() {
		if build := strings.TrimSpace(BuildVersion); build != "" {
			currentVal = normalize(build)
			sourceVal = "build-ldflags"
			return
		}
		if fv := readVersionFile(); fv != "" {
			currentVal = normalize(fv)
			sourceVal = "file:VERSION"
			return
		}

		if vv := versionFromVercelEnv(); vv != "" {
			currentVal = vv
			sourceVal = "env:vercel"
			return
		}
		currentVal = "dev"
		sourceVal = "default"
	})
	return currentVal, sourceVal
}

func readVersionFile() string {
	candidates := []string{"VERSION"}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "VERSION"))
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
		candidates = append(candidates, filepath.Join(repoRoot, "VERSION"))
	}
	seen := map[string]struct{}{}
	for _, c := range candidates {
		c = filepath.Clean(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		b, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	return ""
}

func normalize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return strings.TrimPrefix(v, "v")
}

func Tag(v string) string {
	v = normalize(v)
	if v == "" || v == "dev" {
		return v
	}
	if v[0] < '0' || v[0] > '9' {
		return v
	}
	return "v" + v
}

func versionFromVercelEnv() string {
	if tag := normalize(strings.TrimSpace(os.Getenv("VERCEL_GIT_COMMIT_TAG"))); tag != "" {
		return tag
	}
	ref := strings.TrimSpace(os.Getenv("VERCEL_GIT_COMMIT_REF"))
	sha := strings.TrimSpace(os.Getenv("VERCEL_GIT_COMMIT_SHA"))
	if len(sha) > 7 {
		sha = sha[:7]
	}
	ref = sanitizeVersionLabel(ref)
	sha = sanitizeVersionLabel(sha)
	if ref == "" && sha == "" {
		return ""
	}
	if ref != "" && sha != "" {
		return "preview-" + ref + "." + sha
	}
	if ref != "" {
		return "preview-" + ref
	}
	return "preview-" + sha
}

func sanitizeVersionLabel(in string) string {
	in = strings.TrimSpace(strings.ToLower(in))
	if in == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(in))
	prevDash := false
	for i := 0; i < len(in); i++ {
		c := in[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}

func Compare(a, b string) int {
	pa := parse(normalize(a))
	pb := parse(normalize(b))
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func parse(v string) [3]int {
	var out [3]int
	parts := strings.SplitN(v, ".", 4)
	for i := 0; i < 3 && i < len(parts); i++ {
		n := readLeadingInt(parts[i])
		out[i] = n
	}
	return out
}

func readLeadingInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	i := 0
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			break
		}
	}
	if i == 0 {
		return 0
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0
	}
	return n
}
