(() => {
    const config = window.JGPageSettings || {};
    const i18n = config.i18n || {};
    let backupDatabaseType = 'sqlite';
    let loadedEmailTemplates = {};
    let emailBaseDefaults = { header: '', footer: '' };
    let currentInvitationProfile = {};
    let currentLDAPConfig = {};
    let basePreviewTimer = null;
    let basePreviewRequestId = 0;
    const templateValidationTimers = new Map();
    const templateValidationRequestIds = new Map();

    function t(key, fallback) {
        return i18n[key] || fallback || key;
    }

    function emailTemplateKeyFromId(id) {
        return String(id || '').replace(/^tpl-/, '').replace(/-+/g, '_');
    }

    function getEmailVariableOptions() {
        return [
            { value: '{{.Username}}', label: t('settings_email_var_username', 'username') },
            { value: '{{.Email}}', label: t('settings_email_var_email', 'email address') },
            { value: '{{.InviteLink}}', label: t('settings_email_var_invite_link', 'invitation link') },
            { value: '{{.InviteURL}}', label: t('settings_email_var_invite_link', 'invitation link') },
            { value: '{{.InviteCode}}', label: t('settings_email_var_invite_code', 'invitation code') },
            { value: '{{.HelpURL}}', label: t('settings_email_var_help_url', 'support / onboarding URL') },
            { value: '{{.ResetLink}}', label: t('settings_email_var_reset_link', 'reset link') },
            { value: '{{.ResetURL}}', label: t('settings_email_var_reset_link', 'reset link') },
            { value: '{{.ResetCode}}', label: t('settings_email_var_reset_code', 'reset code') },
            { value: '{{.VerificationLink}}', label: t('settings_email_var_verification_link', 'email verification link') },
            { value: '{{.VerificationURL}}', label: t('settings_email_var_verification_link', 'email verification link') },
            { value: '{{.VerificationCode}}', label: t('settings_email_var_verification_code', 'email verification code') },
            { value: '{{.ExpiresIn}}', label: t('settings_email_var_expires_in', 'validity duration') },
            { value: '{{.ExpiryDate}}', label: t('settings_email_var_expiry_date', 'expiry date') },
            { value: '{{.JellyGateURL}}', label: t('settings_email_var_jellygate_url', 'public JellyGate URL') },
            { value: '{{.JellyfinURL}}', label: t('settings_email_var_jellyfin_url', 'Jellyfin login URL') },
            { value: '{{.JellyseerrURL}}', label: t('settings_email_var_jellyseerr_url', 'Jellyseerr URL') },
            { value: '{{.JellyTrackURL}}', label: t('settings_email_var_jellytrack_url', 'JellyTrack URL') },
            { value: '{{.Message}}', label: t('settings_email_var_message', 'custom message (admin invitation)') },
        ];
    }

    function insertTextAtCursor(field, text) {
        if (!field || !text) {
            return;
        }

        const start = Number.isInteger(field.selectionStart) ? field.selectionStart : field.value.length;
        const end = Number.isInteger(field.selectionEnd) ? field.selectionEnd : start;

        field.focus();
        if (typeof field.setRangeText === 'function') {
            field.setRangeText(text, start, end, 'end');
        } else {
            const value = field.value || '';
            field.value = `${value.slice(0, start)}${text}${value.slice(end)}`;
            const nextPos = start + text.length;
            field.setSelectionRange(nextPos, nextPos);
        }
        field.dispatchEvent(new Event('input', { bubbles: true }));
    }

    function buildVariablePicker(area) {
        const toolbar = document.createElement('div');
        toolbar.className = 'jg-variable-picker mt-3 flex items-center gap-2 max-w-xl group';

        const selectWrapper = document.createElement('div');
        selectWrapper.className = 'relative flex-1';

        const select = document.createElement('select');
        select.className = 'appearance-none jg-input w-full pr-10 text-xs h-9 bg-white/5 border-white/10 hover:border-jg-accent/40 transition-colors focus:ring-1 focus:ring-jg-accent/30';
        select.setAttribute('aria-label', t('settings_email_variable_picker_label', 'Variable to insert'));

        const empty = document.createElement('option');
        empty.value = '';
        empty.textContent = t('settings_email_variable_picker_placeholder', 'Choose a variable...');
        select.appendChild(empty);

        getEmailVariableOptions().forEach((item) => {
            const option = document.createElement('option');
            option.value = item.value;
            option.className = 'bg-jg-bg text-jg-text';
            option.textContent = `${item.value} — ${item.label}`;
            select.appendChild(option);
        });

        const arrow = document.createElement('div');
        arrow.className = 'absolute right-3 top-1/2 -translate-y-1/2 pointer-events-none text-jg-text-muted/50';
        arrow.innerHTML = '<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" /></svg>';

        selectWrapper.append(select, arrow);

        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'jg-btn jg-btn-sm h-9 px-4 bg-jg-accent/10 hover:bg-jg-accent/20 text-jg-accent border border-jg-accent/20 hover:border-jg-accent/40 transition-all font-bold text-[10px] uppercase tracking-wider whitespace-nowrap shadow-sm';
        button.innerHTML = `<span class="flex items-center gap-1.5"><svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" /></svg> ${t('settings_email_variable_insert', 'Insert')}</span>`;
        
        button.addEventListener('click', () => {
            if (!select.value) {
                return;
            }
            insertTextAtCursor(area, select.value);
            // Non-destructive flash effect
            area.classList.add('ring-2', 'ring-jg-accent/30');
            setTimeout(() => area.classList.remove('ring-2', 'ring-jg-accent/30'), 500);
            select.value = '';
        });

        toolbar.append(selectWrapper, button);
        return toolbar;
    }

    function getAllowedTemplateVariableNames() {
        const names = new Set();
        getEmailVariableOptions().forEach((item) => {
            const match = String(item.value || '').match(/^\{\{\.([A-Za-z0-9_]+)\}\}$/);
            if (match && match[1]) {
                names.add(match[1]);
            }
        });
        return names;
    }

    function extractTemplateVariableNames(content) {
        const source = String(content || '');
        const found = [];
        const regex = /\{\{\s*\.([A-Za-z0-9_]+)\s*\}\}/g;
        let match;
        while ((match = regex.exec(source)) !== null) {
            if (match[1]) {
                found.push(match[1]);
            }
        }
        return found;
    }

    function getTemplateValidationBox(area) {
        if (!area || !area.id) {
            return null;
        }
        const boxId = `${area.id}-validation`;
        let box = document.getElementById(boxId);
        if (!box) {
            box = document.createElement('div');
            box.id = boxId;
            box.className = 'mt-2 rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-[11px] text-jg-text-muted';
            area.insertAdjacentElement('afterend', box);
        }
        return box;
    }

    function renderTemplateValidationStatus(box, status, message) {
        if (!box) {
            return;
        }
        box.classList.remove('border-emerald-500/40', 'bg-emerald-500/10', 'text-emerald-200', 'border-amber-500/40', 'bg-amber-500/10', 'text-amber-200', 'border-rose-500/40', 'bg-rose-500/10', 'text-rose-200', 'border-sky-500/40', 'bg-sky-500/10', 'text-sky-200');
        if (status === 'ok') {
            box.classList.add('border-emerald-500/40', 'bg-emerald-500/10', 'text-emerald-200');
        } else if (status === 'warn') {
            box.classList.add('border-amber-500/40', 'bg-amber-500/10', 'text-amber-200');
        } else if (status === 'error') {
            box.classList.add('border-rose-500/40', 'bg-rose-500/10', 'text-rose-200');
        } else {
            box.classList.add('border-sky-500/40', 'bg-sky-500/10', 'text-sky-200');
        }
        box.textContent = message;
    }

    async function runLiveTemplateValidation(area) {
        if (!area || !area.id) {
            return;
        }
        const allowed = getAllowedTemplateVariableNames();
        const found = extractTemplateVariableNames(area.value || '');
        const unknown = [...new Set(found.filter((name) => !allowed.has(name)))];
        const box = getTemplateValidationBox(area);
        if (!box) {
            return;
        }

        if (!String(area.value || '').trim()) {
            renderTemplateValidationStatus(box, 'warn', t('template_empty', 'Template is empty.'));
            return;
        }

        if (unknown.length > 0) {
            const unknownPrefix = t('settings_template_variables_unknown', 'Unknown variables');
            renderTemplateValidationStatus(box, 'error', `${unknownPrefix}: ${unknown.join(', ')}`);
            return;
        }

        renderTemplateValidationStatus(box, 'info', t('preview_loading', 'Loading preview...'));
        const currentRequestId = (templateValidationRequestIds.get(area.id) || 0) + 1;
        templateValidationRequestIds.set(area.id, currentRequestId);

        const jellyfinURL = (document.getElementById('general-jellyfin-url')?.value || '').trim();
        const jellygateURL = (document.getElementById('general-jellygate-url')?.value || '').trim();
        const baseTemplateHeader = document.getElementById('tpl-base-header')?.value || '';
        const baseTemplateFooter = document.getElementById('tpl-base-footer')?.value || '';

        const res = await JG.api('/admin/api/settings/email-templates/preview', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                template: area.value || '',
                template_key: emailTemplateKeyFromId(area.id),
                base_template_header: baseTemplateHeader,
                base_template_footer: baseTemplateFooter,
                context: {
                    JellyfinURL: jellyfinURL || 'https://jellyfin.example.com',
                    JellyGateURL: jellygateURL || 'https://jellygate.example.com',
                    HelpURL: jellyfinURL || 'https://jellyfin.example.com',
                },
            }),
        });

        if (templateValidationRequestIds.get(area.id) !== currentRequestId) {
            return;
        }

        if (!res || !res.success) {
            renderTemplateValidationStatus(box, 'error', (res && res.message) || t('preview_impossible', 'Preview failed'));
            return;
        }

        if (found.length > 0) {
            renderTemplateValidationStatus(box, 'ok', t('settings_template_variables_valid', 'Valid variables ({count}) and syntax OK.').replace('{count}', String(found.length)));
            return;
        }
        renderTemplateValidationStatus(box, 'ok', t('settings_template_syntax_valid', 'Template syntax is valid.'));
    }

    function scheduleLiveTemplateValidation(area) {
        if (!area || !area.id) {
            return;
        }
        const previous = templateValidationTimers.get(area.id);
        if (previous) {
            clearTimeout(previous);
        }
        const timer = setTimeout(() => {
            templateValidationTimers.delete(area.id);
            runLiveTemplateValidation(area);
        }, 350);
        templateValidationTimers.set(area.id, timer);
    }

    function getBaseTemplatePreviewPayload() {
        const jellyfinURL = (document.getElementById('general-jellyfin-url')?.value || '').trim();
        const jellygateURL = (document.getElementById('general-jellygate-url')?.value || '').trim();
        return {
            template: t('email_base_preview_demo', 'You received an invitation to join the Jellyfin server.\n\nThe button and direct link appear automatically below this message.'),
            template_key: 'invitation',
            base_template_header: document.getElementById('tpl-base-header')?.value || '',
            base_template_footer: document.getElementById('tpl-base-footer')?.value || '',
            context: {
                JellyfinURL: jellyfinURL || 'https://jellyfin.example.com',
                JellyGateURL: jellygateURL || 'https://jellygate.example.com',
                HelpURL: jellyfinURL || 'https://jellyfin.example.com',
            },
        };
    }

    async function requestBasePreview(frame, { showLoading = false } = {}) {
        if (!frame) {
            return;
        }

        const requestId = ++basePreviewRequestId;
        if (showLoading) {
            frame.srcdoc = `<div style="font-family:Segoe UI,Arial,sans-serif;padding:24px;color:#0f172a;">${JG.esc(t('preview_loading', 'Loading preview...'))}</div>`;
        }

        const res = await JG.api('/admin/api/settings/email-templates/preview', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(getBaseTemplatePreviewPayload()),
        });

        if (requestId !== basePreviewRequestId) {
            return;
        }

        if (!res || !res.success || !res.data || typeof res.data.html !== 'string') {
            frame.srcdoc = `<div style="font-family:Segoe UI,Arial,sans-serif;padding:24px;color:#b91c1c;">${JG.esc((res && res.message) || t('preview_impossible', 'Preview failed'))}</div>`;
            return;
        }

        frame.srcdoc = String(res.data.html || '');
    }

    function scheduleBaseLivePreview() {
        const frame = document.getElementById('tpl-base-live-preview-frame');
        if (!frame) {
            return;
        }
        if (basePreviewTimer) {
            clearTimeout(basePreviewTimer);
        }
        basePreviewTimer = setTimeout(() => {
            basePreviewTimer = null;
            requestBasePreview(frame);
        }, 250);
    }

    function restoreDefaultBaseTemplate() {
        const header = document.getElementById('tpl-base-header');
        const footer = document.getElementById('tpl-base-footer');
        if (!header || !footer) {
            return;
        }

        header.value = emailBaseDefaults.header || '';
        footer.value = emailBaseDefaults.footer || '';
        scheduleBaseLivePreview();
    }

    function switchTab(name) {
        document.querySelectorAll('.tab-panel').forEach((panel) => panel.classList.add('hidden'));
        document.querySelectorAll('.tab-btn').forEach((button) => button.classList.remove('active'));
        document.getElementById(`panel-${name}`)?.classList.remove('hidden');
        document.getElementById(`tab-${name}`)?.classList.add('active');
    }

    function toggleLDAPFields() {
        const enabled = document.getElementById('ldap-enabled')?.checked;
        const fields = document.getElementById('ldap-fields');
        if (!fields) {
            return;
        }
        if (enabled) {
            fields.style.opacity = '1';
            fields.style.pointerEvents = 'auto';
        } else {
            fields.style.opacity = '0.35';
            fields.style.pointerEvents = 'none';
        }
    }

    function collectLDAPPayload() {
        const payload = {
            ...currentLDAPConfig,
            enabled: document.getElementById('ldap-enabled').checked,
            host: document.getElementById('ldap-host').value,
            port: parseInt(document.getElementById('ldap-port').value, 10) || 636,
            use_tls: document.getElementById('ldap-tls').checked,
            skip_verify: document.getElementById('ldap-skip-verify').checked,
            bind_dn: document.getElementById('ldap-bind-dn').value,
            bind_password: document.getElementById('ldap-bind-password').value,
            base_dn: document.getElementById('ldap-base-dn').value,
            search_filter: document.getElementById('ldap-search-filter').value.trim(),
            search_attributes: document.getElementById('ldap-search-attributes').value.trim(),
            uid_attribute: document.getElementById('ldap-uid-attribute').value.trim(),
            username_attribute: document.getElementById('ldap-username-attribute').value.trim(),
            admin_filter: document.getElementById('ldap-admin-filter').value.trim(),
            admin_filter_memberuid: !!document.getElementById('ldap-admin-filter-memberuid')?.checked,
        };

        if (!payload.provision_mode) payload.provision_mode = 'hybrid';
        if (!payload.user_ou) payload.user_ou = 'CN=Users';
        if (!payload.user_object_class) payload.user_object_class = 'auto';
        if (!payload.group_member_attr) payload.group_member_attr = 'auto';

        return payload;
    }

    function showLDAPTestResult(message, type = 'info') {
        const box = document.getElementById('ldap-test-result');
        if (!box) {
            return;
        }
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

    async function runLDAPTest(endpoint, payload) {
        showLDAPTestResult(t('ldap_test_running', 'Test in progress...'), 'info');
        const res = await JG.api(endpoint, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });

        if (res && res.success) {
            showLDAPTestResult(res.message || t('ldap_test_success', 'Test succeeded'), 'success');
            return;
        }
        showLDAPTestResult((res && res.message) || t('ldap_test_failed', 'Test failed'), 'error');
    }

    function refreshPortalShortcuts(links) {
        const mapping = [
            { id: 'shortcut-jellygate', url: links.jellygate_url || '' },
            { id: 'shortcut-jellyfin', url: links.jellyfin_url || '' },
            { id: 'shortcut-jellyseerr', url: links.jellyseerr_url || '' },
            { id: 'shortcut-jellytrack', url: links.jellytrack_url || '' },
        ];

        mapping.forEach((item) => {
            const el = document.getElementById(item.id);
            if (!el) {
                return;
            }
            const url = String(item.url || '').trim();
            if (!url) {
                el.classList.add('hidden');
                el.removeAttribute('href');
                return;
            }
            el.href = url;
            el.classList.remove('hidden');
        });
    }

    function setSelectOptions(selectId, options, emptyLabel = t('default_none', 'Default (none)')) {
        const select = document.getElementById(selectId);
        if (!select) {
            return;
        }

        const items = Array.isArray(options) ? options : [];
        let html = `<option value="">${JG.esc(emptyLabel)}</option>`;
        items.forEach((item) => {
            html += `<option value="${JG.esc(item.value)}">${JG.esc(item.label)}</option>`;
        });
        select.innerHTML = html;
    }

    async function loadInvitationProfileLookups() {
        const [presetsRes, usersRes] = await Promise.all([
            JG.api('/admin/api/automation/presets'),
            JG.api('/admin/api/users?limit=500&include_jellyfin=0'),
        ]);

        if (presetsRes && presetsRes.success && Array.isArray(presetsRes.data)) {
            const presetOptions = presetsRes.data.map((preset) => ({
                value: preset.id || '',
                label: preset.name || preset.id || t('preset_fallback', 'Preset'),
            }));
            setSelectOptions('invite-profile-preset', presetOptions);
        }

        const usersList = Array.isArray(usersRes?.data)
            ? usersRes.data
            : (Array.isArray(usersRes?.data?.users) ? usersRes.data.users : []);

        if (usersRes && usersRes.success && usersList.length > 0) {
            const userOptions = usersList
                .filter((user) => user && user.jellyfin_id)
                .map((user) => ({
                    value: user.jellyfin_id,
                    label: user.username || user.jellyfin_id,
                }));
            setSelectOptions('invite-profile-template-user', userOptions);
        }
    }

    function applyInvitationProfileConfig(cfg) {
        const profile = cfg || {};
        currentInvitationProfile = { ...profile };
        document.getElementById('invite-profile-require-email').checked = profile.require_email !== false;
        document.getElementById('invite-profile-require-email-verification').checked = profile.require_email_verification !== false;
        const policySelect = document.getElementById('invite-profile-email-verification-policy');
        if (policySelect) {
            const resolvedPolicy = String(profile.email_verification_policy || (profile.require_email_verification === false ? 'disabled' : 'required')).trim().toLowerCase();
            if (policySelect.querySelector(`option[value="${resolvedPolicy}"]`)) {
                policySelect.value = resolvedPolicy;
            } else {
                policySelect.value = 'required';
            }
        }
        document.getElementById('invite-profile-user-min').value = Number.isInteger(profile.username_min_length) ? profile.username_min_length : 3;
        document.getElementById('invite-profile-user-max').value = Number.isInteger(profile.username_max_length) ? profile.username_max_length : 32;
        document.getElementById('invite-profile-pw-min').value = Number.isInteger(profile.password_min_length) ? profile.password_min_length : 8;
        document.getElementById('invite-profile-pw-max').value = Number.isInteger(profile.password_max_length) ? profile.password_max_length : 128;
        document.getElementById('invite-profile-pw-upper').checked = !!profile.password_require_upper;
        document.getElementById('invite-profile-pw-lower').checked = !!profile.password_require_lower;
        document.getElementById('invite-profile-pw-digit').checked = !!profile.password_require_digit;
        document.getElementById('invite-profile-pw-special').checked = !!profile.password_require_special;
        syncInviteEmailPolicyControls();
        syncInviteEmailRequirementFromVerification();
    }

    function syncInviteEmailPolicyControls() {
        const requireEmail = document.getElementById('invite-profile-require-email');
        const requireVerification = document.getElementById('invite-profile-require-email-verification');
        const policySelect = document.getElementById('invite-profile-email-verification-policy');
        if (!requireEmail || !requireVerification || !policySelect) {
            return;
        }

        const mode = String(policySelect.value || '').trim().toLowerCase();
        if (mode === 'required' || mode === 'admin_bypass') {
            requireVerification.checked = true;
            requireEmail.checked = true;
        } else if (mode === 'conditional') {
            requireEmail.checked = true;
        } else if (mode === 'disabled') {
            requireVerification.checked = false;
        }
    }

    function syncInviteEmailRequirementFromVerification() {
        const requireEmail = document.getElementById('invite-profile-require-email');
        const requireVerification = document.getElementById('invite-profile-require-email-verification');
        const policySelect = document.getElementById('invite-profile-email-verification-policy');
        if (!requireEmail || !requireVerification) {
            return;
        }
        if (requireVerification.checked) {
            requireEmail.checked = true;
            if (policySelect && policySelect.value === 'disabled') {
                policySelect.value = 'required';
            }
            return;
        }
        if (policySelect && policySelect.value === 'required') {
            policySelect.value = 'disabled';
        }
    }

    function setBackupMode(databaseType) {
        const normalized = String(databaseType || '').trim().toLowerCase() || 'sqlite';
        backupDatabaseType = normalized;
        const isSQLite = normalized === 'sqlite';

        const note = document.getElementById('backup-db-note');

        if (note) {
            if (isSQLite) {
                note.classList.add('hidden');
            } else {
                note.classList.remove('hidden');
                note.innerHTML = [
                    `<div class="font-semibold mb-1">${JG.esc(t('backup_pg_mode_title', 'PostgreSQL mode detected'))}</div>`,
                    `<div>${JG.esc(t('backup_pg_mode_desc_1', 'Backups and restore are available in PostgreSQL mode.'))}</div>`,
                    `<div class="mt-2">${JG.esc(t('backup_pg_mode_desc_2', 'In Docker mode, pg_dump and psql are included in the JellyGate image. Rebuild the image if PostgreSQL major versions do not match. Outside Docker, keep pg_dump and psql installed on the host.'))}</div>`,
                ].join('');
            }
        }
    }

    async function loadSettings() {
        const res = await JG.api('/admin/api/settings');
        if (!res || !res.success) {
            return;
        }
        const data = res.data;
        setBackupMode(data.database_type || 'sqlite');

        document.getElementById('default-lang').value = data.default_lang || 'fr';
        const links = data.portal_links || {};
        document.getElementById('general-jellygate-url').value = links.jellygate_url || '';
        document.getElementById('general-jellyfin-url').value = links.jellyfin_url || '';
        document.getElementById('general-jellyseerr-url').value = links.jellyseerr_url || '';
        document.getElementById('general-jellytrack-url').value = links.jellytrack_url || '';
        refreshPortalShortcuts(links);

        applyInvitationProfileConfig(data.invitation_profile || {});

        currentLDAPConfig = { ...(data.ldap || {}) };
        document.getElementById('ldap-enabled').checked = data.ldap.enabled || false;
        document.getElementById('ldap-host').value = data.ldap.host || '';
        document.getElementById('ldap-port').value = data.ldap.port || 636;
        document.getElementById('ldap-tls').checked = data.ldap.use_tls !== false;
        document.getElementById('ldap-skip-verify').checked = data.ldap.skip_verify || false;
        document.getElementById('ldap-bind-dn').value = data.ldap.bind_dn || '';
        document.getElementById('ldap-bind-password').value = data.ldap.bind_password || '';
        document.getElementById('ldap-base-dn').value = data.ldap.base_dn || '';
        document.getElementById('ldap-search-filter').value = data.ldap.search_filter || '';
        document.getElementById('ldap-search-attributes').value = data.ldap.search_attributes || 'uid,sAMAccountName,cn,userPrincipalName,mail';
        document.getElementById('ldap-uid-attribute').value = data.ldap.uid_attribute || 'uid';
        document.getElementById('ldap-username-attribute').value = data.ldap.username_attribute || 'auto';
        document.getElementById('ldap-admin-filter').value = data.ldap.admin_filter || '';
        document.getElementById('ldap-admin-filter-memberuid').checked = !!data.ldap.admin_filter_memberuid;
        toggleLDAPFields();

        document.getElementById('smtp-host').value = data.smtp.host || '';
        document.getElementById('smtp-port').value = data.smtp.port || 587;
        document.getElementById('smtp-username').value = data.smtp.username || '';
        document.getElementById('smtp-password').value = data.smtp.password || '';
        document.getElementById('smtp-from').value = data.smtp.from || '';
        document.getElementById('smtp-tls').checked = data.smtp.use_tls !== false;

        document.getElementById('wh-discord-url').value = (data.webhooks.discord && data.webhooks.discord.url) || '';
        document.getElementById('wh-telegram-token').value = (data.webhooks.telegram && data.webhooks.telegram.token) || '';
        document.getElementById('wh-telegram-chat').value = (data.webhooks.telegram && data.webhooks.telegram.chat_id) || '';
        document.getElementById('wh-matrix-url').value = (data.webhooks.matrix && data.webhooks.matrix.url) || '';
        document.getElementById('wh-matrix-room').value = (data.webhooks.matrix && data.webhooks.matrix.room_id) || '';
        document.getElementById('wh-matrix-token').value = (data.webhooks.matrix && data.webhooks.matrix.token) || '';

        const backupCfg = data.backup || {};
        document.getElementById('backup-enabled').checked = !!backupCfg.enabled;
        const backupHour = Number.isInteger(backupCfg.hour) ? backupCfg.hour : 3;
        const backupMinute = Number.isInteger(backupCfg.minute) ? backupCfg.minute : 0;
        document.getElementById('backup-time').value = `${String(backupHour).padStart(2, '0')}:${String(backupMinute).padStart(2, '0')}`;
        document.getElementById('backup-retention').value = 7;

        emailBaseDefaults = {
            header: data.default_email_base_header || '',
            footer: data.default_email_base_footer || '',
        };

        if (data.email_templates) {
            loadedEmailTemplates = { ...data.email_templates };
            document.getElementById('tpl-base-header').value = data.email_templates.base_template_header || '';
            document.getElementById('tpl-base-footer').value = data.email_templates.base_template_footer || '';
            document.getElementById('tpl-confirmation').value = data.email_templates.confirmation || '';
            document.getElementById('tpl-confirmation-subject').value = data.email_templates.confirmation_subject || '';
            document.getElementById('tpl-enable-confirmation-email').checked = !data.email_templates.disable_confirmation_email;
            document.getElementById('tpl-expiry-reminder').value = data.email_templates.expiry_reminder || '';
            document.getElementById('tpl-expiry-reminder-subject').value = data.email_templates.expiry_reminder_subject || '';
            document.getElementById('tpl-enable-expiry-reminder-email').checked = !data.email_templates.disable_expiry_reminder_emails;
            document.getElementById('tpl-expiry-reminder-days').value = data.email_templates.expiry_reminder_days || 3;
            document.getElementById('tpl-invitation').value = data.email_templates.invitation || '';
            document.getElementById('tpl-invitation-subject').value = data.email_templates.invitation_subject || '';
            document.getElementById('tpl-invite-expiry').value = data.email_templates.invite_expiry || '';
            document.getElementById('tpl-invite-expiry-subject').value = data.email_templates.invite_expiry_subject || '';
            document.getElementById('tpl-enable-invite-expiry-email').checked = !data.email_templates.disable_invite_expiry_email;
            document.getElementById('tpl-password-reset').value = data.email_templates.password_reset || '';
            document.getElementById('tpl-password-reset-subject').value = data.email_templates.password_reset_subject || '';
            document.getElementById('tpl-email-verification').value = data.email_templates.email_verification || '';
            document.getElementById('tpl-email-verification-subject').value = data.email_templates.email_verification_subject || '';
            document.getElementById('tpl-user-creation').value = data.email_templates.user_creation || '';
            document.getElementById('tpl-user-creation-subject').value = data.email_templates.user_creation_subject || '';
            document.getElementById('tpl-enable-user-creation-email').checked = !data.email_templates.disable_user_creation_email;
            document.getElementById('tpl-user-deletion').value = data.email_templates.user_deletion || '';
            document.getElementById('tpl-user-deletion-subject').value = data.email_templates.user_deletion_subject || '';
            document.getElementById('tpl-enable-user-deletion-email').checked = !data.email_templates.disable_user_deletion_email;
            document.getElementById('tpl-user-disabled').value = data.email_templates.user_disabled || '';
            document.getElementById('tpl-user-disabled-subject').value = data.email_templates.user_disabled_subject || '';
            document.getElementById('tpl-enable-user-disabled-email').checked = !data.email_templates.disable_user_disabled_email;
            document.getElementById('tpl-user-enabled').value = data.email_templates.user_enabled || '';
            document.getElementById('tpl-user-enabled-subject').value = data.email_templates.user_enabled_subject || '';
            document.getElementById('tpl-enable-user-enabled-email').checked = !data.email_templates.disable_user_enabled_email;
            document.getElementById('tpl-user-expired').value = data.email_templates.user_expired || '';
            document.getElementById('tpl-user-expired-subject').value = data.email_templates.user_expired_subject || '';
            document.getElementById('tpl-enable-user-expired-email').checked = !data.email_templates.disable_user_expired_email;
            document.getElementById('tpl-expiry-adjusted').value = data.email_templates.expiry_adjusted || '';
            document.getElementById('tpl-expiry-adjusted-subject').value = data.email_templates.expiry_adjusted_subject || '';
            document.getElementById('tpl-enable-expiry-adjusted-email').checked = !data.email_templates.disable_expiry_adjusted_email;
            document.getElementById('tpl-welcome').value = data.email_templates.welcome || '';
            document.getElementById('tpl-welcome-subject').value = data.email_templates.welcome_subject || '';
            document.getElementById('tpl-enable-welcome-email').checked = !data.email_templates.disable_welcome_email;
        }

        await requestBasePreview(document.getElementById('tpl-base-live-preview-frame'), { showLoading: true });
    }

    async function saveSettings(section, event) {
        event.preventDefault();

        let body = {};
        const endpoint = `/admin/api/settings/${section}`;

        if (section === 'general') {
            body = {
                default_lang: document.getElementById('default-lang').value,
                jellygate_url: document.getElementById('general-jellygate-url').value.trim(),
                jellyfin_url: document.getElementById('general-jellyfin-url').value.trim(),
                jellyseerr_url: document.getElementById('general-jellyseerr-url').value.trim(),
                jellytrack_url: document.getElementById('general-jellytrack-url').value.trim(),
            };
        } else if (section === 'invitation-profile') {
            body = {
                ...currentInvitationProfile,
                require_email: document.getElementById('invite-profile-require-email').checked,
                require_email_verification: document.getElementById('invite-profile-require-email-verification').checked,
                email_verification_policy: document.getElementById('invite-profile-email-verification-policy').value || 'required',
                username_min_length: parseInt(document.getElementById('invite-profile-user-min').value, 10) || 3,
                username_max_length: parseInt(document.getElementById('invite-profile-user-max').value, 10) || 32,
                password_min_length: parseInt(document.getElementById('invite-profile-pw-min').value, 10) || 8,
                password_max_length: parseInt(document.getElementById('invite-profile-pw-max').value, 10) || 128,
                password_require_upper: document.getElementById('invite-profile-pw-upper').checked,
                password_require_lower: document.getElementById('invite-profile-pw-lower').checked,
                password_require_digit: document.getElementById('invite-profile-pw-digit').checked,
                password_require_special: document.getElementById('invite-profile-pw-special').checked,
            };
        } else if (section === 'ldap') {
            body = collectLDAPPayload();
        } else if (section === 'smtp') {
            body = {
                host: document.getElementById('smtp-host').value,
                port: parseInt(document.getElementById('smtp-port').value, 10) || 587,
                username: document.getElementById('smtp-username').value,
                password: document.getElementById('smtp-password').value,
                from: document.getElementById('smtp-from').value,
                use_tls: document.getElementById('smtp-tls').checked,
            };
        } else if (section === 'webhooks') {
            body = {
                discord: { url: document.getElementById('wh-discord-url').value },
                telegram: {
                    token: document.getElementById('wh-telegram-token').value,
                    chat_id: document.getElementById('wh-telegram-chat').value,
                },
                matrix: {
                    url: document.getElementById('wh-matrix-url').value,
                    room_id: document.getElementById('wh-matrix-room').value,
                    token: document.getElementById('wh-matrix-token').value,
                },
            };
        } else if (section === 'email-templates') {
            const reminderDaysRaw = parseInt(document.getElementById('tpl-expiry-reminder-days').value, 10);
            const reminderDays = Number.isInteger(reminderDaysRaw) ? reminderDaysRaw : 3;
            const reminderTemplate = document.getElementById('tpl-expiry-reminder').value;
            body = {
                base_template_header: document.getElementById('tpl-base-header').value,
                base_template_footer: document.getElementById('tpl-base-footer').value,
                confirmation: document.getElementById('tpl-confirmation').value,
                confirmation_subject: document.getElementById('tpl-confirmation-subject').value.trim(),
                disable_confirmation_email: !document.getElementById('tpl-enable-confirmation-email').checked,
                email_verification_subject: loadedEmailTemplates.email_verification_subject || '',
                email_verification: loadedEmailTemplates.email_verification || '',
                expiry_reminder: reminderTemplate,
                expiry_reminder_subject: document.getElementById('tpl-expiry-reminder-subject').value.trim(),
                disable_expiry_reminder_emails: !document.getElementById('tpl-enable-expiry-reminder-email').checked,
                expiry_reminder_days: Math.max(1, Math.min(365, reminderDays)),
                expiry_reminder_14: reminderTemplate,
                expiry_reminder_7: reminderTemplate,
                expiry_reminder_1: reminderTemplate,
                invitation: document.getElementById('tpl-invitation').value,
                invitation_subject: document.getElementById('tpl-invitation-subject').value.trim(),
                invite_expiry: document.getElementById('tpl-invite-expiry').value,
                invite_expiry_subject: document.getElementById('tpl-invite-expiry-subject').value.trim(),
                disable_invite_expiry_email: !document.getElementById('tpl-enable-invite-expiry-email').checked,
                password_reset: document.getElementById('tpl-password-reset').value,
                password_reset_subject: document.getElementById('tpl-password-reset-subject').value.trim(),
                email_verification: document.getElementById('tpl-email-verification').value,
                email_verification_subject: document.getElementById('tpl-email-verification-subject').value.trim(),
                pre_signup_help: '',
                disable_pre_signup_help_email: true,
                post_signup_help: '',
                disable_post_signup_help_email: true,
                user_creation: document.getElementById('tpl-user-creation').value,
                user_creation_subject: document.getElementById('tpl-user-creation-subject').value.trim(),
                disable_user_creation_email: !document.getElementById('tpl-enable-user-creation-email').checked,
                user_deletion: document.getElementById('tpl-user-deletion').value,
                user_deletion_subject: document.getElementById('tpl-user-deletion-subject').value.trim(),
                disable_user_deletion_email: !document.getElementById('tpl-enable-user-deletion-email').checked,
                user_disabled: document.getElementById('tpl-user-disabled').value,
                user_disabled_subject: document.getElementById('tpl-user-disabled-subject').value.trim(),
                disable_user_disabled_email: !document.getElementById('tpl-enable-user-disabled-email').checked,
                user_enabled: document.getElementById('tpl-user-enabled').value,
                user_enabled_subject: document.getElementById('tpl-user-enabled-subject').value.trim(),
                disable_user_enabled_email: !document.getElementById('tpl-enable-user-enabled-email').checked,
                user_expired: document.getElementById('tpl-user-expired').value,
                user_expired_subject: document.getElementById('tpl-user-expired-subject').value.trim(),
                disable_user_expired_email: !document.getElementById('tpl-enable-user-expired-email').checked,
                expiry_adjusted: document.getElementById('tpl-expiry-adjusted').value,
                expiry_adjusted_subject: document.getElementById('tpl-expiry-adjusted-subject').value.trim(),
                disable_expiry_adjusted_email: !document.getElementById('tpl-enable-expiry-adjusted-email').checked,
                welcome: document.getElementById('tpl-welcome').value,
                welcome_subject: document.getElementById('tpl-welcome-subject').value.trim(),
                disable_welcome_email: !document.getElementById('tpl-enable-welcome-email').checked,
            };
        } else if (section === 'backup') {
            const [hourStr, minuteStr] = (document.getElementById('backup-time').value || '03:00').split(':');
            document.getElementById('backup-retention').value = '7';
            body = {
                enabled: document.getElementById('backup-enabled').checked,
                hour: parseInt(hourStr, 10),
                minute: parseInt(minuteStr, 10),
                retention_count: 7,
            };
        }

        const res = await JG.api(endpoint, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });

        if (res && res.success) {
            JG.toast(res.message || t('settings_saved', 'Settings saved'), 'success');
            if (section === 'invitation-profile') {
                currentInvitationProfile = { ...body };
            }
            if (section === 'general') {
                refreshPortalShortcuts(body);
                const lang = (body.default_lang || '').trim().toLowerCase();
                if (lang) {
                    document.cookie = `lang=${lang};path=/;max-age=31536000;SameSite=Lax`;
                }
                window.location.reload();
            }
        } else {
            JG.toast((res && res.message) || t('settings_save_error', 'Save failed'), 'error');
        }
    }

    async function previewEmailTemplate(templateId) {
        const field = document.getElementById(templateId);
        const modal = document.getElementById('email-preview-modal');
        const frame = document.getElementById('email-preview-frame');
        if (!field || !modal || !frame) {
            return;
        }

        const template = field.value || '';
        if (!template.trim()) {
            JG.toast(t('template_empty', 'Template is empty.'), 'error');
            return;
        }

        frame.srcdoc = `<div style="font-family:Segoe UI,Arial,sans-serif;padding:24px;color:#0f172a;">${JG.esc(t('preview_loading', 'Loading preview...'))}</div>`;
        JG.openModal('email-preview-modal');

        const jellyfinURL = (document.getElementById('general-jellyfin-url')?.value || '').trim();
        const jellygateURL = (document.getElementById('general-jellygate-url')?.value || '').trim();
        const baseTemplateHeader = document.getElementById('tpl-base-header')?.value || '';
        const baseTemplateFooter = document.getElementById('tpl-base-footer')?.value || '';

        const res = await JG.api('/admin/api/settings/email-templates/preview', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                template,
                template_key: emailTemplateKeyFromId(templateId),
                base_template_header: baseTemplateHeader,
                base_template_footer: baseTemplateFooter,
                context: {
                    JellyfinURL: jellyfinURL || 'https://jellyfin.example.com',
                    JellyGateURL: jellygateURL || 'https://jellygate.example.com',
                    HelpURL: jellyfinURL || 'https://jellyfin.example.com',
                },
            }),
        });

        if (!res || !res.success || !res.data || typeof res.data.html !== 'string') {
            frame.srcdoc = `<div style="font-family:Segoe UI,Arial,sans-serif;padding:24px;color:#b91c1c;">${JG.esc((res && res.message) || t('preview_impossible', 'Preview failed'))}</div>`;
            return;
        }

        frame.srcdoc = String(res.data.html || '');
    }

    async function previewBaseEmailTemplate() {
        const modal = document.getElementById('email-preview-modal');
        const frame = document.getElementById('email-preview-frame');
        if (!modal || !frame) {
            return;
        }

        JG.openModal('email-preview-modal');
        await requestBasePreview(frame, { showLoading: true });
    }

    function closeEmailPreviewModal() {
        const modal = document.getElementById('email-preview-modal');
        const frame = document.getElementById('email-preview-frame');
        if (frame) frame.srcdoc = '';
        if (modal) JG.closeModal('email-preview-modal');
    }

    function attachTemplatePreviewButtons() {
        const areas = document.querySelectorAll('#form-email-templates textarea[id^="tpl-"]');
        areas.forEach((area) => {
            const card = area.closest('.p-4.rounded-xl');
            const label = card ? card.querySelector('label.jg-label') : null;
            if (!card || !label || card.querySelector('.jg-preview-btn')) {
                return;
            }

            const btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'jg-btn jg-btn-sm jg-btn-ghost jg-preview-btn mt-2';
            btn.textContent = t('preview_button', 'Preview');
            btn.addEventListener('click', () => previewEmailTemplate(area.id));
            label.insertAdjacentElement('afterend', btn);

            if (!card.querySelector('.jg-variable-picker')) {
                area.insertAdjacentElement('afterend', buildVariablePicker(area));
            }

            getTemplateValidationBox(area);
            if (!area.dataset.validationBound) {
                area.dataset.validationBound = '1';
                area.addEventListener('input', () => scheduleLiveTemplateValidation(area));
            }
            scheduleLiveTemplateValidation(area);
        });

        const closeBtn = document.getElementById('email-preview-close');
        if (closeBtn && !closeBtn.dataset.bound) {
            closeBtn.dataset.bound = '1';
            closeBtn.addEventListener('click', closeEmailPreviewModal);
        }

        const basePreviewBtn = document.getElementById('tpl-base-preview-btn');
        if (basePreviewBtn && !basePreviewBtn.dataset.bound) {
            basePreviewBtn.dataset.bound = '1';
            basePreviewBtn.addEventListener('click', previewBaseEmailTemplate);
        }

        const baseRestoreBtn = document.getElementById('tpl-base-restore-btn');
        if (baseRestoreBtn && !baseRestoreBtn.dataset.bound) {
            baseRestoreBtn.dataset.bound = '1';
            baseRestoreBtn.addEventListener('click', restoreDefaultBaseTemplate);
        }

        const baseHeader = document.getElementById('tpl-base-header');
        const baseFooter = document.getElementById('tpl-base-footer');
        [baseHeader, baseFooter].forEach((field) => {
            if (field && !field.dataset.livePreviewBound) {
                field.dataset.livePreviewBound = '1';
                field.addEventListener('input', scheduleBaseLivePreview);
            }
        });

        const modal = document.getElementById('email-preview-modal');
        if (modal && !modal.dataset.bound) {
            modal.dataset.bound = '1';
            modal.addEventListener('click', (event) => {
                if (event.target === modal) {
                    closeEmailPreviewModal();
                }
            });
        }
    }

    function formatSize(size) {
        if (!size || size <= 0) {
            return '0 B';
        }
        const units = ['B', 'KB', 'MB', 'GB'];
        let idx = 0;
        let value = size;
        while (value >= 1024 && idx < units.length - 1) {
            value /= 1024;
            idx++;
        }
        return `${value.toFixed(idx === 0 ? 0 : 1)} ${units[idx]}`;
    }

    function fmtDateTime(value) {
        const date = new Date(value);
        if (Number.isNaN(date.getTime())) {
            return value || 'â€”';
        }
        return date.toLocaleString();
    }

    async function loadBackups() {
        const tbody = document.getElementById('backup-list-body');
        if (!tbody) {
            return;
        }

        const res = await JG.api('/admin/api/backups');
        if (!res || !res.success) {
            tbody.innerHTML = `<tr><td colspan="4" class="text-center text-red-300 py-8">${JG.esc(t('backup_list_load_error', 'Unable to load backups.'))}</td></tr>`;
            return;
        }

        const backups = res.data || [];
        if (backups.length === 0) {
            tbody.innerHTML = `<tr><td colspan="4" class="text-center text-slate-500 py-8">${JG.esc(t('backup_list_empty', 'No backup available.'))}</td></tr>`;
            return;
        }

        tbody.innerHTML = backups.map((backup) => `
      <tr>
        <td class="font-mono text-xs text-slate-200">${JG.esc(backup.name)}</td>
        <td class="text-slate-400">${JG.esc(formatSize(backup.size_bytes))}</td>
        <td class="text-slate-400">${JG.esc(fmtDateTime(backup.created_at))}</td>
        <td class="text-right">
          <div class="flex justify-end gap-2 flex-wrap">
            <button class="jg-btn jg-btn-sm jg-btn-ghost" data-backup-action="download" data-backup-name="${encodeURIComponent(backup.name)}">${JG.esc(t('backup_action_export', 'Export'))}</button>
            <button class="jg-btn jg-btn-sm" data-backup-action="restore" data-backup-name="${encodeURIComponent(backup.name)}">${JG.esc(t('backup_action_restore', 'Restore'))}</button>
            <button class="jg-btn jg-btn-sm jg-btn-danger" data-backup-action="delete" data-backup-name="${encodeURIComponent(backup.name)}">${JG.esc(t('backup_action_delete', 'Delete'))}</button>
          </div>
        </td>
      </tr>
    `).join('');
    }

    async function createBackupNow() {
        const res = await JG.api('/admin/api/backups/create', { method: 'POST' });
        if (res && res.success) {
            JG.toast(res.message || t('backup_created', 'Backup created'), 'success');
            await loadBackups();
            return;
        }
        JG.toast((res && res.message) || t('backup_error', 'Backup failed'), 'error');
    }

    async function importBackup() {
        const input = document.getElementById('backup-import-file');
        const file = input.files && input.files[0];
        if (!file) {
            JG.toast(t('backup_select_zip', 'Select a .zip file'), 'error');
            return;
        }

        const formData = new FormData();
        formData.append('file', file);
        const res = await JG.api('/admin/api/backups/import', { method: 'POST', body: formData });
        if (res && res.success) {
            JG.toast(res.message || t('backup_imported', 'Backup imported'), 'success');
            input.value = '';
            await loadBackups();
            return;
        }
        JG.toast((res && res.message) || t('backup_import_error', 'Import failed'), 'error');
    }

    function downloadBackup(name) {
        window.location.href = `/admin/api/backups/${encodeURIComponent(name)}/download`;
    }

    async function restoreBackup(name) {
        const confirmSuffix = backupDatabaseType === 'sqlite'
            ? t('backup_restore_confirm_suffix', 'A restart is required.')
            : t('backup_restore_confirm_suffix_pg', 'The restore will be applied immediately.');

        if (!confirm(`${t('backup_restore_confirm_prefix', 'Prepare restoration for')} ${name}? ${confirmSuffix}`)) {
            return;
        }
        const res = await JG.api(`/admin/api/backups/${encodeURIComponent(name)}/restore`, { method: 'POST' });
        if (res && res.success) {
            JG.toast(res.message || t('backup_restore_ready', 'Restoration prepared. Restart JellyGate.'), 'success');
            return;
        }
        JG.toast((res && res.message) || t('backup_restore_error', 'Restore failed'), 'error');
    }

    async function deleteBackup(name) {
        if (!confirm(`${t('backup_delete_confirm_prefix', 'Delete permanently')} ${name}?`)) {
            return;
        }
        const res = await JG.api(`/admin/api/backups/${encodeURIComponent(name)}`, { method: 'DELETE' });
        if (res && res.success) {
            JG.toast(t('backup_deleted', 'Backup deleted'), 'success');
            await loadBackups();
            return;
        }
        JG.toast((res && res.message) || t('backup_delete_error', 'Delete failed'), 'error');
    }

    document.addEventListener('DOMContentLoaded', async () => {
        document.querySelectorAll('[data-tab-target]').forEach((btn) => {
            btn.addEventListener('click', () => switchTab(btn.dataset.tabTarget || 'general'));
        });

        [
            ['form-general', 'general'],
            ['form-invitation-profile', 'invitation-profile'],
            ['form-ldap', 'ldap'],
            ['form-smtp', 'smtp'],
            ['form-webhooks', 'webhooks'],
            ['form-email-templates', 'email-templates'],
            ['form-backup', 'backup'],
        ].forEach(([id, section]) => {
            const form = document.getElementById(id);
            if (form) {
                form.addEventListener('submit', (event) => saveSettings(section, event));
            }
        });

        document.getElementById('ldap-enabled')?.addEventListener('change', toggleLDAPFields);
        document.getElementById('invite-profile-require-email-verification')?.addEventListener('change', syncInviteEmailRequirementFromVerification);
        document.getElementById('invite-profile-email-verification-policy')?.addEventListener('change', syncInviteEmailPolicyControls);

        const toggle = document.getElementById('sidebar-toggle');
        if (toggle) {
            toggle.addEventListener('click', () => {
                const sidebar = document.getElementById('sidebar');
                if (sidebar) sidebar.classList.toggle('open');
            });
        }

        await loadSettings();
        attachTemplatePreviewButtons();
        await loadBackups();

        document.getElementById('backup-list-body')?.addEventListener('click', async (event) => {
            const button = event.target.closest('[data-backup-action]');
            if (!button) {
                return;
            }
            const name = decodeURIComponent(button.dataset.backupName || '');
            const action = button.dataset.backupAction || '';
            if (!name || !action) {
                return;
            }
            if (action === 'download') {
                downloadBackup(name);
                return;
            }
            if (action === 'restore') {
                await restoreBackup(name);
                return;
            }
            if (action === 'delete') {
                await deleteBackup(name);
            }
        });

        document.getElementById('btn-ldap-test-conn')?.addEventListener('click', async () => {
            await runLDAPTest('/admin/api/settings/ldap/test-connection', collectLDAPPayload());
        });

        document.getElementById('btn-ldap-test-user')?.addEventListener('click', async () => {
            const username = (document.getElementById('ldap-test-username').value || '').trim();
            if (!username) {
                showLDAPTestResult(t('ldap_test_username_required', 'Enter a test user identifier.'), 'error');
                return;
            }
            const payload = collectLDAPPayload();
            payload.username = username;
            await runLDAPTest('/admin/api/settings/ldap/test-user', payload);
        });

        document.getElementById('btn-ldap-test-jf')?.addEventListener('click', async () => {
            const username = (document.getElementById('ldap-test-username').value || '').trim();
            const password = document.getElementById('ldap-test-password').value || '';
            if (!username || !password) {
                showLDAPTestResult(t('ldap_test_credentials_required', 'Enter username and password to validate Jellyfin LDAP plugin.'), 'error');
                return;
            }
            await runLDAPTest('/admin/api/settings/ldap/test-jellyfin-auth', { username, password });
        });

        document.getElementById('backup-create-btn')?.addEventListener('click', createBackupNow);
        document.getElementById('backup-import-btn')?.addEventListener('click', importBackup);
    });
})();
