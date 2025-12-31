import { defineConfig, loadEnv } from 'vite';
import path from 'path';
import { fileURLToPath } from 'url';
import { createHtmlPlugin } from 'vite-plugin-html';
import { visualizer } from 'rollup-plugin-visualizer';
import { getAllCdnUrls, getExternals, getGlobals } from './cdn.config.mjs';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const isProduction = mode === 'production';

  // CDN 配置
  const useCdn = env.VITE_USE_CDN === 'true';
  const cdnProvider = env.VITE_CDN_PROVIDER || 'npmmirror';
  const cdnData = useCdn ? getAllCdnUrls(cdnProvider) : { js: [], css: [] };

  // Rollup 外部依赖配置 (如果启用 CDN)
  const externalDeps = useCdn ? getExternals() : [];
  const globals = useCdn ? getGlobals() : {};

  return {
    root: 'src',
    base: './',
    plugins: [
      createHtmlPlugin({
        minify: isProduction,
        inject: {
          data: {
            title: 'API Monitor',
            // 注入 CDN 资源
            cdnScriptTags: cdnData.js
              .map(item => `<script src="${item.url}" defer crossorigin="anonymous"></script>`)
              .join('\n    '),
            cdnStyleTags: cdnData.css
              .map(
                url =>
                  `<link rel="stylesheet" href="${url}" media="print" onload="this.media='all'; this.onload=null;">`
              )
              .join('\n    '),
          },
        },
      }),
      // 构建分析插件 (输出到 dist/stats.html)
      visualizer({
        filename: 'dist/stats.html',
        open: false,
        gzipSize: true,
        brotliSize: true,
      }),
    ],
    build: {
      outDir: '../dist',
      assetsDir: 'assets',
      emptyOutDir: true,
      sourcemap: !isProduction,
      minify: isProduction ? 'terser' : false,
      rollupOptions: {
        external: externalDeps,
        output: {
          globals: globals,
          // 代码分割策略
          manualChunks: id => {
            // 注意：已经在 externalDeps 中的包（CDN 引用的包）不能在此处分包
            if (id.includes('node_modules')) {
              // 终端组件
              if (id.includes('@xterm')) {
                return 'vendor-xterm';
              }
              // 播放器与多媒体
              if (
                id.includes('artplayer') ||
                id.includes('flv.js') ||
                id.includes('hls.js') ||
                id.includes('plyr')
              ) {
                return 'vendor-media';
              }
              // Pixi 渲染引擎
              if (id.includes('@pixi') || id.includes('pixi-filters')) {
                return 'vendor-pixi';
              }
              // 其他大型工具库 (且不在 CDN 中的)
              if (
                id.includes('axios') ||
                id.includes('marked') ||
                id.includes('dompurify') ||
                id.includes('uuid') ||
                id.includes('vue')
              ) {
                // 如果启用了 CDN 且 vue/axios 在 external 中，Vite 会自动忽略它们
                // 这里我们显式将非 CDN 的大库打包
                return 'vendor-utils';
              }
            }
          },
        },
      },
      terserOptions: {
        compress: {
          drop_console: isProduction,
          drop_debugger: isProduction,
        },
      },
    },
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
        // 关键：确保支持在 HTML 中直接写模板 (Runtime Compilation)
        vue: 'vue/dist/vue.esm-bundler.js',
      },
    },
    define: {
      __USE_CDN__: JSON.stringify(useCdn),
      __CDN_PROVIDER__: JSON.stringify(cdnProvider),
      // Vue 特性标志，消除控制台警告
      __VUE_OPTIONS_API__: JSON.stringify(true),
      __VUE_PROD_DEVTOOLS__: JSON.stringify(false),
      __VUE_PROD_HYDRATION_MISMATCH_DETAILS__: JSON.stringify(false),
    },
    server: {
      port: 5173,
      proxy: {
        '/api': {
          target: 'http://127.0.0.1:3000',
          changeOrigin: true,
        },
        '/socket.io': {
          target: 'http://127.0.0.1:3000',
          ws: true,
        },
      },
    },
  };
});
