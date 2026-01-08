/**
 * Aliyun API 封装
 * 使用 @alicloud/pop-core SDK
 */

const RPCClient = require('@alicloud/pop-core');
const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('AliyunAPI');

// API 版本常量
const API_VERSIONS = {
    DNS: '2015-01-09',
    ECS: '2014-05-26',
    CMS: '2019-01-01',
    SWAS: '2018-08-08'
};

const REGION_MAP = {
    'cn-hangzhou': '华东1 (杭州)',
    'cn-shanghai': '华东2 (上海)',
    'cn-qingdao': '华北1 (青岛)',
    'cn-beijing': '华北2 (北京)',
    'cn-zhangjiakou': '华北3 (张家口)',
    'cn-huhehaote': '华北5 (呼和浩特)',
    'cn-wulanchabu': '华北6 (乌兰察布)',
    'cn-shenzhen': '华南1 (深圳)',
    'cn-heyuan': '华南2 (河源)',
    'cn-guangzhou': '华南3 (广州)',
    'cn-chengdu': '西南1 (成都)',
    'cn-hongkong': '中国香港',
    'ap-southeast-1': '新加坡',
    'ap-southeast-2': '澳大利亚 (悉尼)',
    'ap-southeast-3': '马来西亚 (吉隆坡)',
    'ap-southeast-5': '印度尼西亚 (雅加达)',
    'ap-northeast-1': '日本 (东京)',
    'ap-south-1': '印度 (孟买)',
    'us-east-1': '美国 (弗吉尼亚)',
    'us-west-1': '美国 (硅谷)',
    'eu-central-1': '德国 (法兰克福)',
    'eu-west-1': '英国 (伦敦)',
    'me-east-1': '阿联酋 (迪拜)',
    'cn-wuhan-lrb': '华中1 (武汉-轻量)',
};

/**
 * 友好化显示规格
 */
function formatFlavor(flavor) {
    if (!flavor) return '-';
    // 匹配类似 ecs.c5.large 或 s.c2m2s40b1
    if (flavor.includes('c1m1')) return '1核 1GB';
    if (flavor.includes('c2m2')) return '2核 2GB';
    if (flavor.includes('c2m4')) return '2核 4GB';
    if (flavor.includes('c4m4')) return '4核 4GB';
    if (flavor.includes('c4m8')) return '4核 8GB';
    if (flavor.includes('c8m16')) return '8核 16GB';

    // 常见的 ECS 命名
    const match = flavor.match(/(\d+)c(\d+)m/);
    if (match) return `${match[1]}核 ${match[2]}GB`;

    return flavor;
}

/**
 * 创建 CMS (云监控) Client
 */
function createCmsClient(auth) {
    return new RPCClient({
        accessKeyId: auth.accessKeyId,
        accessKeySecret: auth.accessKeySecret,
        endpoint: 'https://metrics.aliyuncs.com',
        apiVersion: API_VERSIONS.CMS,
        opts: { timeout: 15000 }
    });
}

/**
 * 创建通用 Client
 */
function createClient(auth, apiVersion) {
    if (!auth.accessKeyId || !auth.accessKeySecret) {
        throw new Error('Missing AccessKeyId or AccessKeySecret');
    }

    return new RPCClient({
        accessKeyId: auth.accessKeyId,
        accessKeySecret: auth.accessKeySecret,
        endpoint: 'https://ecs.aliyuncs.com', // 默认 ECS
        apiVersion: apiVersion,
        opts: { timeout: 15000 }
    });
}

/**
 * 创建 DNS Client (特殊端点)
 */
function createDnsClient(auth) {
    if (!auth.accessKeyId || !auth.accessKeySecret) {
        throw new Error('Missing AccessKeyId or AccessKeySecret');
    }

    return new RPCClient({
        accessKeyId: auth.accessKeyId,
        accessKeySecret: auth.accessKeySecret,
        endpoint: 'https://alidns.aliyuncs.com',
        apiVersion: API_VERSIONS.DNS,
        opts: { timeout: 15000 }
    });
}

/**
 * 创建不同区域的 ECS Client
 */
