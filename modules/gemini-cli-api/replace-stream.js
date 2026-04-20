const fs = require('fs');
const file = 'E:/Code/api-monitor/modules/gemini-cli-api/utils/stream-processor.js';
let code = fs.readFileSync(file, 'utf8');

const newCode = `const { Readable } = require('stream');
const { createLogger } = require('../../../src/utils/logger');
const logger = createLogger('GCLI-Stream');

class StreamProcessor {
  constructor(client) {
    this.client = client;
    this.DONE_MARKER = '[done]';
    this.CONTINUATION_PROMPT =
      '\\n请从刚才被截断的地方继续输出剩余的所有内容。\\n重要提醒：直接继续输出即可，不要重复前面内容。最后请以 [done] 结尾。';
  }

  /**
   * 解析 Gemini SSE 数据块
   */
  parseGeminiChunk(line) {
    if (!line.startsWith('data: ')) return null;
    try {
      let data = JSON.parse(line.substring(6));

      // 处理 v1internal 的包装层
      if (data.response) {
        data = data.response;
      }

      const candidate = data.candidates?.[0];
      if (!candidate) {
        // Check for safety filter
        if (data.promptFeedback?.blockReason) {
          return { text: '', reasoning: '', finishReason: 'SAFETY', blocked: data.promptFeedback.blockReason };
        }
        return {}; // Return empty object, JSON is valid but no content
      }

      const parts = candidate.content?.parts || [];
      let text = '';
      let reasoning = '';
      let toolCalls = null;

      parts.forEach(part => {
        // 只有 thought === true 的 part 才是思考内容
        if (part.thought === true) {
          reasoning += part.text || '';
        } else if (part.functionCall) {
          if (!toolCalls) toolCalls = [];
          toolCalls.push(part.functionCall);
        } else {
          text += part.text || '';
        }
      });

      return {
        text,
        reasoning,
        toolCalls,
        finishReason: candidate.finishReason,
        usage: data.usageMetadata,
      };
    } catch (e) {
      return null;
    }
  }

  /**
   * 核心流式处理入口
   */
  async *processStream(openaiRequest, accountId, maxAttempts = 3) {
    const isAntiTrunc = openaiRequest.model.includes('流抗/');
    let currentAttempt = 0;
    let fullContent = '';
    let foundDone = !isAntiTrunc; // 如果不开启抗截断，默认视为已找到结束标记（即不循环）
    const responseId = \`chatcmpl-\${Math.random().toString(36).slice(2)}\`;
    const startTime = Date.now();
    let firstTokenTime = null;
    let lastUsage = null; // 局部变量，确保线程安全
    let contentStarted = false; // 状态机：正文是否已开始流出
    let accumulatedReasoning = ''; // 累积的思考内容，用于后处理修正

    const modifiedRequest = JSON.parse(JSON.stringify(openaiRequest));

    if (isAntiTrunc) {
      // 仅在抗截断模式下注入指令
      const systemMsg = modifiedRequest.messages.find(m => m.role === 'system');
      const antiTruncInstr = '\\n[系统指令] 请在回答完全结束时，在最后一行输出 [done] 标记。';
      if (systemMsg) {
        systemMsg.content += antiTruncInstr;
      } else {
        modifiedRequest.messages.unshift({ role: 'system', content: antiTruncInstr });
      }
    }

    const loopLimit = isAntiTrunc ? maxAttempts : 1;

    while (currentAttempt < loopLimit && (isAntiTrunc ? !foundDone : currentAttempt === 0)) {
      currentAttempt++;

      // 如果是后续尝试，调整请求内容实现“续写”
      if (currentAttempt > 1) {
        modifiedRequest.messages.push({ role: 'assistant', content: fullContent });
        modifiedRequest.messages.push({ role: 'user', content: this.CONTINUATION_PROMPT });
      }

      try {
        const response = await this.client.generateContent(modifiedRequest, accountId);
        const stream = response.data;

        let buffer = '';
        for await (const chunk of stream) {
          buffer += chunk.toString();
          const lines = buffer.split('\\n');
          buffer = lines.pop(); // 保留最后一行（可能不完整）

          for (const line of lines) {
            const parsed = this.parseGeminiChunk(line.trim());
            if (!parsed) continue;

            // Detect safety filter blocks
            if (parsed.blocked || parsed.finishReason === 'SAFETY' || parsed.finishReason === 'RECITATION') {
              if (firstTokenTime === null && currentAttempt === 1) {
                // 还没有输出任何内容，且是第一次尝试，抛出异常让外层换号重试
                throw new Error(\`Response blocked by safety filter: \${parsed.blocked || parsed.finishReason}\`);
              } else {
                // 已经开始输出内容了，优雅中断，返回 content_filter
                yield \`data: \${JSON.stringify({
                  id: responseId,
                  object: 'chat.completion.chunk',
                  created: Math.floor(Date.now() / 1000),
                  model: openaiRequest.model,
                  choices: [{ index: 0, delta: {}, finish_reason: 'content_filter' }],
                })}\\n\\n\`;
                yield 'data: [DONE]\\n\\n';
                return;
              }
            }

            let { text = '', reasoning = '', toolCalls = null, usage = null, finishReason } = parsed;

            // 持续更新最后一个有效的使用度统计
            if (usage) {
              lastUsage = usage;
            }

            // 状态机修复：一旦正文开始流出，后续 thought 标记的内容视为正文溢出
            if (text) contentStarted = true;
            if (contentStarted && reasoning) {
              text += reasoning;
              reasoning = '';
            }

            // 防御性处理：如果 text 为空但 reasoning 有值，且模型处于 nothinking 模式
            if (!text && reasoning && openaiRequest.model.includes('-nothinking')) {
              text = reasoning;
              reasoning = '';
            }

            // 抗截断逻辑：检测 [done] 标记
            if (isAntiTrunc && text.includes(this.DONE_MARKER)) {
              foundDone = true;
              text = text.replace(this.DONE_MARKER, '').trim();
            }
            
            // 如果模型给出了结束原因（非 MAX_TOKENS），也视为结束，不再强求 [done] 循环
            if (finishReason && finishReason !== 'MAX_TOKENS') {
              foundDone = true;
            }

            fullContent += text;
            if (reasoning) accumulatedReasoning += reasoning;

            // 记录首字输出时间（仅在首次有内容时）
            if (firstTokenTime === null && (text || reasoning || toolCalls)) {
              firstTokenTime = Date.now() - startTime;
            }

            // 构造 OpenAI 格式的 Chunk
            const delta = {};
            if (text) delta.content = text;
            if (reasoning) delta.reasoning_content = reasoning;
            
            // 构造 Tool Calls
            if (toolCalls && toolCalls.length > 0) {
              delta.tool_calls = toolCalls.map((tc, idx) => ({
                id: \`call_\${Math.random().toString(36).slice(2)}\`,
                index: idx,
                type: 'function',
                function: {
                  name: tc.name,
                  arguments: typeof tc.args === 'string' ? tc.args : JSON.stringify(tc.args)
                }
              }));
              foundDone = true; // Tool Call 后不需要接续
            }

            if (Object.keys(delta).length > 0) {
              yield \`data: \${JSON.stringify({
                id: responseId,
                object: 'chat.completion.chunk',
                created: Math.floor(Date.now() / 1000),
                model: openaiRequest.model,
                choices: [{ index: 0, delta, finish_reason: null }],
              })}\\n\\n\`;
            }
          }
        }

        if (!isAntiTrunc || foundDone) break;
        logger.warn(\`Stream interrupted, attempt \${currentAttempt} failed to find [done].\`);
      } catch (e) {
        logger.error(\`Stream processing error (Attempt \${currentAttempt}): \${e.message}\`);
        if (currentAttempt === 1) {
          throw e; // 第一次尝试失败，抛出异常让外层（如负载均衡/账号重试）处理
        }
        break;
      }
    }

    // 防御性后处理：Gemini 有时将最终回复混入思考内容 (尤其在短对话时)
    if (!contentStarted && !fullContent && accumulatedReasoning) {
      const segments = accumulatedReasoning.split('\\n\\n');
      if (segments.length > 1) {
        // 从末尾向前扫描，找到第一个不以 ** 开头的段落（即非思考标题段）
        const extractedLines = [];
        for (let i = segments.length - 1; i >= 0; i--) {
          const seg = segments[i].trim();
          if (seg.startsWith('**') && seg.includes('**\\n')) break; 
          if (!seg) continue; 
          extractedLines.unshift(seg);
          segments.splice(i, 1);
          if (i > 0 && segments[i - 1]?.trim().startsWith('**')) break;
        }
        if (extractedLines.length > 0) {
          const extractedContent = extractedLines.join('\\n\\n');
          fullContent = extractedContent;
          logger.info(\`[Stream] 从思考内容末尾提取了被混入的最终回复 (\${extractedContent.length} chars)\`);
          yield \`data: \${JSON.stringify({
            id: responseId,
            object: 'chat.completion.chunk',
            created: Math.floor(Date.now() / 1000),
            model: openaiRequest.model,
            choices: [{ index: 0, delta: { content: extractedContent }, finish_reason: null }],
          })}\\n\\n\`;
        }
      }
    }

    // 发送结束标记，并通过扩展字段传递首字输出时间及最终使用度统计
    yield \`data: \${JSON.stringify({
      id: responseId,
      object: 'chat.completion.chunk',
      created: Math.floor(Date.now() / 1000),
      model: openaiRequest.model,
      choices: [{ index: 0, delta: {}, finish_reason: 'stop' }],
      _firstTokenTime: firstTokenTime,
      usage: lastUsage || null, // 回传最终统计
    })}\\n\\n\`;
    yield 'data: [DONE]\\n\\n';

    // 返回 metadata 供外层使用
    this._lastFirstTokenTime = firstTokenTime;
  }
}

module.exports = StreamProcessor;`;

fs.writeFileSync(file, newCode);
console.log("stream-processor.js completely replaced.");
