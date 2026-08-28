import { createLogger, defineConfig, loadEnv } from 'vite';
import path from 'path';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'url';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { visualizer } from 'rollup-plugin-visualizer';
import { resolveAppVersionFromEnvironment } from './tools/app-version.mjs';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const wsProxyAbortCodes = new Set(['ECONNABORTED']);

const rootPackageJson = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8'));

function pkgVersion(name) {
  const raw = rootPackageJson.dependencies?.[name] || rootPackageJson.devDependencies?.[name];
  if (raw) return raw.replace(/^[\^~]/, '');
  try {
    const installed = JSON.parse(readFileSync(new URL(`./node_modules/${name}/package.json`, import.meta.url), 'utf8'));
    return installed.version || '';
  } catch {
    return '';
  }
}

function createDevLogger() {
  const logger = createLogger();
  const error = logger.error.bind(logger);

  logger.error = (message, options) => {
    const proxyError = options?.error;
    const isAbortiveWsProxyClose =
      wsProxyAbortCodes.has(proxyError?.code) &&
      message.includes('ws proxy') &&
      message.includes(proxyError.code);

    if (isAbortiveWsProxyClose) return;

    error(message, options);
  };

  return logger;
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const isProduction = mode === 'production';
  const shouldAnalyze = env.ANALYZE === 'true';
  const appVersion = resolveAppVersionFromEnvironment({
    cwd: __dirname,
    env: { ...process.env, ...env },
  });

  return {
    root: 'src',
    base: '/',
    publicDir: path.resolve(__dirname, './src/pwa-public'),
    customLogger: createDevLogger(),
    define: {
      'import.meta.env.VITE_APP_VERSION': JSON.stringify(appVersion),
      'import.meta.env.VITE_BUILD_TIME': JSON.stringify(new Date().toISOString()),
      'import.meta.env.VITE_FRAMEWORK_VERSIONS': JSON.stringify({
        react: pkgVersion('react'),
        vite: pkgVersion('vite'),
        tailwind: pkgVersion('tailwindcss'),
        kumo: pkgVersion('@cloudflare/kumo'),
        zustand: pkgVersion('zustand'),
      }),
    },
    plugins: [
      react(),
      tailwindcss(),
      shouldAnalyze && visualizer({
        filename: 'dist/stats.html',
        open: false,
        gzipSize: true,
        brotliSize: true,
      }),
    ].filter(Boolean),
    build: {
      outDir: '../dist',
      assetsDir: 'assets',
      emptyOutDir: true,
      sourcemap: !isProduction,
      minify: isProduction,
      // 禁止把小资源内联为 data URI：flag-icons 的 280+ 面国旗 SVG 会被
      // 默认策略（<4KB 内联）全部塞进渲染阻塞的 index.css（+600KB）。
      // 改为独立文件后按需加载，首屏 CSS 大幅缩小。
      assetsInlineLimit: 0,
      rollupOptions: {
        output: {
          // 代码分割策略
          manualChunks: id => {
            if (id.includes('node_modules')) {
              // React 核心库（精确匹配包目录，避免把 @uiw/react-codemirror、
              // @phosphor-icons/react 等路径含 react 的包误打进首屏 vendor）
              if (
                /node_modules[\\/](react|react-dom|scheduler)[\\/]/.test(id)
              ) {
                return 'vendor-react';
              }
              // 注意：不要给 @phosphor-icons / @cloudflare/kumo 建手动 chunk。
              // 手动分组会把「入口用的少量组件」和「懒加载页面用的全量组件」
              // 合并进同一 chunk 并被入口预载，反而放大首屏体积；
              // 交给自动分割可以让入口只带实际用到的部分。
              // 终端组件
              if (id.includes('@xterm')) {
                return 'vendor-xterm';
              }
              if (id.includes('echarts')) {
                return 'vendor-echarts';
              }
              if (id.includes('zrender')) {
                return 'vendor-zrender';
              }
              // 其他大型工具库与状态管理
              if (
                id.includes('axios') ||
                id.includes('marked') ||
                id.includes('dompurify') ||
                id.includes('zustand')
              ) {
                return 'vendor-utils';
              }
            }
          },
        },
      },
      terserOptions: {
        compress: {
          drop_debugger: isProduction,
          // 生产环境移除 console.log/debug，保留 error/warn 用于线上排障
          pure_funcs: isProduction ? ['console.log', 'console.debug'] : [],
        },
      },
    },
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    server: {
      host: true, // 监听所有网络接口，允许手机访问
      port: 5173,
      // 转发中心把自定义域名代理到本地开发端时，Host 头不在默认允许列表内会被
      // Vite 以 Blocked request 拦截。本地开发放行任意 Host（DNS rebinding 风险
      // 仅限本机 dev server，生产构建走静态托管/后端，不受此配置影响）。
      allowedHosts: true,
      hmr: {
        protocol: 'ws',
        host: 'localhost',
      },
      // 文件系统访问控制：阻止 dev server 暴露后端源代码与敏感数据文件
      fs: {
        deny: [
          '**/.env*',
          '**/*.db',
          '**/*.db-journal',
          '**/*.sqlite*',
          '**/data/**',
          '**/backup/**',
          '**/db/**',
          '**/middleware/**',
          '**/routes/**',
          '**/services/**',
          '**/utils/**',
          '**/views/**',
          '**/scripts/**',
          '**/*.sql',
          '**/*.key',
          '**/*.pem',
        ],
      },
      proxy: {
        '^/health(?:/|$)': {
          target: `http://127.0.0.1:${env.PORT || 3000}`,
          changeOrigin: true,
        },
        '^/api(?:/|$)': {
          target: `http://127.0.0.1:${env.PORT || 3000}`,
          changeOrigin: true,
        },
        '^/sub(?:/|$)': {
          target: `http://127.0.0.1:${env.PORT || 3000}`,
          changeOrigin: true,
        },
        '/v1': {
          target: `http://127.0.0.1:${env.PORT || 3000}`,
          changeOrigin: true,
        },
        '/socket.io': {
          target: `http://127.0.0.1:${env.PORT || 3000}`,
          ws: true,
        },
        '/ws': {
          target: `http://127.0.0.1:${env.PORT || 3000}`,
          ws: true,
          changeOrigin: true,
        },
        '/uploads': {
          target: `http://127.0.0.1:${env.PORT || 3000}`,
          changeOrigin: true,
        },
        '^/site-brand-icons(?:/|$)': {
          target: `http://127.0.0.1:${env.PORT || 3000}`,
          changeOrigin: true,
        },
      },
    },
  };
});
