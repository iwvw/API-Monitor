// 转发中心画布 — mock 数据与生产一致（2026-08-27 快照）
// 开发态(USE_MOCK)用此数据预览画布；生产走真实 /api/server/forward。

export const servers = [
  { id: '957a711a-24af-4dbb-910e-4fe0286cb6a9', name: '笔电', host: '', status: 'online', agent_online: true, agent_connected: true, tags: ['Agent'] },
  { id: '39cd3303-fa37-4910-91da-65c395d1639b', name: 'ARM', host: '64.181.246.5', status: 'online', agent_online: true, agent_connected: true, tags: [] },
  { id: '23b3f7e7-dd63-48e1-bb1a-f8eeff774147', name: '香港', host: '189.1.217.109', status: 'online', agent_online: true, agent_connected: true, tags: [] },
  { id: 'dd14ac13-b110-48df-9898-81458f9b5cbc', name: '伦敦', host: '185.168.194.90', status: 'online', agent_online: true, agent_connected: true, tags: [] },
  { id: '39c4956c-17f1-42e4-8bec-180777f17a32', name: '法兰克福', host: '45.32.157.55', status: 'online', agent_online: true, agent_connected: true, tags: [] },
  { id: 'b43d4845-f043-485c-bd7d-2ba4899afb99', name: 'AMD1', host: '147.224.51.94', status: 'online', agent_online: true, agent_connected: true, tags: [] },
  { id: '04ad323f-da52-42d5-8a7d-813b8a48de01', name: 'AMD2', host: '146.235.217.209', status: 'online', agent_online: true, agent_connected: true, tags: [] },
];

export const forwards = [
  {
    id: 'fwd_20bff4ed7288e4e1', apply_status: 'running', access_mode: 'public',
    access_url: 'http://189.1.217.109:55655', name: '3000', server_id: '957a711a-24af-4dbb-910e-4fe0286cb6a9',
    server_name: '笔电', local_host: '127.0.0.1', local_port: 3000, protocol: 'http',
    transport: 'tcp_relay', relay_server_id: '23b3f7e7-dd63-48e1-bb1a-f8eeff774147',
    relay_server_name: '香港', relay_server_host: '189.1.217.109', remote_port: 55655, udp: false, whole_host: false,
    created_at: '2026-08-26 23:30:09',
  },
  {
    id: 'fwd_7a6262d4d149522c', apply_status: 'running', access_mode: 'public',
    access_url: 'tcp://64.181.246.5:55655', name: 'aaa', server_id: '957a711a-24af-4dbb-910e-4fe0286cb6a9',
    server_name: '笔电', local_host: '127.0.0.1', local_port: 3000, protocol: 'tcp',
    transport: 'tcp_relay', relay_server_id: '39cd3303-fa37-4910-91da-65c395d1639b',
    relay_server_name: 'ARM', relay_server_host: '64.181.246.5', remote_port: 55655, udp: false, whole_host: false,
    created_at: '2026-08-27 06:21:45',
  },
  {
    id: 'fwd_ca187af839ebc944', apply_status: 'running', access_mode: 'public',
    access_url: 'udp://189.1.217.109:55658', name: 'udp-41234', server_id: '957a711a-24af-4dbb-910e-4fe0286cb6a9',
    server_name: '笔电', local_host: '127.0.0.1', local_port: 41234, protocol: 'tcp',
    transport: 'tcp_relay', relay_server_id: '23b3f7e7-dd63-48e1-bb1a-f8eeff774147',
    relay_server_name: '香港', relay_server_host: '189.1.217.109', remote_port: 55658, udp: true, whole_host: false,
    created_at: '2026-08-27 07:44:18',
  },
  {
    id: 'fwd_0c48800c8528a754', apply_status: 'running', access_mode: 'public',
    access_url: 'http://189.1.217.109:55656', name: 'HTTP', server_id: '39cd3303-fa37-4910-91da-65c395d1639b',
    server_name: 'ARM', local_host: '127.0.0.1', local_port: 80, protocol: 'http',
    transport: 'tcp_relay', relay_server_id: '23b3f7e7-dd63-48e1-bb1a-f8eeff774147',
    relay_server_name: '香港', relay_server_host: '189.1.217.109', remote_port: 55656, udp: false, whole_host: false,
    created_at: '2026-08-27 06:23:13',
  },
  {
    id: 'fwd_795d57385d6f433d', apply_status: 'running', access_mode: 'public',
    access_url: 'tcp://64.181.246.5:55656', name: 'HTTP', server_id: '957a711a-24af-4dbb-910e-4fe0286cb6a9',
    server_name: '笔电', local_host: '127.0.0.1', local_port: 3389, protocol: 'tcp',
    transport: 'tcp_relay', relay_server_id: '39cd3303-fa37-4910-91da-65c395d1639b',
    relay_server_name: 'ARM', relay_server_host: '64.181.246.5', remote_port: 55656, udp: false, whole_host: false,
    created_at: '2026-08-27 06:22:50',
  },
  {
    id: 'fwd_56a28e363cfe9cac', apply_status: 'running', access_mode: 'token',
    access_url: 'http://fwd-demo.085014.xyz/fwd/fwd_56a28e363cfe9cac', name: 'cf-token-3000',
    server_id: '957a711a-24af-4dbb-910e-4fe0286cb6a9', server_name: '笔电', local_host: '127.0.0.1',
    local_port: 3000, protocol: 'http', transport: 'cloudflare_tunnel', udp: false, whole_host: false,
    tunnel_hostname: 'fwd-demo.085014.xyz', tunnel_path: '/fwd/fwd_56a28e363cfe9cac',
    created_at: '2026-08-27 05:07:28',
  },
  {
    id: 'fwd_5021dbc6648270ab', apply_status: 'running', access_mode: 'public',
    access_url: 'https://fwd-demo.085014.xyz', name: 'CF-3000', server_id: '957a711a-24af-4dbb-910e-4fe0286cb6a9',
    server_name: '笔电', local_host: '127.0.0.1', local_port: 3000, protocol: 'https',
    transport: 'cloudflare_tunnel', udp: false, whole_host: true,
    tunnel_hostname: 'fwd-demo.085014.xyz',
    created_at: '2026-08-27 00:33:58',
  },
];