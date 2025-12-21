import { store } from '../store.js';
import { toast } from './toast.js';

/**
 * 模板模块前端逻辑
 * 将 {{module_name}} 替换为你的模块名称
 */
export const templateMethods = {
    // 初始化数据
    async init{{ModuleName}}() {
        await this.load{{ModuleName}}Accounts();
        this.fetch{{ModuleName}}Data();
    },

    // 加载账号
    async load{{ModuleName}}Accounts() {
        try {
            const response = await fetch('/api/{{module_name}}/accounts', {
                headers: store.getAuthHeaders()
            });
            const accounts = await response.json();
            store.{{module_name}}Accounts = accounts;
        } catch (error) {
            console.error('加载账号失败:', error);
        }
    },

    // 获取业务数据
    async fetch{{ModuleName}}Data() {
        if (store.refreshing) return;
        store.loading = true;
        store.refreshing = true;

        try {
            // 实现数据获取逻辑
            // const res = await fetch('/api/{{module_name}}/data', ...);
            // store.{{module_name}}Data = await res.json();
        } catch (error) {
            toast.error('获取数据失败: ' + error.message);
        } finally {
            store.loading = false;
            store.refreshing = false;
        }
    },

    // 添加账号
    async add{{ModuleName}}Account() {
        if (!this.newAccount.name || !this.newAccount.token) {
            toast.error('请完整填写信息');
            return;
        }

        try {
            const res = await fetch('/api/{{module_name}}/validate-account', {
                method: 'POST',
                headers: store.getAuthHeaders(),
                body: JSON.stringify(this.newAccount)
            });
            const data = await res.json();

            if (res.ok) {
                store.{{module_name}}Accounts.push({ ...this.newAccount, status: 'active' });
                await this.save{{ModuleName}}Accounts();
                toast.success('添加成功');
                this.newAccount = { name: '', token: '' };
            } else {
                toast.error(data.error || '验证失败');
            }
        } catch (error) {
            toast.error('添加失败: ' + error.message);
        }
    },

    // 保存账号到服务器
    async save{{ModuleName}}Accounts() {
        try {
            const serverAccounts = store.{{module_name}}Accounts.filter(a => !a.isEnv);
            await fetch('/api/{{module_name}}/server-accounts', {
                method: 'POST',
                headers: store.getAuthHeaders(),
                body: JSON.stringify({ accounts: serverAccounts })
            });
        } catch (error) {
            console.error('保存失败:', error);
        }
    }
};
