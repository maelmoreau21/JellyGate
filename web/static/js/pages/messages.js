(() => {
    const config = window.JGPageMessages || {};
    const i18n = config.i18n || {};
    const uiLocale = config.uiLocale || undefined;
    const isAdmin = !!config.isAdmin;
    let currentView = 'inbox';

    function formatDateTime(value) {
        if (!value) {
            return '';
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
        if (group === 'admins') return i18n.targetAdmins;
        if (group === 'inviters') return i18n.targetInviters;
        return i18n.targetAll;
    }

    function translateChannels(channels) {
        const labels = (channels || []).map((channel) => {
            if (channel === 'email') return i18n.channelEmail;
            if (channel === 'discord') return i18n.channelDiscord;
            if (channel === 'telegram') return i18n.channelTelegram;
            return i18n.defaultChannel;
        });
        return labels.length ? labels.join(', ') : i18n.defaultChannel;
    }

    function updateViewButtons() {
        const inboxBtn = document.getElementById('btn-view-inbox');
        const adminBtn = document.getElementById('btn-view-admin');
        if (inboxBtn) inboxBtn.classList.toggle('jg-btn-primary', currentView === 'inbox');
        if (inboxBtn) inboxBtn.classList.toggle('jg-btn-ghost', currentView !== 'inbox');
        if (adminBtn) adminBtn.classList.toggle('jg-btn-primary', currentView === 'admin');
        if (adminBtn) adminBtn.classList.toggle('jg-btn-ghost', currentView !== 'admin');
        const viewLabel = document.getElementById('messages-view-label');
        if (viewLabel) viewLabel.textContent = currentView === 'admin' ? i18n.adminView : i18n.inbox;
    }

    function updateSummary(rows) {
        const unreadCount = rows.filter((message) => !message.read).length;
        const channelSet = new Set();
        rows.forEach((message) => (message.channels || []).forEach((channel) => channelSet.add(channel)));
        document.getElementById('messages-total').textContent = String(rows.length);
        document.getElementById('messages-unread').textContent = String(unreadCount);
        document.getElementById('messages-channels-summary').textContent = channelSet.size ? translateChannels(Array.from(channelSet)) : i18n.defaultChannel;
    }

    function channelsForPayload() {
        const channels = ['in_app'];
        if (document.getElementById('msg-ch-email')?.checked) channels.push('email');
        if (document.getElementById('msg-ch-discord')?.checked) channels.push('discord');
        if (document.getElementById('msg-ch-telegram')?.checked) channels.push('telegram');
        return channels;
    }

    function parseCSVIds(raw) {
        return (raw || '')
            .split(',')
            .map((value) => parseInt(value.trim(), 10))
            .filter((value) => Number.isInteger(value) && value > 0);
    }

    async function loadMessages() {
        const res = await JG.api(`/admin/api/messages?view=${encodeURIComponent(currentView)}`);
        if (!res.success) {
            JG.toast(res.message || i18n.errorLoading, 'error');
            return;
        }

        const rows = Array.isArray(res.data) ? res.data : [];
        updateViewButtons();
        updateSummary(rows);
        const tbody = document.getElementById('messages-body');
        if (!tbody) {
            return;
        }
        if (rows.length === 0) {
            tbody.innerHTML = `<tr><td colspan="6" class="text-center text-slate-500 py-10">${JG.esc(i18n.noMessages)}</td></tr>`;
            return;
        }

        tbody.innerHTML = rows.map((message) => {
            const readBadge = message.read
                ? `<span class="badge badge-success">${JG.esc(i18n.read)}</span>`
                : `<span class="badge badge-warning">${JG.esc(i18n.unread)}</span>`;
            const target = translateTarget(message.target_group || 'all') + (message.target_user_ids && message.target_user_ids.length ? ` + ${message.target_user_ids.length} ${i18n.usersSuffix}` : '');
            const channels = translateChannels(message.channels || []);

            return `<tr>
                <td>
                    <div class="font-semibold">${JG.esc(message.title || '')}</div>
                    <div class="text-xs text-slate-400 mt-1">${JG.esc(message.body || '')}</div>
                </td>
                <td class="text-sm text-slate-300">${JG.esc(target)}</td>
                <td class="text-sm text-slate-400">${JG.esc(channels)}</td>
                <td>${readBadge}</td>
                <td class="text-sm text-slate-500">${JG.esc(formatDateTime(message.created_at || ''))}</td>
                <td class="text-right">
                    <div class="flex justify-end gap-2">
                        ${!message.read ? `<button class="jg-btn jg-btn-sm jg-btn-ghost" data-action="read" data-id="${message.id}">${JG.esc(i18n.markRead)}</button>` : ''}
                        ${isAdmin ? `<button class="jg-btn jg-btn-sm jg-btn-danger" data-action="delete" data-id="${message.id}">${JG.esc(i18n.delete)}</button>` : ''}
                    </div>
                </td>
            </tr>`;
        }).join('');
    }

    document.addEventListener('DOMContentLoaded', () => {
        document.getElementById('messages-body')?.addEventListener('click', async (event) => {
            const button = event.target.closest('button');
            if (!button) {
                return;
            }
            const id = button.dataset.id;
            const action = button.dataset.action;

            if (action === 'read') {
                const res = await JG.api(`/admin/api/messages/${id}/read`, { method: 'POST' });
                if (!res.success) {
                    JG.toast(res.message || i18n.genericError, 'error');
                    return;
                }
                await loadMessages();
                return;
            }

            if (action === 'delete') {
                if (!confirm(i18n.confirmDelete)) {
                    return;
                }
                const res = await JG.api(`/admin/api/messages/${id}`, { method: 'DELETE' });
                if (!res.success) {
                    JG.toast(res.message || i18n.deleteFailed, 'error');
                    return;
                }
                JG.toast(i18n.deleted, 'success');
                await loadMessages();
            }
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
                await loadMessages();
            });
        }

        document.getElementById('btn-view-inbox')?.addEventListener('click', async () => {
            currentView = 'inbox';
            await loadMessages();
        });

        document.getElementById('btn-view-admin')?.addEventListener('click', async () => {
            currentView = 'admin';
            await loadMessages();
        });

        const toggle = document.getElementById('sidebar-toggle');
        if (toggle) {
            toggle.addEventListener('click', () => {
                const sidebar = document.getElementById('sidebar');
                if (sidebar) sidebar.classList.toggle('open');
            });
        }

        updateViewButtons();
        loadMessages();
    });
})();