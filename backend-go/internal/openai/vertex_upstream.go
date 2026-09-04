package openai

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Vertex AI（Google Cloud）上游适配层。协议为 generateContent / streamGenerateContent：
//
//	baseURL: https://{region}-aiplatform.googleapis.com/v1/publishers/google
//	         （global 区域省略 region 前缀，如 https://aiplatform.googleapis.com/v1/publishers/google）
//	非流式:  POST {base}/models/{model}:generateContent
//	流式:    POST {base}/models/{model}:streamGenerateContent?alt=sse（SSE data 行，增量文本）
//	鉴权:    x-goog-api-key（API Key 直填，project 由 key 推断）
//	请求体:  camelCase（contents/systemInstruction/tools/toolConfig/generationConfig）
//
// 与 AI Studio（Interactions API，snake_case）不同：Vertex 不提供模型列表端点，
// 模型需手动添加；请求体不含 stream 字段（流式由 URL 端点决定）。

// normalizeVertexBaseURL 归一化 Vertex AI 上游 baseURL：只补 scheme、去尾部斜杠。
// 不追加 OpenAI 风格的 /v1 版本路径（用户按区域填写的 baseURL 已含 /v1/publishers/google）。
func normalizeVertexBaseURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, "/")
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	return u
}

// vertexModelURL 返回 Vertex 生成模型方法地址：{base}/models/{model}:{action}。
func vertexModelURL(baseURL, model, action string) string {
	base := strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	return fmt.Sprintf("%s/models/%s:%s", base, url.PathEscape(model), action)
}

// vertexGenerateURL 返回 Vertex 生成模型方法地址。
// 非流式：{base}/models/{model}:generateContent
// 流式：  {base}/models/{model}:streamGenerateContent?alt=sse
// （streamGenerateContent 不带 alt=sse 时返回 JSON 数组流而非 SSE 增量行，
// 无法按行解析，故显式指定 alt=sse。）
func vertexGenerateURL(baseURL, model string, stream bool) string {
	action := "generateContent"
	if stream {
		action = "streamGenerateContent?alt=sse"
	}
	return vertexModelURL(baseURL, model, action)
}

// openAIChatToVertex 将 OpenAI chat.completions 请求体转换为 Vertex AI
// generateContent 请求体（camelCase）。映射：
//   - system 消息 → systemInstruction.parts（文本数组）
//   - user 消息 → contents 中 role=user 的 content，parts 为文本/内联媒体
//   - assistant 消息（含 tool_calls）→ contents 中 role=model 的 content，
//     tool_calls 转为 functionCall parts（args 为对象）
//   - tool 消息 → contents 中 role=user 的 functionResponse part
//   - tools → tools[].functionDeclarations（参数 JSON Schema 直通）
//   - tool_choice → toolConfig.functionCallingConfig
//   - temperature/top_p/top_k/max_tokens/stop → generationConfig（camelCase）
//
// 与 Interactions 不同：无顶层 model（模型在 URL）、无 store/stream 字段。
func openAIChatToVertex(body map[string]interface{}) (map[string]interface{}, error) {
	out := map[string]interface{}{}

	var contents []interface{}
	var systemParts []interface{}
	// tool_call_id → 函数名映射：functionResponse 需要 name 与对应 functionCall 一致，
	// OpenAI tool 消息只有 tool_call_id，须从 assistant 消息收集。
	callName := map[string]string{}
	if rawMessages, ok := body["messages"].([]interface{}); ok {
		for _, raw := range rawMessages {
			msg, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if toolCalls, ok := msg["tool_calls"].([]interface{}); ok {
				for _, tc := range toolCalls {
					tcMap, ok := tc.(map[string]interface{})
					if !ok {
						continue
					}
					fn, _ := tcMap["function"].(map[string]interface{})
					name, _ := fn["name"].(string)
					id, _ := tcMap["id"].(string)
					if name != "" && id != "" {
						callName[id] = name
					}
				}
			}
		}
	}

	if rawMessages, ok := body["messages"].([]interface{}); ok {
		for _, raw := range rawMessages {
			msg, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			role, _ := msg["role"].(string)
			switch role {
			case "system":
				if t := contentToPlainText(msg["content"]); t != "" {
					systemParts = append(systemParts, map[string]interface{}{"text": t})
				}
			case "assistant":
				var parts []interface{}
				if toolCalls, ok := msg["tool_calls"].([]interface{}); ok {
					for _, tc := range toolCalls {
						tcMap, ok := tc.(map[string]interface{})
						if !ok {
							continue
						}
						fn, _ := tcMap["function"].(map[string]interface{})
						name, _ := fn["name"].(string)
						if name == "" {
							continue
						}
						fc := map[string]interface{}{"name": name, "args": map[string]interface{}{}}
						if argsStr, _ := fn["arguments"].(string); argsStr != "" {
							var argsObj map[string]interface{}
							if err := json.Unmarshal([]byte(argsStr), &argsObj); err == nil && argsObj != nil {
								fc["args"] = argsObj
							}
						}
						// Gemini 2.5/3.x 与 Vertex 要求历史中的 functionCall 带 thought_signature，
						// 缺少会导致 400 INVALID_ARGUMENT（"Function call is missing a thought_signature"）。
						// 客户端通过 OpenAI 格式重放历史时不带此字段，在此自动填补旁路签名（与 new-api 一致）。
						fcPart := map[string]interface{}{
							"functionCall":     fc,
							"thoughtSignature": "context_engineering_is_the_way_to_go",
						}
						parts = append(parts, fcPart)
					}
				}
				parts = append(parts, openAIContentToVertexParts(msg["content"])...)
				if len(parts) > 0 {
					contents = append(contents, map[string]interface{}{"role": "model", "parts": parts})
				}
			case "tool":
				// Vertex/Gemini 强约束：一个 model turn 中的多个 functionCall 必须在紧随其后的
				// 单个 user turn 中以等量的 functionResponse parts 一同返回
				// （"number of function response parts is equal to the number of function call parts"）。
				// OpenAI 协议中多个并发工具调用的返回是以多个独立的 role:"tool" 消息分别传入的，
				// 若直接每次建一个 user content 会导致多轮 user 且每轮只有一个 response，触发 400。
				// 做法：若前一个 content 已经是包含 functionResponse 的 user turn，则追加到该 turn 的 parts 中。
				if part := openAIToolResultToVertexPart(msg, callName); part != nil {
					if len(contents) > 0 {
						if lastContent, ok := contents[len(contents)-1].(map[string]interface{}); ok && lastContent["role"] == "user" {
							lastParts, _ := lastContent["parts"].([]interface{})
							hasFuncResp := false
							for _, p := range lastParts {
								if pm, ok := p.(map[string]interface{}); ok && pm["functionResponse"] != nil {
									hasFuncResp = true
									break
								}
							}
							if hasFuncResp {
								lastContent["parts"] = append(lastParts, part)
								continue
							}
						}
					}
					contents = append(contents, map[string]interface{}{"role": "user", "parts": []interface{}{part}})
				}
			default:
				parts := openAIContentToVertexParts(msg["content"])
				if len(parts) == 0 {
					continue
				}
				contents = append(contents, map[string]interface{}{"role": "user", "parts": parts})
			}
		}
	}

	if len(systemParts) > 0 {
		out["systemInstruction"] = map[string]interface{}{"parts": systemParts}
	}
	// Vertex/Gemini 拒绝以 role=model 开头的 contents（"first turn must be user"）。
	// OpenAI 客户端多轮重放偶发以 assistant 开头，此时前插一条空 user turn 保持可用。
	if len(contents) > 0 {
		if first, ok := contents[0].(map[string]interface{}); ok && first["role"] == "model" {
			contents = append([]interface{}{map[string]interface{}{"role": "user", "parts": []interface{}{map[string]interface{}{"text": "ok"}}}}, contents...)
		}
	}
	if len(contents) == 0 {
		return nil, fmt.Errorf("vertex upstream: no user messages")
	}
	out["contents"] = contents

	if tools := openAIToolsToVertex(body["tools"]); len(tools) > 0 {
		out["tools"] = tools
	}
	if tc := openAIToolChoiceToVertex(body["tool_choice"]); tc != nil {
		out["toolConfig"] = tc
	}
	if cfg := vertexGenerationConfig(body); len(cfg) > 0 {
		out["generationConfig"] = cfg
	}
	return out, nil
}

