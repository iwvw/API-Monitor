import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Badge, Meter, Popover, Tabs, Toolbar } from '@cloudflare/kumo';
import { dialog } from '../modules/dialog.js';
import { toast } from '../modules/toast.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import {
  AppCard,
  AppTable,
  cx,
  DataTableFrame,
  EmptyState,
  PageStack,
  ResponsiveSearchInput,
  SectionCard,
  StatusBadge,
  TabBarOverflowActions,
  stickyTabsBaseClass,
} from '../components/ui/AppPrimitives.jsx';
import CodeEditor from '../components/ui/CodeEditor.jsx';
import {
  ChevronDown,
  Cloud,
  Copy,
  Database,
  Download,
  Folder,
  Globe,
  Key,
  Plus,
  RefreshCw,
  Search,
  Settings,
  Shield,
  Trash,
  Upload,
  User,
  Users,
} from '../components/Icons.jsx';
import { formatDateTime } from '../modules/utils.js';

const M365_REQUIRED_PERMISSIONS = [
  { name: 'User.Read.All', note: '读取用户列表与详情' },
  { name: 'User.ReadWrite.All', note: '创建、编辑、删除用户' },
  { name: 'Organization.Read.All', note: '读取租户订阅与许可证到期时间' },
  { name: 'LicenseAssignment.Read.All', note: '读取用户和组的许可证信息' },
  { name: 'LicenseAssignment.ReadWrite.All', note: '分配或回收许可证' },
  { name: 'Group.Create', note: '创建组' },
  { name: 'GroupMember.ReadWrite.All', note: '添加或移除组成员' },
];

const defaultAccountForm = {
  name: '',
  tenantId: '',
  clientId: '',
  clientSecret: '',
  description: '',
  enabled: true,
};

const defaultUserForm = {
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

const defaultGroupForm = {
  displayName: '',
  mailNickname: '',
  securityEnabled: true,
  mailEnabled: false,
};

const defaultPublicPageForm = {
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

const defaultInviteCodeGeneratorForm = {
  publicPageId: '',
  quantity: 1,
};

const workspaceHeightClass = 'min-h-0 flex-1';
const panelBodyClass = 'flex min-h-0 flex-1 flex-col';
const scrollViewportClass = 'min-h-0 flex-1 overflow-auto scrollbar-thin';
const tableFrameClass = 'flex h-0 min-h-0 flex-1 flex-col overflow-hidden';
const DEFAULT_NEW_USER_PASSWORD = 'Mjj@1234';
const USER_TABLE_COLUMN_WIDTHS = [96, 180, 220, 220, 260, 220];
const REGISTRATION_TABLE_COLUMNS = [
  { id: 'check', role: 'check' },
  { id: 'account', role: 'primary', minWidth: 176 },
  { id: 'status', role: 'status' },
  { id: 'source', role: 'identifier', minWidth: 176 },
  { id: 'tenant', role: 'meta', grow: 1, minWidth: 160 },
  { id: 'graphUserId', role: 'identifier', minWidth: 176 },
  { id: 'createdAt', role: 'datetime', width: 144 },
  { id: 'result', role: 'content', minWidth: 200, verticalAlign: 'middle' },
];
const publicResourceCardClass =
  'flex h-full min-h-[15rem] flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-base';
const publicResourceCardHeaderClass =
  'flex items-center justify-between gap-3 border-b border-kumo-line bg-kumo-recessed/10 px-3.5 py-3';
const publicResourceCardGridClass = 'grid flex-1 grid-cols-2 gap-2 p-3';
const publicResourceCardFieldClass = 'rounded-lg border border-kumo-line bg-kumo-recessed/15 p-2.5';
const publicResourceCardActionBarClass =
  'flex items-center gap-2 border-t border-kumo-line px-3 py-3';
const tenantGridClass = 'grid-cols-1 cq-sm:grid-cols-2 cq-xl:grid-cols-4';
const tenantCardFrameClass = 'min-h-[11.75rem] rounded-xl px-4 py-3.5';
const defaultAccountImportState = {
  text: '',
  overwrite: false,
  fileName: '',
};

function SkuGridSkeleton() {
  return (
    <div
      className={cx(scrollViewportClass, 'grid auto-rows-max content-start gap-2.5 pr-1')}
      style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))' }}
    >
      {Array.from({ length: 6 }).map((_, index) => (
        <div
          key={index}
          className="rounded-lg border border-kumo-line/70 bg-kumo-base/95 px-3 py-2.5"
        >
          <SkeletonLine className="h-5 w-3/5" />
          <div className="mt-2 flex flex-wrap gap-2">
            <SkeletonLine className="h-3 w-20" />
            <SkeletonLine className="h-3 w-16" />
            <SkeletonLine className="h-3 w-14" />
          </div>
          <SkeletonLine className="mt-3 h-1.5 w-full rounded-full" />
        </div>
      ))}
    </div>
  );
}

