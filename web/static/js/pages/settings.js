(() => {
    const config = window.JGPageSettings || {};
    const i18n = config.i18n || {};
    let backupDatabaseType = 'sqlite';

    function t(key, fallback) {
        return i18n[key] || fallback || key;
    }

    function switchTab(name) {
        document.querySelectorAll('.tab-panel').forEach((panel) => panel.classList.add('hidden'));
        document.querySelectorAll('.tab-btn').forEach((button) => button.classList.remove('active'));
        document.getElementById(`panel-${name}`)?.classList.remove('hidden');
        document.getElementById(`tab-${name}`)?.classList.add('active');
    }

    function switchEmailTemplateSection(name) {
        document.querySelectorAll('.email-template-section').forEach((section) => section.classList.add('hidden'));
        document.querySelectorAll('.email-submenu-btn').forEach((button) => button.classList.remove('active'));
        document.getElementById(`email-section-${name}`)?.classList.remove('hidden');
        document.querySelector(`.email-submenu-btn[data-email-section-target="${name}"]`)?.classList.add('active');
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
        return {
            enabled: document.getElementById('ldap-enabled').checked,
            host: document.getElementById('ldap-host').value,
            port: parseInt(document.getElementById('ldap-port').value, 10) || 636,
            use_tls: document.getElementById('ldap-tls').checked,
            skip_verify: document.getElementById('ldap-skip-verify').checked,
            bind_dn: document.getElementById('ldap-bind-dn').value,
            bind_password: document.getElementById('ldap-bind-password').value,
            base_dn: document.getElementById('ldap-base-dn').value,
            username_attribute: document.getElementById('ldap-username-attribute').value,
            user_object_class: document.getElementById('ldap-user-object-class').value.trim(),
            group_member_attr: document.getElementById('ldap-group-member-attr').value.trim(),
            provision_mode: document.getElementById('ldap-provision-mode').value,
            user_ou: document.getElementById('ldap-user-ou').value || 'CN=Users',
            domain: document.getElementById('ldap-domain').value,
            jellyfin_group: document.getElementById('ldap-jellyfin-group').value.trim(),
            inviter_group: document.getElementById('ldap-inviter-group').value.trim(),
            administrators_group: document.getElementById('ldap-admin-group').value.trim(),
            user_group: document.getElementById('ldap-jellyfin-group').value.trim(),
        };
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
            { id: 'shortcut-jellytulli', url: links.jellytulli_url || '' },
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
            JG.api('/admin/api/users'),
        ]);

        if (presetsRes && presetsRes.success && Array.isArray(presetsRes.data)) {
            const presetOptions = presetsRes.data.map((preset) => ({
                value: preset.id || '',
                label: preset.name || preset.id || t('preset_fallback', 'Preset'),
            }));
            setSelectOptions('invite-profile-preset', presetOptions);
        }

        if (usersRes && usersRes.success && Array.isArray(usersRes.data)) {
            const userOptions = usersRes.data
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
        document.getElementById('invite-profile-preset').value = profile.policy_preset_id || '';
        document.getElementById('invite-profile-template-user').value = profile.template_user_id || '';
        document.getElementById('invite-profile-enable-downloads').checked = profile.enable_downloads !== false;
        document.getElementById('invite-profile-require-email').checked = profile.require_email !== false;
        document.getElementById('invite-profile-allow-inviter-grant').checked = !!profile.allow_inviter_grant_invite;
        document.getElementById('invite-profile-allow-inviter-user-expiry').checked = profile.allow_inviter_user_expiry !== false;
        document.getElementById('invite-profile-disable-after').value = Number.isInteger(profile.disable_after_days) ? profile.disable_after_days : 0;
        document.getElementById('invite-profile-delete-after').value = Number.isInteger(profile.delete_after_days) ? profile.delete_after_days : 0;
        document.getElementById('invite-profile-inviter-max-uses').value = Number.isInteger(profile.inviter_max_uses) ? profile.inviter_max_uses : 0;
        document.getElementById('invite-profile-inviter-max-link-hours').value = Number.isInteger(profile.inviter_max_link_hours) ? profile.inviter_max_link_hours : 0;
        document.getElementById('invite-profile-inviter-quota-day').value = Number.isInteger(profile.inviter_quota_day) ? profile.inviter_quota_day : 0;
        document.getElementById('invite-profile-inviter-quota-week').value = Number.isInteger(profile.inviter_quota_week) ? profile.inviter_quota_week : 0;
        document.getElementById('invite-profile-inviter-quota-month').value = Number.isInteger(profile.inviter_quota_month) ? profile.inviter_quota_month : 0;
        document.getElementById('invite-profile-expiry-action').value = profile.expiry_action || 'disable';
        document.getElementById('invite-profile-user-min').value = Number.isInteger(profile.username_min_length) ? profile.username_min_length : 3;
        document.getElementById('invite-profile-user-max').value = Number.isInteger(profile.username_max_length) ? profile.username_max_length : 32;
        document.getElementById('invite-profile-pw-min').value = Number.isInteger(profile.password_min_length) ? profile.password_min_length : 8;
        document.getElementById('invite-profile-pw-max').value = Number.isInteger(profile.password_max_length) ? profile.password_max_length : 128;
        document.getElementById('invite-profile-pw-upper').checked = !!profile.password_require_upper;
        document.getElementById('invite-profile-pw-lower').checked = !!profile.password_require_lower;
        document.getElementById('invite-profile-pw-digit').checked = !!profile.password_require_digit;
        document.getElementById('invite-profile-pw-special').checked = !!profile.password_require_special;
    }

    function setBackupMode(databaseType) {
        const normalized = String(databaseType || '').trim().toLowerCase() || 'sqlite';
        backupDatabaseType = normalized;
        const isSQLite = normalized === 'sqlite';

        const note = document.getElementById('backup-db-note');
        const actionsGrid = document.getElementById('backup-actions-grid');
        const listSection = document.getElementById('backup-list-section');
        const form = document.getElementById('form-backup');

        if (note) {
            if (isSQLite) {
                note.classList.add('hidden');
            } else {
                note.classList.remove('hidden');
                note.innerHTML = [
                    `<div class="font-semibold mb-1">${JG.esc(t('backup_pg_mode_title', 'PostgreSQL mode detected'))}</div>`,
                    `<div>${JG.esc(t('backup_pg_mode_desc_1', 'Native JellyGate ZIP backups (SQLite) are disabled.'))}</div>`,
                    `<div class="mt-2">${JG.esc(t('backup_pg_mode_desc_2', 'Use pg_dump and pg_restore for PostgreSQL.'))}</div>`,
                ].join('');
            }
        }

        actionsGrid?.classList.toggle('hidden', !isSQLite);
        listSection?.classList.toggle('hidden', !isSQLite);

        if (form) {
            form.querySelectorAll('input, select, button').forEach((el) => {
                el.disabled = !isSQLite;
            });
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
        document.getElementById('general-jellytulli-url').value = links.jellytulli_url || '';
        refreshPortalShortcuts(links);

        applyInvitationProfileConfig(data.invitation_profile || {});

        document.getElementById('ldap-enabled').checked = data.ldap.enabled || false;
        document.getElementById('ldap-host').value = data.ldap.host || '';
        document.getElementById('ldap-port').value = data.ldap.port || 636;
        document.getElementById('ldap-tls').checked = data.ldap.use_tls !== false;
        document.getElementById('ldap-skip-verify').checked = data.ldap.skip_verify || false;
        document.getElementById('ldap-bind-dn').value = data.ldap.bind_dn || '';
        document.getElementById('ldap-bind-password').value = data.ldap.bind_password || '';
        document.getElementById('ldap-base-dn').value = data.ldap.base_dn || '';
        document.getElementById('ldap-username-attribute').value = data.ldap.username_attribute || 'auto';
        document.getElementById('ldap-user-object-class').value = data.ldap.user_object_class || 'auto';
        document.getElementById('ldap-group-member-attr').value = data.ldap.group_member_attr || 'auto';
        document.getElementById('ldap-provision-mode').value = data.ldap.provision_mode || 'hybrid';
        document.getElementById('ldap-user-ou').value = data.ldap.user_ou || 'CN=Users';
        document.getElementById('ldap-domain').value = data.ldap.domain || '';
        document.getElementById('ldap-jellyfin-group').value = data.ldap.jellyfin_group || data.ldap.user_group || 'jellyfin';
        document.getElementById('ldap-inviter-group').value = data.ldap.inviter_group || 'jellyfin-Parrainage';
        document.getElementById('ldap-admin-group').value = data.ldap.administrators_group || 'jellyfin-administrateur';
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

        if (data.email_templates) {
            document.getElementById('tpl-confirmation').value = data.email_templates.confirmation || '';
            document.getElementById('tpl-enable-confirmation-email').checked = !data.email_templates.disable_confirmation_email;
            document.getElementById('tpl-email-verification-subject').value = data.email_templates.email_verification_subject || '';
            document.getElementById('tpl-email-verification').value = data.email_templates.email_verification || '';
            document.getElementById('tpl-expiry-reminder').value = data.email_templates.expiry_reminder || '';
            document.getElementById('tpl-enable-expiry-reminder-email').checked = !data.email_templates.disable_expiry_reminder_emails;
            document.getElementById('tpl-expiry-reminder-days').value = data.email_templates.expiry_reminder_days || 3;
            document.getElementById('tpl-expiry-reminder-14').value = data.email_templates.expiry_reminder_14 || '';
            document.getElementById('tpl-expiry-reminder-7').value = data.email_templates.expiry_reminder_7 || '';
            document.getElementById('tpl-expiry-reminder-1').value = data.email_templates.expiry_reminder_1 || '';
            document.getElementById('tpl-invitation').value = data.email_templates.invitation || '';
            document.getElementById('tpl-invite-expiry').value = data.email_templates.invite_expiry || '';
            document.getElementById('tpl-enable-invite-expiry-email').checked = !data.email_templates.disable_invite_expiry_email;
            document.getElementById('tpl-password-reset').value = data.email_templates.password_reset || '';
            document.getElementById('tpl-pre-signup-help').value = data.email_templates.pre_signup_help || '';
            document.getElementById('tpl-enable-pre-signup-help-email').checked = !data.email_templates.disable_pre_signup_help_email;
            document.getElementById('tpl-post-signup-help').value = data.email_templates.post_signup_help || '';
            document.getElementById('tpl-enable-post-signup-help-email').checked = !data.email_templates.disable_post_signup_help_email;
            document.getElementById('tpl-user-creation').value = data.email_templates.user_creation || '';
            document.getElementById('tpl-enable-user-creation-email').checked = !data.email_templates.disable_user_creation_email;
            document.getElementById('tpl-user-deletion').value = data.email_templates.user_deletion || '';
            document.getElementById('tpl-enable-user-deletion-email').checked = !data.email_templates.disable_user_deletion_email;
            document.getElementById('tpl-user-disabled').value = data.email_templates.user_disabled || '';
            document.getElementById('tpl-enable-user-disabled-email').checked = !data.email_templates.disable_user_disabled_email;
            document.getElementById('tpl-user-enabled').value = data.email_templates.user_enabled || '';
            document.getElementById('tpl-enable-user-enabled-email').checked = !data.email_templates.disable_user_enabled_email;
            document.getElementById('tpl-user-expired').value = data.email_templates.user_expired || '';
            document.getElementById('tpl-enable-user-expired-email').checked = !data.email_templates.disable_user_expired_email;
            document.getElementById('tpl-expiry-adjusted').value = data.email_templates.expiry_adjusted || '';
            document.getElementById('tpl-enable-expiry-adjusted-email').checked = !data.email_templates.disable_expiry_adjusted_email;
            document.getElementById('tpl-welcome').value = data.email_templates.welcome || '';
            document.getElementById('tpl-enable-welcome-email').checked = !data.email_templates.disable_welcome_email;
        }
    }

    async function saveSettings(section, event) {
        event.preventDefault();

        if (section === 'backup' && backupDatabaseType !== 'sqlite') {
            JG.toast(t('backup_pg_plan_unavailable', 'Local backup scheduler unavailable in PostgreSQL mode'), 'error');
            return;
        }

        let body = {};
        const endpoint = `/admin/api/settings/${section}`;

        if (section === 'general') {
            body = {
                default_lang: document.getElementById('default-lang').value,
                jellygate_url: document.getElementById('general-jellygate-url').value.trim(),
                jellyfin_url: document.getElementById('general-jellyfin-url').value.trim(),
                jellyseerr_url: document.getElementById('general-jellyseerr-url').value.trim(),
                jellytulli_url: document.getElementById('general-jellytulli-url').value.trim(),
            };
        } else if (section === 'invitation-profile') {
            body = {
                policy_preset_id: (document.getElementById('invite-profile-preset').value || '').trim(),
                template_user_id: (document.getElementById('invite-profile-template-user').value || '').trim(),
                enable_downloads: document.getElementById('invite-profile-enable-downloads').checked,
                require_email: document.getElementById('invite-profile-require-email').checked,
                allow_inviter_grant_invite: document.getElementById('invite-profile-allow-inviter-grant').checked,
                allow_inviter_user_expiry: document.getElementById('invite-profile-allow-inviter-user-expiry').checked,
                disable_after_days: parseInt(document.getElementById('invite-profile-disable-after').value, 10) || 0,
                delete_after_days: parseInt(document.getElementById('invite-profile-delete-after').value, 10) || 0,
                inviter_max_uses: parseInt(document.getElementById('invite-profile-inviter-max-uses').value, 10) || 0,
                inviter_max_link_hours: parseInt(document.getElementById('invite-profile-inviter-max-link-hours').value, 10) || 0,
                inviter_quota_day: parseInt(document.getElementById('invite-profile-inviter-quota-day').value, 10) || 0,
                inviter_quota_week: parseInt(document.getElementById('invite-profile-inviter-quota-week').value, 10) || 0,
                inviter_quota_month: parseInt(document.getElementById('invite-profile-inviter-quota-month').value, 10) || 0,
                expiry_action: (document.getElementById('invite-profile-expiry-action').value || 'disable').trim(),
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
            body = {
                confirmation: document.getElementById('tpl-confirmation').value,
                disable_confirmation_email: !document.getElementById('tpl-enable-confirmation-email').checked,
                email_verification_subject: document.getElementById('tpl-email-verification-subject').value,
                email_verification: document.getElementById('tpl-email-verification').value,
                expiry_reminder: document.getElementById('tpl-expiry-reminder').value,
                disable_expiry_reminder_emails: !document.getElementById('tpl-enable-expiry-reminder-email').checked,
                expiry_reminder_days: Math.max(1, Math.min(365, reminderDays)),
                expiry_reminder_14: document.getElementById('tpl-expiry-reminder-14').value,
                expiry_reminder_7: document.getElementById('tpl-expiry-reminder-7').value,
                expiry_reminder_1: document.getElementById('tpl-expiry-reminder-1').value,
                invitation: document.getElementById('tpl-invitation').value,
                invite_expiry: document.getElementById('tpl-invite-expiry').value,
                disable_invite_expiry_email: !document.getElementById('tpl-enable-invite-expiry-email').checked,
                password_reset: document.getElementById('tpl-password-reset').value,
                pre_signup_help: document.getElementById('tpl-pre-signup-help').value,
                disable_pre_signup_help_email: !document.getElementById('tpl-enable-pre-signup-help-email').checked,
                post_signup_help: document.getElementById('tpl-post-signup-help').value,
                disable_post_signup_help_email: !document.getElementById('tpl-enable-post-signup-help-email').checked,
                user_creation: document.getElementById('tpl-user-creation').value,
                disable_user_creation_email: !document.getElementById('tpl-enable-user-creation-email').checked,
                user_deletion: document.getElementById('tpl-user-deletion').value,
                disable_user_deletion_email: !document.getElementById('tpl-enable-user-deletion-email').checked,
                user_disabled: document.getElementById('tpl-user-disabled').value,
                disable_user_disabled_email: !document.getElementById('tpl-enable-user-disabled-email').checked,
                user_enabled: document.getElementById('tpl-user-enabled').value,
                disable_user_enabled_email: !document.getElementById('tpl-enable-user-enabled-email').checked,
                user_expired: document.getElementById('tpl-user-expired').value,
                disable_user_expired_email: !document.getElementById('tpl-enable-user-expired-email').checked,
                expiry_adjusted: document.getElementById('tpl-expiry-adjusted').value,
                disable_expiry_adjusted_email: !document.getElementById('tpl-enable-expiry-adjusted-email').checked,
                welcome: document.getElementById('tpl-welcome').value,
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
        modal.style.display = '';

        const jellyfinURL = (document.getElementById('general-jellyfin-url')?.value || '').trim();
        const jellygateURL = (document.getElementById('general-jellygate-url')?.value || '').trim();

        const res = await JG.api('/admin/api/settings/email-templates/preview', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                template,
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

    function closeEmailPreviewModal() {
        const modal = document.getElementById('email-preview-modal');
        const frame = document.getElementById('email-preview-frame');
        if (frame) frame.srcdoc = '';
        if (modal) modal.style.display = 'none';
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
        });

        const closeBtn = document.getElementById('email-preview-close');
        if (closeBtn && !closeBtn.dataset.bound) {
            closeBtn.dataset.bound = '1';
            closeBtn.addEventListener('click', closeEmailPreviewModal);
        }

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
            return value || '—';
        }
        return date.toLocaleString();
    }

    async function loadBackups() {
        const tbody = document.getElementById('backup-list-body');
        if (!tbody) {
            return;
        }
        if (backupDatabaseType !== 'sqlite') {
            tbody.innerHTML = `<tr><td colspan="4" class="text-center text-amber-200 py-8">${JG.esc(t('backup_list_pg_unavailable', 'PostgreSQL mode: SQLite archives are unavailable.'))}</td></tr>`;
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
        if (backupDatabaseType !== 'sqlite') {
            JG.toast(t('backup_pg_not_supported', 'Action is not supported in PostgreSQL mode'), 'error');
            return;
        }

        const res = await JG.api('/admin/api/backups/create', { method: 'POST' });
        if (res && res.success) {
            JG.toast(res.message || t('backup_created', 'Backup created'), 'success');
            await loadBackups();
            return;
        }
        JG.toast((res && res.message) || t('backup_error', 'Backup failed'), 'error');
    }

    async function importBackup() {
        if (backupDatabaseType !== 'sqlite') {
            JG.toast(t('backup_pg_not_supported', 'Action is not supported in PostgreSQL mode'), 'error');
            return;
        }

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
        if (backupDatabaseType !== 'sqlite') {
            JG.toast(t('backup_pg_not_supported', 'Action is not supported in PostgreSQL mode'), 'error');
            return;
        }
        window.location.href = `/admin/api/backups/${encodeURIComponent(name)}/download`;
    }

    async function restoreBackup(name) {
        if (backupDatabaseType !== 'sqlite') {
            JG.toast(t('backup_pg_not_supported', 'Action is not supported in PostgreSQL mode'), 'error');
            return;
        }
        if (!confirm(`${t('backup_restore_confirm_prefix', 'Prepare restoration for')} ${name}? ${t('backup_restore_confirm_suffix', 'A restart is required.')}`)) {
            return;
        }
        const res = await JG.api(`/admin/api/backups/${encodeURIComponent(name)}/restore`, { method: 'POST' });
        if (res && res.success) {
            JG.toast(t('backup_restore_ready', 'Restoration prepared. Restart JellyGate.'), 'success');
            return;
        }
        JG.toast((res && res.message) || t('backup_restore_error', 'Restore failed'), 'error');
    }

    async function deleteBackup(name) {
        if (backupDatabaseType !== 'sqlite') {
            JG.toast(t('backup_pg_not_supported', 'Action is not supported in PostgreSQL mode'), 'error');
            return;
        }
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

        document.querySelectorAll('[data-email-section-target]').forEach((btn) => {
            btn.addEventListener('click', () => switchEmailTemplateSection(btn.dataset.emailSectionTarget || 'onboarding'));
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

        const toggle = document.getElementById('sidebar-toggle');
        if (toggle) {
            toggle.addEventListener('click', () => {
                const sidebar = document.getElementById('sidebar');
                if (sidebar) sidebar.classList.toggle('open');
            });
        }

        await loadInvitationProfileLookups();
        await loadSettings();
        switchEmailTemplateSection('onboarding');
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