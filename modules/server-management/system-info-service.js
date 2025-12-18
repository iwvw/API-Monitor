/**
 * 系统信息服务
 * 获取主机的系统信息
 */

const sshService = require('./ssh-service');

class SystemInfoService {
    /**
     * 获取主机完整信息
     * @param {string} serverId - 主机 ID
     * @param {Object} serverConfig - 主机配置
     * @returns {Promise<Object>} 主机信息
     */
    async getServerInfo(serverId, serverConfig) {
        try {
            const [
                systemInfo,
                cpuInfo,
                memoryInfo,
                diskInfo,
                dockerInfo
            ] = await Promise.all([
                this.getSystemInfo(serverId, serverConfig),
                this.getCpuInfo(serverId, serverConfig),
                this.getMemoryInfo(serverId, serverConfig),
                this.getDiskInfo(serverId, serverConfig),
                this.getDockerInfo(serverId, serverConfig)
            ]);

            return {
                success: true,
                system: systemInfo,
                cpu: cpuInfo,
                memory: memoryInfo,
                disk: diskInfo,
                docker: dockerInfo
            };
        } catch (error) {
            return {
                success: false,
                error: error.message
            };
        }
    }

    /**
     * 获取系统基本信息
     * @param {string} serverId - 主机 ID
     * @param {Object} serverConfig - 主机配置
     * @returns {Promise<Object>} 系统信息
     */
    async getSystemInfo(serverId, serverConfig) {
        const command = `
            echo "OS: $(cat /etc/os-release | grep PRETTY_NAME | cut -d'"' -f2)"
            echo "Kernel: $(uname -r)"
            echo "Architecture: $(uname -m)"
            echo "Hostname: $(hostname)"
            echo "Uptime: $(uptime -p)"
        `;

        const result = await sshService.executeCommand(serverId, serverConfig, command);

        if (!result.success) {
            throw new Error(result.error || result.stderr);
        }

        const lines = result.stdout.trim().split('\n');
        const info = {};

        lines.forEach(line => {
            const [key, ...valueParts] = line.split(':');
            if (key && valueParts.length > 0) {
                info[key.trim()] = valueParts.join(':').trim();
            }
        });

        return info;
    }

    /**
     * 获取 CPU 信息
     * @param {string} serverId - 主机 ID
     * @param {Object} serverConfig - 主机配置
     * @returns {Promise<Object>} CPU 信息
     */
    async getCpuInfo(serverId, serverConfig) {
        const command = `
            echo "Model: $(cat /proc/cpuinfo | grep 'model name' | head -1 | cut -d':' -f2 | xargs)"
            echo "Cores: $(nproc)"
            echo "Usage: $(top -bn1 | grep "Cpu(s)" | sed "s/.*, *\\([0-9.]*\\)%* id.*/\\1/" | awk '{print 100 - $1"%"}')"
        `;

        const result = await sshService.executeCommand(serverId, serverConfig, command);

        if (!result.success) {
            throw new Error(result.error || result.stderr);
        }

        const lines = result.stdout.trim().split('\n');
        const info = {};

        lines.forEach(line => {
            const [key, ...valueParts] = line.split(':');
            if (key && valueParts.length > 0) {
                info[key.trim()] = valueParts.join(':').trim();
            }
        });

        return info;
    }

    /**
     * 获取内存信息
     * @param {string} serverId - 主机 ID
     * @param {Object} serverConfig - 主机配置
     * @returns {Promise<Object>} 内存信息
     */
    async getMemoryInfo(serverId, serverConfig) {
        // 使用两个命令：free -h 获取显示值，free -b 获取计算值
        const command = `
            total_h=$(free -h | awk 'NR==2{print $2}')
            used_h=$(free -h | awk 'NR==2{print $3}')
            free_h=$(free -h | awk 'NR==2{print $4}')
            usage=$(free -b | awk 'NR==2{printf "%.2f", $3/$2*100}')
            echo "Total: $total_h"
            echo "Used: $used_h"
            echo "Free: $free_h"
            echo "Usage: $usage%"
        `;

        const result = await sshService.executeCommand(serverId, serverConfig, command);

        if (!result.success) {
            throw new Error(result.error || result.stderr);
        }

        const lines = result.stdout.trim().split('\n');
        const info = {};

        lines.forEach(line => {
            const [key, ...valueParts] = line.split(':');
            if (key && valueParts.length > 0) {
                info[key.trim()] = valueParts.join(':').trim();
            }
        });

        return info;
    }

    /**
     * 获取磁盘信息
     * @param {string} serverId - 主机 ID
     * @param {Object} serverConfig - 主机配置
     * @returns {Promise<Array>} 磁盘信息列表
     */
    async getDiskInfo(serverId, serverConfig) {
        const command = `df -h | grep -E '^/dev/' | awk '{printf "%s|%s|%s|%s|%s\\n", $1, $2, $3, $4, $5}'`;

        const result = await sshService.executeCommand(serverId, serverConfig, command);

        if (!result.success) {
            throw new Error(result.error || result.stderr);
        }

        const lines = result.stdout.trim().split('\n');
        const disks = [];

        lines.forEach(line => {
            const [device, total, used, available, usage] = line.split('|');
            if (device) {
                disks.push({
                    device,
                    total,
                    used,
                    available,
                    usage
                });
            }
        });

        return disks;
    }


