package adminai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

// Mention 是前端 @ 资源菜单选择的结构化引用。传输字段只有 type+id，
// 快照由服务端按 type 对应的列表接口实时拉取并裁剪后注入本轮上下文，
// 语义上保证「引用到的资源数据永远来自实时系统」，而非 prompt 文本猜测。
type Mention struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"` // 展示名（仅落库/历史恢复用，快照拉取不依赖）
}

// MentionSnapshot 是单条引用资源的紧凑快照（裁剪后进入 system prompt）。
type MentionSnapshot struct {
	Type string
	ID   string
	Name string
	Text string
}

const (
	mentionTypeZone    = "zone"
	mentionTypeHost    = "host"
	mentionTypeTask    = "task"
	mentionTypeAccount = "account"
	mentionTypeFlyio   = "flyio"
	mentionTypeKoyeb   = "koyeb"
	mentionTypeNode    = "node"
	mentionTypeChannel = "channel"
)

var mentionTypeAllowed = map[string]bool{
	mentionTypeZone:    true,
	mentionTypeHost:    true,
	mentionTypeTask:    true,
	mentionTypeAccount: true,
	mentionTypeFlyio:   true,
	mentionTypeKoyeb:   true,
	mentionTypeNode:    true,
	mentionTypeChannel: true,
}

// mentionListSources 每类资源的列表数据源：一次拉全量后在内存按 id 过滤，
// 避免「每个引用一次接口调用」的 N+1（zone/flyio 走聚合接口，覆盖所有账号）。
var mentionListSources = map[string]struct {
	Path string
	Key  string
}{
	mentionTypeZone:    {Path: "/api/cloudflare/zones", Key: "zones"},
	mentionTypeHost:    {Path: "/api/server/accounts", Key: "accounts"},
	mentionTypeTask:    {Path: "/api/scheduler/tasks", Key: "tasks"},
	mentionTypeAccount: {Path: "/api/cloudflare/accounts", Key: "accounts"},
	mentionTypeFlyio:   {Path: "/api/flyio/proxy/apps", Key: "apps"},
	mentionTypeKoyeb:   {Path: "/api/koyeb/data", Key: "services"},
	mentionTypeNode:    {Path: "/api/scheduler/nodes", Key: "nodes"},
	mentionTypeChannel: {Path: "/api/notification/channels", Key: "channels"},
}

// mentionTypeLabel 资源类型的中文名（注入块展示用）。
var mentionTypeLabel = map[string]string{
	mentionTypeZone:    "域名",
	mentionTypeHost:    "主机",
	mentionTypeTask:    "定时任务",
	mentionTypeAccount: "Cloudflare 账号",
	mentionTypeFlyio:   "Fly.io 应用",
	mentionTypeKoyeb:   "Koyeb 应用",
	mentionTypeNode:    "调度节点",
	mentionTypeChannel: "通知渠道",
}

const (
	mentionMaxCount    = 10   // 单条消息引用上限
	mentionPerItemCap  = 700  // 单条快照字符上限
	mentionTotalCap    = 3000 // 全部快照注入总字符上限
	mentionListMaxLen  = 4000 // 列表解析时单元素对象参数字符上限（防超大对象撑爆内存）
	mentionFieldCap    = 160  // 单个字段值字符上限（数组/长描述截断）
)

// normalizeMentions 白名单过滤 + 去重（type+id）+ 上限裁剪。
func normalizeMentions(raw []Mention) []Mention {
	if len(raw) == 0 {
		return nil
	}
	out := make([]Mention, 0, len(raw))
	seen := map[string]bool{}
	for _, m := range raw {
		if !mentionTypeAllowed[m.Type] || m.ID == "" {
			continue
		}
		key := m.Type + "\x00" + m.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
		if len(out) >= mentionMaxCount {
			break
		}
	}
	return out
}

