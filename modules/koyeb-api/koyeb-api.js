/**
 * Koyeb API 服务
 */

const https = require('https');

const KOYEB_API_BASE = 'app.koyeb.com';

/**
 * Koyeb API 请求
 */
async function koyebRequest(token, path, method = 'GET', body = null) {
  // 只移除换行符和制表符，保留空格和其他字符
  const cleanToken = token.replace(/[\r\n\t]/g, '').trim();

  return new Promise((resolve, reject) => {
    const options = {
      hostname: KOYEB_API_BASE,
      path: `/v1${path}`,
      method: method,
      headers: {
        Authorization: `Bearer ${cleanToken}`,
        'Content-Type': 'application/json',
      },
      timeout: 15000,
    };

    if (body) {
      const data = JSON.stringify(body);
      options.headers['Content-Length'] = Buffer.byteLength(data);
    }

    const req = https.request(options, res => {
      let responseBody = '';
      res.on('data', chunk => (responseBody += chunk));
      res.on('end', () => {
        try {
          const parsed = JSON.parse(responseBody);
          if (res.statusCode >= 200 && res.statusCode < 300) {
            resolve(parsed);
          } else {
            reject(new Error(parsed.message || `HTTP ${res.statusCode}`));
          }
        } catch (e) {
          reject(new Error('Invalid JSON response'));
        }
      });
    });

    req.on('error', reject);
    req.on('timeout', () => {
      req.destroy();
      reject(new Error('Request timeout'));
    });

    if (body) {
      req.write(JSON.stringify(body));
    }
    req.end();
  });
}

/**
 * 获取账户信息
 */
async function fetchAccountData(token) {
  try {
    // 先获取应用列表来验证 token 有效�?
    const appsResponse = await koyebRequest(token, '/apps');
    const apps = appsResponse.apps || [];

    // 获取组织信息（用于余额等�?
    let organization = null;
    let profile = null;

    // 尝试获取用户信息（PAT Token 可用�?
    try {
      const profileData = await koyebRequest(token, '/account/profile');
      profile = profileData.user;
    } catch (e) {
      // 忽略 403/404，可能是 Organization Token
    }

    try {
      const orgs = await koyebRequest(token, '/organizations');
      if (orgs.organizations && orgs.organizations.length > 0) {
        organization = orgs.organizations[0];
      }
    } catch (e) {
      console.warn('获取组织信息失败:', e.message);
    }

    // 如果没有 profile，使�?organization 信息回退
    if (!profile && organization) {
      profile = {
        id: organization.id,
        name: organization.name,
        email: organization.email || '', // 某些组织对象可能不含 email
        avatar_url: organization.avatar_url,
      };
    } else if (!profile) {
      profile = { id: 'unknown', name: 'Koyeb User', email: '' };
    }

    // 获取每个应用的服�?
    const appsWithServices = await Promise.all(
      apps.map(async app => {
        try {
          const servicesResponse = await koyebRequest(token, `/services?app_id=${app.id}`);
          const services = servicesResponse.services || [];

          // 获取每个服务的部署信�?
          const servicesWithDetails = await Promise.all(
            services.map(async service => {
              try {
                // 获取最新部�?
                const deploymentsResponse = await koyebRequest(
                  token,
                  `/deployments?service_id=${service.id}&limit=1`
                );
                const deployments = deploymentsResponse.deployments || [];
                const latestDeployment = deployments[0] || null;

                // 获取服务域名
                const domains = [];
                if (service.active_deployment) {
                  // Koyeb 自动生成的域�?
                  if (app.name && service.name) {
                    domains.push({
                      domain: `${app.name}-${service.name}.koyeb.app`,
                      isGenerated: true,
                    });
                  }
                }
                // 自定义域�?
                if (service.domains) {
                  service.domains.forEach(d => {
                    domains.push({
                      domain: d.name || d,
                      isGenerated: false,
                    });
                  });
                }

                return {
                  _id: service.id,
                  name: service.name,
                  status: mapKoyebStatus(service.status || service.state),
                  type: service.type || 'web',
                  resourceLimit: {
                    cpu: extractCpuFromInstance(service.definition?.instance_types?.[0]?.type),
                    memory: extractMemoryFromInstance(
                      service.definition?.instance_types?.[0]?.type
                    ),
                  },
                  domains: domains,
                  latestDeployment: latestDeployment,
                  messages: service.messages || [],
                  createdAt: service.created_at,
                  updatedAt: service.updated_at,
                };
              } catch (e) {
                console.warn(`获取服务 ${service.name} 详情失败:`, e.message);
                return {
                  _id: service.id,
                  name: service.name,
                  status: mapKoyebStatus(service.status || service.state),
                  type: service.type || 'web',
                  resourceLimit: { cpu: 0, memory: 0 },
                  domains: [],
                  error: e.message,
                };
              }
            })
          );

          // 尝试从服务实例中获取地区
          let appRegion = 'unknown';
          if (servicesWithDetails.length > 0) {
            const firstService = services[0];
            try {
              // 获取第一个服务的实例来确定地�?
              const instancesResponse = await koyebRequest(
                token,
                `/instances?service_id=${firstService.id}&limit=1`
              );
              const instances = instancesResponse.instances || [];
              if (instances.length > 0) {
                appRegion = instances[0].region || 'unknown';
              }
            } catch (e) {
              console.warn(`获取应用 ${app.name} 地区失败:`, e.message);
            }
          }

          return {
            _id: app.id,
            name: app.name,
            region: mapKoyebRegion(appRegion), // 从实例中获取的地�?
            services: servicesWithDetails,
            createdAt: app.created_at,
            updatedAt: app.updated_at,
          };
        } catch (e) {
          console.warn(`获取应用 ${app.name} 服务失败:`, e.message);
          return {
            _id: app.id,
            name: app.name,
            region: 'unknown',
            services: [],
            error: e.message,
          };
        }
      })
    );

    return {
      user: {
        _id: profile.id || organization?.id,
        username: profile.name || organization?.name || 'Unknown',
        email: profile.email || organization?.email || '',
      },
      organization: organization,
      projects: appsWithServices,
      balance: organization?.remaining_credits || 0, // Koyeb 使用 credits
    };
  } catch (error) {
    throw error;
  }
}

