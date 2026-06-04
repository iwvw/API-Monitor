const axios = require('axios');
const crypto = require('crypto');
const { HttpsProxyAgent } = require('https-proxy-agent');
const AntigravityRequester = require('./antigravity-requester');
const storage = require('./storage');
const path = require('path');
const fs = require('fs');

// 默认配置 (保留作为 fallback)
const DEFAULT_CONFIG = {
  CLIENT_ID: '1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com',
  CLIENT_SECRET: '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',
  API_URL:
    'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:streamGenerateContent?alt=sse',
  MODELS_URL: 'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels',
  NO_STREAM_URL: 'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:generateContent',
  API_HOST: 'daily-cloudcode-pa.sandbox.googleapis.com',
  USER_AGENT: 'antigravity/1.30.0 (Windows; AMD64)',
  // 端点回退顺序：sandbox → daily → prod
  FALLBACK_BASE_URLS: [
    'https://daily-cloudcode-pa.sandbox.googleapis.com',
    'https://daily-cloudcode-pa.googleapis.com',
    'https://cloudcode-pa.googleapis.com',
  ],
  SYSTEM_INSTRUCTION: '',
};

// 模型名称别名映射（CLIProxyAPI 兼容）
const MODEL_ALIAS_MAP = {
  'gemini-3-pro-high': 'gemini-3-pro-preview',
  'gemini-3-pro-image': 'gemini-3-pro-image-preview',
  'gemini-3-flash': 'gemini-3-flash-preview',
  'claude-sonnet-4-5': 'gemini-claude-sonnet-4-5',
  'claude-sonnet-4-5-thinking': 'gemini-claude-sonnet-4-5-thinking',
  'claude-opus-4-5-thinking': 'gemini-claude-opus-4-5-thinking',
};

const ALIAS_TO_MODEL_MAP = Object.fromEntries(
  Object.entries(MODEL_ALIAS_MAP).map(([k, v]) => [v, k])
);

/**
 * 生成稳定的 SessionId（基于第一条用户消息的 SHA256 哈希）
 */
function generateStableSessionId(contents) {
  try {
    for (const content of contents) {
      if (content.role === 'user' && content.parts?.length > 0) {
        const text = content.parts[0].text;
        if (text) {
          const hash = crypto.createHash('sha256').update(text).digest();
          const n = hash.readBigInt64BE(0) & BigInt('0x7FFFFFFFFFFFFFFF');
          return '-' + n.toString();
        }
      }
    }
  } catch (e) { /* fallback to random */ }
  return String(-Math.floor(Math.random() * 9e18));
}

/**
 * 模型名称转别名（用于 API 请求）
 */
function modelName2Alias(modelName) {
  return MODEL_ALIAS_MAP[modelName] || modelName;
}

/**
 * 别名转模型名称（用于响应解析）
 */
function alias2ModelName(alias) {
  return ALIAS_TO_MODEL_MAP[alias] || alias;
}

let requester = null;

/**
 * 获取当前配置
 */
function getConfig() {
  const settings = storage.getSettings();
  // 将设置数组转换为对象如果需要，或者假设 getSettings 返回的是 Key-Value 对象
  // 根据 router.js 中 getSettings 的实现，它似乎返回的是个对象 map
  // 让我们做个简单的映射以防万一

  // 如果 settings 是数组 (key-value pair)，转对象
  let configMap = {};
  if (Array.isArray(settings)) {
    settings.forEach(s => (configMap[s.key] = s.value));
  } else {
    configMap = settings || {};
  }

  return {
    CLIENT_ID: configMap.GOOGLE_CLIENT_ID || DEFAULT_CONFIG.CLIENT_ID,
    CLIENT_SECRET: configMap.GOOGLE_CLIENT_SECRET || DEFAULT_CONFIG.CLIENT_SECRET,
    API_URL: configMap.API_URL || DEFAULT_CONFIG.API_URL,
    MODELS_URL: configMap.API_MODELS_URL || DEFAULT_CONFIG.MODELS_URL,
    NO_STREAM_URL: configMap.API_NO_STREAM_URL || DEFAULT_CONFIG.NO_STREAM_URL,
    API_HOST: configMap.API_HOST || DEFAULT_CONFIG.API_HOST,
    USER_AGENT: configMap.API_USER_AGENT || DEFAULT_CONFIG.USER_AGENT,
    SYSTEM_INSTRUCTION: configMap.SYSTEM_INSTRUCTION || DEFAULT_CONFIG.SYSTEM_INSTRUCTION,
    FALLBACK_BASE_URLS: DEFAULT_CONFIG.FALLBACK_BASE_URLS,
    PROXY: configMap.PROXY || '',
    TIMEOUT: parseInt(configMap.TIMEOUT) || 30000,
  };
}

/**
 * 初始化或获取 Requester
 */
function getRequester() {
  // 这里我们不做单例缓存，或者每次调用前重新检查配置是否变更？
  // 为了支持热重载，我们最好让 requester 实例能更新配置，或者每次只需确保 binPath 正确
  // AntigravityRequester 主要负责 spawn 进程，配置大多在请求时传入
  if (!requester) {
    requester = new AntigravityRequester({
      binPath: path.join(__dirname, 'bin'),
    });
  }
  return requester;
}

/**
 * 刷新 Token
 */
