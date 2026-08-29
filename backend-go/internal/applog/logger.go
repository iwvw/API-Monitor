package applog

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	defaultMaxMB            = 10
	// maxRotatedLogFiles 是轮转历史文件保留上限：超过后删除最旧的轮转文件，
	// 避免 logs 目录随时间无限堆积磁盘空间。
	maxRotatedLogFiles = 10
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
	jsonHandler := slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	stdoutHandler := &consoleHandler{level: slog.LevelInfo}
	logger = slog.New(&multiHandler{handlers: []slog.Handler{jsonHandler, stdoutHandler}})
	slog.SetDefault(logger)
	// 桥接标准库 log：把散落的 log.Printf 调用统一到 applog 格式（console + app.log）。
	// 去掉标准库自带时间前缀，避免与 applog 时间重复。
	log.SetOutput(&stdlogWriter{})
	log.SetFlags(0)
	return nil
}

// stdlogWriter 把标准库 log 的每一行转成 applog 日志，统一格式。
type stdlogWriter struct{}

func (w *stdlogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	if msg == "" {
		return len(p), nil
	}
	Logger().Info("std", "msg", msg)
	return len(p), nil
}

func Logger() *slog.Logger {
	mu.Lock()
	defer mu.Unlock()
	if logger == nil {
		logger = slog.New(&consoleHandler{level: slog.LevelInfo})
		slog.SetDefault(logger)
	}
	return logger
}

func SetLogger(l *slog.Logger) {
	mu.Lock()
	defer mu.Unlock()
	logger = l
	slog.SetDefault(l)
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
	pruneRotatedLogs()
	return nil
}

// pruneRotatedLogs 清理最旧的轮转日志，使 app-*.log 数量不超过 maxRotatedLogFiles。
// 仅在持锁状态下被 rotateLocked 调用；删除失败不影响日志轮转本身。
func pruneRotatedLogs() {
	if logPath == "" {
		return
	}
	dir := filepath.Dir(logPath)
	base := filepath.Base(logPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var rotated []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == base || !strings.HasPrefix(name, "app-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		rotated = append(rotated, filepath.Join(dir, name))
	}
	if len(rotated) <= maxRotatedLogFiles {
		return
	}
	sort.Slice(rotated, func(i, j int) bool {
		li, errI := os.Stat(rotated[i])
		lj, errJ := os.Stat(rotated[j])
		if errI != nil || errJ != nil {
			return rotated[i] < rotated[j]
		}
		return li.ModTime().Before(lj.ModTime())
	})
	for _, path := range rotated[:len(rotated)-maxRotatedLogFiles] {
		_ = os.Remove(path)
	}
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
	if status < http.StatusBadRequest && r.URL.Path == "/api/system/logs/stream" {
		return true
	}
	// 看板 2s 轮询的指标接口，成功响应不记日志避免刷屏；失败照常记录
	if status < http.StatusBadRequest && r.URL.Path == "/api/system/host-metrics" {
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

// Unwrap 透传底层 ResponseWriter：让 http.NewResponseController 能下钻到真实
// response 设置写 deadline（SSE 长连接续期），否则封装层挡住 SetWriteDeadline。
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
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

type consoleHandler struct {
	level slog.Level
	attrs []slog.Attr
}

func (h *consoleHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *consoleHandler) Handle(ctx context.Context, r slog.Record) error {
	timeStr := r.Time.Format("15:04:05")
	
	const (
		ansiReset   = "\033[0m"
		ansiRed     = "\033[31m"
		ansiGreen   = "\033[32m"
		ansiYellow  = "\033[33m"
		ansiBlue    = "\033[34m"
		ansiMagenta = "\033[35m"
		ansiCyan    = "\033[36m"
		ansiGray    = "\033[90m"
	)

	var levelColor string
	switch {
	case r.Level >= slog.LevelError:
		levelColor = ansiRed
	case r.Level >= slog.LevelWarn:
		levelColor = ansiYellow
	case r.Level >= slog.LevelInfo:
		levelColor = ansiGreen
	default:
		levelColor = ansiGray
	}

	levelStr := r.Level.String()
	module := "system"
	isHTTP := false
	method := ""
	path := ""
	status := int64(0)
	duration := int64(0)
	
	var extraAttrs []string
	
	extract := func(a slog.Attr) {
		if a.Key == "module" {
			module = a.Value.String()
			if module == "http" {
				isHTTP = true
			}
		} else {
			switch a.Key {
			case "method":
				method = a.Value.String()
			case "path":
				path = a.Value.String()
			case "status":
				// 业务日志可能以任意类型携带 status（如字符串状态码），
				// 仅在确为整数时解析，避免 Int64() 对 String 取值 panic。
				if a.Value.Kind() == slog.KindInt64 {
					status = a.Value.Int64()
				}
			case "duration_ms":
				if a.Value.Kind() == slog.KindInt64 {
					duration = a.Value.Int64()
				}
			default:
				if a.Key != "user_agent" && a.Key != "remote_addr" {
					extraAttrs = append(extraAttrs, fmt.Sprintf("%s=%v", a.Key, a.Value.Any()))
				}
			}
		}
	}

	for _, a := range h.attrs {
		extract(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		extract(a)
		return true
	})

	if isHTTP && r.Message == "http request" {
		statusColor := ansiGreen
		switch {
		case status >= 500:
			statusColor = ansiRed
		case status >= 400:
			statusColor = ansiYellow
		case status >= 300:
			statusColor = ansiCyan
		}

		methodColor := ansiCyan
		switch method {
		case "POST":
			methodColor = ansiGreen
		case "PUT", "PATCH":
			methodColor = ansiYellow
		case "DELETE":
			methodColor = ansiRed
		}

		durationColor := ansiGreen
		if duration >= 500 {
			durationColor = ansiRed
		} else if duration >= 100 {
			durationColor = ansiYellow
		}

		fmt.Printf("%s%s%s [%s%s%s] [%shttp%s] %s%s%s %s - %s%d%s (%s%dms%s)\n",
			ansiGray, timeStr, ansiReset,
			levelColor, levelStr, ansiReset,
			ansiCyan, ansiReset,
			methodColor, method, ansiReset,
			path,
			statusColor, status, ansiReset,
			durationColor, duration, ansiReset,
		)
		return nil
	}

	var extraStr string
	if len(extraAttrs) > 0 {
		extraStr = " | " + strings.Join(extraAttrs, " ")
	}

	fmt.Printf("%s%s%s [%s%s%s] [%s%s%s] %s%s\n",
		ansiGray, timeStr, ansiReset,
		levelColor, levelStr, ansiReset,
		ansiCyan, module, ansiReset,
		r.Message, extraStr,
	)
	return nil
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &consoleHandler{level: h.level, attrs: newAttrs}
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	return h
}

type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			_ = h.Handle(ctx, r)
		}
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: next}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		next[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: next}
}