// openAIContentToVertexParts 将 OpenAI 消息 content 转换为 Vertex generateContent parts。
// 支持：纯文本、文本块、image_url（data URL 内联 inlineData / http URL 透传 fileData）、
// input_audio 内联、file。
func openAIContentToVertexParts(content interface{}) []interface{} {
	var parts []interface{}
	switch c := content.(type) {
	case string:
		if strings.TrimSpace(c) != "" {
			parts = append(parts, map[string]interface{}{"text": c})
		}
	case []interface{}:
		for _, part := range c {
			pm, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			switch pm["type"] {
			case "text":
				if t, ok := pm["text"].(string); ok && t != "" {
					parts = append(parts, map[string]interface{}{"text": t})
				}
			case "image_url":
				if g := openAIImageURLToVertexPart(pm["image_url"]); g != nil {
					parts = append(parts, g)
				}
			case "input_audio":
				if g := openAIAudioToVertexPart(pm["input_audio"]); g != nil {
					parts = append(parts, g)
				}
			case "file":
				if g := openAIFileToVertexPart(pm); g != nil {
					parts = append(parts, g)
				}
			}
		}
	}
	return parts
}

// openAIImageURLToVertexPart 将 OpenAI image_url 转换为 Vertex inlineData / fileData part。
func openAIImageURLToVertexPart(imageURL interface{}) map[string]interface{} {
	u := ""
	switch v := imageURL.(type) {
	case string:
		u = v
	case map[string]interface{}:
		if s, ok := v["url"].(string); ok {
			u = s
		}
	}
	u = strings.TrimSpace(u)
	if u == "" {
		return nil
	}
	if strings.HasPrefix(u, "data:") {
		mime, data, ok := parseDataURL(u)
		if !ok {
			return nil
		}
		return map[string]interface{}{
			"inlineData": map[string]interface{}{"mimeType": mime, "data": data},
		}
	}
	return map[string]interface{}{
		"fileData": map[string]interface{}{"mimeType": "image/png", "fileUri": u},
	}
}

// openAIAudioToVertexPart 将 OpenAI input_audio（base64）转换为 Vertex inlineData。
func openAIAudioToVertexPart(inputAudio interface{}) map[string]interface{} {
	am, ok := inputAudio.(map[string]interface{})
	if !ok {
		return nil
	}
	data, _ := am["data"].(string)
	format, _ := am["format"].(string)
	if data == "" {
		return nil
	}
	mime := "audio/wav"
	switch format {
	case "mp3":
		mime = "audio/mpeg"
	case "wav":
		mime = "audio/wav"
	case "aac":
		mime = "audio/aac"
	case "flac":
		mime = "audio/flac"
	case "pcm":
		mime = "audio/L16"
	case "opus":
		mime = "audio/opus"
	}
	return map[string]interface{}{
		"inlineData": map[string]interface{}{"mimeType": mime, "data": data},
	}
}

