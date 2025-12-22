/**
 * 系统信息服务
 * 获取主机的系统信息 (最终修复版 - 优化解析与字段对齐)
 */

const sshService = require('./ssh-service');

class SystemInfoService {
    async getServerInfo(serverId, serverConfig) {
        const command = `
            echo "START_METRICS"
            
            echo "---SYSTEM---"
            echo "OS: $(cat /etc/os-release | grep PRETTY_NAME | cut -d'\"' -f2)"
            echo "Kernel: $(uname -r)"
            echo "Architecture: $(uname -m)"
            echo "Hostname: $(hostname)"
            echo "Uptime: $(uptime -p)"
            
            echo "---CPU---"
            echo "Model: $(cat /proc/cpuinfo | grep 'model name' | head -1 | cut -d':' -f2 | xargs)"
            echo "Cores: $(nproc)"
            echo "Load: $(cat /proc/loadavg | cut -d' ' -f1,2,3)"
            # 更加健壮的 CPU 使用率计算 (采样 200ms)
            CPU_STATS_1=$(grep 'cpu ' /proc/stat)
            sleep 0.2
            CPU_STATS_2=$(grep 'cpu ' /proc/stat)
            IDLE1=$(echo $CPU_STATS_1 | awk '{print $5+$6}')
            IDLE2=$(echo $CPU_STATS_2 | awk '{print $5+$6}')
            TOTAL1=$(echo $CPU_STATS_1 | awk '{for(i=2;i<=8;i++) sum+=$i} END {print sum}')
            TOTAL2=$(echo $CPU_STATS_2 | awk '{for(i=2;i<=8;i++) sum+=$i} END {print sum}')
            DIFF_IDLE=$((IDLE2 - IDLE1))
            DIFF_TOTAL=$((TOTAL2 - TOTAL1))
            USAGE=$(echo "$DIFF_IDLE $DIFF_TOTAL" | awk '{printf "%.1f%%", (1 - $1/$2) * 100}')
            echo "Usage: $USAGE"
            
            echo "---MEM---"
            # 使用 /proc/meminfo 获取数据更加底层和稳定
            MEM_TOTAL=$(grep MemTotal /proc/meminfo | awk '{printf "%.1fG", $2/1024/1024}')
            MEM_AVAIL=$(grep MemAvailable /proc/meminfo | awk '{printf "%.1fG", $2/1024/1024}')
            MEM_USED_KB=$(grep MemTotal /proc/meminfo | awk '{t=$2} END {print t}')
            MEM_AVAIL_KB=$(grep MemAvailable /proc/meminfo | awk '{a=$2} END {print a}')
            MEM_USAGE=$(echo "$MEM_USED_KB $MEM_AVAIL_KB" | awk '{printf "%.1f%%", ($1-$2)/$1*100}')
            MEM_USED=$(echo "$MEM_USED_KB $MEM_AVAIL_KB" | awk '{printf "%.1fG", ($1-$2)/1024/1024}')
            echo "Total: $MEM_TOTAL"
            echo "Used: $MEM_USED"
            echo "Free: $MEM_AVAIL"
            echo "Usage: $MEM_USAGE"
            
            echo "---DISK---"
            # 使用 -P (POSIX) 确保输出不换行，且过滤掉特殊文件系统
            df -hP | grep -E '^/dev/|^/dev/root|overlay' | awk '{printf "%s|%s|%s|%s|%s\\n", $1, $2, $3, $4, $5}'
            # 如果上面的 grep 没有匹配到，尝试显示主分区
            if [ $? -ne 0 ]; then
                df -hP / | awk 'NR>1 {printf "%s|%s|%s|%s|%s\\n", $1, $2, $3, $4, $5}'
            fi
            
            echo "---DOCKER---"
            if command -v docker >/dev/null 2>&1;
 then
                echo "Installed: true"
                echo "Version: $(docker --version | awk '{print $3}' | sed 's/,//')"
                echo "---CONTAINERS---"
                docker ps -a --format "{{.ID}}|{{.Names}}|{{.Status}}|{{.Image}}|{{.Ports}}"
            else
                echo "Installed: false"
            fi
            
            echo "END_METRICS"
        `;

        try {
            const result = await sshService.executeCommand(serverId, serverConfig, command);
            if (!result.success) throw new Error(result.error || result.stderr);

            const rawOutput = result.stdout;
            const startIndex = rawOutput.indexOf('START_METRICS');
            const endIndex = rawOutput.lastIndexOf('END_METRICS');

            if (startIndex === -1 || endIndex === -1) {
                throw new Error('未能获取到有效的指标数据段');
            }

            const cleanOutput = rawOutput.substring(startIndex, endIndex);
            return this.parseMergedOutput(cleanOutput);
        } catch (error) {
            console.error(`[SystemInfo] 采集失败:`, error.message);
            return { success: false, error: error.message };
        }
    }

