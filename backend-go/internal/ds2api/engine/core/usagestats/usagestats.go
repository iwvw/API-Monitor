package usagestats

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry 表示某一天某个模型在某个 API Key 下的累计 Token 用量。
type Entry struct {
	Date             string  `json:"date"`
	Model            string  `json:"model"`
	CallerID         string  `json:"caller_id,omitempty"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	Cost             float64 `json:"cost,omitempty"`
	Calls            int64   `json:"calls"`
}

type BackfillItem struct {
	Timestamp  int64
	Model      string
	CallerID   string
	Prompt     int64
	Completion int64
	Reasoning  int64
	Total      int64
}

type Store struct {
	mu           sync.Mutex
	path         string
	settingsPath string
	entries      map[string]*Entry
	settings     UsageSettings
}

// ErrInvalidUsageSettings marks user-input validation failures in
// UsageSettings/SaveSettings so HTTP handlers can map them to 400.
var ErrInvalidUsageSettings = errors.New("invalid usage settings")

var globalStore *Store

// SetGlobal 设置进程级共享存储。server.NewApp 启动时调用一次。
func SetGlobal(s *Store) {
	globalStore = s
}

func Global() *Store {
	return globalStore
}

// Record 在每次成功请求后写入一条累计记录。即使聊天历史关闭也会生效。
func Record(model, callerID string, usage map[string]any) {
	if globalStore == nil {
		return
	}
	prompt, completion, reasoning, total := ParseUsage(usage)
	globalStore.AddCosted(model, callerID, prompt, completion, reasoning, total)
}

func New(path string) *Store {
	settingsPath := ""
	if path != "" {
		settingsPath = path + ".settings"
	}
	s := &Store{
		path:         path,
		settingsPath: settingsPath,
		entries:      map[string]*Entry{},
		settings:     DefaultSettings(),
	}
	s.load()
	s.loadSettings()
	return s
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) AddCosted(model, callerID string, prompt, completion, reasoning, total int64) {
	if s == nil || total <= 0 {
		return
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "unknown"
	}
	callerID = strings.TrimSpace(callerID)
	if callerID == "" {
		callerID = "unknown"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().In(time.FixedZone("GMT+8", 8*60*60))
	date := now.Format("2006-01-02")
	cost := s.computeCostLocked(model, prompt, completion, now)
	key := date + "\x00" + model + "\x00" + callerID
	entry := s.entries[key]
	if entry == nil {
		entry = &Entry{Date: date, Model: model, CallerID: callerID}
		s.entries[key] = entry
	}
	entry.PromptTokens += prompt
	entry.CompletionTokens += completion
	entry.ReasoningTokens += reasoning
	entry.TotalTokens += total
	entry.Calls++
	entry.Cost += cost
	if err := s.saveLocked(); err != nil {
		log.Printf("[usage_stats] save failed: %v", err)
	}
}

func (s *Store) computeCostLocked(model string, prompt, completion int64, now time.Time) float64 {
	if !s.settings.Enabled {
		return 0
	}
	price, ok := matchModelPrice(s.settings.Models, model)
	if !ok || (price.InputPrice == 0 && price.OutputPrice == 0) {
		if def, defOK := matchModelPrice(DefaultSettings().Models, model); defOK {
			price, ok = def, true
		}
	}
	if !ok {
		return 0
	}
	base := float64(prompt)/1_000_000*price.InputPrice + float64(completion)/1_000_000*price.OutputPrice
	if s.settings.Peak.Enabled && isPeakHour(s.settings.Peak, now) {
		base *= s.settings.Peak.Multiplier
	}
	return base
}

func matchModelPrice(models map[string]ModelPrice, model string) (ModelPrice, bool) {
	lowered := strings.ToLower(strings.TrimSpace(model))
	if price, ok := models[lowered]; ok {
		return price, true
	}

	type candidate struct {
		key    string
		price  ModelPrice
		length int
	}
	var candidates []candidate
	for key, price := range models {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "" {
			continue
		}
		if strings.HasPrefix(lowered, normalized+"-") ||
			strings.HasSuffix(lowered, "-"+normalized) ||
			strings.Contains(lowered, "-"+normalized+"-") {
			candidates = append(candidates, candidate{key: key, price: price, length: len(normalized)})
		}
	}
	if len(candidates) == 0 {
		return ModelPrice{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].length != candidates[j].length {
			return candidates[i].length > candidates[j].length
		}
		return candidates[i].key < candidates[j].key
	})
	return candidates[0].price, true
}

func isPeakHour(peak PeakSettings, now time.Time) bool {
	weekday := now.Weekday()
	if (weekday == time.Saturday || weekday == time.Sunday) && peak.WeekendNormal {
		return false
	}
	minutes := now.Hour()*60 + now.Minute()
	return timeRangeContains(peak.Start1, peak.End1, minutes) || timeRangeContains(peak.Start2, peak.End2, minutes)
}

func timeRangeContains(start, end string, minutes int) bool {
	var startHour, startMinute, endHour, endMinute int
	if _, err := fmt.Sscanf(start, "%d:%d", &startHour, &startMinute); err != nil {
		return false
	}
	if _, err := fmt.Sscanf(end, "%d:%d", &endHour, &endMinute); err != nil {
		return false
	}
	startMinutes := startHour*60 + startMinute
	endMinutes := endHour*60 + endMinute
	return minutes >= startMinutes && minutes < endMinutes
}

func (s *Store) Backfill(items []BackfillItem) (bool, error) {
	if s == nil {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.entries) > 0 {
		return false, nil
	}

	rebuilt := map[string]*Entry{}
	gmt8 := time.FixedZone("GMT+8", 8*60*60)
	for _, item := range items {
		if item.Total <= 0 {
			continue
		}
		model := strings.TrimSpace(item.Model)
		if model == "" {
			model = "unknown"
		}
		callerID := strings.TrimSpace(item.CallerID)
		if callerID == "" {
			callerID = "unknown"
		}
		ts := item.Timestamp
		if ts <= 0 {
			ts = time.Now().UnixMilli()
		}
		now := time.UnixMilli(ts).In(gmt8)
		date := now.Format("2006-01-02")
		cost := s.computeCostLocked(model, item.Prompt, item.Completion, now)
		key := date + "\x00" + model + "\x00" + callerID
		entry := rebuilt[key]
		if entry == nil {
			entry = &Entry{Date: date, Model: model, CallerID: callerID}
			rebuilt[key] = entry
		}
		entry.PromptTokens += item.Prompt
		entry.CompletionTokens += item.Completion
		entry.ReasoningTokens += item.Reasoning
		entry.TotalTokens += item.Total
		entry.Calls++
		entry.Cost += cost
	}
	s.entries = rebuilt
	if err := s.saveLocked(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) Clear() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = map[string]*Entry{}
	return s.saveLocked()
}

func (s *Store) Summary() []Entry {
	if s == nil {
		return []Entry{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Entry, 0, len(s.entries))
	changed := false
	for _, entry := range s.entries {
		if entry.Cost == 0 && s.settings.Enabled {
			// Daily aggregation has no hour/minute information, so lazy
			// repricing intentionally uses base prices only. Peak multiplier
			// is applied at request time in computeCostLocked.
			price, ok := matchModelPrice(s.settings.Models, entry.Model)
			if !ok || (price.InputPrice == 0 && price.OutputPrice == 0) {
				if def, defOK := matchModelPrice(DefaultSettings().Models, entry.Model); defOK {
					price, ok = def, true
				}
			}
			if ok && (price.InputPrice > 0 || price.OutputPrice > 0) {
				entry.Cost = float64(entry.PromptTokens)/1_000_000*price.InputPrice + float64(entry.CompletionTokens)/1_000_000*price.OutputPrice
				changed = true
			}
		}
		out = append(out, *entry)
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			log.Printf("[usage_stats] summary save failed: %v", err)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date > out[j].Date
		}
		if out[i].Model != out[j].Model {
			return out[i].Model < out[j].Model
		}
		return out[i].CallerID < out[j].CallerID
	})
	return out
}

func (s *Store) Settings() UsageSettings {
	if s == nil {
		return DefaultSettings()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.settings
	out.Models = make(map[string]ModelPrice, len(s.settings.Models))
	for key, price := range s.settings.Models {
		out.Models[key] = price
	}
	return out
}

func (s *Store) SaveSettings(settings UsageSettings) error {
	if s == nil {
		return nil
	}
	settings.Enabled = true
	if err := validatePeakSettings(settings.Peak); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = normalizeSettings(settings)
	return s.saveSettingsLocked()
}

func validatePeakSettings(peak PeakSettings) error {
	ranges := []struct {
		start string
		end   string
		label string
	}{
		{peak.Start1, peak.End1, "peak window 1"},
		{peak.Start2, peak.End2, "peak window 2"},
	}
	for _, r := range ranges {
		if r.start == "" && r.end == "" {
			continue
		}
		if r.start == "" || r.end == "" {
			return fmt.Errorf("%w: %s must have both start and end times", ErrInvalidUsageSettings, r.label)
		}
		startMinutes, err := clockMinutes(r.start)
		if err != nil {
			return fmt.Errorf("%w: %s start %q is invalid", ErrInvalidUsageSettings, r.label, r.start)
		}
		endMinutes, err := clockMinutes(r.end)
		if err != nil {
			return fmt.Errorf("%w: %s end %q is invalid", ErrInvalidUsageSettings, r.label, r.end)
		}
		if startMinutes >= endMinutes {
			return fmt.Errorf("%w: %s start must be before end", ErrInvalidUsageSettings, r.label)
		}
	}
	if peak.Multiplier < 0 {
		return fmt.Errorf("%w: peak multiplier must be >= 0", ErrInvalidUsageSettings)
	}
	return nil
}

func clockMinutes(value string) (int, error) {
	var hour, minute int
	if _, err := fmt.Sscanf(value, "%d:%d", &hour, &minute); err != nil {
		return 0, err
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("invalid clock value")
	}
	return hour*60 + minute, nil
}

func normalizeSettings(settings UsageSettings) UsageSettings {
	defaults := DefaultSettings()
	if settings.Models == nil {
		settings.Models = map[string]ModelPrice{}
	}
	for key, def := range defaults.Models {
		p, ok := settings.Models[key]
		if !ok {
			settings.Models[key] = def
			continue
		}
		if p.InputPrice <= 0 {
			p.InputPrice = def.InputPrice
		}
		if p.OutputPrice <= 0 {
			p.OutputPrice = def.OutputPrice
		}
		settings.Models[key] = p
	}
	if settings.Peak.Enabled {
		if settings.Peak.Start1 == "" {
			settings.Peak.Start1 = defaults.Peak.Start1
		}
		if settings.Peak.End1 == "" {
			settings.Peak.End1 = defaults.Peak.End1
		}
		if settings.Peak.Start2 == "" {
			settings.Peak.Start2 = defaults.Peak.Start2
		}
		if settings.Peak.End2 == "" {
			settings.Peak.End2 = defaults.Peak.End2
		}
		if settings.Peak.Multiplier <= 0 {
			settings.Peak.Multiplier = defaults.Peak.Multiplier
		}
	}
	return settings
}

func (s *Store) loadSettings() {
	if s == nil || s.settingsPath == "" {
		return
	}
	raw, err := os.ReadFile(s.settingsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[usage_stats] settings load failed: %v", err)
		}
		return
	}
	var settings UsageSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		log.Printf("[usage_stats] settings parse failed: %v", err)
		return
	}
	settings.Enabled = true
	settings = normalizeSettings(settings)
	if err := validatePeakSettings(settings.Peak); err != nil {
		log.Printf("[usage_stats] settings peak invalid, using defaults: %v", err)
		settings.Peak = DefaultSettings().Peak
	}
	s.mu.Lock()
	s.settings = settings
	if err := s.saveSettingsLocked(); err != nil {
		log.Printf("[usage_stats] settings normalize/write failed: %v", err)
	}
	s.mu.Unlock()
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		if cerr := f.Close(); cerr != nil {
			log.Printf("[usage_stats] close tmp after write error: %v", cerr)
		}
		if rerr := os.Remove(tmp); rerr != nil {
			log.Printf("[usage_stats] remove tmp after write error: %v", rerr)
		}
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		if cerr := f.Close(); cerr != nil {
			log.Printf("[usage_stats] close tmp after sync error: %v", cerr)
		}
		if rerr := os.Remove(tmp); rerr != nil {
			log.Printf("[usage_stats] remove tmp after sync error: %v", rerr)
		}
		return fmt.Errorf("sync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		if rerr := os.Remove(tmp); rerr != nil {
			log.Printf("[usage_stats] remove tmp after close error: %v", rerr)
		}
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		if rerr := os.Remove(tmp); rerr != nil {
			log.Printf("[usage_stats] remove tmp after rename error: %v", rerr)
		}
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func (s *Store) saveSettingsLocked() error {
	if s == nil || s.settingsPath == "" {
		return nil
	}
	raw, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		return fmt.Errorf("settings marshal: %w", err)
	}
	return writeFileAtomic(s.settingsPath, raw)
}

func ParseUsage(usage map[string]any) (prompt, completion, reasoning, total int64) {
	if usage == nil {
		return 0, 0, 0, 0
	}
	prompt = firstInt(usage, "prompt_tokens", "input_tokens")
	completion = firstInt(usage, "completion_tokens", "output_tokens")
	reasoning = firstInt(usage, "reasoning_tokens")

	if details, ok := usage["completion_tokens_details"].(map[string]any); ok {
		reasoning = firstInt(details, "reasoning_tokens")
		if reasoning == 0 {
			reasoning = firstInt(usage, "reasoning_tokens")
		}
	}

	total = firstInt(usage, "total_tokens")
	if total == 0 {
		total = prompt + completion
	}
	if total < prompt+completion {
		total = prompt + completion
	}
	return prompt, completion, reasoning, total
}

func firstInt(source map[string]any, keys ...string) int64 {
	for _, key := range keys {
		value, ok := source[key]
		if !ok || value == nil {
			continue
		}
		if n := toInt64(value); n != 0 {
			return n
		}
	}
	return 0
}

func toInt64(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		var n int64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err == nil {
			return n
		}
	}
	return 0
}

func (s *Store) load() {
	if s == nil || s.path == "" {
		return
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[usage_stats] load failed: %v", err)
		}
		return
	}
	var list []Entry
	if err := json.Unmarshal(raw, &list); err != nil {
		log.Printf("[usage_stats] parse failed: %v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range list {
		model := entry.Model
		if model == "" {
			model = "unknown"
		}
		callerID := entry.CallerID
		if callerID == "" {
			callerID = "unknown"
		}
		key := entry.Date + "\x00" + model + "\x00" + callerID
		if existing := s.entries[key]; existing != nil {
			existing.PromptTokens += entry.PromptTokens
			existing.CompletionTokens += entry.CompletionTokens
			existing.ReasoningTokens += entry.ReasoningTokens
			existing.TotalTokens += entry.TotalTokens
			existing.Cost += entry.Cost
			existing.Calls += entry.Calls
			continue
		}
		copied := entry
		copied.Model = model
		copied.CallerID = callerID
		s.entries[key] = &copied
	}
}

func (s *Store) saveLocked() error {
	if s == nil || s.path == "" {
		return nil
	}
	list := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		list = append(list, *entry)
	}
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal usage stats: %w", err)
	}
	return writeFileAtomic(s.path, raw)
}