async function refreshToken(account, token) {
  const config = getConfig();

  const body = new URLSearchParams({
    client_id: config.CLIENT_ID,
    client_secret: config.CLIENT_SECRET,
    grant_type: 'refresh_token',
    refresh_token: token.refresh_token,
  });

  try {
    // 构建 axios 请求配置
    const axiosConfig = {
      method: 'POST',
      url: 'https://oauth2.googleapis.com/token',
      headers: {
        Host: 'oauth2.googleapis.com',
        'User-Agent': 'Go-http-client/1.1',
        'Content-Type': 'application/x-www-form-urlencoded',
      },
      data: body.toString(),
      timeout: config.TIMEOUT,
    };

    // 添加代理支持
    if (
      config.PROXY &&
      (config.PROXY.startsWith('http://') || config.PROXY.startsWith('https://'))
    ) {
      axiosConfig.httpsAgent = new HttpsProxyAgent(config.PROXY);
      axiosConfig.proxy = false; // 禁用 axios 默认代理，使用 httpsAgent
    }

    const response = await axios(axiosConfig);

    const newTokenData = {
      accountId: account.id,
      accessToken: response.data.access_token,
      refreshToken: token.refresh_token, // 保持原有的 refresh_token
      expiresIn: response.data.expires_in,
      timestamp: Date.now(),
      projectId: token.project_id,
      email: token.email,
      userId: token.user_id,
      userEmail: token.user_email,
    };

    storage.saveToken(newTokenData);
    storage.updateAccount(account.id, { status: 'online' });
    return newTokenData;
  } catch (error) {
    console.error(`刷新 Token 失败 (${account.name}):`, error.response?.data || error.message);
    if (error.response?.status === 400 || error.response?.status === 401) {
      storage.disableToken(account.id);
      storage.updateAccount(account.id, { status: 'error' });
    }
    throw error;
  }
}

/**
 * 获取有效的 Token（如果过期会自动刷新）
 */
async function getValidToken(accountId) {
  const account = storage.getAccountById(accountId);
  if (!account || !account.enable) return null;

  const token = storage.getTokenByAccountId(accountId);
  if (!token) return null;

  // 检查是否过期 (提前5分钟刷新)
  const expiresAt = token.timestamp + token.expires_in * 1000;
  if (Date.now() >= expiresAt - 300000) {
    try {
      const newToken = await refreshToken(account, token);
      return newToken.accessToken;
    } catch (e) {
      return null;
    }
  }

  return token.access_token;
}

/**
 * 刷新所有启用账号的凭证
 */
async function refreshAllAccounts() {
  const accounts = storage.getAccounts().filter(a => a.enable);
  const results = { total: accounts.length, success: 0, fail: 0 };

  for (const account of accounts) {
    try {
      const token = storage.getTokenByAccountId(account.id);
      if (token) {
        await refreshToken(account, token);
        results.success++;
      } else {
        results.fail++;
      }
    } catch (e) {
      results.fail++;
    }
  }
  return results;
}

/**
 * 构建请求头
 */
function buildHeaders(accessToken) {
  const config = getConfig();
  let ua = config.USER_AGENT || 'antigravity/1.30.0 (Windows; AMD64)';

  // 强制检查版本：如果是旧版本或是 GeminiCLI 前缀，强制替换为新版，防止数据库配置坑
  if (ua.includes('1.11.3') || ua.includes('GeminiCLI') || ua.includes('1.14.') || ua.includes('1.15.') || ua.includes('1.18.') || ua.includes('1.20.') || ua.includes('1.22.')) {
    ua = 'antigravity/1.30.0 (Windows; AMD64)';
  }

  return {
    Host: config.API_HOST,
    'User-Agent': ua,
    Authorization: `Bearer ${accessToken}`,
    'Content-Type': 'application/json',
    'Accept-Encoding': 'gzip',
  };
}

/**
 * 转换上游模型 ID
 */
function mapModels(data) {
  if (!data || !data.models) return [];
  return Object.keys(data.models)
    .sort()
    .map(id => ({
      id,
      object: 'model',
      created: Math.floor(Date.now() / 1000),
      owned_by: 'google',
    }));
}
// 全局思维签名缓存：用于记录 Gemini 返回的 thoughtSignature（工具调用与文本），
// 并在后续请求中复用，避免后端报缺失错误。
const thoughtSignatureMap = new Map();
const textThoughtSignatureMap = new Map();

function registerThoughtSignature(id, thoughtSignature) {
  if (!id || !thoughtSignature) return;
  thoughtSignatureMap.set(id, thoughtSignature);
}

function getThoughtSignature(id) {
  if (!id) return undefined;
  return thoughtSignatureMap.get(id);
}

function normalizeTextForSignature(text) {
  if (typeof text !== 'string') return '';
  return text
    .replace(/<think>[\s\S]*?<\/think>/g, '')
    .replace(/!\[[^\]]*]\([^)]+\)/g, '')
    .replace(/\r\n/g, '\n')
    .trim();
}

function registerTextThoughtSignature(text, thoughtSignature) {
  if (!text || !thoughtSignature) return;
  const originalText = typeof text === 'string' ? text : String(text);
  const trimmed = originalText.trim();
  const normalized = normalizeTextForSignature(trimmed);
  const payload = { signature: thoughtSignature, text: originalText };
  if (originalText) {
    textThoughtSignatureMap.set(originalText, payload);
  }
  if (normalized) {
    textThoughtSignatureMap.set(normalized, payload);
  }
}

