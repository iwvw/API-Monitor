/**
 * Zeabur API 服务
 */

const https = require('https');

/**
 * Zeabur GraphQL 查询
 */
async function queryZeabur(token, query) {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify({ query });
    const options = {
      hostname: 'api.zeabur.com',
      path: '/graphql',
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/json',
        'Content-Length': data.length
      },
      timeout: 10000
    };

    const req = https.request(options, (res) => {
      let body = '';
      res.on('data', (chunk) => body += chunk);
      res.on('end', () => {
        try {
          resolve(JSON.parse(body));
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
    req.write(data);
    req.end();
  });
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

  const [userData, projectsData, aihubData, serviceCostsData] = await Promise.all([
    queryZeabur(token, userQuery),
    queryZeabur(token, projectsQuery),
    queryZeabur(token, aihubQuery),
    queryZeabur(token, serviceCostsQuery)
  ]);

  const user = userData?.data?.me || {};
  const projects = projectsData?.data?.projects?.edges?.map(e => e.node) || [];
  const aihub = aihubData?.data?.aihubTenant || {};
  const serviceCosts = serviceCostsData?.data?.me?.serviceCostsThisMonth || 0;

  return { user, projects, aihub, serviceCosts };
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
      userID: userID
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
    }`
  };

  return new Promise((resolve, reject) => {
    const data = JSON.stringify(usageQuery);
    const options = {
      hostname: 'api.zeabur.com',
      path: '/graphql',
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(data)
      },
      timeout: 10000
    };

    const req = https.request(options, (res) => {
      let body = '';
      res.on('data', (chunk) => body += chunk);
      res.on('end', () => {
        try {
          const result = JSON.parse(body);
          const usages = result.data?.usages?.data || [];

          const projectCosts = {};
          let totalUsage = 0;

          usages.forEach(project => {
            const projectTotal = project.usageOfEntity.reduce((a, b) => a + b, 0);
            const displayCost = projectTotal > 0 ? Math.ceil(projectTotal * 100) / 100 : 0;
            projectCosts[project.id] = displayCost;
            totalUsage += projectTotal;
          });

          resolve({
            projectCosts,
            totalUsage,
            freeQuotaRemaining: 5 - totalUsage,
            freeQuotaLimit: 5
          });
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
    req.write(data);
    req.end();
  });
}

module.exports = {
  queryZeabur,
  fetchAccountData,
  fetchUsageData
};
