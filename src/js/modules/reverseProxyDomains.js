// 容器 ↔ 反代域名匹配。
//
// 数据链路：1Panel 站点反代上游是「宿主端口」（如 http://127.0.0.1:2260），
// 容器列表里的端口是 Agent 上报的「公网:容器」串（如 2260:2260）。
// 两者唯一可靠的连接点是宿主端口，所以这里只按宿主端口匹配，绝不按容器名猜。

const splitPortText = value =>
  String(value || '')
    .split(',')
    .map(item => item.trim())
    .filter(Boolean);

// 与 docker-format 的规则一致：只认 "公网:容器"，冒号前是宿主端口。
const getHostPort = label => {
  const match = /^(\d+):\d+(\/\w+)?$/.exec(String(label || '').trim());
  return match ? Number(match[1]) : 0;
};

export const getDockerContainerHostPorts = container => {
  const ports = container?.ports ?? container?.Ports ?? container?.portMappings;
  const labels = [];

  if (typeof ports === 'string') {
    labels.push(...splitPortText(ports));
  } else if (Array.isArray(ports)) {
    for (const item of ports) {
      if (typeof item === 'string') {
        labels.push(...splitPortText(item));
        continue;
      }
      const hostPort = item?.PublicPort ?? item?.publicPort ?? item?.hostPort;
      const containerPort = item?.PrivatePort ?? item?.privatePort ?? item?.containerPort;
      if (hostPort !== undefined && hostPort !== null && containerPort !== undefined && containerPort !== null) {
        labels.push(`${hostPort}:${containerPort}`);
      }
    }
  } else if (ports && typeof ports === 'object') {
    for (const [spec, bindings] of Object.entries(ports)) {
      const containerPort = String(spec).split('/')[0];
      if (Array.isArray(bindings)) {
        for (const binding of bindings) {
          const hostPort = binding?.HostPort ?? binding?.hostPort;
          if (hostPort !== undefined && hostPort !== null) labels.push(`${hostPort}:${containerPort}`);
        }
      }
    }
  }

  const seen = new Set();
  const result = [];
  for (const label of labels) {
    const port = getHostPort(label);
    if (port > 0 && !seen.has(port)) {
      seen.add(port);
      result.push(port);
    }
  }
  return result;
};

// targets 为后端 /reverse-proxy/domains 的返回（数组）；返回该容器命中的域名条目。
// 一个容器可命中多个上游端口，去重后按域名排序，首个作为默认打开目标。
export const matchContainerReverseProxyDomains = (container, targets) => {
  const hostPorts = getDockerContainerHostPorts(container);
  if (hostPorts.length === 0 || !Array.isArray(targets)) return [];

  const matched = [];
  const seen = new Set();
  for (const target of targets) {
    const port = Number(target?.upstreamPort);
    if (!hostPorts.includes(port)) continue;
    for (const entry of target.domains || []) {
      const url = String(entry?.url || '').trim();
      if (!url || seen.has(url)) continue;
      seen.add(url);
      matched.push({
        domain: String(entry?.domain || '').trim(),
        url,
        aliases: Array.isArray(entry?.domains) ? entry.domains.filter(Boolean) : [],
        siteId: entry?.siteId,
        status: entry?.status,
        upstreamPort: port,
      });
    }
  }
  matched.sort((a, b) => a.domain.localeCompare(b.domain));
  return matched;
};
