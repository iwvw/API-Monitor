// 轻量消息投影：把 opencode 的 message 列表裁剪成「只含对话」的最小形态。
//
// 背景：opencode 的 /session/{id}/message 会返回完整消息（含 reasoning、工具输出、
// diff 全文）。一个长会话可达数十 MB，且这些「过程」内容默认折叠、不需要首屏加载。
//
// 本模块在主机 Agent 侧对响应做投影：只保留用户文本、助手最终文本、工具调用的
// 元数据壳（供「改动文件」与统计）与 step-finish（token/耗时统计），剥掉 reasoning
// 与工具大输出。这样 host→云 的带宽也随之下降，而不只是客户端。
//
// 展开某个「已处理」折叠块时，客户端再用消息 id 自造游标回拉该轮完整 parts。

use serde_json::{Map, Value};

/// tool.state 中体积最大、折叠态渲染不需要的字段。
const HEAVY_STATE_KEYS: [&str; 4] = ["output", "error", "raw", "attachments"];
/// tool.state.metadata 中体积最大的字段（diff 全文 / 命令输出 / 文件预览）。
/// preview 是 read 工具读到的文件内容、display 是渲染用大块文本，折叠态都不需要。
const HEAVY_METADATA_KEYS: [&str; 5] = ["diff", "output", "patch", "preview", "display"];
/// filediff / files 条目里的大字段。
const HEAVY_FILE_KEYS: [&str; 4] = ["patch", "diff", "before", "after"];

/// 把完整 message 列表 JSON 投影成轻量形态。解析失败时返回 Err，由调用方决定回退。
pub fn project_message_list(body: &[u8]) -> Result<Vec<u8>, String> {
    let messages: Vec<Value> =
        serde_json::from_slice(body).map_err(|err| format!("解析消息列表失败: {}", err))?;
    let mut out: Vec<Value> = Vec::with_capacity(messages.len());
    for message in messages {
        out.push(project_message(message));
    }
    serde_json::to_vec(&out).map_err(|err| format!("序列化轻量消息失败: {}", err))
}

/// v2 单条 tool part 的 state 中体积最大、折叠态不需要的字段。
/// v2 用 `state.content: [{type,text}]` 承载命令/文件输出（v1 是 `state.output`）。
const V2_HEAVY_STATE_KEYS: [&str; 6] =
    ["content", "error", "raw", "attachments", "providerState", "providerResultState"];

/// 投影 v2 的 message 列表（`/api/session/{id}/message` 的 `data[]`）。
///
/// v2 消息形状与 v1 不同：assistant 的 parts 收在 `content[]` 里（v1 是顶层 `parts[]`），
/// 工具输出收在 `state.content[]`（v1 是 `state.output`）。语义一致：剥掉 reasoning
/// 与工具大输出，保留对话文本与工具元数据壳，并回填 lightweight 统计。
pub fn project_v2_message_list(messages: &[Value]) -> Result<Vec<u8>, String> {
    let mut out: Vec<Value> = Vec::with_capacity(messages.len());
    for message in messages {
        out.push(project_v2_message(message.clone()));
    }
    serde_json::to_vec(&out).map_err(|err| format!("序列化轻量消息失败: {}", err))
}

/// 投影 v2 消息列表为**轮次骨架**（每轮只留用户提问 + 最终回复 + 过程计数）。
///
/// 这是「历史记录拉取」的最终形态：默认视图本来就只有「用户 + 助手回复」，
/// 过程（reasoning / 工具调用 / 中间说明）默认折叠在「已处理」块里。既然折叠态
/// 一个字节都不显示，就没有必要把它们的正文传到客户端。
///
/// 实测（179 轮 / 3421 条消息的会话）：
///   - 原始完整消息              ≈ 10.6 MB（18 页 × 0.59 MB）
///   - 剥大字段（旧 lightweight）  29.93 MB
///   - 修掉 files[].data 剥离 bug   6.24 MB
///   - 本函数（轮次骨架）           0.16 MB
///
/// 输出仍是 `data[]` 形状，但元素是「骨架条目」而非 opencode 的 message：
///
///     { "type": "turn", "id": <userMessageId>, "time": {...},
///       "user": { "text": "...", "files": [...] },
///       "reply": { "text": "...", "messageID": "..." } | null,
///       "stats": { "reasoningCount": n, "stepCount": n },
///       "changedFiles": [{ "path": "...", "additions": n, "deletions": n }],
///       "turnCount": 1 }
///
/// 另外保留少量**非对话系统记录**（idle / compaction / agent-switched /
/// model-switched），它们承载「这一轮成功还是失败」「上下文何时压缩」
/// 「会话绑定哪个模型」等权威状态，体积可忽略但缺了会掉功能。
///
/// 展开某一轮的「已处理」块时，客户端仍按消息 id 自造游标回拉该轮完整 parts。
pub fn project_v2_turn_skeleton(messages: &[Value]) -> Result<Vec<u8>, String> {
    let mut out: Vec<Value> = Vec::new();
    // 当前轮的累积状态
    let mut turn: Option<SkeletonTurn> = None;

    /// 把已累积的轮次落盘，保证输出顺序与消息时间一致。
    fn flush(out: &mut Vec<Value>, turn: &mut Option<SkeletonTurn>) {
        if let Some(finished) = turn.take() {
            out.push(finished.into_value());
        }
    }

    for message in messages {
        let obj = match message.as_object() {
            Some(obj) => obj,
            None => continue,
        };
        let msg_type = obj.get("type").and_then(Value::as_str).unwrap_or("");

        match msg_type {
            "user" => {
                flush(&mut out, &mut turn);
                turn = Some(SkeletonTurn::from_user(obj));
            }
            "assistant" => {
                if let Some(current) = turn.as_mut() {
                    current.absorb_assistant(obj);
                }
                // 没有前置 user 的游离助手消息（历史续段）直接丢弃：
                // 骨架按轮组织，没有轮锚点的内容无法定位回拉。
            }
            // 后台任务的完成通知：`<shell id="sh_x" state="completed" ...>` /
            // `<subagent sessionID="ses_x" state="...">`。
            //
            // ⚠️ 必须**在 flush 之前**处理，把状态回填到当前轮 —— synthetic 在消息
            // 流里出现在该轮之后，若先 flush 再处理，这一轮的 backgroundJobs 已经
            // 落盘，状态永远是启动时的 unknown，刷新后界面显示「状态未知」。
            "synthetic" => {
                if let Some(current) = turn.as_mut() {
                    current.absorb_synthetic(obj);
                }
            }
            // 会话级权威状态：体积小，保留。
            //
            // 必须先 flush 当前轮：idle / compaction 在消息流里出现在**该轮之后**，
            // 若直接 push 就会跑到本轮前面，客户端按顺序消费时会把「轮次结论」
            // 错配到上一轮。
            "idle" | "compaction" | "agent-switched" | "model-switched" | "location-switched" => {
                flush(&mut out, &mut turn);
                out.push(match msg_type {
                    // compaction 的 summary / recent 是整段压缩摘要与保留消息全文
                    // （实测 3 条合计 285 KB）。客户端只用它们的**字符数**推算
                    // 压缩后上下文占用，正文没有任何渲染路径读取，换成计数即可。
                    "compaction" => compact_compaction_marker(message),
                    _ => project_v2_message(message.clone()),
                });
            }
            // 其余系统消息（system / skill / shell）体积可能不小，
            // 且对话流从不渲染，丢弃。
            _ => {}
        }
    }
    flush(&mut out, &mut turn);

    serde_json::to_vec(&out).map_err(|err| format!("序列化轮次骨架失败: {}", err))
}

