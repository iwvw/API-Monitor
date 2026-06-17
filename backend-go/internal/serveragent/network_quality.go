package serveragent

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

type networkQualityTarget struct {
	ID      int64
	Name    string
	Host    string
	Port    int
	Type    string
	Enabled bool
	Order   int
}

type networkQualitySample struct {
	ID         int64
	ServerID   string
	TargetID   int64
	TargetName string
	TargetHost string
	TargetPort int
	Success    bool
	LatencyMS  float64
	Error      string
	CheckedAt  string
}

func (s *Service) collectNetworkQuality(ctx context.Context, db *sql.DB, serverID string) ([]networkQualitySample, error) {
	targets, err := s.listNetworkQualityTargets(ctx, db)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return []networkQualitySample{}, nil
	}

	samples := make([]networkQualitySample, 0, len(targets))
	for _, target := range targets {
		sampleCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		sample := probeNetworkQualityTarget(sampleCtx, serverID, target)
		cancel()
		if err := s.insertNetworkQualitySample(ctx, db, sample); err != nil {
			return nil, err
		}
		samples = append(samples, sample)
	}
	return samples, nil
}

func (s *Service) listNetworkQualityTargets(ctx context.Context, db *sql.DB) ([]networkQualityTarget, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, host, port, type, enabled, order_index
		FROM server_network_quality_targets
		WHERE enabled = 1
		ORDER BY order_index ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list network quality targets: %w", err)
	}
	defer rows.Close()

	targets := make([]networkQualityTarget, 0)
	for rows.Next() {
		var target networkQualityTarget
		var enabled int
		if err := rows.Scan(&target.ID, &target.Name, &target.Host, &target.Port, &target.Type, &enabled, &target.Order); err != nil {
			return nil, fmt.Errorf("scan network quality target: %w", err)
		}
		target.Enabled = enabled == 1
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate network quality targets: %w", err)
	}
	return targets, nil
}

func probeNetworkQualityTarget(ctx context.Context, serverID string, target networkQualityTarget) networkQualitySample {
	started := time.Now()
	addr := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, strings.ToLower(target.Type), addr)
	latencyMS := float64(time.Since(started).Milliseconds())
	sample := networkQualitySample{
		ServerID:   serverID,
		TargetID:   target.ID,
		TargetName: target.Name,
		TargetHost: target.Host,
		TargetPort: target.Port,
		Success:    err == nil,
		LatencyMS:  latencyMS,
		CheckedAt:  time.Now().Format("2006-01-02 15:04:05"),
	}
	if err != nil {
		sample.Error = err.Error()
		return sample
	}
	_ = conn.Close()
	return sample
}

func (s *Service) insertNetworkQualitySample(ctx context.Context, db *sql.DB, sample networkQualitySample) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO server_network_quality_samples (
			server_id, target_id, target_name, target_host, target_port,
			success, latency_ms, error_message, checked_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		sample.ServerID,
		sample.TargetID,
		sample.TargetName,
		sample.TargetHost,
		sample.TargetPort,
		boolToInt(sample.Success),
		sample.LatencyMS,
		nullStr(sample.Error),
	)
	if err != nil {
		return fmt.Errorf("insert network quality sample: %w", err)
	}
	return nil
}

func (s *Service) buildNetworkQualityPayload(ctx context.Context, db *sql.DB, serverID string, days, maxPoints int) (map[string]interface{}, error) {
	query := `
		SELECT id, server_id, target_id, target_name, target_host, target_port, success, COALESCE(latency_ms, 0), COALESCE(error_message, ''), checked_at
		FROM server_network_quality_samples
		WHERE server_id = ?
		AND checked_at >= datetime('now', '-' || ? || ' days')
		ORDER BY checked_at DESC, id DESC`
	rows, err := db.QueryContext(ctx, query, serverID, days)
	if err != nil {
		return nil, fmt.Errorf("query network quality samples: %w", err)
	}
	defer rows.Close()

	samples := make([]networkQualitySample, 0)
	for rows.Next() {
		var sample networkQualitySample
		var success int
		if err := rows.Scan(
			&sample.ID,
			&sample.ServerID,
			&sample.TargetID,
			&sample.TargetName,
			&sample.TargetHost,
			&sample.TargetPort,
			&success,
			&sample.LatencyMS,
			&sample.Error,
			&sample.CheckedAt,
		); err != nil {
			return nil, fmt.Errorf("scan network quality sample: %w", err)
		}
		sample.Success = success == 1
		samples = append(samples, sample)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate network quality samples: %w", err)
	}

	grouped := map[string][]networkQualitySample{}
	for _, sample := range samples {
		grouped[sample.TargetName] = append(grouped[sample.TargetName], sample)
	}

	series := make([]map[string]interface{}, 0, len(grouped))
	summary := make([]map[string]interface{}, 0, len(grouped))
	updatedAt := ""

	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)

	totalSampleCount := 0
	for _, name := range names {
		targetSamples := grouped[name]
		if len(targetSamples) == 0 {
			continue
		}
		totalSampleCount += len(targetSamples)
		if updatedAt == "" || targetSamples[0].CheckedAt > updatedAt {
			updatedAt = targetSamples[0].CheckedAt
		}

		points := targetSamples
		if maxPoints > 0 && len(points) > maxPoints {
			points = points[:maxPoints]
		}

		data := make([]map[string]interface{}, 0, len(points))
		successCount := 0
		latencyTotal := 0.0
		prevLatency := -1.0
		jitterTotal := 0.0
		jitterCount := 0
		latest := map[string]interface{}{}

		for index := len(points) - 1; index >= 0; index-- {
			point := points[index]
			ts := parseTimeMillis(point.CheckedAt)
			value := point.LatencyMS
			if !point.Success {
				value = 0
			}
			data = append(data, map[string]interface{}{
				"timestamp": ts,
				"value":     value,
			})
		}

		for _, point := range targetSamples {
			if len(latest) == 0 {
				latest = map[string]interface{}{
					"latencyMs": point.LatencyMS,
					"success":   point.Success,
					"sampledAt": point.CheckedAt,
				}
			}
			if point.Success {
				successCount++
				latencyTotal += point.LatencyMS
				if prevLatency >= 0 {
					jitterTotal += absFloat(point.LatencyMS - prevLatency)
					jitterCount++
				}
				prevLatency = point.LatencyMS
			}
		}

		lossRate := 0.0
		if len(targetSamples) > 0 {
			lossRate = (1 - (float64(successCount) / float64(len(targetSamples)))) * 100
		}
		avgLatency := 0.0
		if successCount > 0 {
			avgLatency = latencyTotal / float64(successCount)
		}
		jitterMs := 0.0
		if jitterCount > 0 {
			jitterMs = jitterTotal / float64(jitterCount)
		}

		series = append(series, map[string]interface{}{
			"name": name,
			"data": data,
		})
		summary = append(summary, map[string]interface{}{
			"name":       name,
			"sampleCount": len(targetSamples),
			"avgLatency": avgLatency,
			"lossRate":   lossRate,
			"jitterMs":   jitterMs,
			"latest":     latest,
		})
	}

	return map[string]interface{}{
		"series":      series,
		"summary":     summary,
		"sampleCount": totalSampleCount,
		"updatedAt":   updatedAt,
		"unsupported": false,
	}, nil
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
