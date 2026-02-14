const axios = require('axios');
const { HttpsProxyAgent } = require('https-proxy-agent');
const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('GCLI-Client');
const AntigravityRequester = require('../antigravity-api/antigravity-requester');
const path = require('path');
const { PassThrough } = require('stream');
let storage;
try {
  storage = require('./storage');
} catch (e) {
  // 允许在没有 sqlite3 的环境下加载类用于逻辑测试
  storage = null;
}

class GeminiCliClient {
  constructor() {
    this.userAgent = 'GeminiCLI/0.1.5 (Windows; AMD64)';
    // 使用 daily 环境端点 (与 Antigravity 保持一致)
    this.v1internalEndpoint = 'https://daily-cloudcode-pa.googleapis.com/v1internal';

    // 初始化 Requester (借用 Antigravity 的二进制)
    this.requester = new AntigravityRequester({
      binPath: path.join(__dirname, '../antigravity-api/bin'),
    });

    // 内存缓存，提升首字响应速度 (TTFB)
    this.projectCache = new Map();
    this.tokenCache = new Map(); // accountId -> {token, expiry}
  }

  /**
   * 关闭资源
   */
  close() {
    if (this.requester) {
      this.requester.close();
    }
  }

  /**
   * 将 OpenAI 请求转换为 Gemini 原生负载
   */
  async convertOpenAIToGemini(openaiRequest) {
    const { messages, model, stream, temperature, top_p, max_tokens, stop } = openaiRequest;

    const settings = storage ? await storage.getSettings() : {};

    const contents = [];
    const systemParts = []; // 收集所有 system 消息

    messages.forEach(msg => {
      if (msg.role === 'system') {
        // 收集所有 system 消息内容
        const textContent = this._extractTextContent(msg.content);
        if (textContent.trim()) {
          systemParts.push(textContent);
        }
      } else {
        const role = msg.role === 'assistant' ? 'model' : 'user';
        const parts = this._convertContentToParts(msg.content);

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
    });

    // 注入实时时间锚点，防止模型在 search 模式下产生时间幻觉
    const now = new Date();
    const currentTimeStr = `Current Time: ${now.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })} (Beijing Time)\n\n`;

    // 合并所有 system 消息（用双换行符分隔）
    let systemInstruction = null;
    if (systemParts.length > 0) {
      systemInstruction = { parts: [{ text: currentTimeStr + systemParts.join('\n\n') }] };
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

    const thinkingConfig = this._getThinkingConfig(model);
    if (thinkingConfig) {
      generationConfig.thinkingConfig = thinkingConfig;

      if (generationConfig.maxOutputTokens && thinkingConfig.thinkingBudget) {
        if (generationConfig.maxOutputTokens < thinkingConfig.thinkingBudget + 1024) {
          generationConfig.maxOutputTokens = Math.min(thinkingConfig.thinkingBudget + 4096, 65536);
          logger.info(`Adjusted maxOutputTokens for thinking budget (${thinkingConfig.thinkingBudget}): ${generationConfig.maxOutputTokens}`);
        }
      }
    }

    // 构建请求体（参考 CatieCli，只添加有值的字段）
    const payload = { contents };

    if (Object.keys(generationConfig).length > 0) {
      payload.generationConfig = generationConfig;
    }

    if (systemInstruction) {
      payload.systemInstruction = systemInstruction;
    }

    payload.safetySettings = [
      { category: 'HARM_CATEGORY_HARASSMENT', threshold: 'BLOCK_NONE' },
      { category: 'HARM_CATEGORY_HATE_SPEECH', threshold: 'BLOCK_NONE' },
      { category: 'HARM_CATEGORY_SEXUALLY_EXPLICIT', threshold: 'BLOCK_NONE' },
      { category: 'HARM_CATEGORY_DANGEROUS_CONTENT', threshold: 'BLOCK_NONE' },
      { category: 'HARM_CATEGORY_CIVIC_INTEGRITY', threshold: 'BLOCK_NONE' },
    ];

    return payload;
  }

  /**
   * 提取内容中的文本部分
   * @param {string|Array} content - OpenAI 消息内容
   * @returns {string} 文本内容
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
   * 将 OpenAI 格式的 content 转换为 Gemini parts
   * 支持多模态输入（文本 + 图像）
   * @param {string|Array} content - OpenAI 消息内容
   * @returns {Array} Gemini parts 数组
   */
  _convertContentToParts(content) {
    // 简单字符串
    if (typeof content === 'string') {
      return [{ text: content }];
    }

    // 数组格式 (多模态)
    if (Array.isArray(content)) {
      const parts = [];
      for (const item of content) {
        if (item.type === 'text') {
          parts.push({ text: item.text || '' });
        } else if (item.type === 'image_url') {
          const imageUrl = item.image_url?.url || '';
          const imagePart = this._parseImageUrl(imageUrl);
          if (imagePart) {
            parts.push(imagePart);
          }
        }
      }
      return parts.length > 0 ? parts : [{ text: '' }];
    }

    // 其他类型，转为字符串
    return [{ text: String(content || '') }];
  }

  /**
   * 解析图像 URL 并转换为 Gemini inlineData 格式
   * @param {string} imageUrl - 图像 URL (base64 data URI 或 HTTP URL)
   * @returns {Object|null} Gemini inlineData part
   */
  _parseImageUrl(imageUrl) {
    if (!imageUrl) return null;

    // 处理 Base64 Data URI: data:image/jpeg;base64,/9j/4AAQ...
    const base64Match = imageUrl.match(/^data:image\/(\w+);base64,(.+)$/);
    if (base64Match) {
      return {
        inlineData: {
          mimeType: `image/${base64Match[1]}`,
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

          logger.info(`[Gemini-Client] Loaded local image: ${filePath} (${Math.round(fileBuffer.length / 1024)}KB)`);

          return {
            inlineData: {
              mimeType: mimeType,
              data: base64Data
            }
          };
        } else {
          logger.warn(`[Gemini-Client] Image file not found: ${filePath}`);
          return null;
        }
      } catch (e) {
        logger.error(`[Gemini-Client] Failed to process local image: ${e.message}`);
        return null;
      }
    }

    // HTTP/HTTPS URL 暂不支持（需要下载图片）
    if (imageUrl.startsWith('http://') || imageUrl.startsWith('https://')) {
      logger.warn(`HTTP image URLs not yet supported, skipping: ${imageUrl.substring(0, 80)}`);
      return null;
    }

    logger.warn(`Unsupported image URL format: ${imageUrl.substring(0, 50)}`);
    return null;
  }

  /**
   * 根据模型名获取 thinking 配置 (参考 gcli2api utils.py)
   */
  _getThinkingConfig(model) {
    // 1. 显式指定 nothinking：彻底禁用，不返回任何配置对象
    if (model.includes('-nothinking')) {
      return null;
    }

    // 2. 显式指定 maxthinking：根据版本设置最高预算/等级
    if (model.includes('-maxthinking')) {
      if (model.includes('gemini-3')) {
        return { thinkingLevel: 'HIGH', includeThoughts: true };
      }
      if (model.includes('flash')) {
        return { thinkingBudget: 24576, includeThoughts: true };
      }
      return { thinkingBudget: 65536, includeThoughts: true };
    }

    // 3. 默认配置处理
    // Gemini 3 系列默认使用 thinkingLevel
    if (model.includes('gemini-3')) {
      return { thinkingLevel: 'HIGH', includeThoughts: true };
    }
    // Gemini 2.5 系列默认开启思考，使用官方默认 budget
    if (model.includes('gemini-2.5')) {
      return { thinkingBudget: 8192, includeThoughts: true };
    }

    // 其他模型 (如 2.0/1.5) 不需要默认 thinkingConfig
    return null;
  }

  /**
   * 获取基础模型名 (移除前缀和后缀)
   * 参考 CatieCli 的 _map_model_name 方法
   */
  _getBaseModelName(model) {
    // 移除前缀
    const prefixes = ['假流/', '流抗/'];
    for (const prefix of prefixes) {
      if (model.startsWith(prefix)) {
        model = model.substring(prefix.length);
        break;
      }
    }

    // 移除后缀 (按长度从长到短排序以避免匹配问题)
    const suffixes = [
      '-maxthinking-search',
      '-nothinking-search',
      '-maxthinking',
      '-nothinking',
      '-search',
    ];
    for (const suffix of suffixes) {
      if (model.endsWith(suffix)) {
        model = model.substring(0, model.length - suffix.length);
        break;
      }
    }

    return model;
  }

  /**
   * 获取 Axios 配置 (包含代理设置)
   */
  async getAxiosConfig() {
    if (!storage) return {};

    try {
      const settings = await storage.getSettings();
      const proxyUrl = settings.PROXY;

      if (proxyUrl && (proxyUrl.startsWith('http://') || proxyUrl.startsWith('https://'))) {
        // 使用 httpsAgent 确保代理对 HTTPS 请求生效
        return {
          httpsAgent: new HttpsProxyAgent(proxyUrl),
          proxy: false, // 禁用 axios 默认代理，使用 httpsAgent
        };
      }
    } catch (e) {
      logger.error(`Failed to parse proxy settings: ${e.message}`);
    }
    return {};
  }

  /**
   * 获取有效的 Access Token (带缓存和自动刷新逻辑)
   */
  async getAccessToken(accountId) {
    // 1. 内存缓存优先
    const cached = this.tokenCache.get(accountId);
    const now = Math.floor(Date.now() / 1000);
    if (cached && cached.expiry > now + 60) {
      return cached.token;
    }

    const account = storage.getAccounts().find(a => a.id === accountId);
    if (!account) throw new Error('Account not found');

    // 2. 数据库缓存
    const tokenRecord = storage.getTokenByAccountId(accountId);
    if (tokenRecord && tokenRecord.expires_at > now + 60) {
      this.tokenCache.set(accountId, { token: tokenRecord.access_token, expiry: tokenRecord.expires_at });
      return tokenRecord.access_token;
    }

    // 刷新 Token
    logger.info(`Refreshing token for account ${account.name}...`);
    const params = new URLSearchParams({
      client_id: account.client_id,
      client_secret: account.client_secret,
      refresh_token: account.refresh_token,
      grant_type: 'refresh_token',
    });

    const axiosConfig = await this.getAxiosConfig();
    const resp = await axios.post('https://oauth2.googleapis.com/token', params.toString(), {
      ...axiosConfig,
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    });

    const newToken = resp.data.access_token;
    const newExpiresAt = now + resp.data.expires_in;

    storage.saveToken({
      id: accountId, // 简单起见，用 accountId 作为 token条目 ID
      account_id: accountId,
      access_token: newToken,
      expires_at: newExpiresAt,
      project_id: account.project_id || 'unknown',
      email: account.email,
    });

    this.tokenCache.set(accountId, { token: newToken, expiry: newExpiresAt });

    return newToken;
  }

  /**
   * 获取 GCP 项目 ID
   * 优先从 cloudresourcemanager API 获取，失败则从 loadCodeAssist 获取
   * (参考 CatieCli + gcli2api 双重实现)
   */
  async fetchGcpProjectId(accountId) {
    const account = storage.getAccounts().find(a => a.id === accountId);
    if (!account) throw new Error('Account not found');

    // 1. 内存缓存优先 (最快)
    if (this.projectCache.has(accountId)) {
      return this.projectCache.get(accountId);
    }

    // 2. 数据库缓存优先
    const cachedId = account.project_id || account.cloudaicompanion_project_id;
    if (cachedId) {
      this.projectCache.set(accountId, cachedId);
      return cachedId;
    }

    const accessToken = await this.getAccessToken(accountId);
    const axiosConfig = await this.getAxiosConfig();

    // 方案1: 尝试从 cloudresourcemanager 获取 GCP 项目 ID (CatieCli 方式)
    try {
      logger.info('Fetching GCP Project ID from cloudresourcemanager...');

      const resp = await axios.get('https://cloudresourcemanager.googleapis.com/v1/projects', {
        ...axiosConfig,
        params: { filter: 'lifecycleState:ACTIVE' },
        headers: {
          Authorization: `Bearer ${accessToken}`,
          'Content-Type': 'application/json',
        },
      });

      const projects = resp.data.projects || [];
      if (projects.length > 0) {
        // 优先选择包含 "default" 的项目
        let projectId = '';
        for (const p of projects) {
          const pid = p.projectId || '';
          const pname = p.name || '';
          if (pid.toLowerCase().includes('default') || pname.toLowerCase().includes('default')) {
            projectId = pid;
            break;
          }
        }
        if (!projectId) {
          projectId = projects[0].projectId || '';
        }

        if (projectId) {
          logger.info(`Successfully fetched GCP Project ID: ${projectId}`);
          this.projectCache.set(accountId, projectId);
          storage.updateAccount(accountId, { project_id: projectId });
          return projectId;
        }
      }
    } catch (e) {
      logger.info(`cloudresourcemanager failed: ${e.message}, trying loadCodeAssist...`);
    }

    // 方案2: Fallback 到 loadCodeAssist (gcli2api 方式)
    try {
      logger.info('Fetching cloudaicompanionProject from loadCodeAssist...');

      const antigravityEndpoint = 'https://daily-cloudcode-pa.googleapis.com/v1internal';
      const loadCodeAssistUrl = `${antigravityEndpoint}:loadCodeAssist`;

      const resp = await axios.post(
        loadCodeAssistUrl,
        { metadata: { ideType: 'ANTIGRAVITY' } },
        {
          ...axiosConfig,
          headers: {
            Authorization: `Bearer ${accessToken}`,
            'Content-Type': 'application/json',
            'User-Agent': this.userAgent,
          },
        }
      );

      const cloudaicompanionProject = resp.data.cloudaicompanionProject;
      logger.debug(`loadCodeAssist response: ${JSON.stringify(resp.data).substring(0, 500)}`);

      if (cloudaicompanionProject) {
        logger.info(`Successfully fetched cloudaicompanionProject: ${cloudaicompanionProject}`);

        // 直接更新数据库
        try {
          storage.updateAccount(accountId, {
            cloudaicompanion_project_id: cloudaicompanionProject,
          });
          logger.info('Cached cloudaicompanion_project_id to database');
        } catch (dbErr) {
          logger.error(`Failed to cache cloudaicompanion_project_id: ${dbErr.message}`);
        }

        return cloudaicompanionProject;
      }
    } catch (e) {
      logger.error(`loadCodeAssist failed: ${e.message}`);
    }

    logger.warn('Unable to fetch any project ID');
    return '';
  }

  /**
   * 发送生成请求 (支持流式和非流式)
   */
  async generateContent(openaiRequest, accountId) {
    const accessToken = await this.getAccessToken(accountId);
    const geminiPayload = await this.convertOpenAIToGemini(openaiRequest);

    // 获取 GCP 项目 ID (参考 CatieCli)
    const projectId = await this.fetchGcpProjectId(accountId);

    const endpoints = [
      'https://cloudcode-pa.googleapis.com/v1internal',
      'https://daily-cloudcode-pa.googleapis.com/v1internal',
      'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal',
    ];

    let lastError = null;
    const settings = storage ? await storage.getSettings() : {};
    const proxy = settings.PROXY || null;

    for (let i = 0; i < endpoints.length; i++) {
      const endpoint = endpoints[i];

      // 获取基础模型名（移除前缀和后缀）
      const baseModel = this._getBaseModelName(openaiRequest.model);
      const isGemini3 = baseModel.includes('gemini-3');
      const isClaude = baseModel.toLowerCase().includes('claude');
      const shouldUseStreamEndpoint = openaiRequest.stream || isGemini3 || isClaude;

      const action = shouldUseStreamEndpoint ? 'streamGenerateContent' : 'generateContent';
      const url = shouldUseStreamEndpoint
        ? `${endpoint}:${action}?alt=sse`
        : `${endpoint}:${action}`;

      // 修正模型名称 (Gemini 3 不需要改名，但需要 Thinking Config)
      let apiModel = baseModel;


      const requestBody = {
        model: apiModel,
        project: projectId || '', // 使用获取到的项目 ID
        request: geminiPayload,
        userAgent: 'antigravity',
        requestType: baseModel.toLowerCase().includes('image') ? 'image_gen' : 'agent'
      };

      const headers = {
        Authorization: `Bearer ${accessToken}`,
        'Content-Type': 'application/json',
        'User-Agent': this.userAgent,
        'Host': new URL(endpoint).host, // Explicitly set Host header
      };

      if (requestBody.model) {
        if (requestBody.model.toLowerCase().includes('image')) {
          headers['requestType'] = 'image_gen';
        } else {
          headers['requestType'] = 'agent';
        }
      }


      const reqOptions = {
        method: 'POST',
        headers: headers,
        body: JSON.stringify(requestBody),
        proxy: proxy,
        timeout: 120000
      };

      let endpointError = null;
      for (let attempt = 0; attempt < 3; attempt++) {
        try {
          if (openaiRequest.stream) {
            // --- Stream Request (True Stream) ---
            const passThrough = new PassThrough();
            const s = this.requester.antigravity_fetchStream(url, reqOptions);
            let streamStatus = 200;
            let streamHeaders = {};

            // Attach listeners immediately to avoid missing data
            s.onData(chunk => passThrough.write(chunk));
            s.onEnd(() => passThrough.end());
            s.onError(err => passThrough.destroy(err));

            await new Promise((resolve, reject) => {
              s.onStart(info => {
                streamStatus = info.status;
                streamHeaders = info.headers;
                resolve();
              });
              s.onError((err) => reject(err)); // Handle connect error
            });

            // If status is critical error, we might want to throw to trigger retry
            // But we already started piping to passThrough. 
            // Ideally we should wait for response before returning pipe?
            // If 404/429, we should catch it here.

            if (streamStatus !== 200) {
              // Wait for full error text
              const errorText = await new Promise((resolve) => {
                let text = '';
                // We need to capture chunks from passThrough or s? 
                // s is already piping to passThrough. We can't double read s.
                // But passThrough is readable.
                // Actually, if we return passThrough, the caller reads it.
                // BUT we want to retry on 429.
                // So we must intercept.
                // Re-implementing aggregation on 's' for error case is hard if we pipe.
                resolve('Stream error occurred (status ' + streamStatus + ')');
              });
              // For true stream, accurate error body capture is hard if we stick to this structure.
              // Simplification: Throw status error immediately.
              const err = new Error(`Stream request failed with status ${streamStatus}`);
              err.response = { status: streamStatus, data: errorText };
              throw err;
            }

            return {
              status: 200,
              data: passThrough,
              headers: streamHeaders
            };

          } else {
            // --- Non-Stream Request (or Forced Stream) ---
            if (shouldUseStreamEndpoint) {
              let chunks = [];
              let finished = false;
              let streamError = null;
              let streamStatus = 200;
              let streamHeaders = {};
              const startTime = Date.now();

              const s = this.requester.antigravity_fetchStream(url, reqOptions);

              s.onData(c => chunks.push(c));
              s.onEnd(() => finished = true);
              s.onError(e => streamError = e);

              await new Promise((resolve, reject) => {
                s.onStart(info => {
                  streamStatus = info.status;
                  streamHeaders = info.headers;
                  resolve();
                });
                s.onError(reject);
              });

              // Wait for completion
              while (!finished && !streamError && (Date.now() - startTime < 120000)) {
                await new Promise(r => setTimeout(r, 50));
              }

              if (!finished && !streamError) { // Timeout
                throw new Error('Stream timeout');
              }
              if (streamError) throw streamError;

              const fullResponse = chunks.join('');

              if (streamStatus !== 200) {
                // The `errorText` variable is not defined in this scope.
                // Assuming the intent was to include `fullResponse` which contains the error body.
                // If `errorText` was intended to be a specific variable, it needs to be defined.
                // For now, I'm using `fullResponse` to ensure syntactic correctness and logical consistency.
                const err = new Error(`Request failed with status code ${streamStatus}. ProjectID=${projectId}. Body: ${fullResponse}`);
                err.response = { status: streamStatus, data: fullResponse };
                throw err;
              }

              // Parse SSE / JSON chunks
              const parts = [];
              let finishReason = 'STOP';
              let usageMetadata = {};
              const lines = fullResponse.split('\n');
              for (const line of lines) {
                if (line.trim().startsWith('data: ')) {
                  try {
                    const data = JSON.parse(line.trim().substring(6));
                    const realData = data.response || data;
                    const candidate = realData.candidates?.[0];
                    const chunkParts = candidate?.content?.parts || [];
                    for (const p of chunkParts) {
                      parts.push(p);
                    }
                    if (candidate?.finishReason) {
                      finishReason = candidate.finishReason;
                    }
                    if (realData.usageMetadata) {
                      usageMetadata = realData.usageMetadata;
                    }
                  } catch (e) {
                    logger.error(`[Gemini-Client] JSON parse error in SSE: ${e.message}`);
                  }
                }
              }

              return {
                status: 200,
                data: {
                  candidates: [
                    {
                      content: {
                        parts: parts.length > 0 ? parts : [{ text: '' }],
                        role: 'model'
                      },
                      finishReason: finishReason,
                      index: 0
                    }
                  ],
                  usageMetadata: usageMetadata
                },
                headers: streamHeaders
              };

            } else {
              // Standard non-stream
              const res = await this.requester.antigravity_fetch(url, reqOptions);
              if (res.status !== 200) {
                const errText = await res.text();
                const error = new Error(`Request failed with status code ${res.status}`);
                error.response = { status: res.status, data: errText };
                throw error;
              }
              return {
                status: res.status,
                data: await res.json(),
                headers: res.headers
              };
            }
          }
        } catch (e) {
          endpointError = e;
          const status = e.response?.status;
          lastError = endpointError;

          // 404: Try next endpoint immediately
          if (status === 404) {
            break;
          }

          // 429 or 5xx: Retry
          if (status === 429 || (status >= 500 && status < 600)) {
            if (attempt < 2) {
              const delay = (attempt + 1) * 2000 + Math.random() * 1000;
              logger.warn(`Endpoint ${endpoint} hit ${status}, retrying in ${Math.round(delay)}ms...`);
              await new Promise(r => setTimeout(r, delay));
              continue;
            }
          } else {
            // Other errors: break
            break;
          }
        }
      }

      // Endpoint failed
      const status = endpointError?.response?.status;
      lastError = endpointError;
      if (i < endpoints.length - 1) {
        logger.warn(`Endpoint ${endpoint} failed with ${status}, trying next...`);
        continue;
      }
      throw endpointError;
    }
    throw lastError;
  }

  /**
   * 获取账号的额度信息 (正式接口)
   * 参考官方 Gemini CLI: packages/core/src/code_assist/server.ts
   * @returns {{ buckets: Array, tier: object, project: string } | null}
   */
  async retrieveUserQuota(account) {
    try {
      const accessToken = await this.getAccessToken(account.id);
      const settings = storage ? storage.getSettings() : {};
      const proxy = settings.PROXY || null;
      const codeAssistBase = 'https://cloudcode-pa.googleapis.com/v1internal';

      // 确定 cloudaicompanionProject
      let companionProject = account.cloudaicompanion_project_id;

      // 如果没有存储的 project，尝试通过 loadCodeAssist 获取
      if (!companionProject) {
        const loadResp = await this.requester.antigravity_fetch(
          `${codeAssistBase}:loadCodeAssist`,
          {
            method: 'POST',
            headers: {
              Authorization: `Bearer ${accessToken}`,
              'Content-Type': 'application/json',
              'User-Agent': this.userAgent,
            },
            body: JSON.stringify({ metadata: { ideType: 'GEMINI_CLI' } }),
            proxy,
            timeout: 15000,
          }
        );
        if (loadResp.status === 200) {
          const loadData = await loadResp.json();
          companionProject = loadData.cloudaicompanionProject;
          // 如果成功获取，更新到数据库
          if (companionProject && storage) {
            storage.updateAccount(account.id, { cloudaicompanion_project_id: companionProject });
          }
        }
      }

      if (!companionProject) {
        logger.warn(`[retrieveUserQuota] Account ${account.name}: No cloudaicompanionProject`);
        return null;
      }

      // 调用 retrieveUserQuota
      const quotaResp = await this.requester.antigravity_fetch(
        `${codeAssistBase}:retrieveUserQuota`,
        {
          method: 'POST',
          headers: {
            Authorization: `Bearer ${accessToken}`,
            'Content-Type': 'application/json',
            'User-Agent': this.userAgent,
          },
          body: JSON.stringify({ project: companionProject }),
          proxy,
          timeout: 15000,
        }
      );

      if (quotaResp.status !== 200) {
        const errText = await quotaResp.text();
        logger.warn(`[retrieveUserQuota] Account ${account.name}: HTTP ${quotaResp.status} - ${errText}`);
        return null;
      }

      const quotaData = await quotaResp.json();

      // 解析 buckets：
      // 1. 过滤掉 _vertex 后缀的重复项
      // 2. 按 modelId 聚合，取最小 remainingFraction（最受限的 tokenType 为准）
      //    避免因 INPUT_TOKENS / OUTPUT_TOKENS 顺序不确定导致数值跳动
      const rawBuckets = (quotaData.buckets || [])
        .filter(b => !b.modelId?.endsWith('_vertex'));

      const bucketMap = new Map();
      for (const b of rawBuckets) {
        const existing = bucketMap.get(b.modelId);
        const fraction = b.remainingFraction ?? 1;
        if (!existing || fraction < existing.remainingFraction) {
          bucketMap.set(b.modelId, {
            modelId: b.modelId,
            remainingFraction: fraction,
            resetTime: b.resetTime,
            tokenType: b.tokenType,
          });
        }
      }
      const buckets = Array.from(bucketMap.values());

      return {
        accountId: account.id,
        accountName: account.name,
        project: companionProject,
        buckets,
      };
    } catch (e) {
      logger.error(`[retrieveUserQuota] Account ${account.name}: ${e.message}`);
      return null;
    }
  }

  /**
   * 获取模型额度信息
   */
  async getQuotas(account) {
    try {
      const accessToken = await this.getAccessToken(account.id);

      // 调用 Google 内部 API 获取模型列表
      const modelsUrl = `${this.v1internalEndpoint}:fetchAvailableModels`;
      logger.info(`Fetching model list from ${modelsUrl}...`);

      const settings = storage ? await storage.getSettings() : {};
      const proxy = settings.PROXY || null;

      // 改用 AntigravityRequester 以绕过指纹校验
      const response = await this.requester.antigravity_fetch(modelsUrl, {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${accessToken}`,
          'Content-Type': 'application/json',
          'User-Agent': this.userAgent,
        },
        body: JSON.stringify({}),
        proxy: proxy,
        timeout: 30000,
      });

      if (response.status !== 200) {
        const errorBody = await response.text();
        throw new Error(`HTTP ${response.status}: ${errorBody}`);
      }

      const data = await response.json();
      const disabledModels = storage?.getDisabledModels() || [];
      const quotas = {};

      if (data && data.models) {
        const models = data.models;
        Object.entries(models).forEach(([modelId, modelData]) => {
          if (!modelId) return;

          quotas[modelId] = {
            remaining:
              modelData.quotaInfo?.remainingFraction || modelData.quotaInfo?.remaining || 100,
            resetTime: modelData.quotaInfo?.resetTime || null,
            enabled: !disabledModels.includes(modelId),
          };
        });
        logger.info(`Successfully fetched ${Object.keys(quotas).length} models`);
      } else {
        logger.warn('Empty model list received, using fallback');
        throw new Error('Empty model list');
      }

      return quotas;
    } catch (e) {
      const is403 = e.message.includes('403') || (e.response && e.response.status === 403);
      if (is403) {
        logger.warn('Account does not have permission to fetch models online (403), using fallback.');
      } else {
        const errorMsg = e.response
          ? `HTTP ${e.response.status}: ${JSON.stringify(e.response.data)}`
          : e.message;
        logger.error(`Failed to fetch models: ${errorMsg}`);
      }

      // 失败时提供备选模型列表，防止前端显示空白
      const fallbackModels = [
        // 3.0 系列 (预览版)
        'gemini-3-pro-preview',
        'gemini-3-flash-preview',

        // 2.5 系列
        'gemini-2.5-pro',
        'gemini-2.5-flash',

        // 2.0 系列
        'gemini-2.0-flash-exp',
        'gemini-2.0-flash-thinking-exp',

        // 1.5 系列
        'gemini-1.5-pro',
        'gemini-1.5-flash',
        'gemini-1.5-flash-8b',

        // 1.0 / Exp 系列
        'gemini-1.0-pro',
        'gemini-exp-1206',
      ];

      const quotas = {};
      const disabledModels = storage?.getDisabledModels() || [];

      fallbackModels.forEach(modelId => {
        quotas[modelId] = {
          remaining: 100,
          resetTime: null,
          enabled: !disabledModels.includes(modelId),
        };
      });

      return quotas;
    }
  }

  /**
   * 收集 Gemini SSE 流式响应并聚合成 JSON 对象
   */
  async _collectStreamResponse(streamRes) {
    let buffer = '';
    const parts = [];
    let finishReason = 'STOP';

    await new Promise((resolve, reject) => {
      streamRes.onData(chunk => {
        buffer += chunk;
        const lines = buffer.split('\n');
        buffer = lines.pop(); // Keep incomplete line

        for (const line of lines) {
          if (line.trim().startsWith('data: ')) {
            const jsonStr = line.trim().substring(6);
            try {
              const data = JSON.parse(jsonStr);
              const realData = data.response || data;
              // Extract text
              const chunkParts = realData.candidates?.[0]?.content?.parts || [];
              for (const p of chunkParts) {
                parts.push(p);
              }
              if (realData.candidates?.[0]?.finishReason) {
                finishReason = realData.candidates[0].finishReason;
              }
            } catch (e) { }
          }
        }
      });
      streamRes.onEnd(resolve);
      streamRes.onError(reject);
    });

    // Construct minimal Gemini response
    return {
      candidates: [
        {
          content: {
            parts: parts.length > 0 ? parts : [{ text: '' }],
            role: 'model'
          },
          finishReason: finishReason,
          index: 0
        }
      ]
    };
  }
}

module.exports = new GeminiCliClient();
