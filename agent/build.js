const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');

// 配置
const UPX_VERSION = '4.2.4';
const DIST_DIR = path.join(__dirname, 'dist');
const TEMP_DIR = path.join(__dirname, 'temp');

// 根据 OS 选择 UPX 下载地址和文件名
const isWin = process.platform === 'win32';
const UPX_URL = isWin
    ? `https://github.com/upx/upx/releases/download/v${UPX_VERSION}/upx-${UPX_VERSION}-win64.zip`
    : `https://github.com/upx/upx/releases/download/v${UPX_VERSION}/upx-${UPX_VERSION}-amd64_linux.tar.xz`;

const UPX_BIN_NAME = isWin ? 'upx.exe' : 'upx';
const UPX_FOLDER = isWin ? `upx-${UPX_VERSION}-win64` : `upx-${UPX_VERSION}-amd64_linux`;
const UPX_BIN = path.join(TEMP_DIR, UPX_FOLDER, UPX_BIN_NAME);

async function run() {
    console.log('🚀 开始构建并压缩 Agent...');
    console.log(`💻 运行平台: ${process.platform}`);

    // 1. 确保目录存在
    if (!fs.existsSync(TEMP_DIR)) fs.mkdirSync(TEMP_DIR, { recursive: true });
    if (!fs.existsSync(DIST_DIR)) fs.mkdirSync(DIST_DIR, { recursive: true });

    // 2. 检查/下载 UPX
    let upxPath = 'upx';
    try {
        execSync('upx --version', { stdio: 'ignore' });
        console.log('✅ 系统已安装 UPX');
    } catch (e) {
        try {
            if (!fs.existsSync(UPX_BIN)) {
                console.log(`📥 正在尝试下载 UPX v${UPX_VERSION} 为 ${process.platform}...`);
                const archivePath = path.join(TEMP_DIR, isWin ? 'upx.zip' : 'upx.tar.xz');

                // 使用 curl 下载 (带重试和镜像支持的可选方案)
                // 优先尝试 GitHub，失败可告知用户手动下载
                execSync(`curl -L -f --connect-timeout 10 "${UPX_URL}" -o "${archivePath}"`, { stdio: 'inherit' });

                console.log('📦 正在解压 UPX...');
                if (isWin) {
                    execSync(`tar -xf "${archivePath}" -C "${TEMP_DIR}"`);
                } else {
                    execSync(`tar -xJf "${archivePath}" -C "${TEMP_DIR}"`);
                }
                console.log('✅ UPX 下载并解压完成');
            }
            upxPath = `"${UPX_BIN}"`;
        } catch (err) {
            console.warn('⚠️ UPX 自动下载失败（可能是网络问题或不支持的平台）。');
            console.log('--- 正在跳过压缩，继续生成原始二进制文件 ---');
            upxPath = null;
        }
    }

    // 3. 执行 pkg 打包
    console.log('🛠️ 正在使用 pkg 打包二进制文件...');
    try {
        // 安装依赖
        if (!fs.existsSync(path.join(__dirname, 'node_modules'))) {
            execSync('npm install', { cwd: __dirname, stdio: 'inherit' });
        }

        // 执行打包
        // 如果在 Dockerfile 中，targets 会通过参数传入，否则使用默认值
        const targets = process.env.PKG_TARGETS || 'node18-linux-x64,node18-win-x64';
        execSync(`npx pkg . --out-path dist --targets ${targets}`, { cwd: __dirname, stdio: 'inherit' });
    } catch (e) {
        console.error('❌ 打包失败:', e.message);
        process.exit(1);
    }

    // 4. 使用 UPX 压缩
    if (upxPath) {
        console.log('✨ 正在使用 UPX 压缩二进制文件...');
        const files = fs.readdirSync(DIST_DIR).filter(f => !f.endsWith('.map'));

        for (const file of files) {
            const filePath = path.join(DIST_DIR, file);
            console.log(`📦 压缩 ${file}...`);
            try {
                // --best: 最高压缩比, --force: 强制压缩
                execSync(`${upxPath} --best --force "${filePath}"`, { stdio: 'inherit' });
            } catch (e) {
                console.warn(`⚠️ 压缩 ${file} 失败。`);
            }
        }
    } else {
        console.log('⏩ 已跳过压缩步骤。');
    }

    // 5. 复制到公共目录 (如果存在)
    const publicAgentDir = path.join(__dirname, '../public/agent');
    if (fs.existsSync(publicAgentDir)) {
        console.log('🚚 正在同步到 public/agent...');
        const files = fs.readdirSync(DIST_DIR).filter(f => !f.endsWith('.map'));
        for (const file of files) {
            fs.copyFileSync(path.join(DIST_DIR, file), path.join(publicAgentDir, file));
        }
    }

    console.log('\n✅ 所有任务完成！');
    console.log('-----------------------------------');
    const finalFiles = fs.readdirSync(DIST_DIR).filter(f => !f.endsWith('.map'));
    const stats = finalFiles.map(f => {
        const s = fs.statSync(path.join(DIST_DIR, f));
        return `${f}: ${(s.size / 1024 / 1024).toFixed(2)} MB`;
    });
    console.log('最终体积:\n' + stats.join('\n'));
}

run().catch(err => {
    console.error('💥 运行时错误:', err);
    process.exit(1);
});