function getTextThoughtSignature(text) {
  if (typeof text !== 'string' || !text.trim()) return undefined;
  if (textThoughtSignatureMap.has(text)) {
    return textThoughtSignatureMap.get(text);
  }
  const trimmed = text.trim();
  if (textThoughtSignatureMap.has(trimmed)) {
    return textThoughtSignatureMap.get(trimmed);
  }
  const normalized = normalizeTextForSignature(trimmed);
  if (!normalized) return undefined;
  return textThoughtSignatureMap.get(normalized);
}

/**
 * 清理 JSON Schema，移除 Gemini 不支持的字段
 */
function cleanJsonSchema(schema) {
  if (!schema || typeof schema !== 'object') {
    return schema;
  }

  const validationFields = {
    minLength: 'minLength',
    maxLength: 'maxLength',
    minimum: 'minimum',
    maximum: 'maximum',
    minItems: 'minItems',
    maxItems: 'maxItems',
    minProperties: 'minProperties',
    maxProperties: 'maxProperties',
    pattern: 'pattern',
    format: 'format',
    multipleOf: 'multipleOf',
  };

  const fieldsToRemove = new Set([
    '$schema',
    'additionalProperties',
    'uniqueItems',
    'exclusiveMinimum',
    'exclusiveMaximum',
  ]);

  const collectValidations = obj => {
    const validations = [];
    for (const [field, value] of Object.entries(validationFields)) {
      if (field in obj) {
        validations.push(`${field}: ${obj[field]}`);
        delete obj[field];
      }
    }
    for (const field of fieldsToRemove) {
      if (field in obj) {
        if (field === 'additionalProperties' && obj[field] === false) {
          validations.push('no additional properties');
        }
        delete obj[field];
      }
    }
    return validations;
  };

  const cleanObject = (obj, path = '') => {
    if (Array.isArray(obj)) {
      return obj.map(item => (typeof item === 'object' ? cleanObject(item, path) : item));
    } else if (obj && typeof obj === 'object') {
      const validations = collectValidations(obj);
      const cleaned = {};

      // 如果有验证项但没有描述，先预设一个空描述，以便循环中处理
      if (validations.length > 0 && !Object.prototype.hasOwnProperty.call(obj, 'description')) {
        obj.description = '';
      }

      for (const [key, value] of Object.entries(obj)) {
        if (fieldsToRemove.has(key)) continue;
        if (key in validationFields) continue;

        if (key === 'description' && validations.length > 0) {
          cleaned[key] = `${value || ''} (${validations.join(', ')})`.trim();
        } else {
          cleaned[key] = typeof value === 'object' ? cleanObject(value, `${path}.${key}`) : value;
        }
      }
      if (cleaned.required && Array.isArray(cleaned.required) && cleaned.required.length === 0) {
        delete cleaned.required;
      }
      return cleaned;
    }
    return obj;
  };

  return cleanObject(schema);
}

/**
 * 将 OpenAI 格式的请求转换为 Antigravity API 格式
 */
