package subscription

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) refreshUpstream(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if err := s.refreshUpstreamNow(r.Context(), db, id); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]bool{"refreshed": true})
}

func (s *Service) refreshUpstreamNow(ctx context.Context, db *sql.DB, id string) error {
	profileID := firstNonEmpty(profileIDForSubscription(ctx, db, id), id)
	var upstreamURL string
	err := db.QueryRowContext(ctx, `SELECT COALESCE(url, '') FROM subscription_upstreams WHERE profile_id = ? AND enabled = 1 ORDER BY updated_at DESC LIMIT 1`, profileID).Scan(&upstreamURL)
	if err == sql.ErrNoRows {
		err = db.QueryRowContext(ctx, `SELECT COALESCE(upstream_url, '') FROM subscription_subscriptions WHERE id = ?`, id).Scan(&upstreamURL)
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(upstreamURL) == "" {
		return fmt.Errorf("未配置托管源 URL")
	}
	bodyText, userinfo, err := s.fetchManagedSource(ctx, upstreamURL)
	if err != nil {
		msg := err.Error()
		_, _ = db.ExecContext(ctx, `UPDATE subscription_subscriptions SET upstream_status = 'failed', upstream_last_error = ?, updated_at = datetime('now') WHERE id = ?`, msg, id)
		_, _ = db.ExecContext(ctx, `UPDATE subscription_upstreams SET status = 'failed', last_error = ?, updated_at = datetime('now') WHERE profile_id = ?`, msg, profileID)
		return err
	}
	nodes := parseImportText(bodyText)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := mergeManagedNodes(ctx, tx, profileID, nodes); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_subscriptions SET upstream_status = 'ok', upstream_last_error = '', upstream_last_refresh_at = datetime('now'), upstream_userinfo = ?, updated_at = datetime('now') WHERE id = ?`, userinfo, id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_upstreams SET status = 'ok', last_error = '', last_refresh_at = datetime('now'), userinfo = ?, updated_at = datetime('now') WHERE profile_id = ?`, userinfo, profileID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) fetchManagedSource(ctx context.Context, sourceURL string) (string, string, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return "", "", fmt.Errorf("托管源 URL 不能为空")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "API-Monitor-Subscription/1.0")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("托管源返回 HTTP %d", resp.StatusCode)
	}
	return string(body), resp.Header.Get("Subscription-Userinfo"), nil
}
