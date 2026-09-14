package subscription

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func (s *Service) importPreview(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Text      string `json:"text"`
		SourceURL string `json:"source_url"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	text := payload.Text
	if strings.TrimSpace(payload.SourceURL) != "" {
		fetched, _, err := s.fetchManagedSource(r.Context(), payload.SourceURL)
		if err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		text = fetched
	}
	response.OK(w, parseImportText(text))
}

func (s *Service) importCommit(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var payload struct {
		SubscriptionID string `json:"subscription_id"`
		Text           string `json:"text"`
		SourceURL      string `json:"source_url"`
		Nodes          []Node `json:"nodes"`
		Replace        bool   `json:"replace"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	payload.SubscriptionID = firstNonEmpty(strings.TrimSpace(payload.SubscriptionID), defaultNodeLibrary)
	profileID := firstNonEmpty(profileIDForSubscription(r.Context(), db, payload.SubscriptionID), payload.SubscriptionID)
	sourceURL := strings.TrimSpace(payload.SourceURL)
	text := payload.Text
	var userinfo string
	if sourceURL != "" {
		fetched, header, err := s.fetchManagedSource(r.Context(), sourceURL)
		if err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		text = fetched
		userinfo = header
	}
	nodes := payload.Nodes
	if len(nodes) == 0 {
		nodes = parseImportText(text)
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var maxOrder int
	_ = tx.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(sort_order), 0) FROM subscription_nodes WHERE COALESCE(profile_id, subscription_id) = ?`, profileID).Scan(&maxOrder)
	source := "manual"
	if sourceURL != "" {
		source = "managed"
	}
	for i := range nodes {
		nodes[i].SubscriptionID = profileID
		nodes[i].ProfileID = profileID
		nodes[i].Source = source
		nodes[i].SortOrder = maxOrder + i + 1
	}
	if payload.Replace {
		if err := replaceImportedNodes(r.Context(), tx, profileID, source, nodes); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		for i := range nodes {
			if err := insertNode(r.Context(), tx, nodes[i]); err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	if sourceURL != "" {
		refreshHours := defaultRefreshHours
		_ = tx.QueryRowContext(r.Context(), `SELECT COALESCE(default_refresh_hours, 24) FROM subscription_settings WHERE id = 1`).Scan(&refreshHours)
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO subscription_upstreams (
				id, profile_id, name, url, enabled, refresh_hours, status, last_error, last_refresh_at, userinfo, updated_at
			) VALUES (?, ?, '托管源', ?, 1, ?, 'ok', '', datetime('now'), ?, datetime('now'))
			ON CONFLICT(id) DO UPDATE SET
				profile_id = excluded.profile_id,
				name = excluded.name,
				url = excluded.url,
				enabled = 1,
				refresh_hours = excluded.refresh_hours,
				status = 'ok',
				last_error = '',
				last_refresh_at = datetime('now'),
				userinfo = excluded.userinfo,
				updated_at = datetime('now')`, "up_"+profileID, profileID, sourceURL, refreshHours, userinfo); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"imported": len(nodes)})
}

