package server

import (
	"os"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/usagestats"
)

const usageBackfillMarkerVersion = "2"

// backfillUsageStatsOnce 把聊天历史中已保存的请求一次性回填到按请求的
// Token 用量统计中。回填只执行一次（用带版本的 marker 文件标记），避免
// 重复计数，也不会删除旧版本遗留的 marker。
// 如果聊天历史里没有任何可回填条目，则保留当前实时统计不重置。
func backfillUsageStatsOnce(history *chathistory.Store, stats *usagestats.Store) {
	if history == nil || stats == nil || stats.Path() == "" {
		return
	}
	marker := stats.Path() + ".backfilled.v" + usageBackfillMarkerVersion
	if _, err := os.Stat(marker); err == nil {
		return
	}

	snapshot, err := history.Snapshot()
	if err != nil {
		config.Logger.Warn("[usage_stats] backfill read history failed", "error", err)
		return
	}
	if len(snapshot.Items) == 0 {
		return
	}

	items := []usagestats.BackfillItem{}
	skipped := 0
	for _, summary := range snapshot.Items {
		detail, getErr := history.Get(summary.ID)
		if getErr != nil {
			skipped++
			continue
		}
		prompt, completion, reasoning, total := usagestats.ParseUsage(detail.Usage)
		if total <= 0 {
			skipped++
			continue
		}
		ts := detail.CompletedAt
		if ts <= 0 {
			ts = detail.CreatedAt
		}
		if ts <= 0 {
			skipped++
			continue
		}
		items = append(items, usagestats.BackfillItem{
			Timestamp:  ts,
			Model:      detail.Model,
			CallerID:   detail.CallerID,
			Prompt:     prompt,
			Completion: completion,
			Reasoning:  reasoning,
			Total:      total,
		})
	}

	if len(items) == 0 {
		config.Logger.Warn("[usage_stats] backfill found no usable chat history; marker not written so it can retry", "items", len(snapshot.Items))
		return
	}

	applied, err := stats.Backfill(items)
	if err != nil {
		config.Logger.Warn("[usage_stats] backfill save failed; marker not written so it can retry", "error", err)
		return
	}
	if skipped > 0 {
		config.Logger.Warn("[usage_stats] backfill applied usable entries; some unusable history entries were skipped", "usable", len(items), "skipped", skipped)
	}
	if !applied {
		config.Logger.Info("[usage_stats] backfill skipped because live usage stats already exist; marker will be written to avoid repeated scans")
	}

	if err := writeBackfillMarker(marker); err != nil {
		config.Logger.Warn("[usage_stats] write backfill marker failed", "error", err)
		return
	}
	config.Logger.Info("[usage_stats] backfilled from chat history", "items", len(items), "skipped", skipped)
}

func writeBackfillMarker(path string) error {
	tmp := path + ".tmp"
	raw := []byte(strings.TrimSpace(time.Now().Format(time.RFC3339)))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
