package openai

import (
	"time"
)

// proxyCooldown 是一个代理被标记失败后的基准冷却间隔；连续失败按指数退避放大，
// 避免瞬时抖动把坏节点快速洗回池内。达到封顶后由预热探活（成功）恢复。
const (
	proxyCooldown      = 60 * time.Second
	proxyCooldownMax   = 30 * time.Minute
	proxyCooldownShift = 5
)

// proxy429Cooldown 是 429 出口的冷却时长：每次上游 429 都立即按出口 IP 组冻结
// 该出口 1 小时（opencode 免密钥、按出口 IP 限流：429 即该 IP 已被上游限死，
// 1 小时内不会再次被选为候选，随机换代理也不会抽回刚 429 过的 IP）。
const proxy429Cooldown = time.Hour

// proxyRateLimitPicks 是一请求内「不同出口 IP」连续 429 的提前收尾阈值。
// 首个 429 后从可用候选（未冷却/未沉淀/本请求未试过）中随机抽新出口继续尝试；
// 已有 5 个不同出口 IP 都返回 429，说明上游限流已扩散到整池，提前以 429 收尾，
// 交给端点级聚合判断（全部限流时快速返回，不再浪费尝试轮）。
const proxyRateLimitPicks = 5

// stickyTTFBMax 是池级粘性代理的延迟上限：成功转发且首字耗时低于该值才记录为
// 粘性出口（一个出口有效就持续用，直到下一个 429）。过慢的出口不值得粘住，
// 继续交给池内择优逻辑。
const stickyTTFBMax = 10 * time.Second

// proxyAllFrozenRetryInterval 是「全部出口禁用时自动解冻全体代理」的节流间隔：
// 距上次自动解冻不足该间隔时仍回退直连，避免上游 IP 级限流未恢复时反复
// 「解冻→又全部冻结→又解冻」打满全池的限流风暴。
const proxyAllFrozenRetryInterval = 10 * time.Minute

// proxyAttemptCap 是单次转发最多尝试的代理数量上限。代理池可容纳数千条
// （文件批量导入），但一次请求串行扫完整池会拖死请求：限流时每个出口都要
// 等一次完整往返。封顶后大池只轮询前 cap 个出口，配合 429 累计冻结，
// 被限死的出口会逐渐退出候选，剩余流量自动向健康出口集中。
// 与 firstTokenTimeout 配合构成单次转发最坏耗时预算：10s × cap，避免
// 池过大 / 出口过慢时把请求拖到客户端超时断开（回 502）。
const proxyAttemptCap = 8

// endpointVerifyTimeout 是端点保存时「验证 Key + 拉取模型列表」的总超时上限。
// 代理池很大或所选出口挂死时，验证请求不应把保存动作拖成「等超时」。
const endpointVerifyTimeout = 8 * time.Second

// endpointRetryRounds 是全部候选端点均失败后，网关在内部重试整轮候选的
// 最大次数。对齐 New API 的 RetryTimes 语义：让客户端保持等待状态，网关
// 内部有耐心地反复重试所有候选，期间上游可能恢复。
// 0 = 不重试（试完即返回），3 = 试完候选后等待 500ms 再试，最多 3 轮。
const endpointRetryRounds = 3

var endpointRetryDelay = 500 * time.Millisecond

// rateLimitRetryBudget 是单请求内「429 等待重试」的总预算：全部候选端点返回
// 429 且端点开启 rateLimitRetryEnabled 时，网关在预算内等待配额窗口（优先
// Retry-After 头，缺省端点配置秒数）后重试整轮候选，预算耗尽才以 429 收尾。
// 适用于 RPM 很低、429 后等待数十秒配额才恢复的端点。
// 用 var 而非 const 以便测试注入更小的预算。
var rateLimitRetryBudget = 30 * time.Second

// rateLimitRetryRoundsCap 是 429 等待重试在预算内重试整轮候选的次数上限。
// 真实停止条件由 rateLimitRetryBudget 控制（每轮至少等待一次间隔），此上限仅
// 防止极端配置（wait_seconds 极短）把重试轮数放大到无意义。
const rateLimitRetryRoundsCap = 20

// attemptHeaderTimeout 是单次转发在「仍有可切换出口」时的响应头等待上限。
// 可切换时，代理在期限内不返回响应头即视为该出口链路不可用，提前切下一个
// （非流式此前只能等 transport 的 ResponseHeaderTimeout=180s，一个挂死的
// 代理会把整条回退链拖住三分钟，「第一次访问等很久」的主要成因之一）。
// 不可切换的终局尝试仍放行到 180s，避免误杀「排队很久但最终成功」的上游。
var attemptHeaderTimeout = 20 * time.Second

// sessionProxyRequestLimit 是同一会话在同一个出口 IP 上的请求数上限：
// 达到上限后主动轮换到下一个代理。opencode 等上游按出口 IP 限额时，
// 提前轮换可避免被限额后再重试（限额前主动换 IP）。
// 用 var 而非 const 以便测试注入更小的值。
var sessionProxyRequestLimit = 50

// verifyAPIKeyRaw 校验上游 API Key（GET /models）。endpointID 用于把 429 限流
// 累计到对应端点的代理池状态（辅助请求也参与 429 熔断，避免半死出口无人标记）。
