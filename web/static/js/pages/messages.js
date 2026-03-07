(() => {
    const config = window.JGPageMessages || {};
    const i18n = config.i18n || {};
    const uiLocale = config.uiLocale || undefined;
    const isAdmin = !!config.isAdmin;

    function bindSidebarToggle() {
        const toggle = document.getElementById('sidebar-toggle');
        if (!toggle) {
            return;
        }
        toggle.addEventListener('click', () => {
            const sidebar = document.getElementById('sidebar');
            if (sidebar) {
                sidebar.classList.toggle('open');
            }
        });
    }

    function formatDateTime(value) {
        if (!value) {
            return '—';
        }
        const date = new Date(value);
        if (Number.isNaN(date.getTime())) {
            return value;
        }
        return date.toLocaleString(uiLocale);
    }

    function translateTarget(group) {
        if (group === 'active') return i18n.targetActive;
        if (group === 'inactive') return i18n.targetInactive;
        if (group === 'inviters') return i18n.targetInviters;
        return i18n.targetAll;
    }

    function translateChannels(channels) {
        const labels = (channels || []).map((channel) => {
            if (channel === 'email') return i18n.channelEmail;
            return i18n.defaultChannel;
        }).filter(Boolean);
        return labels.length ? labels.join(', ') : i18n.defaultChannel;
    }

    function updateSummary(rows) {
        const total = document.getElementById('messages-total');
        const campaigns = document.getElementById('messages-campaigns');
        const channels = document.getElementById('messages-channels-summary');
        const channelSet = new Set();

        rows.forEach((message) => {
            (message.channels || []).forEach((channel) => {
                if (channel === 'email') {
                    channelSet.add(channel);
                }
            });
        });

        if (total) total.textContent = String(rows.length);
        if (campaigns) campaigns.textContent = String(rows.filter((message) => !!message.is_campaign).length);
        if (channels) channels.textContent = channelSet.size ? translateChannels(Array.from(channelSet)) : i18n.defaultChannel;
    }

    function channelsForPayload() {
        const channels = [];
        if (document.getElementById('msg-ch-email')?.checked) {
            channels.push('email');
        }
        return channels;
    }

    function parseCSVIds(raw) {
        return (raw || '')
            .split(',')
            .map((value) => parseInt(value.trim(), 10))
            .filter((value) => Number.isInteger(value) && value > 0);
    }

    async function loadMessages() {
        const res = await JG.api('/admin/api/messages?view=admin');
        if (!res.success) {
            JG.toast(res.message || i18n.errorLoading, 'error');
            return;
        }

        const rows = Array.isArray(res.data) ? res.data : [];
        updateSummary(rows);

        const tbody = document.getElementById('messages-body');
        if (!tbody) {
            return;
        }

        if (rows.length === 0) {
            tbody.innerHTML = `<tr><td colspan="5" class="text-center text-slate-500 py-10">${JG.esc(i18n.noMessages)}</td></tr>`;
            return;
        }

        tbody.innerHTML = rows.map((message) => {
            const target = translateTarget(message.target_group || 'all') + (message.target_user_ids && message.target_user_ids.length ? ` + ${message.target_user_ids.length} ${i18n.usersSuffix}` : '');
            const channels = translateChannels(message.channels || []);
            const campaignBadge = message.is_campaign ? `<span class="badge badge-muted ml-2">${JG.esc(i18n.campaignMode)}</span>` : '';
            const createdAt = formatDateTime(message.created_at || '');

            return `<tr>
                <td>
                    <div class="font-semibold">${JG.esc(message.title || '')}${campaignBadge}</div>
                    <div class="text-xs text-slate-400 mt-1">${JG.esc(message.body || '')}</div>
                </td>
                <td class="text-sm text-slate-300">${JG.esc(target)}</td>
                <td class="text-sm text-slate-400">${JG.esc(channels)}</td>
                <td class="text-sm text-slate-500">${JG.esc(createdAt)}</td>
                <td class="text-right">
                    <button class="jg-btn jg-btn-sm jg-btn-danger" data-action="delete" data-id="${message.id}">${JG.esc(i18n.delete)}</button>
                </td>
            </tr>`;
        }).join('');
    }

    document.addEventListener('DOMContentLoaded', () => {
        bindSidebarToggle();
        if (!isAdmin) {
            return;
        }

        document.getElementById('messages-body')?.addEventListener('click', async (event) => {
            const button = event.target.closest('button');
            if (!button || button.dataset.action !== 'delete') {
                return;
            }

            if (!confirm(i18n.confirmDelete)) {
                return;
            }

            const res = await JG.api(`/admin/api/messages/${button.dataset.id}`, { method: 'DELETE' });
            if (!res.success) {
                JG.toast(res.message || i18n.deleteFailed, 'error');
                return;
            }

            JG.toast(i18n.deleted, 'success');
            await loadMessages();
        });

        const createForm = document.getElementById('message-create-form');
        if (createForm) {
            createForm.addEventListener('submit', async (event) => {
                event.preventDefault();

                const payload = {
                    title: document.getElementById('msg-title').value.trim(),
                    body: document.getElementById('msg-body').value.trim(),
                    target_group: document.getElementById('msg-target-group').value,
                    target_user_ids: parseCSVIds(document.getElementById('msg-target-users').value),
                    channels: channelsForPayload(),
                    is_campaign: document.getElementById('msg-campaign').checked,
                    starts_at: document.getElementById('msg-starts-at').value,
                    ends_at: document.getElementById('msg-ends-at').value,
                };

                if (!payload.channels.length) {
                    JG.toast(i18n.createFailed, 'error');
                    return;
                }

                const res = await JG.api('/admin/api/messages', {
                    method: 'POST',
                    body: JSON.stringify(payload),
                });
                if (!res.success) {
                    JG.toast(res.message || i18n.createFailed, 'error');
                    return;
                }

                JG.toast(i18n.created, 'success');
                createForm.reset();
                const emailCheckbox = document.getElementById('msg-ch-email');
                if (emailCheckbox) {
                    emailCheckbox.checked = true;
                }
                await loadMessages();
            });
        }

        loadMessages();
    });
})();