// openAIFileToVertexPart 将 OpenAI file part（url）转换为 Vertex fileData。
func openAIFileToVertexPart(pm map[string]interface{}) map[string]interface{} {
	fm, ok := pm["file"].(map[string]interface{})
	if !ok {
		return nil
	}
	uri, _ := fm["file_id"].(string)
	mime, _ := fm["mime_type"].(string)
	if uri == "" {
		return nil
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	return map[string]interface{}{
		"fileData": map[string]interface{}{"mimeType": mime, "fileUri": uri},
	}
}

// openAIToolResultToVertexPart 将 OpenAI tool 消息转换为 Vertex functionResponse part。
// name 通过 callName 映射（call_id → 函数名），response 为 {output: text} 对象。
func openAIToolResultToVertexPart(msg map[string]interface{}, callName map[string]string) map[string]interface{} {
	callID, _ := msg["tool_call_id"].(string)
	text := contentToPlainText(msg["content"])
	if text == "" {
		text = "ok"
	}
	name := callName[callID]
	if name == "" {
		name = callID
	}
	return map[string]interface{}{
		"functionResponse": map[string]interface{}{
			"name":     name,
			"response": map[string]interface{}{"output": text},
		},
	}
}

// openAIToolsToVertex 将 OpenAI tools 转换为 Vertex tools[].functionDeclarations。
func openAIToolsToVertex(tools interface{}) []interface{} {
	rawTools, ok := tools.([]interface{})
	if !ok {
		return nil
	}
	var decls []interface{}
	for _, raw := range rawTools {
		tm, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		fn, ok := tm["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		decl := map[string]interface{}{"name": name}
		if desc, ok := fn["description"].(string); ok && desc != "" {
			decl["description"] = desc
		}
		if params, ok := fn["parameters"]; ok && params != nil {
			decl["parameters"] = sanitizeVertexFunctionParameters(params, 0)
		}
		decls = append(decls, decl)
	}
	if len(decls) == 0 {
		return nil
	}
	return []interface{}{map[string]interface{}{"functionDeclarations": decls}}
}

// vertexOpenAPISchemaAllowedFields 是 Vertex/Gemini functionDeclaration parameters 允许的 OpenAPI 字段。
// Vertex 严格遵循子集，遇到 $schema、exclusiveMinimum、additionalProperties、title 等未声明字段
// 会直接返回 400 INVALID_ARGUMENT（"Cannot find field"）。
var vertexOpenAPISchemaAllowedFields = map[string]struct{}{
	"anyOf":            {},
	"default":          {},
	"description":      {},
	"enum":             {},
	"example":          {},
	"format":           {},
	"items":            {},
	"maxItems":         {},
	"maxLength":        {},
	"maxProperties":    {},
	"maximum":          {},
	"minItems":         {},
	"minLength":        {},
	"minProperties":    {},
	"minimum":          {},
	"nullable":         {},
	"pattern":          {},
	"properties":       {},
	"propertyOrdering": {},
	"required":         {},
	"title":            {},
	"type":             {},
}

const vertexFunctionSchemaMaxDepth = 32

// sanitizeVertexFunctionParameters 递归清洗 OpenAPI schema，只保留 Vertex 接受的字段，
// 过滤掉 JSON Schema 标准中常见但在 Vertex 中被拒绝的字段（如 $schema, exclusiveMinimum 等）。
// 特别地：Vertex 规定当指定 anyOf 时，不能与 type/properties/items/format/enum/default 等结构定义字段共存
// （"When using any_of, it must be the only field set"），因此出现 anyOf 时必须删除同层的 type/properties/items/format/enum/default。
func sanitizeVertexFunctionParameters(params interface{}, depth int) interface{} {
	if params == nil || depth >= vertexFunctionSchemaMaxDepth {
		return params
	}
	switch v := params.(type) {
	case map[string]interface{}:
		cleaned := make(map[string]interface{}, len(v))
		for k, val := range v {
			if _, ok := vertexOpenAPISchemaAllowedFields[k]; ok {
				cleaned[k] = val
			}
		}

		// Vertex 约束：anyOf 存在时，不能并存除 title / description 外的任何验证或类型约束字段
		// （如 type, minimum, maximum, minItems, maxItems, minLength, maxLength, format, enum, default, properties, items 等），
		// 否则触发 400 INVALID_ARGUMENT（"schema specified other fields alongside any_of. When using any_of, it must be the only field set"）。
		if anyOf, ok := cleaned["anyOf"].([]interface{}); ok && len(anyOf) > 0 {
			res := map[string]interface{}{}
			if desc, ok := cleaned["description"]; ok {
				res["description"] = desc
			}
			if title, ok := cleaned["title"]; ok {
				res["title"] = title
			}
			cleanedAnyOf := make([]interface{}, len(anyOf))
			for i, item := range anyOf {
				cleanedAnyOf[i] = sanitizeVertexFunctionParameters(item, depth+1)
			}
			res["anyOf"] = cleanedAnyOf
			return res
		}

		if props, ok := cleaned["properties"].(map[string]interface{}); ok && props != nil {
			cleanedProps := make(map[string]interface{}, len(props))
			for propName, propValue := range props {
				cleanedProps[propName] = sanitizeVertexFunctionParameters(propValue, depth+1)
			}
			cleaned["properties"] = cleanedProps
		}
		if items, ok := cleaned["items"].(map[string]interface{}); ok && items != nil {
			cleaned["items"] = sanitizeVertexFunctionParameters(items, depth+1)
		} else if itemsArr, ok := cleaned["items"].([]interface{}); ok && len(itemsArr) > 0 {
			cleaned["items"] = sanitizeVertexFunctionParameters(itemsArr[0], depth+1)
		}

		// Vertex 约束：type 为 array / ARRAY 时，items 字段是必须提供的
		// （否则报错 "parameters.properties[x].items: missing field" 或 "any_of[i].items: missing field"）。
		// 若上游/客户端定义了 array 类型但未提供 items（如松散类型/未指定元素类型的数组），默认补空对象 items。
		if tVal, ok := cleaned["type"].(string); ok && strings.EqualFold(tVal, "array") {
			if _, hasItems := cleaned["items"]; !hasItems {
				cleaned["items"] = map[string]interface{}{}
			}
		}

		return cleaned
	case []interface{}:
		cleanedArr := make([]interface{}, len(v))
		for i, item := range v {
			cleanedArr[i] = sanitizeVertexFunctionParameters(item, depth+1)
		}
		return cleanedArr
	default:
		return params
	}
}

// openAIToolChoiceToVertex 将 OpenAI tool_choice 映射为 Vertex toolConfig.functionCallingConfig。
// 未显式指定 tool_choice 时返回 nil（默认 AUTO，不附加参数）。
func openAIToolChoiceToVertex(toolChoice interface{}) map[string]interface{} {
	if toolChoice == nil {
		return nil
	}
	switch tc := toolChoice.(type) {
	case string:
		switch tc {
		case "none":
			return map[string]interface{}{"functionCallingConfig": map[string]interface{}{"mode": "NONE"}}
		case "required":
			return map[string]interface{}{"functionCallingConfig": map[string]interface{}{"mode": "ANY"}}
		case "auto":
			return map[string]interface{}{"functionCallingConfig": map[string]interface{}{"mode": "AUTO"}}
		default:
			return nil
		}
	case map[string]interface{}:
		if t, ok := tc["type"].(string); ok && t == "function" {
			if fn, ok := tc["function"].(map[string]interface{}); ok {
				if name, ok := fn["name"].(string); ok && name != "" {
					return map[string]interface{}{
						"functionCallingConfig": map[string]interface{}{
							"mode":                 "ANY",
							"allowedFunctionNames": []interface{}{name},
						},
					}
				}
			}
		}
	}
	return nil
}

// vertexGenerationConfig 从 OpenAI 请求体提取 Vertex generationConfig（camelCase）。
func vertexGenerationConfig(body map[string]interface{}) map[string]interface{} {
	cfg := map[string]interface{}{}
	if v, ok := body["temperature"].(float64); ok {
		cfg["temperature"] = v
	}
	if v, ok := body["top_p"].(float64); ok {
		cfg["topP"] = v
	}
	if v, ok := body["top_k"].(float64); ok {
		cfg["topK"] = int(v)
	}
	maxTokens := 0
	if v, ok := body["max_tokens"].(float64); ok {
		maxTokens = int(v)
	}
	if v, ok := body["max_completion_tokens"].(float64); ok {
		maxTokens = int(v)
	}
	if maxTokens > 0 {
		cfg["maxOutputTokens"] = maxTokens
	}
	if v, ok := body["stop"].([]interface{}); ok && len(v) > 0 {
		var stops []string
		for _, s := range v {
			if str, ok := s.(string); ok && str != "" {
				stops = append(stops, str)
			}
		}
		if len(stops) > 0 {
			cfg["stopSequences"] = stops
		}
	} else if v, ok := body["stop"].(string); ok && v != "" {
		cfg["stopSequences"] = []string{v}
	}
	// 候选数：OpenAI n（>1）→ candidateCount。多个候选时 Vertex 返回多候选数组，
	// 客户端期望 choices[0..n-1]；n<=1 时不设（默认单候选）。
	if v, ok := body["n"].(float64); ok && v > 1 {
		cfg["candidateCount"] = int(v)
	}
	if rf := vertexResponseFormat(body["response_format"]); len(rf) > 0 {
		for k, val := range rf {
			cfg[k] = val
		}
	}

	// 思维链配置（thinkingConfig）：
	// 1. Google 官方默认 includeThoughts=false，即只计 token 却不输出 thought part 文本。
	//    为让客户端（如 opencode、Cherry Studio、NextChat 等）能看到 reasoning_content，
	//    默认开启 includeThoughts: true。
	// 2. 支持通过 reasoning_effort (low/medium/high/none) 或 extra_body / thinking_budget 控制
	//    思考强度与预算。
	// 3. Gemini 3.x（gemini-3-pro/flash 等）在 Vertex 上改用 thinkingLevel 表达思考等级，
	//    thinkingBudget 仅 Gemini 2.5 系支持；按模型名自动选择，避免 3.x 忽略预算设置。
	thinkingCfg := map[string]interface{}{
		"includeThoughts": true,
	}
	modelName, _ := body["model"].(string)
	if effort, ok := body["reasoning_effort"].(string); ok {
		switch strings.ToLower(effort) {
		case "none":
			thinkingCfg["thinkingBudget"] = 0
		case "low":
			vertexSetThinkingLevel(thinkingCfg, modelName, 1024)
		case "medium":
			vertexSetThinkingLevel(thinkingCfg, modelName, 8192)
		case "high":
			vertexSetThinkingLevel(thinkingCfg, modelName, 24576)
		}
	}
	if tb, ok := body["thinking_budget"].(float64); ok {
		thinkingCfg["thinkingBudget"] = int(tb)
	}
	cfg["thinkingConfig"] = thinkingCfg

	if len(cfg) == 0 {
		return nil
	}
	return cfg
}

// vertexSetThinkingLevel 按模型版本写入 thinkingConfig 的思考等级字段：
//   - Gemini 3.x 模型 → thinkingLevel（"low"/"medium"/"high"），thinkingBudget 会被 3.x 忽略；
//   - 其余模型 → thinkingBudget（Gemini 2.5 体系）。
func vertexSetThinkingLevel(thinkingCfg map[string]interface{}, modelName string, budget int) {
	if gemini3XModel(modelName) {
		switch budget {
		case 1024:
			thinkingCfg["thinkingLevel"] = "low"
		case 8192:
			thinkingCfg["thinkingLevel"] = "medium"
		default:
			thinkingCfg["thinkingLevel"] = "high"
		}
		return
	}
	thinkingCfg["thinkingBudget"] = budget
}

// gemini3XModel 判断模型名是否为 Gemini 3.x 系（3-pro/3-flash/3.x 等）。
// 用于选择 Vertex thinkingConfig 的表达方式（thinkingLevel vs thinkingBudget）。
func gemini3XModel(model string) bool {
	lower := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(lower, "gemini-3") || strings.HasPrefix(lower, "gemini 3")
}

// vertexResponseFormat 将 OpenAI response_format 映射为 Vertex generationConfig 字段：
//   - json_object → responseMimeType=application/json；
//   - json_schema → responseMimeType=application/json + responseJsonSchema（结构化输出 schema）。
func vertexResponseFormat(rf interface{}) map[string]interface{} {
	rfm, ok := rf.(map[string]interface{})
	if !ok {
		return nil
	}
	rfType, _ := rfm["type"].(string)
	switch rfType {
	case "json_object":
		return map[string]interface{}{"responseMimeType": "application/json"}
	case "json_schema":
		out := map[string]interface{}{"responseMimeType": "application/json"}
		schemaObj, ok := rfm["json_schema"].(map[string]interface{})
		if !ok {
			return out
		}
		if s, ok := schemaObj["schema"].(map[string]interface{}); ok {
			out["responseJsonSchema"] = s
		}
		return out
	}
	return nil
}

// vertexIsBlockedResponse 判断转换后的响应体是否为安全拦截错误（prompt_blocked）。
// 供 relay 在写回前把 2xx 状态码提升为 400（对齐 new-api 的 prompt_blocked 语义）。
func vertexIsBlockedResponse(body []byte) bool {
	var parsed struct {
		Error struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	return parsed.Error.Type == "prompt_blocked" || parsed.Error.Code == "prompt_blocked"
}

// googleRetryAfterFromBody 解析 Google API 标准错误 body 中的限流恢复延迟，供
// rateLimitRetryWaitFor 等待 Vertex/Gemini 上游配额窗口使用。支持两种结构（对齐
// CLIProxyAPI json_retry_helpers.ParseRetryDelay）：
//   - error.details[].@type = "type.googleapis.com/google.rpc.RetryInfo"，字段
//     retryDelay（google.protobuf.Duration 文本，如 "1.5s" / "30s"）；
//   - error.details[].@type = "type.googleapis.com/google.rpc.ErrorInfo"，字段
//     metadata.quotaResetDelay（字符串时长，如 "30s"）。
//
// 解析失败或无延迟返回 nil（调用方回退 Retry-After 头/端点配置）。
func googleRetryAfterFromBody(body []byte) *time.Duration {
	if len(body) == 0 {
		return nil
	}
	var parsed struct {
		Error struct {
			Details []struct {
				Type     string                 `json:"@type"`
				Retry    string                 `json:"retryDelay"`
				Metadata map[string]interface{} `json:"metadata"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	for _, d := range parsed.Error.Details {
		if strings.HasSuffix(d.Type, "google.rpc.RetryInfo") && d.Retry != "" {
			if dur, err := time.ParseDuration(d.Retry); err == nil && dur > 0 {
				return &dur
			}
		}
		if strings.HasSuffix(d.Type, "google.rpc.ErrorInfo") {
			if raw, ok := d.Metadata["quotaResetDelay"]; ok {
				if s, ok := raw.(string); ok && s != "" {
					if dur, err := time.ParseDuration(s); err == nil && dur > 0 {
						return &dur
					}
				}
			}
		}
	}
	return nil
}

// vertexUsage 表示 Vertex generateContent 的 usageMetadata 计费字段。
type vertexUsage struct {
	PromptTokenCount           int `json:"promptTokenCount"`
	ToolUsePromptTokenCount    int `json:"toolUsePromptTokenCount"`
	CandidatesTokenCount       int `json:"candidatesTokenCount"`
	TotalTokenCount            int `json:"totalTokenCount"`
	CachedContentTokenCount    int `json:"cachedContentTokenCount"`
	ThoughtsTokenCount         int `json:"thoughtsTokenCount"`
}

// vertexOpenAIUsage 将 Vertex usageMetadata 换算为 OpenAI usage 结构。对齐 Google 计费口径：
//   - prompt = promptTokenCount + toolUsePromptTokenCount（工具调用计 prompt）
//   - completion = candidatesTokenCount + thoughtsTokenCount（思考 token 计入产出）
//   - reasoning_tokens 细分到 completion_tokens_details；cached_tokens 细分到 prompt_tokens_details
//
// 上游偶发返回负数/溢出计数（预发布模型或服务端统计异常），任何负值都钳制为 0，
// 避免污染调用日志与费用核算。
func vertexOpenAIUsage(u *vertexUsage) map[string]interface{} {
	base := map[string]interface{}{
		"prompt_tokens":     0,
		"completion_tokens": 0,
		"total_tokens":      0,
	}
	if u == nil {
		return base
	}
	prompt := safeTokenSum(u.PromptTokenCount, u.ToolUsePromptTokenCount)
	completion := safeTokenSum(u.CandidatesTokenCount, u.ThoughtsTokenCount)
	base["prompt_tokens"] = prompt
	base["completion_tokens"] = completion
	if u.TotalTokenCount > 0 {
		base["total_tokens"] = u.TotalTokenCount
	} else {
		base["total_tokens"] = prompt + completion
	}
	if u.CachedContentTokenCount > 0 {
		base["prompt_tokens_details"] = map[string]interface{}{"cached_tokens": u.CachedContentTokenCount}
	}
	if u.ThoughtsTokenCount > 0 {
		base["completion_tokens_details"] = map[string]interface{}{"reasoning_tokens": u.ThoughtsTokenCount}
	}
	return base
}

// safeTokenSum 返回两个 token 计数的和。负值按 0 计、溢出按 MaxInt 钳制
// （对齐 CLIProxyAPI safeUsageTokenSum：上游返回负数/溢出应标记 quality
// inconsistent 而非污染计费；此处仅做钳制，不改变 totalTokenCount 透传语义）。
func safeTokenSum(a, b int) int {
	if a < 0 {
		a = 0
	}
	if b < 0 {
		b = 0
	}
	if a > 0 && b > 0 && a > int(^uint(0)>>1)-b {
		return int(^uint(0) >> 1)
	}
	return a + b
}

// vertexFinishReason 将 Vertex finishReason 映射为 OpenAI finish_reason。
// 完整映射对齐 new-api：仅 STOP/空 → stop；MAX_TOKENS → length；
// 其余（SAFETY/RECITATION/BLOCKLIST/PROHIBITED_CONTENT/SPII/MALFORMED_FUNCTION_CALL）→ content_filter。
func vertexFinishReason(fr string) string {
	switch fr {
	case "", "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "MALFORMED_FUNCTION_CALL":
		return "tool_calls"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "OTHER", "UNFINISHED":
		return "content_filter"
	default:
		return "content_filter"
	}
}

// vertexToOpenAIChat 将 Vertex generateContent 非流式响应转换为 OpenAI
// chat.completions 响应。candidates[0].content.parts 的文本拼接为 content，
// thought part 归入 reasoning_content，functionCall parts 归并为 tool_calls。
func vertexToOpenAIChat(body []byte, fallbackModel string) ([]byte, error) {
	var resp struct {
		Candidates []struct {
			Content struct {
				Role  string `json:"role"`
				Parts []struct {
					Text         string `json:"text"`
					Thought      bool   `json:"thought"`
					FunctionCall *struct {
						Name string                 `json:"name"`
						Args map[string]interface{} `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
			Index        int    `json:"index"`
		} `json:"candidates"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		UsageMetadata vertexUsage `json:"usageMetadata"`
		ModelVersion  string      `json:"modelVersion"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	// 无候选且命中安全拦截：转成 OpenAI 语义化错误（prompt_blocked，HTTP 400），
	// 客户端可读的 blockReason 原文，避免「空响应」无从诊断。
	if len(resp.Candidates) == 0 && resp.PromptFeedback.BlockReason != "" {
		blocked, _ := json.Marshal(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "request blocked by Vertex AI: " + resp.PromptFeedback.BlockReason,
				"type":    "prompt_blocked",
				"code":    "prompt_blocked",
			},
		})
		return blocked, nil
	}

	model := resp.ModelVersion
	if model == "" {
		model = fallbackModel
	}

	message := map[string]interface{}{"role": "assistant", "content": ""}
	var toolCalls []interface{}
	var content strings.Builder
	var reasoning strings.Builder
	if len(resp.Candidates) > 0 {
		for _, part := range resp.Candidates[0].Content.Parts {
			if part.FunctionCall != nil {
				name := part.FunctionCall.Name
				argsBytes, _ := json.Marshal(part.FunctionCall.Args)
				if part.FunctionCall.Args == nil {
					argsBytes = []byte("{}")
				}
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   "call_" + strconv.FormatInt(time.Now().UnixNano(), 10),
					"type": "function",
					"function": map[string]interface{}{
						"name":      name,
						"arguments": string(argsBytes),
					},
				})
				continue
			}
			if part.Thought && part.Text != "" {
				reasoning.WriteString(part.Text)
				continue
			}
			if part.Text != "" {
				content.WriteString(part.Text)
			}
		}
	}
	message["content"] = content.String()
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	finishReason := "stop"
	if len(resp.Candidates) > 0 {
		finishReason = vertexFinishReason(resp.Candidates[0].FinishReason)
		if len(toolCalls) > 0 {
			finishReason = "tool_calls"
		}
	}

	usage := vertexOpenAIUsage(&resp.UsageMetadata)

	out := map[string]interface{}{
		"id":      openAICompletionID(""),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
		"usage": usage,
	}
	return json.Marshal(out)
}

// vertexSSETransformer 将 Vertex streamGenerateContent 流式 SSE（data 行，每个
// chunk 是当前已生成的增量文本/工具调用片段）转换为 OpenAI chat.completions
// 流式 chunk。finishReason 出现时输出收尾 chunk（finish_reason + usage）。
type vertexSSETransformer struct {
	model     string
	id        string
	created   int64
	started   bool
	finished  bool
	doneSent  bool
	finishStr string
	// toolCallsEmitted 标记已真正向流中输出过 tool_calls delta。
	// partialArgs 累积中途不得以 tool_calls 收尾；若流在参数完整前被读中断，
	// 从未发出任何 tool_calls delta，finish 不得以 tool_calls 收尾（避免
	// 「工具调用已结束但无工具可执行」的幻影收尾）。仅 delta 实际写出时置 true。
	toolCallsEmitted bool
	usage           *vertexUsage
	// partialToolByName 重组流式工具调用的部分参数：Vertex 通过 partialArgs
	// JSONPath 分片下发参数，name 跨 chunk 必须一致，参数按路径增量累加，
	// willContinue=false 时输出完整 tool_calls delta。按工具名区分，
	// 并发/连续多个工具调用各自独立累积。
	partialToolByName map[string]*vertexPartialToolCall
	toolIdxByID       map[string]int
	nextToolIndex     int
}

// vertexPartialArgPathSegment 是 JSONPath 的一个段（成员或数组下标）。
type vertexPartialArgPathSegment struct {
	member  string
	index   int
	isIndex bool
}

// vertexPartialToolCall 保存跨 chunk 累积的单个流式工具调用状态。
type vertexPartialToolCall struct {
	name         string
	arguments    map[string]interface{}
	willContinue bool
}

func newVertexSSETransformer(model string) *vertexSSETransformer {
	return &vertexSSETransformer{
		model:             model,
		id:                openAICompletionID(""),
		created:           time.Now().Unix(),
		partialToolByName: map[string]*vertexPartialToolCall{},
		toolIdxByID:       map[string]int{},
	}
}

func (t *vertexSSETransformer) chunk(delta map[string]interface{}, finish string) []byte {
	payload := map[string]interface{}{
		"id":      t.id,
		"object":  "chat.completion.chunk",
		"created": t.created,
		"model":   t.model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         delta,
				"finish_reason": nil,
			},
		},
	}
	if finish != "" {
		payload["choices"] = []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         delta,
				"finish_reason": finish,
			},
		}
	}
	data, _ := json.Marshal(payload)
	return append([]byte("data: "), append(data, '\n', '\n')...)
}