// fetchMentionSnapshots 拉取并裁剪引用资源快照。按类型分组、每类型一次列表调用，
// 再按 id 过滤；单条/总量均受字符预算约束；未命中的引用不会报错，由注入块显式
// 标注「引用未找到」，让模型如实告知用户而不虚构。
func (s *Service) fetchMentionSnapshots(ctx context.Context, mentions []Mention) ([]MentionSnapshot, error) {
	if len(mentions) == 0 || s.aiCaller == nil {
		return nil, nil
	}
	byType := map[string][]Mention{}
	for _, m := range mentions {
		byType[m.Type] = append(byType[m.Type], m)
	}
	snaps := make([]MentionSnapshot, 0, len(mentions))
	used := 0
	for t, list := range byType {
		src, ok := mentionListSources[t]
		if !ok {
			continue
		}
		items, err := s.fetchMentionList(ctx, src.Path, src.Key)
		if err != nil {
			snaps = append(snaps, MentionSnapshot{Type: t, ID: "", Name: "", Text: fmt.Sprintf("引用列表拉取失败（%s）", sanitizeToolError(err))})
			continue
		}
		for _, m := range list {
			item := findMentionItem(items, m.ID, m.Type)
			snapshot := MentionSnapshot{Type: t, ID: m.ID}
			if item == nil {
				snapshot.Text = "引用未找到（资源可能已被删除或无权限访问）"
			} else {
				snapshot.Name = pickMentionName(t, item)
				snapshot.Text = buildMentionText(t, snapshot.Name, m.ID, item)
			}
			if used+len(snapshot.Text) > mentionTotalCap && used > 0 {
				break
			}
			used += len(snapshot.Text)
			snaps = append(snaps, snapshot)
		}
	}
	return snaps, nil
}

// fetchMentionList 调用内部列表接口并宽容解析响应里的元素数组。
func (s *Service) fetchMentionList(ctx context.Context, path, key string) ([]map[string]interface{}, error) {
	resp, err := s.aiCaller(ctx, systemmetrics.AICallRequest{Method: "GET", Path: path})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 0 && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	arr := findMentionArray(resp.Body, key)
	if arr == nil {
		return nil, fmt.Errorf("响应中未找到 %s 列表", key)
	}
	items := make([]map[string]interface{}, 0, len(arr))
	for _, v := range arr {
		if obj, ok := v.(map[string]interface{}); ok {
			items = append(items, obj)
		}
	}
	return items, nil
}

// mentionArrayStrongResource 数组元素是否带「强资源标识」（id/_id/appName/channelId）：
// 用于判断扁平资源数组（如 /api/server/accounts 的 data 主机列表），与 mentionArrayIsResource
// 不同——后者把仅带 name 的包裹对象（如 koyeb accounts 元素、flyio account 包裹）也算作资源，
// 会误伤跨元素合并/嵌套穿透。强标识只认真正可作引用 ID 的字段。
func mentionArrayStrongResource(arr []interface{}) bool {
	if len(arr) == 0 {
		return false
	}
	for _, el := range arr {
		m, ok := el.(map[string]interface{})
		if !ok {
			return false
		}
		hasID := false
		for _, k := range []string{"id", "_id", "appName", "channelId"} {
			if v, ok := m[k]; ok && fmt.Sprintf("%v", v) != "" {
				hasID = true
				break
			}
		}
		if !hasID {
			return false
		}
	}
	return true
}

// mentionArrayIsResource 数组元素是否带资源标识（id/_id/appName/name）：
// 区分真正的资源列表与内部数据数组（如主机 info.disk），避免任意子值深入时被截胡。
func mentionArrayIsResource(arr []interface{}) bool {
	if len(arr) == 0 {
		return false
	}
	for _, el := range arr {
		m, ok := el.(map[string]interface{})
		if !ok {
			return false
		}
		hasID := false
		for _, k := range []string{"id", "_id", "appName", "name"} {
			if v, ok := m[k]; ok && fmt.Sprintf("%v", v) != "" {
				hasID = true
				break
			}
		}
		if !hasID {
			return false
		}
	}
	return true
}