/// 一轮的累积器：用户提问 + 过程统计 + 最后一段正文。
struct SkeletonTurn {
    /// 用户消息 id：既是轮次锚点，也是「展开已处理块」时自造游标的依据。
    id: String,
    /// 轮次起始时间（用户消息的 time.created）。
    created: Option<u64>,
    /// 轮次最后一条助手消息的完成时间，用于算出「已处理 N 分钟」。
    completed: Option<u64>,
    /// 该轮是否**尚未定稿**（仍有助手消息没有 completed）。
    ///
    /// 客户端据此把这一轮排除在骨架之外，改用完整消息拉取 —— 因为骨架给的是
    /// 此刻的截断快照，而后续 delta 是 ephemeral 不重放，无法判断重叠。
    unsettled: bool,
    user: Value,
    reply_text: Option<String>,
    reply_message_id: Option<String>,
    /// 「最终回复」在那条助手消息里的 text 序号（0-based）。
    ///
    /// 前端 partID 是 `${messageID}-text-${ordinal}`，实时 delta 也用同一规则
    /// 定位。骨架若一律当成 0，正在生成的那段只要不是 0 就会拼错位置。
    reply_ordinal: u32,
    /// 本条消息处理到目前是否以正文收尾（初始为「尚未见到内容」）。
    ///
    /// 用来判断「这条消息有没有资格提供最终回复」：以 tool 收尾的消息只是过程，
    /// 它前面那句说明属于中间说明，不能当作回复露到折叠块外。
    last_part_is_text: bool,
    reasoning_count: u64,
    step_count: u64,
    changed_files: Vec<Value>,
    /// 「已转入后台」的任务（骨架剥掉了工具 part，必须单独保留）
    background_jobs: Vec<Value>,
}

impl SkeletonTurn {
    fn from_user(obj: &serde_json::Map<String, Value>) -> Self {
        let mut user = serde_json::Map::new();
        if let Some(text) = obj.get("text") {
            user.insert("text".to_string(), text.clone());
        }
        // 附件只留元信息：mime / name / source，正文（base64）一律不带。
        if let Some(Value::Array(files)) = obj.get("files") {
            let meta: Vec<Value> = files
                .iter()
                .map(|file| {
                    let mut kept = serde_json::Map::new();
                    if let Value::Object(f) = file {
                        for key in ["mime", "name", "filename"] {
                            if let Some(value) = f.get(key) {
                                kept.insert(key.to_string(), value.clone());
                            }
                        }
                        if let Some(source) = f.get("source") {
                            kept.insert("source".to_string(), source.clone());
                        }
                        if let Some(Value::String(data)) = f.get("data") {
                            kept.insert(
                                "sizeBytes".to_string(),
                                Value::from(base64_decoded_len(data)),
                            );
                        }
                    }
                    Value::Object(kept)
                })
                .collect();
            user.insert("files".to_string(), Value::Array(meta));
        }
        Self {
            id: obj.get("id").and_then(Value::as_str).unwrap_or("").to_string(),
            created: obj
                .get("time")
                .and_then(|time| time.get("created"))
                .and_then(Value::as_u64),
            completed: None,
            unsettled: false,
            user: Value::Object(user),
            reply_text: None,
            reply_message_id: None,
            reply_ordinal: 0,
            last_part_is_text: false,
            reasoning_count: 0,
            step_count: 0,
            changed_files: Vec::new(),
            background_jobs: Vec::new(),
        }
    }

    fn absorb_assistant(&mut self, obj: &serde_json::Map<String, Value>) {
        let message_id = obj.get("id").and_then(Value::as_str).unwrap_or("");
        // 取该轮最后一个「已完成」时间：折叠块 header 的「已处理 N 分钟」由此算出。
        if let Some(completed) = obj
            .get("time")
            .and_then(|time| time.get("completed"))
            .and_then(Value::as_u64)
        {
            if self.completed.map_or(true, |current| completed > current) {
                self.completed = Some(completed);
            }
        } else {
            // 该条助手消息还没 completed → 这一轮仍在生成中。
            //
            // 骨架只能给出「此刻的截断快照」，而后续的 delta 是 ephemeral
            // （不重放），前端无法判断重叠。因此必须把「未定稿」明确告诉客户端，
            // 让它对这一轮改用完整消息拉取（方案 A）。
            self.unsettled = true;
        }
        let Some(Value::Array(parts)) = obj.get("content") else {
            return;
        };
        // 记录本条消息里 text part 的序号（从 0 开始，只数 text）。
        // 「最终回复」的 ordinal 必须一起带回：前端 partID 是
        // `${messageID}-text-${ordinal}`，实时 delta 也用它定位；
        // 若骨架一律当成 ordinal 0，正在生成的那段一旦不是 0 就会错位。
        let mut text_ordinal: u32 = 0;
        // 每条消息独立判定「是否以正文收尾」，不能沿用上一条的结果
        self.last_part_is_text = false;
        for part in parts {
            let Some(part) = part.as_object() else { continue };
            match part.get("type").and_then(Value::as_str).unwrap_or("") {
                "reasoning" => self.reasoning_count += 1,
                "tool" => {
                    self.step_count += 1;
                    self.collect_changed_files(part);
                    self.collect_background_job(part);
                    // 出现工具调用 → 本条消息**不以正文收尾**。
                    //
                    // 这是判「最终回复」的关键：模型常先说明一句「测试通过，现在
                    // 构建并部署」，然后再调工具。那句话是**中间说明**，不是最终
                    // 回答。若只按「轮内最后一段非空正文」取，它会被当成回复露在
                    // 折叠块外面 —— 用户看到的就是「已处理内容泄露成 Agent 最后
                    // 的消息」（实测：10 分 25 秒的一轮只露出了「18 个测试全部通过。
                    // 现在构建并部署 agent。」）。
                    self.last_part_is_text = false;
                }
                "text" => {
                    if let Some(text) = part.get("text").and_then(Value::as_str) {
                        if !text.trim().is_empty() {
                            // 只有「本条消息以正文收尾」时才把它当候选回复。
                            //
                            // 边说边做很常见：模型先写「18 个测试全部通过。现在构建
                            // 并部署 agent。」再调工具。那是**中间说明**，不是最终
                            // 回答。以 tool 收尾的消息整条都算过程，候选在这里被
                            // 记下、在消息末尾（若最后仍是 tool）清掉。
                            self.reply_text = Some(text.to_string());
                            self.reply_message_id = Some(message_id.to_string());
                            self.reply_ordinal = text_ordinal;
                            self.last_part_is_text = true;
                        }
                    }
                    text_ordinal += 1;
                }
                _ => {}
            }
        }
        // 本条消息以工具收尾：它写在正文之后的过程不属于最终回答。
        //
        // ⚠️ 只清「本条消息」的候选，不能影响整轮：工具密集型回合常以纯工具消息
        // 收尾，而正文在**更早那条消息**的末尾（那才是用户要看的回复）。
        // 因此这里清掉候选后，下一条助手消息若以正文收尾会重新写回。
        if !self.last_part_is_text {
            self.reply_text = None;
            self.reply_message_id = None;
            self.reply_ordinal = 0;
        }
    }