// consume 处理一个 Vertex SSE data 行，返回需要写出的 OpenAI chunk 字节。
func (t *vertexSSETransformer) consume(data []byte) [][]byte {
	if t.finished {
		return nil
	}
	var ev struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					Thought      bool   `json:"thought"`
					FunctionCall *struct {
						Name        string                 `json:"name"`
						Args        map[string]interface{} `json:"args"`
						PartialArgs []vertexPartialArg     `json:"partialArgs"`
						WillContinue *bool                 `json:"willContinue"`
						ID           string                `json:"id"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata vertexUsage `json:"usageMetadata"`
		ModelVersion  string      `json:"modelVersion"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil
	}
	if ev.ModelVersion != "" {
		t.model = ev.ModelVersion
	}

	var out [][]byte
	// 首事件立即输出 role chunk：确认流已建立，避免思考模型长时间无输出时
	// 客户端误以为连接挂死（TTFB 提前到首事件）。
	if !t.started {
		t.started = true
		out = append(out, t.chunk(map[string]interface{}{"role": "assistant"}, ""))
	}

	for _, cand := range ev.Candidates {
		for _, part := range cand.Content.Parts {
			if part.FunctionCall != nil {
				out = append(out, t.consumeToolCall(part.FunctionCall)...)
				continue
			}
			if part.Thought && part.Text != "" {
				// 思考内容 → reasoning_content 增量，与正文分开下发。
				out = append(out, t.chunk(map[string]interface{}{"reasoning_content": part.Text}, ""))
				continue
			}
			if part.Text != "" {
				out = append(out, t.chunk(map[string]interface{}{"content": part.Text}, ""))
			}
		}
		if cand.FinishReason != "" {
			t.finishStr = cand.FinishReason
		}
		if ev.UsageMetadata.TotalTokenCount > 0 || ev.UsageMetadata.PromptTokenCount > 0 || ev.UsageMetadata.CandidatesTokenCount > 0 {
			t.usage = &ev.UsageMetadata
		}
	}
	return out
}

