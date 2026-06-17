package applog

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	defaultMaxMB            = 10
)

var (
	mu      sync.Mutex
	logger  *slog.Logger
	logFile *os.File
	logPath string
	maxSize int64 = defaultMaxMB * 1024 * 1024
)

func Init(dataDir string, maxFileSizeMB int) error {
	mu.Lock()
	defer mu.Unlock()

	if maxFileSizeMB < 1 {
		maxFileSizeMB = defaultMaxMB
	}
	maxSize = int64(maxFileSizeMB) * 1024 * 1024

	dir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logPath = filepath.Join(dir, "app.log")
	if err := rotateLocked(); err != nil {
		return err
	}

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open app log: %w", err)
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	logFile = file

	writer := &rotatingWriter{path: logPath}
	handler := slog.NewJSONHandler(io.MultiWriter(os.Stdout, writer), &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger = slog.New(handler)
	slog.SetDefault(logger)
	return nil
}

func Logger() *slog.Logger {
	mu.Lock()
	defer mu.Unlock()
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
		slog.SetDefault(logger)
	}
	return logger
}

func WithModule(module string) *slog.Logger {
	return Logger().With("module", module)
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	if strings.TrimSpace(requestID) == "" {
		requestID = NewRequestID()
	}
	return context.WithValue(ctx, requestIDKey, requestID)
}

func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value, ok := ctx.Value(requestIDKey).(string); ok {
		return value
	}
	return ""
}

func RequestIDFromHeader(w http.ResponseWriter) string {
	if w == nil {
		return ""
	}
	return w.Header().Get("X-Request-ID")
}

func RequestAttrs(ctx context.Context) []slog.Attr {
	if requestID := RequestID(ctx); requestID != "" {
		return []slog.Attr{slog.String("request_id", requestID)}
	}
	return nil
}

func NewRequestID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("req_%d", time.Now().UnixNano())
	}
	return "req_" + hex.EncodeToString(raw[:])
}

func LogPath() string {
	mu.Lock()
	defer mu.Unlock()
	return logPath
}

func ClearFile() error {
	mu.Lock()
	defer mu.Unlock()
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
	if logPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	logFile = file
	return nil
}

func ClearFileIfPath(path string) (bool, error) {
	mu.Lock()
	current := logPath
	mu.Unlock()
	if current == "" || filepath.Clean(current) != filepath.Clean(path) {
		return false, nil
	}
	return true, ClearFile()
}

type rotatingWriter struct {
	path string
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	mu.Lock()
	defer mu.Unlock()

	if logFile == nil {
		file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return 0, err
		}
		logFile = file
	}
	if err := rotateLocked(); err != nil {
		return 0, err
	}
	return logFile.Write(p)
}

func rotateLocked() error {
	if logPath == "" || maxSize <= 0 {
		return nil
	}
	info, err := os.Stat(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() < maxSize {
		return nil
	}
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
	rotatedPath := strings.TrimSuffix(logPath, filepath.Ext(logPath)) + "-" + time.Now().Format("20060102-150405") + ".log"
	if err := os.Rename(logPath, rotatedPath); err != nil {
		return err
	}
	return nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func isStaticAssetRequest(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "/assets/") {
		return true
	}
	switch {
	case strings.HasSuffix(path, ".css"),
		strings.HasSuffix(path, ".js"),
		strings.HasSuffix(path, ".map"),
		strings.HasSuffix(path, ".svg"),
		strings.HasSuffix(path, ".png"),
		strings.HasSuffix(path, ".jpg"),
		strings.HasSuffix(path, ".jpeg"),
		strings.HasSuffix(path, ".ico"),
		strings.HasSuffix(path, ".woff"),
		strings.HasSuffix(path, ".woff2"),
		strings.HasSuffix(path, ".ttf"):
		return true
	default:
		return false
	}
}

func shouldSkipRequestLog(r *http.Request, status int) bool {
	if r == nil {
		return false
	}
	userAgent := strings.ToLower(strings.TrimSpace(r.UserAgent()))
	if strings.Contains(userAgent, "codex local server discovery") {
		return true
	}
	if status < http.StatusBadRequest && isStaticAssetRequest(r.URL.Path) {
		return true
	}
	return false
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.size += n
	return n, err
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	if r.status == 0 {
		r.status = http.StatusSwitchingProtocols
	}
	return hijacker.Hijack()
}

func (r *statusRecorder) Flush() {
	flusher, ok := r.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	if r.status == 0 {
		r.status = http.StatusOK
	}
	flusher.Flush()
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = NewRequestID()
		}
		ctx := WithRequestID(r.Context(), requestID)
		w.Header().Set("X-Request-ID", requestID)

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}

		defer func() {
			if recovered := recover(); recovered != nil {
				rec.status = http.StatusInternalServerError
				Error(ctx, "http", "panic recovered", "panic", fmt.Sprint(recovered), "method", r.Method, "path", r.URL.Path)
				http.Error(rec, "internal server error", http.StatusInternalServerError)
			}
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			level := slog.LevelInfo
			if status >= 500 {
				level = slog.LevelError
			} else if status >= 400 {
				level = slog.LevelWarn
			}
			if shouldSkipRequestLog(r, status) {
				return
			}
			Logger().LogAttrs(ctx, level, "http request",
				slog.String("module", "http"),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Int("bytes", rec.size),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user_agent", r.UserAgent()),
			)
		}()

		next.ServeHTTP(rec, r.WithContext(ctx))
	})
}

func Info(ctx context.Context, module, message string, args ...any) {
	Logger().InfoContext(ctx, message, append([]any{"module", module}, args...)...)
}

func Warn(ctx context.Context, module, message string, args ...any) {
	Logger().WarnContext(ctx, message, append([]any{"module", module}, args...)...)
}

func Error(ctx context.Context, module, message string, args ...any) {
	Logger().ErrorContext(ctx, message, append([]any{"module", module}, args...)...)
}

func MarshalAttrs(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}