function createEcsClient(auth, regionId) {
    if (!auth.accessKeyId || !auth.accessKeySecret) {
        throw new Error('Missing AccessKeyId or AccessKeySecret');
    }

    return new RPCClient({
        accessKeyId: auth.accessKeyId,
        accessKeySecret: auth.accessKeySecret,
        endpoint: `https://ecs.${regionId}.aliyuncs.com`,
        apiVersion: API_VERSIONS.ECS,
        opts: { timeout: 15000 }
    });
}

// ==================== DNS 相关 ====================

/**
 * 获取域名列表
 */
async function listDomains(auth, options = {}) {
    const client = createDnsClient(auth);
    try {
        const result = await client.request('DescribeDomains', {
            PageSize: options.pageSize || 100,
            PageNumber: options.pageNumber || 1
        });
        return {
            domains: result.Domains?.Domain || [],
            total: result.TotalCount
        };
    } catch (e) {
        throw new Error(`DescribeDomains Failed: ${e.message}`);
    }
}

/**
 * 添加域名
 */
async function addDomain(auth, domainName) {
    const client = createDnsClient(auth);
    try {
        // 1. 先尝试获取域名信息，如果已经存在于账号中，直接返回 NS
        try {
            const info = await client.request('DescribeDomainInfo', {
                DomainName: domainName
            });
            return {
                DomainName: domainName,
                DnsServers: info.DnsServers?.DnsServer || [],
                AlreadyExists: true
            };
        } catch (e) {
            // 如果报错不是域名不存在，则继续尝试添加
            if (e.code !== 'InvalidDomainName.NoExist') {
                logger.warn(`Check domain existence failed for ${domainName}:`, e.message);
            }
        }

        // 2. 尝试添加域名
        let addResult;
        try {
            addResult = await client.request('AddDomain', {
                DomainName: domainName
            });
        } catch (e) {
            // 如果添加失败，但错误提示是域名已存在，则再次尝试获取信息
            if (e.code === 'DomainAlreadyExist' || e.message.includes('exists')) {
                const info = await client.request('DescribeDomainInfo', {
                    DomainName: domainName
                });
                return {
                    DomainName: domainName,
                    DnsServers: info.DnsServers?.DnsServer || [],
                    AlreadyExists: true
                };
            }
            throw e;
        }

        // 3. 获取新添加域名的 NS 记录
        const info = await client.request('DescribeDomainInfo', {
            DomainName: domainName
        });

        return {
            ...addResult,
            DnsServers: info.DnsServers?.DnsServer || []
        };
    } catch (e) {
        // 针对所有权验证失败的特殊处理
        if (e.message.includes('TXT record') || e.code === 'VerificationFailed') {
            throw new Error(`域名所有权验证未通过。请确保已按阿里云要求添加 TXT 记录，或者尝试添加主域名而非子域名。详细错误: ${e.message}`);
        }
        throw new Error(`AddDomain Failed: ${e.message}`);
    }
}

/**
 * 删除域名
 */
async function deleteDomain(auth, domainName) {
    const client = createDnsClient(auth);
    try {
        const result = await client.request('DeleteDomain', {
            DomainName: domainName
        });
        return result;
    } catch (e) {
        throw new Error(`DeleteDomain Failed: ${e.message}`);
    }
}

/**
 * 获取监控数据
 */
async function getMetricData(auth, params) {
    const client = createCmsClient(auth);
    try {
        const result = await client.request('DescribeMetricList', {
            Namespace: params.namespace || 'acs_ecs_dashboard',
            MetricName: params.metricName,
            Dimensions: JSON.stringify(params.dimensions),
            StartTime: params.startTime,
            EndTime: params.endTime,
            Period: params.period || '60'
        });
        return result;
    } catch (e) {
        throw new Error(`GetMetricData Failed: ${e.message}`);
    }
}

/**
 * 获取轻量服务器防火墙规则
 */
async function listFirewallRules(auth, regionId, instanceId) {
    const client = createSwasClient(auth, regionId);
    try {
        const result = await client.request('ListFirewallRules', {
            InstanceId: instanceId,
            RegionId: regionId
        });
        return result.FirewallRules || [];
    } catch (e) {
        throw new Error(`ListFirewallRules Failed: ${e.message}`);
    }
}