/**
 * 获取服务日志
 */
async function fetchServiceLogs(token, serviceId, limit = 100) {
  try {
    // 使用 streams/logs/query 接口，默认为 runtime 日志
    const response = await koyebRequest(
      token,
      `/streams/logs/query?service_id=${serviceId}&type=runtime&limit=${limit}`
    );
    return response.result || [];
  } catch (error) {
    console.warn(`获取服务日志失败 (尝试旧接�?: ${error.message}`);
    try {
      // 备选方案：尝试旧的 /logs 接口
      const response = await koyebRequest(
        token,
        `/logs?service_id=${serviceId}&limit=${limit}&order=desc`
      );
      return response.logs || [];
    } catch (e) {
      throw error;
    }
  }
}

/**
 * 重命名应�?
 */
async function renameApp(token, appId, newName) {
  try {
    await koyebRequest(token, `/apps/${appId}`, 'PATCH', { name: newName });
    return { success: true };
  } catch (error) {
    throw error;
  }
}

/**
 * 重命名服�?
 */
async function renameService(token, serviceId, newName) {
  try {
    await koyebRequest(token, `/services/${serviceId}`, 'PATCH', { name: newName });
    return { success: true };
  } catch (error) {
    throw error;
  }
}

/**
 * 暂停服务 (Koyeb 通过设置 scale �?0 来暂�?
 */
async function pauseService(token, serviceId) {
  try {
    // 获取当前服务定义
    const service = await koyebRequest(token, `/services/${serviceId}`);

    // 创建新部署，将实例数设为 0
    const definition = service.service?.definition;
    if (definition) {
      definition.scaling = { min: 0, max: 0 };

      await koyebRequest(token, `/services/${serviceId}/pause`, 'POST');
    }

    return { success: true };
  } catch (error) {
    throw error;
  }
}

/**
 * 恢复/重启服务
 */
async function restartService(token, serviceId) {
  try {
    // 先获取服务信息以判断状态
    const serviceResponse = await koyebRequest(token, `/services/${serviceId}`);
    const service = serviceResponse.service;

    if (!service) {
      throw new Error('服务不存在');
    }

    // 判断服务状态
    const status = service.status?.toUpperCase();

    // 如果服务是暂停/停止状态,使用 resume
    if (status === 'PAUSED' || status === 'STOPPED' || status === 'SUSPENDED') {
      await koyebRequest(token, `/services/${serviceId}/resume`, 'POST');
    } else {
      // 如果服务正在运行或其他状态,使用 redeploy 来重启
      await koyebRequest(token, `/services/${serviceId}/redeploy`, 'POST');
    }

    return { success: true };
  } catch (error) {
    throw error;
  }
}

/**
 * 重新部署服务
 */
async function redeployService(token, serviceId) {
  try {
    await koyebRequest(token, `/services/${serviceId}/redeploy`, 'POST');
    return { success: true };
  } catch (error) {
    throw error;
  }
}

/**
 * 删除服务
 */
async function deleteService(token, serviceId) {
  try {
    await koyebRequest(token, `/services/${serviceId}`, 'DELETE');
    return { success: true };
  } catch (error) {
    throw error;
  }
}

