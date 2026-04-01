(() => {
    // v3.0.0 - Fully functional with all event handlers
    const config = window.JGPageUsers || {};
    const i18n = config.i18n || {};
    const bulkActionMeta = config.bulkActionMeta || {};
    const text = config.text || {};

    function init() {
        console.info('TRACE: init() started');
        let allUsers = [];
        let filteredUsers = [];
        let jellyfinPresets = [];
        const selectedIds = new Set();
        let pendingDeleteUser = null;

        function fmtDate(value) {
            if (!value) return '\u2014';
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
            const count = selectedIds.size;
            const bulkDrawerCount = document.getElementById('bulk-drawer-count');
            if (bulkDrawerCount) bulkDrawerCount.textContent = count + ' selectionnes';
            const selectionCount = document.getElementById('selection-count');
            if (selectionCount) selectionCount.textContent = count;
            const selectionBar = document.getElementById('selection-bar');
            if (selectionBar) {
                if (count > 0) selectionBar.classList.add('active');
                else { selectionBar.classList.remove('active'); closeBulkDrawer(); }
            }
            const checkAll = document.getElementById('check-all');
            if (checkAll) {
                const selectable = filteredUsers.map((u) => u.id);
                const selectedVisible = selectable.filter((id) => selectedIds.has(id)).length;
                checkAll.checked = selectable.length > 0 && selectedVisible === selectable.length;
                checkAll.indeterminate = selectedVisible > 0 && selectedVisible < selectable.length;
            }
            updateBulkWizardState();
        }

        function openBulkDrawer() {
            document.getElementById('bulk-drawer')?.classList.add('open');
            document.getElementById('bulk-drawer-overlay')?.classList.add('open');
            document.body.style.overflow = 'hidden';
            resetBulkFields();
        }

        function closeBulkDrawer() {
            document.getElementById('bulk-drawer')?.classList.remove('open');
            document.getElementById('bulk-drawer-overlay')?.classList.remove('open');
            document.body.style.overflow = '';
        }

        function resetBulkFields() {
            const actionSelect = document.getElementById('bulk-action');
            if (actionSelect) actionSelect.value = '';
            const bulkFields = document.getElementById('bulk-fields');
            if (bulkFields) bulkFields.innerHTML = '<div class="text-center py-12 text-jg-text-muted/40 border-2 border-dashed border-jg-border rounded-2xl bg-white/5">' + JG.esc(i18n.bulkChooseAction || 'Choisir une action') + '</div>';
            const bulkHelp = document.getElementById('bulk-help');
            if (bulkHelp) { bulkHelp.classList.add('hidden'); bulkHelp.textContent = ''; }
            updateBulkWizardState();
        }

        function toggleFilterPanel() {
            const panel = document.getElementById('filter-panel');
            const btn = document.getElementById('btn-toggle-filters');
            if (!panel) return;
            if (panel.classList.contains('hidden')) {
                panel.classList.remove('hidden');
                btn?.classList.add('bg-white/10', 'text-white');
            } else {
                panel.classList.add('hidden');
                btn?.classList.remove('bg-white/10', 'text-white');
            }
        }

        function clearFilters() {
            ['search-users'].forEach(id => { const el = document.getElementById(id); if (el) el.value = ''; });
            ['filter-status','filter-jellyfin','filter-invite','filter-extra'].forEach(id => { const el = document.getElementById(id); if (el) el.value = 'all'; });
            document.getElementById('btn-clear-filters')?.classList.add('hidden');
            document.getElementById('active-filter-count')?.classList.add('hidden');
            applyFilters();
        }

        function updateFilterIndicators() {
            let c = 0;
            if ((document.getElementById('filter-status')?.value || 'all') !== 'all') c++;
            if ((document.getElementById('filter-jellyfin')?.value || 'all') !== 'all') c++;
            if ((document.getElementById('filter-invite')?.value || 'all') !== 'all') c++;
            if ((document.getElementById('filter-extra')?.value || 'all') !== 'all') c++;
            if ((document.getElementById('search-users')?.value || '').trim()) c++;
            const counter = document.getElementById('active-filter-count');
            const clearBtn = document.getElementById('btn-clear-filters');
            if (counter) { if (c > 0) { counter.textContent = c; counter.classList.remove('hidden'); } else counter.classList.add('hidden'); }
            if (clearBtn) { if (c > 0) clearBtn.classList.remove('hidden'); else clearBtn.classList.add('hidden'); }
        }

        function renderUsers(users) {
            filteredUsers = users;
            const tbody = document.getElementById('users-tbody');
            if (!tbody) return;
            const userCount = document.getElementById('user-count');
            if (userCount) userCount.textContent = users.length + ' ' + (i18n.usersDisplayed||'') + ' ' + allUsers.length;
            const st = document.getElementById('users-stat-total'); if (st) st.textContent = allUsers.length;
            const sf = document.getElementById('users-stat-filtered'); if (sf) sf.textContent = users.length;
            const si = document.getElementById('users-stat-inviters'); if (si) si.textContent = allUsers.filter(u => u.can_invite).length;
            const se = document.getElementById('users-stat-expiring'); if (se) se.textContent = allUsers.filter(u => u.access_expires_at && !isExpired(u)).length;
            if (users.length === 0) {
                const help = allUsers.length === 0 ? i18n.usersNoLocal : i18n.usersNoFilterMatch;
                tbody.innerHTML = '<tr><td colspan="11" class="text-center text-slate-500 py-24">' + JG.esc(help) + '</td></tr>';
                updateSelectionUI(); return;
            }
            tbody.innerHTML = users.map((user) => {
                const checked = selectedIds.has(user.id) ? 'checked' : '';
                const isSelected = selectedIds.has(user.id);
                const bgClass = isSelected ? 'bg-jg-accent/10' : 'hover:bg-white/[0.03]';
                const toggleLabel = user.is_active ? i18n.deactivate : i18n.activate;
                const expiry = user.access_expires_at ? fmtDate(user.access_expires_at) : '\u2014';
                return '<tr class="group ' + bgClass + ' border-b border-white/5">'
                    + '<td class="px-6 py-4 w-12 text-center"><input type="checkbox" class="row-check form-checkbox" data-id="' + user.id + '" ' + checked + '></td>'
                    + '<td class="px-4 py-4"><div class="flex items-center gap-3"><div class="w-8 h-8 rounded-full bg-jg-accent/20 flex items-center justify-center font-bold">' + JG.esc(user.username.charAt(0).toUpperCase()) + '</div><div class="flex flex-col"><span class="font-bold">' + JG.esc(user.username) + '</span><span class="text-xs text-jg-text-muted">' + JG.esc(user.email || '\u2014') + '</span></div></div></td>'
                    + '<td class="px-4 py-4">' + userStatusBadge(user) + '</td>'
                    + '<td class="px-4 py-4">' + jellyfinStatusBadge(user) + '</td>'
                    + '<td class="px-4 py-4">' + JG.esc(user.group_name || '\u2014') + '</td>'
                    + '<td class="px-4 py-4">' + JG.esc(expiry) + '</td>'
                    + '<td class="px-6 py-4 text-right"><div class="flex justify-end gap-2">'
                    + '<button class="action-timeline jg-btn jg-btn-ghost jg-btn-sm" data-id="' + user.id + '">\uD83D\uDCCA</button>'
                    + '<button class="action-edit jg-btn jg-btn-ghost jg-btn-sm" data-id="' + user.id + '">\u270F\uFE0F</button>'
                    + '<button class="action-toggle jg-btn jg-btn-ghost jg-btn-sm" data-id="' + user.id + '">' + (user.is_active ? '\uD83D\uDD12' : '\uD83D\uDD13') + '</button>'
                    + '<button class="action-delete jg-btn jg-btn-sm jg-btn-danger" data-id="' + user.id + '">\uD83D\uDDD1\uFE0F</button>'
                    + '</div></td></tr>';
            }).join('');
            updateSelectionUI();
        }

        function applyFilters() {
            const query = document.getElementById('search-users')?.value.toLowerCase() || '';
            const status = document.getElementById('filter-status')?.value || 'all';
            const jellyfin = document.getElementById('filter-jellyfin')?.value || 'all';
            const invite = document.getElementById('filter-invite')?.value || 'all';
            const extra = document.getElementById('filter-extra')?.value || 'all';
            const result = allUsers.filter(u => {
                const mQ = !query || u.username.toLowerCase().includes(query) || (u.email||'').toLowerCase().includes(query);
                let mS = true;
                if (status === 'active') mS = u.is_active && !u.is_banned;
                else if (status === 'inactive') mS = !u.is_active && !u.is_banned;
                else if (status === 'banned') mS = u.is_banned;
                let mJ = true;
                if (jellyfin === 'ok') mJ = u.jellyfin_exists && !u.jellyfin_disabled;
                else if (jellyfin === 'disabled') mJ = u.jellyfin_exists && u.jellyfin_disabled;
                else if (jellyfin === 'missing') mJ = !u.jellyfin_exists;
                let mI = true;
                if (invite === 'enabled') mI = u.can_invite;
                else if (invite === 'disabled') mI = !u.can_invite;
                let mE = true;
                if (extra === 'with-email') mE = !!u.email;
                else if (extra === 'without-email') mE = !u.email;
                else if (extra === 'expiry-active') mE = u.access_expires_at && !isExpired(u);
                else if (extra === 'expiry-expired') mE = u.access_expires_at && isExpired(u);
                else if (extra === 'expiry-none') mE = !u.access_expires_at;
                return mQ && mS && mJ && mI && mE;
            });
            updateFilterIndicators();
            renderUsers(result);
        }

        async function loadUsers() {
            const res = await JG.api('/admin/api/users');
            if (res.success) { allUsers = res.data || []; applyFilters(); }
            else { JG.toast(i18n.loadError || 'Erreur', 'error'); }
        }

        async function loadPresets() {
            const res = await JG.api('/admin/api/automation/presets');
            if (res.success) jellyfinPresets = res.data || [];
        }

        function updateBulkWizardState() {
            const applyButton = document.getElementById('bulk-apply');
            const action = document.getElementById('bulk-action')?.value || '';
            if (applyButton) {
                const ok = selectedIds.size > 0 && action !== '';
                applyButton.disabled = !ok;
                applyButton.classList.toggle('opacity-60', !ok);
                applyButton.classList.toggle('cursor-not-allowed', !ok);
            }
            const summary = document.getElementById('bulk-summary');
            if (summary) {
                if (selectedIds.size === 0) summary.textContent = i18n.bulkSelectOne || 'Selectionnez au moins un utilisateur.';
                else if (!action) summary.textContent = i18n.bulkChooseAction || 'Choisissez une action.';
                else { const m = bulkActionMeta[action]; summary.textContent = (i18n.bulkValidPrefix||'') + ' ' + (m?m.label:action) + ' ' + (i18n.bulkActionOn||'sur') + ' ' + selectedIds.size + ' ' + (i18n.bulkReadySuffix||'utilisateur(s)'); }
            }
        }

        function renderBulkFields(action) {
            const c = document.getElementById('bulk-fields');
            if (!c) return;
            if (action === 'send_email') {
                c.innerHTML = '<div class="space-y-4"><div><label class="jg-label">' + JG.esc(text.bulkEmailSubjectPlaceholder||'Sujet') + '</label><input type="text" id="bulk-email-subject" class="jg-input h-12" placeholder="' + JG.esc(text.bulkEmailSubjectPlaceholder||'Sujet') + '"></div><div><label class="jg-label">' + JG.esc(text.bulkEmailBodyPlaceholder||'Message') + '</label><textarea id="bulk-email-body" class="jg-input" rows="6" placeholder="' + JG.esc(text.bulkEmailBodyPlaceholder||'Corps du message...') + '"></textarea></div><div class="text-xs text-jg-text-muted">' + JG.esc(text.bulkEmailVariablesLabel||'Variables') + ': {{.Username}}, {{.Email}}</div></div>';
            } else if (action === 'set_expiry') {
                c.innerHTML = '<div class="space-y-4"><div><label class="jg-label">' + JG.esc(text.bulkExpiryLabel||'Expiration') + '</label><input type="datetime-local" id="bulk-expiry" class="jg-input h-12"></div><label class="flex items-center gap-3 cursor-pointer"><input type="checkbox" id="bulk-clear-expiry" class="form-checkbox"><span class="text-sm">' + JG.esc(text.bulkClearExpiry||'Supprimer') + '</span></label></div>';
            } else if (action === 'set_parrainage') {
                c.innerHTML = '<div class="space-y-4"><label class="flex items-center gap-3"><input type="radio" name="bulk-invite-value" value="true" class="form-radio"><span>' + JG.esc(text.bulkInviteEnabled||'Activer') + '</span></label><label class="flex items-center gap-3"><input type="radio" name="bulk-invite-value" value="false" checked class="form-radio"><span>' + JG.esc(text.bulkInviteDisabled||'Desactiver') + '</span></label></div>';
            } else if (action === 'jellyfin_policy') {
                c.innerHTML = '<div class="space-y-4"><div><label class="jg-label">Download</label><select id="bulk-jf-download" class="jg-input h-12"><option value="">' + JG.esc(text.bulkJfDownloadUnchanged||'Inchange') + '</option><option value="true">' + JG.esc(text.bulkJfDownloadAllowed||'Autorise') + '</option><option value="false">' + JG.esc(text.bulkJfDownloadBlocked||'Bloque') + '</option></select></div><div><label class="jg-label">Remote</label><select id="bulk-jf-remote" class="jg-input h-12"><option value="">' + JG.esc(text.bulkJfRemoteUnchanged||'Inchange') + '</option><option value="true">' + JG.esc(text.bulkJfRemoteAllowed||'Autorise') + '</option><option value="false">' + JG.esc(text.bulkJfRemoteBlocked||'Bloque') + '</option></select></div><div><label class="jg-label">Sessions</label><input type="number" id="bulk-jf-sessions" class="jg-input h-12" min="0"></div><div><label class="jg-label">Bitrate</label><input type="number" id="bulk-jf-bitrate" class="jg-input h-12" min="0"></div></div>';
            } else if (action === 'apply_preset') {
                const opts = jellyfinPresets.map(p => '<option value="' + JG.esc(p.id) + '">' + JG.esc(p.name||p.id) + '</option>').join('');
                c.innerHTML = '<div><label class="jg-label">' + JG.esc(text.bulkSelectPreset||'Preset') + '</label><select id="bulk-preset" class="jg-input h-12"><option value="">' + JG.esc(text.bulkSelectPreset||'Selectionner...') + '</option>' + opts + '</select></div>';
            } else if (['activate','deactivate','delete','send_password_reset'].includes(action)) {
                c.innerHTML = '<div class="text-center py-8 text-jg-text-muted">' + JG.esc(text.bulkNoExtraParams||'Aucun parametre supplementaire requis.') + '</div>';
            } else {
                c.innerHTML = '<div class="text-center py-12 text-jg-text-muted/40 border-2 border-dashed border-jg-border rounded-2xl bg-white/5">' + JG.esc(i18n.bulkChooseAction||'Choisir une action') + '</div>';
            }
        }

        async function executeBulkAction() {
            const action = document.getElementById('bulk-action')?.value || '';
            if (!action || selectedIds.size === 0) return;
            const ids = Array.from(selectedIds);
            const m = bulkActionMeta[action];
            if (!confirm((m?m.label:action) + ' ' + (i18n.bulkActionOn||'sur') + ' ' + ids.length + ' ' + (i18n.bulkReadySuffix||'utilisateur(s)') + ' ?')) return;
            let payload = { action: action, user_ids: ids };
            if (action === 'send_email') {
                payload.subject = document.getElementById('bulk-email-subject')?.value || '';
                payload.body = document.getElementById('bulk-email-body')?.value || '';
                if (!payload.subject || !payload.body) { JG.toast(i18n.bulkNeedEmailBody||'Sujet et corps requis', 'error'); return; }
            } else if (action === 'set_expiry') {
                payload.clear_expiry = !!document.getElementById('bulk-clear-expiry')?.checked;
                if (!payload.clear_expiry) { payload.expiry = document.getElementById('bulk-expiry')?.value || ''; if (!payload.expiry) { JG.toast(i18n.bulkNeedExpiry||'Date requise', 'error'); return; } }
            } else if (action === 'set_parrainage') {
                payload.can_invite = document.querySelector('input[name="bulk-invite-value"]:checked')?.value === 'true';
            } else if (action === 'jellyfin_policy') {
                payload.download = document.getElementById('bulk-jf-download')?.value || '';
                payload.remote = document.getElementById('bulk-jf-remote')?.value || '';
                payload.max_sessions = parseInt(document.getElementById('bulk-jf-sessions')?.value||'0', 10)||0;
                payload.bitrate_limit = parseInt(document.getElementById('bulk-jf-bitrate')?.value||'0', 10)||0;
            } else if (action === 'apply_preset') {
                payload.preset_id = document.getElementById('bulk-preset')?.value || '';
                if (!payload.preset_id) { JG.toast(i18n.bulkNeedPreset||'Selectionnez un preset', 'error'); return; }
            }
            const res = await JG.api('/admin/api/users/bulk', { method: 'POST', body: JSON.stringify(payload) });
            if (res.success) { JG.toast(i18n.bulkDone||'OK', 'success'); selectedIds.clear(); closeBulkDrawer(); await loadUsers(); }
            else { JG.toast(res.message||i18n.bulkActionFailed||'Erreur', 'error'); }
        }

        // Event Listeners
        document.getElementById('btn-sync-users')?.addEventListener('click', async () => {
            if (!confirm(i18n.syncConfirm)) return;
            const res = await JG.api('/admin/api/users/sync', { method: 'POST' });
            if (res.success) { JG.toast(i18n.syncDone||'OK', 'success'); loadUsers(); }
            else { JG.toast(res.message||i18n.syncError||'Erreur', 'error'); }
        });

        document.getElementById('search-users')?.addEventListener('input', applyFilters);
        document.getElementById('filter-status')?.addEventListener('change', applyFilters);
        document.getElementById('filter-jellyfin')?.addEventListener('change', applyFilters);
        document.getElementById('filter-invite')?.addEventListener('change', applyFilters);
        document.getElementById('filter-extra')?.addEventListener('change', applyFilters);
        document.getElementById('btn-toggle-filters')?.addEventListener('click', toggleFilterPanel);
        document.getElementById('btn-clear-filters')?.addEventListener('click', clearFilters);

        // Select All
        document.getElementById('check-all')?.addEventListener('change', (e) => {
            const chk = e.target.checked;
            filteredUsers.forEach((u) => { if (chk) selectedIds.add(u.id); else selectedIds.delete(u.id); });
            document.querySelectorAll('.row-check').forEach((cb) => { cb.checked = chk; });
            updateSelectionUI();
        });

        // Row checkboxes
        document.getElementById('users-tbody')?.addEventListener('change', (e) => {
            const cb = e.target.closest('.row-check');
            if (!cb) return;
            if (cb.checked) selectedIds.add(cb.dataset.id);
            else selectedIds.delete(cb.dataset.id);
            updateSelectionUI();
        });

        // Row action buttons
        document.getElementById('users-tbody')?.addEventListener('click', async (e) => {
            const btn = e.target.closest('button');
            if (!btn) return;
            const uid = btn.dataset.id;
            if (!uid) return;
            const user = allUsers.find(u => String(u.id) === String(uid));

            if (btn.classList.contains('action-timeline')) { openTimeline(uid, user); return; }
            if (btn.classList.contains('action-edit')) { openEditModal(uid, user); return; }
            if (btn.classList.contains('action-toggle')) {
                if (!user) return;
                const res = await JG.api('/admin/api/users/' + uid + '/toggle', { method: 'POST' });
                if (res.success) { JG.toast(i18n.toggleUpdated||'OK', 'success'); await loadUsers(); }
                else { JG.toast(res.message||i18n.toggleError||'Erreur', 'error'); }
                return;
            }
            if (btn.classList.contains('action-delete')) { openDeleteModal(uid, user); return; }
        });

        // Bulk email button
        document.getElementById('btn-open-bulk-email')?.addEventListener('click', () => {
            if (selectedIds.size === 0) { JG.toast(i18n.selectionEmpty||'Selectionnez des utilisateurs', 'info'); return; }
            openBulkDrawer();
            const sel = document.getElementById('bulk-action');
            if (sel) { sel.value = 'send_email'; sel.dispatchEvent(new Event('change')); }
        });

        document.getElementById('btn-open-bulk')?.addEventListener('click', openBulkDrawer);
        document.getElementById('btn-close-bulk')?.addEventListener('click', closeBulkDrawer);
        document.getElementById('bulk-drawer-overlay')?.addEventListener('click', closeBulkDrawer);

        document.getElementById('bulk-action')?.addEventListener('change', (e) => {
            const action = e.target.value;
            const meta = bulkActionMeta[action];
            const help = document.getElementById('bulk-help');
            if (help) { if (meta && meta.help) { help.textContent = meta.help; help.classList.remove('hidden'); } else help.classList.add('hidden'); }
            renderBulkFields(action);
            updateBulkWizardState();
        });

        document.getElementById('bulk-apply')?.addEventListener('click', executeBulkAction);

        // Edit Modal
        function openEditModal(uid, user) {
            if (!user) return;
            document.getElementById('edit-user-id').value = uid;
            document.getElementById('edit-email').value = user.email || '';
            document.getElementById('edit-group-name').value = user.group_name || '';
            document.getElementById('edit-expiry').value = toDateTimeLocal(user.access_expires_at);
            document.getElementById('edit-clear-expiry').checked = false;
            document.getElementById('edit-can-invite').checked = !!user.can_invite;
            JG.openModal('edit-modal');
        }
        document.getElementById('edit-cancel-btn')?.addEventListener('click', () => JG.closeModal('edit-modal'));
        document.getElementById('edit-save-btn')?.addEventListener('click', async () => {
            const uid = document.getElementById('edit-user-id').value;
            const clr = document.getElementById('edit-clear-expiry').checked;
            const p = { email: document.getElementById('edit-email').value, group_name: document.getElementById('edit-group-name').value, access_expires_at: clr ? '' : document.getElementById('edit-expiry').value, clear_expiry: clr, can_invite: document.getElementById('edit-can-invite').checked };
            const res = await JG.api('/admin/api/users/' + uid, { method: 'PATCH', body: JSON.stringify(p) });
            if (res.success) { JG.toast(i18n.editUpdated||'OK', 'success'); JG.closeModal('edit-modal'); await loadUsers(); }
            else { JG.toast(res.message||i18n.editUpdateError||'Erreur', 'error'); }
        });
        document.getElementById('edit-modal')?.addEventListener('click', (e) => { if (e.target.id === 'edit-modal' || e.target.closest('[aria-hidden="true"]')) JG.closeModal('edit-modal'); });

        // Delete Modal
        function openDeleteModal(uid, user) {
            pendingDeleteUser = uid;
            const t = document.getElementById('delete-modal-text');
            if (t && user) t.textContent = (i18n.deleteConfirmTemplate||'Supprimer {username} ?').replace('{username}', user.username);
            JG.openModal('delete-modal');
        }
        document.getElementById('delete-cancel-btn')?.addEventListener('click', () => { pendingDeleteUser = null; JG.closeModal('delete-modal'); });
        document.getElementById('delete-confirm-btn')?.addEventListener('click', async () => {
            if (!pendingDeleteUser) return;
            const res = await JG.api('/admin/api/users/' + pendingDeleteUser, { method: 'DELETE' });
            if (res.success) { JG.toast(i18n.deleteSuccess||'OK', 'success'); selectedIds.delete(pendingDeleteUser); pendingDeleteUser = null; JG.closeModal('delete-modal'); await loadUsers(); }
            else { JG.toast(res.message||i18n.deleteError||'Erreur', 'error'); }
        });
        document.getElementById('delete-modal')?.addEventListener('click', (e) => { if (e.target.id === 'delete-modal' || e.target.closest('[aria-hidden="true"]')) { pendingDeleteUser = null; JG.closeModal('delete-modal'); } });

        // Timeline Modal
        async function openTimeline(uid, user) {
            const sub = document.getElementById('timeline-subtitle');
            if (sub && user) sub.textContent = (i18n.timelineSubtitleTemplate||'{username}').replace('{username}', user.username);
            JG.openModal('timeline-modal');
            const list = document.getElementById('timeline-list');
            if (list) list.innerHTML = '<div class="text-center py-20 text-jg-text-muted animate-pulse">Chargement...</div>';
            const res = await JG.api('/admin/api/logs?actor=' + encodeURIComponent(user?.username||'') + '&limit=50');
            if (res.success && Array.isArray(res.data) && res.data.length > 0) {
                list.innerHTML = res.data.map(entry => {
                    const lvl = (entry.level||'info').toLowerCase();
                    let badge = '<span class="badge badge-muted">' + JG.esc(i18n.timelineInfo||'Info') + '</span>';
                    if (lvl === 'critical' || lvl === 'error') badge = '<span class="badge badge-danger">' + JG.esc(i18n.timelineCritical||'Critique') + '</span>';
                    else if (lvl === 'warning') badge = '<span class="badge badge-warning">' + JG.esc(i18n.timelineImportant||'Important') + '</span>';
                    return '<div class="flex gap-4 p-4 rounded-xl bg-white/5 border border-white/5"><div class="flex-shrink-0 mt-1">' + badge + '</div><div class="flex-1 min-w-0"><div class="font-medium text-sm">' + JG.esc(entry.action||entry.message||'') + '</div><div class="text-xs text-jg-text-muted mt-1">' + fmtDate(entry.created_at||entry.timestamp) + '</div></div></div>';
                }).join('');
            } else {
                list.innerHTML = '<div class="text-center py-20 text-jg-text-muted">' + JG.esc(i18n.timelineEmpty||'Aucun evenement.') + '</div>';
            }
        }
        document.getElementById('timeline-close-btn')?.addEventListener('click', () => JG.closeModal('timeline-modal'));
        document.getElementById('timeline-modal')?.addEventListener('click', (e) => { if (e.target.id === 'timeline-modal' || e.target.closest('[aria-hidden="true"]')) JG.closeModal('timeline-modal'); });

        // Initial load
        (async () => { await loadUsers(); await loadPresets(); })();
    }

    if (document.readyState === 'loading') { document.addEventListener('DOMContentLoaded', init); }
    else { init(); }
})();