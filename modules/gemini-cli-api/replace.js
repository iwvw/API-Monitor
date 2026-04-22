const fs = require('fs');
const file = 'E:/Code/api-monitor/modules/gemini-cli-api/gemini-client.js';
let code = fs.readFileSync(file, 'utf8');

const newCode = `  /**
   * 将 OpenAI 请求转换为 Gemini 原生负载
   */
  async convertOpenAIToGemini(openaiRequest) {
    const { messages, model, stream, temperature, top_p, max_tokens, stop, reasoning_effort, tools } = openaiRequest;

    const settings = storage ? await storage.getSettings() : {};

    const contents = [];
    const systemParts = []; // 收集所有 system 消息

    for (const msg of messages) {
      if (msg.role === 'system' || msg.role === 'developer') {
        // 收集所有 system 消息内容
        const textContent = this._extractTextContent(msg.content);
        if (textContent.trim()) {
          systemParts.push(textContent);
        }
      } else {
        const role = (msg.role === 'assistant' || msg.role === 'model') ? 'model' : 'user';
        const parts = await this._convertMessageToParts(msg);

        // Gemini API 要求消息角色严格交替（user ↔ model）
        // 如果上一条消息角色相同，则合并 parts 而不是新增消息
        const lastContent = contents[contents.length - 1];
        if (lastContent && lastContent.role === role) {
          // 合并到上一条消息
          lastContent.parts.push(...parts);
        } else {
          contents.push({
            role: role,
            parts: parts,
          });
        }
      }
    }

    // 注入实时时间锚点，防止模型在 search 模式下产生时间幻觉
    const now = new Date();
    const currentTimeStr = \`Current Time: \${now.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })} (Beijing Time)\\n\\n\`;

    // 合并所有 system 消息（用双换行符分隔）
    let systemInstruction = null;
    if (systemParts.length > 0) {
      systemInstruction = { parts: [{ text: currentTimeStr + systemParts.join('\\n\\n') }] };
    } else if (settings.SYSTEM_INSTRUCTION) {
      systemInstruction = { parts: [{ text: currentTimeStr + settings.SYSTEM_INSTRUCTION }] };
    } else {
      systemInstruction = { parts: [{ text: currentTimeStr }] };
    }

    const generationConfig = {
      temperature: temperature ?? parseFloat(settings.DEFAULT_TEMPERATURE || 1),
      topP: top_p ?? parseFloat(settings.DEFAULT_TOP_P || 0.95),
      topK: parseInt(settings.DEFAULT_TOP_K || 64),
      stopSequences: Array.isArray(stop) ? stop : stop ? [stop] : [],
    };

    if (max_tokens !== undefined && max_tokens !== null) {
      generationConfig.maxOutputTokens = Math.min(max_tokens, 65536);
    } else if (settings.DEFAULT_MAX_TOKENS) {
      generationConfig.maxOutputTokens = Math.min(parseInt(settings.DEFAULT_MAX_TOKENS), 65536);
    }

    const thinkingConfig = this._getThinkingConfig(model, reasoning_effort);
    if (thinkingConfig) {
      generationConfig.thinkingConfig = thinkingConfig;

      if (generationConfig.maxOutputTokens && thinkingConfig.thinkingBudget) {
        if (generationConfig.maxOutputTokens < thinkingConfig.thinkingBudget + 1024) {
          generationConfig.maxOutputTokens = Math.min(thinkingConfig.thinkingBudget + 4096, 65536);
          logger.info(\`Adjusted maxOutputTokens for thinking budget (\${thinkingConfig.thinkingBudget}): \${generationConfig.maxOutputTokens}\`);
        }
      }
    }

    // 构建请求体
    const payload = { contents };

    if (Object.keys(generationConfig).length > 0) {
      payload.generationConfig = generationConfig;
    }

    if (systemInstruction) {
      payload.systemInstruction = systemInstruction;
    }

    // 处理 tools (Function Calling / Search 等)
    const geminiTools = [];
    if (tools && Array.isArray(tools) && tools.length > 0) {
      const functionDeclarations = [];
      for (const t of tools) {
        if (t.type === 'function' && t.function) {
          const fn = t.function;
          const fnDecl = {
            name: fn.name,
            description: fn.description || ''
          };
          if (fn.parameters) {
            fnDecl.parametersJsonSchema = fn.parameters;
            if (!fnDecl.parametersJsonSchema.type) fnDecl.parametersJsonSchema.type = 'object';
            if (!fnDecl.parametersJsonSchema.properties) fnDecl.parametersJsonSchema.properties = {};
          } else {
            fnDecl.parametersJsonSchema = { type: 'object', properties: {} };
          }
          functionDeclarations.push(fnDecl);
        }
        if (t.google_search) geminiTools.push({ googleSearch: t.google_search });
        if (t.code_execution) geminiTools.push({ codeExecution: t.code_execution });
      }
      if (functionDeclarations.length > 0) geminiTools.push({ functionDeclarations });
    }
    if (geminiTools.length > 0) {
      payload.tools = geminiTools;
    }

    payload.safetySettings = [
      { category: 'HARM_CATEGORY_HARASSMENT', threshold: 'OFF' },
      { category: 'HARM_CATEGORY_HATE_SPEECH', threshold: 'OFF' },
      { category: 'HARM_CATEGORY_SEXUALLY_EXPLICIT', threshold: 'OFF' },
      { category: 'HARM_CATEGORY_DANGEROUS_CONTENT', threshold: 'OFF' },
      { category: 'HARM_CATEGORY_CIVIC_INTEGRITY', threshold: 'BLOCK_NONE' },
    ];

    return payload;
  }

  /**
   * 提取内容中的文本部分
   */
  _extractTextContent(content) {
    if (typeof content === 'string') {
      return content;
    }
    if (Array.isArray(content)) {
      return content
        .filter(item => item.type === 'text')
        .map(item => item.text || '')
        .join('');
    }
    return String(content || '');
  }

  /**
   * 将 OpenAI 格式的 msg 转换为 Gemini parts
   * 支持多模态输入、Tool Calls、Tool Responses 及思考内容处理
   */
  async _convertMessageToParts(msg) {
    // 1. 处理 Tool 响应
    if (msg.role === 'tool') {
       let parsedContent = {};
       try { parsedContent = typeof msg.content === 'string' ? JSON.parse(msg.content) : msg.content; } 
       catch (e) { parsedContent = { value: msg.content }; }
       return [{
         functionResponse: {
           name: msg.tool_call_id || msg.name || 'unknown_function',
           response: { result: parsedContent }
         }
       }];
    }

    const parts = [];
    const content = msg.content;
    
    // 2. 转换内容文本或数组
    if (content) {
      if (typeof content === 'string') {
        parts.push({ text: content });
      } else if (Array.isArray(content)) {
        for (const item of content) {
          if (item.type === 'text') {
            parts.push({ text: item.text || '' });
          } else if (item.type === 'image_url') {
            const imageUrl = item.image_url?.url || '';
            const imagePart = await this._parseImageUrl(imageUrl);
            if (imagePart) parts.push(imagePart);
          }
        }
      }
    }

    // 3. 处理历史思考内容 (拍扁转化为普通文本，防报错)
    if (msg.reasoning_content) {
      const thoughtText = \`[Thought: \${msg.reasoning_content}]\\n\`;
      if (parts.length > 0 && parts[0].text !== undefined) {
        parts[0].text = thoughtText + parts[0].text;
      } else {
        parts.unshift({ text: thoughtText });
      }
    }

    // 4. 处理 Assistant 产生的 Tool Calls
    if (msg.role === 'assistant' && msg.tool_calls) {
      for (const tc of msg.tool_calls) {
        if (tc.type === 'function' && tc.function) {
          let args = {};
          try { args = typeof tc.function.arguments === 'string' ? JSON.parse(tc.function.arguments) : tc.function.arguments; } 
          catch(e) {}
          parts.push({
            functionCall: {
              name: tc.function.name,
              args: args
            },
            thoughtSignature: "skip_thought_signature_validator"
          });
        }
      }
    }
    
    return parts.length > 0 ? parts : [{ text: '' }];
  }

  /**
   * 解析图像 URL 并转换为 Gemini inlineData 格式
   */
  async _parseImageUrl(imageUrl) {
    if (!imageUrl) return null;

    // 处理 Base64 Data URI: data:image/jpeg;base64,/9j/4AAQ...
    const base64Match = imageUrl.match(/^data:image\\/(\\w+);base64,(.+)$/);
    if (base64Match) {
      return {
        inlineData: {
          mimeType: \`image/\${base64Match[1]}\`,
          data: base64Match[2],
        },
      };
    }

    // 处理其他 MIME 类型 (如 data:application/octet-stream)
    const genericBase64Match = imageUrl.match(/^data:([^;]+);base64,(.+)$/);
    if (genericBase64Match) {
      return {
        inlineData: {
          mimeType: genericBase64Match[1],
          data: genericBase64Match[2],
        },
      };
    }

    // 处理本地文件路径 (/uploads/...)
    if (imageUrl.startsWith('/uploads/')) {
      try {
        const fs = require('fs');
        const path = require('path');
        // 构造文件路径: process.cwd() + /data + /uploads/...
        const relativePath = imageUrl.startsWith('/') ? imageUrl.slice(1) : imageUrl;
        const filePath = path.join(process.cwd(), 'data', relativePath);

        if (fs.existsSync(filePath)) {
          const fileBuffer = fs.readFileSync(filePath);
          const base64Data = fileBuffer.toString('base64');
          const ext = path.extname(filePath).toLowerCase();

          let mimeType = 'image/jpeg';
          if (ext === '.png') mimeType = 'image/png';
          else if (ext === '.webp') mimeType = 'image/webp';
          else if (ext === '.gif') mimeType = 'image/gif';

          logger.info(\`[Gemini-Client] Loaded local image: \${filePath} (\${Math.round(fileBuffer.length / 1024)}KB)\`);

          return {
            inlineData: {
              mimeType: mimeType,
              data: base64Data
            }
          };
        } else {
          logger.warn(\`[Gemini-Client] Image file not found: \${filePath}\`);
          return null;
        }
      } catch (e) {
        logger.error(\`[Gemini-Client] Failed to process local image: \${e.message}\`);
        return null;
      }
    }

    // HTTP/HTTPS URL 支持下载 (使用 axios)
    if (imageUrl.startsWith('http://') || imageUrl.startsWith('https://')) {
      try {
        const axiosConfig = await this.getAxiosConfig();
        const response = await axios.get(imageUrl, {
          ...axiosConfig,
          responseType: 'arraybuffer',
          timeout: 15000
        });
        const mimeType = response.headers['content-type'] || 'image/jpeg';
        const base64Data = Buffer.from(response.data).toString('base64');
        return {
          inlineData: {
            mimeType: mimeType,
            data: base64Data
          }
        };
      } catch (e) {
        logger.warn(\`Failed to download HTTP image URL: \${imageUrl.substring(0, 80)} - \${e.message}\`);
        return null;
      }
    }

    logger.warn(\`Unsupported image URL format: \${imageUrl.substring(0, 50)}\`);
    return null;
  }`;

const startMarker = '  async convertOpenAIToGemini(openaiRequest) {';
let endMarker = '    logger.warn(`Unsupported image URL format: ${imageUrl.substring(0, 50)}`);\n    return null;\n  }';

const startIdx = code.indexOf(startMarker);
// 考虑前置注释
const prefixIdx = code.lastIndexOf('  /**', startIdx);

let matchEnd = code.indexOf(endMarker);
if (matchEnd === -1) {
  // 可能是因为 \r\n 换行符
  endMarker = endMarker.replace(/\n/g, '\r\n');
  matchEnd = code.indexOf(endMarker);
}

if (prefixIdx !== -1 && matchEnd !== -1) {
  code = code.substring(0, prefixIdx) + newCode + code.substring(matchEnd + endMarker.length);
  fs.writeFileSync(file, code);
  console.log('Replaced successfully');
} else {
  console.log('Could not find markers', prefixIdx, matchEnd);
}