    /// 从工具 part 里抽出「已转入后台」的任务。
    ///
    /// 骨架把工具 part 整个剥掉（只留 stepCount），前端因此完全看不到后台任务：
    /// 判断依据 `state.metadata.shellID` 与「Command moved to the background」
    /// 回执都在被剥掉的字段里。实测一轮 `background=true` 的 bash 调用在骨架里
    /// 只剩 `stepCount: 1`，工作状态面板无从显示。
    ///
    /// 这里只保留几十字节的必要信息（id / 标题 / 状态），完成通知仍由
    /// `session.synthetic` 实时事件纠正。
    fn collect_background_job(&mut self, part: &serde_json::Map<String, Value>) {
        if self.background_jobs.len() >= 20 {
            return;
        }
        let state = part.get("state").and_then(Value::as_object);
        let metadata = state
            .and_then(|s| s.get("metadata"))
            .and_then(Value::as_object);

        // shell：metadata.shellID；子代理：metadata.sessionID
        let mut kind = "";
        let mut id = String::new();
        if let Some(metadata) = metadata {
            if let Some(shell_id) = metadata.get("shellID").and_then(Value::as_str) {
                if !shell_id.is_empty() {
                    kind = "shell";
                    id = shell_id.to_string();
                }
            }
        }
        if id.is_empty() {
            // 回退：从输出文本里认「已转入后台」回执
            let output = state
                .and_then(|s| s.get("content"))
                .and_then(Value::as_array)
                .map(|items| {
                    items
                        .iter()
                        .filter_map(|item| item.get("text").and_then(Value::as_str))
                        .collect::<Vec<_>>()
                        .join("")
                })
                .unwrap_or_default();
            if let Some(captured) = capture_between(&output, "shell ID: ", ")") {
                // ⚠️ 必须校验格式：模型经常在命令里**粘贴源码**，而源码里就有
                // 「Command moved to the background (shell ID: ${shellID})」这样的
                // 模板字符串。不做校验就会把 `${shellID}` 当成真实任务 id 收进面板。
                if is_shell_id(&captured) {
                    kind = "shell";
                    id = captured;
                }
            }
        }
        if id.is_empty() {
            return;
        }

        // 标题：优先 description（人写的短描述），其次命令行，最后回落 id。
        // 命令行常常很长（实测一条 `cd /opt/code/OCC && npx vitest run > ...` 有
        // 150+ 字符），直接当标题会把面板那一行撑爆，因此在主机侧就截断。
        let raw_title = state
            .and_then(|s| s.get("input"))
            .and_then(|i| i.get("description").or_else(|| i.get("command")))
            .and_then(Value::as_str)
            .unwrap_or(id.as_str());
        let title = truncate_chars(raw_title, 80);

        /*
         * 状态判定。
         *
         * ⚠️ 不能用工具 part 的 status 判断任务是否还在跑：`background=true` 的
         * 调用会**立即返回**（工具本身是 completed），任务继续在后台跑。
         *
         * ⚠️ 也不能一律写成 completed：实测骨架会把**此刻仍在跑**的后台任务
         * 标成 completed（`for i in $(seq 1 200); ...` 明明还在输出），
         * 前端据此显示「已结束」，用户看到的就是「后台任务始终是已结束状态」。
         *
         * 正确的判据是**这一轮是否还在生成**：
         * - 轮次未定稿（`unsettled`）→ 任务可能仍在跑，写 running；
         * - 轮次已定稿 → 这一轮结束了，但后台任务未必结束（它本来就不阻塞轮次）。
         *   此时没有完成通知就说明还没收到通知，写 unknown 让前端用
         *   `shell.list` 校正，而不是武断地写成 completed。
         *
         * 前端拿到 unknown 不会直接显示「状态未知」：`backgroundJobStore` 会
         * 用 `shell.list` 与实时事件把它校正成真实状态。
         */
        let mut status = if self.unsettled { "running" } else { "unknown" };
        if let Some(raw_status) = state.and_then(|s| s.get("status")).and_then(Value::as_str) {
            if raw_status == "running" || raw_status == "pending" {
                // 工具本身还在前台执行 —— 还没转成后台，不算后台任务
                return;
            }
            if raw_status == "error" || raw_status == "cancelled" {
                status = raw_status;
            }
        }

        let mut job = serde_json::Map::new();
        job.insert("id".to_string(), Value::from(id));
        job.insert("kind".to_string(), Value::from(kind));
        job.insert("title".to_string(), Value::from(title));
        job.insert("status".to_string(), Value::from(status));
        self.background_jobs.push(Value::Object(job));
    }

    /// 从 synthetic 完成通知里读出后台任务的终态，回填到对应任务上。
    ///
    /// 通知正文：`<shell id="sh_x" state="completed" command="...">output</shell>`。
    /// 只有 `id` 与 `state` 有用（标题已有命令行），因此只解析这两个。
    fn absorb_synthetic(&mut self, obj: &serde_json::Map<String, Value>) {
        if self.background_jobs.is_empty() {
            return;
        }
        let Some(text) = obj.get("text").and_then(Value::as_str) else {
            return;
        };
        let (id, state) = if let Some(rest) = text.split_once("<shell").map(|(_, r)| r) {
            (
                capture_attr(rest, "id"),
                capture_attr(rest, "state"),
            )
        } else if let Some(rest) = text.split_once("<subagent").map(|(_, r)| r) {
            (
                capture_attr(rest, "sessionID"),
                capture_attr(rest, "state"),
            )
        } else {
            (None, None)
        };
        let (Some(id), Some(state)) = (id, state) else {
            return;
        };
        for job in &mut self.background_jobs {
            let matches = job
                .get("id")
                .and_then(Value::as_str)
                .map(|value| value == id)
                .unwrap_or(false);
            if matches {
                job["status"] = Value::from(state.as_str());
            }
        }
    }

    /// 从工具的 metadata 里抽出「改了哪些文件、增删多少行」。
    ///
    /// 折叠态的「已处理」块只显示过程规模，但最终回复下方会展示本轮改动文件
    /// 胶囊（DiffChips），所以这几十字节要保留。
    fn collect_changed_files(&mut self, part: &serde_json::Map<String, Value>) {
        let Some(state) = part.get("state").and_then(Value::as_object) else {
            return;
        };
        let Some(metadata) = state.get("metadata").and_then(Value::as_object) else {
            return;
        };

        let mut push = |path: &str, additions: Option<&Value>, deletions: Option<&Value>| {
            if path.is_empty() || self.changed_files.len() >= 40 {
                return;
            }
            let mut entry = serde_json::Map::new();
            entry.insert("path".to_string(), Value::from(path));
            if let Some(additions) = additions.and_then(Value::as_i64) {
                entry.insert("additions".to_string(), Value::from(additions));
            }
            if let Some(deletions) = deletions.and_then(Value::as_i64) {
                entry.insert("deletions".to_string(), Value::from(deletions));
            }
            self.changed_files.push(Value::Object(entry));
        };

        if let Some(filediff) = metadata.get("filediff").and_then(Value::as_object) {
            if let Some(path) = filediff
                .get("filePath")
                .or_else(|| filediff.get("filepath"))
                .and_then(Value::as_str)
            {
                push(path, filediff.get("additions"), filediff.get("deletions"));
            }
        }
        if let Some(files) = metadata.get("files").and_then(Value::as_array) {
            for file in files {
                if let Some(file) = file.as_object() {
                    if let Some(path) = file
                        .get("filePath")
                        .or_else(|| file.get("filepath"))
                        .and_then(Value::as_str)
                    {
                        push(path, file.get("additions"), file.get("deletions"));
                    }
                }
            }
        }
    }

    fn into_value(self) -> Value {
        let mut obj = serde_json::Map::new();
        obj.insert("type".to_string(), Value::from("turn"));
        // 轮次 id / 时间必须带上：
        //   - id 是「展开已处理块」自造游标的锚点，缺了前端只能造 `skeleton-turn-N`，
        //     回拉时会定位不到真实消息；
        //   - time 缺失会让折叠块的「进行中」计时从 0（1970）起算，
        //     界面显示成「Working for 56y」。
        obj.insert("id".to_string(), Value::from(self.id));
        if let Some(created) = self.created {
            obj.insert(
                "time".to_string(),
                serde_json::json!({ "created": created }),
            );
        }
        // 轮次耗时：折叠块 header 的「已处理 N 分钟」直接读这个字段。
        // 骨架里没有逐条消息的工具时间戳，无法像分页路径那样反推，
        // 因此由 Agent 直接给「首条 user 创建 → 末条 assistant 完成」的总时长。
        if let (Some(created), Some(completed)) = (self.created, self.completed) {
            if completed > created {
                obj.insert("durationMs".to_string(), Value::from(completed - created));
            }
        }
        obj.insert("user".to_string(), self.user);
        if let Some(text) = self.reply_text {
            let mut reply = serde_json::Map::new();
            reply.insert("text".to_string(), Value::from(text));
            if let Some(id) = self.reply_message_id {
                reply.insert("messageID".to_string(), Value::from(id));
            }
            // text 序号：前端据 ${messageID}-text-${ordinal} 生成 partID，
            // 与实时 delta 的定位规则一致。
            reply.insert("ordinal".to_string(), Value::from(self.reply_ordinal));
            obj.insert("reply".to_string(), Value::Object(reply));
        } else {
            obj.insert("reply".to_string(), Value::Null);
        }
        let mut stats = serde_json::Map::new();
        stats.insert("reasoningCount".to_string(), Value::from(self.reasoning_count));
        stats.insert("stepCount".to_string(), Value::from(self.step_count));
        obj.insert("stats".to_string(), Value::Object(stats));
        // 未定稿标记：客户端据此把这一轮排除在骨架之外，改走完整消息拉取。
        if self.unsettled {
            obj.insert("settled".to_string(), Value::Bool(false));
        }
        if !self.changed_files.is_empty() {
            obj.insert("changedFiles".to_string(), Value::Array(self.changed_files));
        }
        if !self.background_jobs.is_empty() {
            obj.insert(
                "backgroundJobs".to_string(),
                Value::Array(self.background_jobs),
            );
        }
        Value::Object(obj)
    }
}

