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

  // 需要 external 的包 (仅针对有 global 定义的 JS 包，排除 vue，因为它有特殊的 shim 处理)
  const externalDeps = useCdn ? getExternals().filter(pkg => pkg !== 'vue') : [];
  const globals = useCdn ? getGlobals() : {};

  // 生成 CDN 脚本标签 (普通 script，不是 module)
  const cdnScriptTags = cdnUrls.js
    .map(item => `<script src="${item.url}"></script>`)
    .join('\n    ');

  // 生成 CDN 样式标签
  const cdnStyleTags = cdnUrls.css
    .map(url => `<link rel="stylesheet" href="${url}">`)
    .join('\n    ');

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
        vue: useCdn ? path.resolve(__dirname, 'src/js/vue-shim.js') : 'vue/dist/vue.esm-bundler.js',
        // AMLL 现在从 npm 包 @applemusic-like-lyrics/core 导入
      },
    },
    plugins: [
      // HTML 模板插件，用于注入 CDN 资源
      createHtmlPlugin({
        minify: true,
        inject: {
          data: {
            cdnScriptTags: useCdn ? cdnScriptTags : '',
            cdnStyleTags: useCdn ? cdnStyleTags : '',
            useCdn: useCdn,
          },
        },
      }),
    ],
    build: {
      outDir: '../dist',
      emptyOutDir: true,
      assetsDir: 'assets',
      // 生产构建时移除 console.log 和 console.warn
      minify: 'esbuild',
      esbuild: {
        drop: ['console', 'debugger'],
      },
      rollupOptions: {
        input: {
          main: path.resolve(__dirname, 'src/index.html'),
        },
        external: externalDeps,
        output: {
          globals: globals,
        },
      },
    },
    server: {
      port: 5173,
      host: true,
      fs: {
        allow: [
          path.resolve(__dirname), // 允许访问项目根目录
        ],
      },
      proxy: {
        '/api': {
          target: 'http://127.0.0.1:3000',
          changeOrigin: true,
        },
        // OpenAI 兼容接口代理
        '/v1': {
          target: 'http://127.0.0.1:3000',
          changeOrigin: true,
        },
        // WebSocket 代理 (SSH, Metrics, Logs)
        '/ws': {
          target: 'ws://127.0.0.1:3000',
          ws: true,
          changeOrigin: true,
        },
        // Socket.IO 代理 (/agent, /metrics 命名空间)
        '/socket.io': {
          target: 'http://127.0.0.1:3000',
          ws: true,
          changeOrigin: true,
        },
        '/agent': {
          target: 'http://127.0.0.1:3000',
          changeOrigin: true,
        },
        '/metrics': {
          target: 'http://127.0.0.1:3000',
          ws: true,
          changeOrigin: true,
        },
      },
      // 配置 history fallback 用于 SPA 单页路由（如 /2FA, /hosts 等）
      // Vite 内置支持，使用自定义中间件处理非文件路径
    },
    // SPA 模式：所有未匹配的路径返回 index.html
    appType: 'spa',
    // 定义全局变量注入
    define: {
      __USE_CDN__: JSON.stringify(useCdn),
      __CDN_PROVIDER__: JSON.stringify(cdnProvider),
      // Vue 3 feature flags (用于 tree-shaking)
      __VUE_OPTIONS_API__: JSON.stringify(true),
      __VUE_PROD_DEVTOOLS__: JSON.stringify(false),
      __VUE_PROD_HYDRATION_MISMATCH_DETAILS__: JSON.stringify(false),
    },
  };
});
