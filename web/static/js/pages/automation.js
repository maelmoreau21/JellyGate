(() => {
    const config = window.JGPageAutomation || {};
    const i18n = config.i18n || {};
    const taskTypeDescriptions = config.taskTypeDescriptions || {};

    document.addEventListener('DOMContentLoaded', () => {
        // --- CSP Compliant Modal Handlers ---
        document.addEventListener("click", (e) => {
            const openBtn = e.target.closest("#btn-open-task-modal");
            if (openBtn) {
                JG.openModal("modal-task-form");
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



        let presets = [];
        let groupMappings = [];
        let tasks = [];

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
            return `<tr>
            <td class="font-medium text-jg-text">${JG.esc(preset.id || '')}</td>
            <td>${JG.esc(preset.name || '')}</td>
            <td>${preset.enable_download ? '<span class="badge badge-success">Oui</span>' : '<span class="badge badge-danger">Non</span>'}</td>
            <td>${preset.enable_remote_access ? '<span class="badge badge-success">Oui</span>' : '<span class="badge badge-danger">Non</span>'}</td>
            <td>${Number.isInteger(preset.max_sessions) ? preset.max_sessions : 0}</td>
            <td>${Number.isInteger(preset.bitrate_limit) ? preset.bitrate_limit : 0}</td>
            <td class="text-right">
                <div class="flex justify-end gap-2">
                    <button class="jg-btn jg-btn-sm jg-btn-ghost" data-action="preset-edit" data-index="${idx}">${JG.esc(i18n.edit || 'Éditer')}</button>
                    <button class="jg-btn jg-btn-sm jg-btn-danger" data-action="preset-delete" data-index="${idx}">${JG.esc(i18n.deleteLabel)}</button>
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
                tbody.innerHTML = `<tr><td colspan="9" class="text-center text-slate-500 py-8">${JG.esc(i18n.noPresets)}</td></tr>`;
                return;
            }
            tbody.innerHTML = presets.map((preset, idx) => presetRow(preset, idx)).join('');
            renderGroupMappings();
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

        function collectPresetsFromUI() {
            return presets;
        }
        
        // Modal Preset Handlers
        let currentPresetIndex = -1;
        
        function openPresetModal(idx) {
            currentPresetIndex = idx;
            const preset = presets[idx] || {};
            document.getElementById('preset-id').value = preset.id || '';
            document.getElementById('preset-name').value = preset.name || '';
            document.getElementById('preset-enable-download').checked = !!preset.enable_download;
            document.getElementById('preset-enable-remote').checked = !!preset.enable_remote_access;
            document.getElementById('preset-max-sessions').value = preset.max_sessions || 0;
            document.getElementById('preset-bitrate').value = preset.bitrate_limit || 0;
            document.getElementById('preset-disable-days').value = preset.disable_after_days || 0;
            document.getElementById('preset-delete-days').value = preset.delete_after_days || 0;
            JG.openModal('modal-preset-form');
        }
        
        document.getElementById('preset-form-internal')?.addEventListener('submit', (e) => {
            e.preventDefault();
            const idx = currentPresetIndex;
            if (idx < 0 || idx >= presets.length) return;
            
            presets[idx].id = document.getElementById('preset-id').value.trim();
            presets[idx].name = document.getElementById('preset-name').value.trim();
            presets[idx].enable_download = document.getElementById('preset-enable-download').checked;
            presets[idx].enable_remote_access = document.getElementById('preset-enable-remote').checked;
            presets[idx].max_sessions = parseInt(document.getElementById('preset-max-sessions').value, 10) || 0;
            presets[idx].bitrate_limit = parseInt(document.getElementById('preset-bitrate').value, 10) || 0;
            presets[idx].disable_after_days = parseInt(document.getElementById('preset-disable-days').value, 10) || 0;
            presets[idx].delete_after_days = parseInt(document.getElementById('preset-delete-days').value, 10) || 0;
            
            renderPresets();
            JG.closeModal('modal-preset-form');
        });

        function groupPresetOptions(selectedID) {
            const options = [`<option value="">${JG.esc(i18n.selectPreset)}</option>`];
            presets.forEach((preset) => {
                const value = JG.esc(preset.id || '');
                const selected = (preset.id || '') === (selectedID || '') ? 'selected' : '';
                options.push(`<option value="${value}" ${selected}>${JG.esc(preset.name || preset.id || '')}</option>`);
            });
            return options.join('');
        }

        function groupMappingRow(mapping, idx) {
            return `<tr>
            <td><input class="jg-input" data-g="${idx}" data-k="group_name" value="${JG.esc(mapping.group_name || '')}" placeholder="Enfants"></td>
            <td>
                <select class="jg-input" data-g="${idx}" data-k="source">
                    <option value="internal" ${(mapping.source || 'internal') === 'internal' ? 'selected' : ''}>internal</option>
                    <option value="ldap" ${(mapping.source || '') === 'ldap' ? 'selected' : ''}>ldap</option>
                </select>
            </td>
            <td><input class="jg-input" data-g="${idx}" data-k="ldap_group_dn" value="${JG.esc(mapping.ldap_group_dn || '')}" placeholder="CN=Enfants,CN=Users,DC=home,DC=lan"></td>
            <td>
                <select class="jg-input" data-g="${idx}" data-k="policy_preset_id">
                    ${groupPresetOptions(mapping.policy_preset_id || '')}
                </select>
            </td>
            <td class="text-right"><button class="jg-btn jg-btn-sm jg-btn-danger" data-action="group-map-delete" data-index="${idx}">${JG.esc(i18n.deleteLabel)}</button></td>
        </tr>`;
        }

        function renderGroupMappings() {
            const tbody = document.getElementById('group-mappings-body');
            if (!tbody) {
                return;
            }
            updateOverview();
            if (!groupMappings.length) {
                tbody.innerHTML = `<tr><td colspan="5" class="text-center text-slate-500 py-8">${JG.esc(i18n.noGroupMappings)}</td></tr>`;
                return;
            }
            tbody.innerHTML = groupMappings.map((mapping, idx) => groupMappingRow(mapping, idx)).join('');
        }

        function collectGroupMappingsFromUI() {
            const rows = document.querySelectorAll('#group-mappings-body tr');
            const next = [];
            rows.forEach((_, idx) => {
                const read = (key) => document.querySelector(`[data-g="${idx}"][data-k="${key}"]`);
                const groupName = (read('group_name')?.value || '').trim();
                const source = (read('source')?.value || 'internal').trim();
                const ldapGroupDN = (read('ldap_group_dn')?.value || '').trim();
                const policyPresetID = (read('policy_preset_id')?.value || '').trim();
                if (!groupName || !policyPresetID) {
                    return;
                }
                next.push({
                    group_name: groupName,
                    source: source === 'ldap' ? 'ldap' : 'internal',
                    ldap_group_dn: ldapGroupDN,
                    policy_preset_id: policyPresetID,
                });
            });
            return next;
        }

        async function loadGroupMappings() {
            const res = await JG.api('/admin/api/automation/group-mappings');
            if (!res.success) {
                JG.toast(res.message || i18n.errorGroupMappings, 'error');
                return;
            }
            groupMappings = Array.isArray(res.data) ? res.data : [];
            renderGroupMappings();
        }

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
                id: `preset-${presets.length + 1}`,
                name: i18n.newPreset,
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
            });
            renderPresets();
            openPresetModal(presets.length - 1);
        });

        document.getElementById('btn-preset-save')?.addEventListener('click', async () => {
            const payload = collectPresetsFromUI();
            const res = await JG.api('/admin/api/automation/presets', {
                method: 'POST',
                body: JSON.stringify(payload),
            });
            if (!res.success) {
                JG.toast(res.message || i18n.savePresetsFailed, 'error');
                return;
            }
            JG.toast(i18n.presetsSaved, 'success');
            await loadPresets();
        });

        document.getElementById('presets-body')?.addEventListener('click', (event) => {
            const button = event.target.closest('button');
            if (!button) return;
            const index = parseInt(button.dataset.index || '-1', 10);
            if (!Number.isInteger(index) || index < 0) return;
            
            if (button.dataset.action === 'preset-delete') {
                presets.splice(index, 1);
                renderPresets();
            } else if (button.dataset.action === 'preset-edit') {
                openPresetModal(index);
            }
        });

        document.getElementById('btn-group-map-add')?.addEventListener('click', () => {
            groupMappings.push({
                group_name: '',
                source: 'internal',
                ldap_group_dn: '',
                policy_preset_id: presets[0]?.id || '',
            });
            renderGroupMappings();
        });

        document.getElementById('btn-group-map-save')?.addEventListener('click', async () => {
            const payload = collectGroupMappingsFromUI();
            const res = await JG.api('/admin/api/automation/group-mappings', {
                method: 'POST',
                body: JSON.stringify(payload),
            });
            if (!res.success) {
                JG.toast(res.message || i18n.saveMappingsFailed, 'error');
                return;
            }
            JG.toast(i18n.mappingsSaved, 'success');
            await loadGroupMappings();
        });

        document.getElementById('group-mappings-body')?.addEventListener('click', (event) => {
            const button = event.target.closest('button');
            if (!button || button.dataset.action !== 'group-map-delete') {
                return;
            }
            const index = parseInt(button.dataset.index || '-1', 10);
            if (!Number.isInteger(index) || index < 0) {
                return;
            }
            groupMappings.splice(index, 1);
            renderGroupMappings();
        });

        document.getElementById('task-create-form')?.addEventListener('submit', async (event) => {
            event.preventDefault();
            const payload = {
                name: document.getElementById('task-name').value.trim(),
                task_type: document.getElementById('task-type').value,
                enabled: document.getElementById('task-enabled').checked,
                hour: parseInt(document.getElementById('task-hour').value || '0', 10),
                minute: parseInt(document.getElementById('task-minute').value || '0', 10),
                payload: document.getElementById('task-payload').value.trim(),
            };

            const res = await JG.api('/admin/api/automation/tasks', {
                method: 'POST',
                body: JSON.stringify(payload),
            });
            if (!res.success) {
                JG.toast(res.message || i18n.taskCreateFailed, 'error');
                return;
            }
            JG.toast(i18n.taskCreated, 'success');
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
                if (!confirm(i18n.taskDeleteConfirm)) {
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
            await loadGroupMappings();
            await loadTasks();
        })();
    });
})();