/// 是否是合法的 shell id（`sh_` + 字母数字）。
///
/// 用来挡掉从命令正文里误捕到的模板字符串（`${shellID}`、`<shellID>` 等）。
fn is_shell_id(value: &str) -> bool {
    let Some(rest) = value.strip_prefix("sh_") else {
        return false;
    };
    !rest.is_empty() && rest.chars().all(|c| c.is_ascii_alphanumeric())
}

/// 按字符（不是字节）截断，避免把一个多字节字符切坏。
fn truncate_chars(text: &str, max_chars: usize) -> String {
    let trimmed = text.trim();
    if trimmed.chars().count() <= max_chars {
        return trimmed.to_string();
    }
    let mut out: String = trimmed.chars().take(max_chars).collect();
    out.push('…');
    out
}

/// 取 `name="value"` 里的 value（XML 属性，单双引号均可）。
fn capture_attr(text: &str, name: &str) -> Option<String> {
    let needle = format!("{}=", name);
    let start = text.find(&needle)? + needle.len();
    let rest = text[start..].trim_start();
    let quote = rest.chars().next()?;
    if quote != '"' && quote != '\'' {
        return None;
    }
    let body = &rest[1..];
    let end = body.find(quote)?;
    let value = body[..end].trim();
    if value.is_empty() {
        None
    } else {
        Some(value.to_string())
    }
}

/// 取 `start` 之后、`end` 之前的那一段（找不到返回 None）。
fn capture_between(text: &str, start: &str, end: &str) -> Option<String> {
    let from = text.find(start)? + start.len();
    let rest = &text[from..];
    let to = rest.find(end)?;
    let value = rest[..to].trim();
    if value.is_empty() {
        None
    } else {
        Some(value.to_string())
    }
}

/// 估算 base64 正文解码后的字节数（只用于展示「附件多大」，不解码）。
fn base64_decoded_len(encoded: &str) -> u64 {
    let len = encoded.len() as u64;
    if len == 0 {
        return 0;
    }
    let padding = encoded
        .as_bytes()
        .iter()
        .rev()
        .take(2)
        .filter(|byte| **byte == b'=')
        .count() as u64;
    (len / 4) * 3 - padding
}

/// compaction 标记：把摘要 / 保留消息的**全文**换成字符数。
///
/// 客户端（`api/v2/message.ts:findCompactionMarker`）只读 `summary.length` 与
/// `recent.length` 来推算压缩后上下文占用，正文本身不进任何渲染路径。
/// 实测某会话 3 条 compaction 合计 285 KB，占骨架总体积的 45%。
fn compact_compaction_marker(message: &Value) -> Value {
    let mut obj = match message.as_object() {
        Some(obj) => obj.clone(),
        None => return message.clone(),
    };
    for key in ["summary", "recent"] {
        if let Some(Value::String(text)) = obj.get(key) {
            obj.insert(format!("{key}Chars"), Value::from(text.chars().count() as u64));
            obj.insert(key.to_string(), Value::String(String::new()));
        }
    }
    Value::Object(obj)
}

fn project_v2_message(message: Value) -> Value {
    let Value::Object(mut obj) = message else {
        return message;
    };

    let mut reasoning_count: u64 = 0;
    let mut step_count: u64 = 0;

    // assistant：content[] 里含 text / reasoning / tool。
    if let Some(Value::Array(parts)) = obj.get_mut("content") {
        let mut kept: Vec<Value> = Vec::with_capacity(parts.len());
        for part in parts.drain(..) {
            if let Some(next) = project_v2_part(part, &mut reasoning_count, &mut step_count) {
                kept.push(next);
            }
        }
        *parts = kept;
    }

    // user：files[] 里的内联图片（data: base64）首屏不拉本体，只留占位。
    if let Some(Value::Array(files)) = obj.get_mut("files") {
        for file in files.iter_mut() {
            strip_v2_inline_data(file);
        }
    }

    if reasoning_count > 0 || step_count > 0 {
        let mut lightweight = Map::new();
        lightweight.insert("reasoningCount".to_string(), Value::from(reasoning_count));
        lightweight.insert("stepCount".to_string(), Value::from(step_count));
        obj.insert("lightweight".to_string(), Value::Object(lightweight));
    }

    Value::Object(obj)
}

/// 投影 v2 单个 content part。返回 None 表示整条丢弃（reasoning）。
fn project_v2_part(part: Value, reasoning_count: &mut u64, step_count: &mut u64) -> Option<Value> {
    let Value::Object(mut obj) = part else {
        return Some(part);
    };
    let part_type = obj.get("type").and_then(Value::as_str).unwrap_or("");

    match part_type {
        "reasoning" => {
            *reasoning_count += 1;
            None
        }
        "tool" => {
            *step_count += 1;
            if let Some(Value::Object(state)) = obj.get_mut("state") {
                for key in V2_HEAVY_STATE_KEYS {
                    state.remove(key);
                }
                if let Some(Value::Object(metadata)) = state.get_mut("metadata") {
                    strip_metadata(metadata);
                }
            }
            Some(Value::Object(obj))
        }
        "file" => {
            let mut value = Value::Object(obj);
            strip_v2_inline_data(&mut value);
            Some(value)
        }
        _ => Some(Value::Object(obj)),
    }
}

/// v2 file 对象：把内联 base64 正文清空，只留 mime/name/source 占位。
///
/// ⚠️ v2 的内联图片**不在** `url` 里，而是：
///
///     { "data": "iVBORw0KGgo...", "mime": "image/webp", "source": {"type":"inline"} }
///
/// 早先只处理 `url` / `source.uri`，于是 `data` 被原样传下去。实测某 179 轮
/// 的会话里 `files[].data` 合计 23.69 MB，占整个轻量投影（29.93 MB）的 79% ——
/// 「轻量投影」因此比不投影的普通分页还大 3 倍，本意完全落空。
fn strip_v2_inline_data(file: &mut Value) {
    if let Value::Object(obj) = file {
        if let Some(Value::String(url)) = obj.get("url") {
            if url.starts_with("data:") {
                obj.insert("url".to_string(), Value::String(String::new()));
                obj.insert("urlStripped".to_string(), Value::Bool(true));
            }
        }
        // v2 内联附件的正文字段名就是 `data`（source.type === "inline"）。
        // 只清空内容、保留字段本身，让前端仍能判断「这里有个附件」。
        if let Some(Value::String(data)) = obj.get("data") {
            if !data.is_empty() {
                obj.insert("data".to_string(), Value::String(String::new()));
                obj.insert("dataStripped".to_string(), Value::Bool(true));
            }
        }
        // v2 file 也可能把内联数据放在 source.uri。
        if let Some(Value::Object(source)) = obj.get_mut("source") {
            if let Some(Value::String(uri)) = source.get("uri") {
                if uri.starts_with("data:") {
                    source.insert("uri".to_string(), Value::String(String::new()));
                }
            }
        }
    }
}

