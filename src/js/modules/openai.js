/**
 * OpenAI API 模块
 * 负责 OpenAI API 相关功能
 */

import { store } from '../store.js';
import { toast } from './toast.js';

// 缓存 key 常量（定义在模块级别，避免 Vue 警告）
const OPENAI_CACHE_KEY = 'openai_endpoints_cache';
const imageUploadCache = new Map(); // 图片上传缓存
import { renderMarkdown } from './utils.js';

export const openaiMethods = {
  // 从内容中提取思考标签（支持各种变体如 <think>, <think_nya>, <thinking> 等）
  extractThinkingContent(content) {
    if (!content || typeof content !== 'string') return { thinking: '', cleaned: content || '' };

    // 匹配各种思考标签变体: <think>, <think_nya>, <thinking>, etc.
    const thinkingPattern = /<(think(?:ing|_\w+)?)\s*>([\s\S]*?)<\/\1>/gi;
    let thinking = '';
    let cleaned = content;

    let match;
    while ((match = thinkingPattern.exec(content)) !== null) {
      thinking += match[2].trim() + '\n';
    }

    // 移除所有思考标签
    cleaned = content.replace(thinkingPattern, '').trim();

    return { thinking: thinking.trim(), cleaned };
  },

  // 带缓存的消息渲染（避免 Base64 图片导致的重复计算）
  getCachedMessageHtml(msg, field = 'content') {
    if (!msg) return '';
    let content = msg[field];
    if (content === undefined || content === null) return '';

    // 生成缓存 key
    const cacheKey = `_cached_${field}`;
    const contentKey = `_cachedSource_${field}`;

    // 检查缓存是否有效（内容未变化）
    const contentHash = typeof content === 'string' ? content : JSON.stringify(content);
    if (msg[cacheKey] && msg[contentKey] === contentHash) {
      return msg[cacheKey];
    }

    // 对于 content 字段，先过滤思考标签
    if (field === 'content' && typeof content === 'string') {
      const { thinking, cleaned } = this.extractThinkingContent(content);

      // 如果提取到了思考内容且 msg.reasoning 为空，自动填充
      if (thinking && !msg.reasoning) {
        msg.reasoning = thinking;
        msg.showReasoning = false; // 默认折叠
      }

      content = cleaned;
    }

    // 渲染并缓存
    const html = renderMarkdown(content);
    msg[cacheKey] = html;
    msg[contentKey] = contentHash;
    return html;
  },

  // 安全获取会话标题（防止巨大 JSON 导致渲染卡顿）
  getSafeSessionTitle(title) {
    if (!title) return '新对话';
    // 检测是否是 JSON 数组格式（历史遗留的多模态数据）
    if (typeof title === 'string' && title.startsWith('[')) {
      try {
        const arr = JSON.parse(title);
        if (Array.isArray(arr)) {
          const textParts = arr.filter(p => p && p.type === 'text').map(p => p.text);
          if (textParts.length > 0) {
            const text = textParts.join(' ');
            return text.slice(0, 30) + (text.length > 30 ? '...' : '');
          }
          return '📷 图片对话';
        }
      } catch (e) {
        // 不是有效 JSON，继续正常处理
      }
    }
    // 限制长度，防止超长字符串
    if (title.length > 50) {
      return title.slice(0, 50) + '...';
    }
    return title;
  },

  // 从本地缓存加载端点数据（立即显示）
  loadFromOpenaiCache() {
    try {
      const cached = localStorage.getItem(OPENAI_CACHE_KEY);
      if (cached) {
        const data = JSON.parse(cached);
        if (data && Array.isArray(data.endpoints)) {
          store.openaiEndpoints = data.endpoints.map(ep => ({
            ...ep,
            showKey: false,
            refreshing: false,
          }));
          return true;
        }
      }
    } catch (e) {
      console.warn('加载 OpenAI 缓存失败:', e);
    }
    return false;
  },

  // 保存端点数据到本地缓存
  saveToOpenaiCache(endpoints) {
    try {
      localStorage.setItem(
        OPENAI_CACHE_KEY,
        JSON.stringify({
          endpoints,
          timestamp: Date.now(),
        })
      );
    } catch (e) {
      console.warn('保存 OpenAI 缓存失败:', e);
    }
  },

  switchToOpenai() {
    store.mainActiveTab = 'openai';
    if (store.openaiEndpoints.length === 0) {
      // 优先加载缓存
      this.loadFromOpenaiCache();
      // 后台刷新最新数据
      this.loadOpenaiEndpoints(true);
    }
    // 加载人设列表
    if (store.openaiPersonas.length === 0) {
      this.loadPersonas();
    }
  },

  showOpenaiToast(message, type = 'success') {
    toast[type](message);
  },

  async loadOpenaiEndpoints(silent = false) {
    if (!silent) store.openaiLoading = true;
    try {
      // 1. 加载端点列表（用于账号管理展示）
      const epResponse = await fetch('/api/openai/endpoints', {
        headers: store.getAuthHeaders(),
      });
      const epData = await epResponse.json();
      if (Array.isArray(epData)) {
        // 保持当前的展开状态
        const expandedIds = { ...this.openaiExpandedEndpoints };

        store.openaiEndpoints = epData.map(ep => ({
          ...ep,
          showKey: false,
          refreshing: false,
        }));

        // 保存到本地缓存
        this.saveToOpenaiCache(epData);
      }

      // 2. 从聚合接口加载全渠道模型列表 (HChat 使用)
      const modelsResponse = await fetch('/api/openai/v1/models', {
        headers: store.getAuthHeaders(),
      });
      const modelsData = await modelsResponse.json();

      if (modelsData && Array.isArray(modelsData.data)) {
        // 存储包含渠道信息的完整对象
        store.openaiAllModels = modelsData.data.sort((a, b) => {
          // 先按渠道排序，再按名称排序
          if (a.owned_by !== b.owned_by) return a.owned_by.localeCompare(b.owned_by);
          return a.id.localeCompare(b.id);
        });

        // 智能初始化模型
        if (store.openaiAllModels.length > 0) {
          // 增加验证逻辑：如果当前选定了端点，且模型在端点的模型列表中，则视为有效
          let currentModelIsValid = false;
          if (store.openaiChatModel) {
            const isGlobalModel = store.openaiAllModels.some(m => m.id === store.openaiChatModel);
            if (isGlobalModel) {
              currentModelIsValid = true;
            } else if (store.openaiChatEndpoint) {
              const selectedEndpoint = store.openaiEndpoints.find(ep => ep.id === store.openaiChatEndpoint);
              if (selectedEndpoint && Array.isArray(selectedEndpoint.models)) {
                currentModelIsValid = selectedEndpoint.models.some(m => (typeof m === 'string' ? m : m.id) === store.openaiChatModel);
              }
            }
          }

          if (!store.openaiChatModel || !currentModelIsValid) {
            // 优先使用默认模型
            if (store.openaiDefaultChatModel && (
              store.openaiAllModels.some(m => m.id === store.openaiDefaultChatModel) ||
              (store.openaiChatEndpoint && store.openaiEndpoints.find(ep => ep.id === store.openaiChatEndpoint)?.models?.includes(store.openaiDefaultChatModel))
            )) {
              store.openaiChatModel = store.openaiDefaultChatModel;
            } else {
              store.openaiChatModel = store.openaiAllModels[0].id;
            }
          }
        }
      }

      if (!silent && store.mainActiveTab === 'openai' && store.openaiCurrentTab === 'endpoints') {
        toast.success('端点及模型列表已刷新');
      }
    } catch (error) {
      console.error('加载模型列表失败:', error);
    } finally {
      if (!silent) store.openaiLoading = false;
    }
  },

  // 设置默认聊天模型
  setDefaultChatModel() {
    if (!store.openaiChatModel) return;

    store.openaiDefaultChatModel = store.openaiChatModel;
    localStorage.setItem('openai_default_model', store.openaiChatModel);
    toast.success(`已将 ${store.openaiChatModel} 设为默认模型`);
  },

  // 清除默认模型
  clearDefaultModel() {
    store.openaiDefaultChatModel = '';
    localStorage.removeItem('openai_default_model');
    toast.success('已清除默认模型');
  },

  // 保存对话设置
  saveOpenaiChatSettings() {
    localStorage.setItem('openai_system_prompt', store.openaiChatSystemPrompt);
    localStorage.setItem('openai_chat_settings', JSON.stringify(store.openaiChatSettings));
    store.showHChatSettingsModal = false;
    toast.success('设置已保存');
  },

  // ==================== 自动标题生成设置 ====================

  // 保存自动标题设置
  saveAutoTitleSettings() {
    localStorage.setItem('openai_auto_title_enabled', store.openaiAutoTitleEnabled);
    localStorage.setItem('openai_title_models', JSON.stringify(store.openaiTitleModels));
  },

  // 添加标题生成模型
  addTitleModel() {
    if (!store.openaiTitleModelToAdd) return;
    if (!store.openaiTitleModels.includes(store.openaiTitleModelToAdd)) {
      store.openaiTitleModels.push(store.openaiTitleModelToAdd);
      this.saveAutoTitleSettings();
    }
    store.openaiTitleModelToAdd = '';
  },

  // 移除标题生成模型
  removeTitleModel(modelId) {
    const index = store.openaiTitleModels.indexOf(modelId);
    if (index > -1) {
      store.openaiTitleModels.splice(index, 1);
      this.saveAutoTitleSettings();
    }
  },

  // 获取可选的标题模型（排除已选的）
  // 聚合所有端点的模型，与 filteredChatModels 逻辑保持一致
  filteredTitleModelOptions() {
    const allModelsMap = new Map();

    // 1. 先加入 store.openaiAllModels
    if (store.openaiAllModels && store.openaiAllModels.length) {
      store.openaiAllModels.forEach(m => allModelsMap.set(m.id, m));
    }

    // 2. 遍历所有端点进行补充
    if (store.openaiEndpoints) {
      store.openaiEndpoints.forEach(ep => {
        if (ep.models && Array.isArray(ep.models)) {
          ep.models.forEach(m => {
            const id = typeof m === 'string' ? m : m.id;
            if (!allModelsMap.has(id)) {
              allModelsMap.set(id, {
                id: id,
                owned_by: ep.name || 'custom',
                object: 'model',
                created: Date.now()
              });
            }
          });
        }
      });
    }

    // 3. 转换为数组并排除已选的模型
    return Array.from(allModelsMap.values())
      .filter(m => !store.openaiTitleModels.includes(m.id));
  },

  // 测试标题生成
  async testTitleGeneration() {
    store.openaiTitleGenerating = true;
    store.openaiTitleLastResult = null;

    const testMessages = [
      { role: 'user', content: '帮我解释一下什么是机器学习' },
      { role: 'assistant', content: '机器学习是人工智能的一个分支，它使计算机能够从数据中学习...' }
    ];

    try {
      const result = await this.generateTitleWithFallback(testMessages);
      store.openaiTitleLastResult = result;
    } catch (e) {
      store.openaiTitleLastResult = { success: false, error: e.message };
    } finally {
      store.openaiTitleGenerating = false;
    }
  },

  // 使用容灾模式生成标题
  async generateTitleWithFallback(messages) {
    // 确定要尝试的模型列表
    const modelsToTry = store.openaiTitleModels.length > 0
      ? [...store.openaiTitleModels]
      : [store.openaiChatModel]; // 如果没有配置，使用当前对话模型

    // 构建生成标题的请求
    const conversationText = messages.slice(0, 4).map(msg => {
      const role = msg.role === 'user' ? '用户' : '助手';
      let text = '';
      if (typeof msg.content === 'string') {
        text = msg.content;
      } else if (Array.isArray(msg.content)) {
        const textParts = msg.content.filter(p => p.type === 'text').map(p => p.text);
        text = textParts.join(' ') || '[图片]';
      }
      return `${role}: ${text.slice(0, 200)}`;
    }).join('\n');

    const titlePrompt = `请根据以下对话内容，生成一个简洁的中文标题（最多15个字，不要使用标点符号，直接输出标题内容）：

${conversationText}

标题：`;

    let lastError = null;

    for (const modelId of modelsToTry) {
      try {
        console.log(`[生成标题] 尝试模型: ${modelId}`);

        const headers = {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        };

        // 尝试找到该模型所属的端点
        const endpoint = store.openaiEndpoints.find(ep =>
          ep.models && ep.models.some(m => (typeof m === 'string' ? m : m.id) === modelId)
        );
        if (endpoint) {
          headers['x-endpoint-id'] = endpoint.id;
        }

        const response = await fetch('/api/openai/v1/chat/completions', {
          method: 'POST',
          headers: headers,
          body: JSON.stringify({
            model: modelId,
            messages: [{ role: 'user', content: titlePrompt }],
            max_tokens: 30,
            temperature: 0.7,
          }),
        });

        if (!response.ok) {
          throw new Error(`HTTP ${response.status}`);
        }

        const result = await response.json();

        // 优先从 content 获取，如果为空则尝试从 reasoning_content 获取
        // （推理模型如 Gemini/DeepSeek-R1 可能把回复放在 reasoning_content 中）
        let generatedTitle = result.choices?.[0]?.message?.content?.trim() || '';

        // 如果 content 为空，尝试从 reasoning_content 提取最后一句或结论
        if (!generatedTitle && result.choices?.[0]?.message?.reasoning_content) {
          const reasoning = result.choices[0].message.reasoning_content.trim();
          // 尝试提取最后一行（通常是结论/答案）
          const lines = reasoning.split('\n').filter(l => l.trim());
          if (lines.length > 0) {
            generatedTitle = lines[lines.length - 1].trim();
          }
        }

        // 清理标题
        generatedTitle = generatedTitle
          .replace(/^["'「『【《]|["'」』】》]$/g, '')
          .replace(/^标题[：:]\s*/i, '')
          .replace(/\n/g, ' ')
          .trim();

        if (generatedTitle.length > 20) {
          generatedTitle = generatedTitle.slice(0, 18) + '...';
        }

        if (!generatedTitle) {
          throw new Error('生成的标题为空');
        }

        console.log(`[生成标题] 成功: ${generatedTitle} (模型: ${modelId})`);
        return { success: true, title: generatedTitle, model: modelId };

      } catch (e) {
        console.warn(`[生成标题] 模型 ${modelId} 失败:`, e.message);
        lastError = e;
        // 继续尝试下一个模型
      }
    }

    // 所有模型都失败了
    throw lastError || new Error('所有模型都无法生成标题');
  },

  // 切换对话端点
  onChatEndpointChange() {
    localStorage.setItem('openai_chat_endpoint', store.openaiChatEndpoint);

    // 检查当前模型是否在新的列表中
    // 注意：this.filteredChatModels 是 main.js 中的 computed 属性
    // 如果无法直接访问，可以使用 store.openaiAllModels 配合 store.openaiChatEndpoint 手动过滤

    let availableModels = [];
    if (store.openaiChatEndpoint) {
      const selectedEndpoint = store.openaiEndpoints.find(ep => ep.id === store.openaiChatEndpoint);
      if (selectedEndpoint && selectedEndpoint.models) {
        // 如果选定了端点，以该端点的模型列表为准
        availableModels = selectedEndpoint.models.map(m => {
          const id = typeof m === 'string' ? m : m.id;
          return { id, owned_by: selectedEndpoint.name || 'custom' };
        });
      }
    } else {
      // 自动模式，使用所有可用模型
      availableModels = store.openaiAllModels || [];
    }

    // 依然排除隐藏模型
    availableModels = availableModels.filter(m =>
      !store.openaiHiddenModels.includes(m.id) || store.openaiPinnedModels.includes(m.id)
    );

    const currentModelValid = availableModels.some(m => m.id === store.openaiChatModel);

    if (!currentModelValid) {
      if (availableModels.length > 0) {
        // 尝试保留默认模型
        if (store.openaiDefaultChatModel && availableModels.some(m => m.id === store.openaiDefaultChatModel)) {
          store.openaiChatModel = store.openaiDefaultChatModel;
        } else {
          store.openaiChatModel = availableModels[0].id;
        }
      } else {
        store.openaiChatModel = '';
      }
    }

    // 同步到当前会话
    this.syncCurrentSessionSettings();
  },

  // 同步当前会话的设置（端点、模型、人设）到数据库
  async syncCurrentSessionSettings() {
    if (!store.openaiChatCurrentSessionId) return;

    try {
      const session = store.openaiChatSessions.find(s => s.id === store.openaiChatCurrentSessionId);
      const currentPersona = store.openaiPersonas.find(p => p.id === store.openaiCurrentPersonaId);

      await fetch(`/api/chat/sessions/${store.openaiChatCurrentSessionId}`, {
        method: 'PUT',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({
          title: session?.title || '新对话',
          model: store.openaiChatModel,
          endpoint_id: store.openaiChatEndpoint || '',
          persona_id: store.openaiCurrentPersonaId || null,
          system_prompt: currentPersona?.system_prompt || store.openaiChatSystemPrompt
        })
      });

      // 更新本地会话数据
      if (session) {
        session.model = store.openaiChatModel;
        session.endpoint_id = store.openaiChatEndpoint || '';
        session.persona_id = store.openaiCurrentPersonaId || null;
        session.system_prompt = currentPersona?.system_prompt || store.openaiChatSystemPrompt;
      }
    } catch (e) {
      console.error('同步会话设置失败:', e);
    }
  },

  // 收藏/取消收藏模型
  togglePinModel(modelId) {
    const index = store.openaiPinnedModels.indexOf(modelId);
    if (index > -1) {
      store.openaiPinnedModels.splice(index, 1);
    } else {
      store.openaiPinnedModels.push(modelId);
    }
    localStorage.setItem('openai_pinned_models', JSON.stringify(store.openaiPinnedModels));
  },

  unpinModel(modelId) {
    const index = store.openaiPinnedModels.indexOf(modelId);
    if (index > -1) {
      store.openaiPinnedModels.splice(index, 1);
      localStorage.setItem('openai_pinned_models', JSON.stringify(store.openaiPinnedModels));
    }
  },

  // 隐藏/显示模型
  toggleHideModel(modelId) {
    const index = store.openaiHiddenModels.indexOf(modelId);
    if (index > -1) {
      store.openaiHiddenModels.splice(index, 1);
    } else {
      store.openaiHiddenModels.push(modelId);
      // 如果隐藏的模型在收藏列表中，也从收藏列表移除
      const pinnedIndex = store.openaiPinnedModels.indexOf(modelId);
      if (pinnedIndex > -1) {
        store.openaiPinnedModels.splice(pinnedIndex, 1);
        localStorage.setItem('openai_pinned_models', JSON.stringify(store.openaiPinnedModels));
      }
    }
    localStorage.setItem('openai_hidden_models', JSON.stringify(store.openaiHiddenModels));
  },

  hideModel(modelId) {
    if (!store.openaiHiddenModels.includes(modelId)) {
      store.openaiHiddenModels.push(modelId);
      localStorage.setItem('openai_hidden_models', JSON.stringify(store.openaiHiddenModels));
    }
    // 同时从收藏列表移除
    this.unpinModel(modelId);
  },

  // 端点健康检测
  async testEndpointHealth(endpoint) {
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'x-admin-password': store.password,
        },
      });

      const result = await response.json();

      // 更新端点状态 - API 返回 status 字段
      const ep = store.openaiEndpoints.find(e => e.id === endpoint.id);
      if (ep) {
        ep.status = result.status || (result.valid ? 'valid' : 'invalid');
      }

      // API 返回 valid 字段表示是否成功
      if (result.valid || result.status === 'valid') {
        toast.success(`${endpoint.name} 验证成功`);
      } else {
        toast.error(`${endpoint.name} 验证失败: ${result.error || result.message || '未知错误'}`);
      }
    } catch (error) {
      toast.error(`检测失败: ${error.message}`);
    }
  },

  // 模型健康检测
  async testModelHealth(model) {
    // 找到该模型所属的端点
    let modelId = typeof model === 'string' ? model : model.id;
    const endpoint = store.openaiEndpoints.find(ep =>
      ep.models && ep.models.includes(modelId)
    );

    if (!endpoint) {
      toast.error('找不到该模型所属的端点');
      return;
    }

    // 设置加载状态
    if (!store.openaiModelHealth[modelId]) {
      store.openaiModelHealth[modelId] = {};
    }
    store.openaiModelHealth[modelId].loading = true;

    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/health-check`, {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({ model: modelId, timeout: 15000 }),
      });

      const result = await response.json();
      this.updateModelHealthState(result);

      if (result.status === 'operational' || result.status === 'degraded') {
        toast.success(`${modelId} 可用 (${result.latency}ms)`);
      } else {
        toast.error(`${modelId} 不可用: ${result.error || '检测失败'}`);
      }
    } catch (error) {
      store.openaiModelHealth[modelId] = {
        status: 'unhealthy',
        loading: false,
        error: error.message,
        checkedAt: new Date().toISOString()
      };
      toast.error(`检测失败: ${error.message}`);
    }
  },

  // 开始配置好的批量健康检测
  async startOpenaiHealthCheck() {
    if (store.openaiModelHealthBatchLoading) return;

    const { useKey, concurrency, timeout } = store.openaiHealthCheckForm;
    store.openaiModelHealthBatchLoading = true;
    store.openaiHealthCheckModal = false; // 立即关闭模态框

    // 1. 预设所有待检测模型的 Loading 状态，让 UI 立即反馈
    const targetEndpoints = useKey === 'all'
      ? store.openaiEndpoints
      : store.openaiEndpoints.filter(ep => ep.id === store.openaiSelectedEndpointId);

    targetEndpoints.forEach(ep => {
      if (ep.models) {
        ep.models.forEach(model => {
          const modelId = typeof model === 'string' ? model : model.id;
          if (!store.openaiModelHealth[modelId]) {
            store.openaiModelHealth[modelId] = { status: 'unknown' };
          }
          store.openaiModelHealth[modelId].loading = true;
        });
      }
    });

    try {
      let url = '/api/openai/health-check-all';
      let payload = { timeout: timeout * 1000, concurrency };

      // 如果选择“单个”，且有选中的端点
      if (useKey === 'single') {
        if (!store.openaiSelectedEndpointId) {
          toast.error('请先选择一个端点或通过列表操作按钮进入');
          store.openaiModelHealthBatchLoading = false;
          return;
        }
        url = `/api/openai/endpoints/${store.openaiSelectedEndpointId}/health-check-all`;
      }

      const response = await fetch(url, {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(payload),
      });

      const result = await response.json();

      if (result.success) {
        // 如果是全局检测返回的结果结构
        if (useKey === 'all' && result.endpoints) {
          result.endpoints.forEach(epResult => {
            if (epResult.results) {
              epResult.results.forEach(mRes => this.updateModelHealthState(mRes));
            }
          });
        }
        // 如果是单端点检测返回的结果结构
        else if (result.results) {
          result.results.forEach(mRes => this.updateModelHealthState(mRes));
        }

        toast.success('健康检测完成');
        store.openaiHealthCheckModal = false;
      } else {
        toast.error('检测失败: ' + (result.error || '未知错误'));
      }
    } catch (error) {
      console.error('健康检测失败:', error);
      toast.error('请求失败: ' + error.message);
    } finally {
      store.openaiModelHealthBatchLoading = false;
    }
  },

  // 辅助方法：更新模型健康状态到 store
  updateModelHealthState(mRes) {
    if (!mRes || !mRes.model) return;

    // 映射后端状态到前端类名
    let status = 'unknown';
    if (mRes.status === 'operational') status = 'healthy';
    else if (mRes.status === 'degraded') status = 'degraded';
    else if (mRes.status === 'failed') status = 'unhealthy';

    store.openaiModelHealth[mRes.model] = {
      status: status,
      loading: false,
      latency: mRes.latency || 0,
      error: mRes.error || null,
      checkedAt: mRes.checkedAt || new Date().toISOString()
    };
  },

  // 打开特定端点的健康检测对话框
  openHealthCheckForEndpoint(endpointId) {
    store.openaiSelectedEndpointId = endpointId;
    store.openaiHealthCheckForm.useKey = 'single';
    store.openaiHealthCheckModal = true;
  },

  // 批量检测所有模型
  async testAllModelsHealth() {
    if (store.openaiModelHealthBatchLoading) return;
    store.openaiModelHealthBatchLoading = true;

    // 预设 loading 状态，让 UI 立即反馈
    store.openaiEndpoints.forEach(ep => {
      if (ep.models) {
        ep.models.forEach(model => {
          const mId = this.getModelName(model);
          if (!store.openaiModelHealth[mId]) {
            store.openaiModelHealth[mId] = { status: 'unknown' };
          }
          store.openaiModelHealth[mId].loading = true;
        });
      }
    });

    try {
      // 调用后端批量检测 API
      const response = await fetch('/api/openai/health-check-all', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({ timeout: 15000 }),
      });

      const result = await response.json();

      if (result.success && result.endpoints) {
        let totalHealthy = 0;
        let totalUnhealthy = 0;

        // 更新每个模型的健康状态
        for (const epResult of result.endpoints) {
          if (epResult.results) {
            for (const modelResult of epResult.results) {
              this.updateModelHealthState(modelResult);
              if (modelResult.status === 'operational' || modelResult.status === 'degraded') {
                totalHealthy++;
              } else {
                totalUnhealthy++;
              }
            }
          }
        }

        toast.success(`批量检测完成: ${totalHealthy} 可用, ${totalUnhealthy} 不可用`);
      } else {
        toast.error('批量检测失败: ' + (result.error || '未知错误'));
      }
    } catch (error) {
      console.error('批量检测失败:', error);
      toast.error(`批量检测失败: ${error.message}`);
    } finally {
      store.openaiModelHealthBatchLoading = false;
    }
  },

  // 切换选定的端点（用于模型管理界面按端点筛选）
  setSelectedEndpoint(endpointId) {
    store.openaiSelectedEndpointId = endpointId;
  },

  // 刷新模型列表：从后端 API 同步最新模型
  async updateOpenaiAllModels(explicitRefresh = false) {
    if (explicitRefresh) {
      // 用户手动刷新：调用后端刷新接口，从远程 API 端点获取最新模型
      store.openaiLoading = true;
      try {
        const response = await fetch('/api/openai/endpoints/refresh', {
          method: 'POST',
          headers: store.getAuthHeaders(),
        });
        const result = await response.json();

        if (result.success) {
          // 刷新成功，重新加载端点和模型列表
          await this.loadOpenaiEndpoints(true);

          // 统计刷新结果
          const successCount = result.results?.filter(r => r.success).length || 0;
          const totalCount = result.results?.length || 0;
          const totalModels = result.results?.reduce((sum, r) => sum + (r.modelsCount || 0), 0) || 0;

          toast.success(`已从 ${successCount}/${totalCount} 个端点刷新 ${totalModels} 个模型`);
        } else {
          toast.error('刷新失败: ' + (result.error || '未知错误'));
        }
      } catch (error) {
        console.error('刷新模型列表失败:', error);
        toast.error('刷新失败: ' + error.message);
      } finally {
        store.openaiLoading = false;
      }
    } else {
      // 静默刷新：仅从数据库缓存读取
      this.loadOpenaiEndpoints(true);
    }
  },

  // 图片压缩工具函数
  async compressImage(file, maxSize = 1920, quality = 0.8) {
    // 优化：如果图片小于 1MB，直接跳过压缩，避免主线程卡顿
    if (file.size < 1024 * 1024) {
      console.log(`[图片压缩] ${file.name}: 文件较小 (${(file.size / 1024).toFixed(0)}KB)，跳过压缩`);
      return new Promise(resolve => {
        const reader = new FileReader();
        reader.onload = e => resolve(e.target.result);
        reader.readAsDataURL(file);
      });
    }

    return new Promise((resolve) => {
      const reader = new FileReader();
      reader.onload = (e) => {
        const img = new Image();
        img.onload = () => {
          // 计算压缩后的尺寸
          let { width, height } = img;
          if (width > maxSize || height > maxSize) {
            if (width > height) {
              height = Math.round((height * maxSize) / width);
              width = maxSize;
            } else {
              width = Math.round((width * maxSize) / height);
              height = maxSize;
            }
          }

          // 使用 Canvas 进行压缩
          const canvas = document.createElement('canvas');
          canvas.width = width;
          canvas.height = height;
          const ctx = canvas.getContext('2d');
          ctx.drawImage(img, 0, 0, width, height);

          // 输出为 JPEG（压缩率更好）或保持原格式
          const outputType = file.type === 'image/png' ? 'image/png' : 'image/jpeg';
          const compressedDataUrl = canvas.toDataURL(outputType, quality);

          // 计算压缩比例（用于日志）
          const originalSize = e.target.result.length;
          const compressedSize = compressedDataUrl.length;
          const ratio = ((1 - compressedSize / originalSize) * 100).toFixed(1);

          if (compressedSize < originalSize) {
            console.log(`[图片压缩] ${file.name}: ${(originalSize / 1024).toFixed(0)}KB -> ${(compressedSize / 1024).toFixed(0)}KB (减少 ${ratio}%)`);
            resolve(compressedDataUrl);
          } else {
            // 如果压缩后反而更大，使用原图
            console.log(`[图片压缩] ${file.name}: 保持原图 (${(originalSize / 1024).toFixed(0)}KB)`);
            resolve(e.target.result);
          }
        };
        img.src = e.target.result;
      };
      reader.readAsDataURL(file);
    });
  },

  // 将图片上传到服务器并返回持久化 URL
  async uploadImageToServer(dataUrl, originalFile = null) {
    try {
      // 检查缓存
      if (imageUploadCache.has(dataUrl)) {
        console.log('[Image Upload] Hit cache:', imageUploadCache.get(dataUrl));
        return imageUploadCache.get(dataUrl);
      }

      // 检查是否已经是服务器 URL (防止重复上传)
      if (dataUrl.startsWith('/uploads/')) return dataUrl;

      // 注意：如果 dataUrl 不是 base64 也不是 /uploads，可能是一个外部 URL，直接返回
      if (!dataUrl.startsWith('data:')) {
        console.log('[Image Upload] External URL detected, skipping upload:', dataUrl.substring(0, 50) + '...');
        return dataUrl;
      }

      console.log('[Image Upload] Starting upload for:', originalFile ? originalFile.name : 'pasted_image');

      // 转换为 Blob
      const res = await fetch(dataUrl);
      const blob = await res.blob();

      const formData = new FormData();
      const fileName = originalFile ? originalFile.name : 'pasted_image.jpg';
      formData.append('image', blob, fileName);

      const headers = store.getAuthHeaders();
      delete headers['Content-Type']; // FormData 此时会自动设置 multipart/form-data 和 boundary

      const uploadResponse = await fetch('/api/chat/upload-image', {
        method: 'POST',
        headers: headers,
        body: formData
      });

      if (!uploadResponse.ok) {
        const errText = await uploadResponse.text();
        console.error('[Image Upload] Server returned error:', uploadResponse.status, errText);
        toast.error(`图片上传服务器失败 (${uploadResponse.status})，将使用 Base64 存储 (可能导致卡顿)`);
        return dataUrl;
      }

      const result = await uploadResponse.json();
      if (result.success) {
        console.log('[Image Upload] Success:', result.url);
        imageUploadCache.set(dataUrl, result.url);
        return result.url;
      } else {
        console.error('[Image Upload] API returned success=false:', result.error);
        toast.error(`图片上传失败: ${result.error}，将使用 Base64 存储`);
        return dataUrl; // 降级使用 base64
      }
    } catch (e) {
      console.error('[Image Upload] Exception:', e);
      toast.error(`图片上传异常: ${e.message}，将使用 Base64 存储`);
      return dataUrl;
    }
  },

  // 处理文件选择 (多模态)
  async handleOpenaiChatFileSelect(event) {
    const files = Array.from(event.target.files);
    if (files.length === 0) return;

    for (const file of files) {
      if (!file.type.startsWith('image/')) {
        toast.warning(`文件 ${file.name} 不是图片，已跳过`);
        continue;
      }

      // 1. 压缩图片
      const compressedUrl = await this.compressImage(file);
      // 2. 上传到服务器
      const persistentUrl = await this.uploadImageToServer(compressedUrl, file);

      store.openaiChatAttachments.push({
        name: file.name,
        url: persistentUrl,
        type: file.type
      });
    }
    // 清空 input 方便下次选择同名文件
    event.target.value = '';
  },

  // 处理剪贴板粘贴 (支持粘贴图片)
  async handleOpenaiChatPaste(event) {
    const items = event.clipboardData?.items;
    if (!items) return;

    for (const item of items) {
      if (item.type.indexOf('image') !== -1) {
        const file = item.getAsFile();
        if (file) {
          // 1. 压缩图片
          const compressedUrl = await this.compressImage(file);
          // 2. 上传到服务器
          const persistentUrl = await this.uploadImageToServer(compressedUrl, file);

          store.openaiChatAttachments.push({
            name: '粘贴的图片',
            url: persistentUrl,
            type: file.type
          });
        }
      }
    }
  },

  // 移除附件
  removeOpenaiChatAttachment(index) {
    store.openaiChatAttachments.splice(index, 1);
  },

  async sendOpenaiChatMessage() {
    if ((!store.openaiChatMessageInput.trim() && store.openaiChatAttachments.length === 0) || store.openaiChatLoading) return;

    const userText = store.openaiChatMessageInput;
    const attachments = [...store.openaiChatAttachments];

    store.openaiChatMessageInput = '';
    store.openaiChatAttachments = [];

    // 重置输入框高度
    const textarea = document.querySelector('.chat-textarea');
    if (textarea) {
      textarea.style.height = 'auto';
    }

    // 如果没有当前会话，自动创建一个
    if (!store.openaiChatCurrentSessionId) {
      await this.createChatSession();
    }

    // 构造 OpenAI 兼容的多模态消息内容
    let userContent;
    if (attachments.length > 0) {
      userContent = [{ type: 'text', text: userText }];
      attachments.forEach(att => {
        userContent.push({
          type: 'image_url',
          image_url: { url: att.url }
        });
      });
    } else {
      userContent = userText;
    }

    // Determine content to save
    const contentToSave = typeof userContent === 'string' ? userContent : JSON.stringify(userContent);

    // 添加用户消息到前端显示
    const userMsg = { role: 'user', content: userContent, timestamp: new Date().toISOString(), isNew: true };
    store.openaiChatMessages.push(userMsg);

    // 保存用户消息到数据库
    this.saveChatMessage('user', contentToSave).then(saved => {
      if (saved && saved.id) {
        userMsg.id = saved.id;
      }
    });

    store.openaiChatAutoScroll = true;
    this.$nextTick(() => {
      this.openaiScrollToBottom(true, true); // 发送消息，强制滚动
    });

    store.openaiChatLoading = true;

    // 创建 AbortController 用于中断请求
    store.openaiChatAbortController = new AbortController();

    // 保存当前会话 ID，用于隔离检查（防止切换会话后内容串扰）
    const requestSessionId = store.openaiChatCurrentSessionId;

    try {
      const messages = [
        { role: 'system', content: store.openaiChatSystemPrompt },
        ...store.openaiChatMessages,
      ];

      const headers = {
        ...store.getAuthHeaders(),
        'Content-Type': 'application/json',
      };

      // 智能端点路由逻辑
      let targetEndpointId = store.openaiChatEndpoint;

      // 如果未指定端点 (聚合模式)，尝试从本地数据中查找该模型所属的端点
      // 帮助后端更准确地路由，避免因后端默认路由失效导致 404/500
      if (!targetEndpointId && store.openaiChatModel) {
        const foundEp = store.openaiEndpoints.find(ep =>
          ep.models && ep.models.some(m => (typeof m === 'string' ? m : m.id) === store.openaiChatModel)
        );
        if (foundEp) {
          targetEndpointId = foundEp.id;
          console.log(`[Chat] Auto-routed model ${store.openaiChatModel} to endpoint: ${foundEp.name} (${foundEp.id})`);
        }
      }

      if (targetEndpointId) {
        headers['x-endpoint-id'] = targetEndpointId;
      }

      const response = await fetch('/api/openai/v1/chat/completions', {
        method: 'POST',
        headers: headers,
        body: JSON.stringify({
          model: store.openaiChatModel,
          messages: messages,
          stream: true,
          ...store.openaiChatSettings,
        }),
        signal: store.openaiChatAbortController?.signal,
      });

      if (!response.ok) {
        let errorMessage = `HTTP 错误 ${response.status}`;
        try {
          const errData = await response.json();
          // 智能提取各种格式的错误消息，并确保转换为字符串以避免 [object Object]
          if (errData.error) {
            if (typeof errData.error === 'string') {
              errorMessage = errData.error;
            } else if (errData.error.message) {
              errorMessage = String(errData.error.message);
            } else {
              errorMessage = JSON.stringify(errData.error);
            }
          } else if (errData.message) {
            errorMessage = String(errData.message);
          } else if (typeof errData === 'string') {
            errorMessage = errData;
          } else {
            const jsonStr = JSON.stringify(errData);
            if (jsonStr && jsonStr !== '{}' && jsonStr !== '[]') {
              errorMessage = jsonStr;
            }
          }
        } catch (e) {
          // 保持默认 HTTP 错误
        }
        throw new Error(errorMessage);
      }

      // 处理流式响应
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      const assistantMsg = {
        role: 'assistant',
        content: '',
        reasoning: '',
        showReasoning: false,
        timestamp: new Date().toISOString(),
        model: store.openaiChatModel,  // 记录使用的模型
        isNew: true,
      };

      // 只有当会话未切换时才向 UI 添加消息
      if (store.openaiChatCurrentSessionId === requestSessionId) {
        store.openaiChatMessages.push(assistantMsg);
        // AI 回复开始时，强制滚动到底部让用户看到回复
        this.$nextTick(() => {
          this.openaiScrollToBottom(true, true);
        });
      }

      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || ''; // 保留最后一行（可能不完整），如果 buffer 是空行则重置

        for (const line of lines) {
          const trimmedLine = line.trim();
          if (trimmedLine.startsWith('data: ')) {
            const dataStr = trimmedLine.slice(6);
            if (dataStr === '[DONE]') break;

            try {
              const data = JSON.parse(dataStr);
              const delta = data.choices?.[0]?.delta;

              if (delta) {
                // 处理思考内容 (Reasoning / Thinking)
                if (delta.reasoning_content) {
                  assistantMsg.reasoning += delta.reasoning_content;
                }
                // 处理标准内容
                if (delta.content) {
                  assistantMsg.content += delta.content;
                }
                // 只有当会话未切换时才更新 UI
                if (store.openaiChatCurrentSessionId === requestSessionId) {
                  this.openaiScrollToBottom(false, false); // 流式输出，智能跟随滚动（非强制）
                }
              }
            } catch (e) {
              // 忽略解析错误
            }
          }
        }
      }

      // 只有当会话未切换时才保存消息和更新状态
      if (store.openaiChatCurrentSessionId === requestSessionId) {
        // 保存助手消息到数据库
        const savedMsg = await this.saveChatMessage('assistant', assistantMsg.content, assistantMsg.reasoning || null);
        if (savedMsg && savedMsg.id) {
          assistantMsg.id = savedMsg.id;
        }

        // 自动生成标题（如果是新对话的第一次回复）
        const currentSession = store.openaiChatSessions.find(s => s.id === requestSessionId);
        if (currentSession && currentSession.title === '新对话' && store.openaiChatMessages.length >= 2) {
          // 后台生成标题，不阻塞用户操作
          this.generateChatTitle().catch(e => console.error('自动生成标题失败:', e));
        }
      } else {
        // 会话已切换，但仍需保存消息到原会话
        // 使用临时保存，不影响当前 UI
        try {
          await fetch(`/api/chat/sessions/${requestSessionId}/messages`, {
            method: 'POST',
            headers: store.getAuthHeaders(),
            body: JSON.stringify({
              role: 'assistant',
              content: assistantMsg.content,
              reasoning: assistantMsg.reasoning || null
            }),
          });
          console.log('[Chat] 会话已切换，消息已保存到原会话:', requestSessionId);
        } catch (e) {
          console.error('[Chat] 保存到原会话失败:', e);
        }
      }
    } catch (error) {
      // 如果是用户主动中断，不显示错误
      if (error.name === 'AbortError') {
        console.log('[对话已中断]');
        return;
      }

      console.error('AI 对话失败:', error);

      // 改进错误提取逻辑，确保不显示 [object Object]
      let displayError = '未知错误';

      if (typeof error === 'string') {
        displayError = error;
      } else if (error && typeof error === 'object') {
        // 优先尝试常见的错误字段
        if (error.message && typeof error.message === 'string') {
          displayError = error.message;
        } else if (error.error) {
          // OpenAI 格式的错误: { error: { message: '...' } }
          if (typeof error.error === 'string') {
            displayError = error.error;
          } else if (error.error.message) {
            displayError = String(error.error.message);
          } else {
            try {
              displayError = JSON.stringify(error.error);
            } catch {
              displayError = String(error.error);
            }
          }
        } else {
          // 最后尝试 JSON.stringify
          try {
            const str = JSON.stringify(error);
            displayError = (str && str !== '{}') ? str : String(error);
            if (displayError === '{}') displayError = '请求失败 (空错误对象)';
          } catch {
            displayError = String(error) || '请求失败';
          }
        }
      }

      // 最终防线
      if (typeof displayError === 'object' || displayError === '[object Object]') {
        displayError = '请求失败 (无法解析错误详情)';
      }

      this.showOpenaiToast('对话失败: ' + displayError, 'error');
      // 只有会话未切换时才添加错误消息到 UI
      if (store.openaiChatCurrentSessionId === requestSessionId) {
        store.openaiChatMessages.push({
          role: 'assistant',
          content: '❌ **错误**: ' + displayError,
        });
      }
    } finally {
      // 只有会话未切换时才更新 loading 状态
      if (store.openaiChatCurrentSessionId === requestSessionId) {
        store.openaiChatLoading = false;
        // 使用 $nextTick 确保 DOM 更新后再强制滚动到底部
        this.$nextTick(() => {
          this.openaiScrollToBottom(true, true);
        });
      }
      store.openaiChatAbortController = null;
    }
  },

  async clearOpenaiChat() {
    // 如果有当前会话，同步清空后端数据库
    if (store.openaiChatCurrentSessionId) {
      try {
        await fetch(`/api/chat/sessions/${store.openaiChatCurrentSessionId}/messages`, {
          method: 'DELETE',
          headers: store.getAuthHeaders(),
        });
      } catch (error) {
        console.error('清空消息失败:', error);
      }
    }
    store.openaiChatMessages = [];
  },

  /**
   * 删除单条消息
   * @param {number} index - 消息索引
   */
  async deleteOpenaiChatMessage(index) {
    if (index >= 0 && index < store.openaiChatMessages.length) {
      const msg = store.openaiChatMessages[index];
      console.log(`[Chat] Deleting message at index ${index}, role: ${msg.role}, id: ${msg.id}`);

      // 如果消息有 id 且有当前会话，同步删除后端
      if (msg.id && store.openaiChatCurrentSessionId) {
        try {
          const res = await fetch(`/api/chat/sessions/${store.openaiChatCurrentSessionId}/messages/${msg.id}`, {
            method: 'DELETE',
            headers: store.getAuthHeaders(),
          });
          if (!res.ok) console.warn('后端删除消息失败:', res.status);
        } catch (error) {
          console.error('删除消息失败:', error);
        }
      }

      // 无论后端是否成功，前端都移除它
      store.openaiChatMessages.splice(index, 1);
    } else {
      console.warn(`[Chat] Attempted to delete invalid index: ${index}`);
    }
  },

  stopOpenaiChat() {
    // 中断进行中的请求
    if (store.openaiChatAbortController) {
      store.openaiChatAbortController.abort();
      store.openaiChatAbortController = null;
    }
    store.openaiChatLoading = false;
  },

  /**
   * 重新生成 AI 回复
   * @param {number} [index] - 可选。要针对其重新生成的索引。如果不传，默认为最后一条 AI 回复。
   */
  async regenerateOpenaiChat(index = -1) {
    if (store.openaiChatLoading) return;
    if (store.openaiChatMessages.length === 0) return;

    let targetIndex = index;

    // 如果没有传入索引，自动寻找最后一条 assistant 消息
    if (targetIndex === -1) {
      for (let i = store.openaiChatMessages.length - 1; i >= 0; i--) {
        if (store.openaiChatMessages[i].role === 'assistant') {
          targetIndex = i;
          break;
        }
      }
    }

    if (targetIndex === -1) {
      // 如果还没找到（比如全是 user 消息），就取最后一条 user 消息
      targetIndex = store.openaiChatMessages.length - 1;
    }

    const targetMsg = store.openaiChatMessages[targetIndex];
    if (!targetMsg) return;

    // 逻辑：删除目标消息之后的所有消息
    // 如果目标是 assistant，则目标本身也要删
    // 如果目标是 user，则保留目标，删掉后面的
    const deleteCount = store.openaiChatMessages.length - (targetMsg.role === 'assistant' ? targetIndex : targetIndex + 1);

    if (deleteCount > 0) {
      console.log(`[Chat] Regenerating: deleting ${deleteCount} messages after index ${targetIndex}`);
      // 从后往前删，确保 ID 同步和后端删除
      for (let i = 0; i < deleteCount; i++) {
        await this.deleteOpenaiChatMessage(store.openaiChatMessages.length - 1);
      }
    }

    // 重新发送请求
    store.openaiChatLoading = true;
    store.openaiChatAbortController = new AbortController();

    try {
      // 构造请求上下文
      // 注意：此时最后的 user 消息应该就是 store.openaiChatMessages 的最后一条（或者上一条）
      const messages = [
        { role: 'system', content: store.openaiChatSystemPrompt },
        ...store.openaiChatMessages,
      ];

      const headers = {
        ...store.getAuthHeaders(),
        'Content-Type': 'application/json',
      };

      if (store.openaiChatEndpoint) {
        headers['x-endpoint-id'] = store.openaiChatEndpoint;
      }

      const response = await fetch('/api/openai/v1/chat/completions', {
        method: 'POST',
        headers: headers,
        body: JSON.stringify({
          model: store.openaiChatModel,
          messages: messages,
          stream: true,
          ...store.openaiChatSettings,
        }),
        signal: store.openaiChatAbortController?.signal,
      });

      if (!response.ok) {
        const errData = await response.json().catch(() => ({}));
        throw errData.error || errData || `HTTP 错误 ${response.status}`;
      }

      const assistantMsg = {
        role: 'assistant',
        content: '',
        reasoning: '',
        showReasoning: false,
        timestamp: new Date().toISOString(),
        model: store.openaiChatModel,
        isNew: true,
      };
      store.openaiChatMessages.push(assistantMsg);
      // AI 回复开始时，强制滚动到底部
      this.$nextTick(() => {
        this.openaiScrollToBottom(true, true);
      });

      const reader = response.body.getReader();
      const decoder = new TextDecoder();

      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || ''; // 保留最后一行（可能不完整）

        for (const line of lines) {
          const trimmedLine = line.trim();
          if (trimmedLine.startsWith('data: ')) {
            const dataStr = trimmedLine.slice(6);
            if (dataStr === '[DONE]') break;

            try {
              const data = JSON.parse(dataStr);
              const delta = data.choices?.[0]?.delta;

              if (delta) {
                if (delta.reasoning_content) {
                  assistantMsg.reasoning += delta.reasoning_content;
                }
                if (delta.content) {
                  assistantMsg.content += delta.content;
                }
                this.openaiScrollToBottom(false); // 流式输出时禁用平滑滚动
              }
            } catch (e) {
              // 忽略解析错误
            }
          }
        }
      }

      this.saveChatMessage('assistant', assistantMsg.content, assistantMsg.reasoning || null);
    } catch (error) {
      if (error.name === 'AbortError') {
        console.log('[重新生成已中断]');
        return;
      }
      console.error('重新生成失败:', error);
      this.showOpenaiToast('重新生成失败', 'error');
    } finally {
      store.openaiChatLoading = false;
      store.openaiChatAbortController = null;
      this.$nextTick(() => {
        this.openaiScrollToBottom(true, true); // 回复完成，强制滚动
      });
    }
  },

  /**
   * 滚动到底部（参考 NextChat 设计）
   * @param {boolean} smooth - 是否使用平滑滚动（已弃用，保留兼容性）
   * @param {boolean} force - 是否强制滚动（忽略用户的滚动位置）
   */
  openaiScrollToBottom(smooth = true, force = false) {
    console.log('[Chat] scrollToBottom called, force:', force);

    const el = document.getElementById('openai-chat-messages');
    if (!el) {
      console.log('[Chat] Element not found');
      return;
    }

    // 非强制模式：检查是否应该滚动
    if (!force && !store.openaiChatAutoScroll) {
      console.log('[Chat] Skipping scroll - user scrolled up');
      return;
    }

    // 强制模式时，重新启用自动滚动
    if (force) {
      store.openaiChatAutoScroll = true;
    }

    // 直接滚动到底部
    el.style.scrollBehavior = 'auto';
    el.scrollTop = el.scrollHeight;

    console.log('[Chat] Scrolled to bottom, scrollHeight:', el.scrollHeight);

    // 触发代码高亮
    this.highlightCodeBlocks();
  },

  /**
   * 处理聊天区域滚动事件（用于智能脱离）
   * @param {Event} event - 滚动事件
   */
  handleChatScroll(event) {
    const el = event?.target || document.getElementById('openai-chat-messages');
    if (!el) return;

    // 计算是否在底部附近（阈值 100px）
    const isAtBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 100;

    // 更新自动滚动状态
    store.openaiChatAutoScroll = isAtBottom;
  },

  /**
   * 检查消息数量变化并自动滚动
   * 类似 NextChat 的 useEffect 监听 messages.length
   */
  checkAndScrollOnNewMessage() {
    const currentCount = store.openaiChatMessages.length;
    const lastCount = store.openaiChatLastMessageCount;

    // 有新消息时滚动
    if (currentCount > lastCount && store.openaiChatAutoScroll) {
      this.openaiScrollToBottom(true, false);
    }

    // 更新记录
    store.openaiChatLastMessageCount = currentCount;
  },

  /**
   * 触发代码高亮
   */
  highlightCodeBlocks() {
    document.querySelectorAll('pre code').forEach(block => {
      if (!block.dataset.highlighted) {
        hljs.highlightElement(block);
        block.dataset.highlighted = 'true';
      }
    });
  },

  // 格式化消息时间为 时:分 格式
  formatMessageTime(timestamp) {
    const date = timestamp ? new Date(timestamp) : new Date();
    const hours = date.getHours().toString().padStart(2, '0');
    const minutes = date.getMinutes().toString().padStart(2, '0');
    return `${hours}:${minutes}`;
  },

  // ==================== Persona Methods (人设系统) ====================

  getPresetIcons() {
    return [
      'fa-robot', 'fa-cat', 'fa-user-ninja', 'fa-code', 'fa-brain',
      'fa-language', 'fa-graduation-cap', 'fa-ghost', 'fa-poo', 'fa-magic',
      'fa-terminal', 'fa-microchip', 'fa-pills', 'fa-stethoscope',
      'fa-gavel', 'fa-user-tie', 'fa-palette', 'fa-flask', 'fa-book'
    ];
  },

  getCurrentPersonaName() {
    const persona = store.openaiPersonas.find(p => p.id === store.openaiCurrentPersonaId);
    return persona ? persona.name : '选择人设';
  },

  getCurrentPersonaIcon() {
    const persona = store.openaiPersonas.find(p => p.id === store.openaiCurrentPersonaId);
    return persona ? persona.icon : 'fa-user-circle';
  },

  async loadPersonas() {
    try {
      const response = await fetch('/api/personas', { headers: store.getAuthHeaders() });
      const data = await response.json();
      if (data.success) {
        store.openaiPersonas = data.data;
        // 如果没有当前人设，设为第一个（通常是默认助手）
        if (!store.openaiCurrentPersonaId && store.openaiPersonas.length > 0) {
          const def = store.openaiPersonas.find(p => p.is_default) || store.openaiPersonas[0];
          store.openaiCurrentPersonaId = def.id;
          // 同步更新 system prompt，确保发送消息时使用正确的人设
          store.openaiChatSystemPrompt = def.system_prompt;
        } else if (store.openaiCurrentPersonaId && store.openaiPersonas.length > 0) {
          // 如果已有选中的人设 ID，确保 system prompt 同步
          const selectedPersona = store.openaiPersonas.find(p => p.id === store.openaiCurrentPersonaId);
          if (selectedPersona && selectedPersona.system_prompt) {
            store.openaiChatSystemPrompt = selectedPersona.system_prompt;
          }
        }
      }
    } catch (e) {
      console.error('加载人设失败:', e);
    }
  },

  // 切换人设下拉框，同时关闭其他下拉框
  togglePersonaDropdown(event) {
    if (event) event.stopPropagation();
    store.showPersonaDropdown = !store.showPersonaDropdown;
    store.openaiShowEndpointDropdown = false;
    store.openaiShowModelDropdown = false;
  },

  async selectPersona(personaId) {
    const persona = store.openaiPersonas.find(p => p.id === personaId);
    if (!persona) return;

    store.openaiCurrentPersonaId = personaId;
    store.openaiChatSystemPrompt = persona.system_prompt;
    store.showPersonaDropdown = false;

    // 如果当前有会话，同步更新会话的人设关联
    if (store.openaiChatCurrentSessionId) {
      try {
        const session = store.openaiChatSessions.find(s => s.id === store.openaiChatCurrentSessionId);
        await fetch(`/api/chat/sessions/${store.openaiChatCurrentSessionId}`, {
          method: 'PUT',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({
            title: session?.title,
            model: store.openaiChatModel,
            endpoint_id: store.openaiChatEndpoint,
            persona_id: personaId,
            system_prompt: persona.system_prompt
          })
        });
        if (session) {
          session.persona_id = personaId;
          session.system_prompt = persona.system_prompt;
        }
      } catch (e) {
        console.error('更新会话人设失败:', e);
      }
    }
  },

  openPersonaModal(persona = null) {
    if (persona) {
      store.editingPersona = persona;
      store.personaForm = {
        name: persona.name,
        systemPrompt: persona.system_prompt,
        icon: persona.icon
      };
    } else {
      store.editingPersona = null;
      store.personaForm = {
        name: '',
        systemPrompt: '',
        icon: 'fa-robot'
      };
    }
    store.showPersonaModal = true;
    store.showPersonaDropdown = false;
  },

  async savePersona() {
    if (!store.personaForm.name || !store.personaForm.systemPrompt) {
      toast.error('请填写完整名称和提示词');
      return;
    }

    try {
      const method = store.editingPersona ? 'PUT' : 'POST';
      const url = store.editingPersona ? `/api/personas/${store.editingPersona.id}` : '/api/personas';

      const response = await fetch(url, {
        method,
        headers: store.getAuthHeaders(),
        body: JSON.stringify({
          name: store.personaForm.name,
          system_prompt: store.personaForm.systemPrompt,
          icon: store.personaForm.icon
        })
      });
      const data = await response.json();

      if (data.success) {
        toast.success(store.editingPersona ? '人设已更新' : '人设已创建');
        store.showPersonaModal = false;
        await this.loadPersonas();
      } else {
        toast.error(data.error || '保存失败');
      }
    } catch (e) {
      console.error('保存人设失败:', e);
      toast.error('保存人设失败');
    }
  },

  async deletePersona(id) {
    if (!confirm('确定要删除其他人设吗？使用该人设的对话将变为无关联人设。')) return;

    try {
      const response = await fetch(`/api/personas/${id}`, {
        method: 'DELETE',
        headers: store.getAuthHeaders()
      });
      const data = await response.json();
      if (data.success) {
        toast.success('人设已删除');
        await this.loadPersonas();
        // 如果删除的是当前选中的，切换回默认
        if (store.openaiCurrentPersonaId == id) {
          const def = store.openaiPersonas.find(p => p.is_default);
          if (def) this.selectPersona(def.id);
        }
      } else {
        toast.error(data.error || '删除失败');
      }
    } catch (e) {
      console.error('删除人设失败:', e);
      toast.error('删除人设失败');
    }
  },

  // ==================== Chat History Methods ====================

  // 加载所有聊天会话
  async loadChatSessions() {
    store.openaiChatHistoryLoading = true;
    try {
      // 确保人设列表已加载（对话功能依赖人设）
      if (store.openaiPersonas.length === 0) {
        await this.loadPersonas();
      }

      // 进入对话页面时，后台刷新模型列表（从远程 API 获取最新模型）
      this.updateOpenaiAllModels(true).catch(e => {
        console.warn('后台刷新模型失败:', e);
      });

      const response = await fetch('/api/chat/sessions', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        store.openaiChatSessions = data.data;
      }
    } catch (error) {
      console.error('加载聊天历史失败:', error);
    } finally {
      store.openaiChatHistoryLoading = false;
    }
  },

  // 创建新会话
  async createChatSession(resetToDefault = false) {
    try {
      // 创建新会话时，强制使用全局默认设置（防止沿用上一个会话的“脏”状态）
      const globalSystemPrompt = localStorage.getItem('openai_system_prompt') || '你是一个有用的 AI 助手。';
      let globalSettings = {};
      try {
        globalSettings = JSON.parse(localStorage.getItem('openai_chat_settings')) || {};
      } catch (e) { }

      // 恢复当前会话状态为全局默认
      store.openaiChatSystemPrompt = globalSystemPrompt;

      // 只有在明确要求重置或当前没有选定模型时，才使用默认模型
      if (store.openaiDefaultChatModel && (resetToDefault || !store.openaiChatModel)) {
        store.openaiChatModel = store.openaiDefaultChatModel;
      }

      // 获取当前人设的 ID 和 Prompt (如果有的话)
      const currentPersona = store.openaiPersonas.find(p => p.id === store.openaiCurrentPersonaId);
      const personaId = currentPersona ? currentPersona.id : null;
      const systemPrompt = currentPersona ? currentPersona.system_prompt : globalSystemPrompt;

      store.openaiChatSystemPrompt = systemPrompt;

      // 恢复高级设置
      if (globalSettings.temperature !== undefined) store.openaiChatSettings.temperature = globalSettings.temperature;
      if (globalSettings.max_tokens !== undefined) store.openaiChatSettings.max_tokens = globalSettings.max_tokens;

      const response = await fetch('/api/chat/sessions', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({
          title: '新对话',
          model: store.openaiChatModel,
          endpoint_id: store.openaiChatEndpoint || '',
          persona_id: personaId,
          system_prompt: systemPrompt,
        }),
      });
      const data = await response.json();
      if (data.success) {
        store.openaiChatSessions.unshift(data.data);
        store.openaiChatCurrentSessionId = data.data.id;
        store.openaiChatMessages = [];
        toast.success('已创建新对话');
      }
    } catch (error) {
      console.error('创建会话失败:', error);
      toast.error('创建会话失败');
    }
  },

  // 自动生成对话标题
  async generateChatTitle() {
    if (!store.openaiChatCurrentSessionId || store.openaiChatMessages.length < 2) return;

    const session = store.openaiChatSessions.find(s => s.id === store.openaiChatCurrentSessionId);
    if (!session || session.title !== '新对话') return;

    // 检查是否启用自动生成
    if (!store.openaiAutoTitleEnabled) {
      // 使用简单的截取方式
      const firstUserMsg = store.openaiChatMessages.find(m => m.role === 'user');
      if (firstUserMsg) {
        let simpleTitle = '';
        if (typeof firstUserMsg.content === 'string') {
          simpleTitle = firstUserMsg.content;
        } else if (Array.isArray(firstUserMsg.content)) {
          const textParts = firstUserMsg.content.filter(p => p.type === 'text').map(p => p.text);
          simpleTitle = textParts.join(' ') || '📷 图片对话';
        }
        simpleTitle = simpleTitle.slice(0, 18) + (simpleTitle.length > 18 ? '...' : '');

        try {
          await fetch(`/api/chat/sessions/${store.openaiChatCurrentSessionId}`, {
            method: 'PUT',
            headers: store.getAuthHeaders(),
            body: JSON.stringify({ title: simpleTitle }),
          });
          session.title = simpleTitle;
        } catch (e) {
          console.error('[生成标题] 保存失败:', e);
        }
      }
      return;
    }

    // 使用 AI 生成标题（支持容灾）
    try {
      const result = await this.generateTitleWithFallback(store.openaiChatMessages);

      if (result.success) {
        // 更新数据库中的会话标题
        await fetch(`/api/chat/sessions/${store.openaiChatCurrentSessionId}`, {
          method: 'PUT',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({
            title: result.title,
            model: store.openaiChatModel,
            endpoint_id: store.openaiChatEndpoint || '',
            system_prompt: store.openaiChatSystemPrompt,
          }),
        });

        // 更新本地会话标题
        session.title = result.title;
        console.log(`[生成标题] 成功: ${result.title} (模型: ${result.model})`);
      }
    } catch (error) {
      console.error('[生成标题] 所有模型都失败:', error);

      // 回退到截取用户消息
      const firstUserMsg = store.openaiChatMessages.find(m => m.role === 'user');
      if (firstUserMsg) {
        let fallbackTitle = '';
        if (typeof firstUserMsg.content === 'string') {
          fallbackTitle = firstUserMsg.content;
        } else if (Array.isArray(firstUserMsg.content)) {
          const textParts = firstUserMsg.content.filter(p => p.type === 'text').map(p => p.text);
          fallbackTitle = textParts.join(' ') || '📷 图片对话';
        }
        fallbackTitle = fallbackTitle.slice(0, 18) + (fallbackTitle.length > 18 ? '...' : '');

        try {
          await fetch(`/api/chat/sessions/${store.openaiChatCurrentSessionId}`, {
            method: 'PUT',
            headers: store.getAuthHeaders(),
            body: JSON.stringify({ title: fallbackTitle }),
          });
          session.title = fallbackTitle;
          console.log('[生成标题] 回退成功:', fallbackTitle);
        } catch (e) {
          console.error('[生成标题] 回退保存失败:', e);
        }
      }
    }
  },

  // 加载指定会话
  async loadChatSession(sessionId) {
    if (store.openaiChatCurrentSessionId === sessionId) return;

    // 切换会话时重置 loading 状态（后台请求会继续完成但不影响新会话 UI）
    store.openaiChatLoading = false;
    store.openaiChatHistoryLoading = true;

    try {
      const response = await fetch(`/api/chat/sessions/${sessionId}/messages`, {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        store.openaiChatCurrentSessionId = sessionId;

        // 恢复会话的模型和端点设置（先获取会话信息）
        const session = store.openaiChatSessions.find(s => s.id === sessionId);
        const sessionModel = session?.model || store.openaiChatModel;

        store.openaiChatMessages = data.data.map(msg => {
          let content = msg.content;
          if (content && typeof content === 'string' && content.startsWith('[')) {
            try {
              content = JSON.parse(content);
            } catch (e) { }
          }
          return {
            id: msg.id,  // 保留消息 ID 用于删除
            role: msg.role,
            content: content,
            reasoning: msg.reasoning,
            showReasoning: false,
            timestamp: msg.created_at || msg.timestamp,  // 添加时间戳
            model: msg.model || sessionModel,  // 添加模型信息
          };
        });

        // 应用会话设置
        if (session && session.model) {
          store.openaiChatModel = session.model;
        }
        if (session && session.endpoint_id) {
          store.openaiChatEndpoint = session.endpoint_id;
        }
        if (session && session.persona_id) {
          store.openaiCurrentPersonaId = session.persona_id;
          // 根据 persona_id 从人设列表获取 system_prompt，确保同步
          const persona = store.openaiPersonas.find(p => p.id === session.persona_id);
          if (persona && persona.system_prompt) {
            store.openaiChatSystemPrompt = persona.system_prompt;
          } else if (session.system_prompt) {
            // 降级使用会话中存储的 system_prompt
            store.openaiChatSystemPrompt = session.system_prompt;
          }
        } else if (session) {
          // 如果会话没存 persona_id，尝试看有没有默认人设
          const def = store.openaiPersonas.find(p => p.is_default) || store.openaiPersonas[0];
          if (def) {
            store.openaiCurrentPersonaId = def.id;
            store.openaiChatSystemPrompt = def.system_prompt;
          } else if (session.system_prompt) {
            store.openaiChatSystemPrompt = session.system_prompt;
          }
        }

        // 添加淡入动画
        const messagesEl = document.getElementById('openai-chat-messages');
        requestAnimationFrame(() => {
          if (messagesEl) {
            messagesEl.classList.add('fade-in');
            setTimeout(() => {
              messagesEl.classList.remove('fade-in');
            }, 300);
          }
        });

        store.openaiChatAutoScroll = true;
        store.openaiChatLastMessageCount = store.openaiChatMessages.length;
        this.$nextTick(() => {
          this.openaiScrollToBottom(true, true);
          // 使用 requestAnimationFrame 确保长对话渲染完成后滚动
          requestAnimationFrame(() => {
            requestAnimationFrame(() => {
              this.openaiScrollToBottom(true, true);
            });
          });
        });
      }
    } catch (error) {
      console.error('加载会话失败:', error);
      toast.error('加载会话失败');
    } finally {
      store.openaiChatHistoryLoading = false;
    }
  },

  // 删除会话
  async deleteChatSession(sessionId) {
    const confirmed = await store.showConfirm({
      title: '删除对话',
      message: '确定要删除这个对话吗？此操作不可撤销。',
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(`/api/chat/sessions/${sessionId}`, {
        method: 'DELETE',
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        store.openaiChatSessions = store.openaiChatSessions.filter(s => s.id !== sessionId);
        if (store.openaiChatCurrentSessionId === sessionId) {
          store.openaiChatCurrentSessionId = null;
          store.openaiChatMessages = [];
        }
        toast.success('对话已删除');
      }
    } catch (error) {
      console.error('删除会话失败:', error);
      toast.error('删除会话失败');
    }
  },

  // 批量删除选中的会话
  async deleteSelectedOpenaiChatSessions() {
    const ids = store.openaiChatSelectedSessionIds;
    if (ids.length === 0) return;

    const confirmed = await store.showConfirm({
      title: '批量删除',
      message: `确定要删除选中的 ${ids.length} 个对话吗？此操作不可撤销。`,
      icon: 'fa-trash-alt',
      confirmText: '确认删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch('/api/chat/sessions', {
        method: 'DELETE',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({ ids }),
      });
      const data = await response.json();
      if (data.success) {
        store.openaiChatSessions = store.openaiChatSessions.filter(s => !ids.includes(s.id));
        if (ids.includes(store.openaiChatCurrentSessionId)) {
          store.openaiChatCurrentSessionId = null;
          store.openaiChatMessages = [];
        }
        store.openaiChatSelectedSessionIds = [];
        toast.success(`已成功删除 ${ids.length} 个对话`);
      }
    } catch (error) {
      console.error('批量删除失败:', error);
      toast.error('批量删除失败');
    }
  },

  // 清空所有会话
  async clearAllOpenaiChatSessions() {
    if (store.openaiChatSessions.length === 0) return;

    const confirmed = await store.showConfirm({
      title: '清空历史记录',
      message: '确定要清空所有聊天历史吗？此操作不可撤销。',
      icon: 'fa-trash-sweep',
      confirmText: '全部删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch('/api/chat/sessions', {
        method: 'DELETE',
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        store.openaiChatSessions = [];
        store.openaiChatCurrentSessionId = null;
        store.openaiChatMessages = [];
        store.openaiChatSelectedSessionIds = [];
        toast.success('所有对话已清空');
      }
    } catch (error) {
      console.error('清空会话失败:', error);
      toast.error('清空会话失败');
    }
  },

  // 切换会话选中状态
  toggleSessionSelection(id) {
    const index = store.openaiChatSelectedSessionIds.indexOf(id);
    if (index === -1) {
      store.openaiChatSelectedSessionIds.push(id);
    } else {
      store.openaiChatSelectedSessionIds.splice(index, 1);
    }
  },

  // 全选/取消全选
  toggleSelectAllSessions() {
    if (store.openaiChatSelectedSessionIds.length === store.openaiChatSessions.length) {
      store.openaiChatSelectedSessionIds = [];
    } else {
      store.openaiChatSelectedSessionIds = store.openaiChatSessions.map(s => s.id);
    }
  },

  // 保存消息到当前会话
  async saveChatMessage(role, content, reasoning = null) {
    if (!store.openaiChatCurrentSessionId) return null;

    try {
      const response = await fetch(`/api/chat/sessions/${store.openaiChatCurrentSessionId}/messages`, {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({ role, content, reasoning }),
      });
      const data = await response.json();
      return data.success ? data.data : null;
    } catch (error) {
      console.error('保存消息失败:', error);
      return null;
    }
  },

  // 切换侧边栏折叠状态
  toggleChatHistory() {
    store.openaiChatHistoryCollapsed = !store.openaiChatHistoryCollapsed;
  },

  openAddOpenaiEndpointModal() {
    this.openaiEditingEndpoint = null;
    this.openaiEndpointForm = { name: '', baseUrl: '', apiKey: '', notes: '' };
    this.openaiEndpointFormError = '';
    this.showOpenaiEndpointModal = true;
  },

  editOpenaiEndpoint(endpoint) {
    this.openaiEditingEndpoint = endpoint;
    this.openaiEndpointForm = {
      name: endpoint.name || '',
      baseUrl: endpoint.baseUrl || '',
      apiKey: endpoint.apiKey || '',
      notes: endpoint.notes || '',
    };
    this.openaiEndpointFormError = '';
    this.showOpenaiEndpointModal = true;
  },

  async saveOpenaiEndpoint() {
    if (!this.openaiEndpointForm.baseUrl || !this.openaiEndpointForm.apiKey) {
      this.openaiEndpointFormError = '请填写 API 地址和 API Key';
      return;
    }

    this.openaiSaving = true;
    this.openaiEndpointFormError = '';

    try {
      const url = this.openaiEditingEndpoint
        ? `/api/openai/endpoints/${this.openaiEditingEndpoint.id}`
        : '/api/openai/endpoints';

      const response = await fetch(url, {
        method: this.openaiEditingEndpoint ? 'PUT' : 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(this.openaiEndpointForm),
      });

      const data = await response.json();
      if (response.ok && (data.success || data.endpoint || data.id)) {
        // 根据验证结果显示不同的提示
        if (this.openaiEditingEndpoint) {
          this.showOpenaiToast('端点已更新', 'success');
        } else if (data.verification && data.verification.valid) {
          const modelsCount = data.endpoint?.models?.length || 0;
          this.showOpenaiToast(`端点已添加，验证成功！找到 ${modelsCount} 个模型`, 'success');
        } else if (data.verification && !data.verification.valid) {
          this.showOpenaiToast('端点已添加，但 API 验证失败', 'error');
        } else {
          this.showOpenaiToast('端点已添加', 'success');
        }
        this.showOpenaiEndpointModal = false;
        await this.loadOpenaiEndpoints(); // 加载端点列表
        this.updateOpenaiAllModels(); // 立即更新 HChat 可用模型列表
      } else {
        this.openaiEndpointFormError = data.error || '保存失败';
      }
    } catch (error) {
      this.openaiEndpointFormError = '保存失败: ' + error.message;
    } finally {
      this.openaiSaving = false;
    }
  },

  async deleteOpenaiEndpoint(endpoint) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除端点 "${endpoint.name || endpoint.baseUrl}" 吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}`, {
        method: 'DELETE',
        headers: store.getAuthHeaders(),
      });

      const data = await response.json();
      if (response.ok && data.success) {
        this.showOpenaiToast('端点已删除', 'success');
        await this.loadOpenaiEndpoints();
      } else {
        this.showOpenaiToast('删除失败: ' + (data.error || '未知错误'), 'error');
      }
    } catch (error) {
      this.showOpenaiToast('删除失败: ' + error.message, 'error');
    }
  },

  async verifyOpenaiEndpoint(endpoint) {
    try {
      toast.info('正在验证...');
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });

      const data = await response.json();
      if (data.valid) {
        this.showOpenaiToast(`验证成功！找到 ${data.modelsCount || 0} 个模型`, 'success');
        await this.loadOpenaiEndpoints();
      } else {
        this.showOpenaiToast('验证失败: ' + (data.error || 'API Key 无效'), 'error');
      }
    } catch (error) {
      this.showOpenaiToast('验证失败: ' + error.message, 'error');
    }
  },

  async refreshEndpointModels(endpoint) {
    if (endpoint.refreshing) return;

    endpoint.refreshing = true;
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });

      const data = await response.json();
      if (data.valid) {
        this.showOpenaiToast(`${endpoint.name || '端点'} 模型列表已更新`, 'success');
        // 重新加载端点列表以获取新模型 (静默模式，不显示加载动画)
        await this.loadOpenaiEndpoints(true);
        // 如果是展开状态，确保它保持展开
      } else {
        this.showOpenaiToast('刷新失败: ' + (data.error || 'API Key 无效'), 'error');
      }
    } catch (error) {
      this.showOpenaiToast('刷新失败: ' + error.message, 'error');
    } finally {
      endpoint.refreshing = false;
    }
  },

  async toggleOpenaiEndpoint(endpoint) {
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/toggle`, {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ enabled: endpoint.enabled }),
      });

      const data = await response.json();
      if (data.success) {
        this.showOpenaiToast(endpoint.enabled ? '端点已启用' : '端点已禁用', 'success');
        // 刷新模型列表，因为禁用端点会影响可用模型
        this.updateOpenaiAllModels();
      } else {
        this.showOpenaiToast('操作失败: ' + (data.error || '未知错误'), 'error');
        // 恢复 UI 状态
        endpoint.enabled = !endpoint.enabled;
      }
    } catch (error) {
      this.showOpenaiToast('操作失败: ' + error.message, 'error');
      endpoint.enabled = !endpoint.enabled;
    }
  },

  async refreshAllOpenaiEndpoints() {
    store.openaiRefreshing = true;
    try {
      const response = await fetch('/api/openai/endpoints/refresh', {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });

      const data = await response.json();
      if (data.success) {
        const successCount = data.results?.filter(r => r.success).length || 0;
        this.showOpenaiToast(`刷新完成！已更新 ${successCount} 个启用端点`, 'success');
        await this.loadOpenaiEndpoints();
      } else {
        this.showOpenaiToast('刷新失败: ' + (data.error || '未知错误'), 'error');
      }
    } catch (error) {
      this.showOpenaiToast('刷新失败: ' + error.message, 'error');
    } finally {
      this.openaiRefreshing = false;
    }
  },

  async batchAddOpenaiEndpoints() {
    this.openaiBatchError = '';
    this.openaiBatchSuccess = '';

    if (!this.openaiBatchText.trim()) {
      this.openaiBatchError = '请输入端点信息';
      return;
    }

    this.openaiAdding = true;

    try {
      // 尝试解析为 JSON
      let endpoints = null;
      try {
        const parsed = JSON.parse(this.openaiBatchText);
        if (Array.isArray(parsed)) {
          endpoints = parsed;
        }
      } catch (e) {
        // 不是 JSON，使用文本格式
      }

      const response = await fetch('/api/openai/batch-add', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(endpoints ? { endpoints } : { text: this.openaiBatchText }),
      });

      const data = await response.json();
      if (data.success) {
        this.openaiBatchSuccess = `成功添加 ${data.added || 0} 个端点`;
        this.openaiBatchText = '';
        await this.loadOpenaiEndpoints();
        setTimeout(() => {
          this.openaiBatchSuccess = '';
        }, 3000);
      } else {
        this.openaiBatchError = data.error || '添加失败';
      }
    } catch (error) {
      this.openaiBatchError = '添加失败: ' + error.message;
    } finally {
      this.openaiAdding = false;
    }
  },

  toggleOpenaiModels(endpointId) {
    this.openaiExpandedEndpoints[endpointId] = !this.openaiExpandedEndpoints[endpointId];
  },

  isOpenaiEndpointExpanded(endpointId) {
    return !!this.openaiExpandedEndpoints[endpointId];
  },

  getModelName(model) {
    if (!model) return 'unknown';
    if (typeof model === 'string') {
      return model.trim();
    }
    if (typeof model === 'object') {
      return (model.id || model.name || 'unknown').trim();
    }
    return 'unknown';
  },

  maskApiKey(apiKey) {
    if (!apiKey) return '';
    if (apiKey.length <= 8) return '****';
    return apiKey.substring(0, 4) + '****' + apiKey.substring(apiKey.length - 4);
  },

  async copyToClipboard(text) {
    try {
      await navigator.clipboard.writeText(text);
      this.showOpenaiToast('已复制到剪贴板', 'success');
    } catch (error) {
      // 降级方案
      const textarea = document.createElement('textarea');
      textarea.value = text;
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand('copy');
      document.body.removeChild(textarea);
      this.showOpenaiToast('已复制到剪贴板', 'success');
    }
  },

  // 导出所有端点
  async exportOpenaiEndpoints() {
    try {
      if (store.openaiEndpoints.length === 0) {
        toast.warning('没有可导出的端点');
        return;
      }

      const exportData = {
        version: '1.0',
        exportTime: new Date().toISOString(),
        endpoints: store.openaiEndpoints.map(ep => ({
          name: ep.name,
          baseUrl: ep.baseUrl,
          apiKey: ep.apiKey,
          notes: ep.notes,
        })),
      };

      const dataStr = JSON.stringify(exportData, null, 2);
      const dataBlob = new Blob([dataStr], { type: 'application/json' });
      const url = URL.createObjectURL(dataBlob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `openai-endpoints-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);

      this.showOpenaiToast('端点导出成功', 'success');
    } catch (error) {
      this.showOpenaiToast('导出失败: ' + error.message, 'error');
    }
  },

  // 从文件导入端点
  // 点击模型跳转到对话页面
  goToChatWithModel(endpointId, modelName) {
    // 设置端点
    store.openaiChatEndpoint = endpointId;
    localStorage.setItem('openai_chat_endpoint', endpointId);

    // 设置模型
    store.openaiChatModel = modelName;

    // 清空当前会话状态，确保开始新对话
    store.openaiChatCurrentSessionId = null;
    store.openaiChatMessages = [];
    store.openaiChatSelectedSessionIds = [];

    // 切换到对话标签页
    store.openaiCurrentTab = 'chat';

    // 显示提示
    toast.success(`已设置端点并选中模型: ${modelName}`);
  },

  async importOpenaiEndpointsFromFile() {
    const confirmed = await store.showConfirm({
      title: '确认导入',
      message: '导入端点将添加到现有端点列表中，是否继续？',
      icon: 'fa-exclamation-triangle',
      confirmText: '确定导入',
      confirmClass: 'btn-primary',
    });

    if (!confirmed) return;

    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json';
    input.onchange = async event => {
      const file = event.target.files[0];
      if (!file) return;

      const reader = new FileReader();
      reader.onload = async e => {
        try {
          const importedData = JSON.parse(e.target.result);

          // 验证数据格式
          if (!importedData.version || !importedData.endpoints) {
            this.showOpenaiToast('无效的备份文件格式', 'error');
            return;
          }

          // 导入端点
          const response = await fetch('/api/openai/import', {
            method: 'POST',
            headers: store.getAuthHeaders(),
            body: JSON.stringify({ endpoints: importedData.endpoints }),
          });

          const data = await response.json();
          if (data.success) {
            let message = `成功导入 ${data.imported || 0} 个端点`;
            if (data.skipped > 0) {
              message += `，跳过 ${data.skipped} 个重复端点`;
            }
            this.showOpenaiToast(message, 'success');
            await this.loadOpenaiEndpoints();
          } else {
            this.showOpenaiToast('导入失败: ' + (data.error || '未知错误'), 'error');
          }
        } catch (error) {
          this.showOpenaiToast('导入失败: ' + error.message, 'error');
        }
      };
      reader.readAsText(file);
    };
    input.click();
  },

  // ==================== 自定义下拉菜单控制 ====================

  toggleEndpointDropdown(event) {
    if (event) event.stopPropagation();
    store.openaiShowEndpointDropdown = !store.openaiShowEndpointDropdown;
    store.openaiShowModelDropdown = false;
    store.showPersonaDropdown = false;
  },

  selectEndpoint(endpointId) {
    store.openaiChatEndpoint = endpointId;
    store.openaiShowEndpointDropdown = false;
    this.onChatEndpointChange();
  },

  toggleModelDropdown(event) {
    if (event) event.stopPropagation();
    store.openaiShowModelDropdown = !store.openaiShowModelDropdown;
    store.openaiShowEndpointDropdown = false;
    store.showPersonaDropdown = false;
    if (store.openaiShowModelDropdown) {
      store.dropdownModelSearch = '';
      this.$nextTick(() => {
        // 使用 $refs 访问搜索框 (需要在模板中设置 ref="modelSearchInput")
        // 由于 mixin 访问 $refs 可能受限，这里尝试更通用的 querySelector
        const input = document.querySelector('.dropdown-search input');
        if (input) input.focus();
      });
    }
  },

  selectChatModelForDropdown(modelId) {
    store.openaiChatModel = modelId;
    store.openaiShowModelDropdown = false;
    // 同步到当前会话
    this.syncCurrentSessionSettings();
  },

  closeAllDropdowns() {
    store.openaiShowEndpointDropdown = false;
    store.openaiShowModelDropdown = false;
    store.showPersonaDropdown = false;
  },

  getEndpointName(id) {
    if (!id) return '';
    const ep = store.openaiEndpoints.find(e => e.id === id);
    return ep ? ep.name : id;
  }
};
