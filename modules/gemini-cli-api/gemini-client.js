const axios = require('axios');
const { HttpsProxyAgent } = require('https-proxy-agent');
const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('GCLI-Client');
const AntigravityRequester = require('../antigravity-api/antigravity-requester');
const path = require('path');
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
    // 使用生产环境端点 (gcli2api 默认使用此端点)
    this.v1internalEndpoint = 'https://cloudcode-pa.googleapis.com/v1internal';

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

    // 合并所有 system 消息（用双换行符分隔）
    let systemInstruction = null;
    if (systemParts.length > 0) {
      systemInstruction = { parts: [{ text: systemParts.join('\n\n') }] };
    }

    // 如果没有消息中的 system 指令，尝试使用设置中的默认指令
    if (!systemInstruction && settings.SYSTEM_INSTRUCTION) {
      systemInstruction = { parts: [{ text: settings.SYSTEM_INSTRUCTION }] };
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
    // 显式指定 nothinking - 使用最小 budget (128)
    if (model.includes('-nothinking')) {
      return { thinkingBudget: 128, includeThoughts: model.includes('pro') };
    }
    // 显式指定 maxthinking
    if (model.includes('-maxthinking')) {
      if (model.includes('flash')) {
        return { thinkingBudget: 24576, includeThoughts: true };
      }
      return { thinkingBudget: 32768, includeThoughts: true };
    }
    // Gemini 3 系列必须包含 thinkingConfig (参考 CatieCli)
    if (model.includes('gemini-3')) {
      if (model.includes('flash')) {
        return { thinkingBudget: 2048, includeThoughts: true };
      }
      return { thinkingBudget: 4096, includeThoughts: true };
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

      const antigravityEndpoint = 'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal';
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

    const action = openaiRequest.stream ? 'streamGenerateContent' : 'generateContent';
    // 只有流式请求才需要 alt=sse 参数
    const url = openaiRequest.stream
      ? `${this.v1internalEndpoint}:${action}?alt=sse`
      : `${this.v1internalEndpoint}:${action}`;

    // 获取基础模型名（移除前缀和后缀）
    const baseModel = this._getBaseModelName(openaiRequest.model);

    const requestBody = {
      model: baseModel,
      project: projectId || '', // 使用获取到的项目 ID
      request: geminiPayload,
    };

    logger.debug(`Sending request: URL=${url}, model=${requestBody.model}, project=${requestBody.project}`);
    logger.debug(`Full payload: ${JSON.stringify(requestBody).substring(0, 1000)}`);
    logger.debug(`generationConfig: ${JSON.stringify(geminiPayload.generationConfig)}`);

    const axiosConfig = await this.getAxiosConfig();
    return axios.post(url, requestBody, {
      ...axiosConfig,
      headers: {
        Authorization: `Bearer ${accessToken}`,
        'Content-Type': 'application/json',
        'User-Agent': this.userAgent,
      },
      responseType: openaiRequest.stream ? 'stream' : 'json',
      timeout: 120000, // 增加到 120s 超时，深度思考模型响应较慢
    });
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
}

module.exports = new GeminiCliClient();
