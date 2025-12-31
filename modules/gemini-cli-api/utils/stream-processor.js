const { Readable } = require('stream');

class StreamProcessor {
  constructor(client) {
    this.client = client;
    this.DONE_MARKER = '[done]';
    this.CONTINUATION_PROMPT =
      '\n请从刚才被截断的地方继续输出剩余的所有内容。\n重要提醒：直接继续输出即可，不要重复前面内容。最后请以 [done] 结尾。';
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
      if (!candidate) return null;

      const parts = candidate.content?.parts || [];
      let text = '';
      let reasoning = '';

      parts.forEach(part => {
        if (part.thought) {
          reasoning += part.text || '';
        } else {
          text += part.text || '';
        }
      });

      return {
        text,
        reasoning,
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
    const responseId = `chatcmpl-${Math.random().toString(36).slice(2)}`;

    const modifiedRequest = JSON.parse(JSON.stringify(openaiRequest));

    if (isAntiTrunc) {
      // 仅在抗截断模式下注入指令
      const systemMsg = modifiedRequest.messages.find(m => m.role === 'system');
      const antiTruncInstr = '\n[系统指令] 请在回答完全结束时，在最后一行输出 [done] 标记。';
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
          const lines = buffer.split('\n');
          buffer = lines.pop(); // 保留最后一行（可能不完整）

          for (const line of lines) {
            const parsed = this.parseGeminiChunk(line.trim());
            if (!parsed) continue;

            let { text, reasoning } = parsed;

            // 抗截断逻辑：检测 [done] 标记
            if (isAntiTrunc && text.includes(this.DONE_MARKER)) {
              foundDone = true;
              text = text.replace(this.DONE_MARKER, '').trim();
            }

            fullContent += text;

            // 构造 OpenAI 格式的 Chunk
            const delta = {};
            if (text) delta.content = text;
            if (reasoning) delta.reasoning_content = reasoning;

            if (Object.keys(delta).length > 0) {
              yield `data: ${JSON.stringify({
                id: responseId,
                object: 'chat.completion.chunk',
                created: Math.floor(Date.now() / 1000),
                model: openaiRequest.model,
                choices: [{ index: 0, delta, finish_reason: null }],
              })}\n\n`;
            }
          }
        }

        if (!isAntiTrunc || foundDone) break;
        console.log(`Stream interrupted, attempt ${currentAttempt} failed to find [done].`);
      } catch (e) {
        console.error(`Stream processing error (Attempt ${currentAttempt}):`, e.message);
        if (currentAttempt === 1) {
          throw e; // 第一次尝试失败，抛出异常让外层（如负载均衡/账号重试）处理
        }
        break;
      }
    }

    // 发送结束标记
    yield `data: ${JSON.stringify({
      id: responseId,
      object: 'chat.completion.chunk',
      created: Math.floor(Date.now() / 1000),
      model: openaiRequest.model,
      choices: [{ index: 0, delta: {}, finish_reason: 'stop' }],
    })}\n\n`;
    yield 'data: [DONE]\n\n';
  }
}

module.exports = StreamProcessor;
