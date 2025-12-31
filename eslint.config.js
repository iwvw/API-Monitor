const js = require('@eslint/js');

module.exports = [
  js.configs.recommended,
  {
    // 全局配置
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      globals: {
        // Node.js 全局变量
        require: 'readonly',
        module: 'readonly',
        exports: 'readonly',
        __dirname: 'readonly',
        __filename: 'readonly',
        process: 'readonly',
        console: 'readonly',
        Buffer: 'readonly',
        global: 'readonly',
        setInterval: 'readonly',
        clearInterval: 'readonly',
        setTimeout: 'readonly',
        clearTimeout: 'readonly',
        setImmediate: 'readonly',
        URL: 'readonly',
        fetch: 'readonly',
        ReadableStream: 'readonly',
        TextEncoder: 'readonly',
        TextDecoder: 'readonly',
        // 浏览器全局变量
        window: 'readonly',
        document: 'readonly',
        localStorage: 'readonly',
        sessionStorage: 'readonly',
        navigator: 'readonly',
        location: 'readonly',
        history: 'readonly',
        WebSocket: 'readonly',
        Audio: 'readonly',
        Image: 'readonly',
        HTMLElement: 'readonly',
        HTMLCanvasElement: 'readonly',
        HTMLVideoElement: 'readonly',
        HTMLAudioElement: 'readonly',
        Event: 'readonly',
        CustomEvent: 'readonly',
        MouseEvent: 'readonly',
        KeyboardEvent: 'readonly',
        ClipboardEvent: 'readonly',
        DragEvent: 'readonly',
        MutationObserver: 'readonly',
        ResizeObserver: 'readonly',
        IntersectionObserver: 'readonly',
        requestAnimationFrame: 'readonly',
        cancelAnimationFrame: 'readonly',
        getComputedStyle: 'readonly',
        matchMedia: 'readonly',
        performance: 'readonly',
        FileReader: 'readonly',
        Blob: 'readonly',
        FormData: 'readonly',
        alert: 'readonly',
        confirm: 'readonly',
        prompt: 'readonly',
        URLSearchParams: 'readonly',
        // Worker API
        Worker: 'readonly',
        SharedWorker: 'readonly',
        // 媒体 API
        MediaMetadata: 'readonly',
        // Chart.js
        Chart: 'readonly',
        // Vue 全局 (CDN 模式)
        Vue: 'readonly',
        // Socket.IO
        io: 'readonly',
        // Chrome 扩展 API
        chrome: 'readonly',
        // Monaco 编辑器
        monaco: 'readonly',
        // Highlight.js
        hljs: 'readonly',
        // Html5-QRCode
        Html5Qrcode: 'readonly',
        // AMLL 背景渲染
        amllBgRender: 'writable',
        // Vite 编译时常量
        __USE_CDN__: 'readonly',
        __CDN_PROVIDER__: 'readonly',
        // 项目自定义全局 (前端模块间共享)
        store: 'readonly',
        toast: 'readonly',
        app: 'readonly',
        auth: 'readonly',
      },
    },
    rules: {
      // 基础规则
      // TODO: 后续逐步修复未使用变量后，可将此规则改为 'error'
      'no-unused-vars': [
        'warn',
        {
          args: 'none', // 不检查函数参数
          varsIgnorePattern: '^_|^DEFAULT_|^startTime$|^statusCode$',
          caughtErrorsIgnorePattern: '.*',
        },
      ],
      'no-console': 'off',
      'no-debugger': 'warn',
      'no-empty': ['warn', { allowEmptyCatch: true }], // 允许空 catch
      'no-constant-condition': 'warn',

      // 代码风格（暂时放宽，避免大量改动）
      semi: ['warn', 'always'],
      quotes: ['warn', 'single', { avoidEscape: true, allowTemplateLiterals: true }],
      indent: 'off', // 暂时关闭缩进检查，项目使用混合缩进
      'comma-dangle': 'off',
      'no-trailing-spaces': 'off',
      'eol-last': 'off',

      // 最佳实践
      eqeqeq: 'off', // 允许 == (项目中很多是有意的)
      'no-var': 'warn',
      'prefer-const': 'off',
      'no-throw-literal': 'warn',
      'no-prototype-builtins': 'off',
      'no-useless-escape': 'off', // 很多正则有意使用转义
      'no-useless-catch': 'off', // 一些 try/catch 是有意保留的
      'no-case-declarations': 'off',
      'no-control-regex': 'off',
    },
  },
  // 后端文件特定配置
  {
    files: ['server.js', 'src/**/*.js', 'modules/**/*.js'],
    languageOptions: {
      sourceType: 'commonjs', // 后端使用 CommonJS
    },
  },
  // 前端文件特定配置
  {
    files: ['src/js/**/*.js'],
    languageOptions: {
      sourceType: 'module', // 前端使用 ESM
    },
  },
  {
    // 忽略文件
    ignores: [
      'node_modules/**',
      'dist/**',
      'public/**',
      'data/**',
      'tmp/**',
      'test/**',
      '*.min.js',
      'agent-go/**',
      'modules/_template/**', // 模板文件包含占位符语法
      'plugin/**', // 浏览器扩展使用特殊 API
      'src/js/modules/template.js', // 模板文件
      'src/*_snippet*.js', // 代码片段
      'modules/cloudflare-dns/router_utf8.js', // 备份文件
    ],
  },
];
