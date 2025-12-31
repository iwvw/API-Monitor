/**
 * 模板加载器 - 用于动态加载各模块的 HTML 模板
 * Refactored for Vite to bundle templates
 */

// 使用 Vite 的 glob import 导入所有模板 (lazy load)
const templateModules = import.meta.glob('../templates/*.html', {
  query: '?raw',
  import: 'default',
  eager: false, // Lazy loading
});

const TemplateLoader = {
  // 模板映射关系 (filename -> selector)
  templateMap: {
    'auth.html': '#template-auth',
    'dashboard.html': '#template-dashboard',
    'paas.html': '#template-paas',
    'dns.html': '#template-dns',
    'gemini-cli.html': '#template-gemini-cli',
    'openai.html': '#template-openai',
    'server.html': '#template-server',
    'self-h.html': '#template-self-h',
    'antigravity.html': '#template-antigravity',
    'settings.html': '#template-settings',
    'modals.html': '#template-modals',
    'r2.html': '#template-r2',
    'totp.html': '#template-totp',
    'music.html': '#template-music',
  },

  // define critical templates for initial load
  criticalTemplates: ['auth.html', 'dashboard.html', 'modals.html'],

  /**
   * 加载指定模板集合
   * @param {Array<string>} filenames - 想要加载的模板文件名列表 (为空则加载所有)
   * @returns {Promise<void>}
   */
  async loadTemplates(filenames = []) {
    const promises = [];

    for (const path in templateModules) {
      const filename = path.split('/').pop();
      // 如果指定了 filenames，则只加载匹配的；否则加载所有
      if (filenames.length > 0 && !filenames.includes(filename)) {
        continue;
      }

      const targetSelector = this.templateMap[filename];
      const loadPromise = templateModules[path]().then(htmlContent => {
        if (targetSelector) {
          const container = document.querySelector(targetSelector);
          if (container) {
            // 避免重复内容
            if (!container.innerHTML) {
              container.innerHTML = htmlContent;
            }
          } else {
            console.warn(`[TemplateLoader] Container ${targetSelector} not found for ${filename}`);
          }
        }
      });
      promises.push(loadPromise);
    }

    await Promise.all(promises);
  },

  async loadCritical() {
    console.log('[TemplateLoader] Loading critical templates...');
    const startTime = performance.now();
    await this.loadTemplates(this.criticalTemplates);
    console.log(
      `[TemplateLoader] Critical templates loaded in ${Math.round(performance.now() - startTime)}ms`
    );
  },

  async loadBackground() {
    console.log('[TemplateLoader] Loading background templates...');
    // Load everything NOT in critical
    const allFiles = Object.keys(templateModules).map(p => p.split('/').pop());
    const backgroundFiles = allFiles.filter(f => !this.criticalTemplates.includes(f));
    await this.loadTemplates(backgroundFiles);
    console.log('[TemplateLoader] Background templates loaded');
  },

  /**
   * 加载所有模板 (Backward Compatibility)
   * 并行加载所有模板以提高速度
   */
  async loadAll() {
    console.log('[TemplateLoader] Loading ALL templates...');
    const startTime = performance.now();
    await Promise.all([this.loadCritical(), this.loadBackground()]);
    console.log(
      `[TemplateLoader] All templates loaded in ${Math.round(performance.now() - startTime)}ms`
    );
    return true;
  },

  // 兼容旧 API，虽然不再需要单独加载
  async loadTemplate(config) {
    console.warn('[TemplateLoader] loadTemplate rule is deprecated in bundled mode');
    return { target: config.target, html: '' };
  },

  clearCache() {
    console.log('[TemplateLoader] Cache clear not needed in bundled mode');
  },

  setVersion(v) {
    // No-op
  },
};

// 导出给全局使用
window.TemplateLoader = TemplateLoader;

export default TemplateLoader;