/**
 * 添加防火墙规则
 */
async function createFirewallRule(auth, regionId, instanceId, rule) {
    const client = createSwasClient(auth, regionId);
    try {
        return await client.request('CreateFirewallRule', {
            InstanceId: instanceId,
            RegionId: regionId,
            RuleProtocol: rule.protocol,
            Port: rule.port,
            Remark: rule.remark
        });
    } catch (e) {
        throw new Error(`CreateFirewallRule Failed: ${e.message}`);
    }
}

/**
 * 删除防火墙规则
 */
async function deleteFirewallRule(auth, regionId, instanceId, ruleId) {
    const client = createSwasClient(auth, regionId);
    try {
        return await client.request('DeleteFirewallRule', {
            InstanceId: instanceId,
            RegionId: regionId,
            RuleId: ruleId
        });
    } catch (e) {
        throw new Error(`DeleteFirewallRule Failed: ${e.message}`);
    }
}

/**
 * 获取域名解析记录
 */
async function listDomainRecords(auth, domainName, options = {}) {
    const client = createDnsClient(auth);
    try {
        const result = await client.request('DescribeDomainRecords', {
            DomainName: domainName,
            PageSize: options.pageSize || 100,
            PageNumber: options.pageNumber || 1
        });
        return {
            records: result.DomainRecords?.Record || [],
            total: result.TotalCount
        };
    } catch (e) {
        throw new Error(`DescribeDomainRecords Failed: ${e.message}`);
    }
}

/**
 * 添加解析记录
 */
async function addDomainRecord(auth, domainName, record) {
    const client = createDnsClient(auth);
    try {
        const result = await client.request('AddDomainRecord', {
            DomainName: domainName,
            RR: record.RR,
            Type: record.Type,
            Value: record.Value,
            TTL: record.TTL || 600,
            Priority: record.Priority
        });
        return result;
    } catch (e) {
        throw new Error(`AddDomainRecord Failed: ${e.message}`);
    }
}

/**
 * 修改解析记录
 */
async function updateDomainRecord(auth, recordId, record) {
    const client = createDnsClient(auth);
    try {
        const result = await client.request('UpdateDomainRecord', {
            RecordId: recordId,
            RR: record.RR,
            Type: record.Type,
            Value: record.Value,
            TTL: record.TTL || 600,
            Priority: record.Priority
        });
        return result;
    } catch (e) {
        throw new Error(`UpdateDomainRecord Failed: ${e.message}`);
    }
}

/**
 * 删除解析记录
 */
async function deleteDomainRecord(auth, recordId) {
    const client = createDnsClient(auth);
    try {
        const result = await client.request('DeleteDomainRecord', {
            RecordId: recordId
        });
        return result;
    } catch (e) {
        throw new Error(`DeleteDomainRecord Failed: ${e.message}`);
    }
}

/**
 * 设置记录状态
 */
async function setDomainRecordStatus(auth, recordId, status) {
    const client = createDnsClient(auth);
    try {
        const result = await client.request('SetDomainRecordStatus', {
            RecordId: recordId,
            Status: status === 'Enable' ? 'Enable' : 'Disable'
        });
        return result;
    } catch (e) {
        throw new Error(`SetDomainRecordStatus Failed: ${e.message}`);
    }
}

// ==================== ECS 相关 ====================

/**
 * 获取可用区域
 */
async function listRegions(auth) {
    const client = createClient(auth, API_VERSIONS.ECS);
    try {
        const result = await client.request('DescribeRegions', {});
        return result.Regions.Region || [];
    } catch (e) {
        console.warn('Failed to fetch regions, fallback to defaults', e.message);
        return [
            { RegionId: 'cn-hangzhou', LocalName: '华东1 (杭州)' },
            { RegionId: 'cn-shanghai', LocalName: '华东2 (上海)' },
            { RegionId: 'cn-beijing', LocalName: '华北2 (北京)' },
            { RegionId: 'cn-shenzhen', LocalName: '华南1 (深圳)' },
            { RegionId: 'cn-hongkong', LocalName: '中国香港' }
        ];
    }
}

