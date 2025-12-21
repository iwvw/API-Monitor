/**
 * 外部 API 交互服务
 */

const axios = require('axios');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('{{ModuleName}}Service');

class TemplateService {
    constructor() {
        this.baseUrl = 'https://api.example.com/v1';
    }

    /**
     * 通用请求方法
     */
    async request(token, endpoint, method = 'GET', data = null) {
        try {
            const response = await axios({
                url: `${this.baseUrl}${endpoint}`,
                method,
                headers: {
                    'Authorization': `Bearer ${token}`,
                    'Content-Type': 'application/json'
                },
                data
            });
            return response.data;
        } catch (error) {
            const msg = error.response?.data?.message || error.message;
            logger.error(`API 请求失败 [${endpoint}]: ${msg}`);
            throw new Error(msg);
        }
    }

    /**
     * 示例：验证 Token
     */
    async validateToken(token) {
        return await this.request(token, '/user/profile');
    }

    /**
     * 示例：获取数据列表
     */
    async fetchData(token) {
        return await this.request(token, '/items');
    }
}

module.exports = new TemplateService();