function convertOpenAIToAntigravityRequest(openaiRequest, token) {
  let { model, messages, temperature, max_tokens, top_p, top_k, stop, tools } = openaiRequest;

  // 剥离功能性前缀（假流/、流抗/），获取基础模型名
  if (model.startsWith('假流/')) {
    model = model.substring(3);
  } else if (model.startsWith('流抗/')) {
    model = model.substring(3);
  }

  // 获取矩阵配置进行硬核校验
  const service = require('./antigravity-service');
  const matrix = service.getMatrixConfig();
  const modelConfig = matrix[model] || {};

  const hasAssistantToolCalls =
    Array.isArray(messages) &&
    messages.some(
      msg => msg?.role === 'assistant' && Array.isArray(msg.tool_calls) && msg.tool_calls.length > 0
    );

  // 基础思考能力判定：必须在矩阵中显式开启了 base 或者是特定已知模型
  const baseEnableThinking =
    model.endsWith('-thinking') ||
    model === 'gemini-2.5-pro' ||
    model.startsWith('gemini-3-pro-') ||
    model === 'rev19-uic3-1p' ||
    model === 'gpt-oss-120b-medium';

  // 核心修正：如果矩阵配置存在且三个开关全为 false，说明该模型在功能矩阵中被彻底关闭
  if (
    modelConfig.base === false &&
    modelConfig.fakeStream === false &&
    modelConfig.antiTrunc === false
  ) {
    throw new Error(`Model '${model}' is explicitly disabled in the function matrix.`);
  }

  // 只有矩阵中 base 开启，才允许思考模式 (如果是强制思考模型，则检查 base 开关)
  const enableThinking = baseEnableThinking && !(model.includes('claude') && hasAssistantToolCalls);

  // 转换 messages 到 contents
  const contents = [];
  const systemParts = []; // 收集所有 system 消息

  for (const msg of messages || []) {
    if (msg.role === 'system') {
      // 收集所有 system 消息内容
      const text = typeof msg.content === 'string' ? msg.content : JSON.stringify(msg.content);
      if (text.trim()) {
        systemParts.push(text);
      }
    } else if (msg.role === 'user') {
      const parts = [];
      if (typeof msg.content === 'string') {
        parts.push({ text: msg.content });
      } else if (Array.isArray(msg.content)) {
        for (const item of msg.content) {
          if (item.type === 'text') {
            parts.push({ text: item.text });
          } else if (item.type === 'image_url') {
            const imageUrl = item.image_url?.url || '';
            const match = imageUrl.match(/^data:image\/(\w+);base64,(.+)$/);
            if (match) {
              parts.push({
                inlineData: { mimeType: `image/${match[1]}`, data: match[2] },
              });
            } else if (imageUrl.startsWith('/uploads/')) {
              // 处理本地上传的图片 (如 /uploads/chat_images/xxx.jpg)
              try {
                // 构造文件路径: process.cwd() + /data + /uploads/...
                const relativePath = imageUrl.startsWith('/') ? imageUrl.slice(1) : imageUrl;
                const filePath = path.join(process.cwd(), 'data', relativePath);

                if (fs.existsSync(filePath)) {
                  const fileBuffer = fs.readFileSync(filePath);
                  const base64Data = fileBuffer.toString('base64');
                  const ext = path.extname(filePath).toLowerCase();
                  // 简单的 mime 类型映射
                  let mimeType = 'image/jpeg';
                  if (ext === '.png') mimeType = 'image/png';
                  else if (ext === '.webp') mimeType = 'image/webp';
                  else if (ext === '.gif') mimeType = 'image/gif';

                  parts.push({
                    inlineData: { mimeType: mimeType, data: base64Data }
                  });
                  console.log(`[Antigravity] Loaded local image: ${filePath} (${Math.round(fileBuffer.length / 1024)}KB)`);
                } else {
                  console.warn(`[Antigravity] Image file not found: ${filePath}`);
                }
              } catch (e) {
                console.error(`[Antigravity] Failed to process local image: ${e.message}`);
              }
            }
          }
        }
      }
      if (parts.length > 0) {
        contents.push({ role: 'user', parts });
      }
    } else if (msg.role === 'assistant') {
      const parts = [];
      let contentText = '';
      if (typeof msg.content === 'string') {
        contentText = msg.content;
      } else if (Array.isArray(msg.content)) {
        contentText = msg.content
          .filter(i => i.type === 'text')
          .map(i => i.text || '')
          .join('');
      }

      if (contentText.trim()) {
        const textPart = { text: contentText };
        // 仅对 gemini-3 系列尝试带回签名
        if (model.includes('gemini-3')) {
          const sigPayload = getTextThoughtSignature(contentText);
          if (sigPayload?.signature) {
            // 处理 GROUP#SIGNATURE 格式
            const parts = sigPayload.signature.split('#');
            if (parts.length === 2) {
              // 简单的逻辑：如果有 #，取后半部分。更严格的通过 modelGroup 匹配可以在之后添加
              textPart.thoughtSignature = parts[1];
            } else {
              textPart.thoughtSignature = sigPayload.signature;
            }
          }
        }
        parts.push(textPart);
      }

      if (msg.tool_calls && msg.tool_calls.length > 0) {
        for (const tc of msg.tool_calls) {
          let args = {};
          try {
            args =
              typeof tc.function.arguments === 'string'
                ? JSON.parse(tc.function.arguments)
                : tc.function.arguments || {};
          } catch (e) { }

          const part = {
            functionCall: { id: tc.id, name: tc.function.name, args },
          };

          const thoughtSignature = getThoughtSignature(tc.id);
          if (thoughtSignature) {
            // 处理 GROUP#SIGNATURE 格式
            const parts = thoughtSignature.split('#');
            if (parts.length === 2) {
              part.thoughtSignature = parts[1];
            } else {
              part.thoughtSignature = thoughtSignature;
            }
          }

          parts.push(part);
        }
      }
      if (parts.length > 0) {
        contents.push({ role: 'model', parts });
      }
    } else if (msg.role === 'tool') {
      // 找到对应的 functionName
      let functionName = '';
      for (let i = contents.length - 1; i >= 0; i--) {
        if (contents[i].role === 'model') {
          const found = contents[i].parts.find(
            p => p.functionCall && p.functionCall.id === msg.tool_call_id
          );
          if (found) {
            functionName = found.functionCall.name;
            break;
          }
        }
      }

      const functionResponse = {
        functionResponse: {
          id: msg.tool_call_id,
          name: functionName || '',
          response: {
            output: typeof msg.content === 'string' ? msg.content : JSON.stringify(msg.content),
          },
        },
      };

      const lastContent = contents[contents.length - 1];
      if (lastContent?.role === 'user' && lastContent.parts.some(p => p.functionResponse)) {
        lastContent.parts.push(functionResponse);
      } else {
        contents.push({ role: 'user', parts: [functionResponse] });
      }
    }
  }

  // 构建 generationConfig
  const generationConfig = {
    topP: top_p ?? 0.85,
    topK: top_k ?? 50,
    temperature: temperature ?? 1,
    candidateCount: 1,
    maxOutputTokens: max_tokens ?? 8096,
    stopSequences: stop
      ? Array.isArray(stop)
        ? stop
        : [stop]
      : ['<|user|>', '<|bot|>', '<|context_request|>', '<|endoftext|>', '<|end_of_turn|>'],
    thinkingConfig: {
      includeThoughts: enableThinking,
      thinkingBudget: enableThinking ? 1024 : 0,
    },
  };

  if (enableThinking && model.includes('claude')) {
    delete generationConfig.topP;
  }

  // 转换 tools 格式
  const antigravityTools =
    tools && tools.length > 0
      ? tools
        .map(tool => {
          const parameters = tool.function?.parameters
            ? cleanJsonSchema({ ...tool.function.parameters })
            : {};
          return {
            functionDeclarations: [
              {
                name: tool.function?.name,
                description: tool.function?.description,
                parameters: parameters,
              },
            ],
          };
        })
        .filter(t => t.functionDeclarations[0].name)
      : [];

  // 使用稳定的 SessionId（基于用户消息哈希）
  const sessionId = token?.sessionId || generateStableSessionId(contents);

  // 合并所有 system 消息（用双换行符分隔），如果没有则使用默认配置
  const mergedSystemText =
    systemParts.length > 0 ? systemParts.join('\n\n') : getConfig().SYSTEM_INSTRUCTION || '';

  // 检测模型类型
  const isClaude = model.toLowerCase().includes('claude');
  const isGemini3Pro = model.includes('gemini-3-pro');

  // 处理 thinking 配置
  const thinkingConfig = generationConfig.thinkingConfig;
  if (thinkingConfig && !model.startsWith('gemini-3-')) {
    // 非 Gemini-3 模型：移除 thinkingLevel，设置 thinkingBudget: -1
    if (thinkingConfig.thinkingLevel) {
      delete thinkingConfig.thinkingLevel;
      thinkingConfig.thinkingBudget = -1;
    }
  }

  // Claude 模型：确保 thinkingBudget < maxOutputTokens
  if (isClaude && thinkingConfig?.thinkingBudget > 0) {
    const maxTokens = generationConfig.maxOutputTokens || 8192;
    if (thinkingConfig.thinkingBudget >= maxTokens) {
      thinkingConfig.thinkingBudget = maxTokens - 1;
    }
  }
  // CLIProxyAPI 会在 parts[0] 插入 Antigravity 身份声明，然后追加原有 parts
  let systemInstructionParts;
  if (isClaude || isGemini3Pro) {
    // Antigravity 身份前缀
    const antigravityIdentity = 'You are Antigravity, a powerful agentic AI coding assistant designed by the Google Deepmind team working on Advanced Agentic Coding.You are pair programming with a USER to solve their coding task. The task may require creating a new codebase, modifying or debugging an existing codebase, or simply answering a question.**Absolute paths only****Proactiveness**';

    // 构建 parts 数组：先放身份声明，再追加原有内容
    systemInstructionParts = [{ text: antigravityIdentity }];
    if (mergedSystemText) {
      systemInstructionParts.push({ text: mergedSystemText });
    }
  } else {
    systemInstructionParts = [{ text: mergedSystemText }];
  }

  const request = {
    contents,
    systemInstruction: {
      role: 'user',
      parts: systemInstructionParts,
    },
    generationConfig,
    sessionId,
  };

  if (antigravityTools.length > 0) {
    request.tools = antigravityTools;
    request.toolConfig = { functionCallingConfig: { mode: 'NONE' } };
  }

  return {
    project: token?.project_id || '',
    requestId: `agent-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
    request,
    model: model,  // 使用原始模型名
    userAgent: 'antigravity',
    requestType: 'agent',
  };
}

/**
 * 列出可用模型
 */
async function listModels(accountId) {
  const token = await getValidToken(accountId);
  if (!token) throw new Error('No valid token available');

  const config = getConfig();
  const headers = buildHeaders(token);
  const req = getRequester();

  const response = await req.antigravity_fetch(config.MODELS_URL, {
    method: 'POST',
    headers,
    body: JSON.stringify({}),
    proxy: config.PROXY,
    timeout: config.TIMEOUT,
  });

  if (response.status !== 200) {
    const text = await response.text();
    throw new Error(`Failed to list models: ${text}`);
  }

  const data = await response.json();
  return {
    object: 'list',
    data: mapModels(data),
  };
}

/**
 * 处理流式响应片段
 */
function parseAndEmitStreamChunk(line, callback) {
  if (!line.startsWith('data: ')) return null;

  try {
    const data = JSON.parse(line.slice(6));
    const parts = data.response?.candidates?.[0]?.content?.parts;

    const result = {
      usage: data.response?.usageMetadata,
      done: !!data.response?.candidates?.[0]?.finishReason,
    };

    if (parts) {
      for (const part of parts) {
        // 捕获思维签名 (如果是工具调用相关的签名)
        if (part.functionCall && part.thoughtSignature) {
          registerThoughtSignature(part.functionCall.id, part.thoughtSignature);
        }

        if (part.thought === true) {
          callback({ type: 'thinking', content: part.text });
        } else if (part.text !== undefined) {
          // 捕获文本思维签名 (Gemini 3 系列)
          if (part.thoughtSignature) {
            registerTextThoughtSignature(part.text, part.thoughtSignature);
            callback({ type: 'signature', content: part.thoughtSignature });
          }
          callback({ type: 'text', content: part.text });
        } else if (part.functionCall) {
          callback({ type: 'tool_calls', tool_calls: [part.functionCall] });
        }
      }
    }

    return result;
  } catch (e) {
    return null;
  }
}

/**
 * 发送聊天补全请求 (流式)
 */
async function chatCompletionsStream(accountId, requestBody, callback) {
  const account = storage.getAccountById(accountId);
  if (!account || !account.enable) throw new Error('Account not found or disabled');

  let tokenObj = storage.getTokenByAccountId(accountId);
  if (!tokenObj) throw new Error('No valid token available');

  // 检查是否过期并刷新
  const expiresAt = tokenObj.timestamp + tokenObj.expires_in * 1000;
  if (Date.now() >= expiresAt - 300000) {
    try {
      const newToken = await refreshToken(account, tokenObj);
      tokenObj = {
        ...tokenObj,
        access_token: newToken.accessToken,
        project_id: newToken.projectId || tokenObj.project_id,
      };
    } catch (e) {
      throw new Error('Token refresh failed');
    }
  }

  const accessToken = tokenObj.access_token;

  const config = getConfig();
  const headers = buildHeaders(accessToken);
  const req = getRequester();

  const startTime = Date.now();
  let statusCode = 200;
  // 将 OpenAI 格式转换为 Antigravity API 格式
  const antigravityRequest = convertOpenAIToAntigravityRequest(requestBody, tokenObj);

  console.log(
    `[Debug] Antigravity Request: Project=${antigravityRequest.project}, Model=${antigravityRequest.model}`
  );

  try {
    const stream = req.antigravity_fetchStream(config.API_URL, {
      method: 'POST',
      headers,
      body: JSON.stringify(antigravityRequest),
      proxy: config.PROXY,
      timeout: config.TIMEOUT,
    });

    let buffer = '';
    let errorText = '';
    await new Promise((resolve, reject) => {
      stream
        .onStart(({ status }) => {
          statusCode = status;
        })
        .onData(chunk => {
          if (statusCode !== 200) {
            errorText += chunk;
            return;
          }
          buffer += chunk;
          const lines = buffer.split('\n');
          buffer = lines.pop();
          lines.forEach(line => parseAndEmitStreamChunk(line, callback));
        })
        .onEnd(() => {
          if (statusCode !== 200) {
            reject(new Error(`API Error ${statusCode}: ${errorText}`));
          } else {
            resolve();
          }
        })
        .onError(reject);
    });
  } catch (error) {
    throw error;
  }
}

/**
 * 发送聊天补全请求 (非流式)
 */
async function chatCompletions(accountId, requestBody) {
  const account = storage.getAccountById(accountId);
  if (!account || !account.enable) throw new Error('Account not found or disabled');

  let tokenObj = storage.getTokenByAccountId(accountId);
  if (!tokenObj) throw new Error('No valid token available');

  // 检查是否过期并刷新
  const expiresAt = tokenObj.timestamp + tokenObj.expires_in * 1000;
  if (Date.now() >= expiresAt - 300000) {
    try {
      const newToken = await refreshToken(account, tokenObj);
      tokenObj = {
        ...tokenObj,
        access_token: newToken.accessToken,
        project_id: newToken.projectId || tokenObj.project_id,
      };
    } catch (e) {
      throw new Error('Token refresh failed');
    }
  }

  const accessToken = tokenObj.access_token;

  const config = getConfig();
  const headers = buildHeaders(accessToken);
  const req = getRequester();

  const startTime = Date.now();
  let statusCode = 200;

  // 将 OpenAI 格式转换为 Antigravity API 格式
  const antigravityRequest = convertOpenAIToAntigravityRequest(requestBody, tokenObj);

  // Claude 和 Gemini-3-Pro 使用流式端点（CLIProxyAPI 的解决方案）
  const model = requestBody.model || '';
  const isClaude = model.toLowerCase().includes('claude');
  const isGemini3Pro = model.includes('gemini-3-pro');
  const useStreamEndpoint = isClaude || isGemini3Pro;

  // 多端点回退逻辑
  const fallbackUrls = config.FALLBACK_BASE_URLS || [];
  const apiPath = useStreamEndpoint
    ? '/v1internal:streamGenerateContent?alt=sse'
    : '/v1internal:generateContent';

  let lastError = null;
  for (let idx = 0; idx < fallbackUrls.length; idx++) {
    const baseUrl = fallbackUrls[idx];
    const fullUrl = baseUrl + apiPath;
    const urlHost = new URL(baseUrl).host;
    const requestHeaders = { ...headers, Host: urlHost };

    try {
      if (useStreamEndpoint) {
        // 使用流式端点但收集所有响应
        const result = await collectStreamResponse(req, fullUrl, requestHeaders, antigravityRequest, config, requestBody.model);
        return result;
      }

      const response = await req.antigravity_fetch(fullUrl, {
        method: 'POST',
        headers: requestHeaders,
        body: JSON.stringify(antigravityRequest),
        proxy: config.PROXY,
        timeout: config.TIMEOUT,
      });

      statusCode = response.status;

      if (statusCode === 429 && idx + 1 < fallbackUrls.length) {
        console.log(`[Antigravity] Rate limited on ${urlHost}, trying next endpoint...`);
        lastError = new Error(`API Error 429 on ${urlHost}`);
        continue;
      }

      if (statusCode !== 200) {
        const text = await response.text();
        throw new Error(`API Error ${statusCode}: ${text}`);
      }

      const data = await response.json();

      // 转换响应为 OpenAI 格式
      const parts = data.response?.candidates?.[0]?.content?.parts || [];
      let content = '';
      let reasoningContent = '';
      const toolCalls = [];

      for (const part of parts) {
        if (part.thought === true) {
          reasoningContent += part.text || '';
        } else if (part.text !== undefined) {
          content += part.text;
        } else if (part.functionCall) {
          toolCalls.push({
            id:
              part.functionCall.id || `call_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
            type: 'function',
            function: {
              name: part.functionCall.name,
              arguments: JSON.stringify(part.functionCall.args || {}),
            },
          });
        }
      }

      const usage = data.response?.usageMetadata || {};
      const result = {
        id: `chatcmpl-${Date.now().toString(36)}`,
        object: 'chat.completion',
        created: Math.floor(Date.now() / 1000),
        model: requestBody.model,
        choices: [
          {
            index: 0,
            message: {
              role: 'assistant',
              content: content,
              reasoning_content: reasoningContent,
            },
            finish_reason: data.response?.candidates?.[0]?.finishReason?.toLowerCase() || 'stop',
          },
        ],
        usage: {
          prompt_tokens: usage.promptTokenCount || 0,
          completion_tokens: usage.candidatesTokenCount || 0,
          total_tokens: (usage.promptTokenCount || 0) + (usage.candidatesTokenCount || 0),
        },
      };

      // 如果有工具调用，添加到消息中
      if (toolCalls.length > 0) {
        result.choices[0].message.tool_calls = toolCalls;
        result.choices[0].finish_reason = 'tool_calls';
      }

      return result;
    } catch (error) {
      const errorMsg = error.message || '';
      if (errorMsg.includes('429') && idx + 1 < fallbackUrls.length) {
        console.log(`[Antigravity] Rate limited, trying next endpoint...`);
        lastError = error;
        continue;
      }
      if (idx + 1 >= fallbackUrls.length) {
        throw error;
      }
      lastError = error;
    }
  }

  throw lastError || new Error('All Antigravity endpoints failed');
}

