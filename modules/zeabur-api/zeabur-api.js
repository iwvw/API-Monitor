/**
 * Zeabur API 服务
 */

const https = require('https');
const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('Zeabur');

const DEFAULT_ZEABUR_API_HOSTS = ['api.zeabur.com', 'api.zeabur.cn'];
let preferredZeaburApiHost = null;

function getZeaburApiHosts() {
  const configured = String(process.env.ZEABUR_API_HOSTS || process.env.ZEABUR_API_HOST || '')
    .split(/[,\s]+/)
    .map(item => item.trim().toLowerCase())
    .filter(Boolean);

  const merged = [...configured, ...DEFAULT_ZEABUR_API_HOSTS];
  const deduped = [];
  for (const host of merged) {
    if (!deduped.includes(host)) deduped.push(host);
  }

  if (preferredZeaburApiHost && deduped.includes(preferredZeaburApiHost)) {
    return [preferredZeaburApiHost, ...deduped.filter(host => host !== preferredZeaburApiHost)];
  }
  return deduped;
}

function shouldRetryOnHost(error) {
  if (!error) return false;
  if (error.retryable === true) return true;

  const message = String(error.message || '');
  return (
    message.includes('timeout') ||
    message.includes('ECONNRESET') ||
    message.includes('ENOTFOUND') ||
    message.includes('EAI_AGAIN') ||
    message.includes('socket hang up')
  );
}

function postGraphQL(hostname, token, payload) {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify(payload);
    const options = {
      hostname,
      path: '/graphql',
      method: 'POST',
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(data),
      },
      timeout: 10000,
    };

    const req = https.request(options, res => {
      let body = '';
      res.on('data', chunk => (body += chunk));
      res.on('end', () => {
        const statusCode = res.statusCode || 0;
        const contentType = String(res.headers['content-type'] || '').toLowerCase();

        if (statusCode >= 500 || statusCode === 429 || statusCode === 408) {
          const error = new Error(`HTTP ${statusCode} from ${hostname}`);
          error.retryable = true;
          error.statusCode = statusCode;
          return reject(error);
        }

        try {
          if (statusCode >= 400) {
            // GraphQL 在某些网关上会返回 4xx，但 body 依然是标准 { data, errors }
            // 这类响应不应直接当作传输失败，让上层按 GraphQL errors 继续处理。
            const parsed = JSON.parse(body);
            if (parsed && typeof parsed === 'object') {
              parsed.__httpStatus = statusCode;
              parsed.__host = hostname;
              return resolve(parsed);
            }
          }

          // 部分网关错误可能返回 HTML，强制 JSON 解析失败后进入回退
          if (!contentType.includes('json') && body.trim().startsWith('<')) {
            const error = new Error(`Non-JSON response from ${hostname}`);
            error.retryable = true;
            error.statusCode = statusCode;
            return reject(error);
          }
          resolve(JSON.parse(body));
        } catch (e) {
          if (statusCode >= 400) {
            const error = new Error(`HTTP ${statusCode} from ${hostname}`);
            error.retryable = false;
            error.statusCode = statusCode;
            error.body = body;
            return reject(error);
          }
          const error = new Error(`Invalid JSON response from ${hostname}`);
          error.retryable = true;
          error.statusCode = statusCode;
          reject(error);
        }
      });
    });

    req.on('error', error => {
      error.retryable = true;
      reject(error);
    });

    req.on('timeout', () => {
      const error = new Error(`Request timeout (${hostname})`);
      error.retryable = true;
      req.destroy(error);
    });

    req.write(data);
    req.end();
  });
}

async function requestZeaburGraphQL(token, payload) {
  const hosts = getZeaburApiHosts();
  let lastError = null;

  for (let i = 0; i < hosts.length; i++) {
    const host = hosts[i];
    try {
      const result = await postGraphQL(host, token, payload);
      if (preferredZeaburApiHost !== host) {
        preferredZeaburApiHost = host;
        logger.info(`Zeabur API host switched to: ${host}`);
      }
      return result;
    } catch (error) {
      lastError = error;
      const isLast = i === hosts.length - 1;
      if (!shouldRetryOnHost(error) || isLast) {
        break;
      }
      logger.warn(`Zeabur request failed on ${host}, fallback to next host: ${error.message}`);
    }
  }

  throw lastError || new Error('Zeabur request failed');
}

/**
 * Zeabur GraphQL 查询
 */
async function queryZeabur(token, query) {
  return requestZeaburGraphQL(token, { query });
}

/**
 * 获取用户信息和项目
 */