func insertNode(ctx context.Context, tx *sql.Tx, node Node) error {
	if node.ID == "" {
		node.ID = randomID("node")
	}
	if strings.TrimSpace(node.Name) == "" {
		node.Name = firstNonEmpty(node.Server, "未命名节点")
	}
	node.ProfileID = firstNonEmpty(node.ProfileID, node.SubscriptionID)
	rawEnc, err := secure.SecureEncrypt(node.Raw)
	if err != nil {
		return err
	}
	cfgEnc, err := secure.SecureEncrypt(node.ConfigJSON)
	if err != nil {
		return err
	}
	fingerprint := nodeFingerprint(node)
	ownership := normalizeNodeOwnership(node.Ownership)
	if strings.TrimSpace(node.Ownership) == "" && node.TrafficServerID != "" {
		ownership = "self"
	}
	management := normalizeNodeManagement(node.Management, ownership)
	reporting := normalizeNodeTrafficReporting(node.TrafficReporting, management)
	trafficServerID := node.TrafficServerID
	if ownership != "self" {
		trafficServerID = ""
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO subscription_nodes (id, subscription_id, profile_id, name, type, server, port, country_code, location, tags, traffic_server_id, ownership, management, traffic_reporting, enabled, stable, sort_order, raw_encrypted, config_encrypted, fingerprint, source, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		node.ID, node.SubscriptionID, node.ProfileID, node.Name, node.Type, node.Server, node.Port, node.CountryCode, node.Location, node.Tags, nullString(trafficServerID), ownership, management, reporting, boolToInt(node.Enabled || !strings.EqualFold(node.Name, "__disabled__")), boolToInt(node.Stable), node.SortOrder, rawEnc, cfgEnc, fingerprint, firstNonEmpty(node.Source, "manual"))
	return err
}

// replaceImportedNodes reconciles a replace import in place. Plans reference
// node IDs, so deleting every row before re-importing silently disconnects all
// downstream subscriptions. Exact fingerprints are preferred; a unique name
// is the stable fallback when an upstream changes the endpoint or credentials.
func replaceImportedNodes(ctx context.Context, tx *sql.Tx, profileID, source string, incoming []Node) error {
	existing, err := loadReplaceCandidates(ctx, tx, profileID, source)
	if err != nil {
		return err
	}
	byFingerprint := make(map[string]Node, len(existing))
	nameCandidates := make(map[string][]Node, len(existing))
	for _, node := range existing {
		fingerprint := nodeFingerprint(node)
		if fingerprint != "" {
			byFingerprint[fingerprint] = node
		}
		nameKey := normalizedNodeIdentityName(node.Name)
		if nameKey != "" {
			nameCandidates[nameKey] = append(nameCandidates[nameKey], node)
		}
	}

	seenIDs := make(map[string]bool, len(incoming))
	for index := range incoming {
		node := incoming[index]
		fingerprint := nodeFingerprint(node)
		current, matched := byFingerprint[fingerprint]
		if !matched {
			matches := nameCandidates[normalizedNodeIdentityName(node.Name)]
			if len(matches) == 1 {
				current, matched = matches[0], true
			}
		}
		if matched && !seenIDs[current.ID] {
			node.ID = current.ID
			node.Enabled = current.Enabled
			node.Stable = current.Stable
			if err := updateImportedNode(ctx, tx, node, source, fingerprint); err != nil {
				return err
			}
			seenIDs[current.ID] = true
			continue
		}
		if err := insertNode(ctx, tx, node); err != nil {
			return err
		}
		seenIDs[node.ID] = true
	}

	for _, node := range existing {
		if seenIDs[node.ID] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_plan_nodes WHERE node_id=? AND source='external'`, node.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_nodes WHERE id=?`, node.ID); err != nil {
			return err
		}
	}
	return nil
}

func loadReplaceCandidates(ctx context.Context, tx *sql.Tx, profileID, source string) ([]Node, error) {
	sourceClause := "source = 'manual'"
	if source == "managed" {
		sourceClause = "source IN ('managed','upstream')"
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,subscription_id,COALESCE(profile_id,subscription_id),name,
		COALESCE(type,''),COALESCE(server,''),COALESCE(port,0),COALESCE(country_code,''),COALESCE(location,''),
		COALESCE(tags,''),enabled,stable,sort_order,COALESCE(fingerprint,'')
		FROM subscription_nodes WHERE COALESCE(profile_id,subscription_id)=? AND `+sourceClause, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Node{}
	for rows.Next() {
		var node Node
		var enabled, stable int
		var fingerprint string
		if err := rows.Scan(&node.ID, &node.SubscriptionID, &node.ProfileID, &node.Name, &node.Type, &node.Server, &node.Port, &node.CountryCode, &node.Location, &node.Tags, &enabled, &stable, &node.SortOrder, &fingerprint); err != nil {
			return nil, err
		}
		node.Enabled = enabled == 1
		node.Stable = stable == 1
		items = append(items, node)
	}
	return items, rows.Err()
}

func updateImportedNode(ctx context.Context, tx *sql.Tx, node Node, source, fingerprint string) error {
	rawEnc, err := secure.SecureEncrypt(node.Raw)
	if err != nil {
		return err
	}
	cfgEnc, err := secure.SecureEncrypt(node.ConfigJSON)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_nodes SET subscription_id=?,profile_id=?,name=?,type=?,server=?,port=?,country_code=?,location=?,tags=?,
		traffic_server_id=NULL,ownership='external',management='unmanaged',traffic_reporting='unavailable',enabled=?,stable=?,sort_order=?,
		raw_encrypted=?,config_encrypted=?,fingerprint=?,source=?,updated_at=datetime('now') WHERE id=?`,
		node.SubscriptionID, node.ProfileID, node.Name, node.Type, node.Server, node.Port, node.CountryCode, node.Location, node.Tags,
		boolToInt(node.Enabled), boolToInt(node.Stable), node.SortOrder, rawEnc, cfgEnc, fingerprint, source, node.ID)
	return err
}

