(() => {
    const $ = (id) => document.getElementById(id);

    const defaults = {
        anti_abuse: { enabled: true, captcha: true, max_failures: 5, window_minutes: 15, block_minutes: 20 },
        content: { invite_intro_markdown: '', invite_success_markdown: '', account_markdown: '' },
        setup: { enabled: true, completed: false, completed_at: '' },
        contacts: { enabled: true, allow_discord: true, allow_telegram: true, allow_matrix: true, require_proof: false },
        admin_timeline: { enabled: true, limit: 80 },
        simulation: { enabled: true, require_preview_before_bulk: false },
        portal_health: { enabled: true },
        sponsorship: { enabled: true },
        lifecycle: { enabled: true, expiry_reminder_days: [14, 7, 1], disable_inactive_days: 0, delete_disabled_after_days: 0 },
        audit: { enabled: true, before_after: true, retention_days: 365 },
    };

    let currentConfig = structuredClone(defaults);

    function numberValue(id, fallback) {
        const el = $(id);
        if (!el) return fallback;
        const parsed = Number.parseInt(el.value, 10);
        return Number.isFinite(parsed) ? parsed : fallback;
    }

    function checked(id, fallback = false) {
        const el = $(id);
        return el ? !!el.checked : fallback;
    }

    function setChecked(id, value) {
        const el = $(id);
        if (el) el.checked = !!value;
    }

    function setValue(id, value) {
        const el = $(id);
        if (el) el.value = value ?? '';
    }

    function applyConfig(cfg) {
        currentConfig = { ...structuredClone(defaults), ...(cfg || {}) };
        currentConfig.anti_abuse = { ...defaults.anti_abuse, ...(currentConfig.anti_abuse || {}) };
        currentConfig.content = { ...defaults.content, ...(currentConfig.content || {}) };
        currentConfig.setup = { ...defaults.setup, ...(currentConfig.setup || {}) };
        currentConfig.contacts = { ...defaults.contacts, ...(currentConfig.contacts || {}) };
        currentConfig.admin_timeline = { ...defaults.admin_timeline, ...(currentConfig.admin_timeline || {}) };
        currentConfig.simulation = { ...defaults.simulation, ...(currentConfig.simulation || {}) };
        currentConfig.portal_health = { ...defaults.portal_health, ...(currentConfig.portal_health || {}) };
        currentConfig.sponsorship = { ...defaults.sponsorship, ...(currentConfig.sponsorship || {}) };
        currentConfig.lifecycle = { ...defaults.lifecycle, ...(currentConfig.lifecycle || {}) };
        currentConfig.audit = { ...defaults.audit, ...(currentConfig.audit || {}) };

        setChecked('anti-enabled', currentConfig.anti_abuse.enabled);
        setChecked('anti-captcha', currentConfig.anti_abuse.captcha);
        setValue('anti-max-failures', currentConfig.anti_abuse.max_failures);
        setValue('anti-window', currentConfig.anti_abuse.window_minutes);
        setValue('anti-block', currentConfig.anti_abuse.block_minutes);

        setValue('content-invite-intro', currentConfig.content.invite_intro_markdown);
        setValue('content-invite-success', currentConfig.content.invite_success_markdown);
        setValue('content-account', currentConfig.content.account_markdown);

        setChecked('setup-enabled', currentConfig.setup.enabled);
        setChecked('setup-completed', currentConfig.setup.completed);
        setChecked('contacts-enabled', currentConfig.contacts.enabled);
        setChecked('contacts-proof', currentConfig.contacts.require_proof);
        setChecked('timeline-enabled', currentConfig.admin_timeline.enabled);
        setChecked('simulation-enabled', currentConfig.simulation.enabled);
        setChecked('health-enabled', currentConfig.portal_health.enabled);
        setChecked('sponsorship-enabled', currentConfig.sponsorship.enabled);
        setChecked('lifecycle-enabled', currentConfig.lifecycle.enabled);
        setChecked('audit-enabled', currentConfig.audit.enabled);
        setValue('timeline-limit', currentConfig.admin_timeline.limit);
        setValue('lifecycle-delete-disabled', currentConfig.lifecycle.delete_disabled_after_days);
        setValue('audit-retention', currentConfig.audit.retention_days);
    }

    function collectConfig() {
        const completed = checked('setup-completed');
        const completedAt = completed
            ? (currentConfig.setup.completed_at || new Date().toISOString())
            : '';

        return {
            anti_abuse: {
                enabled: checked('anti-enabled', true),
                captcha: checked('anti-captcha', true),
                max_failures: numberValue('anti-max-failures', 5),
                window_minutes: numberValue('anti-window', 15),
                block_minutes: numberValue('anti-block', 20),
            },
            content: {
                invite_intro_markdown: $('content-invite-intro')?.value || '',
                invite_success_markdown: $('content-invite-success')?.value || '',
                account_markdown: $('content-account')?.value || '',
            },
            setup: {
                enabled: checked('setup-enabled', true),
                completed,
                completed_at: completedAt,
            },
            contacts: {
                enabled: checked('contacts-enabled', true),
                allow_discord: true,
                allow_telegram: true,
                allow_matrix: true,
                require_proof: checked('contacts-proof', false),
            },
            admin_timeline: {
                enabled: checked('timeline-enabled', true),
                limit: numberValue('timeline-limit', 80),
            },
            simulation: {
                enabled: checked('simulation-enabled', true),
                require_preview_before_bulk: currentConfig.simulation.require_preview_before_bulk || false,
            },
            portal_health: { enabled: checked('health-enabled', true) },
            sponsorship: { enabled: checked('sponsorship-enabled', true) },
            lifecycle: {
                enabled: checked('lifecycle-enabled', true),
                expiry_reminder_days: currentConfig.lifecycle.expiry_reminder_days || [14, 7, 1],
                disable_inactive_days: currentConfig.lifecycle.disable_inactive_days || 0,
                delete_disabled_after_days: numberValue('lifecycle-delete-disabled', 0),
            },
            audit: {
                enabled: checked('audit-enabled', true),
                before_after: currentConfig.audit.before_after !== false,
                retention_days: numberValue('audit-retention', 365),
            },
        };
    }

    async function loadConfig() {
        const res = await JG.api('/admin/api/product/config');
        if (!res.success) {
            JG.toast(res.message || 'Impossible de charger la configuration produit', 'error');
            return;
        }
        applyConfig(res.data || defaults);
    }

    async function saveConfig(event) {
        event.preventDefault();
        const payload = collectConfig();
        const res = await JG.api('/admin/api/product/config', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
        if (!res.success) {
            JG.toast(res.message || 'Sauvegarde impossible', 'error');
            return;
        }
        applyConfig(res.data || payload);
        JG.toast(res.message || 'Configuration produit sauvegardée', 'success');
        await Promise.all([loadHealth(), loadLifecycle(), loadTimeline()]);
    }

    async function previewMarkdown() {
        const preview = $('markdown-preview');
        if (!preview) return;
        const markdown = $('content-invite-intro')?.value || '';
        const res = await JG.api('/admin/api/product/markdown-preview', {
            method: 'POST',
            body: JSON.stringify({ markdown }),
        });
        if (!res.success) {
            JG.toast(res.message || 'Aperçu impossible', 'error');
            return;
        }
        preview.innerHTML = (res.data && res.data.html) || '<p>Aperçu vide.</p>';
        preview.classList.remove('hidden');
    }

    function statusClass(status) {
        if (status === 'ok') return 'text-emerald-400 bg-emerald-500/10 border-emerald-500/20';
        if (status === 'warn') return 'text-amber-300 bg-amber-500/10 border-amber-500/20';
        return 'text-rose-300 bg-rose-500/10 border-rose-500/20';
    }

    async function loadHealth() {
        const list = $('product-health-list');
        if (!list) return;
        list.innerHTML = '<div class="text-sm text-jg-text-muted">Chargement...</div>';
        const res = await JG.api('/admin/api/product/health');
        if (!res.success) {
            list.innerHTML = `<div class="text-sm text-rose-300">${JG.esc(res.message || 'Erreur santé')}</div>`;
            return;
        }
        const checks = Array.isArray(res.data) ? res.data : [];
        list.innerHTML = checks.map(check => `
            <div class="rounded-xl border ${statusClass(check.status)} px-4 py-3">
                <div class="flex items-center justify-between gap-3">
                    <div class="font-bold text-sm">${JG.esc(check.label)}</div>
                    <div class="text-[10px] uppercase tracking-widest font-black">${JG.esc(check.status)}</div>
                </div>
                <div class="text-xs opacity-80 mt-1">${JG.esc(check.message || '')}</div>
            </div>
        `).join('');
    }

    function statTile(label, value) {
        return `
            <div class="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-3">
                <div class="text-[10px] uppercase tracking-widest text-jg-text-muted font-bold">${JG.esc(label)}</div>
                <div class="text-xl font-black text-jg-text mt-1">${JG.esc(String(value ?? '-'))}</div>
            </div>
        `;
    }

    async function loadLifecycle() {
        const target = $('product-lifecycle');
        if (!target) return;
        const res = await JG.api('/admin/api/product/lifecycle');
        if (!res.success) {
            target.innerHTML = statTile('Erreur', res.message || 'Lecture impossible');
            return;
        }
        const data = res.data || {};
        target.innerHTML = [
            statTile('Expirés à traiter', data.expired_due || 0),
            statTile('Suppressions dues', data.delete_due || 0),
            statTile('Désactivés en attente', data.disable_then_delete_pending || 0),
            statTile('Actifs sans expiration', data.active_without_expiry || 0),
        ].join('');
    }

    async function loadSponsorship() {
        const target = $('product-sponsorship');
        if (!target) return;
        const res = await JG.api('/admin/api/invitations/stats');
        if (!res.success) {
            target.innerHTML = statTile('Erreur', res.message || 'Lecture impossible');
            return;
        }
        const data = res.data || {};
        target.innerHTML = [
            statTile('Liens', data.total_links || 0),
            statTile('Actifs', data.active_links || 0),
            statTile('Conversions', data.conversions || 0),
            statTile('Taux', `${Number(data.conversion_rate || 0).toFixed(1)}%`),
        ].join('');
    }

    async function loadTimeline() {
        const target = $('product-timeline');
        if (!target) return;
        target.innerHTML = '<div class="text-sm text-jg-text-muted">Chargement...</div>';
        const res = await JG.api('/admin/api/product/timeline');
        if (!res.success) {
            target.innerHTML = `<div class="text-sm text-rose-300">${JG.esc(res.message || 'Timeline indisponible')}</div>`;
            return;
        }
        const events = Array.isArray(res.data) ? res.data : [];
        if (events.length === 0) {
            target.innerHTML = '<div class="text-sm text-jg-text-muted">Aucun événement.</div>';
            return;
        }
        target.innerHTML = events.map(event => {
            const when = event.at ? new Date(event.at) : null;
            const whenText = when && !Number.isNaN(when.getTime()) ? when.toLocaleString() : (event.at || '');
            return `
                <article class="rounded-xl border border-white/10 bg-white/[0.035] px-4 py-3">
                    <div class="flex items-start justify-between gap-3">
                        <div class="text-sm font-bold text-jg-text">${JG.esc(event.message || event.action || '-')}</div>
                        <span class="text-[10px] uppercase tracking-widest text-jg-accent font-black">${JG.esc(event.severity || 'info')}</span>
                    </div>
                    <div class="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-jg-text-muted">
                        <span>${JG.esc(whenText)}</span>
                        ${event.actor ? `<span>par ${JG.esc(event.actor)}</span>` : ''}
                        ${event.target ? `<span>cible ${JG.esc(event.target)}</span>` : ''}
                    </div>
                </article>
            `;
        }).join('');
    }

    document.addEventListener('DOMContentLoaded', async () => {
        $('product-config-form')?.addEventListener('submit', saveConfig);
        $('btn-preview-markdown')?.addEventListener('click', previewMarkdown);
        $('btn-refresh-health')?.addEventListener('click', loadHealth);
        $('btn-refresh-timeline')?.addEventListener('click', loadTimeline);

        await loadConfig();
        await Promise.all([loadHealth(), loadLifecycle(), loadSponsorship(), loadTimeline()]);
    });
})();
