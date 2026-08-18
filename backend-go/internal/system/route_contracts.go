package system

import (
	"reflect"
	"strings"
)

// routeRequestContracts 登记每个写操作接口的请求契约。
// 键为路由前缀（含 {param} 占位符）；值可以是结构体实例（反射生成 schema），
// 也可以是 map[string]interface{}（手写的 JSON Schema）。
var routeRequestContracts = map[string]interface{}{}

// requestContractFor 返回某路由前缀对应的请求契约（schema + 示例）。
func requestContractFor(prefix string) (map[string]interface{}, interface{}, bool) {
	raw, ok := routeRequestContracts[prefix]
	if !ok {
		return nil, nil, false
	}
	if schema, isMap := raw.(map[string]interface{}); isMap {
		return schema, nil, true
	}
	schema, example := schemaFromValue(raw)
	return schema, example, true
}

// prop 描述一个请求字段，用于手写契约。
type prop struct {
	t   string // type：string / integer / number / boolean / array / object
	req bool   // 是否必填
	e   []string
	d   string // 说明
}

// obj 生成一个 object 类型的 JSON Schema。
func obj(req []string, props map[string]prop) map[string]interface{} {
	propMap := map[string]interface{}{}
	for name, spec := range props {
		field := map[string]interface{}{"type": spec.t}
		if spec.e != nil {
			field["enum"] = spec.e
		}
		if spec.d != "" {
			field["description"] = spec.d
		}
		propMap[name] = field
	}
	schema := map[string]interface{}{"type": "object", "properties": propMap}
	if len(req) > 0 {
		schema["required"] = req
	}
	return schema
}

// schemaFromValue 将结构体实例反射为 JSON Schema 与示例。
func schemaFromValue(v interface{}) (map[string]interface{}, interface{}) {
	rt := reflect.TypeOf(v)
	for rt != nil && rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	return reflectSchema(rt)
}

func reflectSchema(t reflect.Type) (map[string]interface{}, interface{}) {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == nil {
		return map[string]interface{}{}, nil
	}
	switch t.Kind() {
	case reflect.Struct:
		props := map[string]interface{}{}
		required := []string{}
		example := map[string]interface{}{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name, opts := jsonFieldName(field)
			if name == "" {
				continue
			}
			fieldSchema, fieldExample := reflectSchema(field.Type)
			props[name] = fieldSchema
			example[name] = fieldExample
			if !containsOption(opts, "omitempty") {
				required = append(required, name)
			}
		}
		schema := map[string]interface{}{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema, example
	case reflect.Slice, reflect.Array:
		items, _ := reflectSchema(t.Elem())
		return map[string]interface{}{"type": "array", "items": items}, []interface{}{}
	case reflect.Map:
		return map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{}}, map[string]interface{}{}
	case reflect.String:
		return map[string]interface{}{"type": "string"}, ""
	case reflect.Bool:
		return map[string]interface{}{"type": "boolean"}, false
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]interface{}{"type": "integer"}, 0
	case reflect.Float32, reflect.Float64:
		return map[string]interface{}{"type": "number"}, 0.0
	default:
		return map[string]interface{}{}, nil
	}
}

func jsonFieldName(field reflect.StructField) (string, []string) {
	tag := field.Tag.Get("json")
	if tag == "" {
		return "", nil
	}
	parts := strings.Split(tag, ",")
	if len(parts) == 0 || parts[0] == "" {
		return "", nil
	}
	return parts[0], parts[1:]
}

func containsOption(opts []string, target string) bool {
	for _, opt := range opts {
		if strings.TrimSpace(opt) == target {
			return true
		}
	}
	return false
}

