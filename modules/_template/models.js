const BaseModel = require('../../src/db/models/BaseModel');

/**
 * 模板账号模型
 * 将 {{module_name}} 替换为你的模块名称
 */
class TemplateAccount extends BaseModel {
  constructor() {
    super('{{module_name}}_accounts');
  }

  /**
   * 创建账号
   */
  createAccount(accountData) {
    const data = {
      id:
        accountData.id ||
        `{{module_prefix}}_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      name: accountData.name,
      token: accountData.token,
      status: accountData.status || 'active',
      config: accountData.config ? JSON.stringify(accountData.config) : null,
      created_at: accountData.created_at || new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };

    this.insert(data);
    return data;
  }

  /**
   * 更新账号
   */
  updateAccount(id, updates) {
    const allowedFields = ['name', 'token', 'status', 'config', 'last_synced_at'];
    const data = { updated_at: new Date().toISOString() };

    Object.keys(updates).forEach(key => {
      if (allowedFields.includes(key)) {
        data[key] = key === 'config' ? JSON.stringify(updates[key]) : updates[key];
      }
    });

    return this.update(id, data);
  }
}

/**
 * 模板项目模型
 */
class TemplateItem extends BaseModel {
  constructor() {
    super('{{module_name}}_items');
  }

  createItem(itemData) {
    const data = {
      id: itemData.id,
      account_id: itemData.account_id,
      name: itemData.name,
      type: itemData.type || null,
      status: itemData.status || null,
      data: itemData.data ? JSON.stringify(itemData.data) : null,
      created_at: itemData.created_at || new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };

    this.insert(data);
    return data;
  }

  getItemsByAccount(accountId) {
    return this.findWhere({ account_id: accountId });
  }
}

module.exports = {
  TemplateAccount: new TemplateAccount(),
  TemplateItem: new TemplateItem(),
};
