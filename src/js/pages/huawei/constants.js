// 华为云各资源列表的列定义（AppTable 用），与账号表单初值。
export const INSTANCE_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', width: 200, minWidth: 160 },
  { id: 'status', role: 'status' },
  { id: 'publicIp', role: 'identifier', width: 180, minWidth: 150 },
  { id: 'flavorName', role: 'meta', width: 160 },
  { id: 'region', role: 'meta', grow: 1, minWidth: 140 },
  { id: 'createdAt', role: 'datetime' },
  { id: 'actions', role: 'actions-lg', width: 150 },
];

export const FLEXUS_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', width: 180, minWidth: 150 },
  { id: 'serverStatus', role: 'status' },
  { id: 'specDescription', role: 'meta', grow: 1, minWidth: 200 },
  { id: 'expireAt', role: 'datetime' },
  { id: 'traffic', role: 'meta', width: 130 },
  { id: 'actions', role: 'actions-lg', width: 150 },
];

export const DNS_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', width: 220, minWidth: 180 },
  { id: 'type', role: 'type' },
  { id: 'recordNum', role: 'count' },
  { id: 'status', role: 'status' },
  { id: 'createdAt', role: 'datetime', grow: 1 },
  { id: 'actions', role: 'actions-md', width: 96 },
];

export const EIP_TABLE_COLUMNS = [
  { id: 'publicIp', role: 'primary', width: 200, minWidth: 160 },
  { id: 'status', role: 'status' },
  { id: 'bandwidth', role: 'number' },
  { id: 'region', role: 'meta', grow: 1, minWidth: 140 },
];

export const ACCOUNT_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', width: 180, minWidth: 150 },
  { id: 'site', role: 'meta', width: 88 },
  { id: 'accessKeyId', role: 'identifier', width: 170, minWidth: 150 },
  { id: 'defaultRegion', role: 'meta', grow: 1, minWidth: 140 },
  { id: 'status', role: 'status' },
  { id: 'actions', role: 'actions-lg', width: 160, maxWidth: 200 },
];

export const BUCKET_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', width: 220, minWidth: 180 },
  { id: 'region', role: 'meta', width: 160 },
  { id: 'createdAt', role: 'datetime' },
  { id: 'actions', role: 'actions-md', width: 96 },
];

export const OBJECT_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', grow: 1, minWidth: 200 },
  { id: 'size', role: 'number' },
  { id: 'lastModified', role: 'datetime' },
  { id: 'actions', role: 'actions-md', width: 96 },
];

export const BILLING_SUM_COLUMNS = [
  { id: 'serviceTypeName', role: 'primary', grow: 1, minWidth: 160 },
  { id: 'resourceTypeName', role: 'meta', width: 160 },
  { id: 'consumeAmount', role: 'number' },
];

export const FREE_RESOURCE_COLUMNS = [
  { id: 'typeName', role: 'primary', grow: 1, minWidth: 160 },
  { id: 'traffic', role: 'meta', width: 140 },
  { id: 'endTime', role: 'datetime' },
];

export const emptyAccountForm = {
  name: '',
  site: 'cn',
  accessKeyId: '',
  secretAccessKey: '',
  defaultRegion: '',
  defaultProjectId: '',
  description: '',
  sshUser: '',
  sshPort: 22,
  sshPrivateKey: '',
  sshPassword: '',
};
