(() => {
    // v3.0.0 - Fully functional with all event handlers
    const config = window.JGPageUsers || {};
    const i18n = config.i18n || {};
    const bulkActionMeta = config.bulkActionMeta || {};
    const text = config.text || {};

    function init() {
        console.info('TRACE: init() started');
        let allUsers = []; // Still used for some local stats but reflects current page users
        let filteredUsers = []; // Current page users
        let paginationMeta = { page: 1, limit: 25, total: 0, total_pages: 1 };
        let jellyfinPresets = [];
        const selectedIds = new Set();
        let pendingDeleteUser = null;
        let searchTimeout = null;
        let loadUsersAbortController = null;
        let loadUsersSeq = 0;

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
                const selectable = filteredUsers.map((u) => String(u.id));
                const selectedVisible = selectable.filter((id) => selectedIds.has(id)).length;
                checkAll.checked = selectable.length > 0 && selectedVisible === selectable.length;
                checkAll.indeterminate = selectedVisible > 0 && selectedVisible < selectable.length;
            }
            updateBulkWizardState();
        }

        function renderLoadingUsers() {
            const tbody = document.getElementById('users-tbody');
            if (!tbody) return;
            tbody.innerHTML = '<tr><td colspan="7" class="text-center py-16"><div class="flex flex-col items-center gap-3"><span class="spinner w-7 h-7 border-2 border-jg-accent border-t-transparent animate-spin rounded-full"></span><span class="text-jg-text-muted text-xs uppercase tracking-widest font-bold">Chargement...</span></div></td></tr>';
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

        function renderPagination() {
            const container = document.getElementById('pagination-controls');
            if (!container) return;
            
            const info = document.getElementById('pagination-info');
            if (info) {
                info.textContent = (i18n.pageLabel || 'Page') + ' ' + paginationMeta.page + ' ' + (i18n.pageOf || 'sur') + ' ' + paginationMeta.total_pages;
            }

            let html = '';
            const previousLabel = JG.esc(i18n.previous || 'Precedent');
            const nextLabel = JG.esc(i18n.next || 'Suivant');
            // Previous
            html += `<button class="jg-btn jg-btn-ghost h-10 px-3 flex items-center justify-center rounded-xl text-xs font-semibold ${paginationMeta.page <= 1 ? 'opacity-30 cursor-not-allowed' : ''}" data-page="${paginationMeta.page - 1}" ${paginationMeta.page <= 1 ? 'disabled' : ''}>
                ${previousLabel}
            </button>`;

            // Simple pagination: 1 ... current-1 current current+1 ... total
            const startPage = Math.max(1, paginationMeta.page - 2);
            const endPage = Math.min(paginationMeta.total_pages, paginationMeta.page + 2);

            if (startPage > 1) {
                html += `<button class="jg-btn jg-btn-ghost w-10 h-10 p-0 rounded-xl" data-page="1">1</button>`;
                if (startPage > 2) html += `<span class="px-2 text-jg-text-muted">...</span>`;
            }

            for (let i = startPage; i <= endPage; i++) {
                const active = i === paginationMeta.page ? 'bg-jg-accent text-black font-bold' : 'hover:bg-white/5';
                html += `<button class="jg-btn jg-btn-ghost w-10 h-10 p-0 rounded-xl ${active}" data-page="${i}">${i}</button>`;
            }

            if (endPage < paginationMeta.total_pages) {
                if (endPage < paginationMeta.total_pages - 1) html += `<span class="px-2 text-jg-text-muted">...</span>`;
                html += `<button class="jg-btn jg-btn-ghost w-10 h-10 p-0 rounded-xl" data-page="${paginationMeta.total_pages}">${paginationMeta.total_pages}</button>`;
            }

            // Next
            html += `<button class="jg-btn jg-btn-ghost h-10 px-3 flex items-center justify-center rounded-xl text-xs font-semibold ${paginationMeta.page >= paginationMeta.total_pages ? 'opacity-30 cursor-not-allowed' : ''}" data-page="${paginationMeta.page + 1}" ${paginationMeta.page >= paginationMeta.total_pages ? 'disabled' : ''}>
                ${nextLabel}
            </button>`;

            container.innerHTML = html;

            container.querySelectorAll('button[data-page]').forEach(btn => {
                btn.onclick = () => {
                    const p = parseInt(btn.dataset.page);
                    if (p > 0 && p <= paginationMeta.total_pages && p !== paginationMeta.page) {
                        paginationMeta.page = p;
                        loadUsers();
                    }
                };
            });
        }

        function renderUsers(users) {
            filteredUsers = users;
            const tbody = document.getElementById('users-tbody');
            if (!tbody) return;
            const userCount = document.getElementById('user-count');
            if (userCount) userCount.textContent = (paginationMeta.total || 0) + ' ' + (i18n.totalLabel || 'utilisateurs');
            
            const st = document.getElementById('users-stat-total'); if (st) st.textContent = paginationMeta.total_global || 0;
            const sf = document.getElementById('users-stat-filtered'); if (sf) sf.textContent = paginationMeta.total || 0;
            const si = document.getElementById('users-stat-inviters'); if (si) si.textContent = paginationMeta.inviters_count || 0;
            const se = document.getElementById('users-stat-expiring'); if (se) se.textContent = paginationMeta.expiring_count || 0;
            
            if (users.length === 0) {
                const help = paginationMeta.total === 0 ? i18n.usersNoLocal : i18n.usersNoFilterMatch;
                tbody.innerHTML = '<tr><td colspan="7" class="text-center text-slate-500 py-24">' + JG.esc(help) + '</td></tr>';
                updateSelectionUI(); return;
            }
            tbody.innerHTML = users.map((user) => {
                const userID = String(user.id);
                const checked = selectedIds.has(userID) ? 'checked' : '';
                const isSelected = selectedIds.has(userID);
                const bgClass = isSelected ? 'bg-jg-accent/10' : 'hover:bg-white/[0.03]';
                const expiry = user.access_expires_at ? fmtDate(user.access_expires_at) : '\u2014';
                
                let avatarHtml = `<div class="w-8 h-8 rounded-full bg-jg-accent/20 flex items-center justify-center font-bold text-jg-accent text-xs">${JG.esc(user.username.charAt(0).toUpperCase())}</div>`;
                if (user.jellyfin_id && user.jellyfin_primary_image_tag) {
                    const avatarUrl = `/admin/api/users/${user.id}/avatar?tag=${user.jellyfin_primary_image_tag}`;
                    avatarHtml = `<img src="${avatarUrl}" class="w-8 h-8 rounded-full object-cover border border-white/10" alt="${JG.esc(user.username)}" onerror="this.style.display='none'; this.nextElementSibling.style.display='flex';">`
                               + `<div class="w-8 h-8 rounded-full bg-jg-accent/20 items-center justify-center font-bold text-jg-accent text-xs hidden">${JG.esc(user.username.charAt(0).toUpperCase())}</div>`;
                }

                return '<tr class="group ' + bgClass + ' border-b border-white/5">'
                    + '<td class="px-6 py-4 w-12 text-center"><input type="checkbox" class="row-check form-checkbox" data-id="' + user.id + '" ' + checked + '></td>'
                    + '<td class="px-4 py-4"><div class="flex items-center gap-3">' + avatarHtml + '<div class="flex flex-col"><span class="font-bold">' + JG.esc(user.username) + '</span><span class="text-xs text-jg-text-muted">' + JG.esc(user.email || '\u2014') + '</span></div></div></td>'
                    + '<td class="px-4 py-4">' + userStatusBadge(user) + '</td>'
                    + '<td class="px-4 py-4">' + jellyfinStatusBadge(user) + '</td>'
                    + '<td class="px-4 py-4">' + (function() {
                        const p = jellyfinPresets.find(pr => pr.id === user.preset_id);
                        return JG.esc(p ? p.name : (user.preset_id || '\u2014'));
                    })() + '</td>'
                    + '<td class="px-4 py-4">' + JG.esc(expiry) + '</td>'
                    + '<td class="px-6 py-4 text-right"><div class="flex justify-end gap-2">'
                    + '<button class="action-timeline jg-btn jg-btn-ghost jg-btn-sm" data-id="' + user.id + '">\uD83D\uDCCA</button>'
                    + '<button class="action-edit jg-btn jg-btn-ghost jg-btn-sm" data-id="' + user.id + '">\u270F\uFE0F</button>'
                    + '<button class="action-toggle jg-btn jg-btn-ghost jg-btn-sm" data-id="' + user.id + '">' + (user.is_active ? '\uD83D\uDD13' : '\uD83D\uDD12') + '</button>'
                    + '<button class="action-delete jg-btn jg-btn-sm jg-btn-danger" data-id="' + user.id + '">\uD83D\uDDD1\uFE0F</button>'
                    + '</div></td></tr>';
            }).join('');
            updateSelectionUI();
            renderPagination();
        }

        function applyFilters() {
            // Trigger backend fetch
            paginationMeta.page = 1;
            loadUsers();
            updateFilterIndicators();
        }

        async function loadUsers() {
            const requestId = ++loadUsersSeq;
            if (loadUsersAbortController) {
                loadUsersAbortController.abort();
            }
            loadUsersAbortController = new AbortController();

            const query = document.getElementById('search-users')?.value || '';
            const status = document.getElementById('filter-status')?.value || 'all';
            const jellyfin = document.getElementById('filter-jellyfin')?.value || 'all';
            const invite = document.getElementById('filter-invite')?.value || 'all';
            const extra = document.getElementById('filter-extra')?.value || 'all';

            renderLoadingUsers();

            const params = new URLSearchParams({
                page: paginationMeta.page,
                limit: paginationMeta.limit,
                search: query,
                status: status,
                jellyfin: jellyfin, // currently limited impact in backend
                invite: invite,
                extra: extra,
                include_jellyfin: '1'
            });

            const res = await JG.api('/admin/api/users?' + params.toString(), {
                signal: loadUsersAbortController.signal,
            });
            if (requestId !== loadUsersSeq || res.aborted) {
                return;
            }

            if (res.success && res.data) {
                allUsers = res.data.users || [];
                paginationMeta = res.data.meta || paginationMeta;
                renderUsers(allUsers);
            }
            else { JG.toast(i18n.loadError || 'Erreur', 'error'); }
        }

        async function loadPresets() {
            const res = await JG.api('/admin/api/automation/presets');
            if (res.success) {
                jellyfinPresets = res.data || [];
                // Re-render table if users are already loaded (async race)
                if (allUsers.length > 0) renderUsers(allUsers);
            }
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
            const ids = Array.from(selectedIds)
                .map((id) => parseInt(id, 10))
                .filter((id) => Number.isFinite(id));
            if (ids.length === 0) {
                JG.toast(i18n.selectionEmpty || 'Selectionnez des utilisateurs', 'info');
                return;
            }
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

        document.getElementById('search-users')?.addEventListener('input', () => {
            if (searchTimeout) clearTimeout(searchTimeout);
            searchTimeout = setTimeout(() => {
                applyFilters();
            }, 400);
        });

        document.getElementById('items-per-page')?.addEventListener('change', (e) => {
            paginationMeta.limit = parseInt(e.target.value);
            paginationMeta.page = 1;
            loadUsers();
        });

        document.getElementById('filter-status')?.addEventListener('change', applyFilters);
        document.getElementById('filter-jellyfin')?.addEventListener('change', applyFilters);
        document.getElementById('filter-invite')?.addEventListener('change', applyFilters);
        document.getElementById('filter-extra')?.addEventListener('change', applyFilters);
        document.getElementById('btn-toggle-filters')?.addEventListener('click', toggleFilterPanel);
        document.getElementById('btn-clear-filters')?.addEventListener('click', clearFilters);

        // Select All
        document.getElementById('check-all')?.addEventListener('change', (e) => {
            const chk = e.target.checked;
            filteredUsers.forEach((u) => {
                const id = String(u.id);
                if (chk) selectedIds.add(id);
                else selectedIds.delete(id);
            });
            document.querySelectorAll('.row-check').forEach((cb) => { cb.checked = chk; });
            updateSelectionUI();
        });

        // Row checkboxes
        document.getElementById('users-tbody')?.addEventListener('change', (e) => {
            const cb = e.target.closest('.row-check');
            if (!cb) return;
            const rowID = String(cb.dataset.id || '');
            if (!rowID) return;
            if (cb.checked) selectedIds.add(rowID);
            else selectedIds.delete(rowID);
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
            
            // Populate Presets
            const presetSel = document.getElementById('edit-preset-id');
            if (presetSel) {
                let html = '<option value="">(Aucun preset)</option>';
                jellyfinPresets.forEach(p => {
                    html += `<option value="${JG.esc(p.id)}">${JG.esc(p.name || p.id)}</option>`;
                });
                presetSel.innerHTML = html;
                presetSel.value = user.preset_id || '';
            }

            // Show Read-only Group
            const groupWrap = document.getElementById('edit-group-wrapper');
            const groupNameInput = document.getElementById('edit-group-name');
            if (groupWrap && groupNameInput) {
                if (user.group_name) {
                    groupNameInput.textContent = user.group_name;
                    groupWrap.classList.remove('hidden');
                } else {
                    groupWrap.classList.add('hidden');
                }
            }

            document.getElementById('edit-expiry').value = toDateTimeLocal(user.access_expires_at);
            document.getElementById('edit-clear-expiry').checked = false;
            document.getElementById('edit-can-invite').checked = !!user.can_invite;
            JG.openModal('edit-modal');
        }
        document.getElementById('edit-cancel-btn')?.addEventListener('click', () => JG.closeModal('edit-modal'));
        document.getElementById('edit-save-btn')?.addEventListener('click', async () => {
            const uid = document.getElementById('edit-user-id').value;
            const clr = document.getElementById('edit-clear-expiry').checked;
            const p = { 
                email: document.getElementById('edit-email').value, 
                preset_id: document.getElementById('edit-preset-id').value, 
                access_expires_at: clr ? '' : document.getElementById('edit-expiry').value, 
                clear_expiry: clr, 
                can_invite: document.getElementById('edit-can-invite').checked 
            };
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
            if (res.success) { JG.toast(i18n.deleteSuccess||'OK', 'success'); selectedIds.delete(String(pendingDeleteUser)); pendingDeleteUser = null; JG.closeModal('delete-modal'); await loadUsers(); }
            else { JG.toast(res.message||i18n.deleteError||'Erreur', 'error'); }
        });
        document.getElementById('delete-modal')?.addEventListener('click', (e) => { if (e.target.id === 'delete-modal' || e.target.closest('[aria-hidden="true"]')) { pendingDeleteUser = null; JG.closeModal('delete-modal'); } });

        // Timeline Modal
        async function openTimeline(uid, user) {
            const sub = document.getElementById('timeline-subtitle');
            if (sub && user) sub.textContent = (i18n.timelineSubtitleTemplate || '{username}').replace('{username}', user.username);

            JG.openModal('timeline-modal');
            const list = document.getElementById('timeline-list');
            if (list) list.innerHTML = '<div class="text-center py-20 text-jg-text-muted animate-pulse">Chargement de l\'historique...</div>';

            const res = await JG.api(`/admin/api/users/${uid}/timeline`);
            if (res.success && Array.isArray(res.data)) {
                if (res.data.length === 0) {
                    list.innerHTML = `<div class="text-center py-20 text-jg-text-muted/40 border-2 border-dashed border-jg-border rounded-3xl bg-white/5 uppercase text-[10px] items-center justify-center flex flex-col gap-4">
                        <svg class="w-12 h-12 opacity-10" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
                        ${JG.esc(i18n.timelineEmpty || 'Aucun email envoye.')}
                    </div>`;
                    return;
                }

                let html = '<div class="space-y-3 pb-4">';
                res.data.forEach(entry => {
                    const action = (entry.action || '').toLowerCase();
                    const isFailed = action.includes('failed') || action.includes('error');
                    
                    let icon = `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" /></svg>`;
                    if (action.includes('verify')) icon = `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>`;
                    if (action.includes('reset')) icon = `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" /></svg>`;

                    html += `
                    <div class="group p-4 rounded-2xl bg-white/5 border border-jg-border hover:bg-white/10 hover:border-jg-accent/30 transition-all duration-300">
                        <div class="flex items-start gap-4">
                            <div class="w-10 h-10 rounded-xl ${isFailed ? 'bg-rose-500/10 text-rose-400' : 'bg-jg-accent/10 text-jg-accent'} flex items-center justify-center shrink-0 group-hover:scale-110 transition-transform duration-300">
                                ${icon}
                            </div>
                            <div class="flex-1 min-w-0">
                                <div class="flex items-center justify-between gap-4 mb-0.5">
                                    <span class="text-sm font-bold text-jg-text truncate">${JG.esc(entry.message || entry.action)}</span>
                                    <span class="text-[10px] font-medium text-jg-text-muted uppercase tracking-wider whitespace-nowrap">${fmtDate(entry.at)}</span>
                                </div>
                                <div class="flex items-center gap-2">
                                    ${isFailed ? `<span class="flex items-center gap-1 text-[10px] font-bold text-rose-400 uppercase tracking-widest bg-rose-500/10 px-2 py-0.5 rounded-full"><span class="w-1 h-1 rounded-full bg-rose-400 animate-pulse"></span> ECHEC</span>` : `<span class="flex items-center gap-1 text-[10px] font-bold text-emerald-400 uppercase tracking-widest bg-emerald-500/10 px-2 py-0.5 rounded-full"><span class="w-1 h-1 rounded-full bg-emerald-400"></span> ENVOYE</span>`}
                                    ${entry.actor ? `<span class="text-[10px] text-jg-text-muted/60 tracking-wider">PAR : ${JG.esc(entry.actor)}</span>` : ''}
                                </div>
                                ${entry.details ? `<div class="mt-2 text-[11px] text-jg-text-muted/80 leading-relaxed bg-black/20 p-2 rounded-lg border border-white/5 select-all font-mono break-all">${JG.esc(entry.details)}</div>` : ''}
                            </div>
                        </div>
                    </div>`;
                });
                html += '</div>';
                list.innerHTML = html;

            } else {
                list.innerHTML = `<div class="text-center py-20 text-rose-400">${JG.esc(i18n.timelineLoadError || 'Erreur lors du chargement.')}</div>`;
            }
        }
        document.getElementById('timeline-close-btn')?.addEventListener('click', () => JG.closeModal('timeline-modal'));
        document.getElementById('timeline-modal')?.addEventListener('click', (e) => { if (e.target.id === 'timeline-modal' || e.target.closest('[aria-hidden="true"]')) JG.closeModal('timeline-modal'); });

        // Initial load
        (async () => { await Promise.allSettled([loadUsers(), loadPresets()]); })();
    }

    if (document.readyState === 'loading') { document.addEventListener('DOMContentLoaded', init); }
    else { init(); }
})();