fn project_message(message: Value) -> Value {
    let Value::Object(mut obj) = message else {
        return message;
    };

    // info.summary.diffs 是整轮 patch 全文，消息流渲染不读，剥掉。
    if let Some(Value::Object(info)) = obj.get_mut("info") {
        if let Some(Value::Object(summary)) = info.get_mut("summary") {
            summary.remove("diffs");
        }
    }

    let mut reasoning_count: u64 = 0;
    let mut step_count: u64 = 0;

    if let Some(Value::Array(parts)) = obj.get_mut("parts") {
        let mut kept: Vec<Value> = Vec::with_capacity(parts.len());
        for part in parts.drain(..) {
            if let Some(next) = project_part(part, &mut reasoning_count, &mut step_count) {
                kept.push(next);
            }
        }
        *parts = kept;
    }

    // 把过程规模塞进消息，供客户端折叠块 header 在轻量态仍能显示「N 步 / N 段思考」。
    if reasoning_count > 0 || step_count > 0 {
        let mut lightweight = Map::new();
        lightweight.insert("reasoningCount".to_string(), Value::from(reasoning_count));
        lightweight.insert("stepCount".to_string(), Value::from(step_count));
        obj.insert("lightweight".to_string(), Value::Object(lightweight));
    }

    Value::Object(obj)
}

/// 投影单个 part。返回 None 表示整条丢弃（reasoning / snapshot）。
fn project_part(part: Value, reasoning_count: &mut u64, step_count: &mut u64) -> Option<Value> {
    let Value::Object(mut obj) = part else {
        return Some(part);
    };
    let part_type = obj.get("type").and_then(Value::as_str).unwrap_or("");

    match part_type {
        // 思考过程：整条丢弃，只记数量。
        "reasoning" => {
            *reasoning_count += 1;
            None
        }
        // 工具调用：保留元数据壳（工具名、标题、input、文件增删行数），剥掉大输出。
        "tool" => {
            *step_count += 1;
            if let Some(Value::Object(state)) = obj.get_mut("state") {
                for key in HEAVY_STATE_KEYS {
                    state.remove(key);
                }
                if let Some(Value::Object(metadata)) = state.get_mut("metadata") {
                    strip_metadata(metadata);
                }
            }
            Some(Value::Object(obj))
        }
        // 用户消息里的图片/附件：url 多为 data:image base64（单条可达数百 KB）。
        // 首屏不拉图片本体，只留 mime/filename/source 占位，展开时再回拉。
        "file" => {
            if let Some(Value::String(url)) = obj.get("url") {
                if url.starts_with("data:") {
                    obj.insert("url".to_string(), Value::String(String::new()));
                    obj.insert("urlStripped".to_string(), Value::Bool(true));
                }
            }
            Some(Value::Object(obj))
        }
        // step-start 只带快照引用（大字符串），剥掉快照本身。
        "step-start" => {
            obj.remove("snapshot");
            Some(Value::Object(obj))
        }
        // snapshot part 只承载一个快照字符串，渲染层不读，整条丢弃。
        "snapshot" => None,
        // 其余（text / step-finish / file / patch / subtask / retry / compaction 等）
        // 体积可控或渲染需要，原样保留。
        _ => Some(Value::Object(obj)),
    }
}

