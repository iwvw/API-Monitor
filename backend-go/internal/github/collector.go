package github

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (s *Service) collectorLoop() {
	ticker := time.NewTicker(10 * time.Second)
	lastFullCollection := time.Time{}
	defer func() {
		ticker.Stop()
		close(s.stopped)
	}()
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			if lastFullCollection.IsZero() || now.Sub(lastFullCollection) >= time.Minute {
				_ = s.collectOnce(ctx)
				lastFullCollection = now
			} else {
				_ = s.monitorActionsOnce(ctx, now)
			}
			cancel()
		}
	}
}

func (s *Service) monitorActionsOnce(ctx context.Context, now time.Time) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	settings, err := loadSettings(ctx, db)
	if err != nil || !settings.Enabled {
		return err
	}
	repositories, err := listRepositories(ctx, db, true)
	if err != nil {
		return err
	}
	for _, repo := range repositories {
		if repo.RateLimitRemaining >= 0 && repo.RateLimitRemaining <= settings.RateLimitLowThreshold {
			continue
		}
		token, err := s.tokenForRepository(ctx, db, repo)
		if err != nil {
			continue
		}
		if !s.shouldPollActions(repo, token != "", now) {
			continue
		}
		s.recordActionPoll(repo.ID, now)
		_ = s.refreshActionsRepositoryWithDB(ctx, db, repo, token, "actions_monitor")
	}
	return nil
}

func (s *Service) shouldPollActions(repo Repository, authenticated bool, now time.Time) bool {
	s.actionPollMu.Lock()
	lastPoll := s.actionLastPoll[repo.ID]
	s.actionPollMu.Unlock()
	active := false
	switch strings.ToLower(repo.LatestActionStatus) {
	case "queued", "in_progress", "requested", "waiting":
		active = true
	}
	interval := time.Minute
	if authenticated {
		if active || !repo.WebhookEnabled {
			interval = 10 * time.Second
		}
	} else if active {
		interval = time.Minute
	} else {
		interval = 5 * time.Minute
	}
	return lastPoll.IsZero() || now.Sub(lastPoll) >= interval
}

func (s *Service) recordActionPoll(repoID int64, at time.Time) {
	s.actionPollMu.Lock()
	defer s.actionPollMu.Unlock()
	s.actionLastPoll[repoID] = at
}

func (s *Service) refreshActionsRepositoryByID(ctx context.Context, id int64, source string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	repo, ok, err := getRepository(ctx, db, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("仓库不存在")
	}
	token, err := s.tokenForRepository(ctx, db, repo)
	if err != nil {
		return err
	}
	return s.refreshActionsRepositoryWithDB(ctx, db, repo, token, source)
}

