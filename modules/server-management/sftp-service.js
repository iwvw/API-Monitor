/**
 * SFTP 文件管理服务
 */

const SftpClient = require('ssh2-sftp-client');
const path = require('path');

class SFTPService {
    /**
     * 创建 SFTP 客户端
     * @param {Object} serverConfig - 服务器配置
     * @returns {Promise<SftpClient>} SFTP 客户端
     */
    async createClient(serverConfig) {
        const sftp = new SftpClient();

        const config = {
            host: serverConfig.host,
            port: serverConfig.port || 22,
            username: serverConfig.username
        };

        // 根据认证方式添加配置
        if (serverConfig.auth_type === 'password') {
            config.password = serverConfig.password;
        } else if (serverConfig.auth_type === 'key') {
            config.privateKey = serverConfig.private_key;
            if (serverConfig.passphrase) {
                config.passphrase = serverConfig.passphrase;
            }
        }

        await sftp.connect(config);
        return sftp;
    }

    /**
     * 列出目录内容
     * @param {Object} serverConfig - 服务器配置
     * @param {string} remotePath - 远程路径
     * @returns {Promise<Array>} 文件列表
     */
    async listDirectory(serverConfig, remotePath = '/') {
        const sftp = await this.createClient(serverConfig);

        try {
            const list = await sftp.list(remotePath);

            // 格式化文件列表
            const formattedList = list.map(item => ({
                name: item.name,
                type: item.type === 'd' ? 'directory' : 'file',
                size: item.size,
                modifyTime: item.modifyTime,
                accessTime: item.accessTime,
                rights: {
                    user: item.rights.user,
                    group: item.rights.group,
                    other: item.rights.other
                },
                owner: item.owner,
                group: item.group
            }));

            await sftp.end();

            return {
                success: true,
                path: remotePath,
                files: formattedList
            };
        } catch (error) {
            await sftp.end();
            throw error;
        }
    }

    /**
     * 上传文件
     * @param {Object} serverConfig - 服务器配置
     * @param {string} localPath - 本地文件路径
     * @param {string} remotePath - 远程文件路径
     * @returns {Promise<Object>} 上传结果
     */
    async uploadFile(serverConfig, localPath, remotePath) {
        const sftp = await this.createClient(serverConfig);

        try {
            await sftp.put(localPath, remotePath);
            await sftp.end();

            return {
                success: true,
                message: '文件上传成功',
                remotePath
            };
        } catch (error) {
            await sftp.end();
            throw error;
        }
    }

    /**
     * 下载文件
     * @param {Object} serverConfig - 服务器配置
     * @param {string} remotePath - 远程文件路径
     * @param {string} localPath - 本地文件路径
     * @returns {Promise<Object>} 下载结果
     */
    async downloadFile(serverConfig, remotePath, localPath) {
        const sftp = await this.createClient(serverConfig);

        try {
            await sftp.get(remotePath, localPath);
            await sftp.end();

            return {
                success: true,
                message: '文件下载成功',
                localPath
            };
        } catch (error) {
            await sftp.end();
            throw error;
        }
    }

    /**
     * 删除文件
     * @param {Object} serverConfig - 服务器配置
     * @param {string} remotePath - 远程文件路径
     * @param {boolean} isDirectory - 是否为目录
     * @returns {Promise<Object>} 删除结果
     */
    async deleteFile(serverConfig, remotePath, isDirectory = false) {
        const sftp = await this.createClient(serverConfig);

        try {
            if (isDirectory) {
                await sftp.rmdir(remotePath, true); // 递归删除目录
            } else {
                await sftp.delete(remotePath);
            }

            await sftp.end();

            return {
                success: true,
                message: isDirectory ? '目录删除成功' : '文件删除成功'
            };
        } catch (error) {
            await sftp.end();
            throw error;
        }
    }

    /**
     * 重命名文件或目录
     * @param {Object} serverConfig - 服务器配置
     * @param {string} oldPath - 旧路径
     * @param {string} newPath - 新路径
     * @returns {Promise<Object>} 重命名结果
     */
    async renameFile(serverConfig, oldPath, newPath) {
        const sftp = await this.createClient(serverConfig);

        try {
            await sftp.rename(oldPath, newPath);
            await sftp.end();

            return {
                success: true,
                message: '重命名成功',
                newPath
            };
        } catch (error) {
            await sftp.end();
            throw error;
        }
    }

    /**
     * 创建目录
     * @param {Object} serverConfig - 服务器配置
     * @param {string} remotePath - 远程目录路径
     * @param {boolean} recursive - 是否递归创建
     * @returns {Promise<Object>} 创建结果
     */
    async createDirectory(serverConfig, remotePath, recursive = true) {
        const sftp = await this.createClient(serverConfig);

        try {
            await sftp.mkdir(remotePath, recursive);
            await sftp.end();

            return {
                success: true,
                message: '目录创建成功',
                path: remotePath
            };
        } catch (error) {
            await sftp.end();
            throw error;
        }
    }

    /**
     * 检查文件或目录是否存在
     * @param {Object} serverConfig - 服务器配置
     * @param {string} remotePath - 远程路径
     * @returns {Promise<Object>} 检查结果
     */
    async exists(serverConfig, remotePath) {
        const sftp = await this.createClient(serverConfig);

        try {
            const exists = await sftp.exists(remotePath);
            await sftp.end();

            return {
                success: true,
                exists: exists !== false,
                type: exists === 'd' ? 'directory' : exists === '-' ? 'file' : 'unknown'
            };
        } catch (error) {
            await sftp.end();
            throw error;
        }
    }

    /**
     * 获取文件信息
     * @param {Object} serverConfig - 服务器配置
     * @param {string} remotePath - 远程文件路径
     * @returns {Promise<Object>} 文件信息
     */
    async getFileInfo(serverConfig, remotePath) {
        const sftp = await this.createClient(serverConfig);

        try {
            const stat = await sftp.stat(remotePath);
            await sftp.end();

            return {
                success: true,
                info: {
                    size: stat.size,
                    mode: stat.mode,
                    uid: stat.uid,
                    gid: stat.gid,
                    accessTime: stat.atime,
                    modifyTime: stat.mtime,
                    isDirectory: stat.isDirectory,
                    isFile: stat.isFile
                }
            };
        } catch (error) {
            await sftp.end();
            throw error;
        }
    }
}

// 导出单例
module.exports = new SFTPService();
