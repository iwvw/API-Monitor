/**
 * 系统信息服务
 * 获取服务器的系统信息
 */

const sshService = require('./ssh-service');

class SystemInfoService {
    /**
     * 获取服务器完整信息
     * @param {string} serverId - 服务器 ID
     * @param {Object} serverConfig - 服务器配置
     * @returns {Promise<Object>} 服务器信息
     */
    async getServerInfo(serverId, serverConfig) {
        try {
            const [
                systemInfo,
                cpuInfo,
                memoryInfo,
                diskInfo,
                networkInfo,
                dockerInfo
            ] = await Promise.all([
                this.getSystemInfo(serverId, serverConfig),
                this.getCpuInfo(serverId, serverConfig),
                this.getMemoryInfo(serverId, serverConfig),
                this.getDiskInfo(serverId, serverConfig),
                this.getNetworkInfo(serverId, serverConfig),
                this.getDockerInfo(serverId, serverConfig)
            ]);

            return {
                success: true,
                system: systemInfo,
                cpu: cpuInfo,
                memory: memoryInfo,
                disk: diskInfo,
                network: networkInfo,
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
     * @param {string} serverId - 服务器 ID
     * @param {Object} serverConfig - 服务器配置
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
     * @param {string} serverId - 服务器 ID
     * @param {Object} serverConfig - 服务器配置
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
     * @param {string} serverId - 服务器 ID
     * @param {Object} serverConfig - 服务器配置
     * @returns {Promise<Object>} 内存信息
     */
    async getMemoryInfo(serverId, serverConfig) {
        const command = `
            free -h | awk 'NR==2{printf "Total: %s\\nUsed: %s\\nFree: %s\\nUsage: %.2f%%\\n", $2, $3, $4, $3/$2*100}'
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
     * @param {string} serverId - 服务器 ID
     * @param {Object} serverConfig - 服务器配置
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
     * 获取网络接口信息
     * @param {string} serverId - 服务器 ID
     * @param {Object} serverConfig - 服务器配置
     * @returns {Promise<Array>} 网络接口信息列表
     */
    async getNetworkInfo(serverId, serverConfig) {
        const command = `
            ip -o addr show | awk '{print $2"|"$4}' | grep -v '^lo|'
        `;

        const result = await sshService.executeCommand(serverId, serverConfig, command);

        if (!result.success) {
            throw new Error(result.error || result.stderr);
        }

        const lines = result.stdout.trim().split('\n');
        const interfaces = [];

        lines.forEach(line => {
            const [name, address] = line.split('|');
            if (name && address) {
                interfaces.push({
                    name: name.trim(),
                    address: address.trim()
                });
            }
        });

        return interfaces;
    }

    /**
     * 获取 Docker 信息
     * @param {string} serverId - 服务器 ID
     * @param {Object} serverConfig - 服务器配置
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
        const containersCommand = 'docker ps -a --format "{{.ID}}|{{.Names}}|{{.Status}}|{{.Image}}"';
        const containersResult = await sshService.executeCommand(serverId, serverConfig, containersCommand);

        const containers = [];
        if (containersResult.success && containersResult.stdout) {
            const lines = containersResult.stdout.trim().split('\n');
            lines.forEach(line => {
                const [id, name, status, image] = line.split('|');
                if (id) {
                    containers.push({
                        id,
                        name,
                        status,
                        image
                    });
                }
            });
        }

        return {
            installed: true,
            version: versionResult.stdout.trim(),
            containers
        };
    }

    /**
     * 获取 Docker 容器详细信息
     * @param {string} serverId - 服务器 ID
     * @param {Object} serverConfig - 服务器配置
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
            return JSON.parse(result.stdout)[0];
        } catch (error) {
            throw new Error('解析容器信息失败');
        }
    }

    /**
     * 执行服务器操作（重启/关机）
     * @param {string} serverId - 服务器 ID
     * @param {Object} serverConfig - 服务器配置
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
}

// 导出单例
module.exports = new SystemInfoService();