function TenantGridSkeleton() {
  return (
    <div
      className={cx(
        scrollViewportClass,
        'grid auto-rows-max content-start items-start gap-3 p-1',
        tenantGridClass
      )}
    >
      {Array.from({ length: 4 }).map((_, index) => (
        <div key={index} className="rounded-xl border border-kumo-line/80 bg-kumo-base/95 p-3">
          <div className="flex items-start justify-between gap-3">
            <div className="flex min-w-0 items-center gap-2">
              <SkeletonLine className="h-9 w-9 rounded-lg" />
              <div className="min-w-0 space-y-2">
                <SkeletonLine className="h-4 w-32" />
                <SkeletonLine className="h-3 w-24" />
              </div>
            </div>
            <SkeletonLine className="h-6 w-14 rounded-full" />
          </div>
          <div className="mt-3 space-y-2 rounded-lg border border-kumo-line/60 bg-kumo-recessed/20 p-2">
            <SkeletonLine className="h-3 w-full" />
            <SkeletonLine className="h-3 w-5/6" />
            <SkeletonLine className="h-3 w-4/5" />
          </div>
          <div className="mt-3 flex items-center justify-between gap-2">
            <SkeletonLine className="h-3 w-16" />
            <div className="flex gap-2">
              <SkeletonLine className="h-8 w-8 rounded-md" />
              <SkeletonLine className="h-8 w-12 rounded-md" />
              <SkeletonLine className="h-8 w-8 rounded-md" />
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

function CardTableSkeleton({ rows = 6, showToolbar = false }) {
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4">
      {showToolbar ? (
        <div className="flex items-center gap-2">
          <SkeletonLine className="h-8 w-44" />
          <SkeletonLine className="h-8 w-24" />
        </div>
      ) : null}
      <div className={cx(tableFrameClass, 'rounded-lg border border-kumo-line/80 bg-kumo-base')}>
        <div className="space-y-3 p-3">
          <SkeletonLine className="h-9 w-full" />
          {Array.from({ length: rows }).map((_, index) => (
            <SkeletonLine key={index} className="h-10 w-full" />
          ))}
        </div>
      </div>
    </div>
  );
}

function GroupsTabSkeleton() {
  return (
    <div className="grid min-h-0 flex-1 gap-4 cq-lg:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
      <AppCard padding="none" className="flex min-h-0 flex-col">
        <div className="space-y-3 p-3">
          <SkeletonLine className="h-9 w-full" />
          {Array.from({ length: 6 }).map((_, index) => (
            <SkeletonLine key={index} className="h-12 w-full" />
          ))}
        </div>
      </AppCard>

      <SectionCard
        className="flex min-h-0 flex-col"
        bodyClassName={panelBodyClass}
        title="组成员"
        description="输入成员对象 ID"
        icon={<Users className="h-4 w-4" />}
        bodyPadding="sm"
        action={
          <div className="flex items-center gap-2">
            <SkeletonLine className="h-8 w-44" />
            <SkeletonLine className="h-8 w-24" />
          </div>
        }
      >
        <CardTableSkeleton rows={5} showToolbar />
      </SectionCard>
    </div>
  );
}

const SKU_DISPLAY_NAMES = {
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
const SKU_ID_DISPLAY_NAMES = {
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

function getDisplayText(value) {
  if (value === null || value === undefined || value === '') return '-';
  return String(value);
}

function getSkuDisplayName(skuPartNumber, skuId) {
  const skuName = skuPartNumber || skuId || '';
  if (!skuName) return '-';
  return SKU_DISPLAY_NAMES[skuName] || SKU_ID_DISPLAY_NAMES[skuId] || skuName;
}

function getSkuDisplayLabel(skuPartNumber, skuId) {
  return getSkuDisplayName(skuPartNumber, skuId);
}

function getAssignedSkuLabels(assignedLicenses, skuLabelLookup = new Map()) {
  if (!Array.isArray(assignedLicenses) || assignedLicenses.length === 0) return [];
  const labels = [];
  const seen = new Set();
  assignedLicenses.forEach(item => {
    const skuId = String(item?.skuId || '').trim();
    const label = skuLabelLookup.get(skuId) || getSkuDisplayName(item?.skuPartNumber, skuId);
    const normalized = String(label || '').trim();
    if (!normalized || seen.has(normalized)) return;
    seen.add(normalized);
    labels.push(normalized);
  });
  return labels;
}

function clampPercent(value) {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(100, value));
}

function formatMetricNumber(value) {
  const numericValue = Number(value);
  if (!Number.isFinite(numericValue)) return '0';
  return numericValue.toLocaleString('en-US', { useGrouping: false });
}

function formatDateOnly(value) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  });
}

function getSkuLifecycleText(sku) {
  const nextLifecycleDateTime = sku?.nextLifecycleDateTime;
  if (!nextLifecycleDateTime) return '';
  const status = String(sku?.subscriptionStatus || '').toLowerCase();
  const label = ['enabled', 'warning'].includes(status) ? '到期' : '生命周期';
  const dateText = formatDateOnly(nextLifecycleDateTime);
  return dateText ? `${label} ${dateText}` : '';
}

function getDomainFromPrincipalName(value) {
  const text = String(value || '').trim();
  const atIndex = text.indexOf('@');
  if (atIndex < 0 || atIndex === text.length - 1) return '';
  return text.slice(atIndex + 1).trim();
}

function normalizeDomainValue(value) {
  return String(value || '')
    .trim()
    .toLowerCase();
}

function getAccountDomainList(account) {
  const domains = new Set();
  if (Array.isArray(account?.verifiedDomains)) {
    account.verifiedDomains.forEach(domain => {
      const normalized = normalizeDomainValue(domain);
      if (normalized) domains.add(normalized);
    });
  }
  const defaultDomain = normalizeDomainValue(account?.defaultDomain);
  if (defaultDomain) {
    domains.add(defaultDomain);
  }
  return Array.from(domains).sort((a, b) => a.localeCompare(b));
}

function extractOrganizationDomains(organization) {
  if (!organization || !Array.isArray(organization.verifiedDomains)) return [];
  const defaults = [];
  const others = [];
  organization.verifiedDomains.forEach(item => {
    const normalized = normalizeDomainValue(item?.name);
    if (!normalized) return;
    if (item?.isDefault) {
      defaults.push(normalized);
    } else {
      others.push(normalized);
    }
  });
  return Array.from(new Set([...defaults, ...others]));
}

function getPrincipalLocalPart(value) {
  const text = String(value || '').trim();
  const atIndex = text.indexOf('@');
  if (atIndex <= 0) return text;
  return text.slice(0, atIndex).trim();
}

function getFriendlyErrorMessage(message, fallback) {
  if (!message) return fallback;
  if (message.includes('Authorization_RequestDenied')) {
    return `${fallback}：当前租户权限不足`;
  }
  if (message.includes('ResourceNotFound')) {
    return `${fallback}：目标资源不存在`;
  }
  return message;
}

function ItemListPopover({
  items,
  copyText,
  label = '列表',
  emptyText = '暂无内容',
  unitLabel = '个',
  codeStyle = false,
}) {
  return (
    <Popover>
      <Popover.Trigger
        render={
          <Button
            size="sm"
            variant="secondary"
            className="w-full justify-between"
            disabled={items.length === 0}
          >
            <span>{items.length > 0 ? `共 ${items.length} ${unitLabel}` : '暂无'}</span>
            <span className="text-[10px] text-kumo-subtle">查看</span>
          </Button>
        }
      />
      <Popover.Content side="bottom" align="start" className="w-[min(24rem,calc(100vw-2rem))] p-3">
        <div className="mb-2 flex items-center justify-between gap-2">
          <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
            {label}
          </Popover.Title>
          <Button
            size="sm"
            variant="secondary"
            disabled={items.length === 0}
            icon={<Copy className="h-3.5 w-3.5" />}
            onClick={() => copyText(items.join('\n'), `${label}已复制`)}
          >
            全部
          </Button>
        </div>
        <div className="grid gap-2">
          {items.length > 0 ? (
            items.map((item, index) => (
              <div
                key={`${item}-${index}`}
                className="flex min-w-0 items-center gap-2 rounded-md border border-kumo-line bg-kumo-recessed/25 px-2.5 py-2"
              >
                {codeStyle ? (
                  <code
                    className="min-w-0 flex-1 truncate text-xs font-medium text-kumo-strong"
                    title={item}
                  >
                    {item}
                  </code>
                ) : (
                  <div
                    className="min-w-0 flex-1 truncate text-xs font-medium text-kumo-strong"
                    title={item}
                  >
                    {item}
                  </div>
                )}
                <Button
                  size="sm"
                  shape="square"
                  variant="secondary"
                  aria-label={`复制 ${item}`}
                  title="复制"
                  icon={<Copy className="h-3.5 w-3.5" />}
                  onClick={() => copyText(item, `${item} 已复制`)}
                />
              </div>
            ))
          ) : (
            <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-3 text-center text-xs text-kumo-subtle">
              {emptyText}
            </div>
          )}
        </div>
      </Popover.Content>
    </Popover>
  );
}

function DomainListPopover({ domains, copyText, label = '域名列表' }) {
  const items = Array.isArray(domains)
    ? Array.from(new Set(domains.map(item => normalizeDomainValue(item)).filter(Boolean)))
    : [];

  return (
    <ItemListPopover
      items={items}
      copyText={copyText}
      label={label}
      emptyText="暂无域名"
      unitLabel="个"
      codeStyle
    />
  );
}

function LicenseListPopover({ licenses, copyText, label = '许可证列表' }) {
  const items = Array.isArray(licenses)
    ? Array.from(new Set(licenses.map(item => String(item || '').trim()).filter(Boolean)))
    : [];

  return (
    <ItemListPopover
      items={items}
      copyText={copyText}
      label={label}
      emptyText="暂无许可证"
      unitLabel="项"
    />
  );
}

const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

async function parseResponse(response) {
  const payload = await response.json().catch(() => ({}));
  if (!response.ok || payload.success === false) {
    throw new Error(payload.error || '请求失败');
  }
  return payload.data ?? payload;
}

function downloadJson(filename, payload) {
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

function parseJsonInput(input, fallbackKey) {
  const parsed = JSON.parse(input);
  if (Array.isArray(parsed)) return parsed;
  if (fallbackKey && Array.isArray(parsed[fallbackKey])) return parsed[fallbackKey];
  return parsed;
}

function getRegistrationTone(status) {
  if (status === 'success') return 'success';
  if (status === 'partial') return 'warning';
  return 'danger';
}

function getRegistrationStatusLabel(status) {
  if (status === 'success') return '成功';
  if (status === 'partial') return '部分成功';
  return '失败';
}

function getRegistrationResultText(record) {
  if (record?.errorMessage) return record.errorMessage;
  if (record?.status === 'success') return '已完成创建与分配';
  if (record?.status === 'partial') return '账号已创建，后续步骤部分失败';
  return '创建流程失败';
}

function M365Page() {
  const { isArmed, confirmPress } = useConfirmPress();
  const accountImportInputRef = useRef(null);
  const [activeTab, setActiveTab] = useState('tenants');
  const [showPermissionDialog, setShowPermissionDialog] = useState(false);
  const [permissionCheckLoading, setPermissionCheckLoading] = useState(false);
  const [permissionCheckError, setPermissionCheckError] = useState('');
  const [accounts, setAccounts] = useState([]);
  const [selectedAccountId, setSelectedAccountId] = useState('');
  const [loadingAccounts, setLoadingAccounts] = useState(false);
  const [verifyingAccountId, setVerifyingAccountId] = useState('');
  const [showAccountDialog, setShowAccountDialog] = useState(false);
  const [editingAccount, setEditingAccount] = useState(null);
  const [accountForm, setAccountForm] = useState(defaultAccountForm);
  const [submittingAccount, setSubmittingAccount] = useState(false);
  const [showAccountImportDialog, setShowAccountImportDialog] = useState(false);
  const [accountImportState, setAccountImportState] = useState(defaultAccountImportState);
  const [importingAccounts, setImportingAccounts] = useState(false);

  const [userSearch, setUserSearch] = useState('');
  const [usersLoading, setUsersLoading] = useState(false);
  const [users, setUsers] = useState([]);
  const [showUserDialog, setShowUserDialog] = useState(false);
  const [editingUser, setEditingUser] = useState(null);
  const [userForm, setUserForm] = useState(defaultUserForm);
  const [loadingUserDialog, setLoadingUserDialog] = useState(false);
  const [submittingUser, setSubmittingUser] = useState(false);
  const [selectedUserId, setSelectedUserId] = useState('');
  const [togglingUserId, setTogglingUserId] = useState('');
  const [pendingUserDeleteId, setPendingUserDeleteId] = useState('');

  const [skuLoading, setSkuLoading] = useState(false);
  const [skus, setSkus] = useState([]);
  const [userDialogSkuIds, setUserDialogSkuIds] = useState([]);
  const [initialUserSkuIds, setInitialUserSkuIds] = useState([]);
  const [assigningLicense, setAssigningLicense] = useState(false);

  const [groupsLoading, setGroupsLoading] = useState(false);
  const [groups, setGroups] = useState([]);
  const [selectedGroupId, setSelectedGroupId] = useState('');
  const [groupMembers, setGroupMembers] = useState([]);
  const [groupMembersLoading, setGroupMembersLoading] = useState(false);
  const [showGroupDialog, setShowGroupDialog] = useState(false);
  const [groupForm, setGroupForm] = useState(defaultGroupForm);
  const [submittingGroup, setSubmittingGroup] = useState(false);
  const [memberInput, setMemberInput] = useState('');
  const [groupLicenseSkuId, setGroupLicenseSkuId] = useState('');
  const [assigningGroupLicense, setAssigningGroupLicense] = useState(false);
  const [publicTab, setPublicTab] = useState('pages');
  const [publicPagesLoading, setPublicPagesLoading] = useState(false);
  const [publicPages, setPublicPages] = useState([]);
  const [publicPageSkuCatalog, setPublicPageSkuCatalog] = useState({});
  const [inviteCodesLoading, setInviteCodesLoading] = useState(false);
  const [inviteCodes, setInviteCodes] = useState([]);
  const [registrationsLoading, setRegistrationsLoading] = useState(false);
  const [registrations, setRegistrations] = useState([]);
  const [selectedRegistrationIds, setSelectedRegistrationIds] = useState([]);
  const [deletingRegistrations, setDeletingRegistrations] = useState(false);
  const [registrationDetail, setRegistrationDetail] = useState(null);
  const [showPublicPageDialog, setShowPublicPageDialog] = useState(false);
  const [publicPageForm, setPublicPageForm] = useState(defaultPublicPageForm);
  const [publicAccountDomains, setPublicAccountDomains] = useState({});
  const [submittingPublicPage, setSubmittingPublicPage] = useState(false);
  const [togglingPublicPageId, setTogglingPublicPageId] = useState('');
  const [showInviteCodeDialog, setShowInviteCodeDialog] = useState(false);
  const [inviteCodeGeneratorForm, setInviteCodeGeneratorForm] = useState(
    defaultInviteCodeGeneratorForm
  );
  const [generatingInviteCodes, setGeneratingInviteCodes] = useState(false);
  const [pendingInviteBatchDeleteKey, setPendingInviteBatchDeleteKey] = useState('');
  const [permissionItems, setPermissionItems] = useState(
    M365_REQUIRED_PERMISSIONS.map(permission => ({
      ...permission,
      granted: null,
    }))
  );

  const selectedAccount = useMemo(
    () => accounts.find(account => String(account.id) === String(selectedAccountId)) || null,
    [accounts, selectedAccountId]
  );

  const accountLookup = useMemo(
    () => new Map(accounts.map(account => [String(account.id), account])),
    [accounts]
  );

  const getPublicAccountDomainList = useCallback(
    account => {
      const liveDomains = publicAccountDomains[String(account?.id || '')];
      const fallbackDomains = getAccountDomainList(account);
      const selectedAccountUserDomains =
        String(account?.id || '') === String(selectedAccountId)
          ? users
              .map(user =>
                normalizeDomainValue(
                  getDomainFromPrincipalName(user.userPrincipalName || user.mail)
                )
              )
              .filter(Boolean)
          : [];
      return Array.from(
        new Set([...(liveDomains || []), ...fallbackDomains, ...selectedAccountUserDomains])
      ).sort((a, b) => a.localeCompare(b));
    },
    [publicAccountDomains, selectedAccountId, users]
  );

  const accountSelectItems = useMemo(
    () => accounts.map(account => ({ value: String(account.id), label: account.name })),
    [accounts]
  );

  const skuItems = useMemo(
    () =>
      skus.map(sku => ({
        value: sku.skuId,
        label: getSkuDisplayLabel(sku.skuPartNumber, sku.skuId),
      })),
    [skus]
  );

  const skuLabelLookup = useMemo(
    () =>
      new Map(
        skus.map(sku => [String(sku.skuId), getSkuDisplayLabel(sku.skuPartNumber, sku.skuId)])
      ),
    [skus]
  );

  const publicPageSkuLabelLookup = useMemo(() => {
    const lookup = new Map();
    Object.values(publicPageSkuCatalog).forEach(labels => {
      Object.entries(labels || {}).forEach(([skuId, label]) => {
        const normalizedSkuId = String(skuId || '').trim();
        const normalizedLabel = String(label || '').trim();
        if (!normalizedSkuId || !normalizedLabel) return;
        lookup.set(normalizedSkuId, normalizedLabel);
      });
    });
    return lookup;
  }, [publicPageSkuCatalog]);

  const userEmailDomainItems = useMemo(() => {
    const domains = new Set();
    getAccountDomainList(selectedAccount).forEach(domain => domains.add(domain));
    users.forEach(user => {
      const domain = getDomainFromPrincipalName(user.userPrincipalName || user.mail);
      if (domain) domains.add(domain);
    });
    return Array.from(domains)
      .filter(Boolean)
      .sort((a, b) => a.localeCompare(b))
      .map(domain => ({ value: domain, label: `@${domain}` }));
  }, [selectedAccount, users]);

  const selectedGroup = useMemo(
    () => groups.find(group => String(group.id) === String(selectedGroupId)) || null,
    [groups, selectedGroupId]
  );

  const filteredPublicPages = useMemo(() => {
    return publicPages;
  }, [publicPages]);

  const filteredInviteCodes = useMemo(() => {
    return inviteCodes;
  }, [inviteCodes]);

  const filteredRegistrations = useMemo(() => {
    return registrations;
  }, [registrations]);

  const selectedRegistrationRecords = useMemo(
    () => filteredRegistrations.filter(record => selectedRegistrationIds.includes(record.id)),
    [filteredRegistrations, selectedRegistrationIds]
  );

  const groupedInviteCodeBatches = useMemo(() => {
    const grouped = new Map();
    filteredInviteCodes.forEach(codeItem => {
      const batchLabel = String(codeItem.batchId || `single-${codeItem.id}`);
      const key = `${codeItem.publicPageId || 'none'}::${batchLabel}`;
      const createdAtValue = codeItem.createdAt ? new Date(codeItem.createdAt).getTime() : 0;
      const lastUsedAtValue = codeItem.lastUsedAt ? new Date(codeItem.lastUsedAt).getTime() : 0;
      if (!grouped.has(key)) {
        grouped.set(key, {
          key,
          publicPageId: codeItem.publicPageId,
          publicPageName: codeItem.publicPageName || '未命名公开页',
          batchId: codeItem.batchId || '',
          codes: [],
          createdAt: codeItem.createdAt || '',
          createdAtValue,
          lastUsedAt: codeItem.lastUsedAt || '',
          lastUsedAtValue,
          usedCount: 0,
          availableCount: 0,
          domains: new Set(),
        });
      }
      const group = grouped.get(key);
      group.codes.push(codeItem);
      if (createdAtValue > group.createdAtValue) {
        group.createdAt = codeItem.createdAt || group.createdAt;
        group.createdAtValue = createdAtValue;
      }
      if (lastUsedAtValue > group.lastUsedAtValue) {
        group.lastUsedAt = codeItem.lastUsedAt || group.lastUsedAt;
        group.lastUsedAtValue = lastUsedAtValue;
      }
      if (codeItem.used) group.usedCount += 1;
      if (codeItem.available) group.availableCount += 1;
      (codeItem.domains || []).forEach(domain => {
        const normalized = normalizeDomainValue(domain);
        if (normalized) group.domains.add(normalized);
      });
    });
    return Array.from(grouped.values())
      .map(group => ({
        ...group,
        domains: Array.from(group.domains).sort((a, b) => a.localeCompare(b)),
        codes: [...group.codes].sort((a, b) => Number(b.id || 0) - Number(a.id || 0)),
      }))
      .sort(
        (a, b) =>
          b.createdAtValue - a.createdAtValue ||
          Number(b.publicPageId || 0) - Number(a.publicPageId || 0)
      );
  }, [filteredInviteCodes]);

  const requestJSON = useCallback(async (path, options = {}) => {
    const response = await fetch(path, {
      ...options,
      headers: {
        ...getAuthHeaders(),
        ...(options.headers || {}),
      },
    });
    return parseResponse(response);
  }, []);

  const loadAccounts = useCallback(async () => {
    setLoadingAccounts(true);
    try {
      const data = await requestJSON('/api/m365/accounts');
      const items = Array.isArray(data.items) ? data.items : [];
      setAccounts(items);
      setSelectedAccountId(current => {
        if (current && items.some(item => String(item.id) === String(current))) {
          return current;
        }
        return items[0] ? String(items[0].id) : '';
      });
    } catch (error) {
      toast.error(error.message || '加载租户失败');
    } finally {
      setLoadingAccounts(false);
    }
  }, [requestJSON]);

  const loadUsers = useCallback(async () => {
    if (!selectedAccountId) {
      setUsers([]);
      return;
    }
    setUsersLoading(true);
    try {
      const query = new URLSearchParams({ top: '200' });
      if (userSearch.trim()) query.set('search', userSearch.trim());
      const data = await requestJSON(
        `/api/m365/accounts/${selectedAccountId}/users?${query.toString()}`
      );
      const items = Array.isArray(data.items) ? data.items : [];
      setUsers(items);
      setSelectedUserId(current => {
        if (current && items.some(item => String(item.id) === String(current))) {
          return current;
        }
        return '';
      });
    } catch (error) {
      toast.error(error.message || '加载用户失败');
    } finally {
      setUsersLoading(false);
    }
  }, [activeTab, requestJSON, selectedAccountId, userSearch]);

  const loadSkusForAccount = useCallback(
    async accountId => {
      if (!accountId) {
        setSkus([]);
        return;
      }
      setSkuLoading(true);
      try {
        const data = await requestJSON(`/api/m365/accounts/${accountId}/licenses/skus`);
        const items = Array.isArray(data.items) ? data.items : [];
        setSkus(items);
        setGroupLicenseSkuId(current => current || items[0]?.skuId || '');
      } catch (error) {
        toast.error(error.message || '加载许可证失败');
      } finally {
        setSkuLoading(false);
      }
    },
    [requestJSON]
  );

  const loadSkus = useCallback(async () => {
    await loadSkusForAccount(selectedAccountId);
  }, [loadSkusForAccount, selectedAccountId]);

  const refreshUsersAndSkus = useCallback(async () => {
    await Promise.all([loadUsers(), loadSkus()]);
  }, [loadSkus, loadUsers]);

  const loadGroups = useCallback(async () => {
    if (!selectedAccountId) {
      setGroups([]);
      return;
    }
    setGroupsLoading(true);
    try {
      const data = await requestJSON(`/api/m365/accounts/${selectedAccountId}/groups?top=100`);
      const items = Array.isArray(data.items) ? data.items : [];
      setGroups(items);
      setSelectedGroupId(current => {
        if (current && items.some(item => String(item.id) === String(current))) {
          return current;
        }
        return items[0] ? String(items[0].id) : '';
      });
    } catch (error) {
      toast.error(error.message || '加载组失败');
    } finally {
      setGroupsLoading(false);
    }
  }, [requestJSON, selectedAccountId]);

  const loadPublicPages = useCallback(
    async (options = {}) => {
      const { silent = false } = options;
      if (!silent) {
        setPublicPagesLoading(true);
      }
      try {
        const data = await requestJSON('/api/m365/public-pages');
        const items = Array.isArray(data.items) ? data.items : [];
        setPublicPages(items);
        setInviteCodeGeneratorForm(current => {
          if (
            current.publicPageId &&
            items.some(item => String(item.id) === String(current.publicPageId))
          ) {
            return current;
          }
          return {
            ...current,
            publicPageId: items[0] ? String(items[0].id) : '',
          };
        });
      } catch (error) {
        toast.error(error.message || '加载公开页配置失败');
      } finally {
        if (!silent) {
          setPublicPagesLoading(false);
        }
      }
    },
    [requestJSON]
  );

  const loadInviteCodes = useCallback(
    async (options = {}) => {
      const { silent = false } = options;
      if (!silent) {
        setInviteCodesLoading(true);
      }
      try {
        const data = await requestJSON('/api/m365/invite-codes');
        setInviteCodes(Array.isArray(data.items) ? data.items : []);
      } catch (error) {
        toast.error(error.message || '加载邀请码失败');
      } finally {
        if (!silent) {
          setInviteCodesLoading(false);
        }
      }
    },
    [requestJSON]
  );

  const loadRegistrations = useCallback(async () => {
    setRegistrationsLoading(true);
    try {
      const data = await requestJSON('/api/m365/registrations');
      const items = Array.isArray(data.items) ? data.items : [];
      setRegistrations(items);
      setSelectedRegistrationIds(current =>
        current.filter(id => items.some(item => Number(item.id) === Number(id)))
      );
      setRegistrationDetail(current => {
        if (!current) return null;
        return items.find(item => Number(item.id) === Number(current.id)) || null;
      });
    } catch (error) {
      toast.error(error.message || '加载注册记录失败');
    } finally {
      setRegistrationsLoading(false);
    }
  }, [requestJSON]);

  const loadGroupMembers = useCallback(async () => {
    if (!selectedAccountId || !selectedGroupId) {
      setGroupMembers([]);
      return;
    }
    setGroupMembersLoading(true);
    try {
      const data = await requestJSON(
        `/api/m365/accounts/${selectedAccountId}/groups/${selectedGroupId}/members`
      );
      setGroupMembers(Array.isArray(data.items) ? data.items : []);
    } catch (error) {
      toast.error(error.message || '加载组成员失败');
    } finally {
      setGroupMembersLoading(false);
    }
  }, [requestJSON, selectedAccountId, selectedGroupId]);

  const loadUserLicenseDetails = useCallback(
    async userId => {
      if (!selectedAccountId || !userId) {
        return [];
      }
      try {
        const data = await requestJSON(
          `/api/m365/accounts/${selectedAccountId}/users/${userId}/license-details`
        );
        return Array.isArray(data.items) ? data.items : [];
      } catch (error) {
        toast.error(error.message || '加载用户许可证失败');
        return [];
      }
    },
    [requestJSON, selectedAccountId]
  );

  useEffect(() => {
    loadAccounts();
  }, [loadAccounts]);

  useEffect(() => {
    if (activeTab === 'users') {
      loadUsers();
      loadSkus();
    }
    if (activeTab === 'groups') {
      loadGroups();
      loadSkus();
    }
    if (activeTab === 'public') {
      loadSkus();
      loadPublicPages();
      loadInviteCodes();
      loadRegistrations();
    }
  }, [
    activeTab,
    loadGroups,
    loadInviteCodes,
    loadPublicPages,
    loadRegistrations,
    loadSkus,
    loadUsers,
  ]);

  useEffect(() => {
    if (activeTab === 'groups') {
      loadGroupMembers();
    }
  }, [activeTab, loadGroupMembers, selectedGroupId]);

  useEffect(() => {
    if (activeTab !== 'public' || publicPages.length === 0) return;
    const accountIds = Array.from(
      new Set(
        publicPages
          .flatMap(page =>
            Array.isArray(page.accountIds) && page.accountIds.length > 0
              ? page.accountIds
              : page.accountId
                ? [page.accountId]
                : []
          )
          .map(accountId => String(accountId || '').trim())
          .filter(Boolean)
      )
    );
    const missingAccountIds = accountIds.filter(accountId => !publicPageSkuCatalog[accountId]);
    if (missingAccountIds.length === 0) return;

    let cancelled = false;
    void Promise.all(
      missingAccountIds.map(async accountId => {
        try {
          const data = await requestJSON(`/api/m365/accounts/${accountId}/licenses/skus`);
          const items = Array.isArray(data.items) ? data.items : [];
          return [
            accountId,
            Object.fromEntries(
              items.map(sku => [
                String(sku?.skuId || '').trim(),
                getSkuDisplayLabel(sku?.skuPartNumber, sku?.skuId),
              ])
            ),
          ];
        } catch (error) {
          console.error('load public page sku labels failed:', accountId, error);
          return [accountId, {}];
        }
      })
    ).then(results => {
      if (cancelled) return;
      setPublicPageSkuCatalog(current => {
        const next = { ...current };
        results.forEach(([accountId, labels]) => {
          next[String(accountId)] = labels;
        });
        return next;
      });
    });

    return () => {
      cancelled = true;
    };
  }, [activeTab, publicPageSkuCatalog, publicPages, requestJSON]);

  useEffect(() => {
    if (!showPublicPageDialog) return;
    const primaryAccountId = String(publicPageForm.accountIds[0] || '');
    if (!primaryAccountId) {
      setSkus([]);
      return;
    }
    loadSkusForAccount(primaryAccountId);
  }, [loadSkusForAccount, publicPageForm.accountIds, showPublicPageDialog]);

  useEffect(() => {
    if (!showPublicPageDialog || accounts.length === 0) return;
    let cancelled = false;
    const loadOrganizationDomains = async () => {
      const nextMap = {};
      await Promise.all(
        accounts.map(async account => {
          try {
            const organization = await requestJSON(`/api/m365/accounts/${account.id}/organization`);
            const liveDomains = extractOrganizationDomains(organization);
            nextMap[String(account.id)] =
              liveDomains.length > 0 ? liveDomains : getAccountDomainList(account);
          } catch {
            nextMap[String(account.id)] = getAccountDomainList(account);
          }
        })
      );
      if (!cancelled) {
        setPublicAccountDomains(nextMap);
        setPublicPageForm(current => {
          const domainSet = new Set(current.domains);
          accounts.forEach(account => {
            const accountId = String(account.id);
            if (!current.accountIds.includes(accountId)) return;
            const previousDomains = getAccountDomainList(account);
            const alreadyContainedAllPrevious = previousDomains.every(domain =>
              domainSet.has(domain)
            );
            if (!alreadyContainedAllPrevious) return;
            (nextMap[accountId] || []).forEach(domain => domainSet.add(domain));
          });
          return {
            ...current,
            domains: Array.from(domainSet).sort((a, b) => a.localeCompare(b)),
          };
        });
      }
    };
    loadOrganizationDomains();
    return () => {
      cancelled = true;
    };
  }, [accounts, requestJSON, showPublicPageDialog]);

  useEffect(() => {
    setSelectedUserId('');
    setUserDialogSkuIds([]);
    setInitialUserSkuIds([]);
    setSelectedGroupId('');
    setGroupMembers([]);
    setPermissionCheckError('');
    setPermissionItems(
      M365_REQUIRED_PERMISSIONS.map(permission => ({
        ...permission,
        granted: null,
      }))
    );
  }, [selectedAccountId]);

  useEffect(() => {
    if (!pendingInviteBatchDeleteKey) return undefined;
    const timer = window.setTimeout(() => {
      setPendingInviteBatchDeleteKey(current =>
        current === pendingInviteBatchDeleteKey ? '' : current
      );
    }, 2200);
    return () => window.clearTimeout(timer);
  }, [pendingInviteBatchDeleteKey]);

  useEffect(() => {
    if (!pendingUserDeleteId) return undefined;
    const timer = window.setTimeout(() => {
      setPendingUserDeleteId(current => (current === pendingUserDeleteId ? '' : current));
    }, 2200);
    return () => window.clearTimeout(timer);
  }, [pendingUserDeleteId]);

  const openCreateAccount = () => {
    setEditingAccount(null);
    setAccountForm(defaultAccountForm);
    setShowAccountDialog(true);
  };

  const openImportAccounts = () => {
    setAccountImportState(defaultAccountImportState);
    if (accountImportInputRef.current) {
      accountImportInputRef.current.value = '';
    }
    setShowAccountImportDialog(true);
  };

  const openEditAccount = account => {
    setEditingAccount(account);
    setAccountForm({
      name: account.name || '',
      tenantId: account.tenantId || '',
      clientId: account.clientId || '',
      clientSecret: '',
      description: account.description || '',
      enabled: account.enabled !== false,
    });
    setShowAccountDialog(true);
  };

  const submitAccount = async () => {
    if (
      !accountForm.name ||
      !accountForm.tenantId ||
      !accountForm.clientId ||
      (!editingAccount && !accountForm.clientSecret)
    ) {
      toast.warning('请填写完整租户凭据');
      return;
    }
    setSubmittingAccount(true);
    try {
      const target = editingAccount
        ? `/api/m365/accounts/${editingAccount.id}`
        : '/api/m365/accounts';
      const method = editingAccount ? 'PUT' : 'POST';
      await requestJSON(target, {
        method,
        body: JSON.stringify(accountForm),
      });
      toast.success(editingAccount ? '租户已更新' : '租户已创建');
      setShowAccountDialog(false);
      await loadAccounts();
    } catch (error) {
      toast.error(error.message || '保存租户失败');
    } finally {
      setSubmittingAccount(false);
    }
  };

  const deleteAccount = async account => {
    if (!confirmPress(`m365-account-delete:${account.id}`, `删除租户「${account.name}」`)) return;
    try {
      await requestJSON(`/api/m365/accounts/${account.id}`, { method: 'DELETE' });
      toast.success('租户已删除');
      await loadAccounts();
    } catch (error) {
      toast.error(error.message || '删除租户失败');
    }
  };

  const verifyAccount = async account => {
    setVerifyingAccountId(String(account.id));
    try {
      const data = await requestJSON(`/api/m365/accounts/${account.id}/verify`, { method: 'POST' });
      toast.success(`已连接 ${data.organization?.displayName || account.name}`);
      await loadAccounts();
    } catch (error) {
      toast.error(error.message || '校验租户失败');
    } finally {
      setVerifyingAccountId('');
    }
  };

  const exportAccounts = async () => {
    try {
      const data = await requestJSON('/api/m365/export/accounts');
      downloadJson(
        `m365-tenants-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`,
        {
          version: '1.0',
          exportTime: new Date().toISOString(),
          accounts: Array.isArray(data) ? data : data.accounts || [],
        }
      );
      toast.success('租户已导出');
    } catch (error) {
      toast.error(error.message || '导出租户失败');
    }
  };

  const importAccountsFromFile = async event => {
    const file = event.target.files?.[0];
    if (!file) return;
    if (!file.name.toLowerCase().endsWith('.json')) {
      toast.error('仅支持导入 .json 文件');
      event.target.value = '';
      return;
    }
    try {
      const text = await file.text();
      JSON.parse(text);
      setAccountImportState(current => ({
        ...current,
        text,
        fileName: file.name,
      }));
      toast.success(`已载入 ${file.name}`);
    } catch (error) {
      toast.error(error.message || '读取导入文件失败');
    } finally {
      event.target.value = '';
    }
  };

  const submitImportAccounts = async () => {
    setImportingAccounts(true);
    try {
      const accountsToImport = parseJsonInput(accountImportState.text, 'accounts');
      if (!Array.isArray(accountsToImport)) {
        throw new Error('导入内容必须是租户数组或包含 accounts 的对象');
      }
      await requestJSON('/api/m365/import/accounts', {
        method: 'POST',
        body: JSON.stringify({
          accounts: accountsToImport,
          overwrite: accountImportState.overwrite,
        }),
      });
      toast.success(`已导入 ${accountsToImport.length} 个租户`);
      setShowAccountImportDialog(false);
      await loadAccounts();
    } catch (error) {
      toast.error(error.message || '导入租户失败');
    } finally {
      setImportingAccounts(false);
    }
  };

  const toggleRegistrationSelection = (registrationId, checked) => {
    setSelectedRegistrationIds(current => {
      if (checked) return current.includes(registrationId) ? current : [...current, registrationId];
      return current.filter(id => id !== registrationId);
    });
  };

  const deleteSelectedRegistrations = async () => {
    if (selectedRegistrationIds.length === 0) return;
    const confirmed = await dialog.confirm({
      title: '删除注册记录',
      message: `确定要删除选中的 ${selectedRegistrationIds.length} 条注册记录吗？此操作只会清理本地历史，不会删除 Microsoft 365 中已创建的账号。`,
      confirmText: '删除',
    });
    if (!confirmed) return;

    setDeletingRegistrations(true);
    try {
      const result = await requestJSON('/api/m365/registrations', {
        method: 'DELETE',
        body: JSON.stringify({ ids: selectedRegistrationIds }),
      });
      const deletedCount = Number(result?.deletedCount || selectedRegistrationIds.length);
      toast.success(`已删除 ${deletedCount} 条注册记录`);
      setRegistrationDetail(current =>
        current && selectedRegistrationIds.includes(current.id) ? null : current
      );
      await loadRegistrations();
    } catch (error) {
      toast.error(error.message || '删除注册记录失败');
    } finally {
      setDeletingRegistrations(false);
    }
  };

  const openCreateUser = () => {
    setEditingUser(null);
    setUserForm({
      ...defaultUserForm,
      password: DEFAULT_NEW_USER_PASSWORD,
    });
    setUserDialogSkuIds([]);
    setInitialUserSkuIds([]);
    setLoadingUserDialog(false);
    setShowUserDialog(true);
  };

  const openCreatePublicPage = () => {
    setPublicPageForm({
      ...defaultPublicPageForm,
      accountIds: [],
      domains: [],
    });
    setShowPublicPageDialog(true);
  };

  const openEditPublicPage = page => {
    setPublicPageForm({
      id: page.id,
      name: page.name || '',
      accountIds: Array.isArray(page.accountIds)
        ? page.accountIds.map(item => String(item))
        : page.accountId
          ? [String(page.accountId)]
          : [],
      domains: Array.isArray(page.domains)
        ? page.domains.map(item => String(item).trim().toLowerCase())
        : page.domain
          ? [String(page.domain).trim().toLowerCase()]
          : [],
      usageLocation: '',
      skuIds: Array.isArray(page.skuIds) ? page.skuIds.map(item => String(item)) : [],
      enabled: page.enabled !== false,
      forceChangePasswordNextSignIn: page.forceChangePasswordNextSignIn !== false,
      expiresAt: '',
    });
    setShowPublicPageDialog(true);
  };

  const openInviteCodeGenerator = (pageId = '') => {
    const nextPageId = String(pageId || publicPages[0]?.id || '');
    setInviteCodeGeneratorForm({
      publicPageId: nextPageId,
      quantity: 1,
    });
    setShowInviteCodeDialog(true);
  };

  const openEditUser = async user => {
    if (!selectedAccountId || !user?.id) return;
    setSelectedUserId(String(user.id));
    setEditingUser(user);
    setUserForm({
      ...defaultUserForm,
      displayName: user.displayName || '',
      mailNickname: getPrincipalLocalPart(user.userPrincipalName || user.mailNickname || ''),
      userPrincipalName: user.userPrincipalName || '',
      emailDomain: getDomainFromPrincipalName(user.userPrincipalName || user.mail),
      password: '',
      department: user.department || '',
      jobTitle: user.jobTitle || '',
      officeLocation: user.officeLocation || '',
      usageLocation: user.usageLocation || '',
      accountEnabled: user.accountEnabled !== false,
      forceChangePasswordNextSignIn: false,
    });
    setUserDialogSkuIds([]);
    setInitialUserSkuIds([]);
    setLoadingUserDialog(true);
    setShowUserDialog(true);
    try {
      const [details, licenseItems] = await Promise.all([
        requestJSON(`/api/m365/accounts/${selectedAccountId}/users/${user.id}`),
        loadUserLicenseDetails(user.id),
      ]);
      const assignedSkuIds = licenseItems.map(item => String(item?.skuId || '')).filter(Boolean);
      const principalName = details.userPrincipalName || user.userPrincipalName || '';
      const mailNickname = details.mailNickname || getPrincipalLocalPart(principalName);
      setUserForm({
        displayName: details.displayName || '',
        mailNickname,
        userPrincipalName: principalName,
        emailDomain: getDomainFromPrincipalName(principalName || details.mail),
        password: '',
        department: details.department || '',
        jobTitle: details.jobTitle || '',
        officeLocation: details.officeLocation || '',
        usageLocation: details.usageLocation || '',
        accountEnabled: details.accountEnabled !== false,
        forceChangePasswordNextSignIn: false,
      });
      setUserDialogSkuIds(assignedSkuIds);
      setInitialUserSkuIds(assignedSkuIds);
    } catch (error) {
      setShowUserDialog(false);
      toast.error(error.message || '加载用户详情失败');
    } finally {
      setLoadingUserDialog(false);
    }
  };

  const toggleUserEnabled = async (user, checked) => {
    if (!selectedAccountId || !user?.id) return;
    const nextEnabled = !!checked;
    const previousEnabled = user.accountEnabled !== false;
    if (nextEnabled === previousEnabled) return;
    const targetId = String(user.id);
    setTogglingUserId(targetId);
    setUsers(current =>
      current.map(item =>
        String(item.id) === targetId ? { ...item, accountEnabled: nextEnabled } : item
      )
    );
    try {
      await requestJSON(`/api/m365/accounts/${selectedAccountId}/users/${targetId}`, {
        method: 'PATCH',
        body: JSON.stringify({ accountEnabled: nextEnabled }),
      });
      toast.success(nextEnabled ? '用户已启用' : '用户已禁用');
    } catch (error) {
      setUsers(current =>
        current.map(item =>
          String(item.id) === targetId ? { ...item, accountEnabled: previousEnabled } : item
        )
      );
      toast.error(error.message || '更新用户状态失败');
    } finally {
      setTogglingUserId('');
    }
  };

  const submitUser = async () => {
    if (!selectedAccountId) return;
    setSubmittingUser(true);
    try {
      if (editingUser) {
        const mailNickname = String(userForm.mailNickname || '').trim();
        const emailDomain = String(userForm.emailDomain || '').trim();
        const userPrincipalName =
          mailNickname && emailDomain ? `${mailNickname}@${emailDomain}` : '';
        if (!mailNickname || !emailDomain) {
          toast.warning('请填写登录账号前缀和邮箱后缀');
          return;
        }
        await requestJSON(`/api/m365/accounts/${selectedAccountId}/users/${editingUser.id}`, {
          method: 'PATCH',
          body: JSON.stringify({
            displayName: String(userForm.displayName || '').trim() || mailNickname,
            mailNickname,
            userPrincipalName,
            department: userForm.department,
            jobTitle: userForm.jobTitle,
            officeLocation: userForm.officeLocation,
            usageLocation: userForm.usageLocation,
            accountEnabled: userForm.accountEnabled,
            password: String(userForm.password || '').trim(),
            forceChangePasswordNextSignIn: userForm.forceChangePasswordNextSignIn,
          }),
        });
        const nextSkuIds = userDialogSkuIds.map(skuId => String(skuId || '')).filter(Boolean);
        const previousSkuIds = initialUserSkuIds.map(skuId => String(skuId || '')).filter(Boolean);
        const addLicenses = nextSkuIds.filter(skuId => !previousSkuIds.includes(skuId));
        const removeLicenses = previousSkuIds.filter(skuId => !nextSkuIds.includes(skuId));
        if (addLicenses.length > 0 || removeLicenses.length > 0) {
          setAssigningLicense(true);
          await requestJSON(
            `/api/m365/accounts/${selectedAccountId}/users/${editingUser.id}/assign-license`,
            {
              method: 'POST',
              body: JSON.stringify({
                addLicenses: addLicenses.map(skuId => ({ skuId })),
                removeLicenses,
              }),
            }
          );
        }
        toast.success('用户已更新');
      } else {
        const mailNickname = String(userForm.mailNickname || '').trim();
        const emailDomain = String(userForm.emailDomain || '').trim();
        const displayName = String(userForm.displayName || '').trim() || mailNickname;
        if (!mailNickname || !emailDomain || !userForm.password.trim()) {
          toast.warning('请填写邮箱前缀、邮箱后缀和密码');
          return;
        }
        const createdUser = await requestJSON(`/api/m365/accounts/${selectedAccountId}/users`, {
          method: 'POST',
          body: JSON.stringify({
            ...userForm,
            displayName,
            mailNickname,
            userPrincipalName: `${mailNickname}@${emailDomain}`,
          }),
        });
        if (userDialogSkuIds.length > 0 && createdUser?.id) {
          setAssigningLicense(true);
          await requestJSON(
            `/api/m365/accounts/${selectedAccountId}/users/${createdUser.id}/assign-license`,
            {
              method: 'POST',
              body: JSON.stringify({
                addLicenses: userDialogSkuIds.map(skuId => ({ skuId })),
                removeLicenses: [],
              }),
            }
          );
        }
        setUserDialogSkuIds([]);
        toast.success('用户已创建');
      }
      setShowUserDialog(false);
      await refreshUsersAndSkus();
    } catch (error) {
      toast.error(error.message || '保存用户失败');
    } finally {
      setAssigningLicense(false);
      setSubmittingUser(false);
    }
  };

  const deleteUser = async user => {
    try {
      await requestJSON(`/api/m365/accounts/${selectedAccountId}/users/${user.id}`, {
        method: 'DELETE',
      });
      setPendingUserDeleteId('');
      toast.success('用户已删除');
      if (String(selectedUserId) === String(user.id)) {
        setSelectedUserId('');
      }
      await refreshUsersAndSkus();
    } catch (error) {
      toast.error(error.message || '删除用户失败');
    }
  };

  const submitGroup = async () => {
    if (!selectedAccountId || !groupForm.displayName || !groupForm.mailNickname) {
      toast.warning('请填写组名称和别名');
      return;
    }
    setSubmittingGroup(true);
    try {
      await requestJSON(`/api/m365/accounts/${selectedAccountId}/groups`, {
        method: 'POST',
        body: JSON.stringify(groupForm),
      });
      toast.success('组已创建');
      setShowGroupDialog(false);
      setGroupForm(defaultGroupForm);
      await loadGroups();
    } catch (error) {
      toast.error(error.message || '创建组失败');
    } finally {
      setSubmittingGroup(false);
    }
  };

  const addGroupMember = async () => {
    if (!selectedAccountId || !selectedGroupId || !memberInput.trim()) {
      toast.warning('请输入成员 ID');
      return;
    }
    try {
      await requestJSON(
        `/api/m365/accounts/${selectedAccountId}/groups/${selectedGroupId}/members/${encodeURIComponent(memberInput.trim())}`,
        {
          method: 'POST',
        }
      );
      toast.success('组成员已添加');
      setMemberInput('');
      await loadGroupMembers();
    } catch (error) {
      toast.error(error.message || '添加组成员失败');
    }
  };

  const removeGroupMember = async member => {
    if (!confirmPress(`m365-group-member-remove:${member.id}`, `移除组成员「${member.displayName || member.userPrincipalName}」`)) return;
    try {
      await requestJSON(
        `/api/m365/accounts/${selectedAccountId}/groups/${selectedGroupId}/members/${member.id}`,
        {
          method: 'DELETE',
        }
      );
      toast.success('成员已移除');
      await loadGroupMembers();
    } catch (error) {
      toast.error(error.message || '移除成员失败');
    }
  };

  const assignGroupLicense = async () => {
    if (!selectedAccountId || !selectedGroupId || !groupLicenseSkuId) {
      toast.warning('请选择组和 SKU');
      return;
    }
    setAssigningGroupLicense(true);
    try {
      await requestJSON(
        `/api/m365/accounts/${selectedAccountId}/groups/${selectedGroupId}/assign-license`,
        {
          method: 'POST',
          body: JSON.stringify({ addLicenses: [{ skuId: groupLicenseSkuId }], removeLicenses: [] }),
        }
      );
      toast.success('组许可证已分配');
    } catch (error) {
      toast.error(error.message || '组许可证分配失败');
    } finally {
      setAssigningGroupLicense(false);
    }
  };

  const submitPublicPage = async () => {
    if (!publicPageForm.name.trim()) {
      toast.warning('请填写公开页名称');
      return;
    }
    if (publicPageForm.accountIds.length === 0) {
      toast.warning('请至少选择一个目标租户');
      return;
    }
    setSubmittingPublicPage(true);
    try {
      const payload = {
        name: publicPageForm.name.trim(),
        accountIds: publicPageForm.accountIds.map(item => Number(item)).filter(Boolean),
        domains: publicPageForm.domains
          .map(item => String(item).trim().toLowerCase())
          .filter(Boolean),
        usageLocation: '',
        skuIds: publicPageForm.skuIds,
        enabled: publicPageForm.enabled,
        forceChangePasswordNextSignIn: publicPageForm.forceChangePasswordNextSignIn,
        expiresAt: '',
      };
      if (publicPageForm.id) {
        await requestJSON(`/api/m365/public-pages/${publicPageForm.id}`, {
          method: 'PUT',
          body: JSON.stringify(payload),
        });
        toast.success('公开页配置已更新');
      } else {
        await requestJSON('/api/m365/public-pages', {
          method: 'POST',
          body: JSON.stringify(payload),
        });
        toast.success('公开页已创建');
      }
      setShowPublicPageDialog(false);
      setPublicPageForm(defaultPublicPageForm);
      await loadPublicPages();
    } catch (error) {
      toast.error(error.message || '保存公开页配置失败');
    } finally {
      setSubmittingPublicPage(false);
    }
  };

  const togglePublicPageEnabled = async (page, checked) => {
    const targetId = String(page?.id || '');
    if (!targetId) return;
    const previousEnabled = page.enabled !== false;
    const nextEnabled = typeof checked === 'boolean' ? checked : !previousEnabled;
    if (nextEnabled === previousEnabled) return;
    setTogglingPublicPageId(targetId);
    setPublicPages(current =>
      current.map(item => (String(item.id) === targetId ? { ...item, enabled: nextEnabled } : item))
    );
    try {
      await requestJSON(`/api/m365/public-pages/${targetId}`, {
        method: 'PUT',
        body: JSON.stringify({ enabled: nextEnabled }),
      });
      toast.success(nextEnabled ? '公开页已启用' : '公开页已关闭');
      await Promise.all([loadPublicPages({ silent: true }), loadInviteCodes()]);
    } catch (error) {
      setPublicPages(current =>
        current.map(item =>
          String(item.id) === targetId ? { ...item, enabled: previousEnabled } : item
        )
      );
      toast.error(error.message || '更新公开页状态失败');
    } finally {
      setTogglingPublicPageId('');
    }
  };

  const generateInviteCodes = async () => {
    if (!inviteCodeGeneratorForm.publicPageId) {
      toast.warning('请先选择公开页');
      return;
    }
    setGeneratingInviteCodes(true);
    try {
      const result = await requestJSON('/api/m365/invite-codes', {
        method: 'POST',
        body: JSON.stringify({
          publicPageId: Number(inviteCodeGeneratorForm.publicPageId),
          quantity: Math.max(1, Math.min(5, Number(inviteCodeGeneratorForm.quantity) || 1)),
        }),
      });
      toast.success(`已生成 ${result.createdCount || 1} 个邀请码`);
      setShowInviteCodeDialog(false);
      setInviteCodeGeneratorForm(defaultInviteCodeGeneratorForm);
      setPublicTab('codes');
      await Promise.all([loadPublicPages(), loadInviteCodes(), loadRegistrations()]);
    } catch (error) {
      toast.error(error.message || '生成邀请码失败');
    } finally {
      setGeneratingInviteCodes(false);
    }
  };

  const deletePublicPage = async page => {
    if (!confirmPress(`m365-public-page-delete:${page.id}`, `删除公开页「${page.name}」`)) return;
    try {
      await requestJSON(`/api/m365/public-pages/${page.id}`, { method: 'DELETE' });
      toast.success('公开页配置已删除');
      await Promise.all([loadPublicPages({ silent: true }), loadInviteCodes({ silent: true })]);
    } catch (error) {
      toast.error(error.message || '删除公开页配置失败');
    }
  };

  const deleteInviteBatch = async group => {
    const ids = (group.codes || []).map(codeItem => Number(codeItem.id)).filter(Boolean);
    if (ids.length === 0) {
      toast.warning('当前批次没有可删除的邀请码');
      return;
    }
    try {
      const payload = group.batchId
        ? { publicPageId: Number(group.publicPageId) || undefined, batchId: group.batchId }
        : { ids };
      const result = await requestJSON('/api/m365/invite-codes', {
        method: 'DELETE',
        body: JSON.stringify(payload),
      });
      setPendingInviteBatchDeleteKey('');
      toast.success(`已删除 ${result.deletedCount || ids.length} 个邀请码`);
      await Promise.all([loadPublicPages({ silent: true }), loadInviteCodes({ silent: true })]);
    } catch (error) {
      toast.error(error.message || '删除邀请码批次失败');
    }
  };

  const copyText = useCallback(async (text, message) => {
    try {
      await navigator.clipboard.writeText(String(text || ''));
      toast.success(message || '已复制');
    } catch (error) {
      console.error('copy failed:', error);
      toast.error('复制失败');
    }
  }, []);

  const detectPermissions = useCallback(async () => {
    if (!selectedAccountId) {
      toast.warning('请先选择租户');
      return;
    }
    setPermissionCheckLoading(true);
    setPermissionCheckError('');
    try {
      const data = await requestJSON(`/api/m365/accounts/${selectedAccountId}/permissions`);
      const items = Array.isArray(data.items) ? data.items : [];
      setPermissionItems(
        M365_REQUIRED_PERMISSIONS.map(permission => {
          const matched = items.find(item => item.name === permission.name);
          return {
            ...permission,
            granted: typeof matched?.granted === 'boolean' ? matched.granted : null,
          };
        })
      );
      toast.success('权限检测完成');
    } catch (error) {
      const message = getFriendlyErrorMessage(error.message, '权限检测失败');
      setPermissionCheckError(message);
      toast.error(message);
    } finally {
      setPermissionCheckLoading(false);
    }
  }, [requestJSON, selectedAccountId]);

  const renderTenants = () => (
    <SectionCard
      className="flex min-h-0 flex-1 flex-col"
      bodyClassName={panelBodyClass}
      title="租户管理"
      icon={<Cloud className="h-4 w-4" />}
      action={
        <div className="flex items-center gap-2">
          <Toolbar size="sm" aria-label="导出导入租户" className="shrink-0">
            <Toolbar.Button
              title="导出租户"
              aria-label="导出租户"
              icon={<Upload className="h-3.5 w-3.5" />}
              onClick={exportAccounts}
            >
              <span className="hidden cq-sm:inline">导出</span>
            </Toolbar.Button>
            <Toolbar.Button
              title="导入租户"
              aria-label="导入租户"
              icon={<Download className="h-3.5 w-3.5" />}
              onClick={openImportAccounts}
            >
              <span className="hidden cq-sm:inline">导入</span>
            </Toolbar.Button>
          </Toolbar>
          <Button
            size="sm"
            variant="primary"
            icon={<Plus className="h-3.5 w-3.5" />}
            onClick={openCreateAccount}
          >
            新增租户
          </Button>
        </div>
      }
    >
      {loadingAccounts ? (
        <TenantGridSkeleton />
      ) : (
        <div className={cx(scrollViewportClass, 'grid content-start gap-3 p-1', tenantGridClass)}>
          {accounts.map(account => {
            const active = String(account.id) === String(selectedAccountId);
            const verifying = String(account.id) === String(verifyingAccountId);
            return (
              <div
                key={account.id}
                role="button"
                tabIndex={0}
                className={cx(
                  tenantCardFrameClass,
                  'group flex h-full cursor-pointer flex-col justify-between border bg-kumo-base/95 text-left transition hover:border-brand/40 hover:bg-kumo-base focus:outline-none focus-visible:border-brand/70',
                  active ? 'border-brand/70 bg-brand/5' : 'border-kumo-line/80'
                )}
                onClick={() => setSelectedAccountId(String(account.id))}
                onKeyDown={event => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    setSelectedAccountId(String(account.id));
                  }
                }}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex min-w-0 items-center gap-2">
                      <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-kumo-recessed/60 text-brand">
                        <Cloud className="h-4 w-4" />
                      </div>
                      <div className="min-w-0">
                        <div
                          className="truncate text-sm font-semibold text-kumo-strong"
                          title={getDisplayText(account.name)}
                        >
                          {getDisplayText(account.name)}
                        </div>
                        <div
                          className="truncate text-xs text-kumo-subtle"
                          title={getDisplayText(account.organization || '未校验')}
                        >
                          {getDisplayText(account.organization || '未校验')}
                        </div>
                      </div>
                    </div>
                  </div>
                  <StatusBadge
                    tone={account.lastVerifiedErr ? 'danger' : 'success'}
                    className="shrink-0"
                  >
                    {account.lastVerifiedErr ? '待修复' : '已连通'}
                  </StatusBadge>
                </div>

                <div className="mt-2.5 grid gap-2 rounded-lg border border-kumo-line/60 bg-kumo-recessed/20 p-2 text-xs">
                  <div className="flex items-center justify-between gap-3">
                    <span className="shrink-0 text-kumo-subtle">默认域</span>
                    <span
                      className="min-w-0 truncate font-medium text-kumo-strong"
                      title={getDisplayText(account.defaultDomain)}
                    >
                      {getDisplayText(account.defaultDomain)}
                    </span>
                  </div>
                  <div className="flex items-center justify-between gap-3">
                    <span className="shrink-0 text-kumo-subtle">租户 ID</span>
                    <span
                      className="min-w-0 truncate font-mono text-[11px] text-kumo-subtle"
                      title={getDisplayText(account.tenantId)}
                    >
                      {getDisplayText(account.tenantId)}
                    </span>
                  </div>
                  <div className="flex items-center justify-between gap-3">
                    <span className="shrink-0 text-kumo-subtle">客户端 ID</span>
                    <span
                      className="min-w-0 truncate font-mono text-[11px] text-kumo-subtle"
                      title={getDisplayText(account.clientId)}
                    >
                      {getDisplayText(account.clientId)}
                    </span>
                  </div>
                </div>

                <div className="mt-2.5 flex items-center justify-between gap-2">
                  <span
                    className={cx(
                      'text-[11px] font-medium',
                      active ? 'text-brand' : 'text-kumo-subtle'
                    )}
                  >
                    {active ? '已选中' : '选择'}
                  </span>
                  <div className="flex gap-2" onClick={event => event.stopPropagation()}>
                    <Button
                      size="sm"
                      variant="secondary"
                      shape="square"
                      title="校验"
                      aria-label="校验"
                      loading={verifying}
                      icon={<RefreshCw className="h-3.5 w-3.5" />}
                      onClick={() => verifyAccount(account)}
                    />
                    <Button size="sm" variant="secondary" onClick={() => openEditAccount(account)}>
                      编辑
                    </Button>
                    <Button
                      size="sm"
                      variant={isArmed(`m365-account-delete:${account.id}`) ? 'destructive' : 'secondary-destructive'}
                      shape="square"
                      title="删除"
                      aria-label="删除"
                      icon={<Trash className="h-3.5 w-3.5" />}
                      onClick={() => deleteAccount(account)}
                    />
                  </div>
                </div>
              </div>
            );
          })}
          <Button
            key="tenant-placeholder"
            type="button"
            variant="secondary"
            className={cx(
              tenantCardFrameClass,
              'group !h-full w-full justify-center border-dashed border-kumo-line/80 bg-kumo-base/35 text-kumo-subtle hover:border-brand/45 hover:bg-brand/5 hover:text-brand'
            )}
            onClick={openCreateAccount}
            aria-label="添加新租户"
          >
            <div className="flex flex-col items-center gap-2">
              <div className="flex h-10 w-10 items-center justify-center rounded-full border border-dashed border-kumo-line/90 text-kumo-subtle transition group-hover:border-brand/50 group-hover:text-brand">
                <Plus className="h-5 w-5" />
              </div>
              <span className="text-xs font-medium opacity-0 transition group-hover:opacity-100">
                添加新租户
              </span>
            </div>
          </Button>
        </div>
      )}
    </SectionCard>
  );

  const renderPublicPages = () => (
    <PageStack viewport className="min-h-0 flex-1">
      <SectionCard
        className="flex min-h-0 flex-1 flex-col"
        bodyClassName={panelBodyClass}
        title="公开页"
        icon={<Globe className="h-4 w-4" />}
        action={
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="secondary"
              icon={<RefreshCw className="h-3.5 w-3.5" />}
              onClick={() => {
                loadPublicPages();
                loadInviteCodes();
                loadRegistrations();
              }}
            >
              刷新
            </Button>
            {publicTab === 'pages' ? (
              <Button
                size="sm"
                variant="primary"
                icon={<Plus className="h-3.5 w-3.5" />}
                onClick={openCreatePublicPage}
              >
                新建公开页
              </Button>
            ) : null}
            {publicTab === 'codes' ? (
              <Button
                size="sm"
                variant="primary"
                icon={<Plus className="h-3.5 w-3.5" />}
                onClick={() => openInviteCodeGenerator()}
              >
                生成邀请码
              </Button>
            ) : null}
          </div>
        }
      >
        <div className="flex min-h-0 flex-1 flex-col gap-4">
          <div className="shrink-0">
            <Tabs
              {...TOOL_TABS_PROPS}
              value={publicTab}
              onValueChange={setPublicTab}
              tabs={[
                { value: 'pages', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />公开页配置</span> },
                { value: 'codes', label: <span className="inline-flex items-center gap-1.5"><Key className="h-3.5 w-3.5" />邀请码批次</span> },
                { value: 'registrations', label: <span className="inline-flex items-center gap-1.5"><Users className="h-3.5 w-3.5" />注册记录</span> },
              ]}
              className="w-fit max-w-full"
              listClassName="w-fit max-w-full"
            />
          </div>

          {publicTab === 'pages' ? (
            <div className="flex min-h-0 flex-1 flex-col">
              {publicPagesLoading ? (
                <div className="grid auto-rows-fr gap-3 cq-md:grid-cols-2 cq-xl:grid-cols-3 cq-2xl:grid-cols-4">
                  {Array.from({ length: 4 }).map((_, index) => (
                    <SkeletonLine key={index} className="h-60 w-full" />
                  ))}
                </div>
              ) : filteredPublicPages.length === 0 ? (
                <EmptyState
                  icon={Globe}
                  title="还没有公开页"
                  description="点击右上角新建公开页"
                  card={false}
                />
              ) : (
                <div className={cx(scrollViewportClass, 'pr-1')}>
                  <div className="grid auto-rows-fr gap-3 cq-md:grid-cols-2 cq-xl:grid-cols-3 cq-2xl:grid-cols-4">
                    {filteredPublicPages.map(page => {
                      const pageAccounts = (page.accountIds || [])
                        .map(accountId => accountLookup.get(String(accountId)))
                        .filter(Boolean);
                      const accountNames = pageAccounts
                        .map(account => account.name)
                        .filter(Boolean);
                      const domainList = Array.isArray(page.domains) ? page.domains : [];
                      const skuLabels = (page.skuIds || [])
                        .map(skuId => {
                          const normalizedSkuId = String(skuId || '').trim();
                          return (
                            publicPageSkuLabelLookup.get(normalizedSkuId) ||
                            skuLabelLookup.get(normalizedSkuId) ||
                            getSkuDisplayLabel('', normalizedSkuId)
                          );
                        })
                        .filter(Boolean);
                      const totalInviteCodeCount = page.inviteCodeCount || 0;
                      const usedInviteCodeCount = page.usedInviteCodeCount || 0;
                      const inviteCodeSummary = `邀请码 ${usedInviteCodeCount}/${totalInviteCodeCount}`;
                      const pageEnabled = page.enabled !== false;
                      const togglingCurrentPage = togglingPublicPageId === String(page.id);
                      return (
                        <div key={page.id} className={publicResourceCardClass}>
                          <div className={publicResourceCardHeaderClass}>
                            <div className="min-w-0 flex flex-1 items-center">
                              <div className="flex min-w-0 flex-wrap items-center gap-2">
                                <div
                                  className="truncate text-sm font-semibold text-kumo-strong"
                                  title={page.name}
                                >
                                  {page.name}
                                </div>
                                <StatusBadge tone={page.available ? 'success' : 'warning'}>
                                  {page.available ? '可用' : '停用'}
                                </StatusBadge>
                              </div>
                            </div>
                            <div className="flex shrink-0 items-center gap-3">
                              <span title={inviteCodeSummary}>
                                <Badge
                                  variant="outline"
                                  className="max-w-40 !px-2.5 !py-1 !text-[11px] font-medium !text-kumo-strong"
                                >
                                  <span className="truncate">{inviteCodeSummary}</span>
                                </Badge>
                              </span>
                              <div
                                className="flex items-center"
                                title={pageEnabled ? '已启用' : '已关闭'}
                              >
                                <Switch
                                  size="sm"
                                  aria-label={`${page.name} 启用开关`}
                                  checked={pageEnabled}
                                  disabled={togglingCurrentPage}
                                  onCheckedChange={checked =>
                                    togglePublicPageEnabled(page, checked)
                                  }
                                />
                              </div>
                            </div>
                          </div>

                          <div className={publicResourceCardGridClass}>
                            <div className={publicResourceCardFieldClass}>
                              <div className="text-[11px] text-kumo-subtle">目标租户</div>
                              <div
                                className="mt-2 truncate text-xs font-medium text-kumo-strong"
                                title={accountNames.join('、') || '-'}
                              >
                                {accountNames.join('、') || '-'}
                              </div>
                            </div>

                            <div className={publicResourceCardFieldClass}>
                              <div className="text-[11px] text-kumo-subtle">域名</div>
                              <div className="mt-2">
                                <DomainListPopover
                                  domains={domainList}
                                  copyText={copyText}
                                  label={`${page.name} 域名`}
                                />
                              </div>
                            </div>

                            <div className={publicResourceCardFieldClass}>
                              <div className="text-[11px] text-kumo-subtle">配置</div>
                              <div className="mt-2 text-xs font-medium text-kumo-strong">
                                {page.forceChangePasswordNextSignIn ? '首次改密' : '无需改密'}
                              </div>
                            </div>

                            <div className={publicResourceCardFieldClass}>
                              <div className="text-[11px] text-kumo-subtle">许可证</div>
                              <div className="mt-2">
                                <LicenseListPopover
                                  licenses={skuLabels}
                                  copyText={copyText}
                                  label={`${page.name} 许可证`}
                                />
                              </div>
                            </div>
                          </div>

                          <div className={publicResourceCardActionBarClass}>
                            <Button
                              size="sm"
                              variant="primary"
                              className="basis-0 !justify-center text-center"
                              style={{ flex: 2.6 }}
                              onClick={() => openInviteCodeGenerator(page.id)}
                            >
                              生成邀请码
                            </Button>
                            <Button
                              size="sm"
                              variant="secondary"
                              className="basis-0 !justify-center text-center"
                              style={{ flex: 1.2 }}
                              onClick={() => openEditPublicPage(page)}
                            >
                              编辑
                            </Button>
                            <Button
                              size="sm"
                              variant={isArmed(`m365-public-page-delete:${page.id}`) ? 'destructive' : 'secondary-destructive'}
                              className="basis-0 !justify-center text-center"
                              style={{ flex: 1 }}
                              onClick={() => deletePublicPage(page)}
                            >
                              删除
                            </Button>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>
          ) : null}

          {publicTab === 'codes' ? (
            <div className="flex min-h-0 flex-1 flex-col">
              {inviteCodesLoading ? (
                <div className="space-y-2.5">
                  {Array.from({ length: 4 }).map((_, index) => (
                    <SkeletonLine key={index} className="h-20 w-full" />
                  ))}
                </div>
              ) : groupedInviteCodeBatches.length === 0 ? (
                <EmptyState
                  icon={Globe}
                  title="还没有邀请码"
                  description="先创建公开页再生成邀请码"
                  card={false}
                />
              ) : (
                <div className={cx(scrollViewportClass, 'pr-1')}>
                  <div className="grid auto-rows-fr gap-3 cq-md:grid-cols-2 cq-xl:grid-cols-3 cq-2xl:grid-cols-4">
                    {groupedInviteCodeBatches.map(group =>
                      (() => {
                        const totalCount = group.codes.length;
                        const remainingCount = Math.max(totalCount - group.usedCount, 0);
                        const exhausted = remainingCount <= 0;
                        const partiallyUsed = group.usedCount > 0 && !exhausted;
                        const statusTone = exhausted
                          ? 'danger'
                          : partiallyUsed
                            ? 'warning'
                            : 'success';
                        const batchLabel = group.batchId || '单个生成';
                        const fallbackCode = group.codes[0]?.code || '';
                        const publicRegisterPath = group.batchId
                          ? `/m365/register?batch=${encodeURIComponent(group.batchId)}`
                          : `/m365/register?code=${encodeURIComponent(fallbackCode)}`;
                        const publicRegisterUrl = `${window.location.origin}${publicRegisterPath}`;
                        return (
                          <div key={group.key} className={publicResourceCardClass}>
                            <div className={publicResourceCardHeaderClass}>
                              <div className="min-w-0 flex flex-1 items-center">
                                <div className="flex min-w-0 flex-wrap items-center gap-2">
                                  <div className="truncate text-sm font-semibold text-kumo-strong">
                                    {group.publicPageName}
                                  </div>
                                  <StatusBadge tone={statusTone}>
                                    已用 {group.usedCount}/{totalCount}
                                  </StatusBadge>
                                </div>
                              </div>
                              <span title={batchLabel}>
                                <Badge
                                  variant="outline"
                                  className="max-w-40 !px-2.5 !py-1 !text-[11px] font-medium !text-kumo-strong"
                                >
                                  <span className="truncate">{batchLabel}</span>
                                </Badge>
                              </span>
                            </div>

                            <div className={publicResourceCardGridClass}>
                              <div className={publicResourceCardFieldClass}>
                                <div className="text-[11px] text-kumo-subtle">域名</div>
                                <div className="mt-2">
                                  <DomainListPopover
                                    domains={group.domains}
                                    copyText={copyText}
                                    label={`${group.publicPageName} 域名`}
                                  />
                                </div>
                              </div>

                              <div className={publicResourceCardFieldClass}>
                                <div className="text-[11px] text-kumo-subtle">剩余可用</div>
                                <div
                                  className={cx(
                                    'mt-2 text-sm font-semibold',
                                    exhausted ? 'text-kumo-danger' : 'text-kumo-strong'
                                  )}
                                >
                                  {remainingCount} / {totalCount}
                                </div>
                              </div>

                              <div className={publicResourceCardFieldClass}>
                                <div className="text-[11px] text-kumo-subtle">创建时间</div>
                                <div
                                  className="mt-2 truncate text-xs font-medium text-kumo-strong"
                                  title={group.createdAt ? formatDateTime(group.createdAt) : '-'}
                                >
                                  {group.createdAt ? formatDateTime(group.createdAt) : '-'}
                                </div>
                              </div>

                              <div className={publicResourceCardFieldClass}>
                                <div className="text-[11px] text-kumo-subtle">最后使用</div>
                                <div
                                  className="mt-2 truncate text-xs font-medium text-kumo-strong"
                                  title={
                                    group.lastUsedAt ? formatDateTime(group.lastUsedAt) : '未使用'
                                  }
                                >
                                  {group.lastUsedAt ? formatDateTime(group.lastUsedAt) : '未使用'}
                                </div>
                              </div>
                            </div>

                            <div className={publicResourceCardActionBarClass}>
                              <Button
                                size="sm"
                                variant={exhausted ? 'secondary' : 'primary'}
                                className="basis-0 !justify-center text-center"
                                style={{ flex: 3 }}
                                icon={<Copy className="h-3.5 w-3.5" />}
                                onClick={() => copyText(publicRegisterUrl, '注册链接已复制')}
                                disabled={!publicRegisterUrl}
                              >
                                复制注册链接
                              </Button>
                              <Button
                                size="sm"
                                variant="destructive"
                                className="basis-0 !justify-center text-center"
                                style={{ flex: 1 }}
                                onClick={() => {
                                  if (pendingInviteBatchDeleteKey === group.key) {
                                    void deleteInviteBatch(group);
                                    return;
                                  }
                                  setPendingInviteBatchDeleteKey(group.key);
                                }}
                              >
                                {pendingInviteBatchDeleteKey === group.key ? '确认' : '删除'}
                              </Button>
                            </div>
                          </div>
                        );
                      })()
                    )}
                  </div>
                </div>
              )}
            </div>
          ) : null}

          {publicTab === 'registrations' ? (
            <AppCard padding="none" className="flex min-h-0 flex-1 flex-col overflow-hidden">
              <div className="flex items-center justify-between border-b border-kumo-line/60 px-4 py-3">
                <div>
                  <div className="text-sm font-semibold text-kumo-strong">注册记录</div>
                </div>
                <div className="flex items-center gap-2">
                  {selectedRegistrationIds.length > 0 ? (
                    <div className="text-xs text-kumo-subtle">
                      已选 {selectedRegistrationIds.length} 条
                    </div>
                  ) : null}
                  <Button
                    size="sm"
                    variant="secondary-destructive"
                    icon={<Trash className="h-3.5 w-3.5" />}
                    onClick={deleteSelectedRegistrations}
                    disabled={selectedRegistrationIds.length === 0 || deletingRegistrations}
                  >
                    {deletingRegistrations ? '删除中...' : '批量删除'}
                  </Button>
                </div>
              </div>
              {registrationsLoading ? (
                <div className="space-y-3 p-4">
                  {Array.from({ length: 5 }).map((_, index) => (
                    <SkeletonLine key={index} className="h-16 w-full" />
                  ))}
                </div>
              ) : filteredRegistrations.length === 0 ? (
                <EmptyState
                  icon={Users}
                  title="暂无注册记录"
                  card={false}
                />
              ) : (
                <DataTableFrame
                  variant="embedded"
                  density="dense"
                  className="min-h-0 flex-1 overflow-auto scrollbar-thin"
                >
                  <AppTable tableId="m365-registration-records" columns={REGISTRATION_TABLE_COLUMNS}>
                    <Table.Header sticky variant="compact">
                      <Table.Row>
                        <Table.CheckHead
                          checked={
                            filteredRegistrations.length > 0 &&
                            selectedRegistrationRecords.length === filteredRegistrations.length
                          }
                          indeterminate={
                            selectedRegistrationRecords.length > 0 &&
                            selectedRegistrationRecords.length < filteredRegistrations.length
                          }
                          onCheckedChange={checked =>
                            setSelectedRegistrationIds(
                              checked ? filteredRegistrations.map(record => record.id) : []
                            )
                          }
                          aria-label="全选注册记录"
                          className="!px-2 !py-1.5 text-center"
                        />
                        <Table.Head>账号</Table.Head>
                        <Table.Head>状态</Table.Head>
                        <Table.Head>来源</Table.Head>
                        <Table.Head>目标租户</Table.Head>
                        <Table.Head>Graph 用户 ID</Table.Head>
                        <Table.Head>创建时间</Table.Head>
                        <Table.Head>结果 / 错误</Table.Head>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {filteredRegistrations.map(record => (
                        <Table.Row
                          key={record.id}
                          variant={
                            selectedRegistrationIds.includes(record.id) ? 'selected' : 'default'
                          }
                        >
                          <Table.CheckCell
                            checked={selectedRegistrationIds.includes(record.id)}
                            onCheckedChange={checked =>
                              toggleRegistrationSelection(record.id, Boolean(checked))
                            }
                            aria-label={`选择注册记录 ${record.userPrincipalName || record.displayName || record.id}`}
                            className="!px-2 !py-1.5 text-center"
                          />
                          <Table.Cell>
                            <div className="min-w-0">
                              <div
                                className="truncate text-sm font-semibold text-kumo-strong"
                                title={record.displayName || record.userPrincipalName || '-'}
                              >
                                {record.displayName || record.userPrincipalName || '-'}
                              </div>
                              <div
                                className="truncate text-xs text-kumo-subtle"
                                title={record.userPrincipalName || '-'}
                              >
                                {record.userPrincipalName || '-'}
                              </div>
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <StatusBadge tone={getRegistrationTone(record.status)}>
                              {getRegistrationStatusLabel(record.status)}
                            </StatusBadge>
                          </Table.Cell>
                          <Table.Cell>
                            <div className="min-w-0 space-y-1">
                              <div
                                className="truncate text-xs font-medium text-kumo-strong"
                                title={record.publicPageName || record.inviteName || '-'}
                              >
                                {record.publicPageName || record.inviteName || '-'}
                              </div>
                              <div
                                className="truncate font-mono text-[11px] text-kumo-subtle"
                                title={record.inviteCode || '-'}
                              >
                                {record.inviteCode || '-'}
                              </div>
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <div
                              className="truncate text-xs font-medium text-kumo-strong"
                              title={record.accountName || '-'}
                            >
                              {record.accountName || '-'}
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <div
                              className="truncate font-mono text-[11px] text-kumo-subtle"
                              title={record.graphUserId || '-'}
                            >
                              {record.graphUserId || '-'}
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <div
                              className="truncate text-xs text-kumo-strong"
                              title={record.createdAt ? formatDateTime(record.createdAt) : '-'}
                            >
                              {record.createdAt ? formatDateTime(record.createdAt) : '-'}
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <Button
                              type="button"
                              variant="ghost"
                              title={getRegistrationResultText(record)}
                              onClick={() => setRegistrationDetail(record)}
                              className={cx(
                                '!h-auto w-full justify-start overflow-hidden px-2.5 py-1.5 text-left text-xs',
                                record.errorMessage
                                  ? 'border border-kumo-danger/20 bg-kumo-danger/5 text-kumo-danger hover:bg-kumo-danger/10'
                                  : 'text-kumo-subtle hover:bg-kumo-recessed/25'
                              )}
                            >
                              <span className="block truncate">
                                {getRegistrationResultText(record)}
                              </span>
                            </Button>
                          </Table.Cell>
                        </Table.Row>
                      ))}
                    </Table.Body>
                  </AppTable>
                </DataTableFrame>
              )}
            </AppCard>
          ) : null}
        </div>
      </SectionCard>
    </PageStack>
  );

  const renderUsers = () => (
    <PageStack viewport>
      <SectionCard
        className="shrink-0"
        bodyClassName={panelBodyClass}
        title="SKU 库存"
        icon={<Database className="h-4 w-4" />}
        action={
          <Button
            size="sm"
            variant="secondary"
            icon={<RefreshCw className="h-3.5 w-3.5" />}
            onClick={loadSkus}
          >
            刷新
          </Button>
        }
      >
        {!selectedAccountId ? (
          <EmptyState icon={Database} title="请先选择租户" />
        ) : skuLoading ? (
          <SkuGridSkeleton />
        ) : skus.length === 0 ? (
          <EmptyState
            icon={Database}
            title="暂无 SKU 数据"
          />
        ) : (
          <div
            className={cx(scrollViewportClass, 'grid auto-rows-max content-start gap-2.5 pr-1')}
            style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))' }}
          >
            {skus.map(sku => {
              const totalUnits = Number(sku.prepaidUnits?.enabled ?? 0);
              const warningUnits = Number(sku.prepaidUnits?.warning ?? 0);
              const consumedUnits = Number(sku.consumedUnits ?? 0);
              const availableUnits = Math.max(0, totalUnits - consumedUnits);
              const usagePct =
                totalUnits > 0 ? clampPercent((consumedUnits / totalUnits) * 100) : 0;
              const progressTone =
                usagePct >= 90
                  ? '!bg-kumo-danger'
                  : usagePct >= 70
                    ? '!bg-kumo-warning'
                    : '!bg-brand';
              const lifecycleText = getSkuLifecycleText(sku);
              return (
                <div
                  key={sku.skuId}
                  className="rounded-lg border border-kumo-line/70 bg-kumo-base/95 px-3 py-2.5"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0 flex-1">
                      <div
                        className="truncate text-[15px] font-semibold text-kumo-strong"
                        title={getSkuDisplayLabel(sku.skuPartNumber, sku.skuId)}
                      >
                        {getSkuDisplayLabel(sku.skuPartNumber, sku.skuId)}
                      </div>
                      <div className="mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-1 text-[11px]">
                        <span className="font-semibold text-kumo-strong">
                          {formatMetricNumber(consumedUnits)} / {formatMetricNumber(totalUnits)}
                        </span>
                        <span className="text-kumo-subtle">
                          剩余 {formatMetricNumber(availableUnits)}
                        </span>
                        {warningUnits > 0 ? (
                          <span className="text-kumo-subtle">
                            警告 {formatMetricNumber(warningUnits)}
                          </span>
                        ) : null}
                      </div>
                    </div>
                    <div className="flex shrink-0 flex-wrap items-center justify-end gap-1">
                      {lifecycleText ? (
                        <Badge
                          variant="outline"
                          className="border-kumo-line/70 bg-kumo-recessed/30 !px-2 !py-0.5 !text-[11px] font-medium leading-5 !text-kumo-subtle"
                        >
                          {lifecycleText}
                        </Badge>
                      ) : null}
                      <StatusBadge
                        tone={usagePct >= 90 ? 'danger' : usagePct >= 70 ? 'warning' : 'success'}
                      >
                        {usagePct.toFixed(0)}%
                      </StatusBadge>
                    </div>
                  </div>

                  <Meter
                    label=""
                    value={usagePct}
                    max={100}
                    showValue={false}
                    className="mt-2.5"
                    trackClassName="!h-1.5 bg-kumo-recessed/80"
                    indicatorClassName={progressTone}
                  />
                </div>
              );
            })}
          </div>
        )}
      </SectionCard>

      <SectionCard
        title="用户与许可证"
        icon={<Users className="h-4 w-4" />}
        bodyPadding="none"
        action={
          <div className="flex items-center gap-2">
            <ResponsiveSearchInput
              value={userSearch}
              onChange={event => setUserSearch(event.target.value)}
              onSearch={loadUsers}
              placeholder="搜索显示名或 UPN"
              ariaLabel="搜索用户"
              className="cq-sm:w-56"
            />
            <Button
              size="sm"
              variant="primary"
              icon={<Plus className="h-3.5 w-3.5" />}
              onClick={openCreateUser}
            >
              新增用户
            </Button>
          </div>
        }
      >
        {!selectedAccountId ? (
          <div className="p-4">
            <EmptyState
              icon={Users}
              title="请先选择租户"
            />
          </div>
        ) : usersLoading ? (
          <div className="p-4">
            <CardTableSkeleton rows={7} />
          </div>
        ) : users.length === 0 ? (
          <div className="p-4">
            <EmptyState icon={User} title="暂无用户" description="当前筛选无用户" />
          </div>
        ) : (
          <DataTableFrame
            variant="embedded"
            density="compact"
            className="overflow-auto scrollbar-thin"
          >
            <AppTable
              layout="fixed"
              widths={USER_TABLE_COLUMN_WIDTHS}
              className="w-full text-xs [&_td]:align-middle"
            >
              <colgroup>
                {USER_TABLE_COLUMN_WIDTHS.map((width, index) => (
                  <col key={index} style={{ width }} />
                ))}
              </colgroup>
              <Table.Header sticky variant="compact">
                <Table.Row>
                  <Table.Head className="!px-3 !py-2 text-center">状态</Table.Head>
                  <Table.Head className="!px-3 !py-2">显示名</Table.Head>
                  <Table.Head className="!px-3 !py-2">登录账号</Table.Head>
                  <Table.Head className="!px-3 !py-2">邮箱</Table.Head>
                  <Table.Head className="!px-3 !py-2">许可证</Table.Head>
                  <Table.Head className="app-table-action !px-3 !py-2">操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {users.map(user => {
                  const assignedSkuLabels = getAssignedSkuLabels(
                    user.assignedLicenses,
                    skuLabelLookup
                  );
                  const assignedSkuSummary =
                    assignedSkuLabels.length <= 2
                      ? assignedSkuLabels.join('、') || '-'
                      : `${assignedSkuLabels.slice(0, 2).join('、')} +${assignedSkuLabels.length - 2}`;
                  const deleteArmed = String(pendingUserDeleteId) === String(user.id);
                  return (
                    <Table.Row
                      key={user.id}
                      variant={
                        String(user.id) === String(selectedUserId) ? 'selected' : 'default'
                      }
                      className="h-10 cursor-pointer"
                      onClick={() => setSelectedUserId(String(user.id))}
                    >
                      <Table.Cell className="!px-3 !py-1.5 text-center">
                        <div
                          className="flex justify-center"
                          onClick={event => event.stopPropagation()}
                        >
                          <Switch
                            size="sm"
                            aria-label={`${getDisplayText(user.displayName)}状态开关`}
                            checked={user.accountEnabled !== false}
                            disabled={togglingUserId === String(user.id)}
                            onCheckedChange={checked => toggleUserEnabled(user, checked)}
                          />
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-3 !py-1.5">
                        <div
                          className="truncate font-medium text-kumo-strong"
                          title={getDisplayText(user.displayName)}
                        >
                          {getDisplayText(user.displayName)}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-3 !py-1.5">
                        <div className="truncate" title={getDisplayText(user.userPrincipalName)}>
                          {getDisplayText(user.userPrincipalName)}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-3 !py-1.5">
                        <div className="truncate" title={getDisplayText(user.mail)}>
                          {getDisplayText(user.mail)}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-3 !py-1.5">
                        <div
                          className="flex min-w-0 items-center gap-2"
                          title={assignedSkuLabels.join('、') || '-'}
                        >
                          <span className="min-w-0 flex-1 truncate">{assignedSkuSummary}</span>
                          {assignedSkuLabels.length > 1 ? (
                          <Badge
                            variant="outline"
                            className="shrink-0 border-kumo-line/70 bg-kumo-recessed/20 !px-2 !py-0.5 !text-[10px] !text-kumo-subtle"
                          >
                            {assignedSkuLabels.length} 项
                          </Badge>
                          ) : null}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-3 !py-1.5">
                        <div
                          className="flex items-center justify-end gap-2 whitespace-nowrap"
                          onClick={event => event.stopPropagation()}
                        >
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() => openEditUser(user)}
                          >
                            编辑
                          </Button>
                          <Button
                            size="sm"
                            variant={deleteArmed ? 'destructive' : 'secondary-destructive'}
                            title={deleteArmed ? '确认' : '删除用户'}
                            aria-label={deleteArmed ? '确认' : '删除用户'}
                            icon={<Trash className="h-3.5 w-3.5" />}
                            className={deleteArmed ? 'ring-1 ring-kumo-danger/50' : ''}
                            onClick={() => {
                              if (deleteArmed) {
                                void deleteUser(user);
                                return;
                              }
                              setPendingUserDeleteId(String(user.id));
                            }}
                          >
                            {deleteArmed ? '确认' : '删除'}
                          </Button>
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  );
                })}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        )}
      </SectionCard>
    </PageStack>
  );

  const renderGroups = () => (
    <PageStack viewport className="min-h-0 flex-1">
      <SectionCard
        className="flex min-h-0 flex-1 flex-col"
        bodyClassName={panelBodyClass}
        title="组管理"
        icon={<Folder className="h-4 w-4" />}
        action={
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="secondary"
              icon={<RefreshCw className="h-3.5 w-3.5" />}
              onClick={loadGroups}
            >
              刷新
            </Button>
            <Button
              size="sm"
              variant="primary"
              icon={<Plus className="h-3.5 w-3.5" />}
              onClick={() => setShowGroupDialog(true)}
            >
              新建组
            </Button>
          </div>
        }
      >
        {!selectedAccountId ? (
          <EmptyState icon={Folder} title="请先选择租户" />
        ) : groupsLoading ? (
          <GroupsTabSkeleton />
        ) : groups.length === 0 ? (
          <EmptyState
            icon={Folder}
            title="暂无组"
            description="先创建一个组"
          />
        ) : (
          <div className="grid min-h-0 flex-1 gap-4 cq-lg:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
            <AppCard padding="none" className="flex min-h-0 flex-col">
              <DataTableFrame variant="embedded" density="compact" className={scrollViewportClass}>
                <Table layout="auto" className="[&_td]:py-3 [&_th]:py-3">
                  <Table.Header>
                    <Table.Row>
                      <Table.Head>组</Table.Head>
                      <Table.Head>邮件</Table.Head>
                      <Table.Head>类型</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {groups.map(group => (
                      <Table.Row
                        key={group.id}
                        className={
                          String(group.id) === String(selectedGroupId) ? 'bg-brand/5' : ''
                        }
                        onClick={() => setSelectedGroupId(String(group.id))}
                      >
                        <Table.Cell>
                          <div className="font-medium text-kumo-strong">
                            {group.displayName || '-'}
                          </div>
                          <div className="text-xs text-kumo-subtle">{group.id}</div>
                        </Table.Cell>
                        <Table.Cell>{group.mail || '-'}</Table.Cell>
                        <Table.Cell>{group.securityEnabled ? '安全组' : '协作组'}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </DataTableFrame>
            </AppCard>

            <SectionCard
              className="flex min-h-0 flex-col"
              bodyClassName={panelBodyClass}
              title={selectedGroup ? selectedGroup.displayName : '组成员'}
              description="输入成员对象 ID"
              icon={<Users className="h-4 w-4" />}
              bodyPadding="sm"
              action={
                <div className="flex items-center gap-2">
                  <Input
                    aria-label="成员对象 ID"
                    size="sm"
                    value={memberInput}
                    onChange={event => setMemberInput(event.target.value)}
                    placeholder="成员对象 ID"
                  />
                  <Button size="sm" variant="secondary" onClick={addGroupMember}>
                    添加成员
                  </Button>
                </div>
              }
            >
              {!selectedGroup ? (
                <EmptyState
                  icon={Users}
                  title="请选择一个组"
                  card={false}
                />
              ) : groupMembersLoading ? (
                <CardTableSkeleton rows={5} showToolbar />
              ) : (
                <div className="flex min-h-0 flex-1 flex-col gap-4">
                  <div className="flex items-center gap-2">
                    <Select
                      aria-label="组许可证 SKU"
                      size="sm"
                      value={groupLicenseSkuId}
                      onValueChange={setGroupLicenseSkuId}
                      items={skuItems}
                    />
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={assignGroupLicense}
                      disabled={assigningGroupLicense}
                    >
                      分配组许可证
                    </Button>
                  </div>
                  <div
                    className={cx(
                      tableFrameClass,
                      'rounded-lg border border-kumo-line/80 bg-kumo-base'
                    )}
                  >
                    <DataTableFrame
                      variant="embedded"
                      density="compact"
                      className={scrollViewportClass}
                    >
                      <Table layout="auto" className="[&_td]:py-3 [&_th]:py-3">
                        <Table.Header>
                          <Table.Row>
                            <Table.Head>成员</Table.Head>
                            <Table.Head>邮箱</Table.Head>
                            <Table.Head className="app-table-action">操作</Table.Head>
                          </Table.Row>
                        </Table.Header>
                        <Table.Body>
                          {groupMembers.map(member => (
                            <Table.Row key={member.id}>
                              <Table.Cell>
                                <div className="font-medium text-kumo-strong">
                                  {member.displayName || '-'}
                                </div>
                                <div className="text-xs text-kumo-subtle">{member.id}</div>
                              </Table.Cell>
                              <Table.Cell>
                                {member.userPrincipalName || member.mail || '-'}
                              </Table.Cell>
                              <Table.Cell>
                                <div className="flex justify-end">
                                  <Button
                                    size="sm"
                                    variant={isArmed(`m365-group-member-remove:${member.id}`) ? 'destructive' : 'secondary-destructive'}
                                    onClick={() => removeGroupMember(member)}
                                  >
                                    移除
                                  </Button>
                                </div>
                              </Table.Cell>
                            </Table.Row>
                          ))}
                        </Table.Body>
                      </Table>
                    </DataTableFrame>
                  </div>
                </div>
              )}
            </SectionCard>
          </div>
        )}
      </SectionCard>
    </PageStack>
  );

  return (
    <PageStack viewport className={workspaceHeightClass}>
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={[
            {
              value: 'tenants',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Cloud className="h-4 w-4" />
                  租户
                </span>
              ),
            },
            {
              value: 'users',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Users className="h-4 w-4" />
                  用户与许可证
                </span>
              ),
            },
            {
              value: 'groups',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Folder className="h-4 w-4" />组
                </span>
              ),
            },
            {
              value: 'public',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Globe className="h-4 w-4" />
                  公开页
                </span>
              ),
            },
          ]}
        />
        <div className="flex min-w-0 shrink-0 flex-wrap items-center gap-2">
          <TabBarOverflowActions
            items={[
              {
                key: 'permission',
                label: '权限说明',
                icon: <Shield className="h-3.5 w-3.5" />,
                onClick: () => setShowPermissionDialog(true),
              },
              ...(['users', 'groups'].includes(activeTab)
                ? [
                    {
                      key: 'tenant',
                      type: 'select',
                      label: '租户',
                      icon: <Cloud className="h-3.5 w-3.5" />,
                      value: selectedAccountId,
                      onValueChange: setSelectedAccountId,
                      disabled: false,
                      options: accountSelectItems,
                    },
                  ]
                : []),
            ]}
          />
        </div>
      </div>

      {activeTab === 'tenants' && renderTenants()}
      {activeTab === 'users' && renderUsers()}
      {activeTab === 'groups' && renderGroups()}
      {activeTab === 'public' && renderPublicPages()}

      <Dialog.Root open={showAccountDialog} onOpenChange={setShowAccountDialog}>
        <Dialog className="@container w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] p-5 cq-sm:w-full cq-sm:max-w-xl">
          <div className="space-y-4">
            <Dialog.Title>{editingAccount ? '编辑租户' : '新增租户'}</Dialog.Title>
            <div className="grid gap-3">
              <Input
                size="sm"
                aria-label="名称"
                value={accountForm.name}
                onChange={event =>
                  setAccountForm(current => ({ ...current, name: event.target.value }))
                }
                placeholder="显示名称"
              />
              <Input
                size="sm"
                aria-label="租户 ID"
                value={accountForm.tenantId}
                onChange={event =>
                  setAccountForm(current => ({ ...current, tenantId: event.target.value }))
                }
                placeholder="tenant_id"
              />
              <Input
                size="sm"
                aria-label="客户端 ID"
                value={accountForm.clientId}
                onChange={event =>
                  setAccountForm(current => ({ ...current, clientId: event.target.value }))
                }
                placeholder="client_id"
              />
              <Input
                size="sm"
                aria-label="客户端密钥"
                value={accountForm.clientSecret}
                onChange={event =>
                  setAccountForm(current => ({ ...current, clientSecret: event.target.value }))
                }
                placeholder={editingAccount ? '留空则保持原密钥' : 'client_secret'}
              />
              <Textarea
                aria-label="描述"
                value={accountForm.description}
                onChange={event =>
                  setAccountForm(current => ({ ...current, description: event.target.value }))
                }
                placeholder="备注或租户说明"
              />
            </div>
            <div className="flex justify-end gap-2">
              <Button size="sm" variant="secondary" onClick={() => setShowAccountDialog(false)}>
                取消
              </Button>
              <Button
                size="sm"
                variant="primary"
                onClick={submitAccount}
                disabled={submittingAccount}
              >
                {submittingAccount ? '保存中...' : '保存'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={showAccountImportDialog} onOpenChange={setShowAccountImportDialog}>
        <Dialog className="@container w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] p-5 cq-sm:w-full cq-sm:max-w-2xl">
          <div className="space-y-4">
            <Dialog.Title>导入租户</Dialog.Title>
            <input
              ref={accountImportInputRef}
              type="file"
              accept=".json,application/json"
              className="hidden"
              onChange={importAccountsFromFile}
            />
            <div className="space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-kumo-line/80 bg-kumo-recessed/10 px-3 py-2.5">
                <div className="min-w-0">
                  <div className="text-sm font-medium text-kumo-strong">上传文件或直接粘贴</div>
                  <div className="text-xs text-kumo-subtle">
                    仅支持 .json 格式。
                  </div>
                </div>
                <Button
                  size="sm"
                  variant="secondary"
                  icon={<Download className="h-3.5 w-3.5" />}
                  onClick={() => accountImportInputRef.current?.click()}
                >
                  选择文件
                </Button>
              </div>
              {accountImportState.fileName ? (
                <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/10 px-3 py-2 text-xs text-kumo-subtle">
                  当前文件：
                  <span className="font-medium text-kumo-strong">
                    {accountImportState.fileName}
                  </span>
                </div>
              ) : null}
            </div>
            <div className="space-y-2">
              <div className="text-xs font-medium text-kumo-subtle">JSON 内容</div>
              <CodeEditor
                label="租户 JSON"
                language="json"
                value={accountImportState.text}
                onChange={text =>
                  setAccountImportState(current => ({ ...current, text }))
                }
                minHeight="18rem"
                placeholder='{"accounts":[{"name":"Contoso","tenantId":"tenant-id","clientId":"client-id","clientSecret":"client-secret"}]}'
              />
            </div>
            <label className="flex items-center gap-2 text-xs text-kumo-subtle">
              <Checkbox
                checked={accountImportState.overwrite}
                onCheckedChange={checked =>
                  setAccountImportState(current => ({ ...current, overwrite: !!checked }))
                }
              />
              覆盖现有租户数据
            </label>
            <div className="flex justify-end gap-2">
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setShowAccountImportDialog(false)}
              >
                取消
              </Button>
              <Button
                size="sm"
                variant="primary"
                onClick={submitImportAccounts}
                disabled={importingAccounts}
              >
                {importingAccounts ? '导入中...' : '导入'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root
        open={!!registrationDetail}
        onOpenChange={open => {
          if (!open) setRegistrationDetail(null);
        }}
      >
        <Dialog className="@container w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] p-5 cq-sm:w-full cq-sm:max-w-3xl">
          {registrationDetail ? (
            <div className="space-y-4">
              <Dialog.Title>注册记录详情</Dialog.Title>
              <div className="grid gap-3 cq-sm:grid-cols-2">
                <div className="rounded-lg border border-kumo-line/70 bg-kumo-recessed/10 px-3 py-2.5">
                  <div className="text-[11px] text-kumo-subtle">账号</div>
                  <div className="mt-1 text-sm font-semibold text-kumo-strong">
                    {registrationDetail.displayName || registrationDetail.userPrincipalName || '-'}
                  </div>
                  <div className="mt-1 break-all font-mono text-[11px] text-kumo-subtle">
                    {registrationDetail.userPrincipalName || '-'}
                  </div>
                </div>
                <div className="rounded-lg border border-kumo-line/70 bg-kumo-recessed/10 px-3 py-2.5">
                  <div className="text-[11px] text-kumo-subtle">状态</div>
                  <div className="mt-1">
                    <StatusBadge tone={getRegistrationTone(registrationDetail.status)}>
                      {getRegistrationStatusLabel(registrationDetail.status)}
                    </StatusBadge>
                  </div>
                </div>
                <div className="rounded-lg border border-kumo-line/70 bg-kumo-recessed/10 px-3 py-2.5">
                  <div className="text-[11px] text-kumo-subtle">来源公开页 / 邀请码</div>
                  <div className="mt-1 text-sm font-medium text-kumo-strong">
                    {registrationDetail.publicPageName || registrationDetail.inviteName || '-'}
                  </div>
                  <div className="mt-1 break-all font-mono text-[11px] text-kumo-subtle">
                    {registrationDetail.inviteCode || '-'}
                  </div>
                </div>
                <div className="rounded-lg border border-kumo-line/70 bg-kumo-recessed/10 px-3 py-2.5">
                  <div className="text-[11px] text-kumo-subtle">目标租户 / Graph 用户 ID</div>
                  <div className="mt-1 text-sm font-medium text-kumo-strong">
                    {registrationDetail.accountName || '-'}
                  </div>
                  <div className="mt-1 break-all font-mono text-[11px] text-kumo-subtle">
                    {registrationDetail.graphUserId || '-'}
                  </div>
                </div>
              </div>
              <div className="rounded-lg border border-kumo-line/70 bg-kumo-recessed/10 px-3 py-2.5">
                <div className="text-[11px] text-kumo-subtle">创建时间</div>
                <div className="mt-1 text-sm text-kumo-strong">
                  {registrationDetail.createdAt
                    ? formatDateTime(registrationDetail.createdAt)
                    : '-'}
                </div>
              </div>
              <div className="rounded-lg border border-kumo-line/70 bg-kumo-base px-3 py-2.5">
                <div className="text-[11px] text-kumo-subtle">结果 / 错误全文</div>
                <div
                  className={cx(
                    'mt-2 whitespace-pre-wrap break-words rounded-md px-3 py-2 text-sm',
                    registrationDetail.errorMessage
                      ? 'border border-kumo-danger/20 bg-kumo-danger/5 text-kumo-danger'
                      : 'border border-kumo-line/70 bg-kumo-recessed/10 text-kumo-strong'
                  )}
                >
                  {getRegistrationResultText(registrationDetail)}
                </div>
              </div>
              <div className="flex justify-end">
                <Button size="sm" variant="secondary" onClick={() => setRegistrationDetail(null)}>
                  关闭
                </Button>
              </div>
            </div>
          ) : null}
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={showUserDialog} onOpenChange={setShowUserDialog}>
        <Dialog className="@container w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] p-5 cq-sm:w-full cq-sm:max-w-3xl">
          <div className="space-y-4">
            <Dialog.Title>{editingUser ? '编辑用户' : '新增用户'}</Dialog.Title>
            {loadingUserDialog ? (
              <div className="space-y-3">
                <SkeletonLine className="h-10 w-full" />
                <SkeletonLine className="h-10 w-full" />
                <SkeletonLine className="h-10 w-full" />
                <SkeletonLine className="h-40 w-full" />
              </div>
            ) : (
              <div className="grid gap-4 cq-lg:grid-cols-[minmax(0,1fr)_minmax(20rem,24rem)]">
                <div className="grid gap-3">
                  <Input
                    size="sm"
                    aria-label="显示名称"
                    value={userForm.displayName}
                    onChange={event =>
                      setUserForm(current => ({ ...current, displayName: event.target.value }))
                    }
                    placeholder="显示名称"
                  />
                  <div className="grid grid-cols-[minmax(0,1fr)_12rem] gap-2">
                    <Input
                      size="sm"
                      aria-label={editingUser ? '登录账号前缀' : '邮箱前缀'}
                      value={userForm.mailNickname}
                      onChange={event =>
                        setUserForm(current => ({ ...current, mailNickname: event.target.value }))
                      }
                      placeholder={editingUser ? '登录账号前缀' : '邮箱前缀'}
                    />
                    <Select
                      aria-label="邮箱后缀"
                      size="sm"
                      value={userForm.emailDomain}
                      onValueChange={value =>
                        setUserForm(current => ({ ...current, emailDomain: value }))
                      }
                      items={userEmailDomainItems}
                    />
                  </div>
                  <div className="text-xs text-kumo-subtle">
                    {!userForm.mailNickname.trim()
                      ? '显示名称留空时会自动使用邮箱前缀。'
                      : `登录账号预览：${userForm.mailNickname}${userForm.emailDomain ? `@${userForm.emailDomain}` : ''}`}
                  </div>
                  <div className="flex items-center gap-2">
                    <Input
                      size="sm"
                      aria-label={editingUser ? '重置密码' : '初始密码'}
                      value={userForm.password}
                      onChange={event =>
                        setUserForm(current => ({ ...current, password: event.target.value }))
                      }
                      placeholder={editingUser ? '留空则不修改密码' : '初始密码'}
                    />
                    <Button
                      size="sm"
                      variant="secondary"
                      icon={<RefreshCw className="h-3.5 w-3.5" />}
                      onClick={() =>
                        setUserForm(current => ({
                          ...current,
                          password: DEFAULT_NEW_USER_PASSWORD,
                        }))
                      }
                    >
                      预设
                    </Button>
                  </div>
                  {!editingUser ? (
                    <label className="flex items-center gap-2 text-xs text-kumo-subtle">
                      <Checkbox
                        checked={userForm.forceChangePasswordNextSignIn}
                        onCheckedChange={checked =>
                          setUserForm(current => ({
                            ...current,
                            forceChangePasswordNextSignIn: !!checked,
                          }))
                        }
                      />
                      下次登录时强制修改密码
                    </label>
                  ) : null}
                  <label className="flex items-center gap-2 text-xs text-kumo-subtle">
                    <Checkbox
                      checked={userForm.accountEnabled}
                      onCheckedChange={checked =>
                        setUserForm(current => ({ ...current, accountEnabled: !!checked }))
                      }
                    />
                    启用账号
                  </label>
                  {!editingUser ? (
                    <>
                      <Input
                        size="sm"
                        aria-label="部门"
                        value={userForm.department}
                        onChange={event =>
                          setUserForm(current => ({ ...current, department: event.target.value }))
                        }
                        placeholder="部门"
                      />
                      <Input
                        size="sm"
                        aria-label="职位"
                        value={userForm.jobTitle}
                        onChange={event =>
                          setUserForm(current => ({ ...current, jobTitle: event.target.value }))
                        }
                        placeholder="职位"
                      />
                      <Input
                        size="sm"
                        aria-label="办公地点"
                        value={userForm.officeLocation}
                        onChange={event =>
                          setUserForm(current => ({
                            ...current,
                            officeLocation: event.target.value,
                          }))
                        }
                        placeholder="办公地点"
                      />
                      <Input
                        size="sm"
                        aria-label="使用地区"
                        value={userForm.usageLocation}
                        onChange={event =>
                          setUserForm(current => ({
                            ...current,
                            usageLocation: event.target.value,
                          }))
                        }
                        placeholder="CN / US / HK"
                      />
                    </>
                  ) : null}
                </div>

                <div className="space-y-2 rounded-lg border border-kumo-line/80 bg-kumo-recessed/10 p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="text-sm font-medium text-kumo-strong">许可证</div>
                    <div className="text-xs text-kumo-subtle">
                      已选 {userDialogSkuIds.length} 项
                    </div>
                  </div>
                  <div className="text-xs text-kumo-subtle">
                    {editingUser
                      ? '可直接勾选，保存时会一并更新许可证。'
                      : '新增用户后会自动分配已勾选许可证。'}
                  </div>
                  {skus.length === 0 ? (
                    <div className="text-xs text-kumo-subtle">
                      暂无可选订阅
                    </div>
                  ) : (
                    <div className="max-h-80 overflow-auto pr-1 scrollbar-thin">
                      <div className="grid gap-1">
                        {skus.map(sku => {
                          const normalizedId = String(sku.skuId);
                          const checked = userDialogSkuIds.includes(normalizedId);
                          return (
                            <label
                              key={sku.skuId}
                              className="flex min-w-0 items-center gap-2 rounded border border-transparent px-2 py-1.5 hover:border-kumo-line hover:bg-kumo-base/60"
                            >
                              <Checkbox
                                checked={checked}
                                onCheckedChange={value => {
                                  setUserDialogSkuIds(current =>
                                    value
                                      ? current.includes(normalizedId)
                                        ? current
                                        : [...current, normalizedId]
                                      : current.filter(item => item !== normalizedId)
                                  );
                                }}
                                aria-label={`选择 ${getSkuDisplayLabel(sku.skuPartNumber, sku.skuId)}`}
                              />
                              <span className="min-w-0 flex-1 truncate text-xs text-kumo-strong">
                                {getSkuDisplayLabel(sku.skuPartNumber, sku.skuId)}
                              </span>
                            </label>
                          );
                        })}
                      </div>
                    </div>
                  )}
                </div>
              </div>
            )}
            <div className="flex justify-end gap-2">
              <Button size="sm" variant="secondary" onClick={() => setShowUserDialog(false)}>
                取消
              </Button>
              <Button
                size="sm"
                variant="primary"
                onClick={submitUser}
                disabled={submittingUser || loadingUserDialog || assigningLicense}
              >
                {submittingUser || assigningLicense ? '保存中...' : '保存'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={showGroupDialog} onOpenChange={setShowGroupDialog}>
        <Dialog size="sm" className="p-5">
          <div className="space-y-4">
            <Dialog.Title>新建组</Dialog.Title>
            <div className="grid gap-3">
              <Input
                size="sm"
                aria-label="组名称"
                value={groupForm.displayName}
                onChange={event =>
                  setGroupForm(current => ({ ...current, displayName: event.target.value }))
                }
                placeholder="组名称"
              />
              <Input
                size="sm"
                aria-label="邮件别名"
                value={groupForm.mailNickname}
                onChange={event =>
                  setGroupForm(current => ({ ...current, mailNickname: event.target.value }))
                }
                placeholder="mailNickname"
              />
            </div>
            <div className="flex justify-end gap-2">
              <Button size="sm" variant="secondary" onClick={() => setShowGroupDialog(false)}>
                取消
              </Button>
              <Button size="sm" variant="primary" onClick={submitGroup} disabled={submittingGroup}>
                {submittingGroup ? '创建中...' : '创建'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={showPublicPageDialog} onOpenChange={setShowPublicPageDialog}>
        <Dialog className="@container w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] p-5 cq-sm:w-full cq-sm:max-w-3xl">
          <div className="space-y-4">
            <Dialog.Title>{publicPageForm.id ? '编辑公开页' : '新建公开页'}</Dialog.Title>
            <div className="grid gap-4 cq-lg:grid-cols-[minmax(0,1fr)_minmax(18rem,22rem)]">
              <div className="grid gap-3">
                <Input
                  size="sm"
                  aria-label="公开页名称"
                  value={publicPageForm.name}
                  onChange={event =>
                    setPublicPageForm(current => ({ ...current, name: event.target.value }))
                  }
                  placeholder="例如：学生自助开通"
                />
                <div className="rounded-lg border border-kumo-line/80 bg-kumo-recessed/10 p-3">
                  <div className="text-sm font-medium text-kumo-strong">目标租户与域名</div>
                  <div className="text-xs text-kumo-subtle">
                    先勾选租户，再展开域名做选择。未取消的域名都会允许注册。
                  </div>
                  <div className="mt-3 grid gap-2">
                    {accounts.map(account => {
                      const normalizedId = String(account.id);
                      const checked = publicPageForm.accountIds.includes(normalizedId);
                      const accountDomains = getPublicAccountDomainList(account);
                      const selectedCount = accountDomains.filter(domain =>
                        publicPageForm.domains.includes(domain)
                      ).length;
                      return (
                        <div
                          key={account.id}
                          className="rounded-lg border border-kumo-line/70 bg-kumo-base/50 px-3 py-2.5"
                        >
                          <label className="flex min-w-0 items-center gap-2">
                            <Checkbox
                              checked={checked}
                              onCheckedChange={value => {
                                setPublicPageForm(current => {
                                  const nextAccountIds = value
                                    ? current.accountIds.includes(normalizedId)
                                      ? current.accountIds
                                      : [...current.accountIds, normalizedId]
                                    : current.accountIds.filter(item => item !== normalizedId);
                                  const domainSet = new Set(current.domains);
                                  if (value) {
                                    accountDomains.forEach(domain => domainSet.add(domain));
                                  } else {
                                    accountDomains.forEach(domain => domainSet.delete(domain));
                                  }
                                  return {
                                    ...current,
                                    accountIds: nextAccountIds,
                                    domains: Array.from(domainSet).sort((a, b) =>
                                      a.localeCompare(b)
                                    ),
                                  };
                                });
                              }}
                            />
                            <ChevronDown
                              className={cx(
                                'h-3.5 w-3.5 text-kumo-subtle transition',
                                checked ? 'rotate-0' : '-rotate-90'
                              )}
                            />
                            <div className="min-w-0 flex-1">
                              <div className="truncate text-xs font-medium text-kumo-strong">
                                {account.name}
                              </div>
                              <div className="mt-0.5 truncate text-[11px] text-kumo-subtle">
                                默认 @{account.defaultDomain || '-'}
                                {accountDomains.length > 0
                                  ? `，共 ${accountDomains.length} 个域名`
                                  : ''}
                              </div>
                            </div>
                            <span className="shrink-0 text-[11px] text-kumo-subtle">
                              {checked
                                ? `已选 ${selectedCount}/${accountDomains.length || 0}`
                                : '未启用'}
                            </span>
                          </label>

                          {checked ? (
                            <div className="mt-2 border-t border-kumo-line/60 pt-2">
                              {accountDomains.length === 0 ? (
                                <div className="text-[11px] text-kumo-subtle">
                                  当前租户未读取到可选域名，请先校验租户连接。
                                </div>
                              ) : (
                                <div className="grid gap-1">
                                  {accountDomains.map(domain => {
                                    const domainChecked = publicPageForm.domains.includes(domain);
                                    return (
                                      <label
                                        key={domain}
                                        className="flex min-w-0 items-center gap-2 rounded px-2 py-1 hover:bg-kumo-recessed/20"
                                      >
                                        <Checkbox
                                          checked={domainChecked}
                                          onCheckedChange={value => {
                                            setPublicPageForm(current => ({
                                              ...current,
                                              domains: value
                                                ? Array.from(
                                                    new Set([...current.domains, domain])
                                                  ).sort((a, b) => a.localeCompare(b))
                                                : current.domains.filter(item => item !== domain),
                                            }));
                                          }}
                                        />
                                        <span className="min-w-0 flex-1 truncate text-xs text-kumo-strong">
                                          @{domain}
                                        </span>
                                      </label>
                                    );
                                  })}
                                </div>
                              )}
                            </div>
                          ) : null}
                        </div>
                      );
                    })}
                  </div>
                </div>
                <label className="flex items-center gap-2 text-xs text-kumo-subtle">
                  <Checkbox
                    checked={publicPageForm.enabled}
                    onCheckedChange={checked =>
                      setPublicPageForm(current => ({ ...current, enabled: !!checked }))
                    }
                  />
                  启用公开页
                </label>
                <label className="flex items-center gap-2 text-xs text-kumo-subtle">
                  <Checkbox
                    checked={publicPageForm.forceChangePasswordNextSignIn}
                    onCheckedChange={checked =>
                      setPublicPageForm(current => ({
                        ...current,
                        forceChangePasswordNextSignIn: !!checked,
                      }))
                    }
                  />
                  首次登录强制修改密码
                </label>
              </div>

              <div className="space-y-2 rounded-lg border border-kumo-line/80 bg-kumo-recessed/10 p-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="text-sm font-medium text-kumo-strong">许可证模板</div>
                  <div className="text-xs text-kumo-subtle">
                    已选 {publicPageForm.skuIds.length} 项
                  </div>
                </div>
                <div className="text-xs text-kumo-subtle">
                  新注册账号会分配下方许可证。
                </div>
                <div className="rounded-lg border border-kumo-line/70 bg-kumo-base/60 px-3 py-2 text-xs text-kumo-subtle">
                  保存后请到“邀请码”页面单独生成注册链接，每次最多生成 5 个一次性邀请码。
                </div>
                {publicPageForm.accountIds.length === 0 ? (
                  <div className="text-xs text-kumo-subtle">
                    先勾选一个租户，再读取该租户的许可证模板。
                  </div>
                ) : skus.length === 0 ? (
                  <div className="text-xs text-kumo-subtle">暂无可选订阅</div>
                ) : (
                  <div className="max-h-80 overflow-auto pr-1 scrollbar-thin">
                    <div className="grid gap-1">
                      {skus.map(sku => {
                        const normalizedId = String(sku.skuId);
                        const checked = publicPageForm.skuIds.includes(normalizedId);
                        return (
                          <label
                            key={sku.skuId}
                            className="flex min-w-0 items-center gap-2 rounded border border-transparent px-2 py-1.5 hover:border-kumo-line hover:bg-kumo-base/60"
                          >
                            <Checkbox
                              checked={checked}
                              onCheckedChange={value => {
                                setPublicPageForm(current => ({
                                  ...current,
                                  skuIds: value
                                    ? current.skuIds.includes(normalizedId)
                                      ? current.skuIds
                                      : [...current.skuIds, normalizedId]
                                    : current.skuIds.filter(item => item !== normalizedId),
                                }));
                              }}
                            />
                            <span className="min-w-0 flex-1 truncate text-xs text-kumo-strong">
                              {getSkuDisplayLabel(sku.skuPartNumber, sku.skuId)}
                            </span>
                          </label>
                        );
                      })}
                    </div>
                  </div>
                )}
              </div>
            </div>
            <div className="flex justify-end gap-2">
              <Button size="sm" variant="secondary" onClick={() => setShowPublicPageDialog(false)}>
                取消
              </Button>
              <Button
                size="sm"
                variant="primary"
                onClick={submitPublicPage}
                disabled={submittingPublicPage}
              >
                {submittingPublicPage ? '保存中...' : '保存'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={showInviteCodeDialog} onOpenChange={setShowInviteCodeDialog}>
        <Dialog size="sm" className="p-5">
          <div className="space-y-4">
            <Dialog.Title>生成邀请码</Dialog.Title>
            <div className="grid gap-3">
              <Select
                aria-label="公开页"
                size="sm"
                value={inviteCodeGeneratorForm.publicPageId}
                onValueChange={value =>
                  setInviteCodeGeneratorForm(current => ({ ...current, publicPageId: value }))
                }
                items={publicPages.map(page => ({ value: String(page.id), label: page.name }))}
              />
              <Input
                size="sm"
                type="number"
                min="1"
                max="5"
                aria-label="生成数量"
                value={inviteCodeGeneratorForm.quantity}
                onChange={event =>
                  setInviteCodeGeneratorForm(current => ({
                    ...current,
                    quantity: event.target.value,
                  }))
                }
                placeholder="1-5"
              />
              <div className="rounded-lg border border-kumo-line/70 bg-kumo-base/60 px-3 py-2 text-xs text-kumo-subtle">
                使用带 `code` 的注册链接，默认限 1 次。
              </div>
            </div>
            <div className="flex justify-end gap-2">
              <Button size="sm" variant="secondary" onClick={() => setShowInviteCodeDialog(false)}>
                取消
              </Button>
              <Button
                size="sm"
                variant="primary"
                onClick={generateInviteCodes}
                disabled={generatingInviteCodes}
              >
                {generatingInviteCodes ? '生成中...' : '生成'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={showPermissionDialog} onOpenChange={setShowPermissionDialog}>
        <Dialog className="@container w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] p-5 cq-sm:w-full cq-sm:max-w-3xl">
          <div className="space-y-4">
            <div className="space-y-1">
              <Dialog.Title>Graph 权限说明</Dialog.Title>
            </div>

            <div className="flex items-center justify-between gap-3">
              <div className="text-xs text-kumo-subtle">
                {selectedAccount ? `当前租户：${selectedAccount.name}` : '请先选择租户'}
              </div>
              <Button
                size="sm"
                variant="secondary"
                icon={
                  <Shield
                    className={`h-3.5 w-3.5 ${permissionCheckLoading ? 'animate-pulse' : ''}`}
                  />
                }
                onClick={detectPermissions}
                disabled={!selectedAccountId || permissionCheckLoading}
              >
                {permissionCheckLoading ? '检测中...' : '一键检测'}
              </Button>
            </div>

            {permissionCheckError ? (
              <AppCard className="border-kumo-danger/20 bg-kumo-danger/5">
                <div className="text-xs text-kumo-danger">{permissionCheckError}</div>
              </AppCard>
            ) : null}

            <div className="grid gap-3">
              {permissionItems.map(permission => (
                <AppCard key={permission.name} className="p-0">
                  <div className="flex items-center justify-between gap-3 px-4 py-3">
                    <div className="min-w-0">
                      <div className="flex min-w-0 items-center gap-2">
                        <div className="font-mono text-sm font-semibold text-kumo-strong">
                          {permission.name}
                        </div>
                        {permission.granted === true ? (
                          <StatusBadge tone="success">已具备</StatusBadge>
                        ) : null}
                        {permission.granted === false ? (
                          <StatusBadge tone="danger">缺失</StatusBadge>
                        ) : null}
                      </div>
                      <div className="mt-1 text-xs text-kumo-subtle">{permission.note}</div>
                    </div>
                    <Button
                      size="sm"
                      variant="secondary"
                      shape="square"
                      aria-label={`复制 ${permission.name}`}
                      title="复制权限名"
                      icon={<Copy className="h-3.5 w-3.5" />}
                      onClick={() => copyText(permission.name, `${permission.name} 已复制`)}
                    />
                  </div>
                </AppCard>
              ))}
            </div>

            <div className="flex justify-end gap-2">
              <Button size="sm" variant="secondary" onClick={() => setShowPermissionDialog(false)}>
                关闭
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
    </PageStack>
  );
}

export default M365Page;
