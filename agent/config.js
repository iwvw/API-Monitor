/**
 * API Monitor Agent - 配置加载模块
 */

const fs = require('fs');
const path = require('path');

// 默认配置
const defaultConfig = {
    serverUrl: 'http://localhost:3000',
    serverId: '',
    agentKey: '',
    reportInterval: 1500,      // 状态上报间隔 (毫秒)
    hostInfoInterval: 600000,  // 主机信息上报间隔 (毫秒)
    reconnectInterval: 4000,   // 重连间隔 (毫秒)
    debug: false
};

/**
 * 加载配置
 * 优先级: 命令行参数 > 环境变量 > 配置文件 > 默认值
 */
function loadConfig() {
    let config = { ...defaultConfig };

    // 1. 从配置文件加载
    // 优先从输出目录（当前工作目录）加载 config.json
    const configPath = path.join(process.cwd(), 'config.json');
    if (fs.existsSync(configPath)) {
        try {
            const fileConfig = JSON.parse(fs.readFileSync(configPath, 'utf8'));
            config = { ...config, ...fileConfig };
            console.log('[Config] 已加载配置文件:', configPath);
        } catch (e) {
            console.error('[Config] 配置文件解析失败:', e.message);
        }
    } else {
        // 备选方案：尝试从 __dirname 加载（用于开发环境）
        const devConfigPath = path.join(__dirname, 'config.json');
        if (fs.existsSync(devConfigPath)) {
            try {
                const fileConfig = JSON.parse(fs.readFileSync(devConfigPath, 'utf8'));
                config = { ...config, ...fileConfig };
                console.log('[Config] 已加载开发环境配置文件');
            } catch (e) {
                console.error('[Config] 开发环境配置文件解析失败:', e.message);
            }
        }
    }

    // 2. 从环境变量加载
    if (process.env.API_MONITOR_SERVER) {
        config.serverUrl = process.env.API_MONITOR_SERVER;
    }
    if (process.env.API_MONITOR_SERVER_ID) {
        config.serverId = process.env.API_MONITOR_SERVER_ID;
    }
    if (process.env.API_MONITOR_KEY) {
        config.agentKey = process.env.API_MONITOR_KEY;
    }
    if (process.env.API_MONITOR_INTERVAL) {
        config.reportInterval = parseInt(process.env.API_MONITOR_INTERVAL);
    }

    // 3. 从命令行参数加载
    const args = process.argv.slice(2);
    for (let i = 0; i < args.length; i++) {
        const arg = args[i];
        const nextArg = args[i + 1];

        switch (arg) {
            case '--server':
            case '-s':
                config.serverUrl = nextArg;
                i++;
                break;
            case '--id':
                config.serverId = nextArg;
                i++;
                break;
            case '--key':
            case '-k':
                config.agentKey = nextArg;
                i++;
                break;
            case '--interval':
            case '-i':
                config.reportInterval = parseInt(nextArg);
                i++;
                break;
            case '--debug':
            case '-d':
                config.debug = true;
                break;
            case '--help':
            case '-h':
                printHelp();
                process.exit(0);
        }
    }

    // 验证必要配置
    if (!config.serverId) {
        console.error('[Config] 错误: 缺少 serverId');
        console.error('  使用 --id <SERVER_ID> 指定，或在配置文件中设置');
        process.exit(1);
    }
    if (!config.agentKey) {
        console.error('[Config] 错误: 缺少 agentKey');
        console.error('  使用 --key <AGENT_KEY> 指定，或在配置文件中设置');
        process.exit(1);
    }

    return config;
}

function printHelp() {
    console.log(`
API Monitor Agent v2.0.0

用法: node index.js [选项]

选项:
  -s, --server <URL>     Dashboard 地址 (默认: http://localhost:3000)
  --id <SERVER_ID>       主机 ID (必需)
  -k, --key <KEY>        Agent 密钥 (必需)
  -i, --interval <MS>    上报间隔 (默认: 2000ms)
  -d, --debug            开启调试模式
  -h, --help             显示帮助

环境变量:
  API_MONITOR_SERVER     Dashboard 地址
  API_MONITOR_SERVER_ID  主机 ID
  API_MONITOR_KEY        Agent 密钥
  API_MONITOR_INTERVAL   上报间隔

配置文件:
  ./config.json          JSON 格式配置文件
`);
}

module.exports = { loadConfig };
