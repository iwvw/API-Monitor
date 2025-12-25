const { defineConfig, loadEnv } = require('vite');
const path = require('path');
const { createHtmlPlugin } = require('vite-plugin-html');
const { getAllCdnUrls, getExternals, getGlobals } = require('./cdn.config');

module.exports = defineConfig(({ mode }) => {
    // 加载环境变量
    const env = loadEnv(mode, process.cwd(), '');

    // 是否启用 CDN
    const useCdn = env.VITE_USE_CDN === 'true';
    // CDN 提供商 (默认 npmmirror)
    const cdnProvider = env.VITE_CDN_PROVIDER || 'npmmirror';

    // 获取 CDN URLs
    const cdnUrls = useCdn ? getAllCdnUrls(cdnProvider) : { js: [], css: [] };

    // 生成 CDN 脚本标签 (普通 script，不是 module)
    const cdnScriptTags = cdnUrls.js.map(item =>
        `<script src="${item.url}"></script>`
    ).join('\n    ');

    // 生成 CDN 样式标签
    const cdnStyleTags = cdnUrls.css.map(url =>
        `<link rel="stylesheet" href="${url}">`
    ).join('\n    ');

    console.log(`\n[Vite Config] CDN 模式: ${useCdn ? '启用 (' + cdnProvider + ')' : '禁用'}\n`);
    if (useCdn) {
        console.log('[Vite Config] CDN 资源:');
        cdnUrls.js.forEach(item => console.log(`  - ${item.pkg} -> ${item.global}`));
        cdnUrls.css.forEach(url => console.log(`  - CSS: ${url}`));
        console.log('');
    }

    return {
        root: 'src',
        publicDir: '../public', // 使用项目根目录的 public 文件夹
        resolve: {
            alias: {
                // CDN 模式下使用 shim 从全局变量导出 Vue API
                // 非 CDN 模式下使用正常的 ESM bundler 版本
                'vue': useCdn
                    ? path.resolve(__dirname, 'src/js/vue-shim.js')
                    : 'vue/dist/vue.esm-bundler.js'
            }
        },
        plugins: [
            // HTML 模板插件，用于注入 CDN 资源
            createHtmlPlugin({
                minify: true,
                inject: {
                    data: {
                        cdnScriptTags: useCdn ? cdnScriptTags : '',
                        cdnStyleTags: useCdn ? cdnStyleTags : '',
                        useCdn: useCdn
                    }
                }
            })
        ],
        build: {
            outDir: '../dist',
            emptyOutDir: true,
            assetsDir: 'assets',
            rollupOptions: {
                input: {
                    main: path.resolve(__dirname, 'src/index.html')
                }
                // CDN 模式下不再需要 external/globals，因为 shim 会处理
            }
        },
        server: {
            port: 5173,
            host: true,
            // 暂时禁用隔离头，以确保 FFmpeg 0.11.x 稳定运行在单线程模式
            // headers: {
            //     'Cross-Origin-Opener-Policy': 'same-origin',
            //     'Cross-Origin-Embedder-Policy': 'credentialless'
            // },
            proxy: {
                '/api': {
                    target: 'http://127.0.0.1:3000',
                    changeOrigin: true
                },
                // OpenAI 兼容接口代理
                '/v1': {
                    target: 'http://127.0.0.1:3000',
                    changeOrigin: true
                },
                // WebSocket 代理 (SSH, Metrics, Logs)
                '/ws': {
                    target: 'ws://127.0.0.1:3000',
                    ws: true,
                    changeOrigin: true
                },
                // Socket.IO 代理 (/agent, /metrics 命名空间)
                '/socket.io': {
                    target: 'http://127.0.0.1:3000',
                    ws: true,
                    changeOrigin: true
                },
                '/agent': {
                    target: 'http://127.0.0.1:3000',
                    ws: true,
                    changeOrigin: true
                },
                '/metrics': {
                    target: 'http://127.0.0.1:3000',
                    ws: true,
                    changeOrigin: true
                }
            },
            // 配置 history fallback 用于 SPA 单页路由（如 /2FA, /hosts 等）
            // Vite 内置支持，使用自定义中间件处理非文件路径
        },
        // SPA 模式：所有未匹配的路径返回 index.html
        appType: 'spa',
        // 定义全局变量注入
        define: {
            '__USE_CDN__': JSON.stringify(useCdn),
            '__CDN_PROVIDER__': JSON.stringify(cdnProvider)
        }
    };
});
