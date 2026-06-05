import { defineConfig, loadEnv } from 'vite';
import path from 'path';
import { fileURLToPath } from 'url';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { visualizer } from 'rollup-plugin-visualizer';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const isProduction = mode === 'production';

  return {
    root: 'src',
    base: '/',
    plugins: [
      react(),
      tailwindcss(),
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
        output: {
          // 代码分割策略
          manualChunks: id => {
            if (id.includes('node_modules')) {
              // React 核心库
              if (
                id.includes('react') ||
                id.includes('react-dom') ||
                id.includes('scheduler')
              ) {
                return 'vendor-react';
              }
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
              // 其他大型工具库与状态管理
              if (
                id.includes('axios') ||
                id.includes('marked') ||
                id.includes('dompurify') ||
                id.includes('uuid') ||
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
      hmr: {
        protocol: 'ws',
        host: 'localhost',
      },
      // 文件系统访问控制：阻止 dev server 暴露后端源代码
      fs: {
        deny: [
          '**/db/**',
          '**/middleware/**',
          '**/routes/**',
          '**/services/**',
          '**/utils/**',
          '**/views/**',
          '**/scripts/**',
          '**/*.sql',
        ],
      },
      proxy: {
        '/api': {
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
      },
    },
  };
});
