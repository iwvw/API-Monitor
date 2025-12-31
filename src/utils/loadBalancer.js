/**
 * 简单的负载均衡器状态管理
 * 用于维护 Round Robin (轮询) 的索引
 */
class LoadBalancer {
  constructor() {
    this.indices = new Map();
  }

  /**
   * 获取下一个账号
   * @param {string} scope - 作用域 (e.g., 'antigravity', 'gemini-cli')
   * @param {Array} accounts - 可用的账号列表
   * @param {string} strategy - 策略 ('random' | 'round_robin')
   * @returns {Object} 选中的账号
   */
  getNextAccount(scope, accounts, strategy = 'random') {
    if (!accounts || accounts.length === 0) return null;
    if (accounts.length === 1) return accounts[0];

    if (strategy === 'round_robin') {
      let currentIndex = this.indices.get(scope) || 0;

      // 确保索引在有效范围内
      if (currentIndex >= accounts.length) {
        currentIndex = 0;
      }

      const account = accounts[currentIndex];

      // 更新索引指向下一个
      this.indices.set(scope, (currentIndex + 1) % accounts.length);

      return account;
    } else {
      // 默认随机
      const randomIndex = Math.floor(Math.random() * accounts.length);
      return accounts[randomIndex];
    }
  }
}

// 单例导出
module.exports = new LoadBalancer();
