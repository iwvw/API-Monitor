package requestbody

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalidUTF8Body     = errors.New("invalid utf-8 request body")
	errRequestBodyTooLarge = errors.New("request body too large")
)

func IsTooLarge(err error) bool {
	if errors.Is(err, errRequestBodyTooLarge) {
		return true
	}
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

const maxJSONUTF8ValidationSize = 16 << 20

// ValidateJSONUTF8 validates complete JSON request bodies before downstream
// decoders can silently replace malformed UTF-8 or stop before trailing bytes.
func ValidateJSONUTF8(next http.Handler) http.Handler {
	if next == nil {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldValidateJSONBody(r) {
			r.Body = validateAndReplayBody(r.Body)
		}
		next.ServeHTTP(w, r)
	})
}

func shouldValidateJSONBody(r *http.Request) bool {
	if r == nil || r.Body == nil {
		return false
	}
	path := ""
	if r.URL != nil {
		path = r.URL.Path
	}
	return isJSONContentType(r.Header.Get("Content-Type")) || isKnownJSONRequestPath(r.Method, path)
}

func isJSONContentType(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		mediaType = raw
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	return strings.Contains(mediaType, "json")
}

func isKnownJSONRequestPath(method, path string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	switch {
	case path == "/v1/chat/completions" || path == "/chat/completions":
		return true
	case path == "/v1/responses" || path == "/responses":
		return true
	case path == "/v1/embeddings" || path == "/embeddings":
		return true
	case path == "/anthropic/v1/messages" || path == "/v1/messages" || path == "/messages":
		return true
	case path == "/anthropic/v1/messages/count_tokens" || path == "/v1/messages/count_tokens" || path == "/messages/count_tokens":
		return true
	case strings.HasPrefix(path, "/v1beta/models/") || strings.HasPrefix(path, "/v1/models/"):
		return strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent")
	case strings.HasPrefix(path, "/admin/"):
		return true
	default:
		return false
	}
}

func validateAndReplayBody(body io.ReadCloser) io.ReadCloser {
	if body == nil {
		return body
	}
	raw, err := io.ReadAll(io.LimitReader(body, maxJSONUTF8ValidationSize+1))
	if err != nil {
		return &errorReadCloser{err: err, closer: body}
	}
	if len(raw) > maxJSONUTF8ValidationSize {
		return &errorReadCloser{err: errRequestBodyTooLarge, closer: body}
	}
	if !utf8.Valid(raw) {
		return &errorReadCloser{err: ErrInvalidUTF8Body, closer: body}
	}
	return &replayReadCloser{Reader: bytes.NewReader(raw), closer: body}
}

type replayReadCloser struct {
	*bytes.Reader
	closer io.Closer
}

func (r *replayReadCloser) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

type errorReadCloser struct {
	err    error
	closer io.Closer
}

func (r *errorReadCloser) Read([]byte) (int, error) {
	if r == nil || r.err == nil {
		return 0, io.EOF
	}
	return 0, r.err
}

func (r *errorReadCloser) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}