/**
 * 获取单个区域的实例
 */
async function listInstancesInRegion(auth, regionId, options = {}) {
    try {
        const client = createEcsClient(auth, regionId);
        const result = await client.request('DescribeInstances', {
            RegionId: regionId,
            PageSize: options.PageSize || 100,
            PageNumber: options.PageNumber || 1
        });
        return result.Instances.Instance || [];
    } catch (e) {
        // 某些区域可能未开通或无权访问，静默处理
        return [];
    }
}

/**
 * 获取所有区域的所有实例 (ECS)
 */
async function listAllInstances(auth, options = {}) {
    // 1. 获取所有区域
    const regions = await listRegions(auth);

    // 优先加载常用的中国大陆及香港区域，分散并发压力
    const priorityRegions = ['cn-hangzhou', 'cn-shanghai', 'cn-beijing', 'cn-shenzhen', 'cn-hongkong'];
    const otherRegions = regions.filter(r => !priorityRegions.includes(r.RegionId)).map(r => r.RegionId);

    const allInstances = [];

    // 分批查询，防止瞬间几百个请求导致 SSL 错误或限流
    // 先查优先级区域
    const priorityResults = await Promise.all(
        priorityRegions.map(rid => listInstancesInRegion(auth, rid, options))
    );
    priorityResults.forEach(list => allInstances.push(...list));

    // 再查其他区域 (分块进行，每块 5 个)
    const chunkSize = 5;
    for (let i = 0; i < otherRegions.length; i += chunkSize) {
        const chunk = otherRegions.slice(i, i + chunkSize);
        const chunkResults = await Promise.all(
            chunk.map(rid => listInstancesInRegion(auth, rid, options))
        );
        chunkResults.forEach(list => allInstances.push(...list));
    }

    return {
        instances: allInstances,
        total: allInstances.length
    };
}

/**
 * 获取实例列表
 */
async function listInstances(auth, options = {}) {
    if (options.allRegions) {
        return await listAllInstances(auth, options);
    }

    const client = createEcsClient(auth, auth.region_id || 'cn-hangzhou');
    try {
        const result = await client.request('DescribeInstances', {
            RegionId: auth.region_id || 'cn-hangzhou',
            PageSize: options.pageSize || 100,
            PageNumber: options.pageNumber || 1
        });
        return {
            instances: result.Instances.Instance || [],
            total: result.TotalCount
        };
    } catch (e) {
        throw new Error(`DescribeInstances Failed: ${e.message}`);
    }
}

/**
 * 启动实例
 */
async function startInstance(auth, regionId, instanceId) {
    const client = createEcsClient(auth, regionId || auth.regionId || 'cn-hangzhou');
    try {
        const result = await client.request('StartInstance', {
            InstanceId: instanceId
        });
        return result;
    } catch (e) {
        throw new Error(`StartInstance Failed: ${e.message}`);
    }
}

/**
 * 停止实例
 */
async function stopInstance(auth, regionId, instanceId, force = false) {
    const client = createEcsClient(auth, regionId || auth.regionId || 'cn-hangzhou');
    try {
        const result = await client.request('StopInstance', {
            InstanceId: instanceId,
            ForceStop: force ? 'true' : 'false'
        });
        return result;
    } catch (e) {
        throw new Error(`StopInstance Failed: ${e.message}`);
    }
}

/**
 * 重启实例
 */
async function rebootInstance(auth, regionId, instanceId, force = false) {
    const client = createEcsClient(auth, regionId || auth.regionId || 'cn-hangzhou');
    try {
        const result = await client.request('RebootInstance', {
            InstanceId: instanceId,
            ForceStop: force ? 'true' : 'false'
        });
        return result;
    } catch (e) {
        throw new Error(`RebootInstance Failed: ${e.message}`);
    }
}

// ==================== 轻量应用服务器 (SWAS) ====================

const SWAS_API_VERSION = '2020-06-01';

/**
 * 创建 SWAS Client
 */
