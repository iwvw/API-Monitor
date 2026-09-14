export const RECORD_TYPE_OPTIONS = ['A', 'AAAA', 'CNAME', 'TXT', 'MX', 'NS', 'SRV', 'CAA', 'PTR'];

export const SSL_MODES = [
  { value: 'off', label: '关闭' },
  { value: 'flexible', label: '灵活' },
  { value: 'full', label: '完全' },
  { value: 'strict', label: '严格' },
];

export const SSL_MODE_LABELS = Object.fromEntries(SSL_MODES.map((mode) => [mode.value, mode.label]));

export const ZONE_TYPE_LABELS = {
  full: '完全',
  partial: '部分',
};

export const RECORD_TYPE_BADGE_VARIANTS = {
  A: 'blue',
  AAAA: 'teal',
  CNAME: 'purple',
  TXT: 'neutral',
  MX: 'orange',
  NS: 'green',
  SRV: 'info',
  CAA: 'warning',
  PTR: 'secondary',
};

export const EMPTY_ANALYTICS = {
  requests: 0,
  bandwidth: 0,
  cachedRequests: 0,
  cachedBytes: 0,
  threats: 0,
  pageViews: 0,
  uniques: 0,
  cacheHitRate: 0,
  timeseries: [],
};

export const EMPTY_ACCOUNT_FORM = {
  name: '',
  email: '',
  cfAccountId: '',
  apiToken: '',
  skipVerify: false,
};

export const EMPTY_ZONE_FORM = {
  name: '',
  jumpStart: false,
};

export const EMPTY_RECORD_FORM = {
  type: 'A',
  name: '@',
  content: '',
  ttl: 1,
  proxied: false,
  priority: 10,
};

export const EMPTY_TEMPLATE_FORM = {
  name: '',
  description: '',
  type: 'A',
  recordName: '@',
  content: '',
  ttl: 1,
  proxied: false,
  priority: 10,
};

export const EMPTY_WORKER_FORM = {
  name: '',
  script: 'export default {\n  async fetch(request, env, ctx) {\n    return new Response("Hello Cloudflare Workers");\n  },\n};',
};

export const EMPTY_TUNNEL_CONFIG = JSON.stringify(
  {
    ingress: [
      { hostname: 'app.example.com', service: 'http://localhost:8080' },
      { service: 'http_status:404' },
    ],
  },
  null,
  2
);
