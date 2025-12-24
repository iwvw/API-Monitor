/**
 * API Monitor Agent - 系统信息采集器
 * 使用 systeminformation 库采集各项指标
 */

const si = require('systeminformation');
const os = require('os');
const { execSync } = require('child_process');

// 缓存网络流量数据用于计算速度
let lastNetStats = { rx: 0, tx: 0, timestamp: Date.now() };
let cachedHostInfo = null;
let cachedDiskUsed = 0;  // 缓存磁盘使用量（变化慢）

/**
 * 此信息变化频率低，建议 10 分钟采集一次
 */
async function collectHostInfo() {
    try {
        // 添加超时保护
        const withTimeout = (promise, ms, fallback) => {
            return Promise.race([
                promise,
                new Promise(resolve => setTimeout(() => resolve(fallback), ms))
            ]);
        };

        const [osInfo, cpu, mem, diskLayout, graphics, networkInterfaces] = await Promise.all([
            withTimeout(si.osInfo(), 10000, { platform: os.platform(), distro: '', release: '' }),
            withTimeout(si.cpu(), 10000, { manufacturer: '', brand: '', physicalCores: os.cpus().length }),
            withTimeout(si.mem(), 10000, { total: os.totalmem(), swaptotal: 0 }),
            withTimeout(si.diskLayout(), 10000, []),
            withTimeout(si.graphics().catch(() => ({ controllers: [] })), 10000, { controllers: [] }),
            withTimeout(si.networkInterfaces(), 10000, [])
        ]);

        // 获取公网 IP (添加超时保护)
        let publicIp = '';
        try {
            publicIp = await withTimeout(getPublicIP(), 5000, '');
        } catch (e) {
            // 忽略
        }

        // 计算磁盘总量 (优先使用 diskLayout，超时则尝试 fsSize)
        let diskTotal = diskLayout.reduce((sum, d) => sum + (d.size || 0), 0);
        if (diskTotal === 0) {
            // diskLayout 超时，尝试异步获取 fsSize
            try {
                const fsSize = await withTimeout(si.fsSize(), 5000, []);
                diskTotal = fsSize.reduce((sum, fs) => sum + (fs.size || 0), 0);
            } catch (e) { }
        }

        cachedHostInfo = {
            platform: os.platform(),
            platform_version: `${osInfo.distro} ${osInfo.release}`.trim(),
            cpu: [`${cpu.manufacturer} ${cpu.brand} ${cpu.physicalCores} Physical Core`],
            gpu: graphics.controllers.map(g => g.model).filter(Boolean),
            mem_total: mem.total,
            disk_total: diskTotal,
            swap_total: mem.swaptotal || 0,
            arch: os.arch(),
            virtualization: osInfo.virtual ? 'virtual' : '',
            boot_time: Math.floor(Date.now() / 1000 - os.uptime()),
            ip: publicIp,
            country_code: '',
            agent_version: require('./package.json').version
        };

        return cachedHostInfo;
    } catch (error) {
        console.error('[Collector] 采集主机信息失败:', error.message);
        return cachedHostInfo || {};
    }
}

/**
 * 此信息变化频率高，建议 1-2 秒采集一次
 */