/**
 * 收集流式响应并转换为非流式格式（用于 Claude 和 Gemini-3-Pro）
 */
async function collectStreamResponse(req, url, headers, antigravityRequest, config, modelName) {
  let content = '';
  let reasoningContent = '';
  const toolCalls = [];
  let usage = {};
  let finishReason = 'stop';
  let statusCode = 200;
  let errorText = '';

  const stream = req.antigravity_fetchStream(url, {
    method: 'POST',
    headers,
    body: JSON.stringify(antigravityRequest),
    proxy: config.PROXY,
    timeout: config.TIMEOUT,
  });

  await new Promise((resolve, reject) => {
    let buffer = '';
    stream
      .onStart(({ status }) => {
        statusCode = status;
      })
      .onData(chunk => {
        if (statusCode !== 200) {
          errorText += chunk;
          return;
        }
        buffer += chunk;
        const lines = buffer.split('\n');
        buffer = lines.pop();

        for (const line of lines) {
          if (!line.startsWith('data: ')) continue;
          try {
            const data = JSON.parse(line.slice(6));

            const responseObj = data.response || data;
            const parts = responseObj?.candidates?.[0]?.content?.parts;

            if (parts) {
              for (const part of parts) {
                if (part.thought === true) {
                  reasoningContent += part.text || '';
                } else if (part.text !== undefined) {
                  content += part.text;
                } else if (part.functionCall) {
                  toolCalls.push({
                    id: part.functionCall.id || `call_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
                    type: 'function',
                    function: {
                      name: part.functionCall.name,
                      arguments: JSON.stringify(part.functionCall.args || {}),
                    },
                  });
                }
              }
            }

            if (data.response?.usageMetadata) {
              usage = data.response.usageMetadata;
            }
            if (data.response?.candidates?.[0]?.finishReason) {
              finishReason = data.response.candidates[0].finishReason.toLowerCase();
            }
          } catch (e) { /* ignore parse errors */ }
        }
      })
      .onEnd(() => {
        if (statusCode !== 200) {
          reject(new Error(`API Error ${statusCode}: ${errorText}`));
        } else {
          resolve();
        }
      })
      .onError(reject);
  });

  const result = {
    id: `chatcmpl-${Date.now().toString(36)}`,
    object: 'chat.completion',
    created: Math.floor(Date.now() / 1000),
    model: modelName,
    choices: [{
      index: 0,
      message: {
        role: 'assistant',
        content: content,
        reasoning_content: reasoningContent,
      },
      finish_reason: finishReason,
    }],
    usage: {
      prompt_tokens: usage.promptTokenCount || 0,
      completion_tokens: usage.candidatesTokenCount || 0,
      total_tokens: (usage.promptTokenCount || 0) + (usage.candidatesTokenCount || 0),
    },
  };

  if (toolCalls.length > 0) {
    result.choices[0].message.tool_calls = toolCalls;
    result.choices[0].finish_reason = 'tool_calls';
  }

  return result;
}

/**
 * 获取额度并进行模型分组
 */
async function listQuotas(accountId) {
  const token = await getValidToken(accountId);
  if (!token) throw new Error('No valid token available');

  const config = getConfig();
  const headers = buildHeaders(token);
  const req = getRequester();

  const response = await req.antigravity_fetch(config.MODELS_URL, {
    method: 'POST',
    headers,
    body: JSON.stringify({}),
    proxy: config.PROXY,
    timeout: config.TIMEOUT,
  });

  if (response.status !== 200) {
    const text = await response.text();
    throw new Error(`Failed to fetch quotas: ${text}`);
  }

  const data = await response.json();
  const models = data.models || {};

  // 定义分组规则
  const groups = [
    {
      id: '图像生成',
      name: '图像生成',
      description: 'Gemini Pro图像生成模型',
      icon: '🍌',
      patterns: ['gemini-3-pro-image', 'gemini-2.5-flash-image'],
    },
    {
      id: 'claude_gpt',
      name: 'Claude/GPT',
      description: 'Claude和GPT模型共享额度',
      icon: '🧠',
      patterns: [
        'claude-sonnet-4-5-thinking',
        'claude-opus-4-5-thinking',
        'claude-sonnet-4-5',
        'gpt-oss-120b-medium',
      ],
    },
    {
      id: 'tab_completion',
      name: 'Tab补全',
      description: 'Tab补全模型',
      icon: '📝',
      patterns: ['chat_23310', 'chat_20706'],
    },
    {
      id: 'gemini',
      name: 'Gemini',
      description: 'Gemini模型',
      icon: '💎',
      patterns: [
        'gemini-3-pro-high',
        'rev19-uic3-1p',
        'gemini-2.5-flash',
        'gemini-3-pro-low',
        'gemini-2.5-flash-thinking',
        'gemini-2.5-pro',
        'gemini-2.5-flash-lite',
      ],
    },
  ];

  const result = {};
  const processedModels = new Set();

  const formatDate = dateInput => {
    if (!dateInput) return null;
    try {
      // 支持毫秒或秒时间戳
      let val = dateInput;
      if (
        typeof val === 'number' ||
        (typeof val === 'string' && !isNaN(val) && !isNaN(parseFloat(val)))
      ) {
        val = Number(val);
        // 10位时间戳认为是秒，13位认为是毫秒
        if (val > 1000000000 && val < 9999999999) val *= 1000;
      }
      const date = new Date(val);
      if (isNaN(date.getTime())) return null;
      // 返回 ISO 字符串，由前端根据本地时区进行显示和倒计时计算
      return date.toISOString();
    } catch (e) {
      return null;
    }
  };

  const parseRemaining = quotaInfo => {
    if (!quotaInfo) return 0;

    // 提取剩余量，优先使用分数
    let val = null;
    if (quotaInfo.remainingFraction !== undefined && quotaInfo.remainingFraction !== null) {
      val = Number(quotaInfo.remainingFraction);
    } else if (quotaInfo.remaining !== undefined && quotaInfo.remaining !== null) {
      val = Number(quotaInfo.remaining);
    }

    // 如果没有获取到有效数值，默认为 0 (假设耗尽)
    if (val === null || isNaN(val)) return 0;

    // 如果是 0-1 之间的小数，转换为百分数
    if (val >= 0 && val <= 1) {
      return Math.round(val * 100);
    }

    // 如果大于 1，直接按百分比或额度数值处理
    return Math.round(val);
  };

  groups.forEach(group => {
    const groupModels = [];
    let earliestReset = null;

    Object.entries(models).forEach(([id, info]) => {
      if (processedModels.has(id)) return;

      const isMatch = group.patterns.some(p => id.toLowerCase().includes(p.toLowerCase()));
      if (isMatch) {
        processedModels.add(id);

        const modelRem = parseRemaining(info.quotaInfo);

        let modelResetTime = null;
        if (info.quotaInfo && info.quotaInfo.resetTime) {
          modelResetTime = info.quotaInfo.resetTime;
          if (!earliestReset || info.quotaInfo.resetTime < earliestReset) {
            earliestReset = info.quotaInfo.resetTime;
          }
        }

        groupModels.push({
          id: id,
          remaining: modelRem,
          resetTime: formatDate(modelResetTime),
        });
      }
    });

    if (groupModels.length > 0) {
      groupModels.sort((a, b) => a.id.localeCompare(b.id));

      // 计算平均剩余额度
      const totalRemaining = groupModels.reduce((sum, m) => sum + m.remaining, 0);
      const avgRemaining = Math.round(totalRemaining / groupModels.length);

      // 将模型列表加入描述，匹配用户界面需求
      const modelNames = groupModels.map(m => m.id).join(', ');
      const fullDescription = `(${modelNames})\n${group.description}`;

      result[group.id] = {
        name: group.name,
        description: fullDescription,
        icon: group.icon,
        models: groupModels,
        remaining: avgRemaining,
        resetTime: formatDate(earliestReset),
        modelCount: groupModels.length,
      };
    }
  });

  // 其他模型
  const others = [];
  Object.entries(models).forEach(([id, info]) => {
    if (!processedModels.has(id)) {
      const rem = parseRemaining(info.quotaInfo);
      others.push({
        id,
        remaining: rem,
        resetTime: formatDate(info.quotaInfo?.resetTime),
      });
    }
  });

  if (others.length > 0) {
    others.sort((a, b) => a.id.localeCompare(b.id));

    const totalRemaining = others.reduce((sum, m) => sum + m.remaining, 0);
    const avgRemaining = Math.round(totalRemaining / others.length);

    result['others'] = {
      name: '其他模型',
      description: '未分组模型单独计费',
      icon: '📋',
      models: others,
      remaining: avgRemaining,
      resetTime: others.length > 0 ? others[0].resetTime : null,
    };
  }

  return result;
}

module.exports = {
  getConfig,
  getValidToken,
  listModels,
  listQuotas,
  chatCompletions,
  chatCompletionsStream,
  refreshAllAccounts,
  refreshToken,
  getRequester,
  cleanJsonSchema,
  convertOpenAIToAntigravityRequest,
  parseAndEmitStreamChunk,
  registerThoughtSignature,
  registerTextThoughtSignature,
  getThoughtSignature,
  getTextThoughtSignature,
};
