/**
 * 模板加载器 - 用于动态加载各模块的 HTML 模板
 * Refactored for Vite to bundle templates
 */

// 使用 Vite 的 glob import 导入所有模板为原始字符串
const templateModules = import.meta.glob('../templates/*.html', { query: '?raw', import: 'default', eager: true });

const TemplateLoader = {
    // 模板映射关系 (filename -> selector)
    templateMap: {
        'auth.html': '#template-auth',
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
    },

    /**
     * 加载所有模板
     * @returns {Promise<void>}
     */
    async loadAll() {
        const startTime = performance.now();
        console.log('[TemplateLoader] Starting template loading (bundled)...');

        try {
            let loadedCount = 0;

            for (const path in templateModules) {
                const filename = path.split('/').pop();
                const targetSelector = this.templateMap[filename];
                const htmlContent = templateModules[path];

                if (targetSelector) {
                    const container = document.querySelector(targetSelector);
                    if (container) {
                        container.innerHTML = htmlContent;
                        loadedCount++;
                    } else {
                        console.warn(`[TemplateLoader] Container ${targetSelector} not found for ${filename}`);
                    }
                }
            }

            const elapsed = Math.round(performance.now() - startTime);
            console.log(`[TemplateLoader] ${loadedCount} templates injected in ${elapsed}ms`);

            return true;
        } catch (error) {
            console.error('[TemplateLoader] Failed to load templates:', error);
            throw error;
        }
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
    }
};

// 导出给全局使用
window.TemplateLoader = TemplateLoader;

export default TemplateLoader;