    /**
     * 获取 Docker 信息
     * @param {string} serverId - 主机 ID
     * @param {Object} serverConfig - 主机配置
     * @returns {Promise<Object>} Docker 信息
     */
    async getDockerInfo(serverId, serverConfig) {
        // 检查 Docker 是否安装
        const checkCommand = 'which docker';
        const checkResult = await sshService.executeCommand(serverId, serverConfig, checkCommand);

        if (!checkResult.success || !checkResult.stdout.trim()) {
            return {
                installed: false,
                message: 'Docker 未安装'
            };
        }

        // 获取 Docker 版本
        const versionCommand = 'docker --version';
        const versionResult = await sshService.executeCommand(serverId, serverConfig, versionCommand);

        // 获取容器列表
        const containersCommand = 'docker ps -a --format "{{.ID}}|{{.Names}}|{{.Status}}|{{.Image}}|{{.Ports}}"';
        const containersResult = await sshService.executeCommand(serverId, serverConfig, containersCommand);

        const containers = [];
        if (containersResult.success && containersResult.stdout) {
            const lines = containersResult.stdout.trim().split('\n');
            lines.forEach(line => {
                const [id, name, status, image, ports] = line.split('|');
                if (id) {
                    containers.push({
                        id,
                        name,
                        status,
                        image,
                        ports: ports || '-'
                    });
                }
            });
        }

        return {
            installed: true,
            version: versionResult.stdout.trim(),
            containers,
            daemonRunning: true
        };
    }

    /**
     * 获取 Docker 容器详细信息
     * @param {string} serverId - 主机 ID
     * @param {Object} serverConfig - 主机配置
     * @param {string} containerId - 容器 ID
     * @returns {Promise<Object>} 容器详细信息
     */
    async getDockerContainerInfo(serverId, serverConfig, containerId) {
        const command = `docker inspect ${containerId}`;
        const result = await sshService.executeCommand(serverId, serverConfig, command);

        if (!result.success) {
            throw new Error(result.error || result.stderr);
        }

        try {
            const data = JSON.parse(result.stdout);
            return Array.isArray(data) ? data[0] : data;
        } catch (error) {
            throw new Error(`解析容器信息失败: ${result.stdout.substring(0, 100)}`);
        }
    }

    /**
     * 执行主机操作（重启/关机）
     * @param {string} serverId - 主机 ID
     * @param {Object} serverConfig - 主机配置
     * @param {string} action - 操作类型（reboot/shutdown）
     * @returns {Promise<Object>} 操作结果
     */
    async executeServerAction(serverId, serverConfig, action) {
        let command;

        switch (action) {
            case 'reboot':
                command = 'sudo reboot';
                break;
            case 'shutdown':
                command = 'sudo shutdown -h now';
                break;
            default:
                throw new Error('不支持的操作类型');
        }

        const result = await sshService.executeCommand(serverId, serverConfig, command);

        return {
            success: result.success,
            message: result.success ? '操作已执行' : result.error || result.stderr
        };
    }
    /**
     * 执行 Docker 容器操作
     * @param {string} serverId - 主机 ID
     * @param {Object} serverConfig - 主机配置
     * @param {string} containerId - 容器 ID
     * @param {string} action - 操作类型 (start/stop/restart/pause/unpause)
     * @returns {Promise<Object>} 操作结果
     */
    async executeDockerAction(serverId, serverConfig, containerId, action) {
        let command;
        switch (action) {
            case 'start': command = `docker start ${containerId}`; break;
            case 'stop': command = `docker stop ${containerId}`; break;
            case 'restart': command = `docker restart ${containerId}`; break;
            case 'pause': command = `docker pause ${containerId}`; break;
            case 'unpause': command = `docker unpause ${containerId}`; break;
            default: throw new Error('不支持的 Docker 操作');
        }

        const result = await sshService.executeCommand(serverId, serverConfig, command);

        let errorMessage = result.success ? '操作已执行' : result.error || result.stderr;

        // 优化 Docker 常见错误信息
        if (!result.success && errorMessage.includes('Is the docker daemon running')) {
            errorMessage = 'Docker 守护进程未运行';
        } else if (!result.success && errorMessage.includes('No such container')) {
            errorMessage = '找不到指定的容器';
        }

        return {
            success: result.success,
            message: errorMessage,
            details: result.stderr
        };
    }

    /**
     * 检查 Docker 镜像更新
     * @param {string} serverId - 主机 ID
     * @param {Object} serverConfig - 主机配置
     * @param {string} imageName - 镜像名称
     * @returns {Promise<Object>} 更新检查结果
     */
    async checkDockerImageUpdate(serverId, serverConfig, imageName) {
        // 使用 docker pull 来检查更新（注意：这会下载新镜像，但不会重启容器）
        const command = `docker pull ${imageName}`;
        const result = await sshService.executeCommand(serverId, serverConfig, command);

        if (!result.success) {
            throw new Error(result.error || result.stderr);
        }

        const output = result.stdout;

        // 分析输出
        if (output.includes('Image is up to date')) {
            return {
                updateAvailable: false,
                message: '已是最新版本'
            };
        } else if (output.includes('Downloaded newer image') || output.includes('Digest:')) {
            return {
                updateAvailable: true,
                message: '发现新版本'
            };
        } else {
            // 其他情况，可能也是更新了，或者已经是新的但输出格式不同
            return {
                updateAvailable: false,
                message: '检查完成 (状态未知)'
            };
        }
    }
}

// 导出单例
module.exports = new SystemInfoService();
