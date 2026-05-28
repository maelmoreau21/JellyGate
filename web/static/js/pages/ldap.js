(() => {
    const config = window.JGPageLDAP || {};
    const i18n = config.i18n || {};
    const t = (key, fallback) => i18n[key] || fallback || key;
    const qs = (id) => document.getElementById(id);
    const value = (id) => (qs(id)?.value || '').trim();
    const checked = (id) => !!qs(id)?.checked;

    let currentLDAPConfig = {};
    let presets = [];
    let groupMappings = [];

    function setValue(id, nextValue) {
        const el = qs(id);
        if (el) el.value = nextValue == null ? '' : String(nextValue);
    }

    function setChecked(id, nextValue) {
        const el = qs(id);
        if (el) el.checked = !!nextValue;
    }

    function showStatus(id, message, type = 'info') {
        const box = qs(id);
        if (!box) return;
        box.classList.remove('hidden', 'border-emerald-500/40', 'bg-emerald-500/10', 'text-emerald-200', 'border-red-500/40', 'bg-red-500/10', 'text-red-200', 'border-sky-500/40', 'bg-sky-500/10', 'text-sky-200');
        if (type === 'success') {
            box.classList.add('border-emerald-500/40', 'bg-emerald-500/10', 'text-emerald-200');
        } else if (type === 'error') {
            box.classList.add('border-red-500/40', 'bg-red-500/10', 'text-red-200');
        } else {
            box.classList.add('border-sky-500/40', 'bg-sky-500/10', 'text-sky-200');
        }
        box.textContent = message;
    }

    function showLDAPTestResult(message, type = 'info') {
        showStatus('ldap-test-result', message, type);
    }

    function showMappingStatus(message, type = 'info') {
        showStatus('ldap-mappings-result', message, type);
    }

    function toggleLDAPFields() {
        const fields = qs('ldap-fields');
        if (!fields) return;
        const enabled = checked('ldap-enabled');
        fields.style.opacity = enabled ? '1' : '0.35';
        fields.style.pointerEvents = enabled ? 'auto' : 'none';
    }

    function collectLDAPPayload() {
        const payload = {
            ...currentLDAPConfig,
            enabled: checked('ldap-enabled'),
            host: value('ldap-host'),
            port: Number.parseInt(qs('ldap-port')?.value || '636', 10) || 636,
            use_tls: checked('ldap-tls'),
            skip_verify: checked('ldap-skip-verify'),
            bind_dn: value('ldap-bind-dn'),
            bind_password: qs('ldap-bind-password')?.value || '',
            base_dn: value('ldap-base-dn'),
            search_filter: value('ldap-search-filter'),
            search_attributes: value('ldap-search-attributes'),
            uid_attribute: value('ldap-uid-attribute'),
            username_attribute: value('ldap-username-attribute'),
            admin_filter: value('ldap-admin-filter'),
            admin_filter_memberuid: checked('ldap-admin-filter-memberuid'),
            user_object_class: value('ldap-user-object-class'),
            group_member_attr: value('ldap-group-member-attr'),
            user_ou: value('ldap-user-ou'),
            domain: value('ldap-domain'),
            provision_mode: qs('ldap-provision-mode')?.value || 'hybrid',
            jellyfin_ldap_auth_provider_id: value('ldap-jf-auth-provider'),
            jellyfin_ldap_password_reset_provider_id: value('ldap-jf-reset-provider'),
        };

        if (!payload.provision_mode) payload.provision_mode = 'hybrid';
        if (!payload.user_ou) payload.user_ou = 'CN=Users';
        if (!payload.user_object_class) payload.user_object_class = 'auto';
        if (!payload.group_member_attr) payload.group_member_attr = 'auto';
        if (!payload.search_attributes) payload.search_attributes = 'uid,sAMAccountName,cn,userPrincipalName,mail';
        if (!payload.uid_attribute) payload.uid_attribute = 'uid';
        if (!payload.username_attribute) payload.username_attribute = 'auto';
        if (!payload.jellyfin_ldap_auth_provider_id) payload.jellyfin_ldap_auth_provider_id = 'Jellyfin.Plugin.LDAP_Auth.LdapAuthenticationProviderPlugin';
        if (!payload.jellyfin_ldap_password_reset_provider_id) payload.jellyfin_ldap_password_reset_provider_id = 'Jellyfin.Plugin.LDAP_Auth.LdapPasswordResetProvider';
        return payload;
    }

    function applyLDAPConfig(ldap) {
        currentLDAPConfig = { ...(ldap || {}) };
        setChecked('ldap-enabled', ldap?.enabled || false);
        setValue('ldap-host', ldap?.host || '');
        setValue('ldap-port', ldap?.port || 636);
        setChecked('ldap-tls', ldap?.use_tls !== false);
        setChecked('ldap-skip-verify', ldap?.skip_verify || false);
        setValue('ldap-bind-dn', ldap?.bind_dn || '');
        setValue('ldap-bind-password', ldap?.bind_password || '');
        setValue('ldap-base-dn', ldap?.base_dn || '');
        setValue('ldap-search-filter', ldap?.search_filter || '');
        setValue('ldap-search-attributes', ldap?.search_attributes || 'uid,sAMAccountName,cn,userPrincipalName,mail');
        setValue('ldap-uid-attribute', ldap?.uid_attribute || 'uid');
        setValue('ldap-username-attribute', ldap?.username_attribute || 'auto');
        setValue('ldap-admin-filter', ldap?.admin_filter || '');
        setChecked('ldap-admin-filter-memberuid', ldap?.admin_filter_memberuid || false);
        setValue('ldap-user-object-class', ldap?.user_object_class || 'auto');
        setValue('ldap-group-member-attr', ldap?.group_member_attr || 'auto');
        setValue('ldap-user-ou', ldap?.user_ou || 'CN=Users');
        setValue('ldap-domain', ldap?.domain || '');
        setValue('ldap-provision-mode', ldap?.provision_mode || 'hybrid');
        setValue('ldap-jf-auth-provider', ldap?.jellyfin_ldap_auth_provider_id || 'Jellyfin.Plugin.LDAP_Auth.LdapAuthenticationProviderPlugin');
        setValue('ldap-jf-reset-provider', ldap?.jellyfin_ldap_password_reset_provider_id || 'Jellyfin.Plugin.LDAP_Auth.LdapPasswordResetProvider');
        toggleLDAPFields();
    }

    async function loadLDAPConfig() {
        const res = await JG.api('/admin/api/settings');
        if (!res || !res.success) {
            JG.toast((res && res.message) || t('settings_save_error', 'Unable to load settings.'), 'error');
            return;
        }
        applyLDAPConfig(res.data?.ldap || {});
    }

    function presetName(preset) {
        return String(preset?.name || preset?.id || t('defaultPresetName', 'Profile')).trim();
    }

    function isLDAPMapping(mapping) {
        return String(mapping?.source || '').trim().toLowerCase() === 'ldap';
    }

    function isInviterMapping(mapping) {
        const name = String(mapping?.group_name || '').trim().toLowerCase();
        return name.includes('parrain') || name.includes('inviter') || name.includes('sponsor');
    }

    function resolvePresetLDAPGroups(preset) {
        const presetID = String(preset?.id || '').trim().toLowerCase();
        const result = { users: '', inviter: '' };
        if (!presetID) return result;

        groupMappings
            .filter((mapping) => isLDAPMapping(mapping) && String(mapping.policy_preset_id || '').trim().toLowerCase() === presetID)
            .forEach((mapping) => {
                const groupDN = String(mapping.ldap_group_dn || '').trim();
                if (!groupDN) return;
                if (!result.inviter && isInviterMapping(mapping)) {
                    result.inviter = groupDN;
                    return;
                }
                if (!result.users) {
                    result.users = groupDN;
                    return;
                }
                if (!result.inviter) result.inviter = groupDN;
            });

        const legacyGroups = Array.isArray(preset?.ldap_groups) ? preset.ldap_groups.map((entry) => String(entry || '').trim()).filter(Boolean) : [];
        if (!result.users && legacyGroups[0]) result.users = legacyGroups[0];
        if (!result.inviter && legacyGroups[1]) result.inviter = legacyGroups[1];
        return result;
    }

    function renderMappingRows() {
        const tbody = qs('ldap-profile-mappings-body');
        if (!tbody) return;
        if (!presets.length) {
            tbody.innerHTML = `<tr><td colspan="3" class="px-4 py-8 text-center text-jg-text-muted">${JG.esc(t('noPresets', 'No profiles.'))}</td></tr>`;
            return;
        }
        tbody.innerHTML = presets.map((preset, index) => {
            const groups = resolvePresetLDAPGroups(preset);
            return `<tr data-preset-id="${JG.esc(preset.id || '')}" data-preset-index="${index}" class="border-b border-jg-border last:border-none">
                <td class="px-4 py-3 min-w-[150px]">
                    <div class="font-bold text-jg-text">${JG.esc(presetName(preset))}</div>
                    <code class="text-[10px] text-jg-text-muted">${JG.esc(preset.id || '')}</code>
                </td>
                <td class="px-4 py-3 min-w-[260px]">
                    <input class="jg-input h-10 bg-black/20 text-sm ldap-map-users" value="${JG.esc(groups.users)}" placeholder="CN=jellyfin-users,OU=Groups,DC=example,DC=com">
                </td>
                <td class="px-4 py-3 min-w-[260px]">
                    <input class="jg-input h-10 bg-black/20 text-sm ldap-map-inviter" value="${JG.esc(groups.inviter)}" placeholder="CN=jellyfin-inviters,OU=Groups,DC=example,DC=com">
                </td>
            </tr>`;
        }).join('');
    }

    async function loadMappingData() {
        const [presetsRes, mappingsRes] = await Promise.all([
            JG.api('/admin/api/automation/presets'),
            JG.api('/admin/api/automation/group-mappings'),
        ]);
        if (!presetsRes || !presetsRes.success) {
            JG.toast((presetsRes && presetsRes.message) || t('errorPresets', 'Unable to load profiles.'), 'error');
        }
        if (!mappingsRes || !mappingsRes.success) {
            JG.toast((mappingsRes && mappingsRes.message) || t('errorGroupMappings', 'Unable to load group mappings.'), 'error');
        }
        presets = Array.isArray(presetsRes?.data) ? presetsRes.data : [];
        groupMappings = Array.isArray(mappingsRes?.data) ? mappingsRes.data : [];
        renderMappingRows();
    }

    function pushMapping(target, seen, mapping) {
        const groupName = String(mapping.group_name || '').trim();
        const source = String(mapping.source || 'internal').trim().toLowerCase();
        const ldapGroupDN = String(mapping.ldap_group_dn || '').trim();
        const policyPresetID = String(mapping.policy_preset_id || '').trim().toLowerCase();
        if (!groupName || !policyPresetID) return;
        const key = `${source}|${ldapGroupDN.toLowerCase()}|${policyPresetID}|${groupName.toLowerCase()}`;
        if (seen.has(key)) return;
        seen.add(key);
        target.push({
            group_name: groupName,
            source,
            ldap_group_dn: ldapGroupDN,
            policy_preset_id: policyPresetID,
            priority: Number.parseInt(mapping.priority || '0', 10) || 0,
        });
    }

    function collectMappingPayload() {
        const payload = [];
        const seen = new Set();
        const validPresetIDs = new Set(presets.map((preset) => String(preset.id || '').trim().toLowerCase()).filter(Boolean));

        groupMappings.forEach((mapping) => {
            const presetID = String(mapping.policy_preset_id || '').trim().toLowerCase();
            if (!validPresetIDs.has(presetID) || isLDAPMapping(mapping)) return;
            pushMapping(payload, seen, mapping);
        });

        presets.forEach((preset) => {
            const presetID = String(preset.id || '').trim().toLowerCase();
            if (!presetID) return;
            pushMapping(payload, seen, {
                group_name: presetName(preset),
                source: 'internal',
                ldap_group_dn: '',
                policy_preset_id: presetID,
            });
        });

        document.querySelectorAll('#ldap-profile-mappings-body tr[data-preset-id]').forEach((row) => {
            const index = Number.parseInt(row.dataset.presetIndex || '0', 10) || 0;
            const preset = presets[index] || {};
            const presetID = String(row.dataset.presetId || preset.id || '').trim().toLowerCase();
            const label = presetName(preset);
            const usersDN = String(row.querySelector('.ldap-map-users')?.value || '').trim();
            const inviterDN = String(row.querySelector('.ldap-map-inviter')?.value || '').trim();
            if (usersDN) {
                pushMapping(payload, seen, {
                    group_name: `${label} ${t('mappingLdapUsersSuffix', '(LDAP users)')}`,
                    source: 'ldap',
                    ldap_group_dn: usersDN,
                    policy_preset_id: presetID,
                });
            }
            if (inviterDN) {
                pushMapping(payload, seen, {
                    group_name: `${label} ${t('mappingLdapInviterSuffix', '(LDAP sponsorship)')}`,
                    source: 'ldap',
                    ldap_group_dn: inviterDN,
                    policy_preset_id: presetID,
                });
            }
        });
        return payload;
    }

    async function saveMappings(showToast = true) {
        const payload = collectMappingPayload();
        const res = await JG.api('/admin/api/automation/group-mappings', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
        if (!res || !res.success) {
            const message = (res && res.message) || t('saveMappingsFailed', 'Unable to save mappings.');
            showMappingStatus(message, 'error');
            if (showToast) JG.toast(message, 'error');
            return false;
        }
        groupMappings = payload;
        renderMappingRows();
        showMappingStatus(res.message || t('mappingsSaved', 'Mappings saved.'), 'success');
        if (showToast) JG.toast(res.message || t('mappingsSaved', 'Mappings saved.'), 'success');
        return true;
    }

    async function runLDAPTest(endpoint, payload) {
        showLDAPTestResult(t('ldap_test_running', 'Test in progress...'), 'info');
        const res = await JG.api(endpoint, {
            method: 'POST',
            body: JSON.stringify(payload),
        });
        if (res && res.success) {
            showLDAPTestResult(res.message || t('ldap_test_success', 'Test succeeded'), 'success');
            return;
        }
        showLDAPTestResult((res && res.message) || t('ldap_test_failed', 'Test failed'), 'error');
    }

    async function confirmLDAPDryRunBeforeSave(payload) {
        showLDAPTestResult('Dry-run LDAP en cours...', 'info');
        const res = await JG.api('/admin/api/settings/ldap/dry-run', {
            method: 'POST',
            body: JSON.stringify({
                ldap: payload,
                group_mappings: collectMappingPayload(),
                limit: 25,
            }),
        });
        if (!res || !res.success) {
            showLDAPTestResult((res && res.message) || 'Dry-run LDAP impossible', 'error');
            return false;
        }

        const summary = res.data?.summary || {};
        const message = `Dry-run LDAP: ${summary.would_create || 0} creation(s), ${summary.would_sync || 0} synchronisation(s), ${summary.blocking_conflicts || 0} conflit(s) bloquant(s).`;
        showLDAPTestResult(message, (summary.blocking_conflicts || 0) > 0 ? 'error' : 'success');
        if ((summary.blocking_conflicts || 0) <= 0) return true;

        if (typeof JG.confirm !== 'function') return false;
        return JG.confirm(
            'Conflits LDAP detectes',
            `${message} Sauvegarder quand meme la configuration LDAP ?`,
            { danger: true, confirmLabel: 'Sauvegarder quand meme' },
        );
    }

    async function saveLDAP(event) {
        event.preventDefault();
        const payload = collectLDAPPayload();
        if (payload.enabled) {
            const dryRunOK = await confirmLDAPDryRunBeforeSave(payload);
            if (!dryRunOK) return;
        }

        const res = await JG.api('/admin/api/settings/ldap', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
        if (!res || !res.success) {
            JG.toast((res && res.message) || t('settings_save_error', 'Unable to save settings.'), 'error');
            return;
        }

        const mappingsSaved = await saveMappings(false);
        if (!mappingsSaved) return;

        JG.toast(res.message || t('settings_saved', 'Settings saved.'), 'success');
        await loadLDAPConfig();
        await loadMappingData();
    }

    document.addEventListener('DOMContentLoaded', async () => {
        if (!qs('form-ldap')) return;

        qs('ldap-enabled')?.addEventListener('change', toggleLDAPFields);
        qs('form-ldap')?.addEventListener('submit', saveLDAP);
        qs('btn-ldap-mappings-save')?.addEventListener('click', () => saveMappings(true));

        qs('btn-ldap-test-conn')?.addEventListener('click', async () => {
            await runLDAPTest('/admin/api/settings/ldap/test-connection', collectLDAPPayload());
        });

        qs('btn-ldap-test-user')?.addEventListener('click', async () => {
            const username = value('ldap-test-username');
            if (!username) {
                showLDAPTestResult(t('ldap_test_username_required', 'Enter a test user identifier.'), 'error');
                return;
            }
            const payload = collectLDAPPayload();
            payload.username = username;
            await runLDAPTest('/admin/api/settings/ldap/test-user', payload);
        });

        qs('btn-ldap-test-jf')?.addEventListener('click', async () => {
            const username = value('ldap-test-username');
            const password = qs('ldap-test-password')?.value || '';
            if (!username || !password) {
                showLDAPTestResult(t('ldap_test_credentials_required', 'Enter username and password to validate Jellyfin LDAP plugin.'), 'error');
                return;
            }
            await runLDAPTest('/admin/api/settings/ldap/test-jellyfin-auth', { username, password });
        });

        await Promise.all([loadLDAPConfig(), loadMappingData()]);
    });
})();