func mergeManagedNodes(ctx context.Context, tx *sql.Tx, profileID string, incoming []Node) error {
	existing, err := loadManagedNodeFingerprints(ctx, tx, profileID)
	if err != nil {
		return err
	}
	candidates, err := loadReplaceCandidates(ctx, tx, profileID, "managed")
	if err != nil {
		return err
	}
	nameCandidates := make(map[string][]Node, len(candidates))
	for _, candidate := range candidates {
		key := normalizedNodeIdentityName(candidate.Name)
		if key != "" {
			nameCandidates[key] = append(nameCandidates[key], candidate)
		}
	}
	seenIDs := map[string]bool{}
	for i := range incoming {
		node := incoming[i]
		node.SubscriptionID = profileID
		node.ProfileID = profileID
		node.Source = "managed"
		if node.SortOrder == 0 {
			node.SortOrder = i + 1
		}
		fingerprint := nodeFingerprint(node)
		current, ok := existing[fingerprint]
		if !ok {
			matches := nameCandidates[normalizedNodeIdentityName(node.Name)]
			if len(matches) == 1 {
				current, ok = matches[0], true
			}
		}
		if ok && !seenIDs[current.ID] {
			node.ID = current.ID
			node.Name = firstNonEmpty(current.Name, node.Name)
			node.CountryCode = firstNonEmpty(current.CountryCode, node.CountryCode)
			node.Location = firstNonEmpty(current.Location, node.Location)
			node.Tags = firstNonEmpty(current.Tags, node.Tags)
			node.TrafficServerID = current.TrafficServerID
			node.Enabled = current.Enabled
			node.Stable = current.Stable
			node.SortOrder = current.SortOrder
			if err := updateManagedNode(ctx, tx, node, fingerprint); err != nil {
				return err
			}
			seenIDs[node.ID] = true
			continue
		}
		if err := insertNode(ctx, tx, node); err != nil {
			return err
		}
		seenIDs[node.ID] = true
	}
	for _, node := range existing {
		if !seenIDs[node.ID] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_nodes WHERE id = ?`, node.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadManagedNodeFingerprints(ctx context.Context, tx *sql.Tx, profileID string) (map[string]Node, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, subscription_id, COALESCE(profile_id, subscription_id), name, COALESCE(type, ''), COALESCE(server, ''), COALESCE(port, 0), COALESCE(country_code, ''), COALESCE(location, ''), COALESCE(tags, ''), COALESCE(traffic_server_id, ''), enabled, stable, sort_order, COALESCE(fingerprint, '')
		FROM subscription_nodes
		WHERE COALESCE(profile_id, subscription_id) = ? AND source IN ('managed', 'upstream')`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := map[string]Node{}
	for rows.Next() {
		var node Node
		var enabled, stable int
		var fingerprint string
		if err := rows.Scan(&node.ID, &node.SubscriptionID, &node.ProfileID, &node.Name, &node.Type, &node.Server, &node.Port, &node.CountryCode, &node.Location, &node.Tags, &node.TrafficServerID, &enabled, &stable, &node.SortOrder, &fingerprint); err != nil {
			return nil, err
		}
		node.Enabled = enabled == 1
		node.Stable = stable == 1
		stableFingerprint := nodeFingerprint(node)
		if fingerprint != "" {
			nodes[fingerprint] = node
		}
		if stableFingerprint != "" {
			// Keep compatibility with rows created before fingerprints stopped including the name.
			nodes[stableFingerprint] = node
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nodes, nil
}

func updateManagedNode(ctx context.Context, tx *sql.Tx, node Node, fingerprint string) error {
	rawEnc, err := secure.SecureEncrypt(node.Raw)
	if err != nil {
		return err
	}
	cfgEnc, err := secure.SecureEncrypt(node.ConfigJSON)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_nodes SET subscription_id = ?, profile_id = ?, name = ?, type = ?, server = ?, port = ?, country_code = ?, location = ?, tags = ?, traffic_server_id = ?, enabled = ?, stable = ?, sort_order = ?, raw_encrypted = CASE WHEN ? = '' THEN raw_encrypted ELSE ? END, config_encrypted = CASE WHEN ? = '' THEN config_encrypted ELSE ? END, fingerprint = ?, source = 'managed', updated_at = datetime('now') WHERE id = ?`,
		node.SubscriptionID, node.ProfileID, node.Name, node.Type, node.Server, node.Port, node.CountryCode, node.Location, node.Tags, nullString(node.TrafficServerID), boolToInt(node.Enabled), boolToInt(node.Stable), node.SortOrder, node.Raw, rawEnc, node.ConfigJSON, cfgEnc, fingerprint, node.ID)
	return err
}

func normalizedNodeIdentityName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
