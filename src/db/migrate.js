const fs = require('fs');
const path = require('path');
const dbService = require('./database');
const {
    ZeaburAccount,
    CloudflareAccount,
    CloudflareDnsTemplate,
    OpenAIEndpoint,
    UserSettings,
    SystemConfig
} = require('./models');

/**
 * 数据迁移工具
 */
class DataMigration {
    constructor() {
        this.configDir = path.join(__dirname, '../../config');
        this.dataDir = path.join(__dirname, '../../data');
        this.backupDir = path.join(__dirname, '../../backup');
    }

    /**
     * 读取 JSON 文件
     */
    readJsonFile(filePath) {
        try {
            if (!fs.existsSync(filePath)) {
                console.log(`⚠ 文件不存在: ${filePath}`);
                return null;
            }

            const content = fs.readFileSync(filePath, 'utf8');
            return JSON.parse(content);
        } catch (error) {
            console.error(`✗ 读取文件失败 ${filePath}:`, error.message);
            return null;
        }
    }

    /**
     * 备份现有 JSON 文件
     */
    backupJsonFiles() {
        console.log('\n=== 开始备份现有数据文件 ===');

        if (!fs.existsSync(this.backupDir)) {
            fs.mkdirSync(this.backupDir, { recursive: true });
        }

        const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
        const backupSubDir = path.join(this.backupDir, `backup_${timestamp}`);
        fs.mkdirSync(backupSubDir, { recursive: true });

        const filesToBackup = [
            'config/password.json',
            'config/sessions.json',
            'config/zb-accounts.json',
            'config/cf-accounts.json',
            'config/cf-dns-templates.json',
            'config/openai-endpoints.json',
            'config/openai-health.json',
            'data/user-settings.json'
        ];

        let backedUpCount = 0;
        filesToBackup.forEach(file => {
            const sourcePath = path.join(__dirname, '../..', file);
            if (fs.existsSync(sourcePath)) {
                const destPath = path.join(backupSubDir, path.basename(file));
                fs.copyFileSync(sourcePath, destPath);
                console.log(`✓ 已备份: ${file}`);
                backedUpCount++;
            }
        });

        console.log(`✓ 备份完成，共备份 ${backedUpCount} 个文件到: ${backupSubDir}`);
        return backupSubDir;
    }

    /**
     * 迁移系统配置
     */
    migrateSystemConfig() {
        console.log('\n=== 迁移系统配置 ===');

        // 迁移密码配置
        const passwordData = this.readJsonFile(path.join(this.configDir, 'password.json'));
        if (passwordData && passwordData.password) {
            SystemConfig.setConfig('admin_password', passwordData.password, '管理员密码');
            console.log('✓ 已迁移管理员密码');
        }
    }

