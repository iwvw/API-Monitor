const { defineConfig } = require('vitest/config');

module.exports = defineConfig({
    test: {
        // 测试环境
        environment: 'node',

        // 测试文件匹配模式
        include: ['test/**/*.test.js'],

        // 排除目录
        exclude: ['node_modules', 'dist', 'data'],

        // 覆盖率配置
        coverage: {
            provider: 'v8',
            reporter: ['text', 'json', 'html'],
            reportsDirectory: './coverage',
            exclude: [
                'node_modules/**',
                'dist/**',
                'test/**',
                'src/js/**', // 前端代码需要不同的测试环境
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
