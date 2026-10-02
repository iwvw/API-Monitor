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
}