// findMentionArray 宽容解析列表响应：优先精确键，其次信封（data/items/list），
// 再次首元素嵌套（数组元素为包裹对象时深入其 keys/嵌套子数组，如
// flyio: data=[{accountId,apps:[...]}] → apps），最后扫描任意数组值。
// 内部数据数组（disk/cpu 等无资源标识）不会被采纳。
func findMentionArray(body interface{}, key string) []interface{} {
	if body == nil {
		return nil
	}
	// 顶层即为扁平资源数组（如 /api/server/accounts 的 data 主机列表，元素带 id）：
	// 直接返回，避免被「任意子值深入」截胡成内部数据数组（如主机 info.gpu 只取到
	// 1 个 GPU）。koyeb 的 accounts 元素仅带 name（无强标识），此处不命中。
	if arr, ok := body.([]interface{}); ok && mentionArrayStrongResource(arr) {
		return arr
	}
	var walk func(v interface{}, depth int) []interface{}
	walk = func(v interface{}, depth int) []interface{} {
		if depth > 4 {
			return nil
		}
		switch val := v.(type) {
		case []interface{}:
			if len(val) == 0 {
				return nil
			}
			first, ok := val[0].(map[string]interface{})
			if !ok {
				return nil // 标量数组不采纳
			}
			// 收集所有元素的嵌套资源数组（flyio data[].apps 多账号、
			// koyeb projects[].services 多项目）：只取首元素的旧逻辑丢数据
			{
				var gathered []interface{}
				hit := false
				for _, el := range val {
					em, ok := el.(map[string]interface{})
					if !ok {
						continue
					}
					for _, k := range append([]string{key}, "data", "items", "list", "results") {
						if k == "" {
							continue
						}
						if arr, ok := em[k].([]interface{}); ok && len(arr) > 0 {
							if _, ok := arr[0].(map[string]interface{}); ok {
								gathered = append(gathered, arr...)
								hit = true
							}
						}
					}
				}
				if hit {
					return gathered
				}
			}
			if key != "" {
				if arr, ok := first[key].([]interface{}); ok && len(arr) > 0 {
					if _, ok := arr[0].(map[string]interface{}); ok {
						return arr
					}
				}
			}
			for _, k := range []string{"data", "items", "list", "results"} {
				if arr, ok := first[k].([]interface{}); ok && len(arr) > 0 {
					if _, ok := arr[0].(map[string]interface{}); ok {
						return arr
					}
				}
			}
			for _, child := range first {
				if arr := walk(child, depth+1); arr != nil {
					return arr
				}
			}
			if mentionArrayIsResource(val) {
				return val // 首元素是叶资源对象：数组本身即列表
			}
			return nil // 内部数据数组（disk/cpu 等）：不采纳
		case map[string]interface{}:
			if key != "" {
				if arr, ok := val[key].([]interface{}); ok && len(arr) > 0 {
					return arr
				}
			}
			for _, k := range []string{"data", "items", "list", "results"} {
				if arr, ok := val[k].([]interface{}); ok && len(arr) > 0 {
					if _, ok := arr[0].(map[string]interface{}); ok {
						// 扁平资源数组（如 /api/server/accounts 的 data 主机列表）直接采纳：
						// 元素带强标识，避免被数组分支「任意子值深入」截胡成内部数据数组
						// （如主机 info.gpu 只取到 1 个 GPU）。flyio 的 data 是 account 包裹对象
						// （仅 accountName/name）、koyeb 的 accounts 元素仅 name，均不带强标识，
						// 仍走下方数组分支做跨元素合并/嵌套穿透。
						if mentionArrayStrongResource(arr) {
							return arr
						}
						// 交给数组分支统一处理：跨元素合并（flyio data[].apps 多账号）与
						// 嵌套穿透（koyeb data[].projects[].services）都集中在那里
						if inner := walk(arr, depth+1); inner != nil {
							return inner
						}
						return arr
					}
					continue // 标量数组不采纳
				}
			}
			for _, child := range val {
				if arr := walk(child, depth+1); arr != nil {
					return arr
				}
			}
		}
		return nil
	}
	return walk(body, 0)
}

// mentionIDKeys 每类资源的标识键：flyio 聚合 App 无 id 字段（appName 即标识），
// channel 可能只有 channelId；zone/host/task/account/node 均为 id。
var mentionIDKeys = map[string][]string{
	mentionTypeZone:    {"id", "_id"},
	mentionTypeHost:    {"id", "_id"},
	mentionTypeTask:    {"id", "_id"},
	mentionTypeAccount: {"id", "_id"},
	mentionTypeFlyio:   {"appName", "name", "id"},
	mentionTypeKoyeb:   {"_id", "id", "name"},
	mentionTypeNode:    {"id", "_id", "name"},
	mentionTypeChannel: {"id", "channelId", "name"},
}

