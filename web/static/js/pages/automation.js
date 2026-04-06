(() => {
    const config = window.JGPageAutomation || {};
    const i18n = config.i18n || {};
    const taskTypeDescriptions = config.taskTypeDescriptions || {};

    document.addEventListener('DOMContentLoaded', () => {
        // --- CSP Compliant Modal Handlers ---
        document.addEventListener("click", (e) => {
            const openBtn = e.target.closest("#btn-open-task-modal");
            if (openBtn) {
                console.log("Opening task modal...");
                JG.openModal("modal-task-form");
            }
            
            const addPresetBtn = e.target.closest("#btn-preset-add");
            if (addPresetBtn) {
                // Handled in specific listener below, but delegating here for consistency
            }

            const closeBtn = e.target.closest(".modal-close-btn");
            const backdrop = e.target.closest(".modal-backdrop");
            const target = closeBtn || backdrop;

            if (target) {
                const modalId = target.getAttribute("data-modal");
                if (modalId) {
                    JG.closeModal(modalId);
                }
            }
        });

        // Double check for task button explicitly
        const taskBtn = document.getElementById('btn-open-task-modal');
        if (taskBtn) {
            taskBtn.onclick = () => JG.openModal("modal-task-form");
        }

        // --- Custom Confirm Logic ---
        function confirmAction(title, message) {
            return new Promise((resolve) => {
                const modal = document.getElementById('modal-confirm');
                if (!modal) {
                    resolve(window.confirm(message));
                    return;
                }
                const titleEl = document.getElementById('confirm-modal-title');
                const messageEl = document.getElementById('confirm-modal-message');
                if (titleEl) titleEl.textContent = title;
                if (messageEl) messageEl.textContent = message;

                JG.openModal('modal-confirm');

                const btnConfirm = document.getElementById('btn-confirm-action');
                const btnCancel = document.getElementById('btn-confirm-cancel');

                const cleanup = () => {
                    btnConfirm.removeEventListener('click', onConfirm);
                    btnCancel.removeEventListener('click', onCancel);
                    /* Also close if backdrop or X is clicked (handled globally by closeModal)
                       But to be robust, we wait a bit in interval to check if modal is hidden */
                };

                const onConfirm = () => {
                    JG.closeModal('modal-confirm');
                    cleanup();
                    resolve(true);
                };
                const onCancel = () => {
                    JG.closeModal('modal-confirm');
                    cleanup();
                    resolve(false);
                };

                btnConfirm.addEventListener('click', onConfirm);
                btnCancel.addEventListener('click', onCancel);
            });
        }

        let presets = [];
        let groupMappings = [];
        let tasks = [];
        let templateUsers = [];

        function updateTaskPreview() {
            const name = (document.getElementById('task-name')?.value || '').trim();
            const type = document.getElementById('task-type')?.value || '';
            const hour = document.getElementById('task-hour')?.value;
            const minute = document.getElementById('task-minute')?.value;
            const payload = (document.getElementById('task-payload')?.value || '').trim();
            const enabled = !!document.getElementById('task-enabled')?.checked;

            const previewName = document.getElementById('automation-task-preview-name');
            const previewType = document.getElementById('automation-task-preview-type');
            const previewSchedule = document.getElementById('automation-task-preview-schedule');
            const previewPayload = document.getElementById('automation-task-preview-payload');
            const previewState = document.getElementById('automation-task-preview-state');
            const previewNote = document.getElementById('automation-task-preview-note');
            const previewEmpty = document.getElementById('automation-task-preview-empty');

            if (previewName) {
                previewName.textContent = name || i18n.taskPreviewEmpty;
            }
            if (previewType) {
                previewType.textContent = type || '—';
            }
            if (previewSchedule) {
                if (hour === '' || minute === '') {
                    previewSchedule.textContent = '--:--';
                } else {
                    previewSchedule.textContent = `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;
                }
            }
            if (previewPayload) {
                previewPayload.textContent = payload ? i18n.taskPayloadReady : i18n.taskPayloadEmpty;
            }
            if (previewState) {
                previewState.textContent = enabled ? i18n.taskEnabled : i18n.taskDisabled;
                previewState.classList.toggle('badge-success', enabled);
                previewState.classList.toggle('badge-muted', !enabled);
            }
            if (previewNote) {
                previewNote.textContent = taskTypeDescriptions[type] || i18n.taskPreviewEmpty;
            }
            if (previewEmpty) {
                previewEmpty.textContent = name || payload || hour !== '' || minute !== '' ? (taskTypeDescriptions[type] || i18n.taskPreviewEmpty) : i18n.taskPreviewEmpty;
            }
        }

        function updateOverview() {
            const presetCount = document.getElementById('automation-presets-count');
            const mappingCount = document.getElementById('automation-mappings-count');
            const taskCount = document.getElementById('automation-tasks-count');
            if (presetCount) presetCount.textContent = `${presets.length}`;
            if (mappingCount) mappingCount.textContent = `${groupMappings.length}`;
            if (taskCount) taskCount.textContent = `${tasks.length}`;
        }

        function presetRow(preset, idx) {
            const downloadBadge = preset.enable_download 
                ? '<span class="badge-success text-[10px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider">Autorisé</span>' 
                : '<span class="badge-danger text-[10px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider">Refusé</span>';
            const remoteBadge = preset.enable_remote_access 
                ? '<span class="badge-success text-[10px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider">Autorisé</span>' 
                : '<span class="badge-danger text-[10px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider">Refusé</span>';

            return `<tr class="hover:bg-white/[0.02] transition-colors border-b border-jg-border last:border-none">
            <td class="px-6 py-4"><code class="text-[10px] bg-white/5 px-2 py-1 rounded-md text-jg-text-muted border border-white/5">${JG.esc(preset.id || '')}</code></td>
            <td class="px-6 py-4 font-bold text-jg-text">${JG.esc(preset.name || '')}</td>
            <td class="px-6 py-4">${downloadBadge}</td>
            <td class="px-6 py-4">${remoteBadge}</td>
            <td class="px-6 py-4"><span class="text-sm font-medium text-jg-text">${Number.isInteger(preset.max_sessions) ? preset.max_sessions : 0}</span> <span class="text-[10px] text-jg-text-muted uppercase tracking-tighter ml-1">Flux</span></td>
            <td class="px-6 py-4"><span class="text-sm font-medium text-jg-text">${Number.isInteger(preset.bitrate_limit) ? preset.bitrate_limit : 0}</span> <span class="text-[10px] text-jg-text-muted uppercase tracking-tighter ml-1">Mbps</span></td>
            <td class="px-6 py-4 text-right">
                <div class="flex justify-end gap-2">
                    <button class="jg-btn jg-btn-sm jg-btn-ghost hover:bg-white/10" data-action="preset-edit" data-index="${idx}">${JG.esc(i18n.edit || 'Éditer')}</button>
                    <button class="jg-btn jg-btn-sm jg-btn-danger/80 hover:bg-jg-danger transition-colors" data-action="preset-delete" data-index="${idx}">${JG.esc(i18n.deleteLabel)}</button>
                </div>
            </td>
        </tr>`;
        }

        function renderPresets() {
            const tbody = document.getElementById('presets-body');
            if (!tbody) {
                return;
            }
            updateOverview();
            if (!presets.length) {
                tbody.innerHTML = `<tr><td colspan="7" class="text-center text-slate-500 py-8">${JG.esc(i18n.noPresets)}</td></tr>`;
                return;
            }
            tbody.innerHTML = presets.map((preset, idx) => presetRow(preset, idx)).join('');
        }

        async function loadPresets() {
            const res = await JG.api('/admin/api/automation/presets');
            if (!res.success) {
                JG.toast(res.message || i18n.errorPresets, 'error');
                return;
            }
            presets = Array.isArray(res.data) ? res.data : [];
            renderPresets();
        }

        async function loadMappings() {
            const res = await JG.api('/admin/api/automation/group-mappings');
            if (!res.success) {
                JG.toast(res.message || i18n.errorGroupMappings, 'error');
                return;
            }
            groupMappings = Array.isArray(res.data) ? res.data : [];
            renderMappings();
        }

        document.getElementById('btn-group-map-add')?.addEventListener('click', () => {
             groupMappings.push({
                 group_name: '',
                 source: 'ldap',
                 ldap_group_dn: '',
                 policy_preset_id: presets.length ? presets[0].id : ''
             });
             renderMappings();
        });

        document.getElementById('btn-group-map-save')?.addEventListener('click', async () => {
            const rows = document.querySelectorAll('#group-mappings-body tr');
            const data = [];
            rows.forEach((row, idx) => {
                if (idx >= groupMappings.length) return;
                const nameInput = row.querySelector('input[data-field="group_name"]');
                const sourceSelect = row.querySelector('select[data-field="source"]');
                const ldapInput = row.querySelector('input[data-field="ldap_group_dn"]');
                const presetSelect = row.querySelector('select[data-field="policy_preset_id"]');
                
                if (nameInput && sourceSelect && presetSelect) {
                    data.push({
                        group_name: nameInput.value.trim(),
                        source: sourceSelect.value,
                        ldap_group_dn: ldapInput ? ldapInput.value.trim() : '',
                        policy_preset_id: presetSelect.value
                    });
                } else {
                    // If it's a rendered row (static), use existing data
                    data.push(groupMappings[idx]);
                }
            });

            const res = await JG.api('/admin/api/automation/group-mappings', {
                method: 'POST',
                body: JSON.stringify(data)
            });

            if (!res.success) {
                JG.toast(res.message || i18n.saveMappingsFailed, 'error');
                return;
            }
            JG.toast(i18n.mappingsSaved, 'success');
            await loadMappings();
        });

        document.getElementById('group-mappings-body')?.addEventListener('click', async (e) => {
             const btn = e.target.closest('button');
             if (!btn) return;
             if (btn.dataset.action === 'mapping-delete') {
                 const idx = parseInt(btn.dataset.index);
                 const agreed = await confirmAction('Supprimer le mapping', 'Voulez-vous supprimer ce mapping de groupe ?');
                 if (!agreed) return;
                 groupMappings.splice(idx, 1);
                 renderMappings();
             }
        });

        function mappingRow(mapping, idx) {
            const presetOptions = presets.map(p => `<option value="${JG.esc(p.id)}" ${p.id === mapping.policy_preset_id ? 'selected' : ''}>${JG.esc(p.name || p.id)}</option>`).join('');
            
            return `<tr>
                <td><input type="text" class="jg-input jg-input-sm" data-field="group_name" value="${JG.esc(mapping.group_name || '')}" placeholder="Nom du groupe"></td>
                <td>
                    <select class="jg-input jg-input-sm jg-select-premium py-0" data-field="source">
                        <option value="internal" ${mapping.source === 'internal' ? 'selected' : ''}>Interne</option>
                        <option value="ldap" ${mapping.source === 'ldap' ? 'selected' : ''}>LDAP</option>
                    </select>
                </td>
                <td><input type="text" class="jg-input jg-input-sm text-xs" data-field="ldap_group_dn" value="${JG.esc(mapping.ldap_group_dn || '')}" placeholder="CN=...,DC=..."></td>
                <td>
                    <select class="jg-input jg-input-sm jg-select-premium py-0" data-field="policy_preset_id">
                        ${presetOptions}
                    </select>
                </td>
                <td class="text-right">
                    <button class="jg-btn jg-btn-sm jg-btn-danger" data-action="mapping-delete" data-index="${idx}">${JG.esc(i18n.deleteLabel)}</button>
                </td>
            </tr>`;
        }

        function renderMappings() {
            const tbody = document.getElementById('group-mappings-body');
            if (!tbody) return;
            updateOverview();
            
            // Wait for presets if they are not loaded yet
            if (!presets || presets.length === 0) {
                // If we are currently loading presets, they will trigger a re-render
                // Otherwise, it might be that there are really no presets
                console.log("renderMappings called but presets are empty");
            }

            if (!groupMappings.length) {
                tbody.innerHTML = `<tr><td colspan="5" class="text-center text-slate-500 py-8">${JG.esc(i18n.noGroupMappings)}</td></tr>`;
                return;
            }
            tbody.innerHTML = groupMappings.map((m, idx) => mappingRow(m, idx)).join('');
        }

        async function loadMappings() {
            const res = await JG.api('/admin/api/automation/group-mappings');
            if (!res.success) {
                JG.toast(res.message || i18n.errorGroupMappings, 'error');
                return;
            }
            groupMappings = Array.isArray(res.data) ? res.data : [];
            renderMappings();
        }

        // Modal Preset Handlers
        let currentPresetIndex = -1;
        
        function getSlug(text) {
            return (text || '').trim().toLowerCase().replace(/[^a-z0-9]/g, '-');
        }

        function populateTemplateUserSelect(selectedValue) {
            const select = document.getElementById('preset-template-user');
            if (!select) return;

            const options = ['<option value="">Aucun profil clone</option>'];
            templateUsers.forEach((user) => {
                options.push(`<option value="${JG.esc(user.value)}">${JG.esc(user.label)}</option>`);
            });
            select.innerHTML = options.join('');
            select.value = selectedValue || '';
        }

        async function loadTemplateUsers() {
            const res = await JG.api('/admin/api/users?limit=500&include_jellyfin=0');
            const usersList = Array.isArray(res?.data)
                ? res.data
                : (Array.isArray(res?.data?.users) ? res.data.users : []);

            if (!res?.success || !usersList.length) {
                templateUsers = [];
                return;
            }

            templateUsers = usersList
                .filter((user) => user && user.jellyfin_id)
                .map((user) => ({
                    value: user.jellyfin_id,
                    label: user.username || user.jellyfin_id,
                }));
        }

        function resolvePresetLDAPGroups(preset) {
            const presetID = String(preset?.id || '').trim().toLowerCase();
            const result = { users: '', inviter: '' };
            if (!presetID) {
                return result;
            }

            const rows = groupMappings.filter((mapping) => {
                const source = String(mapping?.source || '').trim().toLowerCase();
                const mappingPresetID = String(mapping?.policy_preset_id || '').trim().toLowerCase();
                const groupDN = String(mapping?.ldap_group_dn || '').trim();
                return source === 'ldap' && mappingPresetID === presetID && groupDN !== '';
            });

            rows.forEach((mapping) => {
                const groupDN = String(mapping.ldap_group_dn || '').trim();
                if (!groupDN) {
                    return;
                }

                const name = String(mapping.group_name || '').trim().toLowerCase();
                if (!result.inviter && (name.includes('parrain') || name.includes('inviter') || name.includes('sponsor'))) {
                    result.inviter = groupDN;
                    return;
                }

                if (!result.users) {
                    result.users = groupDN;
                    return;
                }

                if (!result.inviter) {
                    result.inviter = groupDN;
                }
            });

            return result;
        }

        function openPresetModal(idx) {
            currentPresetIndex = idx;
            const preset = presets[idx] || {};
            document.getElementById('preset-name').value = preset.name || '';

            const ldapGroups = resolvePresetLDAPGroups(preset);
            const ldapInput = document.getElementById('preset-ldap-dn');
            if (ldapInput) ldapInput.value = preset._ldap_dn || ldapGroups.users || '';

            const ldapInviterInput = document.getElementById('preset-ldap-dn-inviter');
            if (ldapInviterInput) ldapInviterInput.value = preset._ldap_dn_inviter || ldapGroups.inviter || '';

            document.getElementById('preset-enable-download').checked = !!preset.enable_download;
            document.getElementById('preset-enable-remote').checked = !!preset.enable_remote_access;
            document.getElementById('preset-max-sessions').value = preset.max_sessions || 0;
            document.getElementById('preset-bitrate').value = preset.bitrate_limit || 0;
            document.getElementById('preset-disable-days').value = preset.disable_after_days || 0;
            document.getElementById('preset-delete-days').value = preset.delete_after_days || 0;
            populateTemplateUserSelect(preset.template_user_id || '');
            
            // Sponsorship / Parrainage
            const canInviteEl = document.getElementById('preset-can-invite');
            if (canInviteEl) {
                canInviteEl.checked = !!preset.can_invite;
                
                // Sponsorship options toggle
                const sponsorshipOpts = document.getElementById('preset-sponsorship-options');
                if (sponsorshipOpts) {
                    sponsorshipOpts.style.display = preset.can_invite ? 'grid' : 'none';
                }

                // Add listener if not already present (one-time logic)
                if (!canInviteEl.dataset.listener) {
                    canInviteEl.addEventListener('change', (e) => {
                        const opts = document.getElementById('preset-sponsorship-options');
                        if (opts) opts.style.display = e.target.checked ? 'grid' : 'none';
                    });
                    canInviteEl.dataset.listener = "true";
                }
            }

            const quotaDayEl = document.getElementById('preset-invite-quota-day');
            if (quotaDayEl) quotaDayEl.value = preset.invite_quota_day || 0;

            const quotaMonthEl = document.getElementById('preset-invite-quota-month');
            if (quotaMonthEl) quotaMonthEl.value = preset.invite_quota_month || preset.invite_quota || 0;

            const maxUsesEl = document.getElementById('preset-invite-max-uses');
            if (maxUsesEl) maxUsesEl.value = preset.invite_max_uses || 1;

            const linkDaysEl = document.getElementById('preset-invite-link-days');
            if (linkDaysEl) {
                const linkDays = preset.invite_link_validity_days || (preset.invite_max_link_hours ? Math.max(1, Math.ceil(preset.invite_max_link_hours / 24)) : 0);
                linkDaysEl.value = linkDays;
            }
            
            const targetSelect = document.getElementById('preset-target-preset');
            if (targetSelect) {
                targetSelect.innerHTML = `<option value="">(Même preset que le parrain)</option>` + 
                    presets.filter(p => p.id && p.id !== preset.id).map(p => `<option value="${JG.esc(p.id)}">${JG.esc(p.name || p.id)}</option>`).join('');
                targetSelect.value = preset.target_preset_id || '';
            }

            JG.openModal('modal-preset-form');
        }
        
        document.getElementById('preset-form-internal')?.addEventListener('submit', async (e) => {
            e.preventDefault();
            const idx = currentPresetIndex;
            if (idx < 0 || idx >= presets.length) return;
            
            const name = document.getElementById('preset-name').value.trim();
            if (!presets[idx].id) {
                 // New preset, generate slug
                 presets[idx].id = getSlug(name) || 'preset-' + Math.random().toString(36).substr(2, 5);
            }
            presets[idx].name = name;
            const ldapInput = document.getElementById('preset-ldap-dn');
            presets[idx]._ldap_dn = ldapInput ? ldapInput.value.trim() : '';

            const ldapInviterInput = document.getElementById('preset-ldap-dn-inviter');
            presets[idx]._ldap_dn_inviter = ldapInviterInput ? ldapInviterInput.value.trim() : '';
            
            presets[idx].enable_download = document.getElementById('preset-enable-download').checked;
            presets[idx].enable_remote_access = document.getElementById('preset-enable-remote').checked;
            presets[idx].max_sessions = parseInt(document.getElementById('preset-max-sessions').value, 10) || 0;
            presets[idx].bitrate_limit = parseInt(document.getElementById('preset-bitrate').value, 10) || 0;
            presets[idx].disable_after_days = parseInt(document.getElementById('preset-disable-days').value, 10) || 0;
            presets[idx].delete_after_days = parseInt(document.getElementById('preset-delete-days').value, 10) || 0;
            presets[idx].template_user_id = (document.getElementById('preset-template-user')?.value || '').trim();
            const canInviteEl = document.getElementById('preset-can-invite');
            if (canInviteEl) presets[idx].can_invite = canInviteEl.checked;
            
            const targetPresetEl = document.getElementById('preset-target-preset');
            if (targetPresetEl) presets[idx].target_preset_id = targetPresetEl.value || '';
            
            const quotaDayEl = document.getElementById('preset-invite-quota-day');
            presets[idx].invite_quota_day = quotaDayEl ? (parseInt(quotaDayEl.value, 10) || 0) : 0;

            const quotaMonthEl = document.getElementById('preset-invite-quota-month');
            presets[idx].invite_quota_month = quotaMonthEl ? (parseInt(quotaMonthEl.value, 10) || 0) : 0;
            presets[idx].invite_quota = presets[idx].invite_quota_month;
            
            const maxUsesEl = document.getElementById('preset-invite-max-uses');
            if (maxUsesEl) presets[idx].invite_max_uses = parseInt(maxUsesEl.value, 10) || 1;
            
            const linkDaysEl = document.getElementById('preset-invite-link-days');
            const linkDays = linkDaysEl ? (parseInt(linkDaysEl.value, 10) || 0) : 0;
            presets[idx].invite_link_validity_days = linkDays;
            presets[idx].invite_max_link_hours = linkDays > 0 ? linkDays * 24 : 0;
            
            // Clean payload
            const payload = presets.map(p => {
                const cleaned = {...p};
                delete cleaned._ldap_dn;
                delete cleaned._ldap_dn_inviter;
                return cleaned;
            });
            
            const res = await JG.api('/admin/api/automation/presets', {
                method: 'POST',
                body: JSON.stringify(payload),
            });
            
            if (!res.success) {
                JG.toast(res.message || i18n.savePresetsFailed, 'error');
                return;
            }
            
            // Also generate and save Group Mappings
            const mappingsPayload = [];
            presets.forEach(p => {
                const presetLabel = String(p.name || p.id || 'Preset').trim();

                // Internal group mapping (implicit)
                mappingsPayload.push({
                    group_name: presetLabel,
                    source: 'internal',
                    ldap_group_dn: '',
                    policy_preset_id: p.id
                });
                
                // LDAP users group mapping (if defined)
                if (p._ldap_dn) {
                    mappingsPayload.push({
                        group_name: `${presetLabel} (LDAP users)`,
                        source: 'ldap',
                        ldap_group_dn: p._ldap_dn,
                        policy_preset_id: p.id
                    });
                }

                // LDAP sponsorship group mapping (if defined)
                if (p._ldap_dn_inviter) {
                    mappingsPayload.push({
                        group_name: `${presetLabel} (LDAP parrainage)`,
                        source: 'ldap',
                        ldap_group_dn: p._ldap_dn_inviter,
                        policy_preset_id: p.id
                    });
                }
            });
            
            await JG.api('/admin/api/automation/group-mappings', {
                method: 'POST',
                body: JSON.stringify(mappingsPayload),
            });
            
            JG.toast(i18n.presetsSaved, 'success');
            await loadPresets();
            await loadMappings(); // Refresh mappings to reflect preset changes
            JG.closeModal('modal-preset-form');
        });

        function renderTasks() {
            const tbody = document.getElementById('tasks-body');
            if (!tbody) {
                return;
            }
            updateOverview();
            if (!tasks.length) {
                tbody.innerHTML = `<tr><td colspan="6" class="text-center text-slate-500 py-8">${JG.esc(i18n.noTasks)}</td></tr>`;
                return;
            }
            tbody.innerHTML = tasks.map((task) => `<tr>
            <td>${JG.esc(task.name || '')}</td>
            <td>${JG.esc(task.task_type || '')}</td>
            <td>${String(task.hour).padStart(2, '0')}:${String(task.minute).padStart(2, '0')} ${task.enabled ? '<span class="badge badge-success ml-2">ON</span>' : '<span class="badge badge-muted ml-2">OFF</span>'}</td>
            <td class="text-xs text-slate-400">${JG.esc(task.payload || '')}</td>
            <td class="text-sm text-slate-500">${JG.esc(task.last_run_at || '—')}</td>
            <td class="text-right">
                <div class="flex justify-end gap-2">
                    <button class="jg-btn jg-btn-sm jg-btn-ghost" data-action="task-run" data-id="${task.id}">${JG.esc(i18n.runNow)}</button>
                    <button class="jg-btn jg-btn-sm jg-btn-ghost" data-action="task-edit" data-id="${task.id}">${JG.esc(i18n.edit || 'Éditer')}</button>
                    <button class="jg-btn jg-btn-sm jg-btn-ghost" data-action="task-toggle" data-id="${task.id}">${task.enabled ? JG.esc(i18n.disable) : JG.esc(i18n.enable)}</button>
                    <button class="jg-btn jg-btn-sm jg-btn-danger" data-action="task-delete" data-id="${task.id}">${JG.esc(i18n.deleteLabel)}</button>
                </div>
            </td>
        </tr>`).join('');
        }

        async function loadTasks() {
            const res = await JG.api('/admin/api/automation/tasks');
            if (!res.success) {
                JG.toast(res.message || i18n.errorTasks, 'error');
                return;
            }
            tasks = Array.isArray(res.data) ? res.data : [];
            renderTasks();
        }

        document.getElementById('btn-preset-add')?.addEventListener('click', () => {
            presets.push({
                id: '', // Empty ID = new preset flag
                name: '',
                enable_download: true,
                enable_remote_access: true,
                max_sessions: 0,
                bitrate_limit: 0,
                enable_all_folders: true,
                enabled_folder_ids: [],
                password_min_length: 8,
                require_upper: false,
                require_lower: false,
                require_digit: false,
                require_special: false,
                disable_after_days: 0,
                expiry_action: 'disable',
                delete_after_days: 0,
                can_invite: false,
                target_preset_id: '',
                template_user_id: '',
                invite_quota_day: 0,
                invite_quota_month: 0,
                invite_quota: 0,
                invite_max_uses: 1,
                invite_link_validity_days: 0,
                invite_max_link_hours: 0,
                _ldap_dn: '',
                _ldap_dn_inviter: '',
            });
            openPresetModal(presets.length - 1);
        });

        document.getElementById('presets-body')?.addEventListener('click', async (event) => {
            const button = event.target.closest('button');
            if (!button) return;
            const index = parseInt(button.dataset.index || '-1', 10);
            if (!Number.isInteger(index) || index < 0) return;
            
            if (button.dataset.action === 'preset-delete') {
                const agreed = await confirmAction('Supprimer ce preset', 'Cette action va rendre caduc le preset pour les utilisateurs assignés.');
                if (!agreed) return;

                const deletedPresetID = presets[index].id;
                presets.splice(index, 1);
                
                // Clean payload
                const payload = presets.map(p => {
                    const cleaned = {...p};
                    delete cleaned._ldap_dn;
                    delete cleaned._ldap_dn_inviter;
                    return cleaned;
                });
                
                const res = await JG.api('/admin/api/automation/presets', {
                    method: 'POST',
                    body: JSON.stringify(payload),
                });
                
                if (!res.success) {
                    JG.toast('Erreur lors de la suppression', 'error');
                    await loadPresets();
                    return;
                }
                
                // Also update mappings by just sending the alive ones
                const mappingsPayload = [];
                presets.forEach(p => {
                    const presetLabel = String(p.name || p.id || 'Preset').trim();

                    mappingsPayload.push({
                        group_name: presetLabel,
                        source: 'internal',
                        ldap_group_dn: '',
                        policy_preset_id: p.id
                    });
                    if (p._ldap_dn) {
                        mappingsPayload.push({
                            group_name: `${presetLabel} (LDAP users)`,
                            source: 'ldap',
                            ldap_group_dn: p._ldap_dn,
                            policy_preset_id: p.id
                        });
                    }
                    if (p._ldap_dn_inviter) {
                        mappingsPayload.push({
                            group_name: `${presetLabel} (LDAP parrainage)`,
                            source: 'ldap',
                            ldap_group_dn: p._ldap_dn_inviter,
                            policy_preset_id: p.id
                        });
                    }
                });
                await JG.api('/admin/api/automation/group-mappings', {
                    method: 'POST',
                    body: JSON.stringify(mappingsPayload),
                });

                JG.toast('Preset supprimé', 'success');
                renderPresets();
            } else if (button.dataset.action === 'preset-edit') {
                openPresetModal(index);
            }
        });

        document.getElementById('btn-open-task-modal')?.addEventListener('click', () => {
            document.getElementById('task-id').value = '';
            document.getElementById('task-create-form').reset();
            document.getElementById('task-enabled').checked = true;
            updateTaskPreview();
            JG.openModal('modal-task-form');
        });

        document.getElementById('task-create-form')?.addEventListener('submit', async (event) => {
            event.preventDefault();
            const id = document.getElementById('task-id').value;
            const payload = {
                name: document.getElementById('task-name').value.trim(),
                task_type: document.getElementById('task-type').value,
                enabled: document.getElementById('task-enabled').checked,
                hour: parseInt(document.getElementById('task-hour').value || '0', 10),
                minute: parseInt(document.getElementById('task-minute').value || '0', 10),
                payload: document.getElementById('task-payload').value.trim(),
            };

            const method = id ? 'PATCH' : 'POST';
            const url = id ? `/admin/api/automation/tasks/${id}` : '/admin/api/automation/tasks';

            const res = await JG.api(url, {
                method: method,
                body: JSON.stringify(payload),
            });
            if (!res.success) {
                JG.toast(res.message || (id ? i18n.taskUpdateFailed : i18n.taskCreateFailed), 'error');
                return;
            }
            JG.toast(id ? i18n.taskUpdated || 'Tâche mise à jour' : i18n.taskCreated, 'success');
            event.target.reset();
            document.getElementById('task-enabled').checked = true;
            updateTaskPreview();
            if (typeof JG.closeModal === 'function') {
                JG.closeModal('modal-task-form');
            }
            await loadTasks();
        });

        document.getElementById('tasks-body')?.addEventListener('click', async (event) => {
            const button = event.target.closest('button');
            if (!button) {
                return;
            }
            const id = button.dataset.id;
            const action = button.dataset.action;
            const task = tasks.find((entry) => String(entry.id) === String(id));

            if (action === 'task-delete') {
                const agreed = await confirmAction('Supprimer la tâche', 'Êtes-vous sûr de vouloir supprimer cette tâche planifiée ?');
                if (!agreed) {
                    return;
                }
                const res = await JG.api(`/admin/api/automation/tasks/${id}`, { method: 'DELETE' });
                if (!res.success) {
                    JG.toast(res.message || i18n.taskDeleteFailed, 'error');
                    return;
                }
                JG.toast(i18n.taskDeleted, 'success');
                await loadTasks();
                return;
            }

            if (action === 'task-run') {
                const res = await JG.api(`/admin/api/automation/tasks/${id}/run`, { method: 'POST' });
                if (!res.success) {
                    JG.toast(res.message || i18n.taskRunFailed, 'error');
                    return;
                }
                JG.toast(i18n.taskRunSuccess, 'success');
                await loadTasks();
                return;
            }

            if (action === 'task-edit' && task) {
                document.getElementById('task-id').value = task.id;
                document.getElementById('task-name').value = task.name || '';
                document.getElementById('task-type').value = task.task_type || 'sync_users';
                document.getElementById('task-hour').value = task.hour;
                document.getElementById('task-minute').value = task.minute;
                document.getElementById('task-payload').value = task.payload || '';
                document.getElementById('task-enabled').checked = !!task.enabled;
                updateTaskPreview();
                JG.openModal('modal-task-form');
                return;
            }
            if (action === 'task-toggle' && task) {
                const res = await JG.api(`/admin/api/automation/tasks/${id}`, {
                    method: 'PATCH',
                    body: JSON.stringify({
                        name: task.name,
                        task_type: task.task_type,
                        enabled: !task.enabled,
                        hour: task.hour,
                        minute: task.minute,
                        payload: task.payload || '',
                    }),
                });
                if (!res.success) {
                    JG.toast(res.message || i18n.taskUpdateFailed, 'error');
                    return;
                }
                await loadTasks();
            }
        });

        const toggle = document.getElementById('sidebar-toggle');

        // Sidebar toggle
        if (toggle) {
            toggle.addEventListener('click', () => {
                const sidebar = document.getElementById('sidebar');
                if (sidebar) sidebar.classList.toggle('open');
            });
        }

        ['task-name', 'task-type', 'task-hour', 'task-minute', 'task-payload', 'task-enabled'].forEach((id) => {
            const element = document.getElementById(id);
            if (!element) {
                return;
            }
            element.addEventListener('input', updateTaskPreview);
            element.addEventListener('change', updateTaskPreview);
        });

        (async () => {
            updateTaskPreview();
            await loadPresets();
            await loadTemplateUsers();
            await loadMappings();
            await loadTasks();
        })();
    });
})();