// vertexPartialArg 是 Vertex 流式 functionCall 的部分参数块。
type vertexPartialArg struct {
	JSONPath     string  `json:"jsonPath"`
	Value        any     `json:"value"`
	StringValue  *string `json:"stringValue"`
	NumberValue  *float64 `json:"numberValue"`
	BoolValue    *bool   `json:"boolValue"`
	NullValue    *bool   `json:"nullValue"`
}

// consumeToolCall 处理一个流式 functionCall part：完整 args 直接输出；partialArgs
// 走 JSONPath 重组，willContinue=false/缺失时输出完整 tool_calls delta。OpenAI
// 流式工具调用的 id 需跨 chunk 稳定、index 单调递增。
func (t *vertexSSETransformer) consumeToolCall(fc *struct {
	Name        string                 `json:"name"`
	Args        map[string]interface{} `json:"args"`
	PartialArgs []vertexPartialArg     `json:"partialArgs"`
	WillContinue *bool                 `json:"willContinue"`
	ID           string                `json:"id"`
}) [][]byte {
	// 完整参数：整块输出一个 tool_calls delta（name + 完整 args）。
	if len(fc.PartialArgs) == 0 && len(fc.Args) > 0 {
		toolIdx := t.toolIdxFor(fc.ID)
		toolID := fc.ID
		if toolID == "" {
			toolID = "call_" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
		argsBytes, _ := json.Marshal(fc.Args)
		t.toolCallsEmitted = true
		return [][]byte{t.chunk(map[string]interface{}{
			"tool_calls": []interface{}{
				map[string]interface{}{
					"index": toolIdx,
					"id":    toolID,
					"type":  "function",
					"function": map[string]interface{}{
						"name":      fc.Name,
						"arguments": string(argsBytes),
					},
				},
			},
		}, "")}
	}

	// partialArgs 重组：按 JSONPath 增量写入参数树，willContinue 结束时输出完整
	// tool_calls。以 functionCall 携带的 id 为分桶键（Vertex 支持并行 function calling，
	// 不同调用各自累积，同名调用互不覆盖）；id 缺失时回退到工具名。
	partialKey := fc.ID
	if partialKey == "" {
		partialKey = fc.Name
	}
	partial := t.partialToolByName[partialKey]
	if partial == nil {
		partial = &vertexPartialToolCall{arguments: map[string]interface{}{}}
		t.partialToolByName[partialKey] = partial
	}
	if fc.Name != "" {
		partial.name = fc.Name
	}
	if fc.WillContinue != nil {
		partial.willContinue = *fc.WillContinue
	}
	for _, pa := range fc.PartialArgs {
		if err := applyVertexPartialArg(partial.arguments, pa); err != nil {
			continue
		}
	}

	// 尚未结束：不输出（OpenAI 流式工具调用只要求 name 先出现一次即可，
	// 但我们选择在参数完整时一次性输出，避免发送半程可被误收的片段）。
	if partial.willContinue {
		return nil
	}

	argsBytes, _ := json.Marshal(partial.arguments)
	toolIdx := t.toolIdxFor(fc.ID)
	toolID := fc.ID
	if toolID == "" {
		toolID = "call_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	delete(t.partialToolByName, partialKey)
	t.toolCallsEmitted = true
	return [][]byte{t.chunk(map[string]interface{}{
		"tool_calls": []interface{}{
			map[string]interface{}{
				"index": toolIdx,
				"id":    toolID,
				"type":  "function",
				"function": map[string]interface{}{
					"name":      partial.name,
					"arguments": string(argsBytes),
				},
			},
		},
	}, "")}
}

// toolIdxFor 返回工具调用在流式中的稳定 index：同 id 复用，否则单调递增。
func (t *vertexSSETransformer) toolIdxFor(id string) int {
	if idx, ok := t.toolIdxByID[id]; ok {
		return idx
	}
	idx := t.nextToolIndex
	t.nextToolIndex++
	if id != "" {
		t.toolIdxByID[id] = idx
	}
	return idx
}

// parseVertexPartialPath 解析 $ 开头的 JSONPath：支持 .member、['x']、[N]。
func parseVertexPartialPath(path string) ([]vertexPartialArgPathSegment, error) {
	var segs []vertexPartialArgPathSegment
	p := strings.TrimSpace(path)
	if p == "" || p[0] != '$' {
		return nil, fmt.Errorf("unsupported jsonPath %q", path)
	}
	runes := []rune(p)
	i := 1
	for i < len(runes) {
		switch runes[i] {
		case '.':
			j := i + 1
			for j < len(runes) && runes[j] != '.' && runes[j] != '[' {
				j++
			}
			if j == i+1 {
				return nil, fmt.Errorf("empty member in %q", path)
			}
			segs = append(segs, vertexPartialArgPathSegment{member: string(runes[i+1 : j])})
			i = j
		case '[':
			if i+1 < len(runes) && (runes[i+1] == '\'' || runes[i+1] == '"') {
				q := runes[i+1]
				j := i + 2
				for j < len(runes) && runes[j] != q {
					j++
				}
				if j >= len(runes) {
					return nil, fmt.Errorf("unterminated quote in %q", path)
				}
				segs = append(segs, vertexPartialArgPathSegment{member: string(runes[i+2 : j])})
				i = j + 1
				if i < len(runes) && runes[i] == ']' {
					i++
				}
			} else {
				j := i + 1
				for j < len(runes) && runes[j] >= '0' && runes[j] <= '9' {
					j++
				}
				if j == i+1 || j >= len(runes) || runes[j] != ']' {
					return nil, fmt.Errorf("unsupported index in %q", path)
				}
				idx, err := strconv.Atoi(string(runes[i+1 : j]))
				if err != nil {
					return nil, err
				}
				segs = append(segs, vertexPartialArgPathSegment{index: idx, isIndex: true})
				i = j + 1
			}
		default:
			return nil, fmt.Errorf("unsupported selector at %q", path)
		}
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("path %q targets root", path)
	}
	return segs, nil
}

// applyVertexPartialArg 把一个 partial 参数按 JSONPath 写入参数树。字符串增量拼接：
// partial 用 stringValue 标记，继承已存在的字符串时追加。
func applyVertexPartialArg(root map[string]interface{}, pa vertexPartialArg) error {
	segs, err := parseVertexPartialPath(pa.JSONPath)
	if err != nil {
		return err
	}
	value := pa.Value
	appendStr := false
	if pa.StringValue != nil {
		value = *pa.StringValue
		appendStr = true
	} else if pa.NumberValue != nil {
		value = *pa.NumberValue
	} else if pa.BoolValue != nil {
		value = *pa.BoolValue
	} else if pa.NullValue != nil {
		value = nil
	}
	return setVertexPartialArg(root, segs, value, appendStr)
}

// setVertexPartialArg 把 value 写入 root 参数树的 JSONPath 叶子。第一个段必须是
// 成员（Vertex 的 partialArgs 路径以 $ 开头且首个段为对象属性）。数组段逐层创建，
// 越界自动补 nil 占位。appendStr 时若叶子已存在字符串则拼接（增量式参数）。
func setVertexPartialArg(current any, segs []vertexPartialArgPathSegment, value any, appendStr bool) error {
	if len(segs) == 0 {
		return nil
	}
	seg := segs[0]
	if seg.isIndex {
		arr, ok := current.([]interface{})
		if !ok {
			return fmt.Errorf("jsonPath index %d traverses %T", seg.index, current)
		}
		for len(arr) <= seg.index {
			arr = append(arr, nil)
		}
		if len(segs) == 1 {
			if appendStr {
				if existing, ok := arr[seg.index].(string); ok {
					if vs, ok := value.(string); ok {
						arr[seg.index] = existing + vs
						return nil
					}
				}
			}
			arr[seg.index] = value
			return nil
		}
		nxt := arr[seg.index]
		if nxt == nil {
			if segs[1].isIndex {
				nxt = []interface{}{}
			} else {
				nxt = map[string]interface{}{}
			}
			arr[seg.index] = nxt
		}
		return setVertexPartialArg(nxt, segs[1:], value, appendStr)
	}
	m, ok := current.(map[string]interface{})
	if !ok {
		return fmt.Errorf("jsonPath member %q traverses %T", seg.member, current)
	}
	if len(segs) == 1 {
		if appendStr {
			if existing, ok := m[seg.member].(string); ok {
				if vs, ok := value.(string); ok {
					m[seg.member] = existing + vs
					return nil
				}
			}
		}
		m[seg.member] = value
		return nil
	}
	child := m[seg.member]
	if child == nil {
		if segs[1].isIndex {
			child = []interface{}{}
		} else {
			child = map[string]interface{}{}
		}
		m[seg.member] = child
	}
	return setVertexPartialArg(child, segs[1:], value, appendStr)
}

func (t *vertexSSETransformer) finalChunk(finish string) []byte {
	usage := vertexOpenAIUsage(t.usage)
	payload := map[string]interface{}{
		"id":      t.id,
		"object":  "chat.completion.chunk",
		"created": t.created,
		"model":   t.model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": finish,
			},
		},
		"usage": usage,
	}
	data, _ := json.Marshal(payload)
	return append([]byte("data: "), append(data, '\n', '\n')...)
}

// finish 在流结束（上游 STOP 或读中断）时补发收尾，保证 OpenAI 流式有
// finish_reason 与 usage，并总是以 [DONE] 收尾（与 OpenAI 协议一致）。
func (t *vertexSSETransformer) finish() [][]byte {
	var out [][]byte
	if !t.finished {
		t.finished = true
		finish := "stop"
		if t.toolCallsEmitted {
			finish = "tool_calls"
		}
		if t.finishStr != "" {
			finish = vertexFinishReason(t.finishStr)
			if t.toolCallsEmitted {
				finish = "tool_calls"
			}
		}
		out = append(out, t.finalChunk(finish))
	}
	if !t.doneSent {
		t.doneSent = true
		out = append(out, []byte("data: [DONE]\n\n"))
	}
	return out
}