async function fetchAccountData(token) {
  const userQuery = `
    query {
      me {
        _id
        username
        email
        credit
      }
    }
  `;

  const projectsQuery = `
    query {
      projects {
        edges {
          node {
            _id
            name
            region {
              name
            }
            environments {
              _id
            }
            services {
              _id
              name
              status
              template
              resourceLimit {
                cpu
                memory
              }
              domains {
                domain
                isGenerated
                environmentID
              }
            }
          }
        }
      }
    }
  `;

  const aihubQuery = `
    query GetAIHubTenant {
      aihubTenant {
        balance
        keys {
          keyID
          alias
          cost
        }
      }
    }
  `;

  const serviceCostsQuery = `
    query {
      me {
        serviceCostsThisMonth
      }
    }
  `;

  // 用户与项目是核心数据；AIHub 与 serviceCosts 为可选增强信息，失败不阻塞主流程。
  const [userData, projectsData] = await Promise.all([
    queryZeabur(token, userQuery),
    queryZeabur(token, projectsQuery),
  ]);

  const optionalResults = await Promise.allSettled([
    queryZeabur(token, aihubQuery),
    queryZeabur(token, serviceCostsQuery),
  ]);
  const aihubData = optionalResults[0].status === 'fulfilled' ? optionalResults[0].value : null;
  const serviceCostsData = optionalResults[1].status === 'fulfilled' ? optionalResults[1].value : null;

  if (optionalResults[0].status === 'rejected') {
    logger.warn(`AIHub 查询失败（已降级）: ${optionalResults[0].reason?.message || 'unknown error'}`);
  } else if (Array.isArray(aihubData?.errors) && aihubData.errors.length > 0) {
    logger.warn(`AIHub 查询返回业务错误（已降级）: ${aihubData.errors[0]?.message || 'unknown error'}`);
  }

  if (optionalResults[1].status === 'rejected') {
    logger.warn(
      `serviceCosts 查询失败（已降级）: ${optionalResults[1].reason?.message || 'unknown error'}`
    );
  } else if (Array.isArray(serviceCostsData?.errors) && serviceCostsData.errors.length > 0) {
    logger.warn(
      `serviceCosts 查询返回业务错误（已降级）: ${serviceCostsData.errors[0]?.message || 'unknown error'}`
    );
  }

  const user = userData?.data?.me || {};
  if (!user?._id && Array.isArray(userData?.errors) && userData.errors.length > 0) {
    throw new Error(userData.errors[0]?.message || '无法获取用户信息');
  }

  const queryProjects = projectsData?.data?.projects?.edges?.map(e => e.node) || [];
  if (!Array.isArray(queryProjects) && Array.isArray(projectsData?.errors) && projectsData.errors.length > 0) {
    logger.warn(`项目查询返回业务错误（已降级）: ${projectsData.errors[0]?.message || 'unknown error'}`);
  }

  const aihub = aihubData?.data?.aihubTenant || {};
  const serviceCosts = serviceCostsData?.data?.me?.serviceCostsThisMonth || 0;

  // 在后端直接转换地域为中文
  const projects = queryProjects.map(project => {
    if (project.region && project.region.name) {
      project.region.name = mapZeaburRegion(project.region.name);
    }
    return project;
  });

  return { user, projects, aihub, serviceCosts };
}

/**
 * 内部辅助：映射 Zeabur 地域为中文
 */
function mapZeaburRegion(region) {
  if (!region) return '';
  const lowerRegion = region.toLowerCase();

  const regionMap = {
    silicon: '硅谷',
    jakarta: '雅加达',
    'hong kong': '香港',
    tokyo: '东京',
    singapore: '新加坡',
    frankfurt: '法兰克福',
    london: '伦敦',
    sydney: '悉尼',
  };

  for (const [key, value] of Object.entries(regionMap)) {
    if (lowerRegion.includes(key)) return value;
  }
  return region;
}

/**
 * 获取项目用量数据
 */
async function fetchUsageData(token, userID, projects = []) {
  const now = new Date();
  const year = now.getFullYear();
  const month = now.getMonth() + 1;
  const fromDate = `${year}-${String(month).padStart(2, '0')}-01`;

  const tomorrow = new Date(now);
  tomorrow.setDate(tomorrow.getDate() + 1);
  const toDate = `${tomorrow.getFullYear()}-${String(tomorrow.getMonth() + 1).padStart(2, '0')}-${String(tomorrow.getDate()).padStart(2, '0')}`;

  const usageQuery = {
    operationName: 'GetHeaderMonthlyUsage',
    variables: {
      from: fromDate,
      to: toDate,
      groupByEntity: 'PROJECT',
      groupByTime: 'DAY',
      groupByType: 'ALL',
      userID: userID,
    },
    query: `query GetHeaderMonthlyUsage($from: String!, $to: String!, $groupByEntity: GroupByEntity, $groupByTime: GroupByTime, $groupByType: GroupByType, $userID: ObjectID!) {
      usages(
        from: $from
        to: $to
        groupByEntity: $groupByEntity
        groupByTime: $groupByTime
        groupByType: $groupByType
        userID: $userID
      ) {
        categories
        data {
          id
          name
          groupByEntity
          usageOfEntity
          __typename
        }
        __typename
      }
    }`,
  };

  const result = await requestZeaburGraphQL(token, usageQuery);
  const usages = result.data?.usages?.data || [];

  const projectCosts = {};
  let totalUsage = 0;

  usages.forEach(project => {
    const projectTotal = project.usageOfEntity.reduce((a, b) => a + b, 0);
    const displayCost = projectTotal > 0 ? Math.ceil(projectTotal * 100) / 100 : 0;
    projectCosts[project.id] = displayCost;
    totalUsage += projectTotal;
  });

  // 调试日志：输出原始用量数据
  if (process.env.DEBUG_ZEABUR === 'true') {
    logger.debug('Raw usages:', JSON.stringify(usages, null, 2));
    logger.debug('totalUsage:', totalUsage, 'freeQuotaRemaining:', 5 - totalUsage);
  }

  return {
    projectCosts,
    totalUsage,
    freeQuotaRemaining: 5 - totalUsage,
    freeQuotaLimit: 5,
  };
}

module.exports = {
  queryZeabur,
  fetchAccountData,
  fetchUsageData,
};
