import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Tabs } from '@cloudflare/kumo';
import { PageStack, stickyTabsBaseClass } from '../../components/ui/AppPrimitives.jsx';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import useStore from '../../store.js';
import { dialog } from '../../modules/dialog.js';
import { toast } from '../../modules/toast.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { API, INTERNAL_API, RUNTIME_API, TUNNEL_API, PREFERRED_API, SERVER_INVENTORY_API, DEFAULT_EXTERNAL_POOL_ID, LOAD_TIMEOUT_MS, INITIAL_SKELETON_MS, emptyInternalNodeForm, emptySubscriptionForm, emptyPlanForm, emptyTemplateForm, emptyNodeForm } from './constants.js';
import { getAuthHeaders, copyText, countryFlagEmoji, getInstanceCountryCode, subscriptionURL, normalizeManagedServer, isLinuxManagedServer, normalizePublicBase } from './utils.js';
import { tabs } from './tabs.jsx';
import SubscriptionsPanel from './SubscriptionsPanel.jsx';
import PlansPanel from './PlansPanel.jsx';
import NodesPanel from './NodesPanel.jsx';
import NodesSkeleton from './NodesSkeleton.jsx';
import TunnelControls from './TunnelControls.jsx';
import InstancesPanel from './InstancesPanel.jsx';
import TemplatesPanel from './TemplatesPanel.jsx';
import { PlanDialog, InternalNodeDialog, TunnelDialog, PreferredAddressDialog, SubscriptionDialog, NodeDialog, ImportDialog, TemplateDialog } from './Dialogs.jsx';

