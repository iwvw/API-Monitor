/**
 * 阿里云前端模块
 */
import { store } from '../store.js';
import { toast } from './toast.js';

// 缓存 key
const ALIYUN_ACCOUNTS_CACHE_KEY = 'aliyun_accounts_cache';

export const aliyunMethods = {
    // ==================== 基础与账号 ====================

    aliyunInit() {
        // 尝试从缓存加载账号
        this.aliyunLoadAccountsFromCache();
    },

    aliyunSwitchTo() {
        store.mainActiveTab = 'aliyun';

        // 如果账号列表为空，加载账号列表
        if (store.aliyunAccounts.length === 0) {
            this.aliyunLoadAccounts();
        }

        // 默认显示 DNS 标签页
        if (!store.aliyunActiveSubTab) {
            store.aliyunActiveSubTab = 'dns';
        }

        // 只有在对应列表为空时才自动加载，提高切换速度
        if (store.aliyunSelectedAccountId) {
            if (store.aliyunActiveSubTab === 'dns' && store.aliyunDomains.length === 0) {
                this.aliyunLoadDomains();
            } else if (store.aliyunActiveSubTab === 'ecs' && store.aliyunInstances.length === 0) {
                this.aliyunLoadInstances();
            } else if (store.aliyunActiveSubTab === 'swas' && store.aliyunSwasInstances.length === 0) {
                this.aliyunLoadSwas();
            }
        }
    },

    // 强制刷新当前选中的标签页数据
    aliyunRefreshData() {
        if (!store.aliyunSelectedAccountId) return;

        if (store.aliyunActiveSubTab === 'dns') {
            this.aliyunLoadDomains();
        } else if (store.aliyunActiveSubTab === 'ecs') {
            this.aliyunLoadInstances();
        } else if (store.aliyunActiveSubTab === 'swas') {
            this.aliyunLoadSwas();
        }
    },

    aliyunLoadAccountsFromCache() {
        try {
            const cached = localStorage.getItem(ALIYUN_ACCOUNTS_CACHE_KEY);
            if (cached) {
                const data = JSON.parse(cached);
                store.aliyunAccounts = data;
                // 自动选择第一个
                if (data.length > 0 && !store.aliyunSelectedAccountId) {
                    store.aliyunSelectedAccountId = data[0].id;
                }
            }
        } catch (e) {
            console.warn('[Aliyun] 加载缓存失败', e);
        }
    },

    async aliyunLoadAccounts() {
        try {
            const res = await fetch('/api/aliyun/accounts', { headers: store.getAuthHeaders() });
            const data = await res.json();
            if (Array.isArray(data)) {
                // 将后端下划线命名字段转换为前端可用的驼峰命名 (如果需要的话，但目前模板已经改用下划线)
                // 为了万无一失，我们在前端同时也保留下划线名字
                store.aliyunAccounts = data;
                localStorage.setItem(ALIYUN_ACCOUNTS_CACHE_KEY, JSON.stringify(data));

                // 自动选择逻辑
                if (data.length > 0) {
                    const currentExists = data.find(a => a.id === store.aliyunSelectedAccountId);
                    if (!store.aliyunSelectedAccountId || !currentExists) {
                        this.aliyunSelectAccount(data[0]);
                    }
                }
            }
        } catch (e) {
            toast.error('加载账号失败: ' + e.message);
        }
    },

    aliyunSelectAccount(account) {
        store.aliyunSelectedAccountId = account.id;
        store.aliyunSelectedAccount = account;
        // 切换账号后，清空当前的数据并重新加载
        store.aliyunDomains = [];
        store.aliyunInstances = [];

        if (store.aliyunActiveSubTab === 'dns') {
            this.aliyunLoadDomains();
        } else if (store.aliyunActiveSubTab === 'ecs') {
            this.aliyunLoadInstances();
        }
    },

    aliyunOpenAddAccountModal() {
        store.aliyunAccountForm = {
            id: null, // null = 新增
            name: '',
            accessKeyId: '',
            accessKeySecret: '',
            regionId: 'cn-hangzhou',
            description: ''
        };
        store.showAliyunAccountModal = true;
    },

    aliyunEditAccount(account) {
        store.aliyunAccountForm = {
            id: account.id, // 有 id = 编辑
            name: account.name,
            accessKeyId: '', // 编辑时不预填 AccessKey
            accessKeySecret: '',
            regionId: account.region_id || 'cn-hangzhou',
            description: account.description || ''
        };
        store.showAliyunAccountModal = true;
    },

    async aliyunSubmitAccount() {
        if (!store.aliyunAccountForm.name) {
            toast.error('请填写账号名称');
            return;
        }

        // 新增时必须填写 AccessKey
        if (!store.aliyunAccountForm.id && (!store.aliyunAccountForm.accessKeyId || !store.aliyunAccountForm.accessKeySecret)) {
            toast.error('请填写 AccessKey ID 和 Secret');
            return;
        }

        try {
            const isEdit = !!store.aliyunAccountForm.id;
            const url = isEdit
                ? `/api/aliyun/accounts/${store.aliyunAccountForm.id}`
                : '/api/aliyun/accounts';

            const res = await fetch(url, {
                method: isEdit ? 'PUT' : 'POST',
                headers: store.getAuthHeaders(),
                body: JSON.stringify(store.aliyunAccountForm)
            });
            const data = await res.json();

            if (data.success) {
                toast.success(isEdit ? '账号已更新' : '账号添加成功');
                store.showAliyunAccountModal = false;
                this.aliyunLoadAccounts();
            } else {
                toast.error(data.error || '操作失败');
            }
        } catch (e) {
            toast.error('操作失败: ' + e.message);
        }
    },

    async aliyunDeleteAccount(account) {
        if (!await store.showConfirm({
            title: '删除账号',
            message: `确定要删除账号 "${account.name}" 吗？`,
            type: 'danger'
        })) return;

        try {
            const res = await fetch(`/api/aliyun/accounts/${account.id}`, {
                method: 'DELETE',
                headers: store.getAuthHeaders()
            });
            if (res.ok) {
                toast.success('账号已删除');
                this.aliyunLoadAccounts(true);
            } else {
                toast.error('删除失败');
            }
        } catch (e) {
            toast.error('删除失败: ' + e.message);
        }
    },

    // ==================== DNS ====================

    async aliyunLoadDomains() {
        if (!store.aliyunSelectedAccountId) return;

        store.aliyunLoadingDomains = true;
        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/domains?pageSize=50`, {
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            if (data.domains) {
                store.aliyunDomains = data.domains;
            } else {
                toast.error(data.error || '加载域名失败');
            }
        } catch (e) {
            toast.error('加载域名失败: ' + e.message);
        } finally {
            store.aliyunLoadingDomains = false;
        }
    },

    aliyunOpenAddDomainModal() {
        if (!store.aliyunSelectedAccountId) {
            toast.error('请先选择一个账号');
            return;
        }
        store.aliyunAddDomainName = '';
        store.aliyunAddDomainResult = null;
        store.showAliyunAddDomainModal = true;
    },

    async aliyunSubmitAddDomain() {
        if (!store.aliyunAddDomainName) {
            toast.error('请输入域名');
            return;
        }

        store.aliyunLoadingDomains = true;
        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/domains`, {
                method: 'POST',
                headers: store.getAuthHeaders(),
                body: JSON.stringify({ domainName: store.aliyunAddDomainName })
            });
            const data = await res.json();

            if (data.success) {
                toast.success(data.result.AlreadyExists ? '域名已在账号中' : '域名添加成功');
                store.aliyunAddDomainResult = data.result;
                this.aliyunLoadDomains();
            } else {
                toast.error(data.error || '添加域名失败');
            }
        } catch (e) {
            toast.error('添加域名失败: ' + e.message);
        } finally {
            store.aliyunLoadingDomains = false;
        }
    },

    aliyunSelectDomain(domain) {
        store.aliyunSelectedDomain = domain;
        this.aliyunLoadRecords(domain.DomainName);
    },

    async aliyunDeleteDomain(domainName) {
        if (!store.aliyunSelectedAccountId) return;
        if (!confirm(`确定要从阿里云账号中删除域名 ${domainName} 吗？\n此操作仅从阿里云解析中移除该域名，不会影响域名注册。`)) {
            return;
        }

        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/domains/${domainName}`, {
                method: 'DELETE',
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            if (data.success) {
                toast.success('域名已成功删除');
                if (store.aliyunSelectedDomain?.DomainName === domainName) {
                    store.aliyunSelectedDomain = null;
                }
                this.aliyunLoadDomains();
            } else {
                toast.error(data.error || '删除域名失败');
            }
        } catch (e) {
            toast.error('删除域名请求失败: ' + e.message);
        }
    },

    async aliyunLoadRecords(domainName) {
        if (!store.aliyunSelectedAccountId) return;

        store.aliyunLoadingRecords = true;
        store.aliyunRecords = [];
        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/domains/${domainName}/records?pageSize=100`, {
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            if (data.records) {
                store.aliyunRecords = data.records;
            }
        } catch (e) {
            toast.error('加载解析记录失败: ' + e.message);
        } finally {
            store.aliyunLoadingRecords = false;
        }
    },

    aliyunOpenAddRecordModal() {
        store.aliyunRecordForm = {
            rr: '',
            type: 'A',
            value: '',
            ttl: 600,
            priority: 10,
            line: 'default'
        };
        store.aliyunEditingRecordId = null;
        store.showAliyunRecordModal = true;
    },

    aliyunEditRecord(record) {
        store.aliyunRecordForm = {
            rr: record.RR,
            type: record.Type,
            value: record.Value,
            ttl: record.TTL,
            priority: record.Priority,
            line: record.Line
        };
        store.aliyunEditingRecordId = record.RecordId;
        store.showAliyunRecordModal = true;
    },

    async aliyunSubmitRecord() {
        if (!store.aliyunSelectedDomain) return;

        try {
            const domainName = store.aliyunSelectedDomain.DomainName;
            const url = store.aliyunEditingRecordId
                ? `/api/aliyun/accounts/${store.aliyunSelectedAccountId}/records/${store.aliyunEditingRecordId}`
                : `/api/aliyun/accounts/${store.aliyunSelectedAccountId}/domains/${domainName}/records`;

            const method = store.aliyunEditingRecordId ? 'PUT' : 'POST';

            const res = await fetch(url, {
                method: method,
                headers: store.getAuthHeaders(),
                body: JSON.stringify(store.aliyunRecordForm)
            });

            const data = await res.json();
            if (data.success) {
                toast.success(store.aliyunEditingRecordId ? '记录已修改' : '记录已添加');
                store.showAliyunRecordModal = false;
                this.aliyunLoadRecords(domainName);
            } else {
                toast.error(data.error || '操作失败');
            }
        } catch (e) {
            toast.error('操作失败: ' + e.message);
        }
    },

    async aliyunDeleteRecord(recordId) {
        if (!await store.showConfirm({ title: '删除记录', message: '确定删除该解析记录吗？' })) return;

        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/records/${recordId}`, {
                method: 'DELETE',
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            if (data.success) {
                toast.success('记录已删除');
                this.aliyunLoadRecords(store.aliyunSelectedDomain.DomainName);
            } else {
                toast.error(data.error || '删除失败');
            }
        } catch (e) {
            toast.error('删除失败: ' + e.message);
        }
    },

    async aliyunToggleRecordStatus(record) {
        const newStatus = record.Status === 'ENABLE' ? 'Disable' : 'Enable';
        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/records/${record.RecordId}/status`, {
                method: 'PUT',
                headers: store.getAuthHeaders(),
                body: JSON.stringify({ status: newStatus })
            });
            const data = await res.json();
            if (data.success) {
                toast.success(newStatus === 'Enable' ? '记录已启用' : '记录已暂停');
                // 局部更新状态
                record.Status = newStatus.toUpperCase();
            } else {
                toast.error(data.error || '操作失败');
            }
        } catch (e) {
            toast.error('操作失败: ' + e.message);
        }
    },

    // ==================== ECS ====================

    async aliyunLoadInstances() {
        if (!store.aliyunSelectedAccountId) return;

        store.aliyunLoadingInstances = true;
        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/instances?pageSize=50`, {
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            if (data.instances) {
                store.aliyunInstances = data.instances;
            } else {
                toast.error(data.error || '加载实例失败');
            }
        } catch (e) {
            toast.error('加载实例失败: ' + e.message);
        } finally {
            store.aliyunLoadingInstances = false;
        }
    },

    async aliyunControlInstance(instance, action) {
        // action: start, stop, reboot
        const actionMap = {
            start: '启动',
            stop: '停止',
            reboot: '重启'
        };

        if (!await store.showConfirm({
            title: `${actionMap[action]}实例`,
            message: `确定要${actionMap[action]}实例 "${instance.InstanceName || instance.InstanceId}" 吗？`,
            type: action === 'stop' ? 'warning' : 'info'
        })) return;

        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/instances/${instance.InstanceId}/${action}`, {
                method: 'POST',
                headers: store.getAuthHeaders(),
                body: JSON.stringify({
                    regionId: instance.RegionId,
                    force: false
                })
            });
            const data = await res.json();
            if (data.success) {
                toast.success(`指令已发送: ${actionMap[action]}`);
                // 稍后刷新状态
                setTimeout(() => this.aliyunLoadInstances(), 3000);
            } else {
                toast.error(data.error || '操作失败');
            }
        } catch (e) {
            toast.error('操作失败: ' + e.message);
        }
    },

    // ==================== 轻量应用服务器 (SWAS) ====================

    async aliyunLoadSwas() {
        if (!store.aliyunSelectedAccountId) return;

        store.aliyunLoadingSwas = true;
        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/swas?pageSize=50`, {
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            if (data.instances) {
                store.aliyunSwasInstances = data.instances;
            } else {
                toast.error(data.error || '加载轻量服务器失败');
            }
        } catch (e) {
            toast.error('加载轻量服务器失败: ' + e.message);
        } finally {
            store.aliyunLoadingSwas = false;
        }
    },

    async aliyunControlSwasInstance(instance, action) {
        // action: start, stop, reboot
        const actionMap = {
            start: '启动',
            stop: '停止',
            reboot: '重启'
        };

        if (!await store.showConfirm({
            title: `${actionMap[action]}轻量服务器`,
            message: `确定要${actionMap[action]}实例 "${instance.InstanceName || instance.InstanceId}" 吗？`,
            type: action === 'stop' ? 'warning' : 'info'
        })) return;

        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/swas/${instance.InstanceId}/${action}`, {
                method: 'POST',
                headers: store.getAuthHeaders(),
                body: JSON.stringify({
                    regionId: instance.RegionId,
                    force: false
                })
            });
            const data = await res.json();
            if (data.success) {
                toast.success(`指令已发送: ${actionMap[action]}`);
                // 稍后刷新状态
                setTimeout(() => this.aliyunLoadSwas(), 3000);
            } else {
                toast.error(data.error || '操作失败');
            }
        } catch (e) {
            toast.error('操作失败: ' + e.message);
        }
    },

    // ==================== 防火墙与监控 ====================

    async aliyunLoadFirewall(instance) {
        store.aliyunSelectedInstance = instance;
        store.aliyunLoadingFirewall = true;
        store.aliyunFirewallRules = [];
        store.showAliyunFirewallDrawer = true;

        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/swas/${instance.InstanceId}/firewall?regionId=${instance.RegionId}`, {
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            if (Array.isArray(data)) {
                store.aliyunFirewallRules = data;
            } else {
                toast.error(data.error || '加载防火墙规则失败');
            }
        } catch (e) {
            toast.error('加载防火墙规则失败: ' + e.message);
        } finally {
            store.aliyunLoadingFirewall = false;
        }
    },

    async aliyunDeleteFirewallRule(ruleId) {
        const instance = store.aliyunSelectedInstance;
        if (!instance) return;

        if (!await store.showConfirm({
            title: '删除规则',
            message: '确定要删除此防火墙规则吗？',
            type: 'danger'
        })) return;

        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/swas/${instance.InstanceId}/firewall/${ruleId}?regionId=${instance.RegionId}`, {
                method: 'DELETE',
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            if (data.success) {
                toast.success('规则已删除');
                this.aliyunLoadFirewall(instance);
            } else {
                toast.error(data.error || '删除失败');
            }
        } catch (e) {
            toast.error('删除失败: ' + e.message);
        }
    },

    async aliyunSubmitFirewallRule() {
        const instance = store.aliyunSelectedInstance;
        if (!instance) return;

        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/swas/${instance.InstanceId}/firewall`, {
                method: 'POST',
                headers: store.getAuthHeaders(),
                body: JSON.stringify({
                    regionId: instance.RegionId,
                    rule: store.aliyunFirewallForm
                })
            });
            const data = await res.json();
            if (data.success) {
                toast.success('规则已添加');
                store.showAliyunAddFirewallModal = false;
                this.aliyunLoadFirewall(instance);
            } else {
                toast.error(data.error || '添加失败');
            }
        } catch (e) {
            toast.error('添加失败: ' + e.message);
        }
    },

    async aliyunLoadMetrics(instance, type = 'ECS') {
        store.aliyunSelectedInstance = instance;
        store.showAliyunMetricsDrawer = true;

        const dimensions = type === 'ECS' ? { instanceId: instance.InstanceId } : { instanceId: instance.InstanceId };
        const namespace = type === 'ECS' ? 'acs_ecs_dashboard' : 'acs_swas';
        const metricName = 'CPUUtilization'; // 简化先取 CPU

        try {
            const res = await fetch(`/api/aliyun/accounts/${store.aliyunSelectedAccountId}/metrics`, {
                method: 'POST',
                headers: store.getAuthHeaders(),
                body: JSON.stringify({
                    namespace,
                    metricName,
                    dimensions,
                    startTime: new Date(Date.now() - 3600000).toISOString(),
                    endTime: new Date().toISOString(),
                    period: '60'
                })
            });
            const data = await res.json();
            if (data.Datapoints) {
                const points = JSON.parse(data.Datapoints);
                // 按日期排序
                points.sort((a, b) => a.timestamp - b.timestamp);
                store.aliyunMetrics[instance.InstanceId] = points;
                return points;
            }
        } catch (e) {
            console.error('Fetch metrics failed', e);
        }
        return null;
    },
};
