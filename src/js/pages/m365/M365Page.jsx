import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Tabs } from '@cloudflare/kumo';
import { dialog } from '../../modules/dialog.js';
import { toast } from '../../modules/toast.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import { PageStack, TabBarOverflowActions, stickyTabsBaseClass } from '../../components/ui/AppPrimitives.jsx';
import { Cloud, Shield } from '../../components/Icons.jsx';
import {
  M365_REQUIRED_PERMISSIONS,
  defaultAccountForm,
  defaultAccountImportState,
  defaultGroupForm,
  defaultInviteCodeGeneratorForm,
  defaultPublicPageForm,
  defaultUserForm,
  DEFAULT_NEW_USER_PASSWORD,
  workspaceHeightClass,
} from './constants.js';
import { M365_TABS } from './tabs.jsx';
import {
  downloadJson,
  extractOrganizationDomains,
  getAccountDomainList,
  getDisplayText,
  getDomainFromPrincipalName,
  getFriendlyErrorMessage,
  getPrincipalLocalPart,
  getSkuDisplayLabel,
  normalizeDomainValue,
  parseJsonInput,
  parseResponse,
  getAuthHeaders,
} from './utils.js';
import TenantsPanel from './TenantsPanel.jsx';
import UsersTab from './UsersTab.jsx';
import GroupsTab from './GroupsTab.jsx';
import PublicPagesTab from './PublicPagesTab.jsx';
import AccountDialog from './AccountDialog.jsx';
import AccountImportDialog from './AccountImportDialog.jsx';
import RegistrationDetailDialog from './RegistrationDetailDialog.jsx';
import UserDialog from './UserDialog.jsx';
import GroupDialog from './GroupDialog.jsx';
import PublicPageDialog from './PublicPageDialog.jsx';
import InviteCodeDialog from './InviteCodeDialog.jsx';
import PermissionDialog from './PermissionDialog.jsx';

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
    if (!confirmPress(`m365-user-delete:${user.id}`, `删除用户「${getDisplayText(user.displayName)}」`)) return;
    try {
      await requestJSON(`/api/m365/accounts/${selectedAccountId}/users/${user.id}`, {
        method: 'DELETE',
      });
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
    if (!confirmPress(`m365-invite-batch:${group.key}`, `删除邀请码批次（${ids.length} 个）`)) return;
    try {
      const payload = group.batchId
        ? { publicPageId: Number(group.publicPageId) || undefined, batchId: group.batchId }
        : { ids };
      const result = await requestJSON('/api/m365/invite-codes', {
        method: 'DELETE',
        body: JSON.stringify(payload),
      });
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

  return (
    <PageStack viewport className={workspaceHeightClass}>
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={M365_TABS}
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

      {activeTab === 'tenants' && (
        <TenantsPanel
          accounts={accounts}
          loadingAccounts={loadingAccounts}
          selectedAccountId={selectedAccountId}
          setSelectedAccountId={setSelectedAccountId}
          verifyingAccountId={verifyingAccountId}
          exportAccounts={exportAccounts}
          openImportAccounts={openImportAccounts}
          openCreateAccount={openCreateAccount}
          verifyAccount={verifyAccount}
          openEditAccount={openEditAccount}
          deleteAccount={deleteAccount}
          isArmed={isArmed}
        />
      )}
      {activeTab === 'users' && (
        <UsersTab
          selectedAccountId={selectedAccountId}
          skuLoading={skuLoading}
          skus={skus}
          loadSkus={loadSkus}
          userSearch={userSearch}
          setUserSearch={setUserSearch}
          loadUsers={loadUsers}
          openCreateUser={openCreateUser}
          usersLoading={usersLoading}
          users={users}
          skuLabelLookup={skuLabelLookup}
          selectedUserId={selectedUserId}
          setSelectedUserId={setSelectedUserId}
          togglingUserId={togglingUserId}
          toggleUserEnabled={toggleUserEnabled}
          openEditUser={openEditUser}
          deleteUser={deleteUser}
          isArmed={isArmed}
        />
      )}
      {activeTab === 'groups' && (
        <GroupsTab
          selectedAccountId={selectedAccountId}
          groupsLoading={groupsLoading}
          groups={groups}
          loadGroups={loadGroups}
          setShowGroupDialog={setShowGroupDialog}
          selectedGroupId={selectedGroupId}
          setSelectedGroupId={setSelectedGroupId}
          selectedGroup={selectedGroup}
          memberInput={memberInput}
          setMemberInput={setMemberInput}
          addGroupMember={addGroupMember}
          groupMembersLoading={groupMembersLoading}
          skuItems={skuItems}
          groupLicenseSkuId={groupLicenseSkuId}
          setGroupLicenseSkuId={setGroupLicenseSkuId}
          assignGroupLicense={assignGroupLicense}
          assigningGroupLicense={assigningGroupLicense}
          groupMembers={groupMembers}
          removeGroupMember={removeGroupMember}
          isArmed={isArmed}
        />
      )}
      {activeTab === 'public' && (
        <PublicPagesTab
          publicTab={publicTab}
          setPublicTab={setPublicTab}
          loadPublicPages={loadPublicPages}
          loadInviteCodes={loadInviteCodes}
          loadRegistrations={loadRegistrations}
          openCreatePublicPage={openCreatePublicPage}
          openInviteCodeGenerator={openInviteCodeGenerator}
          publicPagesLoading={publicPagesLoading}
          filteredPublicPages={filteredPublicPages}
          accountLookup={accountLookup}
          publicPageSkuLabelLookup={publicPageSkuLabelLookup}
          skuLabelLookup={skuLabelLookup}
          copyText={copyText}
          togglingPublicPageId={togglingPublicPageId}
          togglePublicPageEnabled={togglePublicPageEnabled}
          openEditPublicPage={openEditPublicPage}
          deletePublicPage={deletePublicPage}
          isArmed={isArmed}
          inviteCodesLoading={inviteCodesLoading}
          groupedInviteCodeBatches={groupedInviteCodeBatches}
          deleteInviteBatch={deleteInviteBatch}
          selectedRegistrationIds={selectedRegistrationIds}
          deletingRegistrations={deletingRegistrations}
          deleteSelectedRegistrations={deleteSelectedRegistrations}
          registrationsLoading={registrationsLoading}
          filteredRegistrations={filteredRegistrations}
          selectedRegistrationRecords={selectedRegistrationRecords}
          setSelectedRegistrationIds={setSelectedRegistrationIds}
          toggleRegistrationSelection={toggleRegistrationSelection}
          setRegistrationDetail={setRegistrationDetail}
        />
      )}

      <AccountDialog
        open={showAccountDialog}
        onOpenChange={setShowAccountDialog}
        editingAccount={editingAccount}
        accountForm={accountForm}
        setAccountForm={setAccountForm}
        submitAccount={submitAccount}
        submittingAccount={submittingAccount}
      />

      <AccountImportDialog
        open={showAccountImportDialog}
        onOpenChange={setShowAccountImportDialog}
        accountImportInputRef={accountImportInputRef}
        importAccountsFromFile={importAccountsFromFile}
        accountImportState={accountImportState}
        setAccountImportState={setAccountImportState}
        submitImportAccounts={submitImportAccounts}
        importingAccounts={importingAccounts}
      />

      <RegistrationDetailDialog
        registrationDetail={registrationDetail}
        setRegistrationDetail={setRegistrationDetail}
      />

      <UserDialog
        open={showUserDialog}
        onOpenChange={setShowUserDialog}
        editingUser={editingUser}
        loadingUserDialog={loadingUserDialog}
        userForm={userForm}
        setUserForm={setUserForm}
        userEmailDomainItems={userEmailDomainItems}
        skus={skus}
        userDialogSkuIds={userDialogSkuIds}
        setUserDialogSkuIds={setUserDialogSkuIds}
        submitUser={submitUser}
        submittingUser={submittingUser}
        assigningLicense={assigningLicense}
      />

      <GroupDialog
        open={showGroupDialog}
        onOpenChange={setShowGroupDialog}
        groupForm={groupForm}
        setGroupForm={setGroupForm}
        submitGroup={submitGroup}
        submittingGroup={submittingGroup}
      />

      <PublicPageDialog
        open={showPublicPageDialog}
        onOpenChange={setShowPublicPageDialog}
        publicPageForm={publicPageForm}
        setPublicPageForm={setPublicPageForm}
        accounts={accounts}
        getPublicAccountDomainList={getPublicAccountDomainList}
        skus={skus}
        submitPublicPage={submitPublicPage}
        submittingPublicPage={submittingPublicPage}
      />

      <InviteCodeDialog
        open={showInviteCodeDialog}
        onOpenChange={setShowInviteCodeDialog}
        inviteCodeGeneratorForm={inviteCodeGeneratorForm}
        setInviteCodeGeneratorForm={setInviteCodeGeneratorForm}
        publicPages={publicPages}
        generateInviteCodes={generateInviteCodes}
        generatingInviteCodes={generatingInviteCodes}
      />

      <PermissionDialog
        open={showPermissionDialog}
        onOpenChange={setShowPermissionDialog}
        selectedAccount={selectedAccount}
        selectedAccountId={selectedAccountId}
        permissionCheckLoading={permissionCheckLoading}
        detectPermissions={detectPermissions}
        permissionCheckError={permissionCheckError}
        permissionItems={permissionItems}
        copyText={copyText}
      />
    </PageStack>
  );
}

export default M365Page;
