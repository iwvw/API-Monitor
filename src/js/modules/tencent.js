/**
 * Tencent Cloud 模块前端逻辑
 */

import { store } from '../store.js';
import { toast } from './toast.js';

export const tencentMethods = {
    // 切换到腾讯云标签页
    async tencentSwitchTo() {
        if (store.tencentAccounts.length === 0) {
            await this.tencentLoadAccounts();
        }

        if (store.tencentSelectedAccountId) {
            if (store.tencentActiveSubTab === 'dns') this.tencentLoadDomains();
            if (store.tencentActiveSubTab === 'cvm') this.tencentLoadCvm();
            if (store.tencentActiveSubTab === 'lighthouse') this.tencentLoadLighthouse();
        }
    },

    // 账号管理
    async tencentLoadAccounts() {
        try {
            const res = await fetch('/api/tencent/accounts', {
                headers: store.getAuthHeaders()
            });
            if (!res.ok) {
                const text = await res.text();
                toast.error(`加载账号失败 (${res.status}): ${text.slice(0, 30)}`);
                return;
            }
            const data = await res.json();
            store.tencentAccounts = data;

            // 自动选中第一个账号
            if (data.length > 0 && !store.tencentSelectedAccountId) {
                this.tencentSelectAccount(data[0]);
            }
        } catch (e) {
            toast.error('请求腾讯云账号接口失败: ' + e.message);
        }
    },

    tencentSelectAccount(acc) {
        store.tencentSelectedAccountId = acc.id;
        // 切换账号后刷新当前子页签数据
        this.tencentSwitchTo();
    },

    tencentOpenAddAccountModal() {
        store.tencentAccountForm = { id: null, name: '', secretId: '', secretKey: '', regionId: 'ap-guangzhou', description: '' };
        store.showTencentAccountModal = true;
    },

    tencentEditAccount(acc) {
        store.tencentAccountForm = { ...acc, secretKey: '' };
        store.showTencentAccountModal = true;
    },

    async tencentSubmitAccount() {
        const isEdit = !!store.tencentAccountForm.id;
        const url = isEdit ? `/api/tencent/accounts/${store.tencentAccountForm.id}` : '/api/tencent/accounts';
        const method = isEdit ? 'PUT' : 'POST';

        try {
            const res = await fetch(url, {
                method,
                headers: { ...store.getAuthHeaders(), 'Content-Type': 'application/json' },
                body: JSON.stringify(store.tencentAccountForm)
            });
            const data = await res.json();
            if (data.success || data.id) {
                toast.success(isEdit ? '账号更新成功' : '账号添加成功');
                store.showTencentAccountModal = false;
                await this.tencentLoadAccounts();
            } else {
                toast.error(data.error || '操作失败');
            }
        } catch (e) {
            toast.error('请求失败: ' + e.message);
        }
    },

    async tencentDeleteAccount(id) {
        if (!confirm('确定要删除此腾讯云账号吗？')) return;
        try {
            const res = await fetch(`/api/tencent/accounts/${id}`, {
                method: 'DELETE',
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            if (data.success) {
                toast.success('账号已删除');
                if (store.tencentSelectedAccountId === id) store.tencentSelectedAccountId = null;
                await this.tencentLoadAccounts();
            }
        } catch (e) {
            toast.error('删除失败');
        }
    },

    // DNS 管理
    async tencentLoadDomains() {
        if (!store.tencentSelectedAccountId) return;
        store.tencentLoadingDomains = true;
        try {
            const res = await fetch(`/api/tencent/accounts/${store.tencentSelectedAccountId}/domains`, {
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            store.tencentDomains = data.domains || [];
        } catch (e) {
            toast.error('加载域名列表失败');
        } finally {
            store.tencentLoadingDomains = false;
        }
    },

    tencentSelectDomain(domain) {
        store.tencentSelectedDomain = domain;
        this.tencentLoadRecords(domain.Name);
    },

    async tencentLoadRecords(domain) {
        store.tencentLoadingRecords = true;
        try {
            const res = await fetch(`/api/tencent/accounts/${store.tencentSelectedAccountId}/domains/${domain}/records`, {
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            store.tencentRecords = data.records || [];
        } catch (e) {
            toast.error('加载记录失败');
        } finally {
            store.tencentLoadingRecords = false;
        }
    },

    tencentOpenAddDomainModal() {
        store.tencentAddDomainName = '';
        store.showTencentAddDomainModal = true;
    },

    async tencentSubmitAddDomain() {
        if (!store.tencentAddDomainName) return;
        try {
            const res = await fetch(`/api/tencent/accounts/${store.tencentSelectedAccountId}/domains`, {
                method: 'POST',
                headers: { ...store.getAuthHeaders(), 'Content-Type': 'application/json' },
                body: JSON.stringify({ domain: store.tencentAddDomainName })
            });
            const data = await res.json();
            if (data.success) {
                toast.success('域名添加成功');
                store.showTencentAddDomainModal = false;
                this.tencentLoadDomains();
            } else {
                toast.error(data.error);
            }
        } catch (e) {
            toast.error('添加失败');
        }
    },

    async tencentDeleteDomain(domain) {
        if (!confirm(`确定要删除域名 ${domain} 吗？`)) return;
        try {
            const res = await fetch(`/api/tencent/accounts/${store.tencentSelectedAccountId}/domains/${domain}`, {
                method: 'DELETE',
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            if (data.success) {
                toast.success('域名已删除');
                store.tencentSelectedDomain = null;
                this.tencentLoadDomains();
            }
        } catch (e) {
            toast.error('删除域名失败');
        }
    },

    // 实例管理 (CVM / Lighthouse)
    async tencentLoadCvm() {
        if (!store.tencentSelectedAccountId) return;
        store.tencentLoadingCvm = true;
        try {
            const res = await fetch(`/api/tencent/accounts/${store.tencentSelectedAccountId}/cvm`, {
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            store.tencentCvmInstances = data.instances || [];
        } catch (e) {
            toast.error('加载 CVM 失败');
        } finally {
            store.tencentLoadingCvm = false;
        }
    },

    async tencentLoadLighthouse() {
        if (!store.tencentSelectedAccountId) return;
        store.tencentLoadingLighthouse = true;
        try {
            const res = await fetch(`/api/tencent/accounts/${store.tencentSelectedAccountId}/lighthouse`, {
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            store.tencentLighthouseInstances = data.instances || [];
        } catch (e) {
            toast.error('加载轻量服务器失败');
        } finally {
            store.tencentLoadingLighthouse = false;
        }
    },

    async tencentControlCvm(ins, action) {
        try {
            const res = await fetch(`/api/tencent/accounts/${store.tencentSelectedAccountId}/cvm/${ins.InstanceId}/control`, {
                method: 'POST',
                headers: { ...store.getAuthHeaders(), 'Content-Type': 'application/json' },
                body: JSON.stringify({ action, region: ins.Placement.Zone.split('-').slice(0, 2).join('-') }) // 简单处理 zone 到 region
            });
            const data = await res.json();
            if (data.success) {
                toast.success(`${action} 指令已发送`);
                this.tencentLoadCvm();
            }
        } catch (e) {
            toast.error('操作失败');
        }
    },

    async tencentControlLighthouse(ins, action) {
        try {
            const res = await fetch(`/api/tencent/accounts/${store.tencentSelectedAccountId}/lighthouse/${ins.InstanceId}/control`, {
                method: 'POST',
                headers: { ...store.getAuthHeaders(), 'Content-Type': 'application/json' },
                body: JSON.stringify({ action, region: ins.Region })
            });
            const data = await res.json();
            if (data.success) {
                toast.success(`${action} 指令已发送`);
                this.tencentLoadLighthouse();
            }
        } catch (e) {
            toast.error('操作失败');
        }
    },

    tencentRefreshData() {
        if (store.tencentActiveSubTab === 'dns') this.tencentLoadDomains();
        if (store.tencentActiveSubTab === 'cvm') this.tencentLoadCvm();
        if (store.tencentActiveSubTab === 'lighthouse') this.tencentLoadLighthouse();
    },

    // 解析记录相关
    tencentOpenAddRecordModal() {
        store.tencentRecordForm = { subDomain: '', recordType: 'A', value: '', ttl: 600, mx: 10 };
        store.tencentEditingRecordId = null;
        store.showTencentRecordModal = true;
    },

    tencentEditRecord(record) {
        store.tencentRecordForm = {
            subDomain: record.Name,
            recordType: record.Type,
            value: record.Value,
            ttl: record.TTL,
            mx: record.MX
        };
        store.tencentEditingRecordId = record.RecordId;
        store.showTencentRecordModal = true;
    },

    async tencentSubmitRecord() {
        const domain = store.tencentSelectedDomain.Name;
        const isEdit = !!store.tencentEditingRecordId;
        const url = isEdit
            ? `/api/tencent/accounts/${store.tencentSelectedAccountId}/domains/${domain}/records/${store.tencentEditingRecordId}`
            : `/api/tencent/accounts/${store.tencentSelectedAccountId}/domains/${domain}/records`;
        const method = isEdit ? 'PUT' : 'POST';

        try {
            const res = await fetch(url, {
                method,
                headers: { ...store.getAuthHeaders(), 'Content-Type': 'application/json' },
                body: JSON.stringify(store.tencentRecordForm)
            });
            const data = await res.json();
            if (data.success) {
                toast.success('保存成功');
                store.showTencentRecordModal = false;
                this.tencentLoadRecords(domain);
            }
        } catch (e) {
            toast.error('保存失败');
        }
    },

    async tencentDeleteRecord(recordId) {
        if (!confirm('确定删除此记录吗？')) return;
        const domain = store.tencentSelectedDomain.Name;
        try {
            const res = await fetch(`/api/tencent/accounts/${store.tencentSelectedAccountId}/domains/${domain}/records/${recordId}`, {
                method: 'DELETE',
                headers: store.getAuthHeaders()
            });
            const data = await res.json();
            if (data.success) {
                toast.success('记录已删除');
                this.tencentLoadRecords(domain);
            }
        } catch (e) {
            toast.error('删除失败');
        }
    },

    async tencentToggleRecordStatus(record) {
        const domain = store.tencentSelectedDomain.Name;
        const newStatus = record.Status === 'ENABLE' ? 'DISABLE' : 'ENABLE';
        try {
            const res = await fetch(`/api/tencent/accounts/${store.tencentSelectedAccountId}/domains/${domain}/records/${record.RecordId}/status`, {
                method: 'PATCH',
                headers: { ...store.getAuthHeaders(), 'Content-Type': 'application/json' },
                body: JSON.stringify({ status: newStatus })
            });
            const data = await res.json();
            if (data.success) {
                toast.success(`状态已切换为 ${newStatus === 'ENABLE' ? '开启' : '关闭'}`);
                this.tencentLoadRecords(domain);
            }
        } catch (e) {
            toast.error('切换状态失败');
        }
    }
};
