import React, { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { toast } from '../modules/toast.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import useStore from '../store.js';
import { renderMarkdown, formatDateTime } from '../modules/utils.js';
import {
  Server,
  Users,
  MessageSquare,
  Plus,
  Trash,
  RotateCw,
  Search,
  Upload,
  Download,
  Edit,
  X,
  ChevronDown,
  ChevronUp,
  RefreshCw,
  History,
  Bot,
  Star,
  Pin,
  Activity,
  Send,
  Check,
  Paperclip,
  Eye,
  EyeOff,
  Plug,
  Brain,
  Sliders,
  Copy
} from '../components/Icons.jsx';

function OpenAIPage() {
  const { theme } = useStore();

  // Tab State
  const [activeTab, setActiveTab] = useState('endpoints'); // 'endpoints' | 'accounts' | 'chat'

  // Global Auth Headers Helper
  const getAuthHeaders = useCallback(() => {
    const password = localStorage.getItem('admin_password') || '';
    return {
      'Content-Type': 'application/json',
      'x-admin-password': password,
    };
  }, []);

  // IP/Address Masking Helper
  const maskAddress = (address) => {
    if (!address) return '';
    try {
      const url = new URL(address);
      return `${url.protocol}//${url.hostname.slice(0, 3)}****${url.hostname.slice(-3)}${url.port ? ':' + url.port : ''}${url.pathname}`;
    } catch {
      if (address.length > 15) {
        return address.slice(0, 6) + '****' + address.slice(-6);
      }
      return address;
    }
  };

  const maskApiKey = (key) => {
    if (!key) return '';
    if (key.length <= 8) return '****';
    return key.substring(0, 4) + '****' + key.substring(key.length - 4);
  };

  // ==================== 1. Endpoints & Accounts State ====================
  const [endpoints, setEndpoints] = useState([]);
  const [endpointsLoading, setEndpointsLoading] = useState(false);
  const [endpointsRefreshing, setEndpointsRefreshing] = useState(false);
  const [expandedEndpoints, setExpandedEndpoints] = useState({});
  const [endpointFormOpen, setEndpointFormOpen] = useState(false);
  const [editingEndpoint, setEditingEndpoint] = useState(null);
  const [endpointForm, setEndpointForm] = useState({ name: '', baseUrl: '', apiKey: '', notes: '' });
  const [endpointFormError, setEndpointFormError] = useState('');
  const [endpointSaving, setEndpointSaving] = useState(false);

  // Batch adding endpoints
  const [batchText, setBatchText] = useState('');
  const [batchError, setBatchError] = useState('');
  const [batchSuccess, setBatchSuccess] = useState('');
  const [batchAdding, setBatchAdding] = useState(false);

  // Load Endpoints
  const loadEndpoints = useCallback(async (silent = false) => {
    if (!silent) setEndpointsLoading(true);
    try {
      const response = await fetch('/api/openai/endpoints', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        setEndpoints(data.map(ep => ({ ...ep, showKey: false, refreshing: false })));
        localStorage.setItem('openai_endpoints_cache', JSON.stringify({
          endpoints: data,
          timestamp: Date.now()
        }));
      }
    } catch (error) {
      console.error('Failed to load endpoints:', error);
      toast.error('加载端点失败');
    } finally {
      if (!silent) setEndpointsLoading(false);
    }
  }, [getAuthHeaders]);

  useEffect(() => {
    // Try cache first
    try {
      const cached = localStorage.getItem('openai_endpoints_cache');
      if (cached) {
        const parsed = JSON.parse(cached);
        if (parsed && Array.isArray(parsed.endpoints)) {
          setEndpoints(parsed.endpoints.map(ep => ({ ...ep, showKey: false, refreshing: false })));
        }
      }
    } catch (e) {
      console.warn('Load endpoints cache failed:', e);
    }
    loadEndpoints(true);
  }, [loadEndpoints]);

  // Expand endpoint models grid
  const toggleEndpointExpand = (id) => {
    setExpandedEndpoints(prev => ({ ...prev, [id]: !prev[id] }));
  };

  // Endpoint Verification & Model Refresh
  const verifyEndpoint = async (endpoint) => {
    try {
      toast.info(`正在验证 ${endpoint.name || '端点'}...`);
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.valid) {
        toast.success(`验证成功！找到 ${data.modelsCount || 0} 个模型`);
        await loadEndpoints(true);
      } else {
        toast.error('验证失败: ' + (data.error || 'API Key 无效'));
      }
    } catch (error) {
      toast.error('验证失败: ' + error.message);
    }
  };

  const refreshEndpointModels = async (endpoint) => {
    if (endpoint.refreshing) return;
    // Set local refreshing
    setEndpoints(prev => prev.map(e => e.id === endpoint.id ? { ...e, refreshing: true } : e));
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.valid) {
        toast.success(`${endpoint.name || '端点'} 模型列表已更新`);
        await loadEndpoints(true);
      } else {
        toast.error('刷新失败: ' + (data.error || 'API Key 无效'));
      }
    } catch (error) {
      toast.error('刷新失败: ' + error.message);
    } finally {
      setEndpoints(prev => prev.map(e => e.id === endpoint.id ? { ...e, refreshing: false } : e));
    }
  };

  const refreshAllEndpoints = async () => {
    setEndpointsRefreshing(true);
    try {
      const response = await fetch('/api/openai/endpoints/refresh', {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        const successCount = data.results?.filter(r => r.success).length || 0;
        toast.success(`刷新完成！已更新 ${successCount} 个启用端点`);
        await loadEndpoints(true);
      } else {
        toast.error('刷新失败: ' + (data.error || '未知错误'));
      }
    } catch (error) {
      toast.error('刷新失败: ' + error.message);
    } finally {
      setEndpointsRefreshing(false);
    }
  };

  const toggleEndpointEnabled = async (endpoint) => {
    const updatedEnabled = !endpoint.enabled;
    // Optimistic UI update
    setEndpoints(prev => prev.map(e => e.id === endpoint.id ? { ...e, enabled: updatedEnabled } : e));
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled: updatedEnabled }),
      });
      const data = await response.json();
      if (!data.success) {
        toast.error('操作失败: ' + (data.error || '未知错误'));
        // Rollback
        setEndpoints(prev => prev.map(e => e.id === endpoint.id ? { ...e, enabled: !updatedEnabled } : e));
      } else {
        toast.success(updatedEnabled ? '端点已启用' : '端点已禁用');
        loadAllModels(true);
      }
    } catch (error) {
      toast.error('操作失败: ' + error.message);
      setEndpoints(prev => prev.map(e => e.id === endpoint.id ? { ...e, enabled: !updatedEnabled } : e));
    }
  };

  const openAddEndpointModal = () => {
    setEditingEndpoint(null);
    setEndpointForm({ name: '', baseUrl: '', apiKey: '', notes: '' });
    setEndpointFormError('');
    setEndpointFormOpen(true);
  };

  const openEditEndpointModal = (endpoint) => {
    setEditingEndpoint(endpoint);
    setEndpointForm({
      name: endpoint.name || '',
      baseUrl: endpoint.baseUrl || '',
      apiKey: endpoint.apiKey || '',
      notes: endpoint.notes || '',
    });
    setEndpointFormError('');
    setEndpointFormOpen(true);
  };

  const saveEndpoint = async () => {
    if (!endpointForm.baseUrl || !endpointForm.apiKey) {
      setEndpointFormError('请填写 API 地址和 API Key');
      return;
    }
    setEndpointSaving(true);
    setEndpointFormError('');
    try {
      const url = editingEndpoint
        ? `/api/openai/endpoints/${editingEndpoint.id}`
        : '/api/openai/endpoints';
      const response = await fetch(url, {
        method: editingEndpoint ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(endpointForm),
      });
      const data = await response.json();
      if (response.ok && (data.success || data.endpoint || data.id)) {
        toast.success(editingEndpoint ? '端点已更新' : '端点已添加');
        setEndpointFormOpen(false);
        await loadEndpoints(true);
        loadAllModels(true);
      } else {
        setEndpointFormError(data.error || '保存失败');
      }
    } catch (error) {
      setEndpointFormError('保存失败: ' + error.message);
    } finally {
      setEndpointSaving(false);
    }
  };

  const deleteEndpoint = async (endpoint) => {
    if (!window.confirm(`确定要删除端点 "${endpoint.name || endpoint.baseUrl}" 吗？`)) {
      return;
    }
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (response.ok && data.success) {
        toast.success('端点已删除');
        await loadEndpoints(true);
        loadAllModels(true);
      } else {
        toast.error('删除失败: ' + (data.error || '未知错误'));
      }
    } catch (error) {
      toast.error('删除失败: ' + error.message);
    }
  };

  const batchAddEndpoints = async () => {
    setBatchError('');
    setBatchSuccess('');
    if (!batchText.trim()) {
      setBatchError('请输入端点信息');
      return;
    }
    setBatchAdding(true);
    try {
      let payload = { text: batchText };
      try {
        const parsed = JSON.parse(batchText);
        if (Array.isArray(parsed)) {
          payload = { endpoints: parsed };
        }
      } catch (e) {
        // Fallback to text format
      }

      const response = await fetch('/api/openai/batch-add', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(payload),
      });
      const data = await response.json();
      if (data.success) {
        setBatchSuccess(`成功添加 ${data.added || 0} 个端点`);
        setBatchText('');
        await loadEndpoints(true);
        loadAllModels(true);
      } else {
        setBatchError(data.error || '添加失败');
      }
    } catch (error) {
      setBatchError('添加失败: ' + error.message);
    } finally {
      setBatchAdding(false);
    }
  };

  const exportEndpoints = () => {
    if (endpoints.length === 0) {
      toast.warning('没有可导出的端点');
      return;
    }
    try {
      const exportData = {
        version: '1.0',
        exportTime: new Date().toISOString(),
        endpoints: endpoints.map(ep => ({
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
      link.download = `openai-endpoints-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);
      toast.success('端点导出成功');
    } catch (error) {
      toast.error('导出失败: ' + error.message);
    }
  };

  const importEndpoints = () => {
    if (!window.confirm('确认导入？导入端点将添加到现有列表中。')) {
      return;
    }
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
          if (!importedData.endpoints) {
            toast.error('无效的备份文件格式');
            return;
          }
          const response = await fetch('/api/openai/import', {
            method: 'POST',
            headers: getAuthHeaders(),
            body: JSON.stringify({ endpoints: importedData.endpoints }),
          });
          const data = await response.json();
          if (data.success) {
            let msg = `成功导入 ${data.imported || 0} 个端点`;
            if (data.skipped > 0) msg += `，跳过 ${data.skipped} 个重复项`;
            toast.success(msg);
            await loadEndpoints(true);
            loadAllModels(true);
          } else {
            toast.error('导入失败: ' + (data.error || '未知错误'));
          }
        } catch (error) {
          toast.error('导入失败: ' + error.message);
        }
      };
      reader.readAsText(file);
    };
    input.click();
  };

  // ==================== 2. Health Checking ====================
  const [openaiModelHealth, setOpenaiModelHealth] = useState(() => {
    try {
      const saved = localStorage.getItem('openai_model_health_cache');
      return saved ? JSON.parse(saved) : {};
    } catch {
      return {};
    }
  });

  const saveModelHealth = (health) => {
    setOpenaiModelHealth(health);
    localStorage.setItem('openai_model_health_cache', JSON.stringify(health));
  };

  const [modelHealthBatchLoading, setModelHealthBatchLoading] = useState(false);
  const [healthCheckModal, setHealthCheckModal] = useState(false);
  const [healthCheckForm, setHealthCheckForm] = useState({
    useKey: 'single', // 'single' | 'all'
    concurrency: false,
    timeout: 10,
  });

  const testModelHealth = async (model, targetEndpointId = null) => {
    const modelId = model.id;
    saveModelHealth(prev => ({
      ...prev,
      [modelId]: { status: 'checking', loading: true, latency: null, checkedAt: Date.now() }
    }));

    try {
      const headers = { ...getAuthHeaders(), 'Content-Type': 'application/json' };
      if (targetEndpointId) {
        headers['x-endpoint-id'] = targetEndpointId;
      }
      const startTime = Date.now();
      const response = await fetch('/api/openai/v1/chat/completions', {
        method: 'POST',
        headers,
        body: JSON.stringify({
          model: modelId,
          messages: [{ role: 'user', content: 'ping' }],
          max_tokens: 5,
        }),
      });

      const latency = Date.now() - startTime;
      if (response.ok) {
        saveModelHealth(prev => ({
          ...prev,
          [modelId]: { status: 'healthy', loading: false, latency, checkedAt: Date.now() }
        }));
      } else {
        saveModelHealth(prev => ({
          ...prev,
          [modelId]: { status: 'error', loading: false, latency: null, checkedAt: Date.now(), error: `HTTP ${response.status}` }
        }));
      }
    } catch (e) {
      saveModelHealth(prev => ({
        ...prev,
        [modelId]: { status: 'error', loading: false, latency: null, checkedAt: Date.now(), error: e.message }
      }));
    }
  };

  const startBatchHealthCheck = async () => {
    setHealthCheckModal(false);
    setModelHealthBatchLoading(true);
    toast.info('已启动后台模型可用性健康检测，请稍候...');

    // Collect all unique models
    const allModelsMap = new Map();
    endpoints.forEach(ep => {
      if (ep.enabled && ep.models) {
        ep.models.forEach(m => {
          const id = typeof m === 'string' ? m : m.id;
          if (id) allModelsMap.set(id, ep.id);
        });
      }
    });

    const modelsToCheck = Array.from(allModelsMap.entries()).map(([id, epId]) => ({ id, epId }));
    if (modelsToCheck.length === 0) {
      toast.warning('没有找到任何启用的端点或模型');
      setModelHealthBatchLoading(false);
      return;
    }

    if (healthCheckForm.concurrency) {
      // Parallel execution
      await Promise.all(modelsToCheck.map(m => testModelHealth(m, m.epId)));
    } else {
      // Sequential execution
      for (const m of modelsToCheck) {
        await testModelHealth(m, m.epId);
      }
    }

    toast.success('所有模型可用性健康检测完成');
    setModelHealthBatchLoading(false);
  };

  const openHealthCheckForEndpoint = async (endpointId) => {
    const ep = endpoints.find(e => e.id === endpointId);
    if (!ep || !ep.models || ep.models.length === 0) {
      toast.warning('该端点无可用模型');
      return;
    }
    setModelHealthBatchLoading(true);
    toast.info(`正在检测 ${ep.name || '端点'} 的所有模型...`);
    for (const m of ep.models) {
      const modelId = typeof m === 'string' ? m : m.id;
      await testModelHealth({ id: modelId }, ep.id);
    }
    toast.success(`${ep.name || '端点'} 模型检测完成`);
    setModelHealthBatchLoading(false);
  };

  // ==================== 3. Models List & Pinning ====================
  const [allModels, setAllModels] = useState([]);
  const [pinnedModels, setPinnedModels] = useState(() => {
    try {
      const saved = localStorage.getItem('openai_pinned_models');
      return saved ? JSON.parse(saved) : [];
    } catch {
      return [];
    }
  });
  const [hiddenModels, setHiddenModels] = useState(() => {
    try {
      const saved = localStorage.getItem('openai_hidden_models');
      return saved ? JSON.parse(saved) : [];
    } catch {
      return [];
    }
  });

  const [chatEndpoint, setChatEndpoint] = useState(() => {
    return localStorage.getItem('openai_chat_endpoint') || '';
  });
  const [chatModel, setChatModel] = useState(() => {
    return localStorage.getItem('openai_chat_model') || '';
  });
  const [defaultChatModel, setDefaultChatModel] = useState(() => {
    return localStorage.getItem('openai_default_model') || '';
  });

  // Settings configurations
  const [showHChatSettingsModal, setShowHChatSettingsModal] = useState(false);
  const [openaiSettingsTab, setOpenaiSettingsTab] = useState('general');
  const [openaiChatSystemPrompt, setOpenaiChatSystemPrompt] = useState(() => {
    return localStorage.getItem('openai_system_prompt') || '你是一个有用的 AI 助手。';
  });
  const [openaiChatSettings, setOpenaiChatSettings] = useState(() => {
    try {
      const saved = localStorage.getItem('openai_chat_settings');
      return saved ? JSON.parse(saved) : { temperature: 0.7, max_tokens: 2000 };
    } catch {
      return { temperature: 0.7, max_tokens: 2000 };
    }
  });

  const [openaiAutoTitleEnabled, setOpenaiAutoTitleEnabled] = useState(() => {
    return localStorage.getItem('openai_auto_title_enabled') === 'true';
  });
  const [openaiTitleModels, setOpenaiTitleModels] = useState(() => {
    try {
      const saved = localStorage.getItem('openai_title_models');
      return saved ? JSON.parse(saved) : [];
    } catch {
      return [];
    }
  });
  const [openaiTitleModelToAdd, setOpenaiTitleModelToAdd] = useState('');
  const [openaiTitleGenerating, setOpenaiTitleGenerating] = useState(false);
  const [openaiTitleLastResult, setOpenaiTitleLastResult] = useState(null);

  // Model selection helper dropdowns UI states
  const [showEndpointDropdown, setShowEndpointDropdown] = useState(false);
  const [showModelDropdown, setShowModelDropdown] = useState(false);
  const [dropdownModelSearch, setDropdownModelSearch] = useState('');
  const [openaiModelSearch, setOpenaiModelSearch] = useState('');
  const [openaiSelectedEndpointId, setOpenaiSelectedEndpointId] = useState('');
  const [openaiShowHiddenModels, setOpenaiShowHiddenModels] = useState(false);

  // Close dropdowns on outside click
  useEffect(() => {
    const handleOutsideClick = () => {
      setShowEndpointDropdown(false);
      setShowModelDropdown(false);
      setShowPersonaDropdown(false);
    };
    window.addEventListener('click', handleOutsideClick);
    return () => window.removeEventListener('click', handleOutsideClick);
  }, []);

  const loadAllModels = useCallback(async (silent = false) => {
    try {
      const response = await fetch('/api/openai/v1/models', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data && Array.isArray(data.data)) {
        const sorted = data.data.sort((a, b) => {
          if (a.owned_by !== b.owned_by) return a.owned_by.localeCompare(b.owned_by);
          return a.id.localeCompare(b.id);
        });
        setAllModels(sorted);

        // Smart initialize model
        if (sorted.length > 0) {
          const currentModel = localStorage.getItem('openai_chat_model');
          let modelIsValid = false;
          if (currentModel) {
            modelIsValid = sorted.some(m => m.id === currentModel);
          }
          if (!modelIsValid) {
            const defModel = localStorage.getItem('openai_default_model');
            if (defModel && sorted.some(m => m.id === defModel)) {
              setChatModel(defModel);
              localStorage.setItem('openai_chat_model', defModel);
            } else {
              setChatModel(sorted[0].id);
              localStorage.setItem('openai_chat_model', sorted[0].id);
            }
          }
        }
      }
    } catch (error) {
      console.error('Failed to load models list:', error);
    }
  }, [getAuthHeaders]);

  useEffect(() => {
    loadAllModels(true);
  }, [loadAllModels]);

  const togglePinModel = (modelId) => {
    if (!modelId) return;
    setPinnedModels(prev => {
      let next;
      if (prev.includes(modelId)) {
        next = prev.filter(id => id !== modelId);
      } else {
        next = [...prev, modelId];
      }
      localStorage.setItem('openai_pinned_models', JSON.stringify(next));
      return next;
    });
  };

  const toggleHideModel = (modelId) => {
    if (!modelId) return;
    setHiddenModels(prev => {
      let next;
      if (prev.includes(modelId)) {
        next = prev.filter(id => id !== modelId);
      } else {
        next = [...prev, modelId];
      }
      localStorage.setItem('openai_hidden_models', JSON.stringify(next));
      return next;
    });
  };

  const handleSetDefaultModel = () => {
    if (!chatModel) return;
    setDefaultChatModel(chatModel);
    localStorage.setItem('openai_default_model', chatModel);
    toast.success(`已将 ${chatModel} 设为默认模型`);
  };

  const handleClearDefaultModel = () => {
    setDefaultChatModel('');
    localStorage.removeItem('openai_default_model');
    toast.success('已清除默认模型');
  };

  const saveChatSettings = () => {
    localStorage.setItem('openai_system_prompt', openaiChatSystemPrompt);
    localStorage.setItem('openai_chat_settings', JSON.stringify(openaiChatSettings));
    setShowHChatSettingsModal(false);
    toast.success('对话设置已保存');
  };

  const saveAutoTitleSettings = (enabled, models) => {
    localStorage.setItem('openai_auto_title_enabled', enabled ? 'true' : 'false');
    localStorage.setItem('openai_title_models', JSON.stringify(models));
  };

  const addTitleModel = () => {
    if (!openaiTitleModelToAdd) return;
    if (!openaiTitleModels.includes(openaiTitleModelToAdd)) {
      const next = [...openaiTitleModels, openaiTitleModelToAdd];
      setOpenaiTitleModels(next);
      saveAutoTitleSettings(openaiAutoTitleEnabled, next);
    }
    setOpenaiTitleModelToAdd('');
  };

  const removeTitleModel = (modelId) => {
    const next = openaiTitleModels.filter(m => m !== modelId);
    setOpenaiTitleModels(next);
    saveAutoTitleSettings(openaiAutoTitleEnabled, next);
  };

  // Helper title models filtering
  const filteredTitleModelOptions = () => {
    const allModelsMap = new Map();
    allModels.forEach(m => allModelsMap.set(m.id, m));
    endpoints.forEach(ep => {
      if (ep.models) {
        ep.models.forEach(m => {
          const id = typeof m === 'string' ? m : m.id;
          if (id && !allModelsMap.has(id)) {
            allModelsMap.set(id, { id, owned_by: ep.name || 'custom' });
          }
        });
      }
    });
    return Array.from(allModelsMap.values()).filter(m => !openaiTitleModels.includes(m.id));
  };

  // ==================== 4. Personas State ====================
  const [personas, setPersonas] = useState([]);
  const [currentPersonaId, setCurrentPersonaId] = useState(null);
  const [showPersonaDropdown, setShowPersonaDropdown] = useState(false);
  const [personaModalOpen, setPersonaModalOpen] = useState(false);
  const [editingPersona, setEditingPersona] = useState(null);
  const [personaForm, setPersonaForm] = useState({ name: '', icon: 'fa-robot', systemPrompt: '' });

  const loadPersonas = useCallback(async () => {
    try {
      const response = await fetch('/api/chat/personas', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success && Array.isArray(data.data)) {
        setPersonas(data.data);
        if (data.data.length > 0) {
          const def = data.data.find(p => p.is_default);
          if (def) {
            setCurrentPersonaId(def.id);
            setOpenaiChatSystemPrompt(def.system_prompt);
          } else {
            setCurrentPersonaId(data.data[0].id);
            setOpenaiChatSystemPrompt(data.data[0].system_prompt);
          }
        }
      }
    } catch (e) {
      console.error('Failed to load personas:', e);
    }
  }, [getAuthHeaders]);

  useEffect(() => {
    loadPersonas();
  }, [loadPersonas]);

  const handleSelectPersona = (personaId) => {
    setCurrentPersonaId(personaId);
    setShowPersonaDropdown(false);
    const p = personas.find(item => item.id === personaId);
    if (p) {
      setOpenaiChatSystemPrompt(p.system_prompt);
      toast.success(`切换人设为: ${p.name}`);
    }
  };

  const openPersonaModal = (persona = null) => {
    setEditingPersona(persona);
    if (persona) {
      setPersonaForm({
        name: persona.name || '',
        icon: persona.icon || 'fa-robot',
        systemPrompt: persona.system_prompt || '',
      });
    } else {
      setPersonaForm({ name: '', icon: 'fa-robot', systemPrompt: '' });
    }
    setPersonaModalOpen(true);
  };

  const savePersona = async () => {
    if (!personaForm.name.trim() || !personaForm.systemPrompt.trim()) {
      toast.warning('请输入名称和提示词');
      return;
    }
    try {
      const url = editingPersona
        ? `/api/chat/personas/${editingPersona.id}`
        : '/api/chat/personas';
      const response = await fetch(url, {
        method: editingPersona ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          name: personaForm.name,
          icon: personaForm.icon,
          system_prompt: personaForm.systemPrompt,
        }),
      });
      const data = await response.json();
      if (data.success) {
        toast.success(editingPersona ? '人设已更新' : '人设已创建');
        setPersonaModalOpen(false);
        await loadPersonas();
      } else {
        toast.error('保存失败: ' + (data.error || '未知错误'));
      }
    } catch (e) {
      toast.error('保存失败: ' + e.message);
    }
  };

  const deletePersona = async (personaId) => {
    if (!window.confirm('确定要删除这个 AI 人设吗？')) {
      return;
    }
    try {
      const response = await fetch(`/api/chat/personas/${personaId}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        toast.success('人设已删除');
        await loadPersonas();
      } else {
        toast.error('删除失败: ' + (data.error || '未知错误'));
      }
    } catch (e) {
      toast.error('删除失败: ' + e.message);
    }
  };

  // ==================== 5. Chat History & Streaming ====================
  const [sessions, setSessions] = useState([]);
  const [currentSessionId, setCurrentSessionId] = useState(null);
  const [messages, setMessages] = useState([]);
  const [messageInput, setMessageInput] = useState('');
  const [attachments, setAttachments] = useState([]);
  const [chatLoading, setChatLoading] = useState(false);
  const [chatHistoryLoading, setChatHistoryLoading] = useState(false);
  const [selectedSessionIds, setSelectedSessionIds] = useState([]);
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
  const [chatHistoryCollapsed, setChatHistoryCollapsed] = useState(false);

  const abortControllerRef = useRef(null);
  const messagesEndRef = useRef(null);

  const loadSessions = useCallback(async () => {
    setChatHistoryLoading(true);
    try {
      const response = await fetch('/api/chat/sessions', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success && Array.isArray(data.data)) {
        setSessions(data.data);
      }
    } catch (e) {
      console.error('Failed to load chat history:', e);
    } finally {
      setChatHistoryLoading(false);
    }
  }, [getAuthHeaders]);

  useEffect(() => {
    if (activeTab === 'chat') {
      loadSessions();
    }
  }, [activeTab, loadSessions]);

  const loadSession = async (sessionId) => {
    if (chatLoading) return;
    setChatHistoryLoading(true);
    try {
      const response = await fetch(`/api/chat/sessions/${sessionId}/messages`, {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        setCurrentSessionId(sessionId);
        const parsedMessages = data.data.map(msg => {
          let content = msg.content;
          if (content && typeof content === 'string' && content.startsWith('[')) {
            try {
              content = JSON.parse(content);
            } catch (e) { }
          }
          return {
            id: msg.id,
            role: msg.role,
            content,
            reasoning: msg.reasoning || '',
            showReasoning: false,
            timestamp: msg.created_at || msg.timestamp,
            model: msg.model || chatModel,
          };
        });
        setMessages(parsedMessages);

        // Sync settings
        const session = sessions.find(s => s.id === sessionId);
        if (session) {
          if (session.model) {
            setChatModel(session.model);
            localStorage.setItem('openai_chat_model', session.model);
          }
          if (session.endpoint_id) {
            setChatEndpoint(session.endpoint_id);
            localStorage.setItem('openai_chat_endpoint', session.endpoint_id);
          }
          if (session.persona_id) {
            setCurrentPersonaId(session.persona_id);
            const p = personas.find(item => item.id === session.persona_id);
            if (p) setOpenaiChatSystemPrompt(p.system_prompt);
          }
        }
      }
    } catch (error) {
      toast.error('加载会话失败');
    } finally {
      setChatHistoryLoading(false);
      setMobileSidebarOpen(false);
    }
  };

  const createSession = async (resetToDefault = false) => {
    try {
      const globalSystemPrompt = localStorage.getItem('openai_system_prompt') || '你是一个有用的 AI 助手。';
      let globalSettings = {};
      try {
        globalSettings = JSON.parse(localStorage.getItem('openai_chat_settings')) || {};
      } catch (e) { }

      let finalModel = chatModel;
      if (defaultChatModel && (resetToDefault || !chatModel)) {
        finalModel = defaultChatModel;
        setChatModel(def => {
          localStorage.setItem('openai_chat_model', def);
          return def;
        });
      }

      const currentPersona = personas.find(p => p.id === currentPersonaId);
      const systemPrompt = currentPersona ? currentPersona.system_prompt : globalSystemPrompt;

      const response = await fetch('/api/chat/sessions', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          title: '新对话',
          model: finalModel,
          endpoint_id: chatEndpoint || '',
          persona_id: currentPersonaId,
          system_prompt: systemPrompt,
        }),
      });
      const data = await response.json();
      if (data.success) {
        setSessions(prev => [data.data, ...prev]);
        setCurrentSessionId(data.data.id);
        setMessages([]);
        toast.success('已创建新对话');
      }
    } catch (error) {
      toast.error('创建会话失败');
    }
  };

  const deleteSession = async (sessionId, e) => {
    if (e) e.stopPropagation();
    if (!window.confirm('确定要删除这个对话吗？此操作不可撤销。')) return;
    try {
      const response = await fetch(`/api/chat/sessions/${sessionId}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        setSessions(prev => prev.filter(s => s.id !== sessionId));
        if (currentSessionId === sessionId) {
          setCurrentSessionId(null);
          setMessages([]);
        }
        toast.success('对话已删除');
      }
    } catch (error) {
      toast.error('删除会话失败');
    }
  };

  const deleteSelectedSessions = async () => {
    if (selectedSessionIds.length === 0) return;
    if (!window.confirm(`确定要删除选中的 ${selectedSessionIds.length} 个对话吗？`)) return;
    try {
      const response = await fetch('/api/chat/sessions', {
        method: 'DELETE',
        headers: getAuthHeaders(),
        body: JSON.stringify({ ids: selectedSessionIds }),
      });
      const data = await response.json();
      if (data.success) {
        setSessions(prev => prev.filter(s => !selectedSessionIds.includes(s.id)));
        if (selectedSessionIds.includes(currentSessionId)) {
          setCurrentSessionId(null);
          setMessages([]);
        }
        setSelectedSessionIds([]);
        toast.success('删除成功');
      }
    } catch (error) {
      toast.error('批量删除失败');
    }
  };

  const clearAllSessions = async () => {
    if (sessions.length === 0) return;
    if (!window.confirm('确定要清空所有聊天历史吗？此操作不可撤销。')) return;
    try {
      const response = await fetch('/api/chat/sessions', {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        setSessions([]);
        setCurrentSessionId(null);
        setMessages([]);
        setSelectedSessionIds([]);
        toast.success('所有对话已清空');
      }
    } catch (error) {
      toast.error('清空历史失败');
    }
  };

  const toggleSessionSelection = (sessionId, e) => {
    if (e) e.stopPropagation();
    setSelectedSessionIds(prev =>
      prev.includes(sessionId) ? prev.filter(id => id !== sessionId) : [...prev, sessionId]
    );
  };

  const toggleSelectAllSessions = () => {
    if (selectedSessionIds.length === sessions.length) {
      setSelectedSessionIds([]);
    } else {
      setSelectedSessionIds(sessions.map(s => s.id));
    }
  };

  // Generate Title
  const generateTitleWithFallback = async (messagesList) => {
    const modelsToTry = openaiTitleModels.length > 0 ? [...openaiTitleModels] : [chatModel];
    const conversationText = messagesList.slice(0, 4).map(msg => {
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

    const titlePrompt = `请根据以下对话内容，生成一个简洁的中文标题（最多15个字，不要使用标点符号，直接输出标题内容）：\n\n${conversationText}\n\n标题：`;

    for (const modelId of modelsToTry) {
      try {
        const headers = { ...getAuthHeaders(), 'Content-Type': 'application/json' };
        const endpoint = endpoints.find(ep =>
          ep.models && ep.models.some(m => (typeof m === 'string' ? m : m.id) === modelId)
        );
        if (endpoint) {
          headers['x-endpoint-id'] = endpoint.id;
        }

        const response = await fetch('/api/openai/v1/chat/completions', {
          method: 'POST',
          headers,
          body: JSON.stringify({
            model: modelId,
            messages: [{ role: 'user', content: titlePrompt }],
            max_tokens: 30,
            temperature: 0.7,
          }),
        });

        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const result = await response.json();
        let generatedTitle = result.choices?.[0]?.message?.content?.trim() || '';

        if (!generatedTitle && result.choices?.[0]?.message?.reasoning_content) {
          const reasoning = result.choices[0].message.reasoning_content.trim();
          const lines = reasoning.split('\n').filter(l => l.trim());
          if (lines.length > 0) generatedTitle = lines[lines.length - 1].trim();
        }

        if (generatedTitle) {
          return { success: true, title: generatedTitle, model: modelId };
        }
      } catch (e) {
        console.warn(`Generate title with model ${modelId} failed:`, e);
      }
    }
    throw new Error('All models failed to generate title');
  };

  const generateChatTitle = async (currentMsgs, sessionId) => {
    if (!sessionId || currentMsgs.length < 2) return;
    const session = sessions.find(s => s.id === sessionId);
    if (!session || session.title !== '新对话') return;

    if (!openaiAutoTitleEnabled) {
      const firstUser = currentMsgs.find(m => m.role === 'user');
      if (firstUser) {
        let simpleTitle = typeof firstUser.content === 'string' ? firstUser.content : '📷 图片对话';
        simpleTitle = simpleTitle.slice(0, 18) + (simpleTitle.length > 18 ? '...' : '');
        try {
          await fetch(`/api/chat/sessions/${sessionId}`, {
            method: 'PUT',
            headers: getAuthHeaders(),
            body: JSON.stringify({ title: simpleTitle }),
          });
          setSessions(prev => prev.map(s => s.id === sessionId ? { ...s, title: simpleTitle } : s));
        } catch (e) {
          console.error(e);
        }
      }
      return;
    }

    try {
      const result = await generateTitleWithFallback(currentMsgs);
      if (result.success) {
        await fetch(`/api/chat/sessions/${sessionId}`, {
          method: 'PUT',
          headers: getAuthHeaders(),
          body: JSON.stringify({
            title: result.title,
            model: chatModel,
            endpoint_id: chatEndpoint || '',
            system_prompt: openaiChatSystemPrompt,
          }),
        });
        setSessions(prev => prev.map(s => s.id === sessionId ? { ...s, title: result.title } : s));
      }
    } catch (error) {
      // Fallback
      const firstUser = currentMsgs.find(m => m.role === 'user');
      if (firstUser) {
        let fallbackTitle = typeof firstUser.content === 'string' ? firstUser.content : '📷 图片对话';
        fallbackTitle = fallbackTitle.slice(0, 18) + (fallbackTitle.length > 18 ? '...' : '');
        try {
          await fetch(`/api/chat/sessions/${sessionId}`, {
            method: 'PUT',
            headers: getAuthHeaders(),
            body: JSON.stringify({ title: fallbackTitle }),
          });
          setSessions(prev => prev.map(s => s.id === sessionId ? { ...s, title: fallbackTitle } : s));
        } catch (e) {
          console.error(e);
        }
      }
    }
  };

  const testTitleGeneration = async () => {
    setOpenaiTitleGenerating(true);
    setOpenaiTitleLastResult(null);
    const testMessages = [
      { role: 'user', content: '帮我解释一下什么是机器学习' },
      { role: 'assistant', content: '机器学习是人工智能的一个分支，它使计算机能够从数据中学习...' }
    ];
    try {
      const result = await generateTitleWithFallback(testMessages);
      setOpenaiTitleLastResult(result);
    } catch (e) {
      setOpenaiTitleLastResult({ success: false, error: e.message });
    } finally {
      setOpenaiTitleGenerating(false);
    }
  };

  // Chat message sending / streaming API
  const saveChatMessage = async (sessionId, role, content, reasoning = null) => {
    try {
      const response = await fetch(`/api/chat/sessions/${sessionId}/messages`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ role, content, reasoning }),
      });
      const data = await response.json();
      return data.success ? data.data : null;
    } catch (error) {
      console.error('Failed to save message:', error);
      return null;
    }
  };

  const stopGenerating = () => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      abortControllerRef.current = null;
    }
    setChatLoading(false);
  };

  const scrollToBottom = (behavior = 'smooth') => {
    if (messagesEndRef.current) {
      messagesEndRef.current.scrollIntoView({ behavior });
    }
  };

  useEffect(() => {
    scrollToBottom();
  }, [messages, chatLoading]);

  const handleSendChat = async () => {
    if ((!messageInput.trim() && attachments.length === 0) || chatLoading) return;

    const userText = messageInput;
    const currentAttachments = [...attachments];
    setMessageInput('');
    setAttachments([]);

    let activeSessionId = currentSessionId;
    if (!activeSessionId) {
      // Create session first
      try {
        const globalSystemPrompt = localStorage.getItem('openai_system_prompt') || '你是一个有用的 AI 助手。';
        const response = await fetch('/api/chat/sessions', {
          method: 'POST',
          headers: getAuthHeaders(),
          body: JSON.stringify({
            title: '新对话',
            model: chatModel,
            endpoint_id: chatEndpoint || '',
            persona_id: currentPersonaId,
            system_prompt: openaiChatSystemPrompt,
          }),
        });
        const data = await response.json();
        if (data.success) {
          setSessions(prev => [data.data, ...prev]);
          activeSessionId = data.data.id;
          setCurrentSessionId(activeSessionId);
        } else {
          toast.error('创建会话失败');
          return;
        }
      } catch (err) {
        toast.error('创建会话失败');
        return;
      }
    }

    let userContent;
    if (currentAttachments.length > 0) {
      userContent = [{ type: 'text', text: userText }];
      currentAttachments.forEach(att => {
        userContent.push({
          type: 'image_url',
          image_url: { url: att.url }
        });
      });
    } else {
      userContent = userText;
    }

    const contentToSave = typeof userContent === 'string' ? userContent : JSON.stringify(userContent);
    const userMsg = {
      role: 'user',
      content: userContent,
      timestamp: new Date().toISOString(),
      isNew: true
    };

    setMessages(prev => [...prev, userMsg]);
    setChatLoading(true);

    // Save user message
    saveChatMessage(activeSessionId, 'user', contentToSave).then(saved => {
      if (saved && saved.id) {
        userMsg.id = saved.id;
      }
    });

    abortControllerRef.current = new AbortController();

    try {
      const messagesPayload = [
        { role: 'system', content: openaiChatSystemPrompt },
        ...messages.map(m => ({ role: m.role, content: typeof m.content === 'string' ? m.content : JSON.stringify(m.content) })),
        { role: 'user', content: contentToSave }
      ];

      const headers = {
        ...getAuthHeaders(),
        'Content-Type': 'application/json',
      };

      let targetEpId = chatEndpoint;
      if (!targetEpId && chatModel) {
        const found = endpoints.find(ep =>
          ep.models && ep.models.some(m => (typeof m === 'string' ? m : m.id) === chatModel)
        );
        if (found) targetEpId = found.id;
      }
      if (targetEpId) {
        headers['x-endpoint-id'] = targetEpId;
      }

      const response = await fetch('/api/openai/v1/chat/completions', {
        method: 'POST',
        headers,
        body: JSON.stringify({
          model: chatModel,
          messages: messagesPayload,
          stream: true,
          ...openaiChatSettings,
        }),
        signal: abortControllerRef.current?.signal,
      });

      if (!response.ok) {
        let errText = `HTTP 错误 ${response.status}`;
        try {
          const json = await response.json();
          errText = json.error?.message || json.message || JSON.stringify(json);
        } catch { }
        throw new Error(errText);
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let assistantMsg = {
        role: 'assistant',
        content: '',
        reasoning: '',
        showReasoning: true,
        timestamp: new Date().toISOString(),
        model: chatModel,
        isNew: true
      };

      setMessages(prev => [...prev, assistantMsg]);

      let buffer = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
          const trimmed = line.trim();
          if (trimmed.startsWith('data: ')) {
            const dataStr = trimmed.slice(6);
            if (dataStr === '[DONE]') break;
            try {
              const parsed = JSON.parse(dataStr);
              const delta = parsed.choices?.[0]?.delta;
              if (delta) {
                if (delta.reasoning_content) {
                  assistantMsg.reasoning += delta.reasoning_content;
                }
                if (delta.content) {
                  assistantMsg.content += delta.content;
                }
                setMessages(prev => prev.map((m, idx) => idx === prev.length - 1 ? { ...assistantMsg } : m));
              }
            } catch (e) { }
          }
        }
      }

      // Save assistant message to DB
      const saved = await saveChatMessage(activeSessionId, 'assistant', assistantMsg.content, assistantMsg.reasoning || null);
      if (saved && saved.id) {
        assistantMsg.id = saved.id;
        setMessages(prev => prev.map((m, idx) => idx === prev.length - 1 ? { ...m, id: saved.id } : m));
      }

      // Check auto title
      const sess = sessions.find(s => s.id === activeSessionId);
      if (sess && sess.title === '新对话') {
        const currentMsgs = [...messages, userMsg, assistantMsg];
        generateChatTitle(currentMsgs, activeSessionId);
      }
    } catch (error) {
      if (error.name === 'AbortError') return;
      toast.error('对话失败: ' + error.message);
      setMessages(prev => [
        ...prev,
        { role: 'assistant', content: `❌ **错误**: ${error.message}`, timestamp: new Date().toISOString() }
      ]);
    } finally {
      setChatLoading(false);
      abortControllerRef.current = null;
    }
  };

  const deleteChatMessage = async (index) => {
    if (index < 0 || index >= messages.length) return;
    const msg = messages[index];
    if (msg.id && currentSessionId) {
      try {
        await fetch(`/api/chat/sessions/${currentSessionId}/messages/${msg.id}`, {
          method: 'DELETE',
          headers: getAuthHeaders(),
        });
      } catch (err) {
        console.error('Delete message backend error:', err);
      }
    }
    setMessages(prev => prev.filter((_, idx) => idx !== index));
  };

  const regenerateChat = async (index = -1) => {
    if (chatLoading || messages.length === 0) return;
    let targetIndex = index;
    if (targetIndex === -1) {
      for (let i = messages.length - 1; i >= 0; i--) {
        if (messages[i].role === 'assistant') {
          targetIndex = i;
          break;
        }
      }
    }
    if (targetIndex === -1) {
      targetIndex = messages.length - 1;
    }

    const targetMsg = messages[targetIndex];
    if (!targetMsg) return;

    const deleteCount = messages.length - (targetMsg.role === 'assistant' ? targetIndex : targetIndex + 1);
    const msgsToKeep = messages.slice(0, messages.length - deleteCount);

    // Backend deletes
    if (currentSessionId) {
      const msgsToDelete = messages.slice(messages.length - deleteCount);
      for (const m of msgsToDelete) {
        if (m.id) {
          try {
            await fetch(`/api/chat/sessions/${currentSessionId}/messages/${m.id}`, {
              method: 'DELETE',
              headers: getAuthHeaders(),
            });
          } catch (e) { }
        }
      }
    }

    setMessages(msgsToKeep);
    setChatLoading(true);
    abortControllerRef.current = new AbortController();

    try {
      const messagesPayload = [
        { role: 'system', content: openaiChatSystemPrompt },
        ...msgsToKeep.map(m => ({ role: m.role, content: typeof m.content === 'string' ? m.content : JSON.stringify(m.content) }))
      ];

      const headers = { ...getAuthHeaders(), 'Content-Type': 'application/json' };
      if (chatEndpoint) {
        headers['x-endpoint-id'] = chatEndpoint;
      }

      const response = await fetch('/api/openai/v1/chat/completions', {
        method: 'POST',
        headers,
        body: JSON.stringify({
          model: chatModel,
          messages: messagesPayload,
          stream: true,
          ...openaiChatSettings,
        }),
        signal: abortControllerRef.current?.signal,
      });

      if (!response.ok) throw new Error(`HTTP ${response.status}`);

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let assistantMsg = {
        role: 'assistant',
        content: '',
        reasoning: '',
        showReasoning: true,
        timestamp: new Date().toISOString(),
        model: chatModel,
        isNew: true
      };

      setMessages(prev => [...prev, assistantMsg]);

      let buffer = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
          const trimmed = line.trim();
          if (trimmed.startsWith('data: ')) {
            const dataStr = trimmed.slice(6);
            if (dataStr === '[DONE]') break;
            try {
              const parsed = JSON.parse(dataStr);
              const delta = parsed.choices?.[0]?.delta;
              if (delta) {
                if (delta.reasoning_content) {
                  assistantMsg.reasoning += delta.reasoning_content;
                }
                if (delta.content) {
                  assistantMsg.content += delta.content;
                }
                setMessages(prev => prev.map((m, idx) => idx === prev.length - 1 ? { ...assistantMsg } : m));
              }
            } catch (e) { }
          }
        }
      }

      const saved = await saveChatMessage(currentSessionId, 'assistant', assistantMsg.content, assistantMsg.reasoning || null);
      if (saved && saved.id) {
        setMessages(prev => prev.map((m, idx) => idx === prev.length - 1 ? { ...m, id: saved.id } : m));
      }
    } catch (error) {
      if (error.name === 'AbortError') return;
      toast.error('重新生成失败: ' + error.message);
    } finally {
      setChatLoading(false);
      abortControllerRef.current = null;
    }
  };

  const clearChatLocal = async () => {
    if (currentSessionId) {
      try {
        await fetch(`/api/chat/sessions/${currentSessionId}/messages`, {
          method: 'DELETE',
          headers: getAuthHeaders(),
        });
        setMessages([]);
        toast.success('已清空当前对话消息');
      } catch (error) {
        toast.error('清空消息失败');
      }
    } else {
      setMessages([]);
    }
  };

  // Image Upload handler
  const fileInputRef = useRef(null);
  const handleFileChange = (e) => {
    const files = Array.from(e.target.files);
    files.forEach(file => {
      if (!file.type.startsWith('image/')) return;
      const reader = new FileReader();
      reader.onload = (event) => {
        setAttachments(prev => [...prev, { file, url: event.target.result }]);
      };
      reader.readAsDataURL(file);
    });
  };

  const removeAttachment = (idx) => {
    setAttachments(prev => prev.filter((_, i) => i !== idx));
  };

  // Paste handler for images
  const handlePaste = (e) => {
    const items = e.clipboardData?.items;
    if (!items) return;
    for (const item of items) {
      if (item.type.startsWith('image/')) {
        const file = item.getAsFile();
        const reader = new FileReader();
        reader.onload = (event) => {
          setAttachments(prev => [...prev, { file, url: event.target.result }]);
        };
        reader.readAsDataURL(file);
      }
    }
  };

  // Model Selector Filtering
  const filteredModelsList = useMemo(() => {
    const list = allModels.filter(m => {
      const matchesSearch = m.id.toLowerCase().includes(openaiModelSearch.toLowerCase());
      const matchesEndpoint = !openaiSelectedEndpointId || m.owned_by === endpoints.find(e => e.id === openaiSelectedEndpointId)?.name;
      const matchesHidden = openaiShowHiddenModels ? true : !hiddenModels.includes(m.id);
      return matchesSearch && matchesEndpoint && matchesHidden;
    });
    return list;
  }, [allModels, openaiModelSearch, openaiSelectedEndpointId, endpoints, hiddenModels, openaiShowHiddenModels]);

  const chatDropdownFilteredModels = useMemo(() => {
    const allModelsMap = new Map();
    // Gather all models
    allModels.forEach(m => allModelsMap.set(m.id, m));
    // Complement with models from enabled endpoints
    endpoints.forEach(ep => {
      if (ep.models) {
        ep.models.forEach(m => {
          const id = typeof m === 'string' ? m : m.id;
          if (id && !allModelsMap.has(id)) {
            allModelsMap.set(id, { id, owned_by: ep.name || 'custom' });
          }
        });
      }
    });

    const fullList = Array.from(allModelsMap.values());
    return fullList.filter(m => {
      const matchesSearch = m.id.toLowerCase().includes(dropdownModelSearch.toLowerCase());
      // Filter by active endpoint
      const matchesEndpoint = !chatEndpoint || m.owned_by === endpoints.find(e => e.id === chatEndpoint)?.name;
      const isHidden = hiddenModels.includes(m.id);
      return matchesSearch && matchesEndpoint && !isHidden;
    });
  }, [allModels, endpoints, chatEndpoint, dropdownModelSearch, hiddenModels]);

  const selectChatModel = (modelId) => {
    setChatModel(modelId);
    localStorage.setItem('openai_chat_model', modelId);
    setShowModelDropdown(false);
  };

  const selectEndpoint = (epId) => {
    setChatEndpoint(epId);
    if (epId) {
      localStorage.setItem('openai_chat_endpoint', epId);
    } else {
      localStorage.removeItem('openai_chat_endpoint');
    }
    setShowEndpointDropdown(false);
  };

  // Auto-resize chat textarea
  const textareaRef = useRef(null);
  const handleTextareaInput = () => {
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
      textareaRef.current.style.height = `${Math.min(200, textareaRef.current.scrollHeight)}px`;
    }
  };

  return (
    <div className="space-y-6 flex flex-col h-full min-h-[75vh]">
      {/* Tab Navigation */}
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center border-b border-kumo-line pb-4 gap-4 select-none">
        <div className="flex border border-kumo-line rounded-lg p-0.5 bg-kumo-recessed">
          <button
            onClick={() => setActiveTab('endpoints')}
            className={`flex items-center gap-2 px-4 py-1.5 rounded-md text-xs font-semibold cursor-pointer transition-colors ${
              activeTab === 'endpoints'
                ? 'bg-kumo-base text-kumo-strong shadow-sm'
                : 'text-kumo-subtle hover:text-kumo-strong'
            }`}
          >
            <Server className="w-3.5 h-3.5" />
            <span>API 端点</span>
          </button>
          <button
            onClick={() => setActiveTab('accounts')}
            className={`flex items-center gap-2 px-4 py-1.5 rounded-md text-xs font-semibold cursor-pointer transition-colors ${
              activeTab === 'accounts'
                ? 'bg-kumo-base text-kumo-strong shadow-sm'
                : 'text-kumo-subtle hover:text-kumo-strong'
            }`}
          >
            <Users className="w-3.5 h-3.5" />
            <span>账号管理</span>
          </button>
          <button
            onClick={() => setActiveTab('chat')}
            className={`flex items-center gap-2 px-4 py-1.5 rounded-md text-xs font-semibold cursor-pointer transition-colors ${
              activeTab === 'chat'
                ? 'bg-kumo-base text-kumo-strong shadow-sm'
                : 'text-kumo-subtle hover:text-kumo-strong'
            }`}
          >
            <MessageSquare className="w-3.5 h-3.5" />
            <span>对话</span>
          </button>
        </div>
      </div>

      {/* ==================== 1. API 端点 Tab ==================== */}
      {activeTab === 'endpoints' && (
        <div className="quick-fade-in space-y-4">
          <div className="flex justify-between items-center bg-kumo-base p-4 border border-kumo-line rounded-lg shadow-sm">
            <div className="flex items-center gap-2">
              <span className="text-xs font-semibold text-kumo-subtle">
                {modelHealthBatchLoading ? '正在批量检测模型可用性...' : `共 ${endpoints.length} 个端点`}
              </span>
            </div>
            <div className="flex gap-2">
              <Button onClick={() => setHealthCheckModal(true)} className="flex items-center gap-1.5">
                <Activity className="w-3.5 h-3.5" />
                <span>健康检测</span>
              </Button>
              <Button onClick={refreshAllEndpoints} disabled={endpointsRefreshing} className="flex items-center gap-1.5">
                <RefreshCw className={`w-3.5 h-3.5 ${endpointsRefreshing ? 'animate-spin' : ''}`} />
                <span>刷新列表</span>
              </Button>
            </div>
          </div>

          <div className="space-y-3">
            {endpointsLoading ? (
              <div className="text-center py-12 text-kumo-subtle">
                <RotateCw className="w-8 h-8 animate-spin mx-auto mb-3" />
                <span>加载中...</span>
              </div>
            ) : endpoints.length === 0 ? (
              <div className="text-center py-12 bg-kumo-base border border-kumo-line rounded-lg text-kumo-subtle">
                <Bot className="w-12 h-12 mx-auto mb-3 opacity-30" />
                <p>暂无 API 端点，可在账号管理中添加</p>
              </div>
            ) : (
              endpoints.map((endpoint) => {
                const isExpanded = !!expandedEndpoints[endpoint.id];
                const validStatus = endpoint.status === 'valid';
                const invalidStatus = endpoint.status === 'invalid';

                return (
                  <div key={endpoint.id} className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
                    {/* Header */}
                    <div
                      onClick={() => toggleEndpointExpand(endpoint.id)}
                      className="flex items-center justify-between p-4 cursor-pointer hover:bg-kumo-recessed/10 transition-colors"
                    >
                      <div className="flex items-center gap-3">
                        <ChevronDown
                          className={`w-4 h-4 text-kumo-subtle transition-transform duration-200 ${
                            isExpanded ? 'transform rotate-180' : ''
                          }`}
                        />
                        <div
                          className="w-10 h-10 rounded-lg flex items-center justify-center font-bold text-white text-sm"
                          style={{ background: 'linear-gradient(135deg, #10a37f, #059669)' }}
                        >
                          {(endpoint.name || 'A').charAt(0).toUpperCase()}
                        </div>
                        <div>
                          <div className="flex items-center gap-2">
                            <span
                              className={`px-1.5 py-0.5 rounded text-[10px] font-bold border ${
                                validStatus
                                  ? 'bg-kumo-success/10 text-kumo-success border-kumo-success/20'
                                  : invalidStatus
                                  ? 'bg-kumo-danger/10 text-kumo-danger border-kumo-danger/20'
                                  : 'bg-kumo-recessed text-kumo-subtle border-kumo-line'
                              }`}
                            >
                              {validStatus ? '有效' : invalidStatus ? '无效' : '未验证'}
                            </span>
                            <span className="font-semibold text-kumo-strong text-xs">
                              {endpoint.name || '未命名端点'}
                            </span>
                          </div>
                          <span className="text-[10px] text-kumo-subtle font-mono block mt-0.5">
                            {maskAddress(endpoint.baseUrl)}
                          </span>
                        </div>
                      </div>

                      <div className="flex items-center gap-3">
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            openHealthCheckForEndpoint(endpoint.id);
                          }}
                          className="p-1.5 hover:bg-kumo-recessed rounded text-kumo-subtle hover:text-kumo-strong"
                          title="模型健康检测"
                        >
                          <Activity className="w-4 h-4" />
                        </button>
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            refreshEndpointModels(endpoint);
                          }}
                          className="p-1.5 hover:bg-kumo-recessed rounded text-kumo-subtle hover:text-kumo-strong"
                          title="刷新模型列表"
                        >
                          <RefreshCw className={`w-4 h-4 ${endpoint.refreshing ? 'animate-spin' : ''}`} />
                        </button>
                        <span className="text-[11px] bg-kumo-recessed border border-kumo-line px-2 py-0.5 rounded text-kumo-strong font-semibold select-none">
                          模型: {endpoint.models ? endpoint.models.length : 0}
                        </span>
                      </div>
                    </div>

                    {/* Expandable Model Tags */}
                    {isExpanded && (
                      <div className="border-t border-kumo-line bg-kumo-recessed/10 p-4">
                        {endpoint.models && endpoint.models.length > 0 ? (
                          <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-2">
                            {endpoint.models.map((model) => {
                              const modelId = typeof model === 'string' ? model.trim() : (model.id || '').trim();
                              const health = openaiModelHealth[modelId];

                              return (
                                <div
                                  key={modelId}
                                  className="flex items-center justify-between p-2 bg-kumo-base border border-kumo-line rounded-md text-xs group"
                                >
                                  <div className="flex items-center gap-2 min-w-0">
                                    {/* Health indicator dot */}
                                    <div
                                      onClick={() => testModelHealth({ id: modelId }, endpoint.id)}
                                      className={`w-2 h-2 rounded-full cursor-pointer ${
                                        health?.loading
                                          ? 'bg-kumo-brand animate-pulse'
                                          : health?.status === 'healthy'
                                          ? 'bg-kumo-success'
                                          : health?.status === 'error'
                                          ? 'bg-kumo-danger'
                                          : 'bg-kumo-subtle'
                                      }`}
                                      title={
                                        health
                                          ? `检测时间: ${formatDateTime(health.checkedAt)}\n延迟: ${health.latency || '-'}ms`
                                          : '点击测试模型健康'
                                      }
                                    />
                                    <span
                                      onClick={() => {
                                        selectEndpoint(endpoint.id);
                                        selectChatModel(modelId);
                                        setActiveTab('chat');
                                      }}
                                      className="font-mono text-[11px] text-kumo-strong hover:text-kumo-brand cursor-pointer truncate"
                                      title="点击进入对话"
                                    >
                                      {modelId}
                                    </span>
                                  </div>
                                  <button
                                    onClick={() => {
                                      navigator.clipboard.writeText(modelId);
                                      toast.success('已复制模型名称');
                                    }}
                                    className="p-1 hover:bg-kumo-recessed rounded text-kumo-subtle hover:text-kumo-strong opacity-0 group-hover:opacity-100 transition-opacity"
                                    title="复制名称"
                                  >
                                    <Copy className="w-3.5 h-3.5" />
                                  </button>
                                </div>
                              );
                            })}
                          </div>
                        ) : (
                          <div className="text-center py-6 text-kumo-subtle text-xs">
                            暂无模型数据，可在账号管理中刷新获取
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                );
              })
            )}
          </div>
        </div>
      )}

      {/* ==================== 2. 账号管理 Tab ==================== */}
      {activeTab === 'accounts' && (
        <div className="quick-fade-in space-y-6">
          {/* Toolbar */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-4 flex flex-col md:flex-row justify-between items-start md:items-center gap-4">
            <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
              <Server className="w-4 h-4 text-kumo-brand" />
              API 端点管理
            </h3>
            <div className="flex flex-wrap gap-2">
              <Button onClick={() => setHealthCheckModal(true)} className="flex items-center gap-1">
                <Activity className="w-3.5 h-3.5" />
                <span>健康检测</span>
              </Button>
              <Button variant="primary" onClick={openAddEndpointModal} className="flex items-center gap-1">
                <Plus className="w-3.5 h-3.5" />
                <span>添加账号</span>
              </Button>
              <Button onClick={refreshAllEndpoints} disabled={endpointsRefreshing} className="flex items-center gap-1">
                <RefreshCw className={`w-3.5 h-3.5 ${endpointsRefreshing ? 'animate-spin' : ''}`} />
                <span>刷新全部</span>
              </Button>
              <Button onClick={exportEndpoints} className="flex items-center gap-1">
                <Upload className="w-3.5 h-3.5" />
                <span>导出</span>
              </Button>
              <Button onClick={importEndpoints} className="flex items-center gap-1">
                <Download className="w-3.5 h-3.5" />
                <span>导入</span>
              </Button>
            </div>
          </div>

          {/* Table */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-x-auto">
            <table className="w-full border-collapse text-left text-xs">
              <thead>
                <tr className="border-b border-kumo-line bg-kumo-recessed/20">
                  <th className="p-3 font-semibold text-kumo-strong">名称</th>
                  <th className="p-3 font-semibold text-kumo-strong">API 地址</th>
                  <th className="p-3 font-semibold text-kumo-strong">API Key</th>
                  <th className="p-3 font-semibold text-kumo-strong text-center">状态</th>
                  <th className="p-3 font-semibold text-kumo-strong text-center">启用</th>
                  <th className="p-3 font-semibold text-kumo-strong text-center">模型数量</th>
                  <th className="p-3 font-semibold text-kumo-strong text-center">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-kumo-line">
                {endpoints.length === 0 ? (
                  <tr>
                    <td colSpan={7} className="p-8 text-center text-kumo-subtle">
                      暂无 API 账号，点击上方按钮添加
                    </td>
                  </tr>
                ) : (
                  endpoints.map((endpoint) => (
                    <tr key={endpoint.id} className="hover:bg-kumo-recessed/5">
                      <td className="p-3 font-bold text-kumo-strong">{endpoint.name || '未命名'}</td>
                      <td className="p-3 font-mono">{maskAddress(endpoint.baseUrl)}</td>
                      <td className="p-3">
                        <div className="flex items-center gap-1.5 font-mono">
                          <span>{endpoint.showKey ? endpoint.apiKey : maskApiKey(endpoint.apiKey)}</span>
                          <button
                            onClick={() =>
                              setEndpoints(prev =>
                                prev.map(e => e.id === endpoint.id ? { ...e, showKey: !e.showKey } : e)
                              )
                            }
                            className="p-1 hover:bg-kumo-recessed rounded text-kumo-subtle"
                          >
                            {endpoint.showKey ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
                          </button>
                        </div>
                      </td>
                      <td className="p-3 text-center">
                        <span
                          className={`px-2 py-0.5 rounded text-[10px] font-bold border ${
                            endpoint.status === 'valid'
                              ? 'bg-kumo-success/10 text-kumo-success border-kumo-success/20'
                              : endpoint.status === 'invalid'
                              ? 'bg-kumo-danger/10 text-kumo-danger border-kumo-danger/20'
                              : 'bg-kumo-recessed text-kumo-subtle border-kumo-line'
                          }`}
                        >
                          {endpoint.status === 'valid' ? '有效' : endpoint.status === 'invalid' ? '无效' : '未验证'}
                        </span>
                      </td>
                      <td className="p-3 text-center">
                        <input
                          type="checkbox"
                          checked={!!endpoint.enabled}
                          onChange={() => toggleEndpointEnabled(endpoint)}
                          className="w-8 h-4 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-3 before:h-3 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
                        />
                      </td>
                      <td className="p-3 text-center text-kumo-strong font-semibold">
                        {endpoint.models ? endpoint.models.length : 0}
                      </td>
                      <td className="p-3">
                        <div className="flex justify-center gap-1.5">
                          <button
                            onClick={() => verifyEndpoint(endpoint)}
                            className="p-1.5 hover:bg-kumo-brand/10 hover:text-kumo-brand rounded text-kumo-subtle"
                            title="验证连接"
                          >
                            <Plug className="w-3.5 h-3.5" />
                          </button>
                          <button
                            onClick={() => openHealthCheckForEndpoint(endpoint.id)}
                            className="p-1.5 hover:bg-kumo-brand/10 hover:text-kumo-brand rounded text-kumo-subtle"
                            title="健康检测"
                          >
                            <Activity className="w-3.5 h-3.5" />
                          </button>
                          <button
                            onClick={() => openEditEndpointModal(endpoint)}
                            className="p-1.5 hover:bg-kumo-brand/10 hover:text-kumo-brand rounded text-kumo-subtle"
                            title="编辑配置"
                          >
                            <Edit className="w-3.5 h-3.5" />
                          </button>
                          <button
                            onClick={() => deleteEndpoint(endpoint)}
                            className="p-1.5 hover:bg-kumo-danger/10 hover:text-kumo-danger rounded text-kumo-subtle"
                            title="删除账号"
                          >
                            <Trash className="w-3.5 h-3.5" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>

          {/* Batch add panel */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-5 space-y-4">
            <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-2">
              <Plus className="w-4 h-4 text-kumo-brand" />
              批量添加端点
            </h4>
            <p className="text-[11px] text-kumo-subtle leading-relaxed">
              每行一个端点，格式：<code>名称:API地址:API_Key</code>
            </p>
            <textarea
              value={batchText}
              onChange={(e) => setBatchText(e.target.value)}
              placeholder="每行一个：名称:https://api.example.com:sk-xxx"
              rows={4}
              className="w-full bg-kumo-recessed text-kumo-strong text-xs font-mono p-3 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand resize-none"
            />
            {batchError && <p className="text-xs text-kumo-danger font-semibold">{batchError}</p>}
            {batchSuccess && <p className="text-xs text-kumo-success font-semibold">{batchSuccess}</p>}
            <Button
              variant="primary"
              disabled={batchAdding || !batchText.trim()}
              onClick={batchAddEndpoints}
              className="w-full font-semibold"
            >
              {batchAdding ? '添加中...' : '批量添加'}
            </Button>
          </div>
        </div>
      )}

      {/* ==================== 3. 对话 (HChat) Tab ==================== */}
      {activeTab === 'chat' && (
        <div className="quick-fade-in flex flex-1 h-full min-h-[55vh] border border-kumo-line rounded-lg overflow-hidden bg-kumo-base shadow-sm relative">
          {/* Sidebar drawer mask for mobile */}
          {mobileSidebarOpen && (
            <div
              onClick={() => setMobileSidebarOpen(false)}
              className="absolute inset-0 bg-black/40 z-20 md:hidden"
            />
          )}

          {/* Chat history sidebar */}
          <div
            className={`w-[260px] flex-shrink-0 border-r border-kumo-line bg-kumo-recessed/40 flex flex-col transition-all duration-200 z-30 absolute md:relative inset-y-0 left-0 transform md:transform-none ${
              mobileSidebarOpen ? 'translate-x-0' : '-translate-x-full md:translate-x-0'
            } ${chatHistoryCollapsed ? 'md:w-0 md:overflow-hidden md:border-r-0' : 'md:w-[260px]'}`}
          >
            <div className="p-3 border-b border-kumo-line flex items-center justify-between">
              <span className="text-xs font-bold text-kumo-strong">
                {selectedSessionIds.length > 0 ? `已选 ${selectedSessionIds.length} 个` : '历史记录'}
              </span>
              <div className="flex gap-1.5">
                {sessions.length > 0 && (
                  <button
                    onClick={toggleSelectAllSessions}
                    className="p-1 hover:bg-kumo-recessed rounded text-kumo-subtle hover:text-kumo-strong"
                    title={selectedSessionIds.length === sessions.length ? '取消全选' : '全选'}
                  >
                    <Check className={`w-3.5 h-3.5 ${selectedSessionIds.length === sessions.length ? 'text-kumo-brand' : ''}`} />
                  </button>
                )}
                {selectedSessionIds.length > 0 ? (
                  <button
                    onClick={deleteSelectedSessions}
                    className="p-1 hover:bg-kumo-danger/10 rounded text-kumo-danger"
                    title="删除选中"
                  >
                    <Trash className="w-3.5 h-3.5" />
                  </button>
                ) : (
                  sessions.length > 0 && (
                    <button
                      onClick={clearAllSessions}
                      className="p-1 hover:bg-kumo-danger/10 rounded text-kumo-subtle hover:text-kumo-danger"
                      title="清空历史"
                    >
                      <Trash className="w-3.5 h-3.5" />
                    </button>
                  )
                )}
              </div>
            </div>

            <button
              onClick={() => createSession(true)}
              className="m-3 py-2 px-4 border border-dashed border-kumo-brand/40 hover:border-kumo-brand hover:bg-kumo-brand/5 text-kumo-brand font-semibold text-xs rounded-lg flex items-center justify-center gap-1.5 transition-all cursor-pointer"
            >
              <Plus className="w-4 h-4" />
              <span>新对话</span>
            </button>

            <div className="flex-1 overflow-y-auto divide-y divide-kumo-line/50 p-2 space-y-1">
              {chatHistoryLoading && sessions.length === 0 ? (
                <div className="text-center py-8 text-kumo-subtle">
                  <RotateCw className="w-5 h-5 animate-spin mx-auto" />
                </div>
              ) : sessions.length === 0 ? (
                <div className="text-center py-12 text-kumo-subtle text-xs">暂无会话历史</div>
              ) : (
                sessions.map((session) => (
                  <div
                    key={session.id}
                    onClick={() => loadSession(session.id)}
                    className={`flex items-center justify-between p-2 rounded-md text-xs cursor-pointer group transition-colors ${
                      session.id === currentSessionId
                        ? 'bg-kumo-brand/10 text-kumo-strong'
                        : 'text-kumo-subtle hover:bg-kumo-recessed/50 hover:text-kumo-strong'
                    }`}
                  >
                    <div className="flex items-center gap-2 min-w-0 flex-1">
                      <MessageSquare className="w-3.5 h-3.5 flex-shrink-0" />
                      <span className="truncate pr-2">{session.title || '新对话'}</span>
                    </div>

                    <div className="flex items-center gap-1">
                      <button
                        onClick={(e) => toggleSessionSelection(session.id, e)}
                        className="p-0.5 hover:bg-kumo-recessed rounded text-kumo-subtle opacity-0 group-hover:opacity-100 md:opacity-100"
                      >
                        <Check
                          className={`w-3.5 h-3.5 ${
                            selectedSessionIds.includes(session.id) ? 'text-kumo-brand' : 'opacity-30'
                          }`}
                        />
                      </button>
                      <button
                        onClick={(e) => deleteSession(session.id, e)}
                        className="p-0.5 hover:bg-kumo-danger/10 rounded text-kumo-subtle hover:text-kumo-danger opacity-0 group-hover:opacity-100"
                      >
                        <Trash className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>

          {/* Main Chat Area */}
          <div className="flex-1 flex flex-col h-full bg-kumo-base min-w-0">
            {/* Chat Toolbar */}
            <div className="p-3 border-b border-kumo-line flex flex-wrap items-center justify-between gap-3 bg-kumo-recessed/10">
              <div className="flex flex-wrap items-center gap-2">
                {/* Mobile history toggle */}
                <Button
                  onClick={() => setMobileSidebarOpen(true)}
                  className="md:hidden p-1.5"
                  title="历史记录"
                >
                  <History className="w-4 h-4" />
                </Button>

                {/* Persona selector */}
                <div className="relative">
                  <div
                    onClick={(e) => {
                      e.stopPropagation();
                      setShowPersonaDropdown(!showPersonaDropdown);
                      setShowEndpointDropdown(false);
                      setShowModelDropdown(false);
                    }}
                    className="flex items-center gap-1.5 px-3 py-1.5 border border-kumo-line rounded-lg text-xs cursor-pointer bg-kumo-base hover:bg-kumo-recessed/50 text-kumo-strong font-semibold select-none"
                  >
                    <Bot className="w-3.5 h-3.5 text-kumo-brand" />
                    <span>
                      {personas.find((p) => p.id === currentPersonaId)?.name || '默认人设'}
                    </span>
                    <ChevronDown className="w-3.5 h-3.5 text-kumo-subtle" />
                  </div>

                  {showPersonaDropdown && (
                    <div
                      onClick={(e) => e.stopPropagation()}
                      className="absolute left-0 mt-1.5 w-56 bg-kumo-base border border-kumo-line rounded-lg shadow-lg py-1 z-30 text-xs"
                    >
                      <div className="max-h-60 overflow-y-auto">
                        {personas.map((persona) => (
                          <div
                            key={persona.id}
                            onClick={() => handleSelectPersona(persona.id)}
                            className={`flex items-center justify-between px-3 py-2 cursor-pointer hover:bg-kumo-recessed/50 ${
                              currentPersonaId === persona.id ? 'text-kumo-brand font-bold bg-kumo-brand/5' : 'text-kumo-strong'
                            }`}
                          >
                            <span>{persona.name}</span>
                            <div className="flex gap-1.5 opacity-0 group-hover:opacity-100">
                              <Edit
                                onClick={(e) => {
                                  e.stopPropagation();
                                  openPersonaModal(persona);
                                  setShowPersonaDropdown(false);
                                }}
                                className="w-3 h-3 text-kumo-subtle hover:text-kumo-strong"
                              />
                              {!persona.is_default && (
                                <Trash
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    deletePersona(persona.id);
                                    setShowPersonaDropdown(false);
                                  }}
                                  className="w-3 h-3 text-kumo-danger"
                                />
                              )}
                            </div>
                          </div>
                        ))}
                      </div>
                      <div className="border-t border-kumo-line my-1" />
                      <div
                        onClick={() => {
                          openPersonaModal();
                          setShowPersonaDropdown(false);
                        }}
                        className="px-3 py-2 cursor-pointer hover:bg-kumo-recessed/50 text-kumo-brand font-semibold flex items-center gap-1.5"
                      >
                        <Plus className="w-3.5 h-3.5" />
                        <span>新增人设</span>
                      </div>
                    </div>
                  )}
                </div>

                {/* Endpoint selector */}
                <div className="relative">
                  <div
                    onClick={(e) => {
                      e.stopPropagation();
                      setShowEndpointDropdown(!showEndpointDropdown);
                      setShowPersonaDropdown(false);
                      setShowModelDropdown(false);
                    }}
                    className="flex items-center gap-1.5 px-3 py-1.5 border border-kumo-line rounded-lg text-xs cursor-pointer bg-kumo-base hover:bg-kumo-recessed/50 text-kumo-strong font-semibold select-none"
                  >
                    <Server className="w-3.5 h-3.5" />
                    <span>
                      {endpoints.find((ep) => ep.id === chatEndpoint)?.name || '所有端点'}
                    </span>
                    <ChevronDown className="w-3.5 h-3.5 text-kumo-subtle" />
                  </div>

                  {showEndpointDropdown && (
                    <div
                      onClick={(e) => e.stopPropagation()}
                      className="absolute left-0 mt-1.5 w-56 bg-kumo-base border border-kumo-line rounded-lg shadow-lg py-1 z-30 text-xs"
                    >
                      <div
                        onClick={() => selectEndpoint('')}
                        className={`px-3 py-2 cursor-pointer hover:bg-kumo-recessed/50 ${
                          !chatEndpoint ? 'text-kumo-brand font-bold bg-kumo-brand/5' : 'text-kumo-strong'
                        }`}
                      >
                        所有端点
                      </div>
                      <div className="max-h-48 overflow-y-auto">
                        {endpoints.map((ep) => (
                          <div
                            key={ep.id}
                            onClick={() => selectEndpoint(ep.id)}
                            className={`px-3 py-2 cursor-pointer hover:bg-kumo-recessed/50 ${
                              chatEndpoint === ep.id ? 'text-kumo-brand font-bold bg-kumo-brand/5' : 'text-kumo-strong'
                            }`}
                          >
                            {ep.name}
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </div>

                {/* Model Selector Dropdown with search */}
                <div className="relative">
                  <div
                    onClick={(e) => {
                      e.stopPropagation();
                      setShowModelDropdown(!showModelDropdown);
                      setShowPersonaDropdown(false);
                      setShowEndpointDropdown(false);
                      setDropdownModelSearch('');
                    }}
                    className="flex items-center gap-1.5 px-3 py-1.5 border border-kumo-line rounded-lg text-xs cursor-pointer bg-kumo-base hover:bg-kumo-recessed/50 text-kumo-strong font-semibold select-none max-w-[200px]"
                  >
                    <Bot className="w-3.5 h-3.5 text-kumo-brand" />
                    <span className="truncate">{chatModel || '选择模型'}</span>
                    <ChevronDown className="w-3.5 h-3.5 text-kumo-subtle" />
                  </div>

                  {showModelDropdown && (
                    <div
                      onClick={(e) => e.stopPropagation()}
                      className="absolute left-0 mt-1.5 w-64 bg-kumo-base border border-kumo-line rounded-lg shadow-lg py-1 z-30 text-xs"
                    >
                      <div className="p-2 border-b border-kumo-line flex items-center gap-1 bg-kumo-recessed/35">
                        <Search className="w-3.5 h-3.5 text-kumo-subtle" />
                        <input
                          type="text"
                          value={dropdownModelSearch}
                          onChange={(e) => setDropdownModelSearch(e.target.value)}
                          placeholder="搜索模型..."
                          className="w-full bg-transparent border-0 focus:outline-none text-xs"
                        />
                      </div>
                      <div className="max-h-60 overflow-y-auto py-1">
                        {/* Pinned Models group */}
                        {pinnedModels.length > 0 && !dropdownModelSearch && (
                          <>
                            <div className="px-3 py-1 text-[10px] font-bold text-kumo-brand flex items-center gap-1">
                              <Star className="w-3 h-3 fill-yellow-400 stroke-yellow-400" />
                              <span>已收藏</span>
                            </div>
                            {pinnedModels
                              .filter((id) => chatDropdownFilteredModels.some((m) => m.id === id))
                              .map((id) => (
                                <div
                                  key={`pinned-${id}`}
                                  onClick={() => selectChatModel(id)}
                                  className={`px-3 py-1.5 cursor-pointer hover:bg-kumo-recessed/50 flex items-center justify-between ${
                                    chatModel === id ? 'text-kumo-brand font-bold bg-kumo-brand/5' : 'text-kumo-strong'
                                  }`}
                                >
                                  <span className="truncate">{id}</span>
                                  {chatModel === id && <Check className="w-3.5 h-3.5 text-kumo-brand" />}
                                </div>
                              ))}
                            <div className="border-t border-kumo-line my-1" />
                            <div className="px-3 py-1 text-[10px] font-bold text-kumo-subtle">所有模型</div>
                          </>
                        )}

                        {chatDropdownFilteredModels.length === 0 ? (
                          <div className="text-center py-4 text-kumo-subtle">无匹配模型</div>
                        ) : (
                          chatDropdownFilteredModels
                            .filter((m) => !(pinnedModels.includes(m.id) && !dropdownModelSearch))
                            .map((model) => (
                              <div
                                key={model.id}
                                onClick={() => selectChatModel(model.id)}
                                className={`px-3 py-1.5 cursor-pointer hover:bg-kumo-recessed/50 flex items-center justify-between ${
                                  chatModel === model.id ? 'text-kumo-brand font-bold bg-kumo-brand/5' : 'text-kumo-strong'
                                }`}
                              >
                                <span className="truncate">{model.id}</span>
                                {chatModel === model.id && <Check className="w-3.5 h-3.5 text-kumo-brand" />}
                              </div>
                            ))
                        )}
                      </div>
                    </div>
                  )}
                </div>

                {/* Operations */}
                <button
                  onClick={() => loadAllModels(false)}
                  disabled={chatLoading}
                  className="p-1.5 hover:bg-kumo-recessed rounded text-kumo-subtle hover:text-kumo-strong transition-colors"
                  title="刷新模型列表"
                >
                  <RefreshCw className={`w-4 h-4 ${chatLoading ? 'animate-spin' : ''}`} />
                </button>
                <button
                  onClick={() => togglePinModel(chatModel)}
                  disabled={!chatModel}
                  className={`p-1.5 hover:bg-kumo-recessed rounded transition-colors ${
                    pinnedModels.includes(chatModel) ? 'text-yellow-500' : 'text-kumo-subtle hover:text-kumo-strong'
                  }`}
                  title={pinnedModels.includes(chatModel) ? '取消收藏' : '收藏当前模型'}
                >
                  <Star className={`w-4 h-4 ${pinnedModels.includes(chatModel) ? 'fill-yellow-500' : ''}`} />
                </button>
                <button
                  onClick={handleSetDefaultModel}
                  disabled={!chatModel}
                  className={`p-1.5 hover:bg-kumo-recessed rounded transition-colors ${
                    chatModel === defaultChatModel ? 'text-kumo-brand' : 'text-kumo-subtle hover:text-kumo-strong'
                  }`}
                  title={chatModel === defaultChatModel ? '当前为默认模型' : '设为默认模型'}
                >
                  <Pin className="w-4 h-4" />
                </button>
              </div>

              <div className="flex gap-1.5">
                <Button onClick={clearChatLocal} className="text-xs font-semibold">
                  清除
                </Button>
                <Button onClick={() => setShowHChatSettingsModal(true)} className="text-xs font-semibold">
                  设置
                </Button>
              </div>
            </div>

            {/* Message Pane */}
            <div className="flex-1 overflow-y-auto p-4 space-y-4">
              {chatHistoryLoading ? (
                <div className="flex flex-col items-center justify-center h-48 text-kumo-subtle space-y-2">
                  <RotateCw className="w-8 h-8 animate-spin" />
                  <span>加载对话中...</span>
                </div>
              ) : messages.length === 0 ? (
                <div className="flex flex-col items-center justify-center h-full text-center py-20 text-kumo-subtle space-y-3">
                  <Bot className="w-12 h-12 opacity-30 text-kumo-brand" />
                  <h4 className="text-sm font-bold text-kumo-strong">OpenAI 兼容对话</h4>
                  <p className="text-xs max-w-sm">
                    选择一个模型并开始对话。本模块直接调用您所配置的 OpenAI API 兼容端点。
                  </p>
                </div>
              ) : (
                messages.map((msg, index) => {
                  const isUser = msg.role === 'user';
                  const showReasoning = !!msg.showReasoning;

                  return (
                    <div
                      key={index}
                      className={`flex gap-3 text-xs max-w-3xl ${isUser ? 'ml-auto flex-row-reverse' : 'mr-auto'}`}
                    >
                      {/* Avatar */}
                      <div
                        className={`w-8 h-8 rounded-full flex items-center justify-center flex-shrink-0 text-white font-bold select-none ${
                          isUser
                            ? 'bg-kumo-brand'
                            : 'bg-emerald-600'
                        }`}
                      >
                        {isUser ? 'U' : 'AI'}
                      </div>

                      {/* Content Bubble */}
                      <div className="min-w-0 flex-1 space-y-1">
                        <div className={`flex items-center gap-2 ${isUser ? 'justify-end' : ''}`}>
                          <span className="font-bold text-kumo-strong">
                            {isUser ? 'MOI' : '哈基喵'}
                          </span>
                          <span className="text-[10px] text-kumo-subtle">
                            {formatDateTime(msg.timestamp || new Date())}
                          </span>
                          {!isUser && msg.model && (
                            <span className="text-[10px] text-kumo-brand bg-kumo-brand/10 border border-kumo-brand/20 px-1.5 py-0.2 rounded font-mono">
                              {msg.model}
                            </span>
                          )}

                          {/* Message actions */}
                          <div className="flex items-center gap-1 ml-2 opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity">
                            <button
                              onClick={() => regenerateChat(index)}
                              className="p-1 hover:bg-kumo-recessed rounded text-kumo-subtle"
                              title="重新生成"
                            >
                              <RotateCw className="w-3 h-3" />
                            </button>
                            <button
                              onClick={() => deleteChatMessage(index)}
                              className="p-1 hover:bg-kumo-danger/10 rounded text-kumo-subtle hover:text-kumo-danger"
                              title="删除消息"
                            >
                              <Trash className="w-3 h-3" />
                            </button>
                          </div>
                        </div>

                        {/* User content (text + image preview) */}
                        {isUser && Array.isArray(msg.content) ? (
                          <div className="bg-kumo-brand/10 text-kumo-strong p-3 rounded-lg border border-kumo-brand/20 space-y-2 select-text">
                            {msg.content.map((chunk, cidx) => {
                              if (chunk.type === 'text') {
                                return <p key={cidx} className="whitespace-pre-wrap">{chunk.text}</p>;
                              }
                              if (chunk.type === 'image_url') {
                                return (
                                  <img
                                    key={cidx}
                                    src={chunk.image_url?.url}
                                    alt="Attached"
                                    className="max-h-40 rounded border border-kumo-line bg-kumo-recessed"
                                  />
                                );
                              }
                              return null;
                            })}
                          </div>
                        ) : isUser ? (
                          <div className="bg-kumo-brand/10 text-kumo-strong p-3 rounded-lg border border-kumo-brand/20 whitespace-pre-wrap select-text leading-relaxed">
                            {msg.content}
                          </div>
                        ) : (
                          <div className="bg-kumo-recessed/60 text-kumo-strong p-3 rounded-lg border border-kumo-line space-y-3 select-text leading-relaxed">
                            {/* Thinking/Reasoning Folding */}
                            {msg.reasoning && (
                              <div className="border border-kumo-line rounded bg-kumo-recessed/80">
                                <div
                                  onClick={() =>
                                    setMessages(prev =>
                                      prev.map((m, idx) =>
                                        idx === index ? { ...m, showReasoning: !m.showReasoning } : m
                                      )
                                    )
                                  }
                                  className="flex items-center gap-1.5 p-2 font-semibold text-kumo-brand bg-kumo-brand/5 border-b border-kumo-line cursor-pointer select-none text-[11px]"
                                >
                                  <Brain className="w-3.5 h-3.5 text-kumo-brand" />
                                  <span>思考过程</span>
                                  <ChevronDown className={`w-3.5 h-3.5 text-kumo-brand ml-auto transition-transform ${showReasoning ? 'transform rotate-180' : ''}`} />
                                </div>
                                {showReasoning && (
                                  <div
                                    className="p-3 font-mono text-[11px] leading-relaxed text-kumo-subtle border-t border-kumo-line max-h-48 overflow-y-auto whitespace-pre-wrap bg-kumo-recessed/30"
                                    dangerouslySetInnerHTML={{ __html: renderMarkdown(msg.reasoning) }}
                                  />
                                )}
                              </div>
                            )}

                            {/* Standard message output */}
                            <div
                              className="prose prose-sm dark:prose-invert max-w-none text-xs break-words"
                              dangerouslySetInnerHTML={{ __html: renderMarkdown(msg.content) }}
                            />
                          </div>
                        )}
                      </div>
                    </div>
                  );
                })
              )}

              {/* Bot typing loader */}
              {chatLoading && (
                <div className="flex gap-3 text-xs max-w-3xl mr-auto">
                  <div className="w-8 h-8 rounded-full flex items-center justify-center flex-shrink-0 bg-emerald-600 text-white font-bold select-none">
                    AI
                  </div>
                  <div className="bg-kumo-recessed/60 text-kumo-strong p-3 rounded-lg border border-kumo-line space-y-1">
                    <div className="flex items-center justify-between border-b border-kumo-line pb-1.5 mb-2 gap-8">
                      <span className="font-bold text-kumo-strong">哈基喵</span>
                      <button
                        onClick={stopGenerating}
                        className="p-1 hover:bg-kumo-danger/15 rounded text-kumo-danger flex items-center gap-1 font-semibold"
                        title="停止生成"
                      >
                        <X className="w-3 h-3" />
                        <span>停止</span>
                      </button>
                    </div>
                    <div className="flex space-x-1 py-1.5">
                      <div className="w-2 h-2 bg-kumo-brand rounded-full animate-bounce" style={{ animationDelay: '0ms' }} />
                      <div className="w-2 h-2 bg-kumo-brand rounded-full animate-bounce" style={{ animationDelay: '150ms' }} />
                      <div className="w-2 h-2 bg-kumo-brand rounded-full animate-bounce" style={{ animationDelay: '300ms' }} />
                    </div>
                  </div>
                </div>
              )}
              <div ref={messagesEndRef} />
            </div>

            {/* Input form */}
            <div className="p-4 border-t border-kumo-line bg-kumo-recessed/10 space-y-2">
              {/* Attachments preview list */}
              {attachments.length > 0 && (
                <div className="flex flex-wrap gap-2 pb-2">
                  {attachments.map((file, idx) => (
                    <div key={idx} className="relative w-16 h-16 rounded border border-kumo-line overflow-hidden bg-kumo-recessed">
                      <img src={file.url} alt="preview" className="w-full h-full object-cover" />
                      <button
                        onClick={() => removeAttachment(idx)}
                        className="absolute top-0.5 right-0.5 p-0.5 bg-black/70 rounded-full text-white hover:bg-black"
                      >
                        <X className="w-3 h-3" />
                      </button>
                    </div>
                  ))}
                </div>
              )}

              <div className="flex items-center gap-2">
                <input
                  type="file"
                  ref={fileInputRef}
                  onChange={handleFileChange}
                  accept="image/*"
                  multiple
                  className="hidden"
                />
                <button
                  onClick={() => fileInputRef.current?.click()}
                  className="p-2 hover:bg-kumo-recessed rounded text-kumo-subtle hover:text-kumo-strong"
                  title="上传图片"
                >
                  <Paperclip className="w-4 h-4" />
                </button>

                <textarea
                  ref={textareaRef}
                  value={messageInput}
                  onChange={(e) => setMessageInput(e.target.value)}
                  onInput={handleTextareaInput}
                  onPaste={handlePaste}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && !e.shiftKey) {
                      e.preventDefault();
                      handleSendChat();
                    }
                  }}
                  placeholder="输入消息，Enter发送，Shift+Enter换行..."
                  rows={1}
                  className="flex-1 bg-kumo-base text-kumo-strong text-xs p-2.5 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand resize-none font-sans max-h-36 overflow-y-auto"
                />

                <Button
                  variant="primary"
                  disabled={(!messageInput.trim() && attachments.length === 0) || chatLoading}
                  onClick={handleSendChat}
                  className="p-2.5 rounded-lg flex items-center justify-center"
                >
                  <Send className="w-4 h-4" />
                </Button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ==================== dialogs & modals ==================== */}

      {/* 1. Endpoint Add/Edit Dialog */}
      <Dialog.Root open={endpointFormOpen} onOpenChange={setEndpointFormOpen}>
        <Dialog className="p-6 sm:max-w-md bg-kumo-base border border-kumo-line rounded-lg shadow-lg">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            {editingEndpoint ? '编辑端点' : '添加 API 端点'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            配置 OpenAI 兼容的 API 端点以供中转或对话使用。
          </Dialog.Description>

          <div className="space-y-4">
            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">名称</label>
              <input
                type="text"
                value={endpointForm.name}
                onChange={(e) => setEndpointForm({ ...endpointForm, name: e.target.value })}
                placeholder="例如：DeepSeek 官方"
                className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand"
              />
            </div>

            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">API 接口地址 (Base URL)</label>
              <input
                type="text"
                value={endpointForm.baseUrl}
                onChange={(e) => setEndpointForm({ ...endpointForm, baseUrl: e.target.value })}
                placeholder="https://api.openai.com/v1"
                className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono"
              />
            </div>

            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">API Key</label>
              <input
                type="password"
                value={endpointForm.apiKey}
                onChange={(e) => setEndpointForm({ ...endpointForm, apiKey: e.target.value })}
                placeholder="sk-xxxxxxxxxxxxxxxxxxxxxxxx"
                className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono"
              />
            </div>

            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">备注</label>
              <input
                type="text"
                value={endpointForm.notes}
                onChange={(e) => setEndpointForm({ ...endpointForm, notes: e.target.value })}
                placeholder="选填"
                className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand"
              />
            </div>

            {endpointFormError && (
              <p className="text-xs text-kumo-danger font-semibold">{endpointFormError}</p>
            )}

            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close>
                <Button>取消</Button>
              </Dialog.Close>
              <Button variant="primary" disabled={endpointSaving} onClick={saveEndpoint}>
                {endpointSaving ? '保存中...' : '保存端点'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 2. Health Check Config Dialog */}
      <Dialog.Root open={healthCheckModal} onOpenChange={setHealthCheckModal}>
        <Dialog className="p-6 sm:max-w-md bg-kumo-base border border-kumo-line rounded-lg shadow-lg">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            模型健康检测
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            设置健康检测参数，批量发送轻量请求测试连接可用性与延迟。
          </Dialog.Description>

          <div className="space-y-4">
            <div className="bg-yellow-500/10 border border-yellow-500/20 text-yellow-600 dark:text-yellow-400 p-3 rounded text-xs space-y-1">
              <p className="font-bold flex items-center gap-1.5">
                <AlertTriangle className="w-3.5 h-3.5" />
                警告
              </p>
              <p>健康检测需要向 API 发送真实请求，按Token/次收费的模型可能会产生小额账单，请谨慎使用。</p>
            </div>

            <div className="flex items-center justify-between text-xs">
              <span className="font-semibold text-kumo-strong">使用密钥</span>
              <div className="flex border border-kumo-line rounded bg-kumo-recessed p-0.5">
                <button
                  onClick={() => setHealthCheckForm({ ...healthCheckForm, useKey: 'single' })}
                  className={`px-3 py-1 rounded text-[10px] font-semibold cursor-pointer ${
                    healthCheckForm.useKey === 'single'
                      ? 'bg-kumo-base text-kumo-strong shadow-sm'
                      : 'text-kumo-subtle'
                  }`}
                >
                  单个
                </button>
                <button
                  onClick={() => setHealthCheckForm({ ...healthCheckForm, useKey: 'all' })}
                  className={`px-3 py-1 rounded text-[10px] font-semibold cursor-pointer ${
                    healthCheckForm.useKey === 'all'
                      ? 'bg-kumo-base text-kumo-strong shadow-sm'
                      : 'text-kumo-subtle'
                  }`}
                >
                  所有
                </button>
              </div>
            </div>

            <div className="flex items-center justify-between text-xs">
              <span className="font-semibold text-kumo-strong">并发检测</span>
              <input
                type="checkbox"
                checked={healthCheckForm.concurrency}
                onChange={(e) => setHealthCheckForm({ ...healthCheckForm, concurrency: e.target.checked })}
                className="w-8 h-4 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-3 before:h-3 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
              />
            </div>

            <div className="flex items-center justify-between text-xs">
              <span className="font-semibold text-kumo-strong">超时限制</span>
              <div className="flex items-center gap-1.5">
                <input
                  type="number"
                  value={healthCheckForm.timeout}
                  onChange={(e) => setHealthCheckForm({ ...healthCheckForm, timeout: Number(e.target.value) })}
                  min={1}
                  max={60}
                  className="w-16 bg-kumo-recessed text-kumo-strong text-xs px-2 py-1 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand text-center"
                />
                <span className="text-kumo-subtle">秒</span>
              </div>
            </div>

            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close>
                <Button>取消</Button>
              </Dialog.Close>
              <Button variant="primary" onClick={startBatchHealthCheck}>
                开始检测
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 3. Persona Add/Edit Dialog */}
      <Dialog.Root open={personaModalOpen} onOpenChange={setPersonaModalOpen}>
        <Dialog className="p-6 sm:max-w-md bg-kumo-base border border-kumo-line rounded-lg shadow-lg">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            {editingPersona ? '编辑人设' : '新增人设'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            设置 AI 人设身份与系统 Prompt，定义特定的回复偏好。
          </Dialog.Description>

          <div className="space-y-4">
            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">人设名称</label>
              <input
                type="text"
                value={personaForm.name}
                onChange={(e) => setPersonaForm({ ...personaForm, name: e.target.value })}
                placeholder="例如：中英翻译官"
                className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand"
              />
            </div>

            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">系统提示词 (System Prompt)</label>
              <textarea
                value={personaForm.systemPrompt}
                onChange={(e) => setPersonaForm({ ...personaForm, systemPrompt: e.target.value })}
                placeholder="定义 AI 的行为指南或任务边界..."
                rows={5}
                className="w-full bg-kumo-recessed text-kumo-strong text-xs p-3 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand resize-none font-sans"
              />
            </div>

            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close>
                <Button>取消</Button>
              </Dialog.Close>
              <Button variant="primary" onClick={savePersona}>
                保存人设
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 4. Chat Settings Modal */}
      <Dialog.Root open={showHChatSettingsModal} onOpenChange={setShowHChatSettingsModal}>
        <Dialog className="p-0 sm:max-w-lg bg-kumo-base border border-kumo-line rounded-lg shadow-lg overflow-hidden flex flex-col max-h-[85vh]">
          {/* Header */}
          <div className="p-4 border-b border-kumo-line flex items-center justify-between">
            <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-1.5">
              <Settings className="w-4 h-4 text-kumo-brand animate-spin-slow" />
              对话参数设置
            </h3>
            <button
              onClick={() => setShowHChatSettingsModal(false)}
              className="p-1 hover:bg-kumo-recessed rounded text-kumo-subtle"
            >
              <X className="w-4 h-4" />
            </button>
          </div>

          {/* Setting Tabs Navigation */}
          <div className="flex border-b border-kumo-line bg-kumo-recessed/20 p-1 select-none">
            <button
              onClick={() => setOpenaiSettingsTab('general')}
              className={`flex-1 py-1.5 text-[11px] font-semibold rounded-md transition-colors cursor-pointer text-center ${
                openaiSettingsTab === 'general'
                  ? 'bg-kumo-base text-kumo-strong shadow-sm'
                  : 'text-kumo-subtle hover:text-kumo-strong'
              }`}
            >
              通用
            </button>
            <button
              onClick={() => setOpenaiSettingsTab('models')}
              className={`flex-1 py-1.5 text-[11px] font-semibold rounded-md transition-colors cursor-pointer text-center ${
                openaiSettingsTab === 'models'
                  ? 'bg-kumo-base text-kumo-strong shadow-sm'
                  : 'text-kumo-subtle hover:text-kumo-strong'
              }`}
            >
              模型管理
            </button>
          </div>

          {/* Modal Body */}
          <div className="flex-1 overflow-y-auto p-4 space-y-4 max-h-[60vh]">
            {openaiSettingsTab === 'general' ? (
              <div className="space-y-4">
                {/* System prompt */}
                <div className="space-y-1">
                  <label className="text-xs font-semibold text-kumo-strong">系统提示词 (System Prompt)</label>
                  <textarea
                    value={openaiChatSystemPrompt}
                    onChange={(e) => setOpenaiChatSystemPrompt(e.target.value)}
                    rows={3}
                    placeholder="你是一个有用的 AI 助手..."
                    className="w-full bg-kumo-recessed text-kumo-strong text-xs p-3 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand resize-none font-sans"
                  />
                </div>

                {/* Parameters */}
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-1">
                    <div className="flex justify-between items-center text-xs">
                      <label className="font-semibold text-kumo-strong">随机性 (Temperature)</label>
                      <span className="font-mono text-kumo-brand font-bold">{openaiChatSettings.temperature}</span>
                    </div>
                    <input
                      type="range"
                      min={0}
                      max={2}
                      step={0.1}
                      value={openaiChatSettings.temperature}
                      onChange={(e) =>
                        setOpenaiChatSettings({ ...openaiChatSettings, temperature: Number(e.target.value) })
                      }
                      className="w-full accent-kumo-brand cursor-pointer"
                    />
                  </div>

                  <div className="space-y-1">
                    <label className="text-xs font-semibold text-kumo-strong block">回复长度 (Max Tokens)</label>
                    <input
                      type="number"
                      value={openaiChatSettings.max_tokens}
                      onChange={(e) =>
                        setOpenaiChatSettings({ ...openaiChatSettings, max_tokens: Number(e.target.value) })
                      }
                      placeholder="e.g. 2000"
                      className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-1.5 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand text-center"
                    />
                  </div>
                </div>

                {/* Default model */}
                <div className="space-y-1.5 p-3 border border-kumo-line rounded-lg bg-kumo-recessed/30 flex justify-between items-center">
                  <div>
                    <label className="text-xs font-bold text-kumo-strong block">默认启动模型</label>
                    <span className="text-[10px] text-kumo-subtle">
                      {defaultChatModel ? defaultChatModel : '未设置，启动时自动选择'}
                    </span>
                  </div>
                  {defaultChatModel && (
                    <Button onClick={handleClearDefaultModel} className="p-1 text-kumo-danger border border-kumo-line">
                      清除
                    </Button>
                  )}
                </div>

                {/* Auto Title */}
                <div className="border border-kumo-line rounded-lg p-4 bg-kumo-recessed/25 space-y-3">
                  <div className="flex justify-between items-center text-xs">
                    <span className="font-bold text-kumo-strong">开启 AI 对话自动命名</span>
                    <input
                      type="checkbox"
                      checked={openaiAutoTitleEnabled}
                      onChange={(e) => {
                        setOpenaiAutoTitleEnabled(e.target.checked);
                        saveAutoTitleSettings(e.target.checked, openaiTitleModels);
                      }}
                      className="w-8 h-4 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-3 before:h-3 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
                    />
                  </div>

                  {openaiAutoTitleEnabled && (
                    <div className="space-y-2 text-xs">
                      <p className="text-[10px] text-kumo-subtle">
                        选择用于生成标题的模型列表（按顺序尝试进行容灾检测）
                      </p>

                      {/* Selected title models chips */}
                      <div className="flex flex-wrap gap-1.5">
                        {openaiTitleModels.map((m, idx) => (
                          <div
                            key={m}
                            className="flex items-center gap-1.5 px-2 py-0.5 bg-kumo-brand/10 text-kumo-brand border border-kumo-brand/20 rounded text-[10px] font-bold"
                          >
                            <span className="bg-kumo-brand text-white w-3.5 h-3.5 rounded-full flex items-center justify-center text-[8px]">
                              {idx + 1}
                            </span>
                            <span className="max-w-[120px] truncate">{m}</span>
                            <button onClick={() => removeTitleModel(m)} className="hover:text-kumo-danger">
                              <X className="w-3 h-3" />
                            </button>
                          </div>
                        ))}
                      </div>

                      {/* Add title model select */}
                      <div className="flex gap-2">
                        <select
                          value={openaiTitleModelToAdd}
                          onChange={(e) => setOpenaiTitleModelToAdd(e.target.value)}
                          className="flex-1 bg-kumo-base text-kumo-strong text-xs border border-kumo-line rounded-lg p-1.5 focus:outline-none focus:ring-1 focus:ring-kumo-brand"
                        >
                          <option value="">添加模型...</option>
                          {filteredTitleModelOptions().map((m) => (
                            <option key={m.id} value={m.id}>
                              {m.id} ({m.owned_by})
                            </option>
                          ))}
                        </select>
                        <Button onClick={addTitleModel} disabled={!openaiTitleModelToAdd}>
                          添加
                        </Button>
                      </div>

                      {/* Test generating */}
                      <div className="flex items-center gap-2 pt-1 border-t border-kumo-line/30">
                        <Button onClick={testTitleGeneration} disabled={openaiTitleGenerating} className="text-[10px]">
                          {openaiTitleGenerating ? '生成中...' : '测试自动命名'}
                        </Button>
                        {openaiTitleLastResult && (
                          <span
                            className={`text-[10px] font-semibold ${
                              openaiTitleLastResult.success ? 'text-kumo-success' : 'text-kumo-danger'
                            }`}
                          >
                            {openaiTitleLastResult.success
                              ? `成功: "${openaiTitleLastResult.title}"`
                              : `错误: ${openaiTitleLastResult.error}`}
                          </span>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              </div>
            ) : (
              // Models management tab
              <div className="space-y-4">
                <div className="flex gap-2 items-center text-xs">
                  <div className="flex-1 flex items-center gap-1.5 px-3 py-1.5 border border-kumo-line rounded-lg bg-kumo-recessed/50">
                    <Search className="w-3.5 h-3.5 text-kumo-subtle" />
                    <input
                      type="text"
                      value={openaiModelSearch}
                      onChange={(e) => setOpenaiModelSearch(e.target.value)}
                      placeholder="搜索模型..."
                      className="bg-transparent border-0 focus:outline-none text-xs w-full"
                    />
                  </div>

                  <select
                    value={openaiSelectedEndpointId}
                    onChange={(e) => setOpenaiSelectedEndpointId(e.target.value)}
                    className="bg-kumo-base text-kumo-strong border border-kumo-line rounded-lg p-1.5 focus:outline-none text-xs"
                  >
                    <option value="">所有端点</option>
                    {endpoints.map((ep) => (
                      <option key={ep.id} value={ep.id}>
                        {ep.name}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="flex justify-between items-center text-xs">
                  <span className="font-semibold text-kumo-strong">显示已隐藏的模型</span>
                  <input
                    type="checkbox"
                    checked={openaiShowHiddenModels}
                    onChange={(e) => setOpenaiShowHiddenModels(e.target.checked)}
                    className="w-8 h-4 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-3 before:h-3 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
                  />
                </div>

                {/* Models list grid */}
                <div className="border border-kumo-line rounded-lg overflow-hidden max-h-[40vh] overflow-y-auto bg-kumo-recessed/10 p-2 divide-y divide-kumo-line">
                  {filteredModelsList.length === 0 ? (
                    <p className="text-center py-8 text-kumo-subtle text-xs">无匹配模型</p>
                  ) : (
                    filteredModelsList.map((model) => {
                      const isPinned = pinnedModels.includes(model.id);
                      const isHidden = hiddenModels.includes(model.id);

                      return (
                        <div key={model.id} className="flex justify-between items-center py-2 first:pt-0 last:pb-0 text-xs">
                          <div className="min-w-0 pr-3">
                            <span className="font-mono font-semibold text-kumo-strong block truncate">
                              {model.id}
                            </span>
                            <span className="text-[9px] text-kumo-subtle font-semibold">
                              渠道: {model.owned_by}
                            </span>
                          </div>

                          <div className="flex gap-1">
                            <button
                              onClick={() => togglePinModel(model.id)}
                              className={`p-1.5 hover:bg-kumo-recessed rounded transition-colors ${
                                isPinned ? 'text-yellow-500' : 'text-kumo-subtle hover:text-kumo-strong'
                              }`}
                              title={isPinned ? '取消收藏' : '收藏'}
                            >
                              <Star className={`w-3.5 h-3.5 ${isPinned ? 'fill-yellow-500' : ''}`} />
                            </button>
                            <button
                              onClick={() => toggleHideModel(model.id)}
                              className={`p-1.5 hover:bg-kumo-recessed rounded transition-colors ${
                                isHidden ? 'text-kumo-brand' : 'text-kumo-subtle hover:text-kumo-strong'
                              }`}
                              title={isHidden ? '显示' : '隐藏'}
                            >
                              {isHidden ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
                            </button>
                          </div>
                        </div>
                      );
                    })
                  )}
                </div>
              </div>
            )}
          </div>

          <div className="p-4 border-t border-kumo-line flex justify-end bg-kumo-recessed/30">
            <Button variant="primary" onClick={saveChatSettings}>
              保存设置
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default OpenAIPage;