    /**
     * 迁移 Zeabur 账号
     */
    migrateZeaburAccounts() {
        console.log('\n=== 迁移 Zeabur 账号 ===');

        const accounts = this.readJsonFile(path.join(this.configDir, 'zb-accounts.json'));
        if (!accounts || !Array.isArray(accounts)) {
            console.log('⚠ 没有找到 Zeabur 账号数据');
            return;
        }

        let successCount = 0;
        accounts.forEach(account => {
            try {
                ZeaburAccount.createAccount({
                    id: account.id || `zb_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
                    name: account.name,
                    token: account.token,
                    status: account.status || 'active',
                    email: account.email,
                    username: account.username,
                    balance: account.balance || 0,
                    cost: account.cost || 0,
                    created_at: account.createdAt || new Date().toISOString()
                });
                successCount++;
            } catch (error) {
                console.error(`✗ 迁移账号失败 ${account.name}:`, error.message);
            }
        });

        console.log(`✓ 成功迁移 ${successCount}/${accounts.length} 个 Zeabur 账号`);
    }

    /**
     * 迁移 Cloudflare 账号
     */
    migrateCloudflareAccounts() {
        console.log('\n=== 迁移 Cloudflare 账号 ===');

        const accounts = this.readJsonFile(path.join(this.configDir, 'cf-accounts.json'));
        if (!accounts || !Array.isArray(accounts)) {
            console.log('⚠ 没有找到 Cloudflare 账号数据');
            return;
        }

        let successCount = 0;
        accounts.forEach(account => {
            try {
                CloudflareAccount.createAccount({
                    id: account.id,
                    name: account.name,
                    apiToken: account.apiToken,
                    email: account.email,
                    createdAt: account.createdAt,
                    lastUsed: account.lastUsed
                });
                successCount++;
            } catch (error) {
                console.error(`✗ 迁移账号失败 ${account.name}:`, error.message);
            }
        });

        console.log(`✓ 成功迁移 ${successCount}/${accounts.length} 个 Cloudflare 账号`);
    }

    /**
     * 迁移 Cloudflare DNS 模板
     */
    migrateCloudflareTemplates() {
        console.log('\n=== 迁移 Cloudflare DNS 模板 ===');

        const templates = this.readJsonFile(path.join(this.configDir, 'cf-dns-templates.json'));
        if (!templates || !Array.isArray(templates)) {
            console.log('⚠ 没有找到 DNS 模板数据');
            return;
        }

        let successCount = 0;
        templates.forEach(template => {
            try {
                CloudflareDnsTemplate.createTemplate({
                    id: template.id,
                    name: template.name,
                    description: template.description,
                    records: template.records,
                    created_at: template.createdAt
                });
                successCount++;
            } catch (error) {
                console.error(`✗ 迁移模板失败 ${template.name}:`, error.message);
            }
        });

        console.log(`✓ 成功迁移 ${successCount}/${templates.length} 个 DNS 模板`);
    }

    /**
     * 迁移 OpenAI 端点
     */
    migrateOpenAIEndpoints() {
        console.log('\n=== 迁移 OpenAI 端点 ===');

        const endpoints = this.readJsonFile(path.join(this.configDir, 'openai-endpoints.json'));
        if (!endpoints || !Array.isArray(endpoints)) {
            console.log('⚠ 没有找到 OpenAI 端点数据');
            return;
        }

        let successCount = 0;
        endpoints.forEach(endpoint => {
            try {
                OpenAIEndpoint.createEndpoint({
                    id: endpoint.id,
                    name: endpoint.name,
                    baseUrl: endpoint.baseUrl,
                    apiKey: endpoint.apiKey,
                    status: endpoint.status || 'unknown',
                    models: endpoint.models,
                    createdAt: endpoint.createdAt,
                    lastUsed: endpoint.lastUsed
                });
                successCount++;
            } catch (error) {
                console.error(`✗ 迁移端点失败 ${endpoint.name}:`, error.message);
            }
        });

        console.log(`✓ 成功迁移 ${successCount}/${endpoints.length} 个 OpenAI 端点`);
    }

    /**
     * 迁移用户设置
     */
    migrateUserSettings() {
        console.log('\n=== 迁移用户设置 ===');

        const settings = this.readJsonFile(path.join(this.dataDir, 'user-settings.json'));
        if (!settings) {
            console.log('⚠ 没有找到用户设置数据，使用默认设置');
            return;
        }

        try {
            UserSettings.updateSettings({
                custom_css: settings.customCss || '',
                module_visibility: settings.moduleVisibility || {
                    zeabur: true,
                    dns: true,
                    openai: true
                },
                module_order: settings.moduleOrder || ['zeabur', 'dns', 'openai']
            });
            console.log('✓ 成功迁移用户设置');
        } catch (error) {
            console.error('✗ 迁移用户设置失败:', error.message);
        }
    }

    /**
     * 执行完整迁移
     */
    async migrate() {
        console.log('\n╔════════════════════════════════════════╗');
        console.log('║   API Monitor 数据迁移工具 v1.0      ║');
        console.log('║   从 JSON 文件迁移到 SQLite 数据库    ║');
        console.log('╚════════════════════════════════════════╝');

        try {
            // 1. 初始化数据库
            console.log('\n=== 初始化数据库 ===');
            dbService.initialize();

            // 2. 备份现有文件
            const backupPath = this.backupJsonFiles();

            // 3. 执行迁移
            this.migrateSystemConfig();
            this.migrateZeaburAccounts();
            this.migrateCloudflareAccounts();
            this.migrateCloudflareTemplates();
            this.migrateOpenAIEndpoints();
            this.migrateUserSettings();

            // 4. 显示统计信息
            console.log('\n=== 迁移统计 ===');
            const stats = dbService.getStats();
            console.log('数据库路径:', stats.dbPath);
            console.log('数据库大小:', (stats.dbSize / 1024).toFixed(2), 'KB');
            console.log('\n各表记录数:');
            Object.entries(stats.tables).forEach(([table, count]) => {
                console.log(`  ${table}: ${count} 条记录`);
            });

            console.log('\n╔════════════════════════════════════════╗');
            console.log('║          ✓ 数据迁移完成！             ║');
            console.log('╚════════════════════════════════════════╝');
            console.log(`\n备份文件位置: ${backupPath}`);
            console.log('数据库文件位置:', stats.dbPath);
            console.log('\n提示: 请验证数据迁移是否正确，确认无误后可删除旧的 JSON 文件');

        } catch (error) {
            console.error('\n✗ 数据迁移失败:', error);
            throw error;
        }
    }

    /**
     * 回滚迁移（从备份恢复）
     */
    rollback(backupPath) {
        console.log('\n=== 回滚数据迁移 ===');

        if (!fs.existsSync(backupPath)) {
            console.error('✗ 备份目录不存在:', backupPath);
            return;
        }

        const files = fs.readdirSync(backupPath);
        let restoredCount = 0;

        files.forEach(file => {
            const sourcePath = path.join(backupPath, file);
            let destPath;

            if (file.startsWith('user-settings')) {
                destPath = path.join(this.dataDir, file);
            } else {
                destPath = path.join(this.configDir, file);
            }

            fs.copyFileSync(sourcePath, destPath);
            console.log(`✓ 已恢复: ${file}`);
            restoredCount++;
        });

        console.log(`✓ 回滚完成，共恢复 ${restoredCount} 个文件`);
    }
}

// 如果直接运行此脚本，执行迁移
if (require.main === module) {
    const migration = new DataMigration();
    migration.migrate()
        .then(() => {
            console.log('\n迁移脚本执行完成');
            process.exit(0);
        })
        .catch(error => {
            console.error('\n迁移脚本执行失败:', error);
            process.exit(1);
        });
}

module.exports = DataMigration;
