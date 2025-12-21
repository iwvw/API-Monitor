const { defineConfig } = require('vite');
const path = require('path');

module.exports = defineConfig({
    root: 'src',
    publicDir: false,
    resolve: {
        alias: {
            'vue': 'vue/dist/vue.esm-bundler.js'
        }
    },
    build: {
        outDir: '../dist',
        emptyOutDir: true,
        assetsDir: 'assets',
        rollupOptions: {
            input: {
                main: path.resolve(__dirname, 'src/index.html')
            }
        }
    },
    server: {
        port: 5173,
        proxy: {
            '/api': {
                target: 'http://127.0.0.1:3000',
                changeOrigin: true
            },
            '/v1': {
                target: 'http://127.0.0.1:3000',
                changeOrigin: true
            },
            '/ws': {
                target: 'ws://127.0.0.1:3000',
                ws: true
            }
        }
    }
});