function createSwasClient(auth, regionId) {
    if (!auth.accessKeyId || !auth.accessKeySecret) {
        throw new Error('Missing AccessKeyId or AccessKeySecret');
    }

    // 轻量服务器优先使用区域端点
    const endpoint = `swas.${regionId}.aliyuncs.com`;

    return new RPCClient({
        accessKeyId: auth.accessKeyId,
        accessKeySecret: auth.accessKeySecret,
        endpoint: `https://${endpoint}`,
        apiVersion: SWAS_API_VERSION,
        opts: { timeout: 15000 }
    });
}

/**
 * 获取支持轻量服务器的区域列表
 */
async function listSwasRegions(auth) {
    // 使用默认管理端点获取区域
    const client = createSwasClient(auth, 'cn-hangzhou');
    try {
        const result = await client.request('ListRegions', {});
        return result.Regions || [];
    } catch (e) {
        // 如果 ListRegions 失败，回退到常见可用区域列表
        return [
            { RegionId: 'cn-hangzhou' }, { RegionId: 'cn-shanghai' },
            { RegionId: 'cn-beijing' }, { RegionId: 'cn-shenzhen' },
            { RegionId: 'cn-hongkong' }, { RegionId: 'ap-southeast-1' },
            { RegionId: 'cn-wuhan-lrb' }, { RegionId: 'cn-chengdu' },
            { RegionId: 'cn-guangzhou' }, { RegionId: 'cn-qingdao' }
        ];
    }
}

/**
 * 获取单个区域的轻量服务器列表
 */
async function listSwasInRegion(auth, regionId, options = {}) {
    try {
        const client = createSwasClient(auth, regionId);
        const params = {
            RegionId: regionId,
            PageSize: options.pageSize || 100,
            PageNumber: options.pageNumber || 1
        };
        const result = await client.request('ListInstances', params);
        // SWAS 目前返回结构通常是 { Instances: [ ... ] }
        return result.Instances || [];
    } catch (e) {
        return [];
    }
}

/**
 * 获取所有区域的轻量应用服务器列表
 */
async function listSwasInstances(auth, options = {}) {
    // 1. 获取所有支持的区域
    const regions = await listSwasRegions(auth);

    // 2. 分批并行查询（每批 3-5 个，防止 SSL 压力过大）
    const allInstances = [];
    const batchSize = 5;

    for (let i = 0; i < regions.length; i += batchSize) {
        const batch = regions.slice(i, i + batchSize);
        const results = await Promise.all(
            batch.map(region => listSwasInRegion(auth, region.RegionId, options))
        );
        results.forEach(instances => {
            if (Array.isArray(instances)) {
                allInstances.push(...instances);
            }
        });
    }

    return {
        instances: allInstances,
        total: allInstances.length
    };
}

/**
 * 启动轻量服务器
 */
async function startSwasInstance(auth, regionId, instanceId) {
    const client = createSwasClient(auth, regionId);
    try {
        return await client.request('StartInstance', {
            InstanceId: instanceId
        });
    } catch (e) {
        throw new Error(`StartSwas Failed: ${e.message}`);
    }
}

/**
 * 停止轻量服务器
 */
async function stopSwasInstance(auth, regionId, instanceId, force = false) {
    const client = createSwasClient(auth, regionId);
    try {
        return await client.request('StopInstance', {
            InstanceId: instanceId,
            ForceStop: force
        });
    } catch (e) {
        throw new Error(`StopSwas Failed: ${e.message}`);
    }
}

/**
 * 重启轻量服务器
 */
async function rebootSwasInstance(auth, regionId, instanceId, force = false) {
    const client = createSwasClient(auth, regionId);
    try {
        return await client.request('RebootInstance', {
            InstanceId: instanceId,
            ForceStop: force
        });
    } catch (e) {
        throw new Error(`RebootSwas Failed: ${e.message}`);
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
    listInstances,
    startInstance,
    stopInstance,
    rebootInstance,
    listSwasInstances,
    startSwasInstance,
    stopSwasInstance,
    rebootSwasInstance,
    getMetricData,
    listFirewallRules,
    createFirewallRule,
    deleteFirewallRule,
    REGION_MAP,
    formatFlavor
};
