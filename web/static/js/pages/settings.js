(() => {
    const config = window.JGPageSettings || {};
    const i18n = config.i18n || {};
    let backupDatabaseType = 'sqlite';
    let loadedEmailTemplatesByLang = {};
    let loadedEmailSharedTemplateConfig = {};
    let activeEmailTemplatesLang = '';
    let emailBaseDefaults = { header: '', footer: '' };
    let currentInvitationProfile = {};
    let currentLDAPConfig = {};
    let basePreviewTimer = null;
    let basePreviewRequestId = 0;
    const templateValidationTimers = new Map();
    const templateValidationRequestIds = new Map();
    const emailLanguageOrder = ['fr', 'en', 'de', 'es', 'it', 'nl', 'pl', 'pt-br', 'ru', 'zh'];
    let activeEmailEditorTarget = null;

    function t(key, fallback) {
        return i18n[key] || fallback || key;
    }

    function normalizeLangTag(raw) {
        const value = String(raw || '').trim().toLowerCase().replace(/_/g, '-');
        if (!value) {
            return '';
        }
        if (value === 'pt' || value.startsWith('pt-')) {
            return 'pt-br';
        }
        if (emailLanguageOrder.includes(value)) {
            return value;
        }
        if (value.includes('-')) {
            const base = value.split('-')[0];
            if (base === 'pt') {
                return 'pt-br';
            }
            if (emailLanguageOrder.includes(base)) {
                return base;
            }
        }
        return value;
    }

    function getEmailTemplateLanguageOptions() {
        const source = document.getElementById('default-lang');
        const byLang = new Map();
        if (source) {
            [...source.options].forEach((option) => {
                const lang = normalizeLangTag(option.value);
                if (!lang || !emailLanguageOrder.includes(lang)) {
                    return;
                }
                if (!byLang.has(lang)) {
                    byLang.set(lang, (option.textContent || option.value || '').trim() || lang);
                }
            });
        }
        emailLanguageOrder.forEach((lang) => {
            if (!byLang.has(lang)) {
                byLang.set(lang, lang);
            }
        });
        return emailLanguageOrder.map((lang) => ({ value: lang, label: byLang.get(lang) || lang }));
    }

    function getActiveEmailLanguageLabel() {
        const select = document.getElementById('email-template-lang-select');
        if (!select) {
            return activeEmailTemplatesLang || t('settings_email_language_label', 'Language');
        }
        const option = select.options[select.selectedIndex];
        return (option && option.textContent ? option.textContent : activeEmailTemplatesLang || t('settings_email_language_label', 'Language')).trim();
    }

    function cloneEmailTemplateConfig(value) {
        if (!value || typeof value !== 'object') {
            return {};
        }
        try {
            return JSON.parse(JSON.stringify(value));
        } catch (_) {
            return { ...value };
        }
    }

    function emailTemplateKeyFromId(id) {
        return String(id || '').replace(/^tpl-/, '').replace(/-+/g, '_');
    }

    function normalizeReminderDays(value) {
        const parsed = parseInt(value, 10);
        if (!Number.isInteger(parsed)) {
            return 3;
        }
        return Math.max(1, Math.min(365, parsed));
    }

    function getDefaultEmailSharedTemplateConfig() {
        return {
            base_template_header: emailBaseDefaults.header || '',
            base_template_footer: emailBaseDefaults.footer || '',
            email_logo_url: '',
            disable_confirmation_email: false,
            disable_expiry_reminder_emails: false,
            expiry_reminder_days: 3,
            disable_invite_expiry_email: false,
            disable_user_creation_email: false,
            disable_user_deletion_email: false,
            disable_user_disabled_email: false,
            disable_user_enabled_email: false,
            disable_user_expired_email: false,
            disable_expiry_adjusted_email: false,
            disable_welcome_email: false,
        };
    }

    function extractEmailTemplateSharedConfig(value) {
        const defaults = getDefaultEmailSharedTemplateConfig();
        const source = value || {};
        return {
            base_template_header: source.base_template_header || defaults.base_template_header,
            base_template_footer: source.base_template_footer || defaults.base_template_footer,
            email_logo_url: source.email_logo_url || '',
            disable_confirmation_email: !!source.disable_confirmation_email,
            disable_expiry_reminder_emails: !!source.disable_expiry_reminder_emails,
            expiry_reminder_days: normalizeReminderDays(source.expiry_reminder_days),
            disable_invite_expiry_email: !!source.disable_invite_expiry_email,
            disable_user_creation_email: !!source.disable_user_creation_email,
            disable_user_deletion_email: !!source.disable_user_deletion_email,
            disable_user_disabled_email: !!source.disable_user_disabled_email,
            disable_user_enabled_email: !!source.disable_user_enabled_email,
            disable_user_expired_email: !!source.disable_user_expired_email,
            disable_expiry_adjusted_email: !!source.disable_expiry_adjusted_email,
            disable_welcome_email: !!source.disable_welcome_email,
        };
    }

    function extractEmailTemplateLocalizedConfig(value) {
        const source = value || {};
        return {
            confirmation: source.confirmation || '',
            confirmation_subject: source.confirmation_subject || '',
            expiry_reminder: source.expiry_reminder || '',
            expiry_reminder_subject: source.expiry_reminder_subject || '',
            invitation: source.invitation || '',
            invitation_subject: source.invitation_subject || '',
            invite_expiry: source.invite_expiry || '',
            invite_expiry_subject: source.invite_expiry_subject || '',
            password_reset: source.password_reset || '',
            password_reset_subject: source.password_reset_subject || '',
            email_verification: source.email_verification || '',
            email_verification_subject: source.email_verification_subject || '',
            user_creation: source.user_creation || '',
            user_creation_subject: source.user_creation_subject || '',
            user_deletion: source.user_deletion || '',
            user_deletion_subject: source.user_deletion_subject || '',
            user_disabled: source.user_disabled || '',
            user_disabled_subject: source.user_disabled_subject || '',
            user_enabled: source.user_enabled || '',
            user_enabled_subject: source.user_enabled_subject || '',
            user_expired: source.user_expired || '',
            user_expired_subject: source.user_expired_subject || '',
            expiry_adjusted: source.expiry_adjusted || '',
            expiry_adjusted_subject: source.expiry_adjusted_subject || '',
            welcome: source.welcome || '',
            welcome_subject: source.welcome_subject || '',
        };
    }

    function mergeEmailTemplateConfig(sharedValue, localizedValue) {
        const shared = extractEmailTemplateSharedConfig(sharedValue);
        const localized = extractEmailTemplateLocalizedConfig(localizedValue);
        return {
            ...shared,
            ...localized,
            expiry_reminder_14: localized.expiry_reminder || '',
            expiry_reminder_7: localized.expiry_reminder || '',
            expiry_reminder_1: localized.expiry_reminder || '',
            pre_signup_help: '',
            disable_pre_signup_help_email: true,
            post_signup_help: '',
            disable_post_signup_help_email: true,
        };
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
            { value: '{{.EmailLogoURL}}', label: t('settings_email_var_logo_url', 'email logo URL') },
            { value: '{{.ExpiryDate}}', label: t('settings_email_var_expiry_date', 'expiry date') },
            { value: '{{.JellyGateURL}}', label: t('settings_email_var_jellygate_url', 'public JellyGate URL') },
            { value: '{{.JellyfinURL}}', label: t('settings_email_var_jellyfin_url', 'Jellyfin login URL') },
            { value: '{{.JellyfinServerName}}', label: t('settings_email_var_jellyfin_server_name', 'Jellyfin server name') },
            { value: '{{.serveurname}}', label: t('settings_email_var_jellyfin_server_name', 'Jellyfin server name') },
            { value: '{{.JellyseerrURL}}', label: t('settings_email_var_jellyseerr_url', 'Jellyseerr URL') },
            { value: '{{.JellyTrackURL}}', label: t('settings_email_var_jellytrack_url', 'JellyTrack URL') },
            { value: '{{.Message}}', label: t('settings_email_var_message', 'custom message (admin invitation)') },
        ];
    }

    function openEmailVariableModal() {
        const modal = document.getElementById('email-variable-modal');
        if (!modal) {
            return;
        }
        renderEmailVariableLibrary();
        modal.style.display = 'flex';
        const search = document.getElementById('email-variable-search');
        if (search) {
            search.focus();
            search.select();
        }
    }

    function closeEmailVariableModal() {
        const modal = document.getElementById('email-variable-modal');
        if (!modal) {
            return;
        }
        modal.style.display = 'none';
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

    function getEmailEditorTargets() {
        return document.querySelectorAll('#panel-email-templates textarea[id^="tpl-"], #panel-email-templates input[id$="-subject"]');
    }

    function getEmailFieldLabel(field) {
        if (!field || !field.id) {
            return t('settings_email_active_field_none', 'No target selected');
        }
        const label = document.querySelector(`label[for="${field.id}"]`);
        if (!label) {
            return field.id;
        }
        return (label.textContent || field.id).trim();
    }

    function updateActiveEmailFieldIndicator() {
        const target = document.getElementById('email-variable-target');
        if (target) {
            target.textContent = activeEmailEditorTarget
                ? getEmailFieldLabel(activeEmailEditorTarget)
                : t('settings_email_active_field_none', 'No target selected');
        }

        document.querySelectorAll('.email-template-item').forEach((item) => item.classList.remove('is-target'));
        if (!activeEmailEditorTarget) {
            return;
        }
        activeEmailEditorTarget.closest('.email-template-item')?.classList.add('is-target');
    }

    function setActiveEmailEditorTarget(field) {
        activeEmailEditorTarget = field || null;
        updateActiveEmailFieldIndicator();
    }

    function bindEmailEditorTargets() {
        getEmailEditorTargets().forEach((field) => {
            if (field.dataset.emailTargetBound) {
                return;
            }
            field.dataset.emailTargetBound = '1';
            field.addEventListener('focus', () => setActiveEmailEditorTarget(field));
            field.addEventListener('click', () => setActiveEmailEditorTarget(field));
        });
        if (!activeEmailEditorTarget) {
            const first = document.getElementById('tpl-confirmation-subject') || document.getElementById('tpl-confirmation');
            if (first) {
                activeEmailEditorTarget = first;
            }
        }
        updateActiveEmailFieldIndicator();
    }

    async function insertVariableFromLibrary(item) {
        if (!item || !item.value) {
            return;
        }
        if (activeEmailEditorTarget && document.body.contains(activeEmailEditorTarget)) {
            insertTextAtCursor(activeEmailEditorTarget, item.value);
            activeEmailEditorTarget.classList.add('ring-2', 'ring-jg-accent/30');
            setTimeout(() => activeEmailEditorTarget?.classList.remove('ring-2', 'ring-jg-accent/30'), 500);
            JG.toast(t('settings_email_variable_inserted', 'Variable inserted'), 'success');
            return;
        }
        const copied = await JG.copyText(item.value);
        JG.toast(
            copied
                ? t('settings_email_variable_copied', 'Variable copied')
                : t('settings_email_variable_pick_target', 'Select a subject or message first'),
            copied ? 'success' : 'info',
        );
    }

    function renderEmailVariableLibrary() {
        const container = document.getElementById('email-variable-list');
        const search = document.getElementById('email-variable-search');
        if (!container || !search) {
            return;
        }

        const query = String(search.value || '').trim().toLowerCase();
        const items = getEmailVariableOptions().filter((item) => {
            if (!query) {
                return true;
            }
            return `${item.value} ${item.label}`.toLowerCase().includes(query);
        });

        container.innerHTML = '';
        if (items.length === 0) {
            container.innerHTML = `<div class="rounded-xl border border-dashed border-white/10 bg-black/15 px-4 py-5 text-sm text-jg-text-muted">${JG.esc(t('settings_email_variable_no_result', 'No variable matches this search.'))}</div>`;
            return;
        }
        items.forEach((item) => {
            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'rounded-xl border border-white/10 bg-black/20 px-4 py-3 text-left transition-all hover:border-jg-accent/40 hover:bg-black/35';
            button.innerHTML = `
                <div class="text-[11px] font-black uppercase tracking-[0.18em] text-jg-accent/90">${JG.esc(item.value)}</div>
                <div class="mt-2 text-sm font-semibold text-white">${JG.esc(item.label)}</div>
            `;
            button.addEventListener('click', () => {
                void insertVariableFromLibrary(item);
            });
            container.appendChild(button);
        });
    }

    function initializeEmailVariableLibrary() {
        const search = document.getElementById('email-variable-search');
        if (!search) {
            return;
        }
        if (!search.dataset.bound) {
            search.dataset.bound = '1';
            search.addEventListener('input', renderEmailVariableLibrary);
        }
        renderEmailVariableLibrary();
        bindEmailEditorTargets();

        const openBtn = document.getElementById('email-variable-open-btn');
        if (openBtn && !openBtn.dataset.bound) {
            openBtn.dataset.bound = '1';
            openBtn.addEventListener('click', openEmailVariableModal);
        }

        const closeBtn = document.getElementById('email-variable-close');
        if (closeBtn && !closeBtn.dataset.bound) {
            closeBtn.dataset.bound = '1';
            closeBtn.addEventListener('click', closeEmailVariableModal);
        }

        const modal = document.getElementById('email-variable-modal');
        if (modal && !modal.dataset.bound) {
            modal.dataset.bound = '1';
            modal.addEventListener('click', (event) => {
                if (event.target === modal) {
                    closeEmailVariableModal();
                }
            });
        }
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

    function buildEmailPreviewContext() {
        const jellygateURL = (document.getElementById('general-jellygate-url')?.value || '').trim();
        const jellyfinURL = (document.getElementById('general-jellyfin-url')?.value || '').trim();
        const jellyfinServerName = (document.getElementById('general-jellyfin-server-name')?.value || '').trim();
        const jellyseerrURL = (document.getElementById('general-jellyseerr-url')?.value || '').trim();
        const jellytrackURL = (document.getElementById('general-jellytrack-url')?.value || '').trim();
        return {
            JellyGateURL: jellygateURL || 'https://jellygate.example.com',
            JellyfinURL: jellyfinURL || 'https://jellyfin.example.com',
            JellyfinServerName: jellyfinServerName || 'Jellyfin',
            JellyseerrURL: jellyseerrURL || 'https://jellyseerr.example.com',
            JellyTrackURL: jellytrackURL || 'https://jellytrack.example.com',
            HelpURL: jellygateURL || jellyfinURL || 'https://jellygate.example.com',
            EmailLogoURL: resolveEmailLogoPreviewURL(),
        };
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

        const baseTemplateHeader = document.getElementById('tpl-base-header')?.value || '';
        const baseTemplateFooter = document.getElementById('tpl-base-footer')?.value || '';

        const res = await JG.api('/admin/api/settings/email-templates/preview', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                template: area.value || '',
                template_key: emailTemplateKeyFromId(area.id),
                language: activeEmailTemplatesLang || normalizeLangTag(document.getElementById('default-lang')?.value || '') || 'fr',
                base_template_header: baseTemplateHeader,
                base_template_footer: baseTemplateFooter,
                context: buildEmailPreviewContext(),
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
        return {
            template: t('email_base_preview_demo', 'You received an invitation to join the Jellyfin server.\n\nThe button and direct link appear automatically below this message.'),
            template_key: 'invitation',
            language: activeEmailTemplatesLang || normalizeLangTag(document.getElementById('default-lang')?.value || '') || 'fr',
            base_template_header: document.getElementById('tpl-base-header')?.value || '',
            base_template_footer: document.getElementById('tpl-base-footer')?.value || '',
            context: buildEmailPreviewContext(),
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
        const logo = document.getElementById('tpl-email-logo-url');
        if (!header || !footer) {
            return;
        }

        header.value = emailBaseDefaults.header || '';
        footer.value = emailBaseDefaults.footer || '';
        if (logo) {
            logo.value = '';
        }
        scheduleBaseLivePreview();
    }

    function resolveEmailLogoPreviewURL() {
        const configured = (document.getElementById('tpl-email-logo-url')?.value || '').trim();
        if (/^https?:\/\//i.test(configured)) {
            return configured;
        }

        const path = configured || '/static/img/logos/jellygate.svg';
        if (/^https?:\/\//i.test(path)) {
            return path;
        }

        const baseURL = (document.getElementById('general-jellygate-url')?.value || '').trim() || window.location.origin;
        if (!path.startsWith('/')) {
            return `${String(baseURL).replace(/\/+$/, '')}/${path}`;
        }
        return `${String(baseURL).replace(/\/+$/, '')}${path}`;
    }

    function readEmailTemplateSharedForm() {
        return extractEmailTemplateSharedConfig({
            base_template_header: document.getElementById('tpl-base-header').value,
            base_template_footer: document.getElementById('tpl-base-footer').value,
            email_logo_url: (document.getElementById('tpl-email-logo-url')?.value || '').trim(),
            disable_confirmation_email: !document.getElementById('tpl-enable-confirmation-email').checked,
            disable_expiry_reminder_emails: !document.getElementById('tpl-enable-expiry-reminder-email').checked,
            expiry_reminder_days: normalizeReminderDays(document.getElementById('tpl-expiry-reminder-days').value),
            disable_invite_expiry_email: !document.getElementById('tpl-enable-invite-expiry-email').checked,
            disable_user_creation_email: !document.getElementById('tpl-enable-user-creation-email').checked,
            disable_user_deletion_email: !document.getElementById('tpl-enable-user-deletion-email').checked,
            disable_user_disabled_email: !document.getElementById('tpl-enable-user-disabled-email').checked,
            disable_user_enabled_email: !document.getElementById('tpl-enable-user-enabled-email').checked,
            disable_user_expired_email: !document.getElementById('tpl-enable-user-expired-email').checked,
            disable_expiry_adjusted_email: !document.getElementById('tpl-enable-expiry-adjusted-email').checked,
            disable_welcome_email: !document.getElementById('tpl-enable-welcome-email').checked,
        });
    }

    function readEmailTemplateLocalizedForm() {
        return extractEmailTemplateLocalizedConfig({
            confirmation: document.getElementById('tpl-confirmation').value,
            confirmation_subject: document.getElementById('tpl-confirmation-subject').value.trim(),
            expiry_reminder: document.getElementById('tpl-expiry-reminder').value,
            expiry_reminder_subject: document.getElementById('tpl-expiry-reminder-subject').value.trim(),
            invitation: document.getElementById('tpl-invitation').value,
            invitation_subject: document.getElementById('tpl-invitation-subject').value.trim(),
            invite_expiry: document.getElementById('tpl-invite-expiry').value,
            invite_expiry_subject: document.getElementById('tpl-invite-expiry-subject').value.trim(),
            password_reset: document.getElementById('tpl-password-reset').value,
            password_reset_subject: document.getElementById('tpl-password-reset-subject').value.trim(),
            email_verification: document.getElementById('tpl-email-verification').value,
            email_verification_subject: document.getElementById('tpl-email-verification-subject').value.trim(),
            user_creation: document.getElementById('tpl-user-creation').value,
            user_creation_subject: document.getElementById('tpl-user-creation-subject').value.trim(),
            user_deletion: document.getElementById('tpl-user-deletion').value,
            user_deletion_subject: document.getElementById('tpl-user-deletion-subject').value.trim(),
            user_disabled: document.getElementById('tpl-user-disabled').value,
            user_disabled_subject: document.getElementById('tpl-user-disabled-subject').value.trim(),
            user_enabled: document.getElementById('tpl-user-enabled').value,
            user_enabled_subject: document.getElementById('tpl-user-enabled-subject').value.trim(),
            user_expired: document.getElementById('tpl-user-expired').value,
            user_expired_subject: document.getElementById('tpl-user-expired-subject').value.trim(),
            expiry_adjusted: document.getElementById('tpl-expiry-adjusted').value,
            expiry_adjusted_subject: document.getElementById('tpl-expiry-adjusted-subject').value.trim(),
            welcome: document.getElementById('tpl-welcome').value,
            welcome_subject: document.getElementById('tpl-welcome-subject').value.trim(),
        });
    }

    function readEmailTemplateForm() {
        return mergeEmailTemplateConfig(readEmailTemplateSharedForm(), readEmailTemplateLocalizedForm());
    }

    function applyEmailTemplateSharedForm(configValue) {
        const value = extractEmailTemplateSharedConfig(configValue);
        document.getElementById('tpl-base-header').value = value.base_template_header || '';
        document.getElementById('tpl-base-footer').value = value.base_template_footer || '';
        document.getElementById('tpl-email-logo-url').value = value.email_logo_url || '';
        document.getElementById('tpl-enable-confirmation-email').checked = !value.disable_confirmation_email;
        document.getElementById('tpl-enable-expiry-reminder-email').checked = !value.disable_expiry_reminder_emails;
        document.getElementById('tpl-expiry-reminder-days').value = value.expiry_reminder_days || 3;
        document.getElementById('tpl-enable-invite-expiry-email').checked = !value.disable_invite_expiry_email;
        document.getElementById('tpl-enable-user-creation-email').checked = !value.disable_user_creation_email;
        document.getElementById('tpl-enable-user-deletion-email').checked = !value.disable_user_deletion_email;
        document.getElementById('tpl-enable-user-disabled-email').checked = !value.disable_user_disabled_email;
        document.getElementById('tpl-enable-user-enabled-email').checked = !value.disable_user_enabled_email;
        document.getElementById('tpl-enable-user-expired-email').checked = !value.disable_user_expired_email;
        document.getElementById('tpl-enable-expiry-adjusted-email').checked = !value.disable_expiry_adjusted_email;
        document.getElementById('tpl-enable-welcome-email').checked = !value.disable_welcome_email;
        document.querySelectorAll('.email-template-item').forEach((item) => syncEmailTemplateCardState(item));
    }

    function applyEmailTemplateLocalizedForm(configValue) {
        const value = extractEmailTemplateLocalizedConfig(configValue);
        document.getElementById('tpl-confirmation').value = value.confirmation || '';
        document.getElementById('tpl-confirmation-subject').value = value.confirmation_subject || '';
        document.getElementById('tpl-expiry-reminder').value = value.expiry_reminder || '';
        document.getElementById('tpl-expiry-reminder-subject').value = value.expiry_reminder_subject || '';
        document.getElementById('tpl-invitation').value = value.invitation || '';
        document.getElementById('tpl-invitation-subject').value = value.invitation_subject || '';
        document.getElementById('tpl-invite-expiry').value = value.invite_expiry || '';
        document.getElementById('tpl-invite-expiry-subject').value = value.invite_expiry_subject || '';
        document.getElementById('tpl-password-reset').value = value.password_reset || '';
        document.getElementById('tpl-password-reset-subject').value = value.password_reset_subject || '';
        document.getElementById('tpl-email-verification').value = value.email_verification || '';
        document.getElementById('tpl-email-verification-subject').value = value.email_verification_subject || '';
        document.getElementById('tpl-user-creation').value = value.user_creation || '';
        document.getElementById('tpl-user-creation-subject').value = value.user_creation_subject || '';
        document.getElementById('tpl-user-deletion').value = value.user_deletion || '';
        document.getElementById('tpl-user-deletion-subject').value = value.user_deletion_subject || '';
        document.getElementById('tpl-user-disabled').value = value.user_disabled || '';
        document.getElementById('tpl-user-disabled-subject').value = value.user_disabled_subject || '';
        document.getElementById('tpl-user-enabled').value = value.user_enabled || '';
        document.getElementById('tpl-user-enabled-subject').value = value.user_enabled_subject || '';
        document.getElementById('tpl-user-expired').value = value.user_expired || '';
        document.getElementById('tpl-user-expired-subject').value = value.user_expired_subject || '';
        document.getElementById('tpl-expiry-adjusted').value = value.expiry_adjusted || '';
        document.getElementById('tpl-expiry-adjusted-subject').value = value.expiry_adjusted_subject || '';
        document.getElementById('tpl-welcome').value = value.welcome || '';
        document.getElementById('tpl-welcome-subject').value = value.welcome_subject || '';
        bindEmailEditorTargets();
    }

    function applyEmailTemplateForm(configValue) {
        applyEmailTemplateLocalizedForm(configValue);
    }

    function storeActiveEmailTemplateDraft() {
        if (!activeEmailTemplatesLang) {
            return;
        }
        loadedEmailSharedTemplateConfig = cloneEmailTemplateConfig(readEmailTemplateSharedForm());
        loadedEmailTemplatesByLang[activeEmailTemplatesLang] = cloneEmailTemplateConfig(readEmailTemplateLocalizedForm());
    }

    function ensureEmailTemplateForLanguage(lang) {
        const normalized = normalizeLangTag(lang);
        if (!normalized) {
            return '';
        }
        if (!loadedEmailTemplatesByLang[normalized]) {
            const defaultLang = normalizeLangTag(document.getElementById('default-lang')?.value || '');
            const seed = loadedEmailTemplatesByLang[defaultLang] || Object.values(loadedEmailTemplatesByLang)[0] || {};
            loadedEmailTemplatesByLang[normalized] = extractEmailTemplateLocalizedConfig(cloneEmailTemplateConfig(seed));
        }
        return normalized;
    }

    function syncEmailTemplateLanguageControls() {
        const select = document.getElementById('email-template-lang-select');
        if (select && activeEmailTemplatesLang) {
            select.value = activeEmailTemplatesLang;
        }
        const label = getActiveEmailLanguageLabel();
        document.querySelectorAll('[data-email-toolbar-lang]').forEach((node) => {
            node.textContent = label;
        });
    }

    function switchEmailTemplatesLanguage(lang) {
        const normalized = ensureEmailTemplateForLanguage(lang);
        if (!normalized) {
            return;
        }
        if (activeEmailTemplatesLang && activeEmailTemplatesLang !== normalized) {
            storeActiveEmailTemplateDraft();
        }
        activeEmailTemplatesLang = normalized;
        applyEmailTemplateLocalizedForm(loadedEmailTemplatesByLang[activeEmailTemplatesLang]);
        syncEmailTemplateLanguageControls();
        document.querySelectorAll('#form-email-templates textarea[id^="tpl-"]').forEach((area) => scheduleLiveTemplateValidation(area));
        scheduleBaseLivePreview();
    }

    function renderEmailTemplateLanguageControls(defaultLang) {
        const options = getEmailTemplateLanguageOptions();
        const select = document.getElementById('email-template-lang-select');
        if (!select) {
            return;
        }

        select.innerHTML = '';
        options.forEach((option) => {
            const opt = document.createElement('option');
            opt.value = option.value;
            opt.textContent = option.label;
            select.appendChild(opt);
        });

        if (!select.dataset.bound) {
            select.dataset.bound = '1';
            select.addEventListener('change', () => switchEmailTemplatesLanguage(select.value));
        }

        const fallbackLang = normalizeLangTag(defaultLang) || options[0]?.value || 'fr';
        if (!activeEmailTemplatesLang) {
            activeEmailTemplatesLang = fallbackLang;
        }
        switchEmailTemplatesLanguage(activeEmailTemplatesLang);
    }

    function switchTab(name) {
        const emailPanel = document.getElementById('panel-email-templates');
        if (emailPanel && !emailPanel.classList.contains('hidden')) {
            storeActiveEmailTemplateDraft();
        }
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
    }

    function setBackupMode(databaseType) {
        const normalized = String(databaseType || '').trim().toLowerCase() || 'sqlite';
        backupDatabaseType = normalized;
    }

    async function loadSettings() {
        const res = await JG.api('/admin/api/settings');
        if (!res || !res.success) {
            return;
        }
        const data = res.data || {};
        const ldap = data.ldap || {};
        const smtp = data.smtp || {};
        const webhooks = data.webhooks || {};
        const discordWebhook = webhooks.discord || {};
        const telegramWebhook = webhooks.telegram || {};
        const matrixWebhook = webhooks.matrix || {};
        setBackupMode(data.database_type || 'sqlite');

        document.getElementById('default-lang').value = data.default_lang || 'fr';
        const links = data.portal_links || {};
        document.getElementById('general-jellygate-url').value = links.jellygate_url || '';
        document.getElementById('general-jellyfin-url').value = links.jellyfin_url || '';
        document.getElementById('general-jellyfin-server-name').value = links.jellyfin_server_name || 'Jellyfin';
        if (!links.jellyfin_server_name || links.jellyfin_server_name === 'Jellyfin') {
            void (async () => {
                const res = await JG.api('/admin/api/settings/general/fetch-server-name', { method: 'POST' });
                if (res && res.success && res.data && res.data.server_name) {
                    const input = document.getElementById('general-jellyfin-server-name');
                    if (input && (!input.value || input.value === 'Jellyfin')) {
                        input.value = res.data.server_name;
                        scheduleBaseLivePreview();
                    }
                }
            })();
        }
        document.getElementById('general-jellyseerr-url').value = links.jellyseerr_url || '';
        document.getElementById('general-jellytrack-url').value = links.jellytrack_url || '';
        refreshPortalShortcuts(links);

        applyInvitationProfileConfig(data.invitation_profile || {});

        currentLDAPConfig = { ...ldap };
        document.getElementById('ldap-enabled').checked = ldap.enabled || false;
        document.getElementById('ldap-host').value = ldap.host || '';
        document.getElementById('ldap-port').value = ldap.port || 636;
        document.getElementById('ldap-tls').checked = ldap.use_tls !== false;
        document.getElementById('ldap-skip-verify').checked = ldap.skip_verify || false;
        document.getElementById('ldap-bind-dn').value = ldap.bind_dn || '';
        document.getElementById('ldap-bind-password').value = ldap.bind_password || '';
        document.getElementById('ldap-base-dn').value = ldap.base_dn || '';
        document.getElementById('ldap-search-filter').value = ldap.search_filter || '';
        document.getElementById('ldap-search-attributes').value = ldap.search_attributes || 'uid,sAMAccountName,cn,userPrincipalName,mail';
        document.getElementById('ldap-uid-attribute').value = ldap.uid_attribute || 'uid';
        document.getElementById('ldap-username-attribute').value = ldap.username_attribute || 'auto';
        document.getElementById('ldap-admin-filter').value = ldap.admin_filter || '';
        document.getElementById('ldap-admin-filter-memberuid').checked = !!ldap.admin_filter_memberuid;
        toggleLDAPFields();

        document.getElementById('smtp-host').value = smtp.host || '';
        document.getElementById('smtp-port').value = smtp.port || 587;
        document.getElementById('smtp-username').value = smtp.username || '';
        document.getElementById('smtp-password').value = smtp.password || '';
        document.getElementById('smtp-from').value = smtp.from || '';
        document.getElementById('smtp-tls').checked = smtp.use_tls !== false;

        document.getElementById('wh-discord-url').value = discordWebhook.url || '';
        document.getElementById('wh-telegram-token').value = telegramWebhook.token || '';
        document.getElementById('wh-telegram-chat').value = telegramWebhook.chat_id || '';
        document.getElementById('wh-matrix-url').value = matrixWebhook.url || '';
        document.getElementById('wh-matrix-room').value = matrixWebhook.room_id || '';
        document.getElementById('wh-matrix-token').value = matrixWebhook.token || '';

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

        loadedEmailTemplatesByLang = {};
        loadedEmailSharedTemplateConfig = getDefaultEmailSharedTemplateConfig();
        const templatesByLang = data.email_templates_by_lang || {};
        Object.entries(templatesByLang).forEach(([rawLang, value]) => {
            const lang = normalizeLangTag(rawLang);
            if (!lang || !emailLanguageOrder.includes(lang) || !value || typeof value !== 'object') {
                return;
            }
            loadedEmailTemplatesByLang[lang] = extractEmailTemplateLocalizedConfig(value);
        });

        const defaultEmailLang = normalizeLangTag(data.default_lang || '') || 'fr';
        const sharedSource = templatesByLang[defaultEmailLang] || data.email_templates || Object.values(templatesByLang)[0] || {};
        loadedEmailSharedTemplateConfig = extractEmailTemplateSharedConfig(sharedSource);
        applyEmailTemplateSharedForm(loadedEmailSharedTemplateConfig);
        if (Object.keys(loadedEmailTemplatesByLang).length === 0 && data.email_templates) {
            loadedEmailTemplatesByLang[defaultEmailLang] = extractEmailTemplateLocalizedConfig(data.email_templates);
        }
        if (!loadedEmailTemplatesByLang[defaultEmailLang] && data.email_templates) {
            loadedEmailTemplatesByLang[defaultEmailLang] = extractEmailTemplateLocalizedConfig(data.email_templates);
        }

        activeEmailTemplatesLang = defaultEmailLang;
        renderEmailTemplateLanguageControls(defaultEmailLang);

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
                jellyfin_server_name: document.getElementById('general-jellyfin-server-name').value.trim(),
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
            storeActiveEmailTemplateDraft();
            if (!activeEmailTemplatesLang) {
                activeEmailTemplatesLang = normalizeLangTag(document.getElementById('default-lang')?.value || '') || 'fr';
            }
            loadedEmailSharedTemplateConfig = cloneEmailTemplateConfig(readEmailTemplateSharedForm());
            if (!loadedEmailTemplatesByLang[activeEmailTemplatesLang]) {
                loadedEmailTemplatesByLang[activeEmailTemplatesLang] = cloneEmailTemplateConfig(readEmailTemplateLocalizedForm());
            }
            const templatesByLang = {};
            Object.entries(loadedEmailTemplatesByLang).forEach(([lang, localized]) => {
                templatesByLang[lang] = mergeEmailTemplateConfig(loadedEmailSharedTemplateConfig, localized);
            });
            body = {
                language: activeEmailTemplatesLang,
                templates_by_lang: templatesByLang,
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

        const baseTemplateHeader = document.getElementById('tpl-base-header')?.value || '';
        const baseTemplateFooter = document.getElementById('tpl-base-footer')?.value || '';

        const res = await JG.api('/admin/api/settings/email-templates/preview', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                template,
                template_key: emailTemplateKeyFromId(templateId),
                language: activeEmailTemplatesLang || normalizeLangTag(document.getElementById('default-lang')?.value || '') || 'fr',
                base_template_header: baseTemplateHeader,
                base_template_footer: baseTemplateFooter,
                context: buildEmailPreviewContext(),
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

    function syncEmailTemplateCardState(item) {
        if (!item) {
            return;
        }
        const toggleId = item.dataset.emailToggleId || '';
        const toggle = toggleId ? document.getElementById(toggleId) : null;
        const badge = item.querySelector('[data-email-status]');
        const enabled = !toggle || !!toggle.checked;
        item.classList.toggle('email-template-item-disabled', !enabled);
        if (badge) {
            badge.textContent = enabled
                ? t('settings_email_status_enabled', 'Enabled')
                : t('settings_email_status_disabled', 'Disabled');
            badge.className = enabled
                ? 'rounded-full border border-emerald-400/30 bg-emerald-500/10 px-2.5 py-1 text-[10px] font-black uppercase tracking-[0.18em] text-emerald-200'
                : 'rounded-full border border-rose-400/30 bg-rose-500/10 px-2.5 py-1 text-[10px] font-black uppercase tracking-[0.18em] text-rose-200';
        }
    }

    function initializeEmailTemplateEditors() {
        document.querySelectorAll('.email-template-item').forEach((item) => {
            const summary = item.querySelector('.email-template-summary');
            const title = item.querySelector('.email-template-summary-title');
            const panel = item.querySelector('.email-template-panel');
            if (!summary || !title || !panel) {
                return;
            }

            if (!summary.querySelector('[data-email-desc]')) {
                const desc = document.createElement('span');
                desc.dataset.emailDesc = '1';
                desc.className = 'mt-1 block text-xs font-medium text-jg-text-muted/75 normal-case tracking-normal';
                desc.textContent = item.dataset.emailTemplateDesc || '';
                title.insertAdjacentElement('afterend', desc);
            }

            if (!summary.querySelector('[data-email-status]')) {
                const status = document.createElement('span');
                status.dataset.emailStatus = '1';
                summary.appendChild(status);
            }

            if (!panel.querySelector('[data-email-toolbar]')) {
                const toolbar = document.createElement('div');
                toolbar.dataset.emailToolbar = '1';
                toolbar.className = 'mb-4 flex flex-col gap-3 rounded-xl border border-white/10 bg-black/20 px-4 py-3 xl:flex-row xl:items-center xl:justify-between';

                const copy = document.createElement('div');
                copy.className = 'space-y-1';
                copy.innerHTML = `
                    <div data-email-toolbar-lang class="text-[10px] font-black uppercase tracking-[0.2em] text-jg-text-muted/70">${JG.esc(getActiveEmailLanguageLabel())}</div>
                    <div class="text-sm text-slate-300">${JG.esc(item.dataset.emailTemplateDesc || '')}</div>
                `;

                const actions = document.createElement('div');
                actions.className = 'flex flex-wrap items-center gap-2';

                const previewField = item.dataset.emailPreviewField || '';
                if (previewField) {
                    const btn = document.createElement('button');
                    btn.type = 'button';
                    btn.className = 'jg-btn jg-btn-sm jg-btn-ghost';
                    btn.textContent = t('preview_button', 'Preview');
                    btn.addEventListener('click', () => previewEmailTemplate(previewField));
                    actions.appendChild(btn);
                }

                const toggleId = item.dataset.emailToggleId || '';
                if (toggleId && !document.getElementById(toggleId)) {
                    const label = document.createElement('label');
                    label.className = 'inline-flex items-center gap-2 rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-xs font-semibold text-slate-200';
                    const input = document.createElement('input');
                    input.type = 'checkbox';
                    input.id = toggleId;
                    input.className = 'form-checkbox';
                    input.addEventListener('change', () => syncEmailTemplateCardState(item));
                    const text = document.createElement('span');
                    text.textContent = t('settings_email_toggle_send', 'Send this email');
                    label.append(input, text);
                    actions.appendChild(label);
                }

                toolbar.append(copy, actions);
                panel.prepend(toolbar);
            }
        });

        document.querySelectorAll('#form-email-templates textarea[id^="tpl-"]').forEach((area) => {
            getTemplateValidationBox(area);
            if (!area.dataset.validationBound) {
                area.dataset.validationBound = '1';
                area.addEventListener('input', () => scheduleLiveTemplateValidation(area));
            }
            scheduleLiveTemplateValidation(area);
        });

        bindEmailEditorTargets();
        initializeEmailVariableLibrary();
        document.querySelectorAll('.email-template-item').forEach((item) => syncEmailTemplateCardState(item));

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
        const logoField = document.getElementById('tpl-email-logo-url');
        [baseHeader, baseFooter, logoField].forEach((field) => {
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

    function backupSourceLabel(backup) {
        const source = String((backup && backup.source) || 'unknown').trim().toLowerCase() || 'unknown';
        return t(`backup_source_${source}`, (backup && backup.display_label) || source);
    }

    function renderBackupArchiveCell(backup) {
        const legacy = backup && backup.is_legacy_name
            ? `<span class="rounded-full border border-amber-400/30 bg-amber-500/10 px-2 py-0.5 text-[9px] font-black uppercase tracking-[0.16em] text-amber-200">${JG.esc(t('backup_legacy_name', 'Legacy name'))}</span>`
            : '';
        return `
            <div class="flex flex-col gap-1">
                <div class="flex flex-wrap items-center gap-2">
                    <span class="font-semibold text-slate-100">${JG.esc(backupSourceLabel(backup))}</span>
                    ${legacy}
                </div>
                <span class="font-mono text-[11px] text-slate-400">${JG.esc(backup.name)}</span>
            </div>
        `;
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
        <td>${renderBackupArchiveCell(backup)}</td>
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

        const agreed = await JG.confirm(
            t('backup_action_restore', 'Restore'),
            `${t('backup_restore_confirm_prefix', 'Prepare restoration for')} ${name}? ${confirmSuffix}`,
            { confirmLabel: t('backup_action_restore', 'Restore') },
        );
        if (!agreed) {
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
        const agreed = await JG.confirm(
            t('backup_action_delete', 'Delete'),
            `${t('backup_delete_confirm_prefix', 'Delete permanently')} ${name}?`,
            { confirmLabel: t('backup_action_delete', 'Delete'), danger: true },
        );
        if (!agreed) {
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

        initializeEmailTemplateEditors();
        await loadSettings();
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
        document.getElementById('btn-fetch-server-name')?.addEventListener('click', async () => {
            const btn = document.getElementById('btn-fetch-server-name');
            if (btn) btn.disabled = true;
            try {
                const res = await JG.api('/admin/api/settings/general/fetch-server-name', { method: 'POST' });
                if (res && res.success && res.data && res.data.server_name) {
                    const input = document.getElementById('general-jellyfin-server-name');
                    if (input) {
                        input.value = res.data.server_name;
                        JG.toast(t('settings_server_name_fetched', 'Server name fetched: {name}').replace('{name}', res.data.server_name), 'success');
                        scheduleBaseLivePreview();
                    }
                } else {
                    JG.toast((res && res.message) || 'Failed to fetch server name', 'error');
                }
            } finally {
                if (btn) btn.disabled = false;
            }
        });
    });
})();
