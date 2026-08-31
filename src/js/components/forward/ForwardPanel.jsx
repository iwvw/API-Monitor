import React, { forwardRef, useCallback, useEffect, useImperativeHandle, useState } from 'react';
import { Loader } from '@cloudflare/kumo/components/loader';
import { toast } from '../../modules/toast.js';
import useConfirmPress from '../../hooks/useConfirmPress.js';
import ForwardCanvas from './ForwardCanvas.jsx';
import ForwardDialog from './ForwardDialog.jsx';

const FORWARD_API = '/api/server/forward';

/**
 * 端口转发面板 — 内嵌于主机实例页「端口转发」tab。
 * 负责转发规则的数据加载与全部操作编排，渲染画布与创建/编辑弹窗。
 * 通过 ref 暴露 openCreate()，供宿主页在主 tab 栏触发「创建转发规则」。
 */
const ForwardPanel = forwardRef(function ForwardPanel(_props, ref) {
  const [forwards, setForwards] = useState([]);
  const [servers, setServers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [deploying, setDeploying] = useState(new Set());
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingForward, setEditingForward] = useState(null);
  const [isDeleting, setIsDeleting] = useState(false);
  // 启停操作进行中标记（key 形如 stop:{id} / start:{id}），防连点
  const [acting, setActing] = useState(new Set());
  const setActingKey = (key, on) => setActing((prev) => {
    const next = new Set(prev);
    if (on) next.add(key); else next.delete(key);
    return next;
  });
  // 站点标准两段式删除（4 秒确认窗 + toast 提示 + 自动复位）
  const { isArmed, confirmPress } = useConfirmPress();

  const loadForwards = useCallback(async () => {
    try {
      const res = await fetch(`${FORWARD_API}?limit=100&offset=0`);
      const json = await res.json();
      if (json.success) setForwards(json.data || []);
    } catch (e) {
      toast.error('加载转发规则失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadForwards();
    const interval = setInterval(loadForwards, 30000);
    return () => clearInterval(interval);
  }, [loadForwards]);

  const loadServers = useCallback(async () => {
    try {
      const res = await fetch('/api/server/accounts?limit=200');
      const json = await res.json();
      if (json.success) setServers(json.data || []);
    } catch (e) {}
  }, []);

  useEffect(() => { loadServers(); }, [loadServers]);

  const handleDeploy = async (id) => {
    setDeploying((prev) => new Set(prev).add(id));
    try {
      const res = await fetch(`${FORWARD_API}/${id}/deploy`, { method: 'POST' });
      const json = await res.json();
      if (json.success) {
        toast.success('部署任务已提交');
        let attempts = 0;
        const poll = setInterval(async () => {
          attempts++;
          try {
            // 按 ID 轮询单条状态：列表 limit=1 窗口会被其它规则的更新挤出
            const r = await fetch(`${FORWARD_API}/${id}/status`);
            const j = await r.json();
            const status = j.data?.apply_status;
            const finished = status === 'running' || status === 'failed' || status === 'stopped';
            if (!finished && attempts <= 10) return;
            clearInterval(poll);
            setDeploying((prev) => { const n = new Set(prev); n.delete(id); return n; });
            loadForwards();
            if (status === 'running') toast.success('部署成功');
            else if (status === 'failed') toast.error(j.data?.last_error || '部署失败');
          } catch (e) {
            clearInterval(poll);
            setDeploying((prev) => { const n = new Set(prev); n.delete(id); return n; });
          }
        }, 3000);
      } else {
        toast.error(json.error || '部署失败');
        setDeploying((prev) => { const n = new Set(prev); n.delete(id); return n; });
      }
    } catch (e) {
      toast.error('部署请求失败');
      setDeploying((prev) => { const n = new Set(prev); n.delete(id); return n; });
    }
  };

  const handleStop = async (id) => {
    setActingKey(`stop:${id}`, true);
    try {
      const res = await fetch(`${FORWARD_API}/${id}/stop`, { method: 'POST' });
      const json = await res.json();
      if (json.success) { toast.success('已停止'); loadForwards(); }
      else toast.error(json.error || '停止失败');
    } catch (e) { toast.error('停止请求失败'); }
    finally { setActingKey(`stop:${id}`, false); }
  };

  const handleStart = async (id) => {
    setActingKey(`start:${id}`, true);
    try {
      const res = await fetch(`${FORWARD_API}/${id}/start`, { method: 'POST' });
      const json = await res.json();
      if (json.success) { toast.success('已启动'); loadForwards(); }
      else toast.error(json.error || '启动失败');
    } catch (e) { toast.error('启动请求失败'); }
    finally { setActingKey(`start:${id}`, false); }
  };

  const handleDelete = (fwd) => {
    if (!confirmPress(`fwd:${fwd.id}`, `删除转发规则「${fwd.name}」`)) return;
    setIsDeleting(true);
    (async () => {
      try {
        const res = await fetch(`${FORWARD_API}/${fwd.id}?cascade=1`, { method: 'DELETE' });
        const json = await res.json();
        if (json.success) { toast.success('已删除'); loadForwards(); }
        else toast.error(json.error || '删除失败');
      } catch (e) { toast.error('删除请求失败'); }
      finally { setIsDeleting(false); }
    })();
  };

  const handleCreate = async (data) => {
    try {
      const res = await fetch(FORWARD_API, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      });
      const json = await res.json();
      if (json.success) {
        toast.success('创建成功');
        loadForwards();
        handleDeploy(json.data.id);
        // token 模式：弹窗停留展示一次性访问令牌，用户复制后再关闭
        if (!json.access_token) {
          setDialogOpen(false);
          setEditingForward(null);
        }
      } else {
        toast.error(json.error || '创建失败');
      }
      return json;
    } catch (e) { toast.error('创建请求失败'); }
  };

  const handleUpdate = async (id, data) => {
    try {
      const res = await fetch(`${FORWARD_API}/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      });
      const json = await res.json();
      if (json.success) {
        toast.success('更新成功');
        setDialogOpen(false);
        setEditingForward(null);
        loadForwards();
      } else {
        toast.error(json.error || '更新失败');
      }
    } catch (e) { toast.error('更新请求失败'); }
  };

  const openCreate = () => {
    setEditingForward(null);
    setDialogOpen(true);
  };

  useImperativeHandle(ref, () => ({
    openCreate,
  }), []);

  const openEdit = (row) => {
    setEditingForward(row);
    setDialogOpen(true);
  };

  return (
    <div className="flex min-w-0 flex-col gap-3">
      {loading && (
        <div className="flex items-center justify-center py-20"><Loader /></div>
      )}

      {!loading && (
        <ForwardCanvas
          forwards={forwards}
          servers={servers}
          deploying={deploying}
          acting={acting}
          onEdit={openEdit}
          onDeploy={handleDeploy}
          onStop={handleStop}
          onStart={handleStart}
          onDelete={handleDelete}
          deleteConfirmActive={isArmed}
          isDeleting={isDeleting}
        />
      )}

      <ForwardDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSubmit={editingForward ? (data) => handleUpdate(editingForward.id, data) : handleCreate}
        servers={servers}
        editing={editingForward}
      />
    </div>
  );
});

export default ForwardPanel;