/**
 * 删除应用
 */
async function deleteApp(token, appId) {
  try {
    await koyebRequest(token, `/apps/${appId}`, 'DELETE');
    return { success: true };
  } catch (error) {
    throw error;
  }
}

/**
 * 获取服务实例列表
 */
async function fetchServiceInstances(token, serviceId) {
  try {
    const response = await koyebRequest(token, `/instances?service_id=${serviceId}`);
    return response.instances || [];
  } catch (error) {
    throw error;
  }
}

/**
 * 获取服务指标
 */
async function fetchServiceMetrics(
  token,
  serviceId,
  instanceId = null,
  metricName = 'CPU_TOTAL_PERCENT',
  start = null,
  end = null
) {
  try {
    let path = `/streams/metrics?name=${metricName}`;
    if (instanceId) {
      path += `&instance_id=${instanceId}`;
    } else {
      path += `&service_id=${serviceId}`;
    }

    if (start) path += `&start=${start}`;
    if (end) path += `&end=${end}`;

    const response = await koyebRequest(token, path);
    return response.metrics || [];
  } catch (error) {
    throw error;
  }
}

/**
 * 获取组织使用情况
 */
async function fetchOrganizationUsage(token, startTime = null, endTime = null) {
  try {
    let path = '/usages';
    const params = [];
    if (startTime) params.push(`starting_time=${startTime}`);
    if (endTime) params.push(`ending_time=${endTime}`);

    if (params.length > 0) {
      path += '?' + params.join('&');
    }

    const response = await koyebRequest(token, path);
    return response;
  } catch (error) {
    throw error;
  }
}

// ============ 辅助函数 ============

/**
 * �?Koyeb 状态映射为标准状�?
 */
function mapKoyebStatus(status) {
  const statusMap = {
    STARTING: 'STARTING',
    HEALTHY: 'RUNNING',
    UNHEALTHY: 'ERROR',
    STOPPING: 'STOPPING',
    STOPPED: 'SUSPENDED',
    PAUSING: 'STOPPING',
    PAUSED: 'SUSPENDED',
    RESUMING: 'STARTING',
    ERRORED: 'ERROR',
    DELETING: 'DELETING',
  };
  return statusMap[status?.toUpperCase()] || status || 'UNKNOWN';
}

/**
 * 将 Koyeb 地区代码/名称映射为中文
 */
function mapKoyebRegion(region) {
  if (!region) return '未知地区';

  // 调试日志：确认接收到的地区字符串
  console.log(`[Koyeb] Mapping region: "${region}"`);

  const regionMap = {
    was: '华盛顿',
    fra: '法兰克福',
    par: '巴黎',
    sin: '新加坡',
    tok: '东京',
    sfo: '旧金山',
    'silicon valley': '硅谷',
    'united states': '美国',
    germany: '德国',
    france: '法国',
    singapore: '新加坡',
    japan: '日本',
    washington: '华盛顿',
    frankfurt: '法兰克福',
    paris: '巴黎',
    tokyo: '东京',
  };

  const lowerRegion = region.toLowerCase();

  // 1. 优先尝试完全匹配
  if (regionMap[lowerRegion]) return regionMap[lowerRegion];

  // 2. 尝试关键词包含匹配（按长度倒序，确保长词优先，如 "silicon valley" 优于 "united states"）
  const keys = Object.keys(regionMap).sort((a, b) => b.length - a.length);
  for (const key of keys) {
    if (lowerRegion.includes(key)) {
      return regionMap[key];
    }
  }

  return region;
}

/**
 * 从实例类型提�?CPU 配置 (毫核)
 */
function extractCpuFromInstance(instanceType) {
  if (!instanceType) return 0;
  // Koyeb 实例类型示例: nano, micro, small, medium, large, xlarge
  const cpuMap = {
    free: 100,
    nano: 100,
    micro: 250,
    small: 500,
    medium: 1000,
    large: 2000,
    xlarge: 4000,
  };
  return cpuMap[instanceType?.toLowerCase()] || 0;
}

/**
 * 从实例类型提取内存配�?(MB)
 */
function extractMemoryFromInstance(instanceType) {
  if (!instanceType) return 0;
  const memoryMap = {
    free: 256,
    nano: 256,
    micro: 512,
    small: 1024,
    medium: 2048,
    large: 4096,
    xlarge: 8192,
  };
  return memoryMap[instanceType?.toLowerCase()] || 0;
}

module.exports = {
  koyebRequest,
  fetchAccountData,
  fetchServiceLogs,
  pauseService,
  restartService,
  redeployService,
  deleteService,
  deleteApp,
  fetchServiceInstances,
  fetchServiceMetrics,
  fetchOrganizationUsage,
  renameApp,
  renameService,
};