func init() {
	// ===== 系统内：AI 接入 / API 密钥 / 备份 =====
	routeRequestContracts["/api/ai-access/mcp-servers"] = obj([]string{"name"}, map[string]prop{
		"name":        {t: "string", req: true, d: "服务名称"},
		"transport":   {t: "string", e: []string{"stdio", "http", "sse"}, d: "传输方式"},
		"command":     {t: "string", d: "stdio 启动命令"},
		"url":         {t: "string", d: "HTTP/SSE 地址"},
		"description": {t: "string"},
		"enabled":     {t: "boolean"},
		"envJson":     {t: "string", d: "环境变量 JSON"},
	})
	routeRequestContracts["/api/ai-access/mcp-servers/{id}"] = routeRequestContracts["/api/ai-access/mcp-servers"]
	routeRequestContracts["/api/system/ai-access/mcp-servers"] = routeRequestContracts["/api/ai-access/mcp-servers"]
	routeRequestContracts["/api/system/ai-access/mcp-servers/{id}"] = routeRequestContracts["/api/ai-access/mcp-servers"]
	routeRequestContracts["/api/ai-access/skills"] = obj([]string{"name"}, map[string]prop{
		"name":        {t: "string", req: true, d: "Skill 名称"},
		"description": {t: "string"},
		"entrypoint":  {t: "string", d: "入口路径或命令"},
		"version":     {t: "string"},
		"enabled":     {t: "boolean"},
		"permissions": {t: "array", d: "权限列表"},
	})
	routeRequestContracts["/api/ai-access/skills/{id}"] = routeRequestContracts["/api/ai-access/skills"]
	routeRequestContracts["/api/system/ai-access/skills"] = routeRequestContracts["/api/ai-access/skills"]
	routeRequestContracts["/api/system/ai-access/skills/{id}"] = routeRequestContracts["/api/ai-access/skills"]
	routeRequestContracts["/api/ai-access/write"] = obj([]string{"writeEnabled"}, map[string]prop{"writeEnabled": {t: "boolean", req: true, d: "是否允许 AI 写操作"}})
	routeRequestContracts["/api/system/ai-access/write"] = routeRequestContracts["/api/ai-access/write"]
	routeRequestContracts["/api/ai-access/policy"] = obj([]string{"policy"}, map[string]prop{"policy": {t: "string", req: true, e: []string{"minimal", "standard", "full"}, d: "AI 接入权限模式"}})
	routeRequestContracts["/api/system/ai-access/policy"] = routeRequestContracts["/api/ai-access/policy"]
	routeRequestContracts["/api/api-keys"] = obj([]string{"name"}, map[string]prop{
		"name":      {t: "string", req: true, d: "密钥名称"},
		"kind":      {t: "string", d: "用途类型"},
		"scopes":    {t: "array", d: "权限范围"},
		"expiresAt": {t: "string", d: "过期时间 RFC3339"},
		"enabled":   {t: "boolean"},
	})
	routeRequestContracts["/api/system/api-keys"] = routeRequestContracts["/api/api-keys"]
	routeRequestContracts["/api/system/api-keys/{id}"] = routeRequestContracts["/api/api-keys"]
	routeRequestContracts["/api/system/api-stats"] = obj(nil, map[string]prop{
		"days": {t: "integer", d: "统计天数（1-90，默认 14）"},
	})
	routeRequestContracts["/api/backup/configs"] = obj([]string{"provider"}, map[string]prop{
		"provider":          {t: "string", req: true, e: []string{"local", "oss", "cos", "s3"}},
		"local_dir":         {t: "string", d: "本地备份目录"},
		"cron":              {t: "string", d: "定时表达式"},
		"endpoint":          {t: "string", d: "云存储端点"},
		"bucket":            {t: "string", d: "存储桶"},
		"access_key_id":     {t: "string"},
		"access_key_secret": {t: "string"},
	})
	routeRequestContracts["/api/backup/restore"] = obj([]string{"id", "confirm"}, map[string]prop{
		"id":      {t: "string", req: true},
		"confirm": {t: "string", req: true, e: []string{"RESTORE"}},
	})

	// ===== PaaS：Fly.io =====
	routeRequestContracts["/api/flyio/apps"] = obj([]string{"accountId", "name"}, map[string]prop{
		"accountId": {t: "string", req: true, d: "Fly.io 账号 ID"},
		"name":      {t: "string", req: true, d: "应用名"},
		"region":    {t: "string", d: "部署区域，如 nrt"},
		"image":     {t: "string", d: "镜像，如 iwvw/api-monitor:dev"},
	})
	routeRequestContracts["/api/flyio/apps/{appName}/update-image"] = obj([]string{"accountId", "image"}, map[string]prop{
		"accountId": {t: "string", req: true, d: "Fly.io 账号 ID"},
		"image":     {t: "string", req: true, d: "镜像引用，如 iwvw/api-monitor:dev"},
	})
	routeRequestContracts["/api/flyio/apps/{appName}/redeploy"] = obj([]string{"accountId"}, map[string]prop{
		"accountId": {t: "string", req: true, d: "Fly.io 账号 ID"},
	})
	routeRequestContracts["/api/flyio/apps/{appName}/rename"] = obj([]string{"accountId", "name"}, map[string]prop{
		"accountId": {t: "string", req: true},
		"name":      {t: "string", req: true, d: "新名称"},
	})
	routeRequestContracts["/api/flyio/accounts/{id}/update-all-images"] = obj([]string{"image"}, map[string]prop{
		"image": {t: "string", req: true, d: "镜像引用，如 iwvw/api-monitor:dev"},
	})

	// ===== OpenAI 网关 =====
	routeRequestContracts["/api/openai/endpoints"] = obj([]string{"name", "baseUrl", "apiKey"}, map[string]prop{
		"name":         {t: "string", req: true},
		"baseUrl":      {t: "string", req: true, d: "OpenAI 兼容地址，如 https://api.example.com/v1"},
		"apiKey":       {t: "string", req: true},
		"headers":      {t: "array", d: "自定义请求头 [{name,value}]"},
		"proxyPool":    {t: "array", d: "代理池"},
		"proxyBatches": {t: "array", d: "按文件导入的代理批次 [{id,name,createdAt,proxies:[...]}]"},
		"autoSwitch":   {t: "boolean", d: "失败自动切换"},
		"protocol":     {t: "string", d: "连接协议 auto/http1/h2，默认 auto（HTTP/2 优先）"},
		"skipVerify":   {t: "boolean"},
	})
	routeRequestContracts["/api/openai/endpoints/{id}"] = routeRequestContracts["/api/openai/endpoints"]
	routeRequestContracts["/api/openai/sessions"] = obj(nil, map[string]prop{
		"title":   {t: "string"},
		"modelId": {t: "string"},
	})

	// ===== 通知中心 =====
	routeRequestContracts["/api/notification/channels"] = obj([]string{"name", "type"}, map[string]prop{
		"name":   {t: "string", req: true},
		"type":   {t: "string", req: true, d: "渠道类型"},
		"config": {t: "object", d: "渠道配置，如 telegram bot token / chat_id"},
	})
	routeRequestContracts["/api/notification/channels/{id}"] = routeRequestContracts["/api/notification/channels"]
	routeRequestContracts["/api/notification/rules"] = obj([]string{"name"}, map[string]prop{
		"name":             {t: "string", req: true},
		"source_module":    {t: "string"},
		"event_type":       {t: "string"},
		"severity":         {t: "string"},
		"channels":         {t: "array", d: "渠道 ID 列表"},
		"conditions":       {t: "object"},
		"suppression":      {t: "object"},
		"time_window":      {t: "object"},
		"title_template":   {t: "string"},
		"message_template": {t: "string"},
	})
	routeRequestContracts["/api/notification/rules/{id}"] = routeRequestContracts["/api/notification/rules"]

	// ===== 定时任务 =====
	routeRequestContracts["/api/scheduler/tasks"] = obj([]string{"name", "command"}, map[string]prop{
		"name":                 {t: "string", req: true},
		"description":          {t: "string"},
		"schedule":             {t: "string", d: "cron 表达式"},
		"command":              {t: "string", req: true},
		"type":                 {t: "string", d: "shell | internal | agent | http | ai"},
		"enabled":              {t: "boolean", d: "是否启用（兼容 0/1 整数）"},
		"timeout_seconds":      {t: "integer"},
		"retry_count":          {t: "integer"},
		"retry_interval_seconds": {t: "integer"},
		"max_concurrency":      {t: "integer"},
		"node_id":              {t: "string"},
		"node_selector":        {t: "string"},
		"config":               {t: "string", d: "任务扩展配置 JSON（AI 任务为 {\"model\",\"policy\",\"channelId\"}，policy 仅支持 allow/readonly，默认 allow）"},
	})
	routeRequestContracts["/api/scheduler/tasks/{id}"] = routeRequestContracts["/api/scheduler/tasks"]
	routeRequestContracts["/api/cron/tasks"] = routeRequestContracts["/api/scheduler/tasks"]
	routeRequestContracts["/api/cron/tasks/{id}"] = routeRequestContracts["/api/scheduler/tasks"]

	// ===== 双因子认证 =====
	routeRequestContracts["/api/totp/accounts"] = obj([]string{"account", "secret"}, map[string]prop{
		"account":   {t: "string", req: true, d: "账号标识"},
		"secret":    {t: "string", req: true, d: "Base32 密钥"},
		"issuer":    {t: "string", d: "发行方"},
		"otp_type":  {t: "string", e: []string{"totp", "hotp"}},
		"algorithm": {t: "string", e: []string{"SHA1", "SHA256", "SHA512"}},
		"digits":    {t: "integer"},
		"period":    {t: "integer"},
		"counter":   {t: "integer", d: "HOTP 计数"},
		"group_id":  {t: "string"},
		"icon":      {t: "string"},
		"color":     {t: "string"},
	})
	routeRequestContracts["/api/totp/accounts/{id}"] = routeRequestContracts["/api/totp/accounts"]
	routeRequestContracts["/api/totp/groups"] = obj([]string{"name"}, map[string]prop{
		"name":  {t: "string", req: true},
		"icon":  {t: "string"},
		"color": {t: "string"},
	})
	routeRequestContracts["/api/totp/groups/{id}"] = routeRequestContracts["/api/totp/groups"]

	// ===== 可用性监测 =====
	routeRequestContracts["/api/uptime/monitors"] = obj([]string{"name"}, map[string]prop{
		"name":     {t: "string", req: true},
		"url":      {t: "string"},
		"type":     {t: "string"},
		"interval": {t: "integer", d: "监测间隔秒"},
	})
	routeRequestContracts["/api/uptime/monitors/{id}"] = routeRequestContracts["/api/uptime/monitors"]

	// ===== Cloudflare =====
	routeRequestContracts["/api/cloudflare/accounts"] = obj([]string{"name", "token"}, map[string]prop{
		"name":  {t: "string", req: true},
		"token": {t: "string", req: true, d: "Cloudflare API Token"},
	})
	routeRequestContracts["/api/cloudflare/accounts/{id}"] = obj([]string{"name"}, map[string]prop{
		"name":  {t: "string", req: true},
		"token": {t: "string"},
	})
	routeRequestContracts["/api/cloudflare/accounts/{id}/zones"] = obj([]string{"name"}, map[string]prop{
		"name":     {t: "string", req: true, d: "域名"},
		"zoneType": {t: "string", e: []string{"full", "partial"}},
	})
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records"] = obj([]string{"type", "name", "content"}, map[string]prop{
		"type":     {t: "string", req: true, e: []string{"A", "AAAA", "CNAME", "TXT", "MX", "SRV", "CAA"}},
		"name":     {t: "string", req: true},
		"content":  {t: "string", req: true},
		"ttl":      {t: "integer"},
		"proxied":  {t: "boolean"},
		"priority": {t: "integer"},
	})
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records/{recordId}"] = routeRequestContracts["/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records"]
	routeRequestContracts["/api/cloudflare/accounts/{id}/tunnels"] = obj([]string{"name"}, map[string]prop{
		"name": {t: "string", req: true},
	})
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/configuration"] = obj(nil, map[string]prop{
		"config": {t: "object", d: "隧道配置（ingress 规则等）"},
	})
	routeRequestContracts["/api/cloudflare/accounts/{id}/workers/{scriptName}"] = obj([]string{"code"}, map[string]prop{
		"code": {t: "string", req: true, d: "Worker 脚本内容"},
	})
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/r2/buckets"] = obj([]string{"name"}, map[string]prop{
		"name": {t: "string", req: true, d: "存储桶名称"},
	})
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}"] = obj(nil, map[string]prop{
		"content": {t: "string", d: "对象内容"},
	})
	routeRequestContracts["/api/cloudflare/accounts/{id}/pages"] = obj([]string{"projectName"}, map[string]prop{
		"projectName": {t: "string", req: true},
		"source":      {t: "object", d: "构建源配置"},
	})
	routeRequestContracts["/api/cloudflare/templates"] = obj([]string{"name"}, map[string]prop{
		"name":    {t: "string", req: true},
		"records": {t: "array", d: "DNS 记录模板"},
	})
	routeRequestContracts["/api/cloudflare/templates/{id}"] = routeRequestContracts["/api/cloudflare/templates"]
	routeRequestContracts["/api/cloudflare/templates/{templateId}/apply"] = obj([]string{"zoneId"}, map[string]prop{
		"zoneId": {t: "string", req: true},
	})
	routeRequestContracts["/api/cloudflare/accounts/{id}/verify"] = obj(nil, map[string]prop{})

	// ===== GitHub =====
	routeRequestContracts["/api/github/tokens"] = obj([]string{"name", "token"}, map[string]prop{
		"name":  {t: "string", req: true},
		"token": {t: "string", req: true, d: "GitHub Personal Access Token"},
	})
	routeRequestContracts["/api/github/tokens/{id}"] = obj(nil, map[string]prop{
		"name": {t: "string"},
	})
	routeRequestContracts["/api/github/repositories"] = obj(nil, map[string]prop{})
	routeRequestContracts["/api/github/repositories/{id}/refresh"] = obj(nil, map[string]prop{})
	routeRequestContracts["/api/github/repositories/{id}/webhook/configure"] = obj(nil, map[string]prop{
		"secret": {t: "string", d: "Webhook 密钥"},
	})
	routeRequestContracts["/api/github/history/compact"] = obj(nil, map[string]prop{
		"days": {t: "integer", d: "压缩多少天前的历史"},
	})
	routeRequestContracts["/api/github/public-pages"] = obj([]string{"title", "slug"}, map[string]prop{
		"title":        {t: "string", req: true},
		"slug":         {t: "string", req: true},
		"description":  {t: "string"},
		"repositories": {t: "array", d: "公开仓库 ID 列表"},
	})
	routeRequestContracts["/api/github/public-pages/{id}"] = routeRequestContracts["/api/github/public-pages"]

	// ===== 主机实例 server / serveragent =====
	noBody := obj(nil, map[string]prop{})
	// 公开订阅信息页（无凭据 GET），无需请求体。
	routeRequestContracts["/api/subscription/public/{token}"] = noBody
	routeRequestContracts["/api/server/accounts"] = obj([]string{"name", "host", "port"}, map[string]prop{
		"name":       {t: "string", req: true},
		"host":       {t: "string", req: true},
		"port":       {t: "integer", req: true},
		"username":   {t: "string"},
		"authMethod": {t: "string", e: []string{"password", "key", "agent"}},
		"password":   {t: "string"},
		"privateKey": {t: "string"},
		"group":      {t: "string"},
	})
	routeRequestContracts["/api/server/accounts/{id}"] = routeRequestContracts["/api/server/accounts"]
	routeRequestContracts["/api/server/accounts/import"] = obj(nil, map[string]prop{
		"accounts": {t: "array", d: "主机账号列表"},
	})
	routeRequestContracts["/api/server/accounts/reorder"] = obj([]string{"ids"}, map[string]prop{
		"ids": {t: "array", req: true, d: "排序后的账号 ID 列表"},
	})
	routeRequestContracts["/api/server/accounts/{id}/test-traffic-alert"] = noBody
	routeRequestContracts["/api/server/credentials"] = obj([]string{"name"}, map[string]prop{
		"name":       {t: "string", req: true},
		"username":   {t: "string"},
		"password":   {t: "string"},
		"privateKey": {t: "string"},
		"keyType":    {t: "string"},
	})
	routeRequestContracts["/api/server/credentials/{id}"] = routeRequestContracts["/api/server/credentials"]
	routeRequestContracts["/api/server/credentials/default"] = obj(nil, map[string]prop{"id": {t: "string"}})
	routeRequestContracts["/api/server/credentials/{id}/default"] = noBody
	routeRequestContracts["/api/server/snippets"] = obj([]string{"name", "content"}, map[string]prop{
		"name":      {t: "string", req: true},
		"content":   {t: "string", req: true},
		"type":      {t: "string"},
		"serverIds": {t: "array"},
		"group":     {t: "string"},
	})
	routeRequestContracts["/api/server/snippets/{id}"] = routeRequestContracts["/api/server/snippets"]
	routeRequestContracts["/api/server/snippets/preview"] = obj([]string{"content"}, map[string]prop{
		"content": {t: "string", req: true},
	})
	routeRequestContracts["/api/server/monitor/config"] = obj(nil, map[string]prop{
		"interval": {t: "integer", d: "采集间隔秒"},
		"enabled":  {t: "boolean"},
	})
	routeRequestContracts["/api/server/tasks"] = obj([]string{"serverId", "type"}, map[string]prop{
		"serverId": {t: "string", req: true},
		"type":     {t: "string", req: true, d: "任务类型"},
		"params":   {t: "object"},
	})
	routeRequestContracts["/api/server/tasks/{id}"] = noBody
	routeRequestContracts["/api/server/tasks/{id}/stream"] = noBody
	routeRequestContracts["/api/server/v2/tasks"] = obj([]string{"serverId", "type"}, map[string]prop{
		"serverId": {t: "string", req: true},
		"type":     {t: "string", req: true},
		"params":   {t: "object"},
	})
	routeRequestContracts["/api/server/info"] = obj([]string{"serverId"}, map[string]prop{"serverId": {t: "string", req: true}})
	routeRequestContracts["/api/server/test-connection"] = obj([]string{"id"}, map[string]prop{"id": {t: "string", req: true}})
	routeRequestContracts["/api/server/action"] = obj([]string{"serverId", "action"}, map[string]prop{
		"serverId": {t: "string", req: true},
		"action": {t: "string", req: true,
			e: []string{"reboot", "restart", "shutdown"},
			d: "仅支持预设主机动作；执行任意命令（如关闭进程 taskkill）请改用 POST /api/server/agent/command/{id} 向在线 Agent 下发命令"},
		"params": {t: "object"},
	})
	routeRequestContracts["/api/server/check-all"] = noBody
	routeRequestContracts["/api/server/v2/tasks"] = obj([]string{"serverId", "action"}, map[string]prop{
		"serverId": {t: "string", req: true},
		"domain":   {t: "string", d: "动作域（docker/compose/image/network/volume 等），服务端以 action 前缀为准"},
		"action": {t: "string", req: true,
			d: "域内动作：container.start/stop/restart/delete/pull/logs/update/rename、compose.restart、image.list/delete、network.create/remove 等"},
		"payload": {t: "object", d: "动作参数（containerId、image、project、configFiles 等）"},
	})
	routeRequestContracts["/api/server/status-pages"] = obj([]string{"title", "slug"}, map[string]prop{
		"title":          {t: "string", req: true},
		"slug":           {t: "string", req: true},
		"description":    {t: "string"},
		"serverIds":      {t: "array"},
		"includeSnippet": {t: "boolean"},
	})
	routeRequestContracts["/api/server/status-pages/{id}"] = routeRequestContracts["/api/server/status-pages"]
	routeRequestContracts["/api/server/s/{id}"] = obj(nil, map[string]prop{
		"action": {t: "string", d: "对主机执行的动作"},
	})
	routeRequestContracts["/api/server/network-quality/targets"] = obj([]string{"url"}, map[string]prop{
		"url":   {t: "string", req: true},
		"name":  {t: "string"},
		"group": {t: "string"},
	})
	routeRequestContracts["/api/server/network-quality/targets/{id}"] = routeRequestContracts["/api/server/network-quality/targets"]
	routeRequestContracts["/api/server/network-quality/{id}"] = noBody
	routeRequestContracts["/api/server/network-quality/{id}/collect"] = noBody

	// SFTP
	routeRequestContracts["/api/server/sftp/write"] = obj([]string{"serverId", "path", "content"}, map[string]prop{
		"serverId": {t: "string", req: true},
		"path":     {t: "string", req: true},
		"content":  {t: "string", req: true},
		"mode":     {t: "integer", d: "权限模式"},
	})
	routeRequestContracts["/api/server/sftp/mkdir"] = obj([]string{"serverId", "path"}, map[string]prop{
		"serverId": {t: "string", req: true},
		"path":     {t: "string", req: true},
	})
	routeRequestContracts["/api/server/sftp/rename"] = obj([]string{"serverId", "from", "to"}, map[string]prop{
		"serverId": {t: "string", req: true},
		"from":     {t: "string", req: true},
		"to":       {t: "string", req: true},
	})
	routeRequestContracts["/api/server/sftp/rmdir"] = obj([]string{"serverId", "path"}, map[string]prop{
		"serverId": {t: "string", req: true},
		"path":     {t: "string", req: true},
	})
	routeRequestContracts["/api/server/sftp/chmod"] = obj([]string{"serverId", "path", "mode"}, map[string]prop{
		"serverId": {t: "string", req: true},
		"path":     {t: "string", req: true},
		"mode":     {t: "integer", req: true},
	})
	routeRequestContracts["/api/server/sftp/upload"] = obj([]string{"serverId", "path", "content"}, map[string]prop{
		"serverId": {t: "string", req: true},
		"path":     {t: "string", req: true},
		"content":  {t: "string", req: true},
	})
	routeRequestContracts["/api/server/sftp/download/{serverId}"] = obj(nil, map[string]prop{"path": {t: "string"}})

	// Docker 与远程桌面
	routeRequestContracts["/api/server/docker/check-update"] = obj([]string{"serverId"}, map[string]prop{"serverId": {t: "string", req: true}})
	routeRequestContracts["/api/server/v2/docker"] = obj([]string{"serverId", "operation"}, map[string]prop{
		"serverId":  {t: "string", req: true},
		"operation": {t: "string", req: true, d: "容器操作"},
		"params":    {t: "object"},
	})
	routeRequestContracts["/api/server/v2/docker/overview"] = obj([]string{"serverId"}, map[string]prop{"serverId": {t: "string", req: true}})
	routeRequestContracts["/api/server/remote-desktop/sessions"] = obj([]string{"serverId"}, map[string]prop{
		"serverId": {t: "string", req: true},
		"quality":  {t: "integer", d: "画质 0-100"},
		"scale":    {t: "number", d: "分辨率缩放"},
	})
	routeRequestContracts["/api/server/remote-desktop/sessions/{id}"] = noBody
	routeRequestContracts["/api/server/remote-desktop/sessions/{id}/signals"] = obj([]string{"signal"}, map[string]prop{
		"signal": {t: "string", req: true},
		"args":   {t: "object"},
	})

	// Agent
	routeRequestContracts["/api/server/agent/quick-install"] = obj([]string{"serverId"}, map[string]prop{"serverId": {t: "string", req: true}})
	routeRequestContracts["/api/server/agent/regenerate-key"] = noBody
	routeRequestContracts["/api/server/agent/heartbeat"] = obj(nil, map[string]prop{
		"hostname": {t: "string"},
		"version":  {t: "string"},
		"metrics":  {t: "object"},
	})
	routeRequestContracts["/api/server/agent/command/{id}"] = obj([]string{"command"}, map[string]prop{
		"command": {t: "string", req: true},
		"timeout": {t: "integer"},
	})
	routeRequestContracts["/api/server/agent/auto-install/{id}"] = obj(nil, map[string]prop{"protocol": {t: "string"}})
	routeRequestContracts["/api/server/monitor/collect"] = noBody
	routeRequestContracts["/api/server/agent/batch-install"] = obj([]string{"serverIds"}, map[string]prop{
		"serverIds": {t: "array", req: true},
		"protocol":  {t: "string"},
	})
	routeRequestContracts["/api/server/agent/batch-upgrade"] = routeRequestContracts["/api/server/agent/batch-install"]
	routeRequestContracts["/api/server/agent/uninstall/{id}"] = obj(nil, map[string]prop{"force": {t: "boolean"}})
	routeRequestContracts["/api/server/agent/install-script/{id}"] = noBody
	routeRequestContracts["/api/server/agent/install/linux/{id}/{key}"] = noBody
	routeRequestContracts["/api/server/agent/install/win/{id}/{key}"] = noBody
	routeRequestContracts["/api/server/agent/key/generate"] = noBody
	routeRequestContracts["/api/server/agent/key"] = obj(nil, map[string]prop{"key": {t: "string"}})
	routeRequestContracts["/api/server/agent/proxy/{id}"] = obj([]string{"enabled"}, map[string]prop{
		"enabled":  {t: "boolean", req: true},
		"port":     {t: "integer"},
		"protocol": {t: "string"},
	})
	routeRequestContracts["/api/server/agent/proxy/{id}/reconcile"] = noBody
	routeRequestContracts["/api/server/agent/proxy/{id}/traffic"] = obj(nil, map[string]prop{"days": {t: "integer"}})
	routeRequestContracts["/api/server/agent/proxy/nodes"] = obj([]string{"name"}, map[string]prop{
		"name":     {t: "string", req: true},
		"bindIp":   {t: "string"},
		"port":     {t: "integer"},
		"serverId": {t: "string"},
	})
	routeRequestContracts["/api/server/agent/proxy/nodes/{id}"] = routeRequestContracts["/api/server/agent/proxy/nodes"]
	routeRequestContracts["/api/server/agent/proxy/nodes/{id}/reconcile"] = noBody
	routeRequestContracts["/api/server/agent/proxy/tunnels"] = obj([]string{"name"}, map[string]prop{
		"name":          {t: "string", req: true},
		"source":        {t: "string"},
		"destination":   {t: "string"},
		"serverId":      {t: "string"},
		"preferredAddr": {t: "string"},
	})
	routeRequestContracts["/api/server/agent/proxy/tunnels/{serverId}"] = routeRequestContracts["/api/server/agent/proxy/tunnels"]
	routeRequestContracts["/api/server/agent/proxy/tunnels/{serverId}/deploy"] = noBody
	routeRequestContracts["/api/server/agent/proxy/tunnels/preflight"] = obj(nil, map[string]prop{
		"serverId": {t: "string"},
		"protocol": {t: "string"},
	})
	routeRequestContracts["/api/server/agent/proxy/preferred-addresses"] = obj([]string{"address"}, map[string]prop{
		"address":  {t: "string", req: true},
		"label":    {t: "string"},
		"serverId": {t: "string"},
	})
	routeRequestContracts["/api/server/agent/proxy/preferred-addresses/{id}"] = routeRequestContracts["/api/server/agent/proxy/preferred-addresses"]
	routeRequestContracts["/api/server/agent/proxy/preferred-addresses/{id}/check"] = noBody
	routeRequestContracts["/api/server/agent/proxy/runtimes"] = obj([]string{"name", "version"}, map[string]prop{
		"name":    {t: "string", req: true},
		"version": {t: "string", req: true},
	})
	routeRequestContracts["/api/server/agent/proxy/runtimes/{id}/{action}"] = noBody

	// ===== 提示词库 prompts =====
	routeRequestContracts["/api/prompts/entries"] = obj([]string{"title"}, map[string]prop{
		"title":        {t: "string", req: true},
		"content":      {t: "string"},
		"description":  {t: "string"},
		"tags":         {t: "array"},
		"collectionId": {t: "string"},
		"isPublic":     {t: "boolean"},
	})
	routeRequestContracts["/api/prompts/entries/{id}"] = routeRequestContracts["/api/prompts/entries"]
	routeRequestContracts["/api/prompts/entries/{id}/draft"] = obj(nil, map[string]prop{
		"content": {t: "string"},
	})
	routeRequestContracts["/api/prompts/entries/{id}/duplicate"] = noBody
	routeRequestContracts["/api/prompts/entries/{id}/publish"] = obj(nil, map[string]prop{
		"isPublic": {t: "boolean"},
	})
	routeRequestContracts["/api/prompts/entries/{id}/public/regenerate"] = noBody
	routeRequestContracts["/api/prompts/entries/{id}/versions/{versionId}/restore"] = noBody
	routeRequestContracts["/api/prompts/entries/{id}/versions/{versionId}"] = noBody
	routeRequestContracts["/api/prompts/collections"] = obj([]string{"name"}, map[string]prop{
		"name":        {t: "string", req: true},
		"description": {t: "string"},
	})
	routeRequestContracts["/api/prompts/collections/{id}"] = routeRequestContracts["/api/prompts/collections"]
	routeRequestContracts["/api/prompts/settings"] = obj(nil, map[string]prop{
		"allowPublic": {t: "boolean"},
		"siteName":    {t: "string"},
	})
	routeRequestContracts["/api/prompts/public/{publicId}"] = obj([]string{"content"}, map[string]prop{"content": {t: "string", req: true}})

	// ===== 图编辑器 drawio =====
	routeRequestContracts["/api/drawio/documents"] = obj([]string{"title"}, map[string]prop{
		"title":    {t: "string", req: true},
		"content":  {t: "string", d: "drawio XML 内容"},
		"tagsJson": {t: "string"},
	})
	routeRequestContracts["/api/drawio/documents/{id}"] = obj(nil, map[string]prop{
		"title":   {t: "string"},
		"content": {t: "string"},
		"tags":    {t: "array"},
	})
	routeRequestContracts["/api/drawio/documents/{id}/draft"] = obj(nil, map[string]prop{
		"content":       {t: "string"},
		"title":         {t: "string"},
		"thumbnailPath": {t: "string"},
	})
	routeRequestContracts["/api/drawio/documents/{id}/clone"] = noBody
	routeRequestContracts["/api/drawio/documents/{id}/thumbnails/rebuild"] = noBody
	routeRequestContracts["/api/drawio/documents/{id}/versions/{versionId}/restore"] = noBody
	routeRequestContracts["/api/drawio/documents/{id}/versions/{versionId}"] = noBody
	routeRequestContracts["/api/drawio/import"] = obj([]string{"title", "content"}, map[string]prop{
		"title":   {t: "string", req: true},
		"content": {t: "string", req: true, d: "drawio/.xml/.excalidraw 内容"},
	})
	routeRequestContracts["/api/drawio/thumbnails/rebuild"] = obj([]string{"ids"}, map[string]prop{
		"ids": {t: "array", req: true, d: "文档 ID 列表"},
	})
	routeRequestContracts["/api/drawio/settings"] = obj(nil, map[string]prop{
		"autoSave":  {t: "boolean"},
		"thumbnail": {t: "boolean"},
	})

	// ===== 文件柜 filebox =====
	routeRequestContracts["/api/filebox/share"] = obj([]string{"fileId"}, map[string]prop{
		"fileId":           {t: "string", req: true},
		"burnAfterReading": {t: "boolean", d: "阅后即焚"},
		"expiresAt":        {t: "string", d: "过期时间"},
	})
	routeRequestContracts["/api/filebox/settings"] = obj(nil, map[string]prop{
		"publicUploadEnabled": {t: "boolean"},
		"maxSizeMb":           {t: "integer"},
	})
	routeRequestContracts["/api/filebox/shares"] = noBody
	routeRequestContracts["/api/filebox/access-logs"] = noBody
	routeRequestContracts["/api/m365/registrations"] = noBody
	routeRequestContracts["/api/filebox/void/rooms"] = obj([]string{"name"}, map[string]prop{
		"name":    {t: "string", req: true},
		"expires": {t: "integer", d: "有效期秒"},
	})
	routeRequestContracts["/api/filebox/void/rooms/{roomId}"] = obj(nil, map[string]prop{
		"name": {t: "string"},
	})
	routeRequestContracts["/api/filebox/void/rooms/{roomId}/participants"] = obj(nil, map[string]prop{
		"name": {t: "string"},
	})

	// ===== 阿里云 aliyun =====
	routeRequestContracts["/api/aliyun/accounts"] = obj([]string{"name", "accessKeyId", "accessKeySecret"}, map[string]prop{
		"name":            {t: "string", req: true},
		"accessKeyId":     {t: "string", req: true},
		"accessKeySecret": {t: "string", req: true},
		"region":          {t: "string"},
	})
	routeRequestContracts["/api/aliyun/accounts/{id}"] = routeRequestContracts["/api/aliyun/accounts"]
	routeRequestContracts["/api/aliyun/accounts/{id}/domains"] = obj([]string{"domain"}, map[string]prop{
		"domain": {t: "string", req: true},
	})
	routeRequestContracts["/api/aliyun/accounts/{id}/domains/{domainName}/records"] = obj([]string{"type", "rr", "value"}, map[string]prop{
		"type":  {t: "string", req: true, e: []string{"A", "AAAA", "CNAME", "TXT", "MX", "SRV"}},
		"rr":    {t: "string", req: true},
		"value": {t: "string", req: true},
		"ttl":   {t: "integer"},
		"line":  {t: "string"},
	})
	routeRequestContracts["/api/aliyun/accounts/{id}/records/{recordId}"] = routeRequestContracts["/api/aliyun/accounts/{id}/domains/{domainName}/records"]
	routeRequestContracts["/api/aliyun/accounts/{id}/records/{recordId}/status"] = obj(nil, map[string]prop{
		"status": {t: "string", e: []string{"enable", "disable"}},
	})
	routeRequestContracts["/api/aliyun/accounts/{id}/metrics"] = noBody
	routeRequestContracts["/api/aliyun/accounts/{id}/instances/{instanceId}/{action}"] = obj(nil, map[string]prop{
		"action": {t: "string", d: "实例动作"},
	})
	routeRequestContracts["/api/aliyun/accounts/{id}/swas/{instanceId}/firewall"] = obj(nil, map[string]prop{
		"rules": {t: "array", d: "防火墙规则"},
	})
	routeRequestContracts["/api/aliyun/accounts/{id}/swas/{instanceId}/{action}"] = noBody

	// ===== 腾讯云 tencent =====
	routeRequestContracts["/api/tencent/accounts"] = obj([]string{"name", "secretId", "secretKey"}, map[string]prop{
		"name":      {t: "string", req: true},
		"secretId":  {t: "string", req: true},
		"secretKey": {t: "string", req: true},
		"region":    {t: "string"},
	})
	routeRequestContracts["/api/tencent/accounts/{id}"] = routeRequestContracts["/api/tencent/accounts"]
	routeRequestContracts["/api/tencent/accounts/{id}/domains"] = obj([]string{"domain"}, map[string]prop{
		"domain": {t: "string", req: true},
	})
	routeRequestContracts["/api/tencent/accounts/{id}/domains/{domain}/records"] = obj([]string{"type", "name", "value"}, map[string]prop{
		"type":   {t: "string", req: true},
		"name":   {t: "string", req: true},
		"value":  {t: "string", req: true},
		"ttl":    {t: "integer"},
		"weight": {t: "integer"},
	})
	routeRequestContracts["/api/tencent/accounts/{id}/domains/{domain}/records/{recordId}"] = routeRequestContracts["/api/tencent/accounts/{id}/domains/{domain}/records"]
	routeRequestContracts["/api/tencent/accounts/{id}/domains/{domain}/records/{recordId}/status"] = obj(nil, map[string]prop{
		"status": {t: "string", e: []string{"enable", "disable"}},
	})
	routeRequestContracts["/api/tencent/accounts/{id}/cvm/{instanceId}/control"] = obj(nil, map[string]prop{
		"action": {t: "string", d: "实例控制动作"},
	})
	routeRequestContracts["/api/tencent/accounts/{id}/lighthouse/{instanceId}/control"] = obj(nil, map[string]prop{
		"action": {t: "string", d: "实例控制动作"},
	})

	// ===== Koyeb =====
	routeRequestContracts["/api/koyeb/accounts"] = obj([]string{"name", "token"}, map[string]prop{
		"name":  {t: "string", req: true},
		"token": {t: "string", req: true},
	})
	routeRequestContracts["/api/koyeb/accounts/{id}/refresh"] = noBody
	routeRequestContracts["/api/koyeb/data"] = obj([]string{"accountId"}, map[string]prop{"accountId": {t: "string", req: true}})
	routeRequestContracts["/api/koyeb/apps/{appId}/rename"] = obj([]string{"name"}, map[string]prop{
		"name": {t: "string", req: true},
	})
	routeRequestContracts["/api/koyeb/services/{serviceId}/rename"] = obj([]string{"name"}, map[string]prop{
		"name": {t: "string", req: true},
	})
	routeRequestContracts["/api/koyeb/services/{serviceId}/redeploy"] = obj(nil, map[string]prop{
		"image": {t: "string"},
	})
	routeRequestContracts["/api/koyeb/services/{serviceId}/restart"] = noBody
	routeRequestContracts["/api/koyeb/services/{serviceId}/pause"] = noBody
	routeRequestContracts["/api/koyeb/services/{serviceId}/instances"] = obj([]string{"accountId"}, map[string]prop{
		"accountId": {t: "string", req: true},
	})

	// ===== 定时任务 scheduler / cron =====
	routeRequestContracts["/api/scheduler/nodes"] = obj([]string{"name"}, map[string]prop{
		"name":     {t: "string", req: true},
		"hostname": {t: "string"},
		"enabled":  {t: "boolean"},
	})
	routeRequestContracts["/api/scheduler/nodes/{id}"] = routeRequestContracts["/api/scheduler/nodes"]
	routeRequestContracts["/api/scheduler/workflows"] = obj([]string{"name", "nodes"}, map[string]prop{
		"name":               {t: "string", req: true},
		"description":        {t: "string"},
		"schedule":           {t: "string"},
		"enabled":            {t: "boolean"},
		"concurrency_policy": {t: "string"},
		"failure_policy":     {t: "string"},
		"nodes":              {t: "array", req: true, d: "工作流节点（start/end 标记与任务节点）"},
		"edges":              {t: "array", d: "节点连线（from/to/condition）"},
	})
	routeRequestContracts["/api/scheduler/workflows/import"] = obj([]string{"workflows"}, map[string]prop{
		"workflows": {t: "array", req: true},
	})
	routeRequestContracts["/api/scheduler/workflows/{id}/run"] = obj(nil, map[string]prop{
		"inputs": {t: "object"},
	})
	routeRequestContracts["/api/scheduler/workflow-runs/{id}/retry"] = noBody
	routeRequestContracts["/api/scheduler/workflow-runs/{id}/cancel"] = noBody
	routeRequestContracts["/api/scheduler/cron/preview"] = obj([]string{"schedule"}, map[string]prop{
		"schedule": {t: "string", req: true},
	})

	// ===== OpenAI 网关剩余 =====
	routeRequestContracts["/api/openai/endpoints/{id}/toggle"] = obj([]string{"enabled"}, map[string]prop{
		"enabled": {t: "boolean", req: true},
	})
	routeRequestContracts["/api/openai/endpoints/{id}/models/toggle"] = obj([]string{"model", "enabled"}, map[string]prop{
		"model":   {t: "string", req: true, d: "模型名称"},
		"enabled": {t: "boolean", req: true, d: "是否启用该模型"},
	})
	routeRequestContracts["/api/openai/endpoints/{id}/model-mappings"] = obj([]string{"mappings"}, map[string]prop{
		"mappings": {t: "object", req: true, d: "模型对外映射名称，键为真实模型名，值为对外名称"},
	})
	routeRequestContracts["/api/openai/keys"] = obj([]string{"name"}, map[string]prop{
		"name":      {t: "string", req: true, d: "密钥名称"},
		"expiresAt": {t: "string", d: "过期时间，ISO 8601 字符串，留空表示不过期"},
	})
	routeRequestContracts["/api/openai/keys/{id}"] = routeRequestContracts["/api/openai/keys"]
	routeRequestContracts["/api/openai/keys/{id}/toggle"] = obj([]string{"enabled"}, map[string]prop{
		"enabled": {t: "boolean", req: true},
	})
	routeRequestContracts["/api/openai/keys/{id}/rotate"] = noBody
	routeRequestContracts["/api/openai/keys/{id}/default"] = noBody
	routeRequestContracts["/api/openai/proxies/resolve-subscription"] = obj([]string{"url"}, map[string]prop{
		"url": {t: "string", req: true, d: "订阅链接，必须以 http:// 或 https:// 开头"},
	})
	routeRequestContracts["/api/openai/analytics/clear"] = noBody
	routeRequestContracts["/api/openai/relay-errors"] = noBody
	routeRequestContracts["/api/openai/endpoints/{id}/routing"] = obj([]string{"priority", "weight"}, map[string]prop{
		"priority": {t: "number", d: "路由优先级（越小越优先），与 weight 至少提供一个"},
		"weight":   {t: "number", d: "路由权重，与 priority 至少提供一个"},
	})
	routeRequestContracts["/api/openai/endpoints/{id}/proxy-state"] = noBody
	routeRequestContracts["/api/openai/endpoints/{id}/proxy-state/unban"] = noBody
	routeRequestContracts["/api/openai/endpoints/{id}/verify"] = noBody
	routeRequestContracts["/api/openai/endpoints/{id}/test"] = noBody
	routeRequestContracts["/api/openai/endpoints/{id}/health-check"] = noBody
	routeRequestContracts["/api/openai/endpoints/{id}/health-check-all"] = noBody
	routeRequestContracts["/api/openai/endpoints/{id}/key-check"] = obj([]string{"keys"}, map[string]prop{
		"keys":    {t: "array", req: true},
		"timeout": {t: "number"},
	})
	routeRequestContracts["/api/openai/endpoints/refresh"] = noBody
	routeRequestContracts["/api/openai/endpoints/refresh-all"] = noBody
	routeRequestContracts["/api/openai/health-check-all"] = noBody
	routeRequestContracts["/api/openai/import"] = obj([]string{"endpoints"}, map[string]prop{
		"endpoints": {t: "array", req: true},
	})
	routeRequestContracts["/api/openai/personas"] = obj([]string{"name"}, map[string]prop{
		"name":        {t: "string", req: true},
		"description": {t: "string"},
		"prompt":      {t: "string"},
	})
	routeRequestContracts["/api/openai/personas/{id}"] = routeRequestContracts["/api/openai/personas"]
	routeRequestContracts["/api/openai/sessions/{id}"] = obj(nil, map[string]prop{
		"title": {t: "string"},
	})
	routeRequestContracts["/api/openai/sessions/{id}/messages"] = obj([]string{"role", "content"}, map[string]prop{
		"role":    {t: "string", req: true, e: []string{"user", "assistant", "system"}},
		"content": {t: "string", req: true},
	})
	routeRequestContracts["/api/openai/sessions"] = obj(nil, map[string]prop{
		"title":   {t: "string"},
		"modelId": {t: "string"},
	})

	// ===== 认证 auth =====
	routeRequestContracts["/api/auth/change-password"] = obj([]string{"currentPassword", "newPassword"}, map[string]prop{
		"currentPassword": {t: "string", req: true},
		"newPassword":     {t: "string", req: true},
	})
	routeRequestContracts["/api/auth/logout"] = noBody
	routeRequestContracts["/api/auth/2fa/setup"] = obj([]string{"secret", "code"}, map[string]prop{
		"secret": {t: "string", req: true, d: "TOTP 密钥"},
		"code":   {t: "string", req: true, d: "6 位验证码"},
	})
	routeRequestContracts["/api/auth/login-options"] = noBody
	routeRequestContracts["/api/auth/plugin-pairings/claim"] = obj([]string{"code"}, map[string]prop{
		"code": {t: "string", req: true},
	})
	routeRequestContracts["/api/auth/github/config"] = obj(nil, map[string]prop{
		"clientId":     {t: "string"},
		"clientSecret": {t: "string"},
	})
	routeRequestContracts["/api/auth/github/start"] = noBody
	routeRequestContracts["/api/auth/github/callback"] = obj([]string{"code"}, map[string]prop{
		"code": {t: "string", req: true},
	})
	routeRequestContracts["/api/auth/github/2fa"] = obj([]string{"code"}, map[string]prop{
		"code": {t: "string", req: true},
	})

	// ===== 系统设置 settings =====
	routeRequestContracts["/api/settings"] = obj(nil, map[string]prop{
		"siteName":  {t: "string"},
		"themeMode": {t: "string"},
		"pageWidth": {t: "string", e: []string{"standard", "wide", "full"}},
		"uiFont":    {t: "string", e: []string{"default", "serif", "lxgw-wenkai-screen"}},
	})
	routeRequestContracts["/api/settings/site-brand/icons"] = obj([]string{"name"}, map[string]prop{
		"name":     {t: "string", req: true},
		"data":     {t: "string", d: "图标数据 URL"},
		"mimeType": {t: "string"},
	})
	routeRequestContracts["/api/settings/site-brand/icons/{id}"] = noBody
	routeRequestContracts["/api/settings/database/import/preview"] = obj([]string{"databasePath"}, map[string]prop{
		"databasePath": {t: "string", req: true},
	})
	routeRequestContracts["/api/settings/database/import"] = obj([]string{"databasePath"}, map[string]prop{
		"databasePath": {t: "string", req: true},
	})
	routeRequestContracts["/api/settings/import-database"] = obj([]string{"databasePath"}, map[string]prop{
		"databasePath": {t: "string", req: true},
	})
	routeRequestContracts["/api/settings/export-database"] = noBody
	routeRequestContracts["/api/settings/vacuum-database"] = noBody
	routeRequestContracts["/api/settings/clear-logs"] = noBody

	// ===== Cloudflare 剩余 / 其他 =====
	routeRequestContracts["/api/cloudflare/import/accounts"] = obj([]string{"accounts"}, map[string]prop{"accounts": {t: "array", req: true}})
	routeRequestContracts["/api/cloudflare/import/templates"] = obj([]string{"templates"}, map[string]prop{"templates": {t: "array", req: true}})
	routeRequestContracts["/api/cloudflare/accounts/{id}/token"] = noBody
	routeRequestContracts["/api/cloudflare/accounts/{id}/cf-account-id"] = obj(nil, map[string]prop{
		"token": {t: "string"},
	})
	routeRequestContracts["/api/cloudflare/accounts/{id}/pages/{projectName}/domains"] = obj([]string{"domain"}, map[string]prop{"domain": {t: "string", req: true}})
	routeRequestContracts["/api/cloudflare/accounts/{id}/workers/{scriptName}/toggle"] = obj([]string{"enabled"}, map[string]prop{"enabled": {t: "boolean", req: true}})
	routeRequestContracts["/api/cloudflare/accounts/{id}/workers/{scriptName}/domains"] = obj([]string{"domain"}, map[string]prop{"domain": {t: "string", req: true}})
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}/download-info"] = noBody
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/r2/metrics"] = noBody
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}/download"] = noBody
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/folder-download"] = noBody
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}/preview"] = noBody
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/token"] = noBody
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/connections"] = noBody
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/zones/{zoneId}/workers/routes"] = obj([]string{"pattern", "script"}, map[string]prop{
		"pattern": {t: "string", req: true},
		"script":  {t: "string", req: true},
	})
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/zones/{zoneId}/workers/routes/{routeId}"] = routeRequestContracts["/api/cloudflare/accounts/{accountId}/zones/{zoneId}/workers/routes"]
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/zones/{zoneId}/purge"] = obj(nil, map[string]prop{
		"files":    {t: "array", d: "文件 URL 列表"},
		"cacheAll": {t: "boolean"},
	})
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/zones/{zoneId}/ssl"] = obj(nil, map[string]prop{
		"ssl":    {t: "string", e: []string{"off", "flexible", "full", "strict"}},
		"minTls": {t: "string"},
	})
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/zones/{zoneId}/switch"] = obj(nil, map[string]prop{
		"switchZoneId": {t: "string"},
	})
	routeRequestContracts["/api/cloudflare/accounts/{accountId}/zones/{zoneId}/batch"] = obj([]string{"records"}, map[string]prop{
		"records": {t: "array", req: true},
	})

	// ===== M365 / Fly.io / GitHub 公开页 =====
	routeRequestContracts["/api/m365/public/register"] = obj([]string{"email"}, map[string]prop{
		"email":      {t: "string", req: true},
		"name":       {t: "string"},
		"inviteCode": {t: "string"},
	})
	routeRequestContracts["/api/m365/public/invites/{code}"] = noBody
	routeRequestContracts["/api/github/webhook"] = obj(nil, map[string]prop{"payload": {t: "object"}})
	routeRequestContracts["/api/github/webhook/{repositoryId}"] = routeRequestContracts["/api/github/webhook"]
	routeRequestContracts["/api/github/public/page-by-domain"] = obj(nil, map[string]prop{"domain": {t: "string"}})
	routeRequestContracts["/api/github/public/pages/{slug}"] = noBody
	routeRequestContracts["/api/github/public/pages/{slug}/repositories/{id}"] = noBody
	routeRequestContracts["/api/flyio/accounts"] = obj([]string{"token"}, map[string]prop{
		"token": {t: "string", req: true, d: "Fly.io API Token"},
		"name":  {t: "string"},
	})
	routeRequestContracts["/api/flyio/apps/{appName}/events"] = noBody
	routeRequestContracts["/api/flyio/apps/{appName}/config"] = obj(nil, map[string]prop{
		"config": {t: "object", d: "应用配置 JSON"},
	})

	// ===== 1Panel 快捷控制 =====
	routeRequestContracts["/api/onepanel/spec"] = noBody
	routeRequestContracts["/api/onepanel/config"] = obj([]string{"serverId", "apiKey"}, map[string]prop{
		"serverId": {t: "string", req: true, d: "API Monitor 服务器 ID"},
		"apiKey":   {t: "string", req: true, d: "1Panel API 签名密钥（从 core.db settings 的 ApiKey 获取）"},
		"baseUrl":  {t: "string", d: "面板基地址，默认 https://127.0.0.1:8888"},
	})
	routeRequestContracts["/api/onepanel/config/{serverId}"] = obj([]string{"apiKey"}, map[string]prop{
		"apiKey":  {t: "string", req: true, d: "1Panel API 签名密钥"},
		"baseUrl": {t: "string", d: "面板基地址，默认 https://127.0.0.1:8888"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/health"] = noBody
	routeRequestContracts["/api/onepanel/{serverId}/overview"] = noBody
	routeRequestContracts["/api/onepanel/{serverId}/dashboard/current"] = noBody
	routeRequestContracts["/api/onepanel/{serverId}/upgrade/check"] = noBody
	routeRequestContracts["/api/onepanel/{serverId}/upgrade"] = obj([]string{"version"}, map[string]prop{
		"version": {t: "string", req: true, d: "目标版本号，可从 /upgrade/check 获取"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/websites"] = obj([]string{"type", "alias", "webSiteGroupID"}, map[string]prop{
		"type":           {t: "string", req: true, e: []string{"proxy", "static"}, d: "站点类型"},
		"alias":          {t: "string", req: true, d: "站点别名（通常为域名）"},
		"webSiteGroupID": {t: "integer", req: true, d: "站点分组 ID"},
		"proxy":          {t: "string", d: "反向代理目标，如 127.0.0.1:8080"},
		"domains":        {t: "array", d: "域名列表 [{domain, port}]"},
		"enableSSL":      {t: "boolean", d: "是否同时启用 SSL"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/websites/{id}/operate"] = obj([]string{"id", "operate"}, map[string]prop{
		"id":      {t: "integer", req: true, d: "网站 ID"},
		"operate": {t: "string", req: true, e: []string{"start", "stop", "restart"}},
	})
	routeRequestContracts["/api/onepanel/{serverId}/websites/{id}/proxy"] = obj([]string{"id", "name", "operate", "proxyHost", "proxyPass", "match"}, map[string]prop{
		"id":        {t: "integer", req: true, d: "网站 ID"},
		"name":      {t: "string", req: true, d: "反代配置名"},
		"operate":   {t: "string", req: true, e: []string{"update"}, d: "操作类型"},
		"proxyHost": {t: "string", req: true, d: "代理目标主机，如 127.0.0.1"},
		"proxyPass": {t: "string", req: true, d: "代理目标地址，如 http://127.0.0.1:9000"},
		"match":     {t: "string", req: true, d: "匹配规则，如 ^~ /"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/websites/{id}/https"] = obj([]string{"websiteId", "enable", "type"}, map[string]prop{
		"websiteId":  {t: "integer", req: true, d: "网站 ID"},
		"enable":     {t: "boolean", req: true},
		"type":       {t: "string", req: true, e: []string{"existed", "auto", "manual"}},
		"httpConfig": {t: "string", e: []string{"HTTPSOnly", "HTTPAlso", "HTTPToHTTPS"}},
	})
	routeRequestContracts["/api/onepanel/{serverId}/websites/{id}/nginx"] = obj([]string{"id", "content"}, map[string]prop{
		"id":      {t: "integer", req: true, d: "网站 ID"},
		"content": {t: "string", req: true, d: "nginx 配置内容"},
		"operate": {t: "string", e: []string{"add", "update", "delete"}},
		"params":  {t: "object"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/websites/{id}"] = noBody
	routeRequestContracts["/api/onepanel/{serverId}/apps/install"] = obj([]string{"appDetailId", "name"}, map[string]prop{
		"appDetailId":   {t: "integer", req: true, d: "应用详细 ID（从应用市场搜索接口获取）"},
		"name":          {t: "string", req: true, d: "应用实例名称"},
		"appKey":        {t: "string", d: "应用标识，如 mysql / nginx"},
		"version":       {t: "string", d: "应用版本"},
		"dockerCompose": {t: "string", d: "自定义 compose 内容"},
		"params":        {t: "object"},
		"editCompose":   {t: "boolean"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/apps/installed/{appInstallId}/op"] = obj([]string{"installId", "operate"}, map[string]prop{
		"installId": {t: "integer", req: true, d: "应用安装 ID"},
		"operate":   {t: "string", req: true, e: []string{"enable", "disable", "restart"}, d: "应用操作"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/containers/operate"] = obj([]string{"names", "operation"}, map[string]prop{
		"names":     {t: "array", req: true, d: "容器名列表"},
		"operation": {t: "string", req: true, e: []string{"up", "start", "stop", "restart", "kill", "pause", "unpause", "remove"}},
	})
	routeRequestContracts["/api/onepanel/{serverId}/containers/{name}/logs"] = noBody
	routeRequestContracts["/api/onepanel/{serverId}/containers/compose"] = obj([]string{"from"}, map[string]prop{
		"from":    {t: "string", req: true, d: "compose 来源：path / file / url"},
		"name":    {t: "string", d: "compose 名称"},
		"compose": {t: "object"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/ssl/obtain"] = obj([]string{"ID"}, map[string]prop{
		"ID":           {t: "integer", req: true, d: "SSL 证书条目 ID"},
		"nameservers":  {t: "array", d: "自定义 DNS 服务器"},
		"skipDNSCheck": {t: "boolean"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/acme"] = obj([]string{"type", "email", "keyType"}, map[string]prop{
		"email":    {t: "string", req: true, d: "ACME 注册邮箱"},
		"keyType":  {t: "string", req: true, e: []string{"EC256", "EC384", "RSA2048", "RSA3072", "RSA4096", "RSA8192"}},
		"type":     {t: "string", req: true, e: []string{"letsencrypt", "zerossl", "buypass", "google", "custom"}, d: "ACME 服务商"},
		"caDirURL": {t: "string", d: "自定义 CA 目录 URL"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/openresty/reload"] = noBody
	routeRequestContracts["/api/onepanel/{serverId}/backup"] = obj([]string{"type"}, map[string]prop{
		"type":       {t: "string", req: true, e: []string{"app", "mysql", "mariadb", "redis", "website", "postgresql", "mongodb"}},
		"name":       {t: "string", d: "备份存储账号名"},
		"detailName": {t: "string", d: "备份对象，如网站名 / 数据库名"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/backups/records"] = noBody
	routeRequestContracts["/api/onepanel/{serverId}/backups/options"] = noBody
	routeRequestContracts["/api/onepanel/{serverId}/databases"] = obj([]string{"from", "name", "type", "username", "version"}, map[string]prop{
		"type":     {t: "string", req: true, d: "数据库类型：mysql / mariadb / postgresql / redis / mongodb"},
		"name":     {t: "string", req: true, d: "数据库名"},
		"from":     {t: "string", req: true, d: "来源：create / existing"},
		"username": {t: "string", req: true, d: "数据库用户"},
		"version":  {t: "string", req: true, d: "数据库版本"},
		"format":   {t: "string", d: "字符集"},
		"password": {t: "string"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/databases/{id}/password"] = obj([]string{"password"}, map[string]prop{
		"password": {t: "string", req: true, d: "新密码"},
		"database": {t: "string", d: "数据库名"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/databases/{id}"] = noBody
	routeRequestContracts["/api/onepanel/{serverId}/runtimes"] = obj(nil, map[string]prop{
		"type":    {t: "string", d: "runtime 类型：php / python / nodejs"},
		"name":    {t: "string"},
		"version": {t: "string"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/cronjobs"] = obj(nil, map[string]prop{
		"type":   {t: "string", d: "任务类型"},
		"name":   {t: "string"},
		"spec":   {t: "string", d: "cron 表达式"},
		"script": {t: "string"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/proxy"] = obj([]string{"method", "path"}, map[string]prop{
		"method": {t: "string", req: true, e: []string{"GET", "POST", "PUT", "DELETE"}, d: "1Panel API 方法"},
		"path":   {t: "string", req: true, d: "1Panel API 路径，如 /websites/list（不含 /api/v2 前缀）"},
		"body":   {t: "object", d: "请求体（可选）"},
	})
	routeRequestContracts["/api/onepanel/{serverId}/proxy/catalog"] = noBody

	// ===== AI 接入 / 备份 =====
	routeRequestContracts["/api/ai-access/key/rotate"] = noBody
	routeRequestContracts["/api/system/ai-access/key/rotate"] = noBody
	routeRequestContracts["/api/ai-access/audit/clear"] = noBody
	routeRequestContracts["/api/system/ai-access/audit/clear"] = noBody
	routeRequestContracts["/api/ai-access/audit"] = obj(nil, map[string]prop{
		"days": {t: "integer"}, "page": {t: "integer"}, "pageSize": {t: "integer"},
	})
	routeRequestContracts["/api/system/ai-access/audit"] = routeRequestContracts["/api/ai-access/audit"]
	routeRequestContracts["/api/system/ai-access"] = noBody
	routeRequestContracts["/api/ai-access"] = noBody
	routeRequestContracts["/api/ai/manifest"] = noBody
	routeRequestContracts["/api/ai/mcp"] = noBody
	routeRequestContracts["/api/backup/run"] = noBody
	routeRequestContracts["/api/system/logs/download"] = noBody

	// ===== 管理 AI admin-ai =====
	routeRequestContracts["/api/admin-ai/sessions"] = obj(nil, map[string]prop{
		"title":  {t: "string", d: "会话标题"},
		"model":  {t: "string", d: "LLM 模型"},
		"source": {t: "string", d: "会话来源，如 web 或 channel:<id>"},
	})
	routeRequestContracts["/api/admin-ai/sessions/{id}"] = noBody
	routeRequestContracts["/api/admin-ai/messages"] = obj([]string{"sessionId", "prompt"}, map[string]prop{
		"sessionId": {t: "string", req: true, d: "会话 ID"},
		"prompt":    {t: "string", req: true, d: "用户消息"},
		"model":     {t: "string", d: "指定模型"},
		"source":    {t: "string", d: "会话来源"},
		"rewindId":  {t: "string", d: "编辑重发：删除该消息及其后所有消息后再执行"},
	})
	routeRequestContracts["/api/admin-ai/cancel"] = obj([]string{"runId"}, map[string]prop{
		"runId": {t: "string", req: true, d: "要取消的执行 ID"},
	})
	routeRequestContracts["/api/admin-ai/cron/daily-briefing"] = noBody
	routeRequestContracts["/api/admin-ai/cron/task-run"] = obj([]string{"prompt"}, map[string]prop{
		"prompt":    {t: "string", req: true, d: "AI 提示词"},
		"model":     {t: "string", d: "指定模型，留空回退默认模型"},
		"policy":    {t: "string", e: []string{"allow", "readonly"}, d: "allow（默认，写操作免审批）| readonly"},
		"channelId": {t: "string", d: "可选：完成后推送到绑定频道的接收者"},
		"title":     {t: "string", d: "会话标题，留空取 prompt 摘要"},
	})
	routeRequestContracts["/api/admin-ai/channels"] = obj(nil, map[string]prop{
		"channelId": {t: "string", d: "渠道 ID"},
		"name":      {t: "string", d: "渠道名称"},
		"config":    {t: "object", d: "渠道配置"},
	})
	routeRequestContracts["/api/admin-ai/channels/{id}"] = routeRequestContracts["/api/admin-ai/channels"]
	routeRequestContracts["/api/admin-ai/channels/{id}/start"] = noBody
	routeRequestContracts["/api/admin-ai/channels/{id}/stop"] = noBody
	routeRequestContracts["/api/admin-ai/channels/{id}/status"] = noBody
	routeRequestContracts["/api/admin-ai/approvals"] = noBody
	routeRequestContracts["/api/admin-ai/audit"] = noBody
	routeRequestContracts["/api/admin-ai/channel-bindings"] = obj([]string{"channelId", "channelUserId"}, map[string]prop{
		"channelId":     {t: "string", req: true, d: "频道配置 ID"},
		"channelUserId": {t: "string", req: true, d: "渠道侧用户 ID（如 Telegram 数字 ID）"},
		"username":      {t: "string", d: "渠道用户名"},
		"panelUserId":   {t: "string", d: "绑定的面板用户 ID"},
		"role":          {t: "string", d: "角色，默认 admin"},
	})
	routeRequestContracts["/api/admin-ai/channel-bindings/{id}"] = noBody
	routeRequestContracts["/api/admin-ai/memories"] = obj([]string{"content"}, map[string]prop{
		"content":    {t: "string", req: true, d: "记忆内容（最多 500 字）"},
		"importance": {t: "number", d: "重要性 1-10，默认 5"},
		"triggers":   {t: "string", d: "逗号分隔的触发词（选填）"},
		"pinned":     {t: "boolean", d: "是否置顶"},
	})
	routeRequestContracts["/api/admin-ai/memories/{id}"] = obj(nil, map[string]prop{
		"content":    {t: "string", d: "记忆内容（最多 500 字）"},
		"importance": {t: "number", d: "重要性 1-10"},
		"triggers":   {t: "string", d: "逗号分隔的触发词"},
		"pinned":     {t: "boolean", d: "是否置顶"},
	})
	routeRequestContracts["/api/admin-ai/approvals/{id}"] = obj([]string{"action"}, map[string]prop{
		"action":         {t: "string", req: true, e: []string{"approve", "reject"}, d: "批准或拒绝写操作"},
		"applyToSession": {t: "boolean", d: "批准并授权本会话后续写操作免审批"},
		"reason":         {t: "string", d: "拒绝/请求更改的原因"},
	})
	routeRequestContracts["/api/admin-ai/settings"] = obj(nil, map[string]prop{
		"gatewayKey":                    {t: "string", d: "管理 AI 网关密钥"},
		"admin_ai_enabled":              {t: "string", d: "管理 AI 总开关（true/false）"},
		"admin_ai_default_model":        {t: "string", d: "默认推理模型"},
		"admin_ai_write_enabled":        {t: "string", d: "写操作全局开关（true/false）"},
		"admin_ai_tool_call_limit":      {t: "string", d: "单轮最大工具调用次数"},
		"admin_ai_timeout_seconds":      {t: "string", d: "单轮执行超时秒数"},
		"admin_ai_context_window":       {t: "string", d: "上下文窗口 token 上限"},
		"admin_ai_audit_retention_days": {t: "string", d: "审计记录保留天数"},
	})

	// ---- 契约覆盖审计修复补登（2026-08-19）----
	// 写路由必须登记请求契约，见 TestRouteContractCoverage。
	routeRequestContracts["/api/server/snippets/history"] = obj(nil, map[string]prop{
		"serverId": {t: "string", d: "目标主机 ID（可选）"},
	})
	routeRequestContracts["/api/scheduler/tasks/{id}/run"] = noBody
	routeRequestContracts["/api/scheduler/workflows/{id}"] = obj([]string{"name", "nodes"}, map[string]prop{
		"name":              {t: "string", req: true, d: "工作流名称"},
		"nodes":             {t: "array", req: true, d: "工作流节点"},
		"edges":             {t: "array", d: "节点连线"},
		"schedule":          {t: "string", d: "cron 表达式"},
		"enabled":           {t: "boolean", d: "是否启用"},
		"concurrency_policy": {t: "string", d: "并发策略"},
		"failure_policy":    {t: "string", d: "失败策略"},
	})
	routeRequestContracts["/api/server/accounts/refresh-locations"] = noBody
	routeRequestContracts["/api/cron/tasks/{id}/run"] = noBody
	routeRequestContracts["/api/admin-ai/approvals/{id}/resolve"] = obj([]string{"action"}, map[string]prop{
		"action":         {t: "string", req: true, e: []string{"approve", "reject"}, d: "批准或拒绝写操作"},
		"applyToSession": {t: "boolean", d: "批准并授权本会话后续写操作免审批"},
		"reason":         {t: "string", d: "拒绝/请求更改的原因"},
	})
	routeRequestContracts["/api/notification/channels/{id}/test"] = obj(nil, map[string]prop{
		"message": {t: "string", d: "测试消息内容（可选）"},
	})
	routeRequestContracts["/api/notification/rules/{id}/dry-run"] = obj(nil, map[string]prop{
		"eventType": {t: "string", d: "模拟的事件类型"},
		"context":   {t: "object", d: "模拟事件的上下文"},
	})
	routeRequestContracts["/api/notification/rules/{id}/enable"] = noBody
	routeRequestContracts["/api/notification/rules/{id}/disable"] = noBody
	routeRequestContracts["/api/notification/templates/preview"] = obj([]string{"template"}, map[string]prop{
		"template":  {t: "string", req: true, d: "模板文本"},
		"eventType": {t: "string", d: "事件类型"},
		"context":   {t: "object", d: "渲染上下文"},
	})
	routeRequestContracts["/api/notification/config"] = obj(nil, map[string]prop{
		"globalEnabled": {t: "boolean", d: "全局开关"},
	})
	routeRequestContracts["/api/notification/trigger"] = obj([]string{"eventType"}, map[string]prop{
		"eventType": {t: "string", req: true, d: "事件类型"},
		"serverId":  {t: "string", d: "目标主机 ID"},
		"context":   {t: "object", d: "事件上下文"},
	})
	routeRequestContracts["/api/github/tokens/{id}/test"] = obj(nil, map[string]prop{
		"repositoryId": {t: "integer", d: "可选的仓库 ID（同时探测该仓库权限）"},
	})
	routeRequestContracts["/api/github/tokens/{id}/rotate"] = noBody
	routeRequestContracts["/api/github/repositories/parse-url"] = obj([]string{"url"}, map[string]prop{
		"url": {t: "string", req: true, d: "仓库 URL，如 https://github.com/owner/repo"},
	})
	routeRequestContracts["/api/github/repositories/reorder"] = obj([]string{"ids"}, map[string]prop{
		"ids": {t: "array", req: true, d: "按新顺序排列的仓库 ID 数组"},
	})
	routeRequestContracts["/api/github/repositories/{id}"] = obj(nil, map[string]prop{
		"name":        {t: "string", d: "仓库显示名称"},
		"description": {t: "string", d: "仓库描述"},
		"tags":        {t: "array", d: "标签"},
		"clean":       {t: "boolean", d: "删除时是否同时清空历史（DELETE）"},
	})
	routeRequestContracts["/api/github/repositories/{id}/actions/runs/{runId}/rerun"] = noBody
	routeRequestContracts["/api/github/repositories/{id}/actions/runs/{runId}/rerun-failed-jobs"] = noBody
	routeRequestContracts["/api/github/repositories/{id}/actions/runs/{runId}/cancel"] = noBody
	routeRequestContracts["/api/github/repositories/{id}/actions/refresh"] = noBody
	routeRequestContracts["/api/github/repositories/{id}/actions/workflows/{workflowId}/dispatch"] = obj([]string{"ref"}, map[string]prop{
		"ref":    {t: "string", req: true, d: "目标分支或 tag"},
		"inputs": {t: "object", d: "workflow_dispatch 输入参数"},
	})
	routeRequestContracts["/api/github/settings"] = obj(nil, map[string]prop{
		"intervalMinutes": {t: "number", d: "采集间隔（分钟）"},
		"enabled":         {t: "boolean", d: "采集器开关"},
	})
	routeRequestContracts["/api/github/collector/run"] = noBody
	routeRequestContracts["/api/server/v2/docker/{serverId}/containers/{containerId}/{action}"] = noBody
	routeRequestContracts["/api/server/v2/docker/{serverId}/images/prune"] = noBody
	routeRequestContracts["/api/server/v2/docker/{serverId}/networks/prune"] = noBody
	routeRequestContracts["/api/server/v2/docker/{serverId}/volumes/prune"] = noBody
	routeRequestContracts["/api/server/v2/docker/{serverId}/compose/{project}/{action}"] = obj(nil, map[string]prop{
		"configFiles":  {t: "array", d: "compose 配置文件路径列表"},
		"configFile":   {t: "string", d: "单配置文件路径"},
		"wait":         {t: "boolean", d: "等待完成（默认 true）"},
	})
	routeRequestContracts["/api/server/v2/docker/{serverId}/stacks/sync"] = noBody
	routeRequestContracts["/api/server/v2/docker/{serverId}/stacks/{project}/{action}"] = obj(nil, map[string]prop{
		"configFiles": {t: "array", d: "compose 配置文件路径列表"},
		"configFile":  {t: "string", d: "单配置文件路径"},
	})
}
