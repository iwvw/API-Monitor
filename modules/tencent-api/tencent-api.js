/**
 * Tencent Cloud API 封装
 */

const tencentcloud = require('tencentcloud-sdk-nodejs');
const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('TencentAPI');

// API 版本及客户端定义
const DnspodClient = tencentcloud.dnspod.v20210323.Client;
const CvmClient = tencentcloud.cvm.v20170312.Client;
const LighthouseClient = tencentcloud.lighthouse.v20200324.Client;
const MonitorClient = tencentcloud.monitor.v20180724.Client;

const REGION_MAP = {
    'ap-guangzhou': '华南地区 (广州)',
    'ap-shanghai': '华东地区 (上海)',
    'ap-nanjing': '华东地区 (南京)',
    'ap-beijing': '华北地区 (北京)',
    'ap-chengdu': '西南地区 (成都)',
    'ap-chongqing': '西南地区 (重庆)',
    'ap-hongkong': '中国香港',
    'ap-singapore': '新加坡',
    'ap-tokyo': '日本 (东京)',
    'ap-seoul': '韩国 (首尔)',
    'ap-bangkok': '泰国 (曼谷)',
    'ap-mumbai': '印度 (孟买)',
    'na-siliconvalley': '美西 (硅谷)',
    'na-ashburn': '美东 (弗吉尼亚)',
    'eu-frankfurt': '欧洲地区 (法兰克福)',
    'eu-moscow': '欧洲地区 (莫斯科)'
};

/**
 * 格式化机型配置
 */
function formatFlavor(instance) {
    if (!instance) return '-';
    if (instance.CPU && instance.Memory) {
        return `${instance.CPU}核 ${instance.Memory}GB`;
    }
    return instance.InstanceType || '-';
}

/**
 * 创建客户端工具函数
 */
function createClient(ClientClass, auth, region = 'ap-guangzhou') {
    const clientConfig = {
        credential: {
            secretId: auth.secretId,
            secretKey: auth.secretKey,
        },
        region: region,
        profile: {
            httpProfile: {
                endpoint: "",
            },
        },
    };
    return new ClientClass(clientConfig);
}

// ==================== DNS 相关 (DNSPod) ====================

/**
 * 获取域名列表
 */
async function listDomains(auth) {
    const client = createClient(DnspodClient, auth);
    try {
        const result = await client.DescribeDomainList({});
        return {
            domains: result.DomainList || [],
            total: result.DomainCount || 0
        };
    } catch (e) {
        throw new Error(`DescribeDomainList Failed: ${e.message}`);
    }
}

/**
 * 添加域名
 */
async function addDomain(auth, domain) {
    const client = createClient(DnspodClient, auth);
    try {
        const result = await client.CreateDomain({ Domain: domain });
        return result;
    } catch (e) {
        throw new Error(`CreateDomain Failed: ${e.message}`);
    }
}

/**
 * 删除域名
 */
async function deleteDomain(auth, domain) {
    const client = createClient(DnspodClient, auth);
    try {
        const result = await client.DeleteDomain({ Domain: domain });
        return result;
    } catch (e) {
        throw new Error(`DeleteDomain Failed: ${e.message}`);
    }
}

/**
 * 获取解析记录
 */
async function listDomainRecords(auth, domain) {
    const client = createClient(DnspodClient, auth);
    try {
        const result = await client.DescribeRecordList({ Domain: domain });
        return {
            records: result.RecordList || [],
            total: result.RecordCount || 0
        };
    } catch (e) {
        throw new Error(`DescribeRecordList Failed: ${e.message}`);
    }
}

/**
 * 添加解析记录
 */
async function addDomainRecord(auth, domain, record) {
    const client = createClient(DnspodClient, auth);
    try {
        return await client.CreateRecord({
            Domain: domain,
            SubDomain: record.subDomain,
            RecordType: record.recordType,
            RecordLine: record.recordLine || '默认',
            Value: record.value,
            TTL: record.ttl || 600,
            MX: record.mx,
            Status: "ENABLE"
        });
    } catch (e) {
        throw new Error(`CreateRecord Failed: ${e.message}`);
    }
}

/**
 * 修改解析记录
 */
async function updateDomainRecord(auth, domain, recordId, record) {
    const client = createClient(DnspodClient, auth);
    try {
        return await client.ModifyRecord({
            Domain: domain,
            RecordId: parseInt(recordId),
            SubDomain: record.subDomain,
            RecordType: record.recordType,
            RecordLine: record.recordLine || '默认',
            Value: record.value,
            TTL: record.ttl || 600,
            MX: record.mx
        });
    } catch (e) {
        throw new Error(`ModifyRecord Failed: ${e.message}`);
    }
}

