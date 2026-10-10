import { defineConfig } from 'vitest/config';

export default defineConfig({
    test: {
        // 测试环境
        environment: 'node',

        // 测试文件匹配模式
        include: [
            'test/**/*.test.js',
            'src/js/modules/**/*.test.js',
            'src/js/components/**/*.test.js',
            // 页面级纯逻辑模块（如 remote-desktop 的偏好持久化）与源码同目录存放，
            // 便于就近维护；它们不依赖 React 运行时，可在 node 环境直接测试。
            'src/js/pages/**/*.test.js',
        ],

        // 排除目录
        exclude: ['node_modules', 'dist', 'data'],

        // 覆盖率配置
        coverage: {
            provider: 'v8',
            reporter: ['text', 'json', 'html'],
            reportsDirectory: './coverage',
            include: ['src/js/modules/**'],
            exclude: [
                'node_modules/**',
                'dist/**',
                'test/**',
                'src/js/modules/**/*.test.js',
                'public/**',
                '*.config.js',
            ],
        },

        // 全局设置
        globals: true,

        // vitest 5 起 clearMocks 默认 true，会在每个测试前清空全部
        // mock 的历史调用，导致模块顶层调用计数断言被清掉。本项目
        // 各测试文件均手动 mockClear，保持 vitest 4 行为。
        clearMocks: false,

        // 测试超时时间
        testTimeout: 10000,

        // 钩子超时时间
        hookTimeout: 10000,

        // 并行执行
        pool: 'forks',

        // 监听模式设置
        watch: false,
    },
});
