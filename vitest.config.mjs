import { defineConfig } from 'vitest/config';

export default defineConfig({
    test: {
        // 测试环境
        environment: 'node',

        // 测试文件匹配模式
        include: ['test/**/*.test.js', 'src/js/modules/**/*.test.js', 'src/js/components/**/*.test.js'],

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