// findMentionItem 按类型候选键匹配 id。
func findMentionItem(items []map[string]interface{}, id, t string) map[string]interface{} {
	keys := mentionIDKeys[t]
	if len(keys) == 0 {
		keys = []string{"id", "_id"}
	}
	for _, it := range items {
		for _, k := range keys {
			if v, ok := it[k]; ok {
				if fmt.Sprintf("%v", v) == id {
					return it
				}
			}
		}
	}
	return nil
}

// pickMentionName 取资源的展示名（不同接口字段不一，宽容取第一命中）。
func pickMentionName(t string, item map[string]interface{}) string {
	var keys []string
	switch t {
	case mentionTypeZone:
		keys = []string{"name", "domain", "hostname"}
	case mentionTypeHost:
		keys = []string{"name", "hostname", "title", "ip", "ip_address"}
	case mentionTypeTask:
		keys = []string{"name", "title", "description"}
	case mentionTypeFlyio:
		keys = []string{"name", "appName", "hostname"}
	case mentionTypeKoyeb:
		keys = []string{"name", "serviceName", "title"}
	case mentionTypeNode:
		keys = []string{"name", "hostname", "title", "ip", "ip_address", "address"}
	default:
		keys = []string{"name", "title"}
	}
	for _, k := range keys {
		if v, ok := item[k]; ok {
			if s := fmt.Sprintf("%v", v); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

// buildMentionText 按字段白名单生成紧凑快照文本（单条受字符预算约束）。
func buildMentionText(t, name, id string, item map[string]interface{}) string {
	// 兼容嵌套账号对象（CF 聚合接口 zone.account = {id, name}，无扁平 account_id 字段）
	if acc, ok := item["account"].(map[string]interface{}); ok && t == mentionTypeZone {
		if _, has := item["account_id"]; !has {
			if v, ok := acc["id"]; ok {
				item["account_id"] = v
			}
		}
		if _, has := item["account_name"]; !has {
			if v, ok := acc["name"]; ok {
				item["account_name"] = v
			}
		}
	}
var fields []string
	switch t {
	case mentionTypeZone:
		fields = []string{"status", "account_id", "account_name", "plan", "paused", "type", "activated_on", "created_on", "modified_on"}
	case mentionTypeHost:
		fields = []string{"ip", "region", "province", "city", "isp", "country", "status", "online", "protocol", "monitor_mode", "agent_version", "last_seen", "uptime", "memory", "disk"}
	case mentionTypeTask:
		fields = []string{"schedule", "enabled", "next_run", "last_run", "type", "node_id", "node_selector", "retry_count", "retry_interval_seconds", "timeout_seconds", "max_concurrency", "description"}
	case mentionTypeAccount:
		fields = []string{"cfAccountId", "type", "plan", "status", "email", "userEmail", "hasToken", "lastUsed"}
	case mentionTypeFlyio:
		fields = []string{"region", "state", "hostname", "version", "org", "orgId", "machineCount", "machines", "autoscale", "healthy", "startedAt"}
	case mentionTypeKoyeb:
		fields = []string{"status", "type", "region", "updatedAt", "messages"}
	case mentionTypeNode:
		fields = []string{"hostname", "enabled", "online", "address", "ip", "last_seen", "version"}
	default:
		fields = []string{"type", "enabled", "status", "chatId", "channelId", "botUsername", "createdAt"}
	}
	parts := make([]string, 0, len(fields)+1)
	for _, k := range fields {
		v, ok := mentionFieldLookup(item, k)
		if !ok {
			continue
		}
		s := ""
		switch val := v.(type) {
		case string:
			s = val
		case bool:
			if val {
				s = "true"
			} else {
				s = "false"
			}
		case float64:
			s = fmt.Sprintf("%g", val)
		case json.Number:
			s = val.String()
		default:
			if b, err := json.Marshal(val); err == nil && len(b) <= mentionListMaxLen {
				s = string(b)
			}
		}
		s = strings.TrimSpace(s)
		if s != "" {
			parts = append(parts, k+"="+truncateContentAt(s, mentionFieldCap))
		}
	}
	text := name + " (id=" + id + ")"
	if len(parts) > 0 {
		text += "：" + strings.Join(parts, ", ")
	}
	if extra := mentionSpecificAPIs(t, id, item); extra != "" {
		text += extra
	}
	return truncateContentAt(text, mentionPerItemCap)
}

// mentionFieldLookup 按点号路径取值（如 plan.name → item["plan"]["name"]），
// 支持嵌套对象字段，扁平缺失时自动从嵌套对象兜底。
func mentionFieldLookup(item map[string]interface{}, key string) (interface{}, bool) {
	for _, k := range []string{key, strings.ReplaceAll(key, ".", "_")} {
		if v, ok := item[k]; ok {
			return v, true
		}
	}
	parts := strings.Split(key, ".")
	if len(parts) > 1 {
		var cur interface{} = item
		for _, p := range parts {
			m, ok := cur.(map[string]interface{})
			if !ok {
				return nil, false
			}
			cur, ok = m[p]
			if !ok {
				return nil, false
			}
		}
		return cur, true
	}
	return nil, false
}

// mentionSpecificAPIs 引用资源的专属明细接口（真实 ID 已填充，模型操作时可零猜测直达，
// 不必再从列表接口逐层定位）。zone 需要 account_id——聚合接口的 zone 响应为嵌套对象，
// 此处同样兼容扁平与嵌套两种形态。
func mentionSpecificAPIs(t, id string, item map[string]interface{}) string {
	switch t {
	case mentionTypeZone:
		accID := flatMentionString(item["account_id"])
		if accID == "" {
			if acc, ok := item["account"].(map[string]interface{}); ok {
				accID = flatMentionString(acc["id"])
			}
		}
		if accID == "" {
			return ""
		}
		return "；专属接口：GET /api/cloudflare/accounts/" + accID + "/zones/" + id + "/records（列出该域名全部 DNS 记录）"
	case mentionTypeHost:
		return "；专属接口：GET /api/server/s/" + id + "（读取该主机详情）"
	case mentionTypeTask:
		return "；专属接口：GET /api/scheduler/tasks/" + id + "（任务详情）、POST /api/scheduler/tasks/" + id + "/run（立即执行）"
	case mentionTypeAccount:
		return "；专属接口：GET /api/cloudflare/accounts/" + id + "/zones（该账号全部域名）"
	case mentionTypeFlyio:
		return "；专属接口：POST /api/flyio/apps/" + id + "/update-image（更新应用镜像）"
	case mentionTypeKoyeb:
		return "；专属接口：POST /api/koyeb/services/" + id + "/update（更新镜像/配置）、POST /api/koyeb/services/" + id + "/redeploy（重新部署）、POST /api/koyeb/services/" + id + "/pause（暂停服务）、GET /api/koyeb/services/" + id + "/deployments（部署历史）、DELETE /api/koyeb/services/" + id + "（删除服务）"
	case mentionTypeNode:
		return "；专属接口：PUT /api/scheduler/nodes/" + id + "（更新节点）"
	case mentionTypeChannel:
		return "；专属接口：PUT /api/notification/channels/" + id + "（更新渠道）、POST /api/notification/channels/" + id + "/test（发送测试消息）"
	}
	return ""
}

// flatMentionString 取对象的字符串标量（兼容 string/float64 等 JSON 数值形态）。
func flatMentionString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%g", val)
	case json.Number:
		return val.String()
	}
	return ""
}

// truncateContentAt 按字符截断（utf8 安全），尾部加省略号。
func truncateContentAt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// buildMentionBlock 快照集合 → system prompt 注入块。
func buildMentionBlock(snaps []MentionSnapshot) string {
	if len(snaps) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## 本次会话引用的资源（实时快照）\n")
	b.WriteString("以下为本轮用户通过 @ 引用的资源及其实时状态快照，回答涉及这些资源时以此为准；标注「引用未找到」的条目表示对应资源当前不存在或不可访问，必须如实告知用户，不得虚构其状态。\n")
	for _, s := range snaps {
		label := mentionTypeLabel[s.Type]
		if label == "" {
			label = s.Type
		}
		head := "「" + s.Name + "」"
		if head == "「」" {
			head = fmt.Sprintf("(id=%s)", s.ID)
		}
		b.WriteString("- " + label + " " + head + "：" + s.Text + "\n")
	}
	return b.String()
}