async function collectState() {
    try {
        // 添加超时保护，避免卡住
        const withTimeout = (promise, ms, fallback) => {
            return Promise.race([
                promise,
                new Promise(resolve => setTimeout(() => resolve(fallback), ms))
            ]);
        };

        // 使用 Node.js 原生 os 模块获取内存（更快更稳定）
        const memTotal = os.totalmem();
        const memFree = os.freemem();
        const memUsed = memTotal - memFree;

        // 只对 CPU 和网络使用 systeminformation（移除 fsSize 同步等待）
        const [cpuLoad, networkStats] = await Promise.all([
            withTimeout(si.currentLoad(), 5000, { currentLoad: 0 }),
            withTimeout(si.networkStats('*'), 5000, [])
        ]);

        // 磁盘使用缓存数据，后台异步更新（磁盘变化慢，不需要实时）
        si.fsSize().then(fsSize => {
            cachedDiskUsed = fsSize.reduce((sum, fs) => sum + (fs.used || 0), 0);
        }).catch(() => { });
        const diskUsed = cachedDiskUsed;

        // 计算网络流量和速度
        const netRx = networkStats.reduce((sum, n) => sum + (n.rx_bytes || 0), 0);
        const netTx = networkStats.reduce((sum, n) => sum + (n.tx_bytes || 0), 0);
        const now = Date.now();
        const timeDiff = (now - lastNetStats.timestamp) / 1000;

        let netInSpeed = 0, netOutSpeed = 0;
        // 修复：只要有上一次记录且时间差有效就计算速率（移除 rx > 0 的限制）
        if (timeDiff > 0 && lastNetStats.timestamp > 0) {
            netInSpeed = Math.max(0, (netRx - lastNetStats.rx) / timeDiff);
            netOutSpeed = Math.max(0, (netTx - lastNetStats.tx) / timeDiff);
        }

        // 调试输出
        if (process.argv.includes('--debug') || process.argv.includes('-d')) {
            console.log(`[Collector] 网络统计: RX=${netRx} bytes, TX=${netTx} bytes, 速率: ↓${Math.round(netInSpeed)} B/s ↑${Math.round(netOutSpeed)} B/s`);
        }

        lastNetStats = { rx: netRx, tx: netTx, timestamp: now };

        // 获取负载 (Windows 不支持 load average)
        let load1 = 0, load5 = 0, load15 = 0;
        if (os.platform() !== 'win32') {
            const loadAvg = os.loadavg();
            load1 = loadAvg[0];
            load5 = loadAvg[1];
            load15 = loadAvg[2];
        } else {
            // Windows 使用 CPU 使用率模拟
            load1 = cpuLoad.currentLoad / 100 * os.cpus().length;
            load5 = load1;
            load15 = load1;
        }

        // 获取连接数 (添加超时保护)
        let tcpConnCount = 0, udpConnCount = 0;
        try {
            const connections = await withTimeout(si.networkConnections(), 2000, []);
            tcpConnCount = connections.filter(c => c.protocol === 'tcp').length;
            udpConnCount = connections.filter(c => c.protocol === 'udp').length;
        } catch (e) {
            // 连接数获取可能需要权限
        }

        // Docker 信息 (添加超时保护)
        const docker = await withTimeout(collectDockerInfo(), 2000, { installed: false, running: 0, stopped: 0, containers: [] });

        return {
            cpu: cpuLoad.currentLoad || 0,
            mem_used: memUsed,  // 使用 os 模块获取
            swap_used: 0,       // swap 暂不追踪
            disk_used: diskUsed,
            net_in_transfer: netRx,
            net_out_transfer: netTx,
            net_in_speed: Math.round(netInSpeed),
            net_out_speed: Math.round(netOutSpeed),
            uptime: Math.floor(os.uptime()),
            load1,
            load5,
            load15,
            tcp_conn_count: tcpConnCount,
            udp_conn_count: udpConnCount,
            process_count: 0,  // 进程数暂不追踪（采集太慢）
            temperatures: [],  // 温度采集较复杂，暂不实现
            gpu: 0,            // GPU 使用率暂不实现
            docker
        };
    } catch (error) {
        console.error('[Collector] 采集状态失败:', error.message);
        return {
            cpu: 0,
            mem_used: 0,
            swap_used: 0,
            disk_used: 0,
            net_in_transfer: 0,
            net_out_transfer: 0,
            net_in_speed: 0,
            net_out_speed: 0,
            uptime: Math.floor(os.uptime()),
            load1: 0,
            load5: 0,
            load15: 0,
            tcp_conn_count: 0,
            udp_conn_count: 0,
            process_count: 0,
            temperatures: [],
            gpu: 0,
            docker: { installed: false, running: 0, stopped: 0, containers: [] }
        };
    }
}

/**
 * 采集 Docker 信息
 */
async function collectDockerInfo() {
    const defaultDocker = {
        installed: false,
        running: 0,
        stopped: 0,
        containers: []
    };

    try {
        // 检查 Docker 是否安装
        const dockerVersion = await si.dockerInfo().catch(() => null);
        if (!dockerVersion) {
            return defaultDocker;
        }

        const containers = await si.dockerContainers(true);

        const running = containers.filter(c => c.state === 'running').length;
        const stopped = containers.length - running;

        return {
            installed: true,
            running,
            stopped,
            containers: containers.map(c => ({
                id: c.id.substring(0, 12),
                name: c.name,
                image: c.image,
                status: c.state,
                created: c.created
            }))
        };
    } catch (error) {
        return defaultDocker;
    }
}

/**
 * 获取公网 IP
 */
async function getPublicIP() {
    const https = require('https');
    const http = require('http');

    const endpoints = [
        { url: 'https://api.ipify.org', protocol: https },
        { url: 'https://icanhazip.com', protocol: https },
        { url: 'http://ip.sb', protocol: http }
    ];

    for (const endpoint of endpoints) {
        try {
            const ip = await new Promise((resolve, reject) => {
                const req = endpoint.protocol.get(endpoint.url, { timeout: 5000 }, (res) => {
                    let data = '';
                    res.on('data', chunk => data += chunk);
                    res.on('end', () => resolve(data.trim()));
                });
                req.on('error', reject);
                req.on('timeout', () => {
                    req.destroy();
                    reject(new Error('Timeout'));
                });
            });
            if (ip && /^[\d.]+$/.test(ip) || /^[a-f0-9:]+$/i.test(ip)) {
                return ip;
            }
        } catch (e) {
            continue;
        }
    }

    return '';
}

/**
 * 获取缓存的主机信息
 */
function getCachedHostInfo() {
    return cachedHostInfo;
}

module.exports = {
    collectHostInfo,
    collectState,
    collectDockerInfo,
    getPublicIP,
    getCachedHostInfo
};
