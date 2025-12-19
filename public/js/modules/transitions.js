/**
 * 页面切换动画模块
 * 负责处理主标签页和子标签页的切换动画
 */

export const transitionsMethods = {
  // 获取标签页动画类
  getTabAnimationClass(tabName) {
    if (!this.previousMainTab) {
      // 首次加载，使用淡入动画
      return 'fade-in';
    }

    // 获取标签页的索引
    const currentIndex = this.moduleOrder.indexOf(tabName);
    const previousIndex = this.moduleOrder.indexOf(this.previousMainTab);

    // 根据切换方向选择动画
    if (currentIndex > previousIndex) {
      // 向右切换
      return 'slide-in-right';
    } else if (currentIndex < previousIndex) {
      // 向左切换
      return 'slide-in-left';
    } else {
      // 同一个标签页，使用淡入
      return 'fade-in';
    }
  },

  // 切换到 DNS 模块
  switchToDns() {
    this.previousMainTab = this.mainActiveTab;
    this.mainActiveTab = 'dns';

    // 首次切换到 DNS 时加载数据
    if (!this.dnsAccounts || this.dnsAccounts.length === 0) {
      this.loadDnsAccounts();
    }
  },

  // 切换到 OpenAI 模块
  switchToOpenai() {
    this.previousMainTab = this.mainActiveTab;
    this.mainActiveTab = 'openai';

    // 首次切换到 OpenAI 时加载数据
    if (!this.openaiEndpoints || this.openaiEndpoints.length === 0) {
      this.loadOpenaiEndpoints();
    }
  },

  // 切换到 Zeabur 模块
  switchToZeabur() {
    this.previousMainTab = this.mainActiveTab;
    this.mainActiveTab = 'zeabur';

    // 首次切换到 Zeabur 时加载数据
    if (!this.accounts || this.accounts.length === 0) {
      this.fetchData();
    }
  },

  // 平滑滚动到顶部
  smoothScrollToTop() {
    window.scrollTo({
      top: 0,
      behavior: 'smooth'
    });
  },

  // 切换标签页时的通用处理
  handleTabSwitch(newTab) {
    // 如果点击的是当前标签页，不执行任何操作
    if (this.mainActiveTab === newTab) {
      return;
    }

    // 清除之前的防抖定时器
    if (this.tabSwitchDebounce) {
      clearTimeout(this.tabSwitchDebounce);
    }

    // 保存上一个标签页
    this.previousMainTab = this.mainActiveTab;

    // 切换到新标签页
    this.mainActiveTab = newTab;

    // 滚动到顶部
    this.smoothScrollToTop();

    // 使用防抖延迟加载数据，避免频繁切换时重复请求
    this.tabSwitchDebounce = setTimeout(() => {
      this.$nextTick(() => {
        switch (newTab) {
          case 'dns':
            if (!this.dnsAccounts || this.dnsAccounts.length === 0) {
              this.loadDnsAccounts();
            }
            if (!this.dnsTemplates || this.dnsTemplates.length === 0) {
              this.loadDnsTemplates();
            }
            break;
          case 'openai':
            if (!this.openaiEndpoints || this.openaiEndpoints.length === 0) {
              this.loadOpenaiEndpoints();
            }
            break;
          case 'zeabur':
            if (!this.accounts || this.accounts.length === 0) {
              this.fetchData();
            }
            break;
          case 'antigravity':
            if (!this.antigravityAccounts || this.antigravityAccounts.length === 0) {
              this.loadAntigravityAccounts();
            }
            break;
          case 'gemini-cli':
            this.initGeminiCli();
            break;
        }
      });
    }, 150);
  },

  // 为列表项添加交错动画
  addStaggerAnimation(selector, delay = 50) {
    this.$nextTick(() => {
      const items = document.querySelectorAll(selector);
      items.forEach((item, index) => {
        item.style.animationDelay = `${index * delay}ms`;
        item.classList.add('stagger-item');
      });
    });
  },

  // 移除交错动画类
  removeStaggerAnimation(selector) {
    const items = document.querySelectorAll(selector);
    items.forEach(item => {
      item.classList.remove('stagger-item');
      item.style.animationDelay = '';
    });
  }
};