fn strip_metadata(metadata: &mut Map<String, Value>) {
    for key in HEAVY_METADATA_KEYS {
        metadata.remove(key);
    }
    if let Some(Value::Object(filediff)) = metadata.get_mut("filediff") {
        for key in HEAVY_FILE_KEYS {
            filediff.remove(key);
        }
    }
    if let Some(Value::Array(files)) = metadata.get_mut("files") {
        for file in files.iter_mut() {
            if let Value::Object(entry) = file {
                for key in HEAVY_FILE_KEYS {
                    entry.remove(key);
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn project_one(message: Value) -> Value {
        let bytes = serde_json::to_vec(&vec![message]).unwrap();
        let out = project_message_list(&bytes).unwrap();
        let mut parsed: Vec<Value> = serde_json::from_slice(&out).unwrap();
        parsed.pop().unwrap()
    }

    #[test]
    fn drops_reasoning_and_counts_it() {
        let message = json!({
            "info": { "id": "a1", "role": "assistant", "time": { "created": 1 } },
            "parts": [
                { "id": "r1", "type": "reasoning", "text": "thinking", "time": {} },
                { "id": "r2", "type": "reasoning", "text": "more", "time": {} },
                { "id": "t1", "type": "text", "text": "answer" }
            ]
        });
        let projected = project_one(message);
        let parts = projected["parts"].as_array().unwrap();
        assert_eq!(parts.len(), 1);
        assert_eq!(parts[0]["type"], "text");
        assert_eq!(projected["lightweight"]["reasoningCount"], 2);
        assert_eq!(projected["lightweight"]["stepCount"], 0);
    }

    #[test]
    fn strips_heavy_tool_state_but_keeps_file_stats() {
        let message = json!({
            "info": { "id": "a1", "role": "assistant", "time": { "created": 1 } },
            "parts": [{
                "id": "tool1",
                "type": "tool",
                "callID": "call1",
                "tool": "edit",
                "state": {
                    "status": "completed",
                    "input": { "filePath": "src/app.ts" },
                    "output": "x".repeat(10000),
                    "title": "edit src/app.ts",
                    "metadata": {
                        "filepath": "src/app.ts",
                        "diff": "unified diff body",
                        "filediff": { "patch": "patch body", "before": "a", "after": "b", "additions": 3, "deletions": 1 },
                        "files": [{ "filePath": "src/app.ts", "diff": "file diff", "additions": 3, "deletions": 1 }]
                    },
                    "time": { "start": 1, "end": 2 }
                }
            }]
        });
        let projected = project_one(message);
        let part = &projected["parts"][0];
        assert_eq!(projected["lightweight"]["stepCount"], 1);

        let state = &part["state"];
        assert!(state.get("output").is_none());
        assert_eq!(state["title"], "edit src/app.ts");

        let metadata = &state["metadata"];
        assert!(metadata.get("diff").is_none());
        assert_eq!(metadata["filepath"], "src/app.ts");
        assert_eq!(metadata["filediff"]["additions"], 3);
        assert!(metadata["filediff"].get("patch").is_none());
        assert_eq!(metadata["files"][0]["filePath"], "src/app.ts");
        assert!(metadata["files"][0].get("diff").is_none());
    }

    #[test]
    fn strips_summary_diffs_and_snapshot_parts() {
        let message = json!({
            "info": {
                "id": "u1",
                "role": "user",
                "time": { "created": 1 },
                "summary": { "title": "keep", "diffs": [{ "file": "a.ts", "patch": "huge" }] }
            },
            "parts": [
                { "id": "s1", "type": "snapshot", "snapshot": "very long" },
                { "id": "t1", "type": "text", "text": "hi" }
            ]
        });
        let projected = project_one(message);
        assert!(projected["info"]["summary"].get("diffs").is_none());
        assert_eq!(projected["info"]["summary"]["title"], "keep");
        let parts = projected["parts"].as_array().unwrap();
        assert_eq!(parts.len(), 1);
        assert_eq!(parts[0]["type"], "text");
    }

    /// 真实数据冒烟：设置 AIAGENT_PROJECTION_SAMPLE 指向一份真实 message 列表 JSON，
    /// 验证投影体积下降与对话完整性。默认忽略，仅手动运行。
    #[test]
    #[ignore]
    fn real_sample_projection_reduces_size() {
        let path = match std::env::var("AIAGENT_PROJECTION_SAMPLE") {
            Ok(value) => value,
            Err(_) => return,
        };
        let raw = std::fs::read(&path).expect("read sample");
        let projected = project_message_list(&raw).expect("project");
        let ratio = projected.len() as f64 / raw.len() as f64;
        println!(
            "原始 {} bytes -> 投影 {} bytes ({}%)",
            raw.len(),
            projected.len(),
            (ratio * 100.0).round()
        );

        // 投影后仍应是合法 JSON 数组，且消息条数不变。
        let before: Vec<Value> = serde_json::from_slice(&raw).unwrap();
        let after: Vec<Value> = serde_json::from_slice(&projected).unwrap();
        assert_eq!(before.len(), after.len());

        // 每条消息都不应再含 reasoning part。
        for message in &after {
            if let Some(parts) = message["parts"].as_array() {
                assert!(
                    parts.iter().all(|p| p["type"] != "reasoning"),
                    "reasoning part 未剥掉"
                );
            }
        }
    }

    #[test]
    fn keeps_text_and_step_finish() {
        let message = json!({
            "info": { "id": "a1", "role": "assistant", "time": { "created": 1 } },
            "parts": [
                { "id": "sf", "type": "step-finish", "reason": "stop", "cost": 0, "tokens": { "input": 1 } },
                { "id": "t1", "type": "text", "text": "done" }
            ]
        });
        let projected = project_one(message);
        let types: Vec<&str> = projected["parts"]
            .as_array()
            .unwrap()
            .iter()
            .map(|p| p["type"].as_str().unwrap())
            .collect();
        assert_eq!(types, vec!["step-finish", "text"]);
        assert!(projected.get("lightweight").is_none());
    }

    fn project_v2_one(message: Value) -> Value {
        let out = project_v2_message_list(&[message]).unwrap();
        let mut parsed: Vec<Value> = serde_json::from_slice(&out).unwrap();
        parsed.pop().unwrap()
    }

    /// v2：assistant 的 content[] 里剥掉 reasoning、重工具输出，回填统计。
    #[test]
    fn v2_drops_reasoning_and_strips_heavy_tool_state() {
        let message = json!({
            "info": { "id": "a1", "role": "assistant" },
            "content": [
                { "type": "reasoning", "text": "thinking" },
                { "type": "text", "text": "answer" },
                {
                    "type": "tool",
                    "tool": "bash",
                    "state": {
                        "status": "completed",
                        "content": [ { "type": "text", "text": "huge output" } ],
                        "metadata": { "preview": "big", "diff": "big" }
                    }
                }
            ]
        });
        let projected = project_v2_one(message);
        let parts = projected["content"].as_array().unwrap();
        assert_eq!(parts.len(), 2);
        assert_eq!(parts[0]["type"], "text");
        assert_eq!(parts[1]["type"], "tool");
        assert!(parts[1]["state"].get("content").is_none(), "state.content 应被剥掉");
        assert!(parts[1]["state"]["metadata"].get("preview").is_none());
        assert_eq!(projected["lightweight"]["reasoningCount"], 1);
        assert_eq!(projected["lightweight"]["stepCount"], 1);
    }

    /// v2：user 消息内联 data: 图片只留占位，本体清空。
    #[test]
    fn v2_strips_inline_image_data() {
        let message = json!({
            "info": { "id": "u1", "role": "user" },
            "files": [ { "url": "data:image/png;base64,AAAA", "mime": "image/png" } ]
        });
        let projected = project_v2_one(message);
        let file = &projected["files"][0];
        assert_eq!(file["url"], "");
        assert_eq!(file["urlStripped"], true);
        assert_eq!(file["mime"], "image/png");
    }

    /// 回归：v2 内联附件的正文在 `files[].data`，不是 `url` / `source.uri`。
    ///
    /// 早先只处理了后两者，导致 base64 正文原样下发 —— 实测某会话 23.69 MB
    /// 图片数据占了轻量投影的 79%，比不投影还大 3 倍。
    #[test]
    fn v2_strips_inline_file_data_field() {
        let message = json!({
            "type": "user",
            "id": "u1",
            "text": "看截图",
            "files": [{
                "data": "iVBORw0KGgoAAAANSUhEUg",
                "mime": "image/webp",
                "name": "image.png",
                "source": { "type": "inline" }
            }]
        });
        let projected = project_v2_one(message);
        let file = &projected["files"][0];
        assert_eq!(file["data"], "", "files[].data 必须清空");
        assert_eq!(file["dataStripped"], true);
        assert_eq!(file["mime"], "image/webp");
        assert_eq!(file["name"], "image.png");
        assert_eq!(projected["text"], "看截图");
    }

    fn skeleton_of(messages: Vec<Value>) -> Vec<Value> {
        serde_json::from_slice(&project_v2_turn_skeleton(&messages).unwrap()).unwrap()
    }

    /// 骨架：每轮只留用户提问 + **真正的最终回复** + 过程计数。
    ///
    /// 「最终回复」= 轮内最后一条**以正文收尾**的消息的正文。判据必须与前端
    /// `splitProcessRenderItems` 一致：以 tool 收尾的消息整条都是过程，它前面那句
    /// 是中间说明。
    ///
    /// 反例（本条回归的就是它）：a2 是 `text + tool`，模型说完「改好了」又去调工具；
    /// 若把 a2 的 text 当回复，用户会在折叠块外看到一句过程说明，而真正的收尾在
    /// a4（`text` 结尾）。
    #[test]
    fn skeleton_picks_reply_from_message_ending_with_text() {
        let messages = vec![
            json!({ "type": "user", "id": "u1", "text": "改一下" }),
            json!({
                "type": "assistant", "id": "a1",
                "content": [
                    { "type": "reasoning", "text": "想一想" },
                    { "type": "text", "text": "先看看代码" }
                ]
            }),
            json!({
                "type": "assistant", "id": "a2",
                "content": [
                    { "type": "text", "text": "改好了，两个问题都修了。" },
                    { "type": "tool", "name": "edit", "state": { "status": "completed" } }
                ]
            }),
            json!({
                "type": "assistant", "id": "a3",
                "content": [
                    { "type": "reasoning", "text": "收尾" },
                    { "type": "tool", "name": "bash", "state": { "status": "completed" } }
                ]
            }),
            json!({
                "type": "assistant", "id": "a4",
                "content": [{ "type": "text", "text": "两个问题都改完了，测试通过。" }]
            })
        ];
        let skeleton = skeleton_of(messages);
        assert_eq!(skeleton.len(), 1);
        let turn = &skeleton[0];
        assert_eq!(turn["type"], "turn");
        assert_eq!(turn["user"]["text"], "改一下");
        // 只有 a4 以正文收尾 → 它才是最终回复
        assert_eq!(turn["reply"]["text"], "两个问题都改完了，测试通过。");
        assert_eq!(turn["reply"]["messageID"], "a4");
        assert_eq!(turn["stats"]["reasoningCount"], 2);
        assert_eq!(turn["stats"]["stepCount"], 2);
    }

    /// 回归：中间说明不能泄露成最终回复。
    ///
    /// 实测场景：一轮跑了 10 分 25 秒，模型在过程里说了「18 个测试全部通过。现在
    /// 构建并部署 agent。」然后继续调工具，那一轮最终被打断（没有正文收尾）。
    ///
    /// 正确结果是 **reply = null**（整轮都是过程，前端只显示折叠块），而不是把那句
    /// 中间说明当回复露在折叠块外面。
    #[test]
    fn skeleton_does_not_leak_mid_turn_narration_as_reply() {
        let messages = vec![
            json!({ "type": "user", "id": "u1", "text": "继续" }),
            json!({
                "type": "assistant", "id": "a1",
                "content": [
                    { "type": "reasoning", "text": "跑完了" },
                    { "type": "text", "text": "18 个测试全部通过。现在构建并部署 agent。" },
                    { "type": "tool", "name": "shell", "state": { "status": "running" } }
                ]
            })
        ];
        let skeleton = skeleton_of(messages);
        assert_eq!(skeleton[0]["reply"], serde_json::Value::Null);
        assert_eq!(skeleton[0]["stats"]["stepCount"], 1);
    }

    /// 骨架：轮次必须带 id / time / durationMs。
    ///
    /// - id 是「展开已处理块」自造游标的锚点，缺了只能兜底生成 `skeleton-turn-N`，
    ///   回拉时定位不到真实消息；
    /// - time 缺失会让折叠块的计时从 0（1970）起算，显示成「Working for 56y」；
    /// - durationMs 缺失会让折叠块显示「已处理 0ms」（骨架里没有工具时间戳可反推）。
    #[test]
    fn skeleton_keeps_turn_id_created_time_and_duration() {
        let messages = vec![
            json!({ "type": "user", "id": "msg_u1", "text": "hi", "time": { "created": 1730000000000u64 } }),
            json!({
                "type": "assistant", "id": "msg_a1",
                "time": { "created": 1730000001000u64, "completed": 1730000090000u64 },
                "content": [{ "type": "text", "text": "ok" }]
            })
        ];
        let skeleton = skeleton_of(messages);
        assert_eq!(skeleton[0]["id"], "msg_u1");
        assert_eq!(skeleton[0]["time"]["created"], 1730000000000u64);
        assert_eq!(skeleton[0]["reply"]["messageID"], "msg_a1");
        // 90 秒
        assert_eq!(skeleton[0]["durationMs"], 90000u64);
    }

    /// 骨架：`reply.ordinal` 必须反映该 text 在那条消息里的真实序号。
    ///
    /// 前端 partID 是 `${messageID}-text-${ordinal}`，实时 delta 用同一规则定位。
    /// 骨架一律当 0 的话，正在生成的那段只要不是 0，delta 就会拼到错误位置。
    #[test]
    fn skeleton_reports_reply_text_ordinal() {
        let messages = vec![
            json!({ "type": "user", "id": "u1", "text": "hi", "time": { "created": 1000u64 } }),
            json!({
                "type": "assistant", "id": "a1",
                "time": { "created": 1001u64, "completed": 2000u64 },
                "content": [
                    { "type": "text", "text": "第一段" },
                    { "type": "tool", "name": "bash", "state": { "status": "completed" } },
                    { "type": "text", "text": "第二段（最终回复）" }
                ]
            })
        ];
        let skeleton = skeleton_of(messages);
        assert_eq!(skeleton[0]["reply"]["text"], "第二段（最终回复）");
        // 第二个 text part → ordinal 1
        assert_eq!(skeleton[0]["reply"]["ordinal"], 1);
    }

    /// 骨架必须保留「已转入后台」的任务。
    ///
    /// 骨架把工具 part 整个剥掉（只留 stepCount），而判断后台任务的依据
    /// （`state.metadata.shellID` 与「Command moved to the background」回执）
    /// 全在被剥掉的字段里。不单独保留的话，工作状态面板完全看不到后台任务 ——
    /// 实测一轮 `background=true` 的 bash 调用在骨架里只剩 `stepCount: 1`。
    #[test]
    fn skeleton_keeps_background_jobs() {
        let messages = vec![
            json!({ "type": "user", "id": "u1", "text": "起个后台任务", "time": { "created": 1000u64 } }),
            json!({
                "type": "assistant", "id": "a1",
                "time": { "created": 1001u64, "completed": 2000u64 },
                "content": [
                    {
                        "type": "tool",
                        "name": "bash",
                        "state": {
                            "status": "completed",
                            "input": { "command": "sleep 120 && echo LONGDONE", "background": true },
                            "metadata": { "shellID": "sh_real1", "status": "running" }
                        }
                    },
                    { "type": "text", "text": "已在后台启动" }
                ]
            }),
        ];
        let skeleton = skeleton_of(messages);
        let jobs = skeleton[0]["backgroundJobs"]
            .as_array()
            .expect("backgroundJobs must be present");
        assert_eq!(jobs.len(), 1);
        assert_eq!(jobs[0]["id"], "sh_real1");
        assert_eq!(jobs[0]["kind"], "shell");
        assert_eq!(jobs[0]["title"], "sleep 120 && echo LONGDONE");
        // 已定稿轮次 + 没有完成通知 → unknown（交给前端用 shell.list 校正），
        // 不能武断写成 completed：实测那样会把仍在跑的任务显示成已结束
        assert_eq!(jobs[0]["status"], "unknown");
    }

    /// 未定稿轮次里的后台任务应标成 running。
    ///
    /// 这一轮还在生成，任务几乎必然仍在跑；写成 completed 会让用户在任务
    /// 刚开始时就看到「已结束」。
    #[test]
    fn skeleton_marks_background_jobs_of_unsettled_turn_as_running() {
        let messages = vec![
            json!({ "type": "user", "id": "u1", "text": "起个后台任务", "time": { "created": 1000u64 } }),
            json!({
                "type": "assistant", "id": "a1",
                // 没有 completed → 该轮未定稿
                "time": { "created": 1001u64 },
                "content": [
                    {
                        "type": "tool",
                        "name": "bash",
                        "state": {
                            "status": "completed",
                            "input": { "command": "for i in 1 2 3; do echo x; sleep 9; done", "background": true },
                            "metadata": { "shellID": "sh_live" }
                        }
                    }
                ]
            }),
        ];
        let skeleton = skeleton_of(messages);
        let jobs = skeleton[0]["backgroundJobs"]
            .as_array()
            .expect("backgroundJobs must be present");
        assert_eq!(jobs[0]["id"], "sh_live");
        assert_eq!(jobs[0]["status"], "running");
    }

    /// 命令正文里出现的模板字符串不能被当成后台任务。
    ///
    /// 模型经常把源码粘贴进 shell 命令，而源码里就有
    /// `Command moved to the background (shell ID: ${shellID})` 这样的模板。
    /// 回退解析（从输出文本里认回执）若不校验格式，就会把 `${shellID}`
    /// 收成一条任务，面板上出现一条永远点不开的幽灵任务。
    #[test]
    fn skeleton_ignores_template_placeholders_in_command_text() {
        let messages = vec![
            json!({ "type": "user", "id": "u1", "text": "看下源码", "time": { "created": 1000u64 } }),
            json!({
                "type": "assistant", "id": "a1",
                "time": { "created": 1001u64, "completed": 2000u64 },
                "content": [
                    {
                        "type": "tool",
                        "name": "bash",
                        "state": {
                            "status": "completed",
                            // 命令里粘贴了含模板字符串的源码，且没有 metadata.shellID
                            "input": { "command": "grep -n 'shell ID: ' src/foo.ts" },
                            "content": [{
                                "type": "text",
                                "text": "Command moved to the background (shell ID: ${shellID})."
                            }]
                        }
                    },
                    { "type": "text", "text": "找到了" }
                ]
            }),
        ];
        let skeleton = skeleton_of(messages);
        assert!(
            skeleton[0].get("backgroundJobs").is_none(),
            "模板占位符不应被当成后台任务: {:?}",
            skeleton[0].get("backgroundJobs")
        );
    }

    /// 完成通知（synthetic）必须把后台任务状态从 unknown 纠正为终态。
    ///
    /// 通知在消息流里出现在该轮**之后**。若先 flush 该轮再处理 synthetic，
    /// 任务状态已经落盘成启动时的 `unknown`，刷新后界面永远显示「状态未知」。
    #[test]
    fn skeleton_reconciles_background_job_status_from_synthetic() {
        let messages = vec![
            json!({ "type": "user", "id": "u1", "text": "起个后台任务", "time": { "created": 1000u64 } }),
            json!({
                "type": "assistant", "id": "a1",
                "time": { "created": 1001u64, "completed": 2000u64 },
                "content": [
                    {
                        "type": "tool",
                        "name": "bash",
                        "state": {
                            "status": "completed",
                            "input": { "command": "sleep 8 && echo SHORTDONE", "background": true },
                            "metadata": { "shellID": "sh_1", "status": "running" }
                        }
                    },
                    { "type": "text", "text": "已启动" }
                ]
            }),
            // 完成通知：出现在该轮之后
            json!({
                "type": "synthetic", "id": "syn1",
                "time": { "created": 3000u64 },
                "text": "<shell id=\"sh_1\" state=\"completed\" command=\"sleep 8 && echo SHORTDONE\">\nSHORTDONE\n</shell>"
            }),
        ];
        let skeleton = skeleton_of(messages);
        let jobs = skeleton[0]["backgroundJobs"].as_array().expect("jobs");
        assert_eq!(jobs[0]["id"], "sh_1");
        assert_eq!(jobs[0]["status"], "completed");
    }

    /// 普通（前台）工具不应被误判成后台任务。
    #[test]
    fn skeleton_ignores_foreground_tools() {
        let messages = vec![
            json!({ "type": "user", "id": "u1", "text": "ls", "time": { "created": 1000u64 } }),
            json!({
                "type": "assistant", "id": "a1",
                "time": { "created": 1001u64, "completed": 2000u64 },
                "content": [
                    {
                        "type": "tool",
                        "name": "bash",
                        "state": {
                            "status": "completed",
                            "input": { "command": "ls" },
                            "metadata": { "truncated": false }
                        }
                    },
                    { "type": "text", "text": "列出来了" }
                ]
            }),
        ];
        let skeleton = skeleton_of(messages);
        assert!(skeleton[0].get("backgroundJobs").is_none());
    }

    /// 骨架：未定稿的轮次要打上 `settled: false`。
    ///
    /// 客户端据此把这一轮排除在骨架之外，改用完整消息拉取 —— 骨架给的是
    /// 此刻的截断快照，而后续 delta 是 ephemeral 不重放，无法判断重叠，
    /// 直接拼会重复或错位。
    #[test]
    fn skeleton_marks_unsettled_turn() {
        let messages = vec![
            json!({ "type": "user", "id": "u1", "text": "第一轮", "time": { "created": 1000u64 } }),
            json!({
                "type": "assistant", "id": "a1",
                "time": { "created": 1001u64, "completed": 2000u64 },
                "content": [{ "type": "text", "text": "已完成的回复" }]
            }),
            json!({ "type": "user", "id": "u2", "text": "正在跑的一轮", "time": { "created": 3000u64 } }),
            json!({
                "type": "assistant", "id": "a2",
                "time": { "created": 3001u64 },
                "content": [{ "type": "text", "text": "写了一半" }]
            })
        ];
        let skeleton = skeleton_of(messages);
        assert_eq!(skeleton.len(), 2);
        // 第一轮已定稿 → 不带 settled 字段（缺省视为已定稿）
        assert_eq!(skeleton[0]["id"], "u1");
        assert!(skeleton[0].get("settled").is_none());
        // 第二轮仍在生成 → settled=false
        assert_eq!(skeleton[1]["id"], "u2");
        assert_eq!(skeleton[1]["settled"], false);
    }

    /// 骨架：完全没有正文的轮次，reply 为 null（而不是伪造空串）。
    #[test]
    fn skeleton_emits_null_reply_when_no_text() {
        let messages = vec![
            json!({ "type": "user", "id": "u1", "text": "跑一下" }),
            json!({
                "type": "assistant", "id": "a1",
                "content": [{ "type": "tool", "name": "bash", "state": { "status": "completed" } }]
            })
        ];
        let skeleton = skeleton_of(messages);
        assert_eq!(skeleton[0]["reply"], Value::Null);
        assert_eq!(skeleton[0]["stats"]["stepCount"], 1);
    }

    /// 骨架：用户附件只留元信息，base64 正文一律不进骨架。
    #[test]
    fn skeleton_strips_attachment_bodies() {
        let messages = vec![json!({
            "type": "user", "id": "u1", "text": "看图",
            "files": [{ "data": "AAAA".repeat(1000), "mime": "image/png", "name": "a.png" }]
        })];
        let skeleton = skeleton_of(messages);
        let out = serde_json::to_string(&skeleton).unwrap();
        assert!(!out.contains("AAAAAAAAAAAA"), "附件正文不应出现在骨架里");
        let file = &skeleton[0]["user"]["files"][0];
        assert_eq!(file["mime"], "image/png");
        assert_eq!(file["name"], "a.png");
        // 4000 个 'A' 的 base64 解码后是 3000 字节
        assert_eq!(file["sizeBytes"], 3000);
    }

    /// 骨架：保留 idle / compaction / agent-switched 这类权威状态，丢弃对话无关系统消息。
    #[test]
    fn skeleton_keeps_authoritative_markers_only() {
        let messages = vec![
            json!({ "type": "user", "id": "u1", "text": "hi" }),
            json!({ "type": "assistant", "id": "a1", "content": [{ "type": "text", "text": "ok" }] }),
            json!({ "type": "idle", "id": "i1", "outcome": "succeeded", "time": { "created": 5 } }),
            json!({ "type": "compaction", "id": "c1", "status": "completed" }),
            json!({ "type": "system", "id": "s1", "text": "tool catalog changed" }),
            json!({ "type": "synthetic", "id": "y1", "text": "noise" })
        ];
        let skeleton = skeleton_of(messages);
        let types: Vec<&str> = skeleton
            .iter()
            .map(|item| item["type"].as_str().unwrap())
            .collect();
        assert_eq!(types, vec!["turn", "idle", "compaction"]);
    }

    /// 骨架：改动文件清单（含增删行数）保留，供最终回复下的 DiffChips 渲染。
    #[test]
    fn skeleton_collects_changed_files() {
        let messages = vec![
            json!({ "type": "user", "id": "u1", "text": "改" }),
            json!({
                "type": "assistant", "id": "a1",
                "content": [
                    { "type": "text", "text": "改完了" },
                    {
                        "type": "tool", "name": "edit",
                        "state": {
                            "status": "completed",
                            "metadata": {
                                "filediff": { "filePath": "src/a.cs", "additions": 3, "deletions": 1 }
                            }
                        }
                    }
                ]
            })
        ];
        let skeleton = skeleton_of(messages);
        let files = skeleton[0]["changedFiles"].as_array().unwrap();
        assert_eq!(files.len(), 1);
        assert_eq!(files[0]["path"], "src/a.cs");
        assert_eq!(files[0]["additions"], 3);
        assert_eq!(files[0]["deletions"], 1);
    }

    /// 诊断用：把骨架结果写到 AIAGENT_PROJECTION_DUMP 指定的路径，
    /// 供前端 e2e 用真实数据联调。默认忽略。
    #[test]
    #[ignore]
    fn dump_skeleton_sample() {
        let path = match std::env::var("AIAGENT_PROJECTION_SAMPLE") {
            Ok(value) => value,
            Err(_) => return,
        };
        let out = match std::env::var("AIAGENT_PROJECTION_DUMP") {
            Ok(value) => value,
            Err(_) => return,
        };
        let raw = std::fs::read(&path).expect("read sample");
        let parsed: Vec<Value> = serde_json::from_slice(&raw).expect("parse sample");
        let skeleton = project_v2_turn_skeleton(&parsed).unwrap();
        std::fs::write(&out, &skeleton).expect("write dump");
        println!("dumped {} bytes to {}", skeleton.len(), out);
    }

    /// 骨架体积验收：拿真实样本跑一遍，确认「轮次骨架」比「剥大字段」小一个量级。
    /// 需要 AIAGENT_PROJECTION_SAMPLE 指向真实 message 数组 JSON。
    #[test]
    #[ignore]
    fn real_sample_skeleton_shrinks_orders_of_magnitude() {
        let path = match std::env::var("AIAGENT_PROJECTION_SAMPLE") {
            Ok(value) => value,
            Err(_) => return,
        };
        let raw = std::fs::read(&path).expect("read sample");
        let parsed: Vec<Value> = serde_json::from_slice(&raw).expect("parse sample");

        let full = project_v2_message_list(&parsed).unwrap();
        let skeleton = project_v2_turn_skeleton(&parsed).unwrap();
        println!(
            "原始 {} B -> 剥字段 {} B -> 骨架 {} B（缩小 {:.0}x）",
            raw.len(),
            full.len(),
            skeleton.len(),
            full.len() as f64 / skeleton.len().max(1) as f64
        );
        assert!(
            skeleton.len() * 10 < full.len(),
            "骨架应至少比剥字段小一个量级"
        );
        assert!(
            !String::from_utf8_lossy(&skeleton).contains("\"data\":\"iVBOR"),
            "骨架里不应残留 base64 图片正文"
        );
    }
}
