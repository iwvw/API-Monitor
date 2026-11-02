package rawsample

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var referenceMarkerRe = regexp.MustCompile(`(?i)\[reference:\s*\d+\]`)

type CaptureRound struct {
	Label         string `json:"label,omitempty"`
	URL           string `json:"url,omitempty"`
	StatusCode    int    `json:"status_code"`
	ResponseBytes int    `json:"response_bytes"`
}

type CaptureSummary struct {
	Label                    string         `json:"label,omitempty"`
	URL                      string         `json:"url,omitempty"`
	StatusCode               int            `json:"status_code"`
	ResponseBytes            int            `json:"response_bytes"`
	Rounds                   []CaptureRound `json:"rounds,omitempty"`
	ContainsReferenceMarkers bool           `json:"contains_reference_markers,omitempty"`
	ReferenceMarkerCount     int            `json:"reference_marker_count,omitempty"`
	ContainsFinishedToken    bool           `json:"contains_finished_token,omitempty"`
	FinishedTokenCount       int            `json:"finished_token_count,omitempty"`
}

type Meta struct {
	SampleID      string         `json:"sample_id"`
	CapturedAtUTC string         `json:"captured_at_utc"`
	Source        string         `json:"source,omitempty"`
	Request       any            `json:"request"`
	Capture       CaptureSummary `json:"capture"`
}

type PersistOptions struct {
	RootDir      string
	SampleID     string
	Source       string
	Request      any
	Capture      CaptureSummary
	UpstreamBody []byte
}

type SavedSample struct {
	SampleID     string
	Dir          string
	MetaPath     string
	UpstreamPath string
	Meta         Meta
}

func Persist(opts PersistOptions) (SavedSample, error) {
	root := strings.TrimSpace(opts.RootDir)
	if root == "" {
		return SavedSample{}, errors.New("root dir is required")
	}
	if len(opts.UpstreamBody) == 0 {
		return SavedSample{}, errors.New("upstream body is required")
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return SavedSample{}, fmt.Errorf("create root dir: %w", err)
	}

	baseID := NormalizeSampleID(opts.SampleID)
	if baseID == "" {
		baseID = DefaultSampleID("capture")
	}
	sampleID, err := uniqueSampleID(root, baseID)
	if err != nil {
		return SavedSample{}, err
	}

	tempID := ".tmp-" + sampleID + "-" + strings.ToLower(strings.ReplaceAll(uuid.NewString(), "-", ""))
	tempDir := filepath.Join(root, tempID)
	finalDir := filepath.Join(root, sampleID)
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return SavedSample{}, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	upstreamPath := filepath.Join(tempDir, "upstream.stream.sse")
	if err := os.WriteFile(upstreamPath, opts.UpstreamBody, 0o644); err != nil {
		cleanup()
		return SavedSample{}, fmt.Errorf("write upstream stream: %w", err)
	}

	now := time.Now().UTC()
	capture := opts.Capture
	capture.ResponseBytes = len(opts.UpstreamBody)
	capture.ContainsReferenceMarkers, capture.ReferenceMarkerCount, capture.ContainsFinishedToken, capture.FinishedTokenCount = analyzeBytes(opts.UpstreamBody)

	meta := Meta{
		SampleID:      sampleID,
		CapturedAtUTC: now.Format(time.RFC3339),
		Source:        strings.TrimSpace(opts.Source),
		Request:       opts.Request,
		Capture:       capture,
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		cleanup()
		return SavedSample{}, fmt.Errorf("marshal meta: %w", err)
	}
	metaPath := filepath.Join(tempDir, "meta.json")
	if err := os.WriteFile(metaPath, append(metaBytes, '\n'), 0o644); err != nil {
		cleanup()
		return SavedSample{}, fmt.Errorf("write meta: %w", err)
	}

	if err := os.Rename(tempDir, finalDir); err != nil {
		cleanup()
		return SavedSample{}, fmt.Errorf("promote sample dir: %w", err)
	}

	return SavedSample{
		SampleID:     sampleID,
		Dir:          finalDir,
		MetaPath:     filepath.Join(finalDir, "meta.json"),
		UpstreamPath: filepath.Join(finalDir, "upstream.stream.sse"),
		Meta:         meta,
	}, nil
}

func NormalizeSampleID(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-_.")
	if out == "" {
		return ""
	}
	return out
}

func DefaultSampleID(prefix string) string {
	prefix = NormalizeSampleID(prefix)
	if prefix == "" {
		prefix = "capture"
	}
	return fmt.Sprintf("%s-%s", prefix, time.Now().UTC().Format("20060102T150405Z"))
}

func uniqueSampleID(root, base string) (string, error) {
	if base == "" {
		base = DefaultSampleID("capture")
	}
	candidate := base
	for i := 2; ; i++ {
		finalDir := filepath.Join(root, candidate)
		if _, err := os.Stat(finalDir); err != nil {
			if os.IsNotExist(err) {
				return candidate, nil
			}
			return "", fmt.Errorf("stat sample dir: %w", err)
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

func analyzeBytes(raw []byte) (containsReferenceMarkers bool, referenceMarkerCount int, containsFinishedToken bool, finishedTokenCount int) {
	if len(raw) == 0 {
		return false, 0, false, 0
	}
	text := string(raw)
	referenceMarkerCount = len(referenceMarkerRe.FindAllStringIndex(text, -1))
	containsReferenceMarkers = referenceMarkerCount > 0
	upper := strings.ToUpper(text)
	finishedTokenCount = strings.Count(upper, "FINISHED")
	containsFinishedToken = finishedTokenCount > 0
	return
}