    parseMergedOutput(stdout) {
        const data = {
            success: true,
            system: {},
            cpu: {},
            memory: {},
            disk: [],
            docker: { installed: false, containers: [] }
        };

        const getSection = (name) => {
            const startTag = `---${name}---`;
            const startIdx = stdout.indexOf(startTag);
            if (startIdx === -1) return '';

            const contentStart = startIdx + startTag.length;
            const remaining = stdout.substring(contentStart);

            // 查找下一个任何形式的 ---TAG---
            const nextTagMatch = remaining.match(/\n---[A-Z]+---/);
            const nextTagIdx = nextTagMatch ? nextTagMatch.index : -1;

            const sectionContent = nextTagIdx === -1 ? remaining.trim() : remaining.substring(0, nextTagIdx).trim();
            return sectionContent;
        };

        const parseKV = (text) => {
            const res = {};
            if (!text) return res;
            text.split(/[\r\n]+/).forEach(line => {
                const trimLine = line.trim();
                if (!trimLine) return;
                const parts = trimLine.split(':');
                if (parts.length >= 2) {
                    res[parts[0].trim()] = parts.slice(1).join(':').trim();
                }
            });
            return res;
        };

        // 系统与核心指标
        data.system = parseKV(getSection('SYSTEM'));
        data.cpu = parseKV(getSection('CPU'));
        data.memory = parseKV(getSection('MEM'));

        // 磁盘列表
        const diskText = getSection('DISK');
        if (diskText) {
            diskText.split(/[\r\n]+/).forEach(line => {
                const trimLine = line.trim();
                if (!trimLine) return;
                const parts = trimLine.split('|');
                if (parts.length >= 5) {
                    const [device, total, used, available, usage] = parts;
                    data.disk.push({
                        device: device.trim(),
                        total: total.trim(),
                        used: used.trim(),
                        available: available.trim(),
                        usage: usage.trim()
                    });
                }
            });
        }

        // Docker 状态
        const dockerText = getSection('DOCKER');
        if (dockerText) {
            const dockerKV = parseKV(dockerText);
            data.docker.installed = dockerKV.Installed === 'true';
            data.docker.version = dockerKV.Version || '';
        }

        // 容器列表
        const containersText = getSection('CONTAINERS');
        if (containersText) {
            containersText.split(/[\r\n]+/).forEach(line => {
                const trimLine = line.trim();
                if (!trimLine || trimLine.includes('END_METRICS')) return;
                const parts = trimLine.split('|');
                if (parts.length >= 3) {
                    const [id, name, status, image, ports] = parts;
                    data.docker.containers.push({
                        id: id.trim(),
                        name: name.trim(),
                        status: status.trim(),
                        image: (image || '-').trim(),
                        ports: (ports || '-').trim()
                    });
                }
            });
        }

        return data;
    }

    /**
     * 其他方法...
     */
    async executeServerAction(serverId, serverConfig, action) {
        let command;
        switch (action) {
            case 'reboot': command = 'sudo reboot'; break;
            case 'shutdown': command = 'sudo shutdown -h now'; break;
            default: throw new Error('不支持的操作类型');
        }
        const result = await sshService.executeCommand(serverId, serverConfig, command);
        return { success: result.success, message: result.success ? '操作已执行' : result.error || result.stderr };
    }

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
        return { success: result.success, message: result.success ? '操作已执行' : result.error || result.stderr };
    }

    async checkDockerImageUpdate(serverId, serverConfig, imageName) {
        const command = `docker pull ${imageName}`;
        const result = await sshService.executeCommand(serverId, serverConfig, command);
        if (!result.success) throw new Error(result.error || result.stderr);
        const output = result.stdout;
        if (output.includes('Image is up to date')) return { updateAvailable: false, message: '已是最新版本' };
        else if (output.includes('Downloaded newer image') || output.includes('Digest:')) return { updateAvailable: true, message: '发现新版本' };
        return { updateAvailable: false, message: '检查完成 (状态未知)' };
    }
}

module.exports = new SystemInfoService();
