(() => {
    const config = window.JGPageUsers || {};
    const i18n = config.i18n || {};
    const bulkActionMeta = config.bulkActionMeta || {};
    const text = config.text || {};

    document.addEventListener('DOMContentLoaded', () => {
        let allUsers = [];
        let filteredUsers = [];
        let jellyfinPresets = [];
        const selectedIds = new Set();
        let pendingDeleteUser = null;

        function fmtDate(value) {
            if (!value) return '—';
            const date = new Date(value);
            if (Number.isNaN(date.getTime())) return value;
            return date.toLocaleString();
        }

        function toDateTimeLocal(value) {
            if (!value) return '';
            const date = new Date(value);
            if (Number.isNaN(date.getTime())) return '';
            const pad = (n) => `${n}`.padStart(2, '0');
            return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
        }

        function userStatusBadge(user) {
            if (user.is_banned) return `<span class="badge badge-danger">${JG.esc(i18n.statusBanned)}</span>`;
            if (user.is_active) return `<span class="badge badge-success">${JG.esc(i18n.statusActive)}</span>`;
            return `<span class="badge badge-warning">${JG.esc(i18n.statusInactive)}</span>`;
        }

        function jellyfinStatusBadge(user) {
            if (!user.jellyfin_exists) return `<span class="badge badge-muted">${JG.esc(i18n.jellyfinMissing)}</span>`;
            if (user.jellyfin_disabled) return `<span class="badge badge-warning">${JG.esc(i18n.jellyfinDisabled)}</span>`;
            return '<span class="badge badge-success">OK</span>';
        }

        function isExpired(user) {
            if (!user.access_expires_at) return false;
            const date = new Date(user.access_expires_at);
            if (Number.isNaN(date.getTime())) return false;
            return date.getTime() < Date.now();
        }

        function updateSelectionUI() {
            document.getElementById('bulk-selected-count').textContent = selectedIds.size;
            const focusSelected = document.getElementById('users-focus-selected');
            if (focusSelected) {
                focusSelected.textContent = `${selectedIds.size}`;
            }
            const checkAll = document.getElementById('check-all');
            const selectable = filteredUsers.map((user) => user.id);
            const selectedVisible = selectable.filter((id) => selectedIds.has(id)).length;
            checkAll.checked = selectable.length > 0 && selectedVisible === selectable.length;
            checkAll.indeterminate = selectedVisible > 0 && selectedVisible < selectable.length;
            renderSelectionPreview();
            updateBulkWizardState();
        }

        function renderFilterSnapshot() {
            const container = document.getElementById('users-active-filters');
            if (!container) {
                return;
            }

            const query = document.getElementById('search-users').value.trim();
            const status = document.getElementById('filter-status');
            const jellyfin = document.getElementById('filter-jellyfin');
            const invite = document.getElementById('filter-invite');
            const extra = document.getElementById('filter-extra');
            const chips = [];

            if (query) {
                chips.push(`“${JG.esc(query)}”`);
            }
            [status, jellyfin, invite, extra].forEach((select) => {
                if (select && select.value !== 'all') {
                    chips.push(JG.esc(select.options[select.selectedIndex].text));
                }
            });

            if (!chips.length) {
                container.innerHTML = `<span class="jg-chip jg-chip-muted">${JG.esc(i18n.filtersNone)}</span>`;
                return;
            }

            container.innerHTML = chips.map((chip) => `<span class="jg-chip">${chip}</span>`).join('');
        }

        function renderSelectionPreview() {
            const container = document.getElementById('users-selected-preview');
            if (!container) {
                return;
            }

            const selectedUsers = allUsers.filter((user) => selectedIds.has(user.id));
            if (!selectedUsers.length) {
                container.innerHTML = `<span class="jg-chip jg-chip-muted">${JG.esc(i18n.selectionEmpty)}</span>`;
                return;
            }

            const chips = selectedUsers.slice(0, 5).map((user) => `<span class="jg-chip">${JG.esc(user.username)}</span>`);
            if (selectedUsers.length > 5) {
                chips.push(`<span class="jg-chip jg-chip-muted">${JG.esc(fmtTemplate(i18n.selectionMore, { count: selectedUsers.length - 5 }))}</span>`);
            }
            container.innerHTML = chips.join('');
        }

        function clearFilters() {
            document.getElementById('search-users').value = '';
            document.getElementById('filter-status').value = 'all';
            document.getElementById('filter-jellyfin').value = 'all';
            document.getElementById('filter-invite').value = 'all';
            document.getElementById('filter-extra').value = 'all';
            applyFilters();
        }

        function openBulkEmailComposer() {
            const bulkCard = document.getElementById('bulk-card');
            const actionSelect = document.getElementById('bulk-action');
            if (!bulkCard || !actionSelect) return;

            bulkCard.scrollIntoView({ behavior: 'smooth', block: 'start' });
            actionSelect.value = 'send_email';
            actionSelect.dispatchEvent(new Event('change'));

            setTimeout(() => {
                const subjectInput = document.getElementById('bulk-email-subject');
                if (subjectInput) {
                    subjectInput.focus();
                }
            }, 180);
        }

        function fmtTemplate(template, values) {
            return String(template || '').replace(/\{(\w+)\}/g, (_, key) => {
                if (!values || values[key] === undefined || values[key] === null) return '';
                return String(values[key]);
            });
        }

        function renderUsers(users) {
            filteredUsers = users;
            const tbody = document.getElementById('users-tbody');
            document.getElementById('user-count').textContent = `${users.length} ${i18n.usersDisplayed} ${allUsers.length}`;
            document.getElementById('users-stat-total').textContent = allUsers.length;
            document.getElementById('users-stat-filtered').textContent = users.length;
            document.getElementById('users-stat-inviters').textContent = allUsers.filter((user) => user.can_invite).length;
            document.getElementById('users-stat-expiring').textContent = allUsers.filter((user) => isExpired(user)).length;
            const focusFiltered = document.getElementById('users-focus-filtered');
            if (focusFiltered) {
                focusFiltered.textContent = `${users.length}`;
            }

            if (users.length === 0) {
                const help = allUsers.length === 0 ? i18n.usersNoLocal : i18n.usersNoFilterMatch;
                tbody.innerHTML = `<tr><td colspan="11" class="text-center text-slate-500 py-12">${JG.esc(help)}</td></tr>`;
                updateSelectionUI();
                return;
            }

            tbody.innerHTML = users.map((user) => {
                const checked = selectedIds.has(user.id) ? 'checked' : '';
                const rowClass = selectedIds.has(user.id) ? ' class="is-selected"' : '';
                const toggleLabel = user.is_active ? i18n.deactivate : i18n.activate;
                const expiry = user.access_expires_at ? fmtDate(user.access_expires_at) : '—';
                const expiryClass = isExpired(user) ? 'text-red-300' : 'text-slate-400';

                return `<tr${rowClass}>
                    <td><input type="checkbox" class="form-checkbox row-check" data-id="${user.id}" ${checked}></td>
                    <td><span class="font-medium">${JG.esc(user.username)}</span></td>
                    <td class="text-slate-300">${JG.esc(user.email || '—')}</td>
                    <td>${userStatusBadge(user)}</td>
                    <td>${jellyfinStatusBadge(user)}</td>
                    <td class="text-slate-400">${JG.esc(user.invited_by || '—')}</td>
                    <td class="text-slate-300">${JG.esc(user.group_name || '—')}</td>
                    <td>${user.can_invite ? `<span class="badge badge-success">${JG.esc(i18n.yes)}</span>` : `<span class="badge badge-muted">${JG.esc(i18n.no)}</span>`}</td>
                    <td class="${expiryClass}">${JG.esc(expiry)}</td>
                    <td class="text-slate-500 text-sm">${JG.esc(fmtDate(user.created_at))}</td>
                    <td class="text-right">
                        <div class="flex justify-end gap-1 flex-wrap">
                            <button class="jg-btn jg-btn-sm jg-btn-ghost action-timeline" data-id="${user.id}">${JG.esc(i18n.timeline)}</button>
                            <button class="jg-btn jg-btn-sm jg-btn-ghost action-edit" data-id="${user.id}">${JG.esc(i18n.edit)}</button>
                            <button class="jg-btn jg-btn-sm jg-btn-ghost action-reset" data-id="${user.id}">${JG.esc(i18n.reset)}</button>
                            <button class="jg-btn jg-btn-sm jg-btn-ghost action-toggle" data-id="${user.id}">${toggleLabel}</button>
                            <button class="jg-btn jg-btn-sm jg-btn-danger action-delete" data-id="${user.id}">${JG.esc(i18n.delete)}</button>
                        </div>
                    </td>
                </tr>`;
            }).join('');

            updateSelectionUI();
        }

        function applyFilters() {
            const query = document.getElementById('search-users').value.trim().toLowerCase();
            const status = document.getElementById('filter-status').value;
            const jellyfin = document.getElementById('filter-jellyfin').value;
            const invite = document.getElementById('filter-invite').value;
            const extra = document.getElementById('filter-extra').value;

            const result = allUsers.filter((user) => {
                const textMatch = !query ||
                    (user.username || '').toLowerCase().includes(query) ||
                    (user.email || '').toLowerCase().includes(query) ||
                    (user.group_name || '').toLowerCase().includes(query) ||
                    (user.invited_by || '').toLowerCase().includes(query);

                const statusMatch =
                    status === 'all' ||
                    (status === 'active' && user.is_active && !user.is_banned) ||
                    (status === 'inactive' && !user.is_active && !user.is_banned) ||
                    (status === 'banned' && user.is_banned);

                const jellyfinMatch =
                    jellyfin === 'all' ||
                    (jellyfin === 'ok' && user.jellyfin_exists && !user.jellyfin_disabled) ||
                    (jellyfin === 'disabled' && user.jellyfin_exists && user.jellyfin_disabled) ||
                    (jellyfin === 'missing' && !user.jellyfin_exists);

                const inviteMatch =
                    invite === 'all' ||
                    (invite === 'enabled' && user.can_invite) ||
                    (invite === 'disabled' && !user.can_invite);

                const expired = isExpired(user);
                const hasExpiry = !!user.access_expires_at;
                const extraMatch =
                    extra === 'all' ||
                    (extra === 'with-email' && !!(user.email || '').trim()) ||
                    (extra === 'without-email' && !(user.email || '').trim()) ||
                    (extra === 'expiry-active' && hasExpiry) ||
                    (extra === 'expiry-expired' && expired) ||
                    (extra === 'expiry-none' && !hasExpiry);

                return textMatch && statusMatch && jellyfinMatch && inviteMatch && extraMatch;
            });

            renderFilterSnapshot();
            renderUsers(result);
        }

        function actionLabel(action) {
            if (!action) return i18n.bulkChooseAction;
            return bulkActionMeta[action]?.label || i18n.bulkActionGeneric;
        }

        function collectBulkPayload(action, userIDs) {
            const payload = { action, user_ids: userIDs };

            if (action === 'send_email') {
                payload.email_subject = (document.getElementById('bulk-email-subject')?.value || '').trim();
                payload.email_body = (document.getElementById('bulk-email-body')?.value || '').trim();
            }

            if (action === 'set_parrainage') {
                payload.can_invite = (document.getElementById('bulk-can-invite')?.value || 'false') === 'true';
            }

            if (action === 'set_expiry') {
                const clearExpiry = document.getElementById('bulk-clear-expiry')?.checked;
                payload.clear_expiry = !!clearExpiry;
                if (!clearExpiry) {
                    payload.access_expires_at = document.getElementById('bulk-expiry')?.value || '';
                }
            }

            if (action === 'jellyfin_policy') {
                const downloads = document.getElementById('bulk-jf-downloads')?.value || '';
                const remote = document.getElementById('bulk-jf-remote')?.value || '';
                const sessionsRaw = document.getElementById('bulk-jf-sessions')?.value;
                const bitrateRaw = document.getElementById('bulk-jf-bitrate')?.value;

                const policy = {};
                if (downloads) policy.enable_downloads = downloads === 'true';
                if (remote) policy.enable_remote_access = remote === 'true';
                if (sessionsRaw !== undefined && sessionsRaw !== '') policy.max_active_sessions = parseInt(sessionsRaw, 10);
                if (bitrateRaw !== undefined && bitrateRaw !== '') policy.remote_bitrate_limit = parseInt(bitrateRaw, 10);

                payload.jellyfin_policy = policy;
            }

            if (action === 'apply_preset') {
                payload.policy_preset_id = (document.getElementById('bulk-jf-preset')?.value || '').trim();
            }

            return payload;
        }

        function validateBulkPayload(action, payload) {
            if (!action) return i18n.bulkChooseAction;
            if (!payload.user_ids || payload.user_ids.length === 0) return i18n.bulkSelectOne;
            if (action === 'send_email' && (!payload.email_subject || !payload.email_body)) return i18n.bulkNeedEmailBody;
            if (action === 'set_expiry' && !payload.clear_expiry && !payload.access_expires_at) return i18n.bulkNeedExpiry;
            if (action === 'jellyfin_policy') {
                const policy = payload.jellyfin_policy || {};
                if (Object.keys(policy).length === 0) return i18n.bulkNeedJellyfinParam;
                if (Number.isNaN(policy.max_active_sessions) || Number.isNaN(policy.remote_bitrate_limit)) return i18n.bulkJellyfinInvalid;
            }
            if (action === 'apply_preset' && !payload.policy_preset_id) return i18n.bulkNeedPreset;
            return '';
        }

        function updateBulkWizardState() {
            const action = document.getElementById('bulk-action').value;
            const userIDs = Array.from(selectedIds);
            const payload = collectBulkPayload(action, userIDs);
            const validationError = validateBulkPayload(action, payload);

            const stepSelect = document.getElementById('bulk-step-select');
            const stepAction = document.getElementById('bulk-step-action');
            const stepConfig = document.getElementById('bulk-step-config');

            stepSelect.classList.remove('active', 'done');
            stepAction.classList.remove('active', 'done');
            stepConfig.classList.remove('active', 'done');

            if (userIDs.length > 0) {
                stepSelect.classList.add('done');
            } else {
                stepSelect.classList.add('active');
            }

            if (action) {
                stepAction.classList.add('done');
            } else if (userIDs.length > 0) {
                stepAction.classList.add('active');
            }

            if (action) {
                if (validationError) {
                    stepConfig.classList.add('active');
                } else {
                    stepConfig.classList.add('done');
                }
            }

            document.getElementById('bulk-action-label').textContent = actionLabel(action);
            document.getElementById('bulk-validation-text').textContent = validationError || i18n.bulkConfigReady;

            const summary = document.getElementById('bulk-summary');
            const selectedUsers = allUsers.filter((user) => selectedIds.has(user.id));
            if (selectedUsers.length === 0) {
                summary.textContent = i18n.bulkSelectOne;
            } else if (!action) {
                summary.textContent = i18n.bulkChooseAction;
            } else {
                const firstUsers = selectedUsers.slice(0, 3).map((user) => user.username).join(', ');
                const hiddenCount = selectedUsers.length > 3 ? ` +${selectedUsers.length - 3}` : '';
                const header = `${actionLabel(action)} (${selectedUsers.length}) [${firstUsers || i18n.bulkNone}${hiddenCount}]`;
                summary.textContent = validationError ? `${header}. ${validationError}` : `${header}. ${i18n.bulkConfigReady}`;
            }

            const applyButton = document.getElementById('bulk-apply');
            const isReady = !validationError;
            applyButton.disabled = !isReady;
            applyButton.classList.toggle('opacity-60', !isReady);
            applyButton.classList.toggle('cursor-not-allowed', !isReady);
        }

        function resetBulkFields() {
            const container = document.getElementById('bulk-fields');
            const help = document.getElementById('bulk-help');
            const action = document.getElementById('bulk-action').value;
            if (!action) {
                container.classList.add('hidden');
                container.innerHTML = '';
                help.classList.add('hidden');
                help.innerHTML = '';
                updateBulkWizardState();
                return;
            }

            help.classList.remove('hidden');
            help.textContent = bulkActionMeta[action]?.help || i18n.bulkActionGeneric;

            container.classList.remove('hidden');
            if (action === 'send_email') {
                container.innerHTML = `
                    <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                        <input id="bulk-email-subject" class="jg-input" placeholder="${JG.esc(text.bulkEmailSubjectPlaceholder || '')}">
                        <input id="bulk-email-tooltips" class="jg-input" disabled value="${JG.esc(text.bulkEmailVariablesLabel || '')}: {{.Username}} {{.Email}} {{.Actor}}">
                    </div>
                    <textarea id="bulk-email-body" class="jg-input mt-3 font-mono text-sm" rows="5" placeholder="${JG.esc(text.bulkEmailBodyPlaceholder || '')}"></textarea>
                `;
            } else if (action === 'set_parrainage') {
                container.innerHTML = `
                    <select id="bulk-can-invite" class="jg-input max-w-xs">
                        <option value="true">${JG.esc(text.bulkInviteEnabled || '')}</option>
                        <option value="false">${JG.esc(text.bulkInviteDisabled || '')}</option>
                    </select>
                `;
            } else if (action === 'set_expiry') {
                container.innerHTML = `
                    <div class="flex flex-wrap items-center gap-3">
                        <label class="text-xs text-slate-300">${JG.esc(text.bulkExpiryLabel || '')}:</label>
                        <input type="datetime-local" id="bulk-expiry" class="jg-input max-w-xs">
                        <label class="inline-flex items-center gap-2 text-sm text-slate-300">
                            <input type="checkbox" id="bulk-clear-expiry" class="form-checkbox">
                            ${JG.esc(text.bulkClearExpiry || '')}
                        </label>
                    </div>
                `;
            } else if (action === 'jellyfin_policy') {
                container.innerHTML = `
                    <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-3">
                        <select id="bulk-jf-downloads" class="jg-input">
                            <option value="">${JG.esc(text.bulkJfDownloadUnchanged || '')}</option>
                            <option value="true">${JG.esc(text.bulkJfDownloadAllowed || '')}</option>
                            <option value="false">${JG.esc(text.bulkJfDownloadBlocked || '')}</option>
                        </select>
                        <select id="bulk-jf-remote" class="jg-input">
                            <option value="">${JG.esc(text.bulkJfRemoteUnchanged || '')}</option>
                            <option value="true">${JG.esc(text.bulkJfRemoteAllowed || '')}</option>
                            <option value="false">${JG.esc(text.bulkJfRemoteBlocked || '')}</option>
                        </select>
                        <input type="number" id="bulk-jf-sessions" class="jg-input" min="0" placeholder="${JG.esc(text.bulkJfSessionsPlaceholder || '')}">
                        <input type="number" id="bulk-jf-bitrate" class="jg-input" min="0" placeholder="${JG.esc(text.bulkJfBitratePlaceholder || '')}">
                    </div>
                `;
            } else if (action === 'apply_preset') {
                const options = jellyfinPresets.map((preset) => `<option value="${JG.esc(preset.id)}">${JG.esc(preset.name)}</option>`).join('');
                container.innerHTML = `
                    <div class="max-w-lg">
                        <label class="jg-label" for="bulk-jf-preset">${JG.esc(bulkActionMeta.apply_preset?.label || '')}</label>
                        <select id="bulk-jf-preset" class="jg-input">
                            <option value="">${JG.esc(text.bulkSelectPreset || '')}</option>
                            ${options}
                        </select>
                    </div>
                `;
            } else {
                container.innerHTML = `<p class="text-sm text-slate-400">${JG.esc(text.bulkNoExtraParams || '')}</p>`;
            }

            const clearExpiry = document.getElementById('bulk-clear-expiry');
            const expiryInput = document.getElementById('bulk-expiry');
            if (clearExpiry && expiryInput) {
                const syncExpiryInput = () => {
                    expiryInput.disabled = clearExpiry.checked;
                };
                clearExpiry.addEventListener('change', syncExpiryInput);
                syncExpiryInput();
            }

            container.querySelectorAll('input, select, textarea').forEach((el) => {
                el.addEventListener('input', updateBulkWizardState);
                el.addEventListener('change', updateBulkWizardState);
            });

            updateBulkWizardState();
        }

        async function loadUsers() {
            const res = await JG.api('/admin/api/users');
            if (!res.success) {
                JG.toast(res.message || i18n.loadError, 'error');
                document.getElementById('users-tbody').innerHTML = `<tr><td colspan="11" class="text-center text-red-300 py-12">${JG.esc(i18n.loadErrorDetails)}</td></tr>`;
                return;
            }
            allUsers = res.data || [];
            applyFilters();
        }

        async function loadPresets() {
            const res = await JG.api('/admin/api/automation/presets');
            if (!res.success) {
                jellyfinPresets = [];
                return;
            }
            jellyfinPresets = Array.isArray(res.data) ? res.data : [];
        }

        function getUserById(id) {
            return allUsers.find((user) => user.id === id);
        }

        function timelineLevel(action) {
            if (!action) return 'neutral';
            if (action.includes('deleted')) return 'danger';
            if (action.includes('expired') || action.includes('disabled')) return 'warning';
            if (action.includes('created') || action.includes('enabled') || action.includes('success')) return 'success';
            return 'neutral';
        }

        function timelineBadge(action) {
            const level = timelineLevel(action);
            if (level === 'danger') return `<span class="badge badge-danger">${JG.esc(i18n.timelineCritical)}</span>`;
            if (level === 'warning') return `<span class="badge badge-warning">${JG.esc(i18n.timelineImportant)}</span>`;
            if (level === 'success') return `<span class="badge badge-success">${JG.esc(i18n.timelineInfo)}</span>`;
            return `<span class="badge badge-muted">${JG.esc(i18n.timelineTrace)}</span>`;
        }

        function renderTimelineEvents(events) {
            const container = document.getElementById('timeline-list');
            if (!Array.isArray(events) || events.length === 0) {
                container.innerHTML = `<div class="text-slate-400">${JG.esc(i18n.timelineEmpty)}</div>`;
                return;
            }

            container.innerHTML = events.map((eventItem) => {
                const action = eventItem.action || 'event';
                const actor = eventItem.actor ? `<span class="text-slate-400">par ${JG.esc(eventItem.actor)}</span>` : '';
                const details = eventItem.details ? `<div class="text-xs text-slate-500 mt-1 break-all">${JG.esc(eventItem.details)}</div>` : '';
                return `<div class="rounded-lg border border-white/10 bg-black/20 px-3 py-3">
                    <div class="flex items-center justify-between gap-3">
                        <div class="font-medium text-slate-100">${JG.esc(eventItem.message || action)}</div>
                        ${timelineBadge(action)}
                    </div>
                    <div class="mt-1 text-xs text-slate-400 flex flex-wrap items-center gap-3">
                        <span>${JG.esc(fmtDate(eventItem.at || ''))}</span>
                        <span class="text-slate-500">${JG.esc(action)}</span>
                        ${actor}
                    </div>
                    ${details}
                </div>`;
            }).join('');
        }

        function openEditModal(id) {
            const user = getUserById(id);
            if (!user) return;
            document.getElementById('edit-user-id').value = user.id;
            document.getElementById('edit-email').value = user.email || '';
            document.getElementById('edit-group-name').value = user.group_name || '';
            document.getElementById('edit-expiry').value = toDateTimeLocal(user.access_expires_at || '');
            document.getElementById('edit-clear-expiry').checked = false;
            document.getElementById('edit-can-invite').checked = !!user.can_invite;
            JG.openModal('edit-modal');
        }

        function closeEditModal() {
            JG.closeModal('edit-modal');
        }

        function confirmDelete(id, username) {
            pendingDeleteUser = { id, username };
            document.getElementById('delete-modal-text').textContent = fmtTemplate(i18n.deleteConfirmTemplate, { username });
            JG.openModal('delete-modal');
        }

        function closeDeleteModal() {
            pendingDeleteUser = null;
            JG.closeModal('delete-modal');
        }

        async function openTimelineModal(id) {
            const user = getUserById(id);
            if (!user) return;

            document.getElementById('timeline-subtitle').textContent = fmtTemplate(i18n.timelineSubtitleTemplate, {
                username: user.username,
                email: user.email || '-',
            });
            document.getElementById('timeline-list').innerHTML = '<div class="text-center text-slate-500 py-8"><span class="spinner"></span></div>';
            JG.openModal('timeline-modal');

            const res = await JG.api(`/admin/api/users/${id}/timeline`);
            if (!res.success) {
                document.getElementById('timeline-list').innerHTML = `<div class="text-red-300">${JG.esc(res.message || i18n.timelineLoadError)}</div>`;
                return;
            }

            renderTimelineEvents(res.data || []);
        }

        function closeTimelineModal() {
            JG.closeModal('timeline-modal');
        }

        document.getElementById('users-tbody').addEventListener('click', async (event) => {
            const button = event.target.closest('button');
            if (!button) return;
            const id = Number(button.dataset.id);
            const user = getUserById(id);
            if (!user) return;

            if (button.classList.contains('action-edit')) {
                openEditModal(id);
                return;
            }
            if (button.classList.contains('action-timeline')) {
                openTimelineModal(id);
                return;
            }
            if (button.classList.contains('action-reset')) {
                const res = await JG.api(`/admin/api/users/${id}/password-reset/send`, { method: 'POST' });
                if (res.success) JG.toast(fmtTemplate(i18n.resetSent, { username: user.username }), 'success');
                else JG.toast(res.message || i18n.resetError, 'error');
                return;
            }
            if (button.classList.contains('action-toggle')) {
                const res = await JG.api(`/admin/api/users/${id}/toggle`, { method: 'POST' });
                if (res.success) {
                    JG.toast(res.message || i18n.toggleUpdated, 'success');
                    loadUsers();
                } else {
                    JG.toast(res.message || i18n.toggleError, 'error');
                }
                return;
            }
            if (button.classList.contains('action-delete')) {
                confirmDelete(id, user.username);
            }
        });

        document.getElementById('users-tbody').addEventListener('change', (event) => {
            const checkbox = event.target.closest('.row-check');
            if (!checkbox) return;
            const id = Number(checkbox.dataset.id);
            if (checkbox.checked) selectedIds.add(id);
            else selectedIds.delete(id);
            updateSelectionUI();
        });

        document.getElementById('check-all').addEventListener('change', (event) => {
            const checked = event.target.checked;
            filteredUsers.forEach((user) => {
                if (checked) selectedIds.add(user.id);
                else selectedIds.delete(user.id);
            });
            renderUsers(filteredUsers);
        });

        document.getElementById('btn-select-filtered').addEventListener('click', () => {
            filteredUsers.forEach((user) => selectedIds.add(user.id));
            renderUsers(filteredUsers);
        });

        document.getElementById('edit-save-btn').addEventListener('click', async () => {
            const id = Number(document.getElementById('edit-user-id').value);
            const payload = {
                email: document.getElementById('edit-email').value.trim(),
                group_name: document.getElementById('edit-group-name').value.trim(),
                can_invite: document.getElementById('edit-can-invite').checked,
                access_expires_at: document.getElementById('edit-expiry').value,
                clear_expiry: document.getElementById('edit-clear-expiry').checked,
            };

            const res = await JG.api(`/admin/api/users/${id}`, {
                method: 'PATCH',
                body: JSON.stringify(payload),
            });

            if (res.success) {
                JG.toast(i18n.editUpdated, 'success');
                closeEditModal();
                loadUsers();
            } else {
                JG.toast(res.message || i18n.editUpdateError, 'error');
            }
        });

        document.getElementById('bulk-action').addEventListener('change', resetBulkFields);

        document.getElementById('bulk-apply').addEventListener('click', async () => {
            const action = document.getElementById('bulk-action').value;
            const userIDs = Array.from(selectedIds);

            const payload = collectBulkPayload(action, userIDs);
            const validationError = validateBulkPayload(action, payload);
            if (validationError) {
                JG.toast(validationError, 'error');
                updateBulkWizardState();
                return;
            }

            const selectedUsers = allUsers.filter((user) => selectedIds.has(user.id));
            const confirmationMsg = fmtTemplate(text.bulkConfirmTemplate, {
                action: actionLabel(action),
                count: selectedUsers.length,
            });

            if ((action === 'delete' || action === 'deactivate' || action === 'jellyfin_policy') && !confirm(confirmationMsg)) {
                return;
            }

            const res = await JG.api('/admin/api/users/bulk', {
                method: 'POST',
                body: JSON.stringify(payload),
            });

            if (res.success) {
                const success = res.data?.success || 0;
                const total = res.data?.total || userIDs.length;
                JG.toast(fmtTemplate(i18n.bulkDone, { success, total }), success === total ? 'success' : 'info');
                if (action === 'delete') {
                    selectedIds.clear();
                }
                updateBulkWizardState();
                loadUsers();
            } else {
                JG.toast(res.message || i18n.bulkActionFailed, 'error');
            }
        });

        ['search-users', 'filter-status', 'filter-jellyfin', 'filter-invite', 'filter-extra'].forEach((id) => {
            document.getElementById(id).addEventListener('input', applyFilters);
            document.getElementById(id).addEventListener('change', applyFilters);
        });

        document.querySelectorAll('.modal-overlay').forEach((overlay) => {
            overlay.addEventListener('click', (event) => {
                if (event.target !== overlay) return;
                closeDeleteModal();
                closeEditModal();
                closeTimelineModal();
            });
        });

        document.getElementById('sidebar-toggle')?.addEventListener('click', () => {
            const sidebar = document.getElementById('sidebar');
            if (sidebar) sidebar.classList.toggle('open');
        });

        document.getElementById('btn-open-bulk-email')?.addEventListener('click', openBulkEmailComposer);
        document.getElementById('btn-sync-users')?.addEventListener('click', syncUsers);
        document.getElementById('btn-clear-filters')?.addEventListener('click', clearFilters);
        document.getElementById('btn-clear-selection')?.addEventListener('click', () => {
            selectedIds.clear();
            renderUsers(filteredUsers);
        });
        document.getElementById('edit-cancel-btn')?.addEventListener('click', closeEditModal);
        document.getElementById('delete-cancel-btn')?.addEventListener('click', closeDeleteModal);
        document.getElementById('timeline-close-btn')?.addEventListener('click', closeTimelineModal);

        document.getElementById('delete-confirm-btn')?.addEventListener('click', async () => {
            if (!pendingDeleteUser) return;
            const { id, username } = pendingDeleteUser;
            closeDeleteModal();
            const res = await JG.api(`/admin/api/users/${id}`, { method: 'DELETE' });
            if (res.success) {
                selectedIds.delete(id);
                JG.toast(fmtTemplate(i18n.deleteSuccess, { username }), 'success');
                loadUsers();
            } else {
                JG.toast(res.message || i18n.deleteError, 'error');
            }
        });

        (async () => {
            await loadPresets();
            resetBulkFields();
            renderFilterSnapshot();
            renderSelectionPreview();
            await loadUsers();
        })();

        async function syncUsers() {
            if (!confirm(i18n.syncConfirm)) return;

            try {
                const data = await JG.api('/admin/api/users/sync', { method: 'POST' });
                if (data.success) {
                    JG.toast(data.message || i18n.syncDone, 'success');
                    setTimeout(() => window.location.reload(), 1000);
                } else {
                    JG.toast(data.message || i18n.syncError, 'error');
                }
            } catch {
                JG.toast(i18n.syncNetworkError, 'error');
            }
        }
    });
})();