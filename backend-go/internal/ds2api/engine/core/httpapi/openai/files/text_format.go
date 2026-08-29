package files

import (
	"path/filepath"
	"strings"
)

// DefaultTextFileExtensions is the built-in allow-list for text-file inlining.
// Users can override it via config, but this covers the common formats that are
// safe to read as UTF-8 and insert directly into a prompt.
var DefaultTextFileExtensions = []string{
	".txt", ".md", ".markdown",
	".csv", ".tsv",
	".json", ".jsonl", ".ndjson",
	".xml", ".yaml", ".yml",
	".ini", ".cfg", ".conf", ".config", ".properties", ".toml",
	".log",
	".sql",
	".sh", ".bash", ".zsh", ".fish", ".ps1", ".bat", ".cmd",
	".py", ".pyi", ".pyw",
	".js", ".mjs", ".cjs", ".jsx",
	".ts", ".tsx",
	".html", ".htm", ".xhtml",
	".css", ".scss", ".sass", ".less",
	".go",
	".c", ".cpp", ".cc", ".cxx", ".h", ".hpp", ".hh", ".hxx",
	".java", ".kt", ".kts",
	".rs",
	".rb", ".erb",
	".php",
	".swift", ".m", ".mm",
	".cs", ".vb",
	".fs", ".fsx",
	".lua",
	".pl", ".pm",
	".r",
	".scala", ".sc",
	".erl",
	".ex", ".exs",
	".ml",
	".clj", ".cljs",
	".elm",
	".hs",
	".pas",
	".d",
	".nim",
	".cr",
	".v", ".zig",
	".wren",
	".gleam",
	".bicep",
	".tf", ".hcl",
	".dockerfile", ".nginx",
	".gitignore", ".gitattributes", ".editorconfig",
	".env",
	".lock", ".sum", ".mod", ".work",
	".lisp", ".lsp", ".el",
	".vim", ".vimrc",
	".emacs",
	".org", ".pod", ".man",
	".rst", ".tex",
	".mdx", ".svx",
	".svelte", ".vue", ".astro", ".liquid",
	".graphql", ".gql",
	".prisma",
	".proto", ".thrift", ".avdl",
	".license", ".readme", ".changes", ".changelog",
	".credits", ".authors", ".copying",
	".makefile", ".mk",
	".cmake", ".ninja",
	".gradle", ".pom",
	".iml", ".project", ".classpath", ".factorypath",
	".bazel", ".bzl", ".bazelrc", ".star",
	".build", ".gn", ".gni", ".gyp", ".gypi",
	".vcxproj", ".sln", ".csproj", ".fsproj", ".vbproj",
	".targets", ".props", ".ruleset", ".nuspec",
	".resx", ".resources", ".settings",
}

// DefaultTextFileMimeTypes is consulted when the extension is unknown or
// missing. It lists non-text/* MIME types that are still plain text.
var DefaultTextFileMimeTypes = map[string]struct{}{
	"application/json":                  {},
	"application/xml":                   {},
	"application/javascript":            {},
	"application/ecmascript":            {},
	"application/sql":                   {},
	"application/x-yaml":                {},
	"application/x-toml":                {},
	"application/x-httpd-php":           {},
	"application/x-sh":                  {},
	"application/x-python-code":         {},
	"application/x-go":                  {},
	"application/x-java-source":         {},
	"application/graphql":               {},
	"application/vnd.api+json":          {},
	"application/ld+json":               {},
	"application/x-ndjson":              {},
	"application/rtf":                   {},
	"application/x-www-form-urlencoded": {},
}

// IsTextFile reports whether a file is a common text-convertible format based
// on its extension and/or MIME type.
func IsTextFile(filename, contentType string, allowedExtensions map[string]struct{}) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	if ext != "" {
		if allowedExtensions != nil {
			_, ok := allowedExtensions[ext]
			return ok
		}
		if isDefaultTextExtension(ext) {
			return true
		}
	}

	if allowedExtensions != nil {
		// When the user supplied an extension list we do not fall back to the
		// default MIME allow-list; the config is explicit about extensions.
		return false
	}

	ct := strings.ToLower(strings.TrimSpace(contentType))
	if before, _, found := strings.Cut(ct, ";"); found {
		ct = strings.TrimSpace(before)
	}
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	_, ok := DefaultTextFileMimeTypes[ct]
	return ok
}

func isDefaultTextExtension(ext string) bool {
	for _, e := range DefaultTextFileExtensions {
		if e == ext {
			return true
		}
	}
	return false
}

// ExtensionSet converts a string slice into a lookup set. If the slice is empty
// it returns nil so that IsTextFile falls back to the built-in defaults.
func ExtensionSet(exts []string) map[string]struct{} {
	if len(exts) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(exts))
	for _, e := range exts {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		out[e] = struct{}{}
	}
	return out
}
