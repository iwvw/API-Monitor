/**
 * {{MODULE_NAME}} 模块 - 业务逻辑服务
 * 
 * 使用说明：
 * 1. 将 {{MODULE_NAME}} 替换为实际模块名
 * 2. 此文件用于处理外部 API 调用、复杂业务逻辑等
 * 3. 如果模块不需要外部服务调用，可以删除此文件
 */

// ==================== 配置 ====================

const DEFAULT_TIMEOUT = 30000; // 30秒超时
const MAX_RETRIES = 3;

// ==================== 辅助函数 ====================

/**
 * 延迟执行
 */
function delay(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * 带重试的请求
 */
async function fetchWithRetry(url, options = {}, retries = MAX_RETRIES) {
    for (let i = 0; i < retries; i++) {
        try {
            const controller = new AbortController();
            const timeout = setTimeout(() => controller.abort(), DEFAULT_TIMEOUT);

            const response = await fetch(url, {
                ...options,
                signal: controller.signal
            });

            clearTimeout(timeout);

            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }

            return response;
        } catch (error) {
            if (i === retries - 1) throw error;
            await delay(1000 * (i + 1)); // 指数退避
        }
    }
}

// ==================== API 调用 ====================

/**
 * 验证 API 凭证
 * @param {string} apiKey - API 密钥
 */
async function verifyCredentials(apiKey) {
    try {
        // 示例：调用外部 API 验证凭证
        const response = await fetchWithRetry('https://api.example.com/verify', {
            method: 'GET',
            headers: {
                'Authorization': `Bearer ${apiKey}`,
                'Content-Type': 'application/json'
            }
        });

        const data = await response.json();
        return {
            valid: true,
            ...data
        };
    } catch (error) {
        return {
            valid: false,
            error: error.message
        };
    }
}

/**
 * 获取外部资源列表
 * @param {string} apiKey - API 密钥
 * @param {Object} options - 查询选项
 */
async function fetchResources(apiKey, options = {}) {
    try {
        const { page = 1, perPage = 20 } = options;

        const response = await fetchWithRetry(
            `https://api.example.com/resources?page=${page}&per_page=${perPage}`,
            {
                method: 'GET',
                headers: {
                    'Authorization': `Bearer ${apiKey}`,
                    'Content-Type': 'application/json'
                }
            }
        );

        const data = await response.json();
        return {
            success: true,
            resources: data.result || [],
            pagination: data.result_info || {}
        };
    } catch (error) {
        return {
            success: false,
            error: error.message,
            resources: []
        };
    }
}

/**
 * 创建外部资源
 * @param {string} apiKey - API 密钥
 * @param {Object} resourceData - 资源数据
 */
async function createResource(apiKey, resourceData) {
    try {
        const response = await fetchWithRetry('https://api.example.com/resources', {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${apiKey}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(resourceData)
        });

        const data = await response.json();
        return {
            success: true,
            resource: data.result
        };
    } catch (error) {
        return {
            success: false,
            error: error.message
        };
    }
}

/**
 * 更新外部资源
 */
async function updateResource(apiKey, resourceId, updates) {
    try {
        const response = await fetchWithRetry(
            `https://api.example.com/resources/${resourceId}`,
            {
                method: 'PUT',
                headers: {
                    'Authorization': `Bearer ${apiKey}`,
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(updates)
            }
        );

        const data = await response.json();
        return {
            success: true,
            resource: data.result
        };
    } catch (error) {
        return {
            success: false,
            error: error.message
        };
    }
}

/**
 * 删除外部资源
 */
async function deleteResource(apiKey, resourceId) {
    try {
        await fetchWithRetry(
            `https://api.example.com/resources/${resourceId}`,
            {
                method: 'DELETE',
                headers: {
                    'Authorization': `Bearer ${apiKey}`,
                    'Content-Type': 'application/json'
                }
            }
        );

        return { success: true };
    } catch (error) {
        return {
            success: false,
            error: error.message
        };
    }
}

// ==================== 导出 ====================

module.exports = {
    verifyCredentials,
    fetchResources,
    createResource,
    updateResource,
    deleteResource
};