function SubscriptionPage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const publicApiUrl = useStore((state) => state.publicApiUrl);
  const [activeTab, setActiveTab] = useState('instances');
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [profiles, setProfiles] = useState([]);
  const [plans, setPlans] = useState([]);
  const [subscriptions, setSubscriptions] = useState([]);
  const [nodes, setNodes] = useState([]);
  const [internalNodes, setInternalNodes] = useState([]);
	const [managedTunnels, setManagedTunnels] = useState([]);
	const [managedRuntimes, setManagedRuntimes] = useState([]);
	const [preferredAddresses, setPreferredAddresses] = useState([]);
	const [tunnelForm, setTunnelForm] = useState({ account_id: '', zone_id: '', hostname: '' });
	const [tunnelModalOpen, setTunnelModalOpen] = useState(false);
	const [tunnelTargetServer, setTunnelTargetServer] = useState(null);
	const [preferredModalOpen, setPreferredModalOpen] = useState(false);
	const [preferredForm, setPreferredForm] = useState({ name: '', address: '', port: 443, enabled: true, is_default: false });
	const [cloudflareAccounts, setCloudflareAccounts] = useState([]);
	const [cloudflareZones, setCloudflareZones] = useState([]);
	const [tunnelTasks, setTunnelTasks] = useState({});
	const [nodeTasks, setNodeTasks] = useState({});
	const [selectedInternalHosts, setSelectedInternalHosts] = useState(new Set());
	const [selectedRuntimeHosts, setSelectedRuntimeHosts] = useState(new Set());
	const [internalNodeModalOpen, setInternalNodeModalOpen] = useState(false);
	const [internalNodeForm, setInternalNodeForm] = useState(emptyInternalNodeForm);
	const [editingInternalNodeId, setEditingInternalNodeId] = useState(null);
  const [internalNodeActions, setInternalNodeActions] = useState({});
  const [externalNodeActions, setExternalNodeActions] = useState({});
  const [uninstallingServerId, setUninstallingServerId] = useState('');
  const [templates, setTemplates] = useState([]);
  const [logs, setLogs] = useState([]);
  const [servers, setServers] = useState([]);
  const [settings, setSettings] = useState(null);

  const [subscriptionModalOpen, setSubscriptionModalOpen] = useState(false);
  const [subscriptionForm, setSubscriptionForm] = useState(emptySubscriptionForm);
  const [editingSubscriptionId, setEditingSubscriptionId] = useState(null);
  const [planModalOpen, setPlanModalOpen] = useState(false);
  const [planForm, setPlanForm] = useState(emptyPlanForm);
  const [editingPlanId, setEditingPlanId] = useState(null);
  const [planNodeTypeFilter, setPlanNodeTypeFilter] = useState('all');
  const [planNodeSourceFilter, setPlanNodeSourceFilter] = useState('all');

  const [importModalOpen, setImportModalOpen] = useState(false);
  const [importText, setImportText] = useState('');
  const [importSourceURL, setImportSourceURL] = useState('');
  const [importPreview, setImportPreview] = useState([]);
  const [nodeModalOpen, setNodeModalOpen] = useState(false);
  const [nodeForm, setNodeForm] = useState(emptyNodeForm);
  const [editingNodeId, setEditingNodeId] = useState(null);
  const [protocolFilter, setProtocolFilter] = useState('all');
  const [tagFilter, setTagFilter] = useState('all');

  const [templateModalOpen, setTemplateModalOpen] = useState(false);
  const [templateForm, setTemplateForm] = useState(emptyTemplateForm);
  const [editingTemplateId, setEditingTemplateId] = useState(null);
  const [templateSubscriptionId, setTemplateSubscriptionId] = useState('');
  const [templateBindingId, setTemplateBindingId] = useState('');
  const loadGenerationRef = useRef(0);
	const terminalTaskIDsRef = useRef(new Set());

  const publicBase = useMemo(
    () => normalizePublicBase(publicApiUrl, typeof window === 'undefined' ? '' : window.location.origin),
    [publicApiUrl]
  );

  const loadAll = async () => {
    const generation = ++loadGenerationRef.current;
    setLoading(true);
    const loadJSON = async (url) => {
      const controller = new AbortController();
      const timeout = window.setTimeout(() => controller.abort(), LOAD_TIMEOUT_MS);
      try {
        const response = await fetch(url, { headers: getAuthHeaders(), signal: controller.signal, cache: 'no-store' });
        if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
        return await response.json();
      } finally {
        window.clearTimeout(timeout);
      }
    };

    const resources = [
      [`${API}/profiles`, setProfiles, []],
      [`${API}/plans`, setPlans, []],
      [`${API}/subscriptions`, setSubscriptions, []],
      [`${API}/nodes`, setNodes, []],
      [INTERNAL_API, setInternalNodes, []],
	  [TUNNEL_API, setManagedTunnels, []],
	  [PREFERRED_API, setPreferredAddresses, []],
      [`${API}/templates`, setTemplates, []],
      [`${API}/logs?limit=200`, setLogs, []],
      [SERVER_INVENTORY_API, (items) => setServers((Array.isArray(items) ? items : []).map(normalizeManagedServer).filter(isLinuxManagedServer)), []],
      [`${API}/settings`, setSettings, {}],
	  [RUNTIME_API, setManagedRuntimes, []],
    ];
    const requests = resources.map(([url, setter, fallback]) => loadJSON(url)
      .then((payload) => {
        if (loadGenerationRef.current === generation) setter(payload.data ?? fallback);
        return true;
      })
      .catch((error) => {
        console.warn(`Subscription resource unavailable: ${url}`, error);
		if (loadGenerationRef.current === generation) setter(fallback);
        return false;
      }));

    // A slow optional resource must never own the whole page's loading state.
    await Promise.race([
      Promise.allSettled([requests[4], requests[7]]),
      new Promise((resolve) => window.setTimeout(resolve, INITIAL_SKELETON_MS)),
    ]);
    if (loadGenerationRef.current === generation) setLoading(false);

    const results = await Promise.all(requests);
    if (loadGenerationRef.current === generation && results.some((success) => !success)) {
      toast.warning(`${results.filter((success) => !success).length} 项数据暂时无法载入，其余内容已显示`);
    }
  };

  useEffect(() => {
    loadAll();
  }, []);

	const runtimeLifecycleServers = useMemo(() => servers.filter((server) => server.status === 'online' && server.agent_capabilities?.proxy_runtime_lifecycle_v2 === true), [servers]);
	useEffect(() => {
		const valid = new Set(runtimeLifecycleServers.map((server) => String(server.id)));
		setSelectedRuntimeHosts((current) => {
			const next = new Set([...current].filter((id) => valid.has(String(id))));
			return next.size === current.size ? current : next;
		});
	}, [runtimeLifecycleServers]);

	useEffect(() => {
	    const entries = [
	      ...Object.entries(tunnelTasks).map(([taskID, value]) => ['tunnel', taskID, value]),
	      ...Object.entries(nodeTasks).map(([taskID, value]) => ['node', taskID, value]),
	    ];
	    if (entries.length === 0) return undefined;
	    const removeTask = (kind, taskID) => {
	      const setter = kind === 'tunnel' ? setTunnelTasks : setNodeTasks;
	      setter((current) => { const next = { ...current }; delete next[taskID]; return next; });
	    };
	    const handleEvent = (kind, taskID, payload, source) => {
	      const data = payload?.data || {};
	      const terminal = payload?.status === 'completed' || payload?.status === 'failed' || payload?.type === 'completed' || payload?.type === 'failed';
	      const setter = kind === 'tunnel' ? setTunnelTasks : setNodeTasks;
	      setter((current) => ({ ...current, [taskID]: payload }));
	      if (payload?.type === 'progress' && !terminal && Number(payload.progress || 0) < 100 && data?.message) toast.info(`${data.message}（${payload.progress || 0}%）`, { isManual: true, timeout: 2500 });
	      if (terminal && !terminalTaskIDsRef.current.has(taskID)) {
	        terminalTaskIDsRef.current.add(taskID);
	        if (payload.status === 'completed' || payload.type === 'completed') {
	          toast.success(typeof data === 'string' ? data : (data?.message || (kind === 'node' ? '节点部署已完成' : 'Tunnel 任务已完成')));
	          loadAll();
	        } else {
	          toast.error(payload.error || '任务失败');
	        }
	        source?.close();
	        removeTask(kind, taskID);
	      }
	    };
    const pollers = [];
    const sources = entries.map(([kind, taskID]) => {
      const source = new EventSource(`/api/server/tasks/${taskID}/stream`);
      source.onmessage = (event) => {
        try { handleEvent(kind, taskID, JSON.parse(event.data), source); } catch { /* wait for the status fallback */ }
      };
      source.onerror = () => {
        source.close();
        const poll = window.setInterval(async () => {
          try {
            const response = await fetch(`/api/server/tasks/${taskID}`, { headers: getAuthHeaders(), cache: 'no-store' });
            if (!response.ok) return;
            const body = await response.json();
            handleEvent(kind, taskID, body.data || body, source);
            if ((body.data || body).status === 'completed' || (body.data || body).status === 'failed') window.clearInterval(poll);
          } catch { /* keep retrying while the task is running */ }
        }, 750);
        pollers.push(poll);
      };
      return source;
    });
    return () => { sources.forEach((source) => source.close()); pollers.forEach((poll) => window.clearInterval(poll)); };
  }, [Object.keys(tunnelTasks).join(','), Object.keys(nodeTasks).join(',')]);

	useEffect(() => {
		if (!tunnelModalOpen) return undefined;
		let cancelled = false;
		fetch('/api/cloudflare/accounts', { headers: getAuthHeaders(), cache: 'no-store' }).then((response) => response.json()).then((payload) => {
			if (cancelled) return;
			const accounts = Array.isArray(payload) ? payload : (payload.data || payload.accounts || []);
			setCloudflareAccounts(accounts);
			setTunnelForm((current) => ({ ...current, account_id: current.account_id || accounts[0]?.id || '' }));
		}).catch(() => { if (!cancelled) setCloudflareAccounts([]); });
		return () => { cancelled = true; };
	}, [tunnelModalOpen]);

	useEffect(() => {
		if (!tunnelForm.account_id) { setCloudflareZones([]); return undefined; }
		let cancelled = false;
		fetch(`/api/cloudflare/accounts/${encodeURIComponent(tunnelForm.account_id)}/zones`, { headers: getAuthHeaders(), cache: 'no-store' }).then((response) => response.json()).then((payload) => {
			if (cancelled) return;
			const zones = Array.isArray(payload) ? payload : (payload.data || payload.zones || []);
			setCloudflareZones(zones);
			setTunnelForm((current) => ({ ...current, zone_id: current.zone_id || zones[0]?.id || '' }));
		}).catch(() => { if (!cancelled) setCloudflareZones([]); });
		return () => { cancelled = true; };
	}, [tunnelForm.account_id]);

	useEffect(() => {
		if (!tunnelTargetServer || !tunnelForm.zone_id) return;
		const zone = cloudflareZones.find((item) => String(item.id) === String(tunnelForm.zone_id));
		const zoneName = String(zone?.name || '').trim().toLowerCase().replace(/\.$/, '');
		if (!zoneName) return;
		const generated = `cf-${String(tunnelTargetServer.id).replace(/[^a-z0-9]/gi, '').slice(0, 12) || 'node'}.${zoneName}`;
		setTunnelForm((current) => ({ ...current, hostname: generated }));
	}, [tunnelTargetServer, tunnelForm.zone_id, cloudflareZones]);

  const openTunnelDeployment = (server) => {
		setTunnelTargetServer(server);
		setTunnelModalOpen(true);
	};

	const deployProxyRuntime = async (serverIDs) => {
		const targets = Array.isArray(serverIDs) ? serverIDs : [serverIDs];
		if (targets.length === 0) return;
		setSaving(true);
		try {
			const results = await Promise.allSettled(targets.map(async (serverID) => {
				const response = await fetch(`${RUNTIME_API}/${serverID}/install`, { method: 'POST', headers: getAuthHeaders() });
				const payload = await response.json();
				if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || '代理程序部署失败');
				const taskID = payload.data?.task_id;
				if (taskID) setNodeTasks((current) => ({ ...current, [taskID]: { progress: 0, status: 'pending', data: { message: '代理程序部署任务已提交' } } }));
				return payload;
			}));
			const failed = results.filter((item) => item.status === 'rejected');
			if (failed.length) toast.error(`${failed.length} 台提交失败：${failed[0].reason?.message || '未知错误'}`);
			if (results.length > failed.length) toast.info(`已提交 ${results.length - failed.length} 台代理程序部署`, { isManual: true });
			setSelectedRuntimeHosts(new Set());
			await loadAll();
		} finally { setSaving(false); }
	};

	const uninstallProxyRuntime = async (server) => {
		if (!confirmPress(`runtime-uninstall:${server.id}`, `卸载 ${server.name} 的 sing-box`)) return;
		try {
			const response = await fetch(`${RUNTIME_API}/${server.id}/uninstall`, { method: 'POST', headers: getAuthHeaders() });
			const payload = await response.json();
			if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || '代理程序卸载失败');
			const taskID = payload.data?.task_id;
			if (taskID) setNodeTasks((current) => ({ ...current, [taskID]: { progress: 0, status: 'pending', data: { message: `${server.name} 卸载任务已提交` } } }));
			toast.info('代理程序卸载任务已提交', { isManual: true });
		} catch (error) { toast.error(error.message || '代理程序卸载失败'); }
	};

  const deployTunnel = async (server = tunnelTargetServer) => {
    if (!tunnelForm.account_id || !tunnelForm.zone_id || !tunnelForm.hostname) {
      toast.warning('请填写 Cloudflare 账号、Zone ID 和 Tunnel 域名');
      return;
    }
    try {
      const response = await fetch(`${TUNNEL_API}/${server.id}/deploy`, { method: 'POST', headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify(tunnelForm) });
      const payload = await response.json();
      if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || 'Tunnel 部署失败');
      const taskID = payload.data?.task_id;
      if (taskID) setTunnelTasks((current) => ({ ...current, [taskID]: { progress: 0, data: { message: '任务已提交' } } }));
      toast.info(`${server.name} 的 Tunnel 部署已提交`, { isManual: true });
		setTunnelModalOpen(false);
    } catch (error) { toast.error(error.message || 'Tunnel 部署失败'); }
  };

	const savePreferredAddress = async () => {
		if (!preferredForm.name.trim() || !preferredForm.address.trim()) return toast.warning('请填写优选地址名称和域名/IP');
		try {
			const response = await fetch(PREFERRED_API, { method: 'POST', headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify(preferredForm) });
			const payload = await response.json();
			if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || '保存优选地址失败');
			setPreferredForm({ name: '', address: '', port: 443, enabled: true, is_default: false });
			await loadAll();
			toast.success('优选地址已保存');
		} catch (error) { toast.error(error.message || '保存优选地址失败'); }
	};

	const deletePreferredAddress = async (item) => {
		if (!confirmPress(`preferred-address:${item.id}`, `删除优选地址 ${item.name}`)) return;
		try {
			const response = await fetch(`${PREFERRED_API}/${item.id}`, { method: 'DELETE', headers: getAuthHeaders() });
			const payload = await response.json();
			if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || '删除优选地址失败');
			await loadAll();
			toast.success('优选地址已删除');
		} catch (error) { toast.error(error.message || '删除优选地址失败'); }
	};

	const setPreferredDefault = async (item) => {
		try {
			const response = await fetch(`${PREFERRED_API}/${item.id}`, { method: 'PUT', headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify({ name: item.name, address: item.address, port: item.port, enabled: item.enabled !== false, is_default: true, sort_order: item.sort_order || 0 }) });
			const payload = await response.json();
			if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || '设为默认失败');
			await loadAll();
			toast.success(`已将 ${item.name} 设为默认`);
		} catch (error) { toast.error(error.message || '设为默认失败'); }
	};

  const uninstallTunnel = async (server) => {
    const entry = managedTunnels.find((item) => item.server_id === server.id);
    if (!entry) return toast.warning('该主机没有 Managed Tunnel');
		const affectedNodes = Number(entry.node_count ?? internalNodes.filter((node) => node.server_id === server.id && node.access_mode === 'cloudflare_tunnel').length);
    if (!confirmPress(`managed-tunnel-delete:${server.id}`, `卸载 ${server.name} 的 Tunnel、DNS、cloudflared 以及 ${affectedNodes} 个关联节点`)) return;
    try {
      const response = await fetch(`${TUNNEL_API}/${server.id}?cascade=1`, { method: 'DELETE', headers: getAuthHeaders() });
      const payload = await response.json();
      if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || 'Tunnel 卸载失败');
      const taskID = payload.data?.task_id;
      if (taskID) setTunnelTasks((current) => ({ ...current, [taskID]: { progress: 0, data: { message: '卸载任务已提交' } } }));
      toast.info('Tunnel 卸载任务已提交', { isManual: true });
    } catch (error) { toast.error(error.message || 'Tunnel 卸载失败'); }
  };

	const waitForTaskTerminal = async (taskID, timeoutMs = 240000) => {
		const deadline = Date.now() + timeoutMs;
		while (Date.now() < deadline) {
			const response = await fetch(`/api/server/tasks/${taskID}`, { headers: getAuthHeaders(), cache: 'no-store' });
			const payload = await response.json();
			if (!response.ok) throw new Error(payload.error || '无法读取任务状态');
			const task = payload.data || payload;
			if (task.status === 'completed') return task;
			if (task.status === 'failed' || task.status === 'cancelled') throw new Error(task.error || '任务执行失败');
			await new Promise((resolve) => window.setTimeout(resolve, 750));
		}
		throw new Error('任务等待超时，请到任务状态查看结果');
	};

  const createInternalNode = async () => {
    const selectedIDs = [...selectedInternalHosts];
    const targetServerIDs = selectedIDs.length > 0 ? selectedIDs : [internalNodeForm.server_id].filter(Boolean);
    if (targetServerIDs.length === 0) {
      toast.warning('请选择目标实例');
      return;
    }
    setSaving(true);
    try {
      const results = await Promise.allSettled(targetServerIDs.map(async (serverID) => {
        const server = servers.find((item) => item.id === serverID);
        const customName = internalNodeForm.name.trim();
        const flag = countryFlagEmoji(getInstanceCountryCode(server));
        const generatedName = `${flag ? `${flag} ` : ''}${server?.name || serverID}`;
        const namedNode = customName
          ? `${flag && !customName.startsWith(flag) ? `${flag} ` : ''}${targetServerIDs.length > 1 ? `${customName}-${server?.name || serverID}` : customName}`
          : generatedName;
        const existingNode = internalNodes.find((node) => (
          node.server_id === serverID
          && node.protocol === internalNodeForm.protocol
          && node.name === namedNode
        ));
        if (existingNode) {
          const res = await fetch(`${INTERNAL_API}/${existingNode.id}/reconcile`, { method: 'POST', headers: getAuthHeaders() });
          const data = await res.json();
          if (!res.ok || data.success === false) throw new Error(`${server?.name || serverID}: ${data.error || data.message || '重新部署失败'}`);
          if (data.data?.task_id) setNodeTasks((current) => ({ ...current, [data.data.task_id]: { progress: 0, status: 'pending', data: { message: `${server?.name || serverID} 部署任务已提交` } } }));
          return { ...data, reused: true };
        }
        const payload = {
          ...internalNodeForm,
          server_id: serverID,
          name: namedNode,
          public_host: server?.host || '',
        };
        const res = await fetch(INTERNAL_API, { method: 'POST', headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
        const data = await res.json();
        if (!res.ok || data.success === false) throw new Error(`${servers.find((server) => server.id === serverID)?.name || serverID}: ${data.error || data.message || '部署失败'}`);
        if (data.data?.task_id) setNodeTasks((current) => ({ ...current, [data.data.task_id]: { progress: 0, status: 'pending', data: { message: `${server?.name || serverID} 部署任务已提交` } } }));
        return data;
      }));
      const succeeded = results.filter((result) => result.status === 'fulfilled');
      const failed = results.filter((result) => result.status === 'rejected');
      if (succeeded.length > 0) toast.info(`已提交 ${succeeded.length} 个节点生成任务`, { isManual: true });
      if (failed.length > 0) toast.error(`${failed.length} 台部署失败：${failed[0].reason?.message || '未知错误'}`);
      if (succeeded.length > 0) {
        setInternalNodeModalOpen(false);
        setInternalNodeForm(emptyInternalNodeForm);
        setSelectedInternalHosts(new Set());
      }
      await loadAll();
    } catch (error) { toast.error(error.message); } finally { setSaving(false); }
  };

  const withInternalNodeAction = async (nodeID, action, callback) => {
    const actionKey = `${nodeID}:${action}`;
    if (internalNodeActions[actionKey]) return;
    setInternalNodeActions((previous) => ({ ...previous, [actionKey]: true }));
    try {
      await callback();
    } finally {
      setInternalNodeActions((previous) => {
        const next = { ...previous };
        delete next[actionKey];
        return next;
      });
    }
  };

  const reconcileInternalNode = async (node) => {
    await withInternalNodeAction(node.id, 'reconcile', async () => {
      try {
        const res = await fetch(`${INTERNAL_API}/${node.id}/reconcile`, { method: 'POST', headers: getAuthHeaders() });
        const data = await res.json();
        if (!res.ok || data.success === false) throw new Error(data.error || data.message || '重新部署失败');
        if (data.data?.task_id) setNodeTasks((current) => ({ ...current, [data.data.task_id]: { progress: 0, status: 'pending', data: { message: `${node.name} 部署任务已提交` } } }));
        else toast.success(`${node.name} 已重新部署`);
        await loadAll();
      } catch (error) {
        toast.error(error.message || '重新部署失败');
      }
    });
  };

  const deleteInternalNode = async (node) => {
    if (!confirmPress(`internal-node-delete:${node.id}`, `卸载节点 ${node.name}`)) return;
    await withInternalNodeAction(node.id, 'delete', async () => {
      try {
		const requestDelete = async (force = false) => {
			const suffix = force ? '?force=1' : '';
			const response = await fetch(`${INTERNAL_API}/${node.id}${suffix}`, { method: 'DELETE', headers: getAuthHeaders() });
			const payload = await response.json().catch(() => ({}));
			return { response, payload };
		};
		let { response, payload } = await requestDelete();
		if ((!response.ok || payload.success === false) && payload.data?.can_force_detach) {
			const confirmed = await dialog.confirm({
				title: '仅从面板移除节点',
				message: `${payload.error}。继续会移除面板记录和套餐关联，但主机恢复连接后仍可能存在残留服务与防火墙规则。`,
				confirmText: '从面板移除',
				cancelText: '保留节点',
				variant: 'destructive',
			});
			if (!confirmed) return;
			({ response, payload } = await requestDelete(true));
		}
		if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || '卸载失败');
				const taskID = payload.data?.task_id;
				if (taskID) {
					setNodeTasks((current) => ({ ...current, [taskID]: { progress: 0, status: 'pending', data: { message: `${node.name} 卸载任务已提交` } } }));
					toast.info(`${node.name} 卸载任务已提交`, { isManual: true });
				} else toast.success(`${node.name} 已卸载`);
        await loadAll();
      } catch (error) {
        toast.error(error.message || '卸载失败');
      }
    });
  };

  const reconcileInternalNodes = async (managed) => {
    setSaving(true);
    try {
		let completed = 0;
		for (const node of managed) {
        const res = await fetch(`${INTERNAL_API}/${node.id}/reconcile`, { method: 'POST', headers: getAuthHeaders() });
        const data = await res.json();
        if (!res.ok || data.success === false) throw new Error(data.error || data.message || `${node.name} 重新部署失败`);
			if (data.data?.task_id) {
				setNodeTasks((current) => ({ ...current, [data.data.task_id]: { progress: 0, status: 'pending', data: { message: `${node.name} 部署任务已提交` } } }));
				await waitForTaskTerminal(data.data.task_id);
			}
			completed += 1;
		}
		toast.success(`已重新部署 ${completed} 个节点`);
      await loadAll();
    } catch (error) {
      toast.error(error.message || '重新部署失败');
    } finally {
      setSaving(false);
    }
  };

  const uninstallInternalNodes = async (server, managed) => {
    if (!confirmPress(`instance-proxy-uninstall:${server.id}`, `卸载 ${server.name} 的 ${managed.length} 个节点`)) return;
    setUninstallingServerId(server.id);
    setSaving(true);
    try {
		let completed = 0;
		for (const node of managed) {
        const res = await fetch(`${INTERNAL_API}/${node.id}`, { method: 'DELETE', headers: getAuthHeaders() });
        const data = await res.json();
        if (!res.ok || data.success === false) throw new Error(data.error || data.message || `${node.name} 卸载失败`);
			const taskID = data.data?.task_id;
			if (taskID) {
				setNodeTasks((current) => ({ ...current, [taskID]: { progress: 0, status: 'pending', data: { message: `${node.name} 卸载任务已提交` } } }));
				await waitForTaskTerminal(taskID);
			}
			completed += 1;
		}
		toast.success(`已卸载 ${completed} 个节点`);
      await loadAll();
    } catch (error) {
      toast.error(error.message || '卸载失败');
    } finally {
      setUninstallingServerId('');
      setSaving(false);
    }
  };

  const toggleInternalNodeEnabled = async (node, enabled) => {
    try {
      const res = await fetch(`${INTERNAL_API}/${node.id}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled }),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || data.message || '更新失败');
      if (data.data?.task_id) {
        setNodeTasks((current) => ({ ...current, [data.data.task_id]: { progress: 0, status: 'pending', data: { message: `${node.name} 状态更新任务已提交` } } }));
        toast.info(enabled ? `正在启用 ${node.name}` : `正在停用 ${node.name}`, { isManual: true });
      } else toast.success(enabled ? '内部节点已启用' : '内部节点已停用');
      await loadAll();
    } catch (error) {
      toast.error(error.message || '更新失败');
    }
  };

  const toggleInternalHost = (serverID, checked) => setSelectedInternalHosts((previous) => {
    const next = new Set(previous); if (checked) next.add(serverID); else next.delete(serverID); return next;
  });

  const startInternalDeployment = (serverID = '') => {
    setEditingInternalNodeId(null);
    const nextSelection = serverID ? new Set([serverID]) : new Set(selectedInternalHosts);
    if (serverID) setSelectedInternalHosts(nextSelection);
    const selected = serverID || [...nextSelection][0] || '';
    setInternalNodeForm((prev) => ({ ...emptyInternalNodeForm, server_id: selected, protocol: prev.protocol || 'vless-reality', public_host: servers.find((server) => server.id === selected)?.host || '' }));
    setInternalNodeModalOpen(true);
  };

  const openEditInternalNode = (node) => {
    setEditingInternalNodeId(node.id);
    setSelectedInternalHosts(new Set([node.server_id]));
    setInternalNodeForm({ ...emptyInternalNodeForm, ...node, server_name: node.server_name || 'www.cloudflare.com' });
    setInternalNodeModalOpen(true);
  };

  const saveInternalNode = async () => {
    if (!internalNodeForm.name.trim()) return toast.warning('请输入节点名称');
    setSaving(true);
    try {
      const res = await fetch(`${INTERNAL_API}/${editingInternalNodeId}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ name: internalNodeForm.name.trim(), stable: !!internalNodeForm.stable, preferred_address_id: internalNodeForm.preferred_address_id || '', connect_address: '', connect_port: 0 }),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || data.message || '保存失败');
      setInternalNodeModalOpen(false);
      setEditingInternalNodeId(null);
      toast.success('节点配置已更新');
      await loadAll();
    } catch (error) {
      toast.error(error.message || '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const templateItems = useMemo(() => templates.map((item) => ({
    value: item.id,
    label: item.valid === false ? `${item.name}（配置错误）` : item.name,
    disabled: item.valid === false,
  })), [templates]);
  const runtimeByServer = useMemo(() => new Map(managedRuntimes.map((item) => [String(item.server_id), item])), [managedRuntimes]);
  const runtimeReadyServers = useMemo(() => servers.filter((server) => runtimeByServer.get(String(server.id))?.apply_status === 'running'), [servers, runtimeByServer]);
	useEffect(() => {
		const valid = new Set(runtimeReadyServers.map((server) => String(server.id)));
		setSelectedInternalHosts((current) => {
			const next = new Set([...current].filter((id) => valid.has(String(id))));
			return next.size === current.size ? current : next;
		});
	}, [runtimeReadyServers]);
  const serverNameById = useMemo(() => {
    const map = new Map();
    servers.forEach((item) => map.set(String(item.id), item.name || item.host || item.id));
    return map;
  }, [servers]);
  const externalNodePool = useMemo(
    () => profiles.find((item) => item.id === DEFAULT_EXTERNAL_POOL_ID) || null,
    [profiles]
  );
  const exportSubscriptions = subscriptions;
  const subscriptionItems = useMemo(() => exportSubscriptions.map((item) => ({ value: item.id, label: item.name })), [exportSubscriptions]);
  const planItems = useMemo(() => plans.map((item) => ({ value: item.id, label: item.enabled ? item.name : `${item.name}（已停用）`, disabled: !item.enabled })), [plans]);
  const firstEnabledPlanID = useMemo(() => plans.find((item) => item.enabled)?.id || '', [plans]);
  const planCandidateNodes = useMemo(() => [
    ...internalNodes.map((node) => ({ ...node, source_group: 'internal', display_type: node.protocol === 'vless-reality' ? 'vless' : node.protocol })),
    ...nodes.map((node) => ({ ...node, source_group: 'external', display_type: String(node.type || 'unknown').toLowerCase() })),
  ], [internalNodes, nodes]);
  const planNodeTypeItems = useMemo(() => [{ value: 'all', label: '全部类型' }, ...Array.from(new Set(planCandidateNodes.map((node) => node.display_type))).sort().map((value) => ({ value, label: value.toUpperCase() }))], [planCandidateNodes]);
  const visiblePlanNodes = useMemo(() => planCandidateNodes.filter((node) => (
    (planNodeTypeFilter === 'all' || node.display_type === planNodeTypeFilter)
    && (planNodeSourceFilter === 'all' || node.source_group === planNodeSourceFilter)
  )), [planCandidateNodes, planNodeSourceFilter, planNodeTypeFilter]);
  const visiblePlanNodeIDs = useMemo(() => visiblePlanNodes.map((node) => node.id), [visiblePlanNodes]);
  const allVisiblePlanNodesSelected = visiblePlanNodeIDs.length > 0 && visiblePlanNodeIDs.every((id) => planForm.node_ids.includes(id));
  const selectedTemplateSubscription = useMemo(
    () => exportSubscriptions.find((item) => item.id === templateSubscriptionId) || null,
    [exportSubscriptions, templateSubscriptionId]
  );
  useEffect(() => {
    if (exportSubscriptions.length > 0 && !exportSubscriptions.some((item) => item.id === templateSubscriptionId)) {
      setTemplateSubscriptionId(exportSubscriptions[0].id);
    }
  }, [exportSubscriptions, templateSubscriptionId]);
  useEffect(() => {
    setTemplateBindingId(
      selectedTemplateSubscription?.template_id
      || settings?.default_template_id
      || 'builtin_mihomo_default'
    );
  }, [selectedTemplateSubscription, settings?.default_template_id]);
  const visibleNodes = useMemo(
    () => nodes,
    [nodes]
  );
  const protocolItems = useMemo(() => {
    const counts = new Map();
    visibleNodes.forEach((node) => {
      const key = String(node.type || 'unknown').toLowerCase();
      counts.set(key, (counts.get(key) || 0) + 1);
    });
    return [
      { value: 'all', label: `全部 (${visibleNodes.length})` },
      ...Array.from(counts.entries())
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([value, count]) => ({ value, label: `${value.toUpperCase()} (${count})` })),
    ];
  }, [visibleNodes]);
  const tagItems = useMemo(() => {
    const counts = new Map();
    visibleNodes.forEach((node) => {
      const tags = String(node.tags || '').split(',').map((item) => item.trim()).filter(Boolean);
      if (tags.length === 0) return;
      tags.forEach((tag) => counts.set(tag, (counts.get(tag) || 0) + 1));
    });
    return [
      { value: 'all', label: `全部 (${visibleNodes.length})` },
      ...Array.from(counts.entries())
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([value, count]) => ({ value, label: `${value} (${count})` })),
    ];
  }, [visibleNodes]);
  const filteredNodes = useMemo(() => (
    visibleNodes.filter((node) => {
      const protocolOK = protocolFilter === 'all' || String(node.type || 'unknown').toLowerCase() === protocolFilter;
      const tagOK = tagFilter === 'all' || String(node.tags || '').split(',').map((item) => item.trim()).includes(tagFilter);
      return protocolOK && tagOK;
    })
  ), [protocolFilter, tagFilter, visibleNodes]);
  useEffect(() => {
    if (!protocolItems.some((item) => item.value === protocolFilter)) {
      setProtocolFilter('all');
    }
  }, [protocolFilter, protocolItems]);

  useEffect(() => {
    if (!tagItems.some((item) => item.value === tagFilter)) {
      setTagFilter('all');
    }
  }, [tagFilter, tagItems]);

  useEffect(() => {
    setTemplateBindingId(selectedTemplateSubscription?.template_id || settings?.default_template_id || 'builtin_mihomo_default');
  }, [selectedTemplateSubscription, settings]);

  const openCreateSubscription = () => {
    const linkIndex = exportSubscriptions.length + 1;
    setEditingSubscriptionId(null);
    setSubscriptionForm({
      ...emptySubscriptionForm,
      plan_id: firstEnabledPlanID,
      name: `订阅 ${linkIndex}`,
      template_id: settings?.default_template_id || 'builtin_mihomo_default',
    });
    setSubscriptionModalOpen(true);
  };

  const openCreatePlan = () => {
    setEditingPlanId(null);
    setPlanForm({ ...emptyPlanForm, node_ids: [] });
    setPlanNodeTypeFilter('all'); setPlanNodeSourceFilter('all');
    setPlanModalOpen(true);
  };

  const openEditPlan = (plan) => {
    setEditingPlanId(plan.id);
    setPlanForm({ ...emptyPlanForm, ...plan, node_ids: Array.isArray(plan.node_ids) ? plan.node_ids : [] });
    setPlanNodeTypeFilter('all'); setPlanNodeSourceFilter('all');
    setPlanModalOpen(true);
  };

  const savePlan = async () => {
    if (!planForm.name.trim()) return toast.warning('请输入套餐名称');
    setSaving(true);
    try {
      const res = await fetch(editingPlanId ? `${API}/plans/${editingPlanId}` : `${API}/plans`, { method: editingPlanId ? 'PUT' : 'POST', headers: getAuthHeaders(), body: JSON.stringify(planForm) });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || '保存失败');
      setPlanModalOpen(false); await loadAll(); toast.success('套餐已保存');
    } catch (error) { toast.error(error.message); } finally { setSaving(false); }
  };

  const deletePlan = async (plan) => {
    if (!confirmPress(`plan-delete:${plan.id}`, `删除套餐「${plan.name}」`)) return;
    const res = await fetch(`${API}/plans/${plan.id}`, { method: 'DELETE', headers: getAuthHeaders() });
    const data = await res.json();
    if (!res.ok || data.success === false) return toast.error(data.error || '删除失败');
    await loadAll(); toast.success('套餐已删除');
  };

  const togglePlanEnabled = async (plan, enabled) => {
    try {
      const res = await fetch(`${API}/plans/${plan.id}`, { method: 'PATCH', headers: getAuthHeaders(), body: JSON.stringify({ enabled }) });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || data.success === false) throw new Error(data.error || '套餐状态更新失败');
      await loadAll();
      toast.success(enabled ? '套餐已启用，对应订阅已恢复' : '套餐已停用，对应订阅已失效');
    } catch (error) { toast.error(error.message || '套餐状态更新失败'); }
  };

  const openEditSubscription = (sub) => {
    setEditingSubscriptionId(sub.id);
    setSubscriptionForm({
      ...emptySubscriptionForm,
      plan_id: sub.plan_id || '',
      name: sub.name || '',
      remark: sub.remark || '',
      enabled: sub.enabled !== false,
      template_id: sub.template_id || settings?.default_template_id || 'builtin_mihomo_default',
    });
    setSubscriptionModalOpen(true);
  };

  const saveSubscription = async () => {
    if (!subscriptionForm.name.trim()) {
      toast.warning('请输入名称');
      return;
    }
    setSaving(true);
    try {
      const res = await fetch(editingSubscriptionId ? `${API}/subscriptions/${editingSubscriptionId}` : `${API}/subscriptions`, {
        method: editingSubscriptionId ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(subscriptionForm),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || '保存失败');
      if (!editingSubscriptionId && data.data?.public_token) {
        await copyText(subscriptionURL(publicBase, data.data), '订阅链接已创建并复制');
      } else {
        toast.success(editingSubscriptionId ? '订阅链接已更新' : '订阅链接已创建');
      }
      setSubscriptionModalOpen(false);
      loadAll();
    } catch (error) {
      toast.error(error.message || '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const deleteSubscription = async (sub) => {
    if (!confirmPress(`subscription-delete:${sub.id}`, `删除订阅链接「${sub.name}」`)) return;
    const res = await fetch(`${API}/subscriptions/${sub.id}`, { method: 'DELETE', headers: getAuthHeaders() });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || data.success === false) {
      toast.error(data.error || '删除失败');
      return;
    }
    toast.success('订阅链接已删除');
    loadAll();
  };

  const toggleSubscriptionEnabled = async (sub, enabled) => {
    try {
      const res = await fetch(`${API}/subscriptions/${sub.id}`, { method: 'PATCH', headers: getAuthHeaders(), body: JSON.stringify({ enabled }) });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || data.success === false) throw new Error(data.error || '订阅状态更新失败');
      await loadAll();
      toast.success(enabled ? '订阅已启用' : '订阅已停用');
    } catch (error) { toast.error(error.message || '订阅状态更新失败'); }
  };

  const resetToken = async (sub) => {
    const confirmed = await dialog.confirm({
      title: '重置连接凭据',
      message: `确定要重置「${sub.name}」的连接凭据吗？旧链接、VLESS UUID 和 HY2 密码都会失效，已下载配置会在 Agent 同步后断开。`,
      confirmText: '重置并同步',
      confirmClass: 'text-kumo-warning',
    });
    if (!confirmed) return;
    const res = await fetch(`${API}/subscriptions/${sub.id}/reset-token`, { method: 'POST', headers: getAuthHeaders() });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '重置失败');
      return;
    }
    const queued = Number(data.data?.nodes_queued || 0);
    toast.success(queued > 0 ? `连接凭据已重置，正在同步 ${queued} 个节点` : '连接凭据已重置');
    loadAll();
  };

  const rotateAddress = async (sub) => {
    const confirmed = await dialog.confirm({
      title: '更换订阅地址',
      message: `确定要更换「${sub.name}」的订阅链接吗？旧链接立即失效，VLESS UUID 和 HY2 密码保持不变，已配置的客户端不会断开。`,
      confirmText: '更换订阅地址',
    });
    if (!confirmed) return;
    const res = await fetch(`${API}/subscriptions/${sub.id}/rotate-address`, { method: 'POST', headers: getAuthHeaders() });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '更换失败');
      return;
    }
    toast.success('订阅地址已更换');
    loadAll();
  };

  const refreshProfileUpstream = async (profile) => {
    const res = await fetch(`${API}/profiles/${profile.id}/refresh-upstream`, { method: 'POST', headers: getAuthHeaders() });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '刷新失败');
      return;
    }
    toast.success('节点来源已刷新');
    loadAll();
  };

  const openImportModal = () => {
    setImportText('');
    setImportSourceURL('');
    setImportPreview([]);
    setImportModalOpen(true);
  };

  const previewImport = async () => {
    if (!importSourceURL.trim() && !importText.trim()) {
      toast.warning('请填写原订阅 URL 或粘贴订阅内容');
      return;
    }
    const res = await fetch(`${API}/import/preview`, {
      method: 'POST',
      headers: getAuthHeaders(),
      body: JSON.stringify({ text: importText, source_url: importSourceURL }),
    });
    const data = await res.json();
    setImportPreview(data.data || []);
  };

  const commitImport = async (replace = false) => {
    if (!importSourceURL.trim() && !importText.trim()) {
      toast.warning('请填写原订阅 URL 或粘贴订阅内容');
      return;
    }
    const res = await fetch(`${API}/import/commit`, {
      method: 'POST',
      headers: getAuthHeaders(),
      body: JSON.stringify({ subscription_id: DEFAULT_EXTERNAL_POOL_ID, text: importText, source_url: importSourceURL, replace }),
    });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '导入失败');
      return;
    }
    toast.success(`已接管 ${data.data?.imported || 0} 个节点`);
    setImportModalOpen(false);
    loadAll();
  };

  const openEditNode = (node) => {
    setEditingNodeId(node.id);
    setNodeForm({
      ...emptyNodeForm,
      ...node,
      port: node.port || 0,
      sort_order: node.sort_order || 0,
    });
    setNodeModalOpen(true);
  };

  const saveNode = async () => {
    if (!editingNodeId) return;
    if (!nodeForm.name.trim()) {
      toast.warning('请输入节点名称');
      return;
    }
    setSaving(true);
    try {
      const res = await fetch(`${API}/nodes/${editingNodeId}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          ...nodeForm,
          port: Number(nodeForm.port) || 0,
          sort_order: Number(nodeForm.sort_order) || 0,
        }),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || '保存失败');
      toast.success('节点已更新');
      setNodeModalOpen(false);
      loadAll();
    } catch (error) {
      toast.error(error.message || '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const toggleNodeEnabled = async (node, enabled) => {
    try {
      const res = await fetch(`${API}/nodes/${node.id}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          ...node,
          enabled,
          port: Number(node.port) || 0,
          sort_order: Number(node.sort_order) || 0,
        }),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || '更新失败');
      toast.success(enabled ? '节点已启用' : '节点已停用');
      loadAll();
    } catch (error) {
      toast.error(error.message || '更新失败');
    }
  };

  const deleteNode = async (node) => {
    if (!confirmPress(`external-node-delete:${node.id}`, `删除节点 ${node.name}`)) return;
    const actionKey = `${node.id}:delete`;
    if (externalNodeActions[actionKey]) return;
    setExternalNodeActions((current) => ({ ...current, [actionKey]: true }));
    try {
      const res = await fetch(`${API}/nodes/${node.id}`, { method: 'DELETE', headers: getAuthHeaders() });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || data.success === false) throw new Error(data.error || '删除失败');
      toast.success(`${node.name} 已删除`);
      await loadAll();
    } catch (error) {
      toast.error(error.message || '删除失败');
    } finally {
      setExternalNodeActions((current) => {
        const next = { ...current };
        delete next[actionKey];
        return next;
      });
    }
  };

  const openCreateTemplate = () => {
    setEditingTemplateId(null);
    setTemplateForm(emptyTemplateForm);
    setTemplateModalOpen(true);
  };

  const openEditTemplate = (tpl) => {
    setEditingTemplateId(tpl.id);
    setTemplateForm({ name: tpl.name, format: tpl.format, content: tpl.content, description: tpl.description || '' });
    setTemplateModalOpen(true);
  };

  const openCloneTemplate = (tpl) => {
    setEditingTemplateId(null);
    setTemplateForm({
      name: `${tpl.name}（自定义）`,
      format: tpl.format,
      content: tpl.content,
      description: `基于 ${tpl.name} 的自定义模板`,
    });
    setTemplateModalOpen(true);
  };

  const saveTemplate = async () => {
    setSaving(true);
    try {
      const res = await fetch(editingTemplateId ? `${API}/templates/${editingTemplateId}` : `${API}/templates`, {
        method: editingTemplateId ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(templateForm),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || '保存失败');
      toast.success('模板已保存');
      setTemplateModalOpen(false);
      loadAll();
    } catch (error) {
      toast.error(error.message || '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const setDefaultTemplate = async (tpl) => {
    const res = await fetch(`${API}/templates/${tpl.id}/default`, { method: 'POST', headers: getAuthHeaders() });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '设置失败');
      return;
    }
    toast.success('默认模板已更新');
    loadAll();
  };

  const deleteTemplate = async (tpl) => {
    if (tpl.builtin) {
      toast.warning('内置模板不能删除');
      return;
    }
    if (!confirmPress(`template-delete:${tpl.id}`, `删除模板「${tpl.name}」`)) return;
    const res = await fetch(`${API}/templates/${tpl.id}`, { method: 'DELETE', headers: getAuthHeaders() });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '删除失败');
      return;
    }
    toast.success('模板已删除');
    loadAll();
  };

  const saveTemplateBinding = async () => {
    if (!selectedTemplateSubscription) {
      toast.warning('请选择对外订阅');
      return;
    }
    setSaving(true);
    try {
      const res = await fetch(`${API}/subscriptions/${selectedTemplateSubscription.id}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          ...selectedTemplateSubscription,
          template_id: templateBindingId || settings?.default_template_id || 'builtin_mihomo_default',
        }),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || '保存失败');
      toast.success('转换模板已更新');
      loadAll();
    } catch (error) {
      toast.error(error.message || '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const saveSettings = async () => {
    const res = await fetch(`${API}/settings`, {
      method: 'PUT',
      headers: getAuthHeaders(),
      body: JSON.stringify(settings),
    });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '保存失败');
      return;
    }
    toast.success('设置已保存');
    loadAll();
  };

  const exportBackup = () => {
    fetch(`${API}/export`, { headers: getAuthHeaders() })
      .then((res) => res.json())
      .then((payload) => {
        if (!payload.success) throw new Error(payload.error || '导出失败');
        const blob = new Blob([JSON.stringify(payload.data, null, 2)], { type: 'application/json' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = `subscriptions_export_${new Date().toISOString().slice(0, 10)}.json`;
        link.click();
        URL.revokeObjectURL(url);
      })
      .catch((error) => toast.error(error.message || '导出失败'));
  };

  return (
    <PageStack>
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={(value) => setActiveTab(String(value))}
          tabs={tabs}
        />
      </div>

      <div className="min-w-0">
        {loading && servers.length === 0 && nodes.length === 0 && plans.length === 0 && subscriptions.length === 0 ? <NodesSkeleton /> : (
          <div className="min-w-0">
            {activeTab === 'nodes' && (
              <NodesPanel
                internalNodes={internalNodes}
                runtimeReadyServers={runtimeReadyServers}
                servers={servers}
                preferredAddresses={preferredAddresses}
                internalNodeActions={internalNodeActions}
                externalNodeActions={externalNodeActions}
                filteredNodes={filteredNodes}
                visibleNodes={visibleNodes}
                serverNameById={serverNameById}
                isArmed={isArmed}
                protocolFilter={protocolFilter}
                protocolItems={protocolItems}
                tagFilter={tagFilter}
                tagItems={tagItems}
                onStartInternalDeployment={startInternalDeployment}
                onEditInternalNode={openEditInternalNode}
                onToggleInternalNodeEnabled={toggleInternalNodeEnabled}
                onReconcileInternalNode={reconcileInternalNode}
                onDeleteInternalNode={deleteInternalNode}
                onProtocolFilterChange={setProtocolFilter}
                onTagFilterChange={setTagFilter}
                onOpenImportModal={openImportModal}
                onEditNode={openEditNode}
                onToggleNodeEnabled={toggleNodeEnabled}
                onDeleteNode={deleteNode}
              />
            )}
            {activeTab === 'instances' && <><TunnelControls managedTunnels={managedTunnels} preferredAddresses={preferredAddresses} onOpenPreferredModal={setPreferredModalOpen} /><InstancesPanel
              servers={servers}
              selectedRuntimeHosts={selectedRuntimeHosts}
              setSelectedRuntimeHosts={setSelectedRuntimeHosts}
              runtimeLifecycleServers={runtimeLifecycleServers}
              saving={saving}
              internalNodes={internalNodes}
              runtimeByServer={runtimeByServer}
              managedTunnels={managedTunnels}
              isArmed={isArmed}
              onDeployProxyRuntime={deployProxyRuntime}
              onUninstallProxyRuntime={uninstallProxyRuntime}
              onUninstallTunnel={uninstallTunnel}
              onOpenTunnelDeployment={openTunnelDeployment}
            /></>}
            {activeTab === 'plans' && (
              <PlansPanel
                plans={plans}
                nodes={nodes}
                internalNodes={internalNodes}
                isArmed={isArmed}
                onCreate={openCreatePlan}
                onEdit={openEditPlan}
                onToggleEnabled={togglePlanEnabled}
                onDelete={deletePlan}
              />
            )}
            {activeTab === 'subscriptions' && (
              <SubscriptionsPanel
                subscriptions={exportSubscriptions}
                visibleNodesCount={visibleNodes.length}
                createDisabled={!firstEnabledPlanID}
                publicBase={publicBase}
                plans={plans}
                isArmed={isArmed}
                onCreate={openCreateSubscription}
                onEdit={openEditSubscription}
                onToggleEnabled={toggleSubscriptionEnabled}
                onDelete={deleteSubscription}
                onResetToken={resetToken}
                onRotateAddress={rotateAddress}
              />
            )}
            {activeTab === 'templates' && (
              <TemplatesPanel
                templates={templates}
                publicBase={publicBase}
                saving={saving}
                selectedTemplateSubscription={selectedTemplateSubscription}
                templateSubscriptionId={templateSubscriptionId}
                setTemplateSubscriptionId={setTemplateSubscriptionId}
                subscriptionItems={subscriptionItems}
                templateBindingId={templateBindingId}
                setTemplateBindingId={setTemplateBindingId}
                templateItems={templateItems}
                isArmed={isArmed}
                onOpenCreateTemplate={openCreateTemplate}
                onSaveTemplateBinding={saveTemplateBinding}
                onSetDefaultTemplate={setDefaultTemplate}
                onOpenCloneTemplate={openCloneTemplate}
                onOpenEditTemplate={openEditTemplate}
                onDeleteTemplate={deleteTemplate}
              />
            )}
          </div>
        )}
      </div>

      <PlanDialog
        open={planModalOpen}
        onOpenChange={setPlanModalOpen}
        editingPlanId={editingPlanId}
        planForm={planForm}
        setPlanForm={setPlanForm}
        nodes={nodes}
        allVisiblePlanNodesSelected={allVisiblePlanNodesSelected}
        visiblePlanNodeIDs={visiblePlanNodeIDs}
        visiblePlanNodes={visiblePlanNodes}
        planNodeTypeItems={planNodeTypeItems}
        planNodeSourceFilter={planNodeSourceFilter}
        setPlanNodeSourceFilter={setPlanNodeSourceFilter}
        planNodeTypeFilter={planNodeTypeFilter}
        setPlanNodeTypeFilter={setPlanNodeTypeFilter}
        saving={saving}
        onSave={savePlan}
      />

      <InternalNodeDialog
        open={internalNodeModalOpen}
        onOpenChange={setInternalNodeModalOpen}
        editingInternalNodeId={editingInternalNodeId}
        internalNodeForm={internalNodeForm}
        setInternalNodeForm={setInternalNodeForm}
        selectedInternalHosts={selectedInternalHosts}
        setSelectedInternalHosts={setSelectedInternalHosts}
        runtimeReadyServers={runtimeReadyServers}
        servers={servers}
        preferredAddresses={preferredAddresses}
        saving={saving}
        onSave={saveInternalNode}
        onCreate={createInternalNode}
      />

      <TunnelDialog
        open={tunnelModalOpen}
        onOpenChange={setTunnelModalOpen}
        tunnelForm={tunnelForm}
        setTunnelForm={setTunnelForm}
        cloudflareAccounts={cloudflareAccounts}
        cloudflareZones={cloudflareZones}
        tunnelTargetServer={tunnelTargetServer}
        onDeploy={deployTunnel}
      />

      <PreferredAddressDialog
        open={preferredModalOpen}
        onOpenChange={setPreferredModalOpen}
        preferredAddresses={preferredAddresses}
        preferredForm={preferredForm}
        setPreferredForm={setPreferredForm}
        onSave={savePreferredAddress}
        onSetDefault={setPreferredDefault}
        onDelete={deletePreferredAddress}
      />

      <SubscriptionDialog
        open={subscriptionModalOpen}
        onOpenChange={setSubscriptionModalOpen}
        editingSubscriptionId={editingSubscriptionId}
        subscriptionForm={subscriptionForm}
        setSubscriptionForm={setSubscriptionForm}
        planItems={planItems}
        saving={saving}
        onSave={saveSubscription}
      />

      <NodeDialog
        open={nodeModalOpen}
        onOpenChange={setNodeModalOpen}
        nodeForm={nodeForm}
        setNodeForm={setNodeForm}
        saving={saving}
        onSave={saveNode}
      />

      <ImportDialog
        open={importModalOpen}
        onOpenChange={setImportModalOpen}
        importSourceURL={importSourceURL}
        setImportSourceURL={setImportSourceURL}
        importText={importText}
        setImportText={setImportText}
        importPreview={importPreview}
        onPreview={previewImport}
        onCommit={commitImport}
      />

      <TemplateDialog
        open={templateModalOpen}
        onOpenChange={setTemplateModalOpen}
        editingTemplateId={editingTemplateId}
        templateForm={templateForm}
        setTemplateForm={setTemplateForm}
        saving={saving}
        onSave={saveTemplate}
      />
    </PageStack>
  );
}

export default SubscriptionPage;