func (s *Service) refreshActionsRepositoryWithDB(ctx context.Context, db *sql.DB, repo Repository, token, source string) error {
	runs, rate, err := s.client.fetchWorkflowRuns(ctx, token, repo.Owner, repo.Name, 50)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	latestStatus := ""
	latestConclusion := ""
	for i, run := range runs.WorkflowRuns {
		if i == 0 {
			latestStatus = run.Status
			latestConclusion = run.Conclusion
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO github_action_runs (
			repository_id, run_id, workflow_name, display_title, status, conclusion, event, branch,
			commit_sha, commit_message, actor, html_url, run_started_at, created_at, updated_at, collected_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			repo.ID, run.ID, run.Name, run.DisplayTitle, run.Status, run.Conclusion, run.Event, run.Branch, run.SHA, run.HeadCommit.Message, run.Actor.Login, run.HTMLURL, nullEmpty(run.RunStartedAt), nullEmpty(run.CreatedAt), nullEmpty(run.UpdatedAt)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE github_repositories SET latest_action_status = ?, latest_action_conclusion = ?, rate_limit_remaining = ?, rate_limit_reset = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		latestStatus, latestConclusion, rate.Remaining, timeOrNil(rate.Reset), repo.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.publish(map[string]interface{}{
		"kind":          "repository_actions_refresh",
		"repository_id": repo.ID,
		"repository":    repo.FullName,
		"source":        source,
		"updated_at":    nowString(),
	})
	return nil
}

func (s *Service) currentStatus() CollectorStatus {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	return s.status
}

func (s *Service) setStatus(status CollectorStatus) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.status = status
}

func (s *Service) collectOnce(ctx context.Context) error {
	db, err := s.open(ctx)
	if err != nil {
		s.setStatus(CollectorStatus{Running: false, LastTickAt: nowString(), LastError: err.Error()})
		return err
	}
	defer db.Close()
	settings, err := loadSettings(ctx, db)
	if err != nil {
		s.setStatus(CollectorStatus{Running: false, LastTickAt: nowString(), LastError: err.Error()})
		return err
	}
	if !settings.Enabled {
		s.setStatus(CollectorStatus{Running: false, LastTickAt: nowString(), LastError: "collector disabled"})
		return nil
	}
	repos, err := listRepositories(ctx, db, true)
	if err != nil {
		s.setStatus(CollectorStatus{Running: false, LastTickAt: nowString(), LastError: err.Error()})
		return err
	}
	count := 0
	for _, repo := range repos {
		if !shouldCollect(repo) {
			continue
		}
		count++
		if err := s.refreshRepositoryWithDB(ctx, db, repo, "collector", settings); err != nil {
			_, _ = db.ExecContext(ctx, `UPDATE github_repositories SET last_status = 'failed', last_error = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, err.Error(), repo.ID)
		}
	}
	_, _ = cleanupHistory(ctx, db, 0, settings.DefaultRetentionDays)
	s.setStatus(CollectorStatus{Running: true, LastTickAt: nowString(), LastRunCount: count})
	return nil
}

func shouldCollect(repo Repository) bool {
	if !repo.Enabled {
		return false
	}
	if repo.LastCollectedAt == nil || *repo.LastCollectedAt == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, *repo.LastCollectedAt)
	if err != nil {
		if parsed, err := time.Parse("2006-01-02 15:04:05", *repo.LastCollectedAt); err == nil {
			last = parsed
		} else {
			return true
		}
	}
	interval := repo.CollectInterval
	if interval < 60 {
		interval = 900
	}
	return time.Since(last) >= time.Duration(interval)*time.Second
}

func (s *Service) refreshRepositoryByID(ctx context.Context, id int64, source string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	settings, _ := loadSettings(ctx, db)
	repo, ok, err := getRepository(ctx, db, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("仓库不存在")
	}
	return s.refreshRepositoryWithDB(ctx, db, repo, source, settings)
}

func (s *Service) refreshRepositoryWithDB(ctx context.Context, db *sql.DB, repo Repository, source string, settings Settings) error {
	token, err := s.tokenForRepository(ctx, db, repo)
	if err != nil {
		s.emitRepositoryEvent(ctx, db, repo, "token_invalid", "critical", "GitHub Token 不可用", err.Error(), source, map[string]interface{}{"error": err.Error()})
		return err
	}
	tokenAccountLogin := s.tokenAccountLoginForRepository(ctx, db, repo)
	prev := repo
	ghRepo, rate, err := s.client.fetchRepository(ctx, token, repo.Owner, repo.Name)
	if err != nil {
		s.emitRepositoryEvent(ctx, db, repo, "repository_unreachable", "critical", "GitHub 仓库不可访问", err.Error(), source, map[string]interface{}{"error": err.Error()})
		return err
	}
	repo.Stars = ghRepo.StargazersCount
	repo.Forks = ghRepo.ForksCount
	repo.Watchers = ghRepo.WatchersCount
	repo.OpenIssues = ghRepo.OpenIssuesCount
	repo.Private = ghRepo.Private
	repo.OwnedByToken = tokenAccountLogin != "" && strings.EqualFold(tokenAccountLogin, ghRepo.Owner.Login)
	repo.CanOperateActions = ghRepo.Permissions.Admin || ghRepo.Permissions.Maintain || ghRepo.Permissions.Push
	repo.HTMLURL = firstNonEmpty(ghRepo.HTMLURL, repo.HTMLURL)
	repo.Description = ghRepo.Description
	repo.DefaultBranch = ghRepo.DefaultBranch
	repo.Language = ghRepo.Language
	repo.RateLimitRemaining = rate.Remaining
	if !rate.Reset.IsZero() {
		repo.RateLimitReset = rate.Reset.Format(time.RFC3339)
	}

	if pulls, nextRate, err := s.client.fetchPullCount(ctx, token, repo.Owner, repo.Name); err == nil {
		repo.OpenPullRequests = pulls
		rate = mergeRate(rate, nextRate)
	}
	releaseCount := 0
	if latest, nextRate, err := s.client.fetchLatestRelease(ctx, token, repo.Owner, repo.Name); err == nil {
		rate = mergeRate(rate, nextRate)
		if name := firstNonEmpty(asString(latest["tag_name"]), asString(latest["name"])); name != "" {
			repo.LatestRelease = name
			repo.LatestReleaseURL = asString(latest["html_url"])
			releaseCount = 1
		}
	}
	commitCount := 0
	if commits, nextRate, err := s.client.fetchCommits(ctx, token, repo.Owner, repo.Name, time.Now().AddDate(0, 0, -30)); err == nil {
		commitCount = len(commits)
		rate = mergeRate(rate, nextRate)
	}
	contributorCount := 0
	if contributors, nextRate, err := s.client.fetchContributors(ctx, token, repo.Owner, repo.Name); err == nil {
		contributorCount = len(contributors)
		rate = mergeRate(rate, nextRate)
		_, _ = db.ExecContext(ctx, `DELETE FROM github_contributors WHERE repository_id = ?`, repo.ID)
		for _, contributor := range contributors {
			_, _ = db.ExecContext(ctx, `INSERT OR REPLACE INTO github_contributors (repository_id, login, avatar_url, html_url, contributions, collected_at)
				VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`, repo.ID, asString(contributor["login"]), asString(contributor["avatar_url"]), asString(contributor["html_url"]), intFromAny(contributor["contributions"]))
		}
	}
	actionsTotal, actionsSuccess, actionsFailed := 0, 0, 0
	if runs, nextRate, err := s.client.fetchWorkflowRuns(ctx, token, repo.Owner, repo.Name, 50); err == nil {
		rate = mergeRate(rate, nextRate)
		actionsTotal = runs.TotalCount
		for i, run := range runs.WorkflowRuns {
			if run.Conclusion == "success" {
				actionsSuccess++
			}
			if run.Conclusion == "failure" || run.Conclusion == "timed_out" || run.Conclusion == "cancelled" {
				actionsFailed++
			}
			if i == 0 {
				repo.LatestActionStatus = run.Status
				repo.LatestActionConclusion = run.Conclusion
			}
			_, _ = db.ExecContext(ctx, `INSERT OR REPLACE INTO github_action_runs (
				repository_id, run_id, workflow_name, display_title, status, conclusion, event, branch,
				commit_sha, commit_message, actor, html_url, run_started_at, created_at, updated_at, collected_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
				repo.ID, run.ID, run.Name, run.DisplayTitle, run.Status, run.Conclusion, run.Event, run.Branch, run.SHA, run.HeadCommit.Message, run.Actor.Login, run.HTMLURL, nullEmpty(run.RunStartedAt), nullEmpty(run.CreatedAt), nullEmpty(run.UpdatedAt))
		}
	}
	trafficViews, trafficUniques, trafficClones, trafficCloneUniques := 0, 0, 0, 0
	if traffic, nextRate, err := s.client.fetchTraffic(ctx, token, repo.Owner, repo.Name); err == nil {
		rate = mergeRate(rate, nextRate)
		trafficViews = traffic["views"]
		trafficUniques = traffic["view_uniques"]
		trafficClones = traffic["clones"]
		trafficCloneUniques = traffic["clone_uniques"]
		_, _ = db.ExecContext(ctx, `INSERT INTO github_traffic_samples (repository_id, views, view_uniques, clones, clone_uniques)
			VALUES (?, ?, ?, ?, ?)`, repo.ID, trafficViews, trafficUniques, trafficClones, trafficCloneUniques)
	}
	snapshot := Snapshot{
		Stars: repo.Stars, Forks: repo.Forks, Watchers: repo.Watchers, OpenIssues: repo.OpenIssues, OpenPullRequests: repo.OpenPullRequests,
		CommitCount: commitCount, ReleaseCount: releaseCount, ContributorCount: contributorCount, ActionsTotal: actionsTotal,
		ActionsSuccess: actionsSuccess, ActionsFailed: actionsFailed, TrafficViews: trafficViews, TrafficUniques: trafficUniques,
		TrafficClones: trafficClones, TrafficCloneUniques: trafficCloneUniques,
	}
	_ = insertSnapshot(ctx, db, repo.ID, snapshot)
	_, err = db.ExecContext(ctx, `UPDATE github_repositories SET html_url = ?, description = ?, private = ?, owned_by_token = ?, can_operate_actions = ?, default_branch = ?,
		language = ?, last_status = 'success', last_error = '', last_collected_at = CURRENT_TIMESTAMP, stars = ?, forks = ?,
		watchers = ?, open_issues = ?, open_pull_requests = ?, latest_release = ?, latest_release_url = ?,
		latest_action_status = ?, latest_action_conclusion = ?, rate_limit_remaining = ?, rate_limit_reset = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, repo.HTMLURL, repo.Description, boolInt(repo.Private), boolInt(repo.OwnedByToken), boolInt(repo.CanOperateActions), repo.DefaultBranch, repo.Language, repo.Stars, repo.Forks,
		repo.Watchers, repo.OpenIssues, repo.OpenPullRequests, repo.LatestRelease, repo.LatestReleaseURL, repo.LatestActionStatus,
		repo.LatestActionConclusion, rate.Remaining, timeOrNil(rate.Reset), repo.ID)
	if err != nil {
		return err
	}
	s.evaluateEvents(ctx, db, prev, repo, settings, source)
	s.publish(map[string]interface{}{
		"kind":          "repository_refresh",
		"repository_id": repo.ID,
		"repository":    repo.FullName,
		"source":        source,
		"updated_at":    nowString(),
	})
	return nil
}

func (s *Service) evaluateEvents(ctx context.Context, db *sql.DB, prev, current Repository, settings Settings, source string) {
	if current.NotifyEnabled && current.Stars-prev.Stars >= settings.StarSpikeThreshold && prev.Stars > 0 {
		s.emitRepositoryEvent(ctx, db, current, "star_spike", "info", "GitHub Star 激增", fmt.Sprintf("%s 新增 %d 个 star", current.FullName, current.Stars-prev.Stars), source, map[string]interface{}{"previous": prev.Stars, "current": current.Stars, "delta": current.Stars - prev.Stars})
	}
	if current.NotifyEnabled && current.OpenIssues > prev.OpenIssues && prev.OpenIssues > 0 {
		s.emitRepositoryEvent(ctx, db, current, "issue_opened", "info", "GitHub Issue 新增", fmt.Sprintf("%s 新增 %d 个 issue", current.FullName, current.OpenIssues-prev.OpenIssues), source, map[string]interface{}{"previous": prev.OpenIssues, "current": current.OpenIssues, "delta": current.OpenIssues - prev.OpenIssues})
	}
	if current.NotifyEnabled && current.OpenPullRequests > prev.OpenPullRequests && prev.OpenPullRequests > 0 {
		s.emitRepositoryEvent(ctx, db, current, "pull_request_opened", "info", "GitHub PR 新增", fmt.Sprintf("%s 新增 %d 个 PR", current.FullName, current.OpenPullRequests-prev.OpenPullRequests), source, map[string]interface{}{"previous": prev.OpenPullRequests, "current": current.OpenPullRequests, "delta": current.OpenPullRequests - prev.OpenPullRequests})
	}
	if current.NotifyEnabled && current.LatestRelease != "" && prev.LatestRelease != "" && current.LatestRelease != prev.LatestRelease {
		s.emitRepositoryEvent(ctx, db, current, "release_published", "info", "GitHub 新版本发布", fmt.Sprintf("%s 发布 %s", current.FullName, current.LatestRelease), source, map[string]interface{}{"previous": prev.LatestRelease, "current": current.LatestRelease, "htmlUrl": current.LatestReleaseURL})
	}
	if current.NotifyEnabled && current.LatestActionConclusion == "failure" && prev.LatestActionConclusion != "failure" {
		s.emitRepositoryEvent(ctx, db, current, "action_failed", "critical", "GitHub Actions 失败", fmt.Sprintf("%s 最新 workflow 失败", current.FullName), source, map[string]interface{}{"status": current.LatestActionStatus, "conclusion": current.LatestActionConclusion})
	}
	if current.NotifyEnabled && current.LatestActionConclusion == "success" && prev.LatestActionConclusion == "failure" {
		s.emitRepositoryEvent(ctx, db, current, "action_recovered", "info", "GitHub Actions 已恢复", fmt.Sprintf("%s 最新 workflow 已恢复成功", current.FullName), source, map[string]interface{}{"status": current.LatestActionStatus, "conclusion": current.LatestActionConclusion})
	}
	if current.NotifyEnabled && current.RateLimitRemaining >= 0 && current.RateLimitRemaining <= settings.RateLimitLowThreshold {
		s.emitRepositoryEvent(ctx, db, current, "rate_limit_low", "warning", "GitHub Rate Limit 偏低", fmt.Sprintf("%s 剩余 GitHub API 额度 %d", current.FullName, current.RateLimitRemaining), source, map[string]interface{}{"rateLimitRemaining": current.RateLimitRemaining, "rateLimitReset": current.RateLimitReset})
	}
}

func (s *Service) emitRepositoryEvent(ctx context.Context, db *sql.DB, repo Repository, eventType, severity, title, message, source string, payload map[string]interface{}) {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	payload["repositoryId"] = repo.ID
	payload["repositoryFullName"] = repo.FullName
	payload["owner"] = repo.Owner
	payload["repo"] = repo.Name
	payload["htmlUrl"] = repo.HTMLURL
	payload["eventType"] = "github." + eventType
	payload["severity"] = severity
	payload["collectedAt"] = nowString()
	fingerprint := fmt.Sprintf("%d:%s:%s", repo.ID, eventType, strings.TrimSpace(message))
	notified := false
	if repo.NotifyEnabled && s.notifier != nil {
		if err := s.notifier.Trigger(ctx, "github", eventType, payload); err == nil {
			notified = true
		}
	}
	repoID := repo.ID
	_ = insertEvent(ctx, db, &repoID, eventType, severity, title, message, source, payload, fingerprint, notified)
	s.publish(map[string]interface{}{"repository_id": repo.ID, "repository": repo.FullName, "event_type": eventType, "severity": severity, "title": title, "message": message, "payload": payload, "created_at": nowString()})
}

func (s *Service) handleWebhookEvent(ctx context.Context, db *sql.DB, repo Repository, eventType string, payload map[string]interface{}) {
	switch eventType {
	case "workflow_run":
		action := asString(payload["action"])
		run := objectValue(payload["workflow_run"])
		conclusion := asString(run["conclusion"])
		if action == "completed" && conclusion == "failure" {
			s.emitRepositoryEvent(ctx, db, repo, "action_failed", "critical", "GitHub Actions 失败", fmt.Sprintf("%s workflow 失败", repo.FullName), "webhook", run)
		}
		if action == "completed" && conclusion == "success" {
			s.emitRepositoryEvent(ctx, db, repo, "action_recovered", "info", "GitHub Actions 成功", fmt.Sprintf("%s workflow 成功", repo.FullName), "webhook", run)
		}
	case "release":
		if asString(payload["action"]) == "published" {
			s.emitRepositoryEvent(ctx, db, repo, "release_published", "info", "GitHub 新版本发布", fmt.Sprintf("%s 发布新版本", repo.FullName), "webhook", objectValue(payload["release"]))
		}
	case "issues":
		if asString(payload["action"]) == "opened" {
			s.emitRepositoryEvent(ctx, db, repo, "issue_opened", "info", "GitHub Issue 新增", fmt.Sprintf("%s 新增 issue", repo.FullName), "webhook", objectValue(payload["issue"]))
		}
	case "pull_request":
		if asString(payload["action"]) == "opened" {
			s.emitRepositoryEvent(ctx, db, repo, "pull_request_opened", "info", "GitHub PR 新增", fmt.Sprintf("%s 新增 PR", repo.FullName), "webhook", objectValue(payload["pull_request"]))
		}
	case "star":
		if asString(payload["action"]) == "created" {
			s.emitRepositoryEvent(ctx, db, repo, "star_spike", "info", "GitHub Star 新增", fmt.Sprintf("%s 收到新 star", repo.FullName), "webhook", payload)
		}
	case "ping":
		s.emitRepositoryEvent(ctx, db, repo, "webhook_ping", "info", "GitHub Webhook 已连接", fmt.Sprintf("%s webhook ping 成功", repo.FullName), "webhook", payload)
	}
	if webhookUsesActionsRefresh(eventType) {
		s.scheduleWebhookRefresh(repo.ID, s.refreshActionsRepositoryByID, "webhook")
		return
	}
	s.scheduleWebhookRefresh(repo.ID, s.refreshRepositoryByID, "webhook")
}

// scheduleWebhookRefresh 调度 webhook 触发的仓库刷新：同一仓库刷新进行中
// 时不再并发发起（事件风暴去重）；刷新刚结束的 5 秒内到达的事件会被抖动
// 延迟合并为一次补刷，避免每个事件都开一个 goroutine 打爆 GitHub API。
func (s *Service) scheduleWebhookRefresh(repoID int64, refresh func(context.Context, int64, string) error, reason string) {
	const cooldown = 5 * time.Second
	s.webhookRefreshMu.Lock()
	if s.webhookRefreshBusy[repoID] {
		s.webhookRefreshMu.Unlock()
		return
	}
	if timer, ok := s.webhookRefreshTimer[repoID]; ok {
		timer.Reset(cooldown)
		s.webhookRefreshMu.Unlock()
		return
	}
	timer := time.AfterFunc(cooldown, func() {
		s.webhookRefreshMu.Lock()
		if s.webhookRefreshBusy[repoID] {
			s.webhookRefreshMu.Unlock()
			return
		}
		s.webhookRefreshBusy[repoID] = true
		delete(s.webhookRefreshTimer, repoID)
		s.webhookRefreshMu.Unlock()
		refresh(context.Background(), repoID, reason)
		s.webhookRefreshMu.Lock()
		delete(s.webhookRefreshBusy, repoID)
		s.webhookRefreshMu.Unlock()
	})
	s.webhookRefreshTimer[repoID] = timer
	s.webhookRefreshMu.Unlock()
}

func webhookUsesActionsRefresh(eventType string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "workflow_run", "workflow_job":
		return true
	default:
		return false
	}
}

func mergeRate(a, b rateLimitInfo) rateLimitInfo {
	if b.Limit != 0 {
		a.Limit = b.Limit
	}
	if b.Remaining != 0 || a.Remaining == 0 {
		a.Remaining = b.Remaining
	}
	if !b.Reset.IsZero() {
		a.Reset = b.Reset
	}
	return a
}

func (s *Service) subscribe() (<-chan map[string]interface{}, func()) {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	s.streamNext++
	id := s.streamNext
	ch := make(chan map[string]interface{}, 32)
	s.streams[id] = ch
	return ch, func() {
		s.streamMu.Lock()
		defer s.streamMu.Unlock()
		if existing, ok := s.streams[id]; ok {
			delete(s.streams, id)
			close(existing)
		}
	}
}

func (s *Service) publish(event map[string]interface{}) {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	for _, ch := range s.streams {
		select {
		case ch <- event:
		default:
		}
	}
}