/**
 * 删除解析记录
 */
async function deleteDomainRecord(auth, domain, recordId) {
    const client = createClient(DnspodClient, auth);
    try {
        return await client.DeleteRecord({
            Domain: domain,
            RecordId: parseInt(recordId)
        });
    } catch (e) {
        throw new Error(`DeleteRecord Failed: ${e.message}`);
    }
}

/**
 * 设置记录状态
 */
async function setDomainRecordStatus(auth, domain, recordId, status) {
    const client = createClient(DnspodClient, auth);
    try {
        return await client.ModifyRecordStatus({
            Domain: domain,
            RecordId: parseInt(recordId),
            Status: status === 'ENABLE' ? 'ENABLE' : 'DISABLE'
        });
    } catch (e) {
        throw new Error(`ModifyRecordStatus Failed: ${e.message}`);
    }
}

// ==================== CVM 相关 ====================

/**
 * 获取所有区域的 CVM 实例
 */
async function listCvmInstances(auth, region) {
    const client = createClient(CvmClient, auth, region);
    try {
        const result = await client.DescribeInstances({});
        return result.InstanceSet || [];
    } catch (e) {
        // 区域无权限或报错静默返回
        return [];
    }
}

/**
 * 获取常用区域的所有 CVM
 */
async function listAllCvmInstances(auth) {
    const regions = ['ap-guangzhou', 'ap-shanghai', 'ap-beijing', 'ap-hongkong', 'ap-singapore'];
    const results = await Promise.all(regions.map(r => listCvmInstances(auth, r)));
    const all = [].concat(...results);
    return {
        instances: all,
        total: all.length
    };
}

/**
 * CVM 实例控制
 */
async function controlCvmInstance(auth, region, instanceId, action) {
    const client = createClient(CvmClient, auth, region);
    const params = { InstanceIds: [instanceId] };
    try {
        switch (action) {
            case 'start': return await client.StartInstances(params);
            case 'stop': return await client.StopInstances(params);
            case 'reboot': return await client.RebootInstances(params);
            default: throw new Error('Invalid action');
        }
    } catch (e) {
        throw new Error(`CVM ${action} Failed: ${e.message}`);
    }
}

// ==================== Lighthouse 相关 ====================

/**
 * 获取单个区域的轻量实例
 */
async function listLighthouseInRegion(auth, region) {
    const client = createClient(LighthouseClient, auth, region);
    try {
        const result = await client.DescribeInstances({});
        return result.InstanceSet || [];
    } catch (e) {
        return [];
    }
}

/**
 * 获取所有区域的轻量服务器
 */
async function listAllLighthouseInstances(auth) {
    const regions = ['ap-guangzhou', 'ap-shanghai', 'ap-beijing', 'ap-hongkong', 'ap-singapore', 'ap-nanjing', 'ap-chengdu'];
    const results = await Promise.all(regions.map(r => listLighthouseInRegion(auth, r)));
    const all = [].concat(...results);
    return {
        instances: all,
        total: all.length
    };
}

/**
 * 轻量服务器实例控制
 */
async function controlLighthouseInstance(auth, region, instanceId, action) {
    const client = createClient(LighthouseClient, auth, region);
    const params = { InstanceIds: [instanceId] };
    try {
        switch (action) {
            case 'start': return await client.StartInstances(params);
            case 'stop': return await client.StopInstances(params);
            case 'reboot': return await client.RebootInstances(params);
            default: throw new Error('Invalid action');
        }
    } catch (e) {
        throw new Error(`Lighthouse ${action} Failed: ${e.message}`);
    }
}

// ==================== 监控相关 ====================

/**
 * 获取监控指标
 */
async function getMetricData(auth, region, params) {
    const client = createClient(MonitorClient, auth, region);
    try {
        return await client.GetMonitorData(params);
    } catch (e) {
        throw new Error(`GetMonitorData Failed: ${e.message}`);
    }
}

module.exports = {
    listDomains,
    addDomain,
    deleteDomain,
    listDomainRecords,
    addDomainRecord,
    updateDomainRecord,
    deleteDomainRecord,
    setDomainRecordStatus,
    listAllCvmInstances,
    controlCvmInstance,
    listAllLighthouseInstances,
    controlLighthouseInstance,
    getMetricData,
    REGION_MAP,
    formatFlavor
};
