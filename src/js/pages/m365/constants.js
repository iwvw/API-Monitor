export const M365_REQUIRED_PERMISSIONS = [
  { name: 'User.Read.All', note: '读取用户列表与详情' },
  { name: 'User.ReadWrite.All', note: '创建、编辑、删除用户' },
  { name: 'Organization.Read.All', note: '读取租户订阅与许可证到期时间' },
  { name: 'LicenseAssignment.Read.All', note: '读取用户和组的许可证信息' },
  { name: 'LicenseAssignment.ReadWrite.All', note: '分配或回收许可证' },
  { name: 'Group.Create', note: '创建组' },
  { name: 'GroupMember.ReadWrite.All', note: '添加或移除组成员' },
  { name: 'Files.Read.All', note: '读取用户 OneDrive 容量（已用/总容量）' },
];

export const defaultAccountForm = {
  name: '',
  tenantId: '',
  clientId: '',
  clientSecret: '',
  description: '',
  enabled: true,
};

export const defaultUserForm = {
  displayName: '',
  mailNickname: '',
  userPrincipalName: '',
  emailDomain: '',
  password: '',
  department: '',
  jobTitle: '',
  officeLocation: '',
  usageLocation: '',
  accountEnabled: true,
  forceChangePasswordNextSignIn: true,
};

export const defaultGroupForm = {
  displayName: '',
  mailNickname: '',
  securityEnabled: true,
  mailEnabled: false,
};

export const defaultPublicPageForm = {
  id: null,
  name: '',
  accountIds: [],
  domains: [],
  usageLocation: '',
  skuIds: [],
  enabled: true,
  forceChangePasswordNextSignIn: true,
  expiresAt: '',
};

export const defaultInviteCodeGeneratorForm = {
  publicPageId: '',
  quantity: 1,
};

export const workspaceHeightClass = 'min-h-0 flex-1';
export const panelBodyClass = 'flex min-h-0 flex-1 flex-col';
export const scrollViewportClass = 'min-h-0 flex-1 overflow-auto scrollbar-thin';
export const tableFrameClass = 'flex h-0 min-h-0 flex-1 flex-col overflow-hidden';
export const DEFAULT_NEW_USER_PASSWORD = 'Mjj@1234';
// 语义列定义（替代旧 widths 数组）：状态居中，显示名为主列，
// 账号/邮箱/许可证/OneDrive 用量为内容列，操作为固定列。
export const USER_TABLE_COLUMNS = [
  { id: 'status', role: 'status' },
  { id: 'displayName', role: 'primary', minWidth: 160, maxWidth: 220, grow: 1 },
  { id: 'account', role: 'identifier', minWidth: 180, maxWidth: 240, grow: 1 },
  { id: 'email', role: 'content', minWidth: 180, verticalAlign: 'middle' },
  { id: 'license', role: 'content', minWidth: 200, verticalAlign: 'middle' },
  { id: 'oneDrive', role: 'content', minWidth: 180, verticalAlign: 'middle' },
  { id: 'actions', role: 'actions-lg' },
];
export const REGISTRATION_TABLE_COLUMNS = [
  { id: 'check', role: 'check' },
  { id: 'account', role: 'primary', minWidth: 176 },
  { id: 'status', role: 'status' },
  { id: 'source', role: 'identifier', minWidth: 176 },
  { id: 'tenant', role: 'meta', grow: 1, minWidth: 160 },
  { id: 'graphUserId', role: 'identifier', minWidth: 176 },
  { id: 'createdAt', role: 'datetime', width: 144 },
  { id: 'result', role: 'content', minWidth: 200, verticalAlign: 'middle' },
];
export const publicResourceCardClass =
  'flex h-full min-h-[15rem] flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-base';
export const publicResourceCardHeaderClass =
  'flex items-center justify-between gap-3 border-b border-kumo-line bg-kumo-recessed/10 px-3.5 py-3';
export const publicResourceCardGridClass = 'grid flex-1 grid-cols-2 gap-2 p-3';
export const publicResourceCardFieldClass = 'rounded-lg border border-kumo-line bg-kumo-recessed/15 p-2.5';
export const publicResourceCardActionBarClass =
  'flex items-center gap-2 border-t border-kumo-line px-3 py-3';
export const tenantGridClass = 'grid-cols-1 cq-sm:grid-cols-2 cq-xl:grid-cols-4';
export const tenantCardFrameClass = 'min-h-[11.75rem] rounded-xl px-4 py-3.5';
export const defaultAccountImportState = {
  text: '',
  overwrite: false,
  fileName: '',
};

export const SKU_DISPLAY_NAMES = {
  STANDARDWOFFPACK_STUDENT: 'A1 学生版',
  STANDARDWOFFPACK_FACULTY: 'A1 教师版',
  OFFICE_365_A1_PLUS_FOR_STUDENT: 'A1 Plus 学生版',
  OFFICE_365_A1_PLUS_FOR_FACULTY: 'A1 Plus 教师版',
  M365EDU_A3_STUUSEBNFT_RPA1: 'A3 无人值守版',
  Office_365_E3Y: 'E3Y',
  DEVELOPERPACK: 'E3 开发者订阅',
  DEVELOPERPACK_E5: 'E5 开发者订阅',
  FLOW_FREE: 'Power Automate 免费版',
  ENTERPRISEPACK: 'Office 365 E3',
};
export const SKU_ID_DISPLAY_NAMES = {
  '314c4481-f395-4525-be8b-2ec4bb1e9d91': 'A1 学生版',
  '94763226-9b3c-4e75-a931-5c89701abe66': 'A1 教师版',
  'e82ae690-a2d5-4d76-8d30-7c6e01e6022e': 'A1 Plus 学生版',
  '78e66a63-337a-4a9a-8959-41c6654dfb56': 'A1 Plus 教师版',
  '1aa94593-ca12-4254-a738-81a5972958e8': 'A3 无人值守版',
  '189a915c-fe4f-4ffa-bde4-85b9628d07a0': 'E3 开发者订阅',
  '6fd2c87f-b296-42f0-b197-1e91e994b900': 'E3Y',
  'c42b9cae-ea4f-4ab7-9717-81576235ccac': 'E5 开发者订阅',
  'f30db892-07e9-47e9-837c-80727f46fd3d': 'Power Automate 免费版',
};
