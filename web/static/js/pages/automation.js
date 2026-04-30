(() => {
    const config = window.JGPageAutomation || {};
    const i18n = config.i18n || {};
    const taskTypeDescriptions = config.taskTypeDescriptions || {};

    function nonNegativeInt(value, fallback = 0) {
        const parsed = Number.parseInt(value, 10);
        return Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback;
    }

    const DEFAULT_HOME_SECTIONS = ['smalllibrarytiles', 'resume', 'resumeaudio', 'resumebook', 'livetv', 'nextup', 'latestmedia', 'none', 'none', 'none'];
    const HOME_SECTION_OPTIONS = [
        ['none', 'homeSectionNone'],
        ['smalllibrarytiles', 'homeSectionSmallLibraryTiles'],
        ['librarybuttons', 'homeSectionLibraryButtons'],
        ['activerecordings', 'homeSectionActiveRecordings'],
        ['resume', 'homeSectionResume'],
        ['resumeaudio', 'homeSectionResumeAudio'],
        ['resumebook', 'homeSectionResumeBook'],
        ['livetv', 'homeSectionLiveTv'],
        ['nextup', 'homeSectionNextUp'],
        ['latestmedia', 'homeSectionLatestMedia'],
    ];

    function presetInt(preset, key, fallback = 0) {
        const value = preset ? preset[key] : undefined;
        return Number.isInteger(value) && value >= 0 ? value : fallback;
    }

    function defaultUserConfiguration() {
        return {
            display_missing_episodes: false,
            hide_played_in_latest: false,
            ordered_views: [],
            grouped_folders: [],
            my_media_excludes: [],
            latest_items_excludes: [],
        };
    }

    function defaultDisplayPreferences() {
        return {
            screensaver: 'none',
            screensaver_time: 180,
            backdrop_screensaver_interval: 5,
            slideshow_interval: 5,
            enable_fast_fadein: true,
            enable_blurhash: true,
            enable_backdrops: false,
            enable_theme_songs: false,
            enable_theme_videos: false,
            details_banner: true,
            library_page_size: 100,
            max_days_for_next_up: 365,
            enable_rewatching_next_up: false,
            use_episode_images_next_up_resume: true,
            home_sections: DEFAULT_HOME_SECTIONS.slice(),
        };
    }

    function normalizePresetSettings(preset) {
        const normalized = preset || {};
        normalized.enable_all_folders = normalized.enable_all_folders !== false;
        normalized.enabled_folder_ids = Array.isArray(normalized.enabled_folder_ids) ? normalized.enabled_folder_ids : [];
        normalized.user_configuration = { ...defaultUserConfiguration(), ...(normalized.user_configuration || {}) };
        normalized.display_preferences = { ...defaultDisplayPreferences(), ...(normalized.display_preferences || {}) };
        if (!Array.isArray(normalized.display_preferences.home_sections) || !normalized.display_preferences.home_sections.length) {
            normalized.display_preferences.home_sections = DEFAULT_HOME_SECTIONS.slice();
        }
        while (normalized.display_preferences.home_sections.length < 10) {
            normalized.display_preferences.home_sections.push('none');
        }
        normalized.display_preferences.home_sections = normalized.display_preferences.home_sections.slice(0, 10);
        return normalized;
    }

    document.addEventListener('DOMContentLoaded', () => {
        // --- CSP Compliant Modal Handlers ---
        document.addEventListener("click", (e) => {
            const openBtn = e.target.closest("#btn-open-task-modal");
            if (openBtn) {
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
            if (typeof JG.confirm === 'function') {
                return JG.confirm(title, message);
            }
            return Promise.resolve(false);
        }

        let presets = [];
        let groupMappings = [];
        let tasks = [];
        let libraries = [];
        let librariesLoaded = false;


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
                previewType.textContent = type || '-';
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

        function showQuickTaskStatus(message, type = 'info') {
            const box = document.getElementById('quick-task-status');
            if (!box) {
                return;
            }
            box.classList.remove('hidden', 'border-sky-500/30', 'bg-sky-500/10', 'text-sky-200', 'border-emerald-500/30', 'bg-emerald-500/10', 'text-emerald-200', 'border-rose-500/30', 'bg-rose-500/10', 'text-rose-200');
            if (type === 'success') {
                box.classList.add('border-emerald-500/30', 'bg-emerald-500/10', 'text-emerald-200');
            } else if (type === 'error') {
                box.classList.add('border-rose-500/30', 'bg-rose-500/10', 'text-rose-200');
            } else {
                box.classList.add('border-sky-500/30', 'bg-sky-500/10', 'text-sky-200');
            }
            box.textContent = message;
        }

        function taskMessage(template, label, taskName) {
            return String(template || '')
                .split('{label}').join(label)
                .split('{task}').join(taskName || label);
        }

        function findTaskByType(taskType) {
            const normalized = String(taskType || '').trim().toLowerCase();
            if (!normalized) {
                return null;
            }
            const enabledTask = tasks.find((task) => String(task.task_type || '').trim().toLowerCase() === normalized && !!task.enabled);
            if (enabledTask) {
                return enabledTask;
            }
            return tasks.find((task) => String(task.task_type || '').trim().toLowerCase() === normalized) || null;
        }

        async function runQuickTask(taskType, label) {
            let target = findTaskByType(taskType);
            if (!target) {
                await loadTasks();
                target = findTaskByType(taskType);
            }

            if (!target) {
                if (String(taskType || '').trim().toLowerCase() === 'create_backup') {
                    const runningMsg = taskMessage(i18n.quickTaskRunning, label);
                    showQuickTaskStatus(runningMsg, 'info');
                    const res = await JG.api('/admin/api/backups/create', { method: 'POST' });
                    if (!res.success) {
                        const failedMsg = taskMessage(i18n.quickTaskFailed, label);
                        showQuickTaskStatus(res.message || failedMsg, 'error');
                        return;
                    }
                    const successMsg = taskMessage(i18n.quickTaskSuccess, label);
                    showQuickTaskStatus(successMsg, 'success');
                    return;
                }
                const msg = taskMessage(i18n.quickTaskMissing, label);
                showQuickTaskStatus(msg, 'error');
                return;
            }

            const runningMsg = taskMessage(i18n.quickTaskRunning, label, String(target.name || target.id));
            showQuickTaskStatus(runningMsg, 'info');
            const res = await JG.api(`/admin/api/automation/tasks/${target.id}/run`, { method: 'POST' });
            if (!res.success) {
                const failedMsg = taskMessage(i18n.quickTaskFailed, label, String(target.name || target.id));
                showQuickTaskStatus(res.message || failedMsg, 'error');
                return;
            }

            const successMsg = taskMessage(i18n.quickTaskSuccess, label, String(target.name || target.id));
            showQuickTaskStatus(successMsg, 'success');
            await loadTasks();
        }

        function presetRow(preset, idx) {
            const downloadBadge = preset.enable_download 
                ? `<span class="badge-success text-[10px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider">${JG.esc(i18n.allowedLabel)}</span>`
                : `<span class="badge-danger text-[10px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider">${JG.esc(i18n.deniedLabel)}</span>`;
            const remoteBadge = preset.enable_remote_access 
                ? `<span class="badge-success text-[10px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider">${JG.esc(i18n.allowedLabel)}</span>`
                : `<span class="badge-danger text-[10px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider">${JG.esc(i18n.deniedLabel)}</span>`;

            return `<tr class="hover:bg-white/[0.02] transition-colors border-b border-jg-border last:border-none">
            <td class="px-6 py-4"><code class="text-[10px] bg-white/5 px-2 py-1 rounded-md text-jg-text-muted border border-white/5">${JG.esc(preset.id || '')}</code></td>
            <td class="px-6 py-4 font-bold text-jg-text">${JG.esc(preset.name || '')}</td>
            <td class="px-6 py-4">${downloadBadge}</td>
            <td class="px-6 py-4">${remoteBadge}</td>
            <td class="px-6 py-4"><span class="text-sm font-medium text-jg-text">${Number.isInteger(preset.max_sessions) ? preset.max_sessions : 0}</span> <span class="text-[10px] text-jg-text-muted uppercase tracking-tighter ml-1">${JG.esc(i18n.streamsUnit)}</span></td>
            <td class="px-6 py-4"><span class="text-sm font-medium text-jg-text">${Number.isInteger(preset.bitrate_limit) ? preset.bitrate_limit : 0}</span> <span class="text-[10px] text-jg-text-muted uppercase tracking-tighter ml-1">Mbps</span></td>
            <td class="px-6 py-4 text-right">
                <div class="flex justify-end gap-2">
                    <button class="jg-btn jg-btn-sm jg-btn-ghost hover:bg-white/10" data-action="preset-edit" data-index="${idx}">${JG.esc(i18n.editLabel)}</button>
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
                 const agreed = await confirmAction(i18n.deleteLabel, i18n.mappingDeleteConfirm);
                 if (!agreed) return;
                 groupMappings.splice(idx, 1);
                 renderMappings();
             }
        });

        document.getElementById('preset-enable-all-folders')?.addEventListener('change', updateLibraryAccessState);
        document.getElementById('preset-library-list')?.addEventListener('click', (e) => {
            const button = e.target.closest('.preset-library-move');
            if (!button) return;
            e.preventDefault();
            moveLibraryRow(button);
        });
        document.getElementById('preset-library-list')?.addEventListener('change', (e) => {
            if (e.target?.classList?.contains('preset-library-access')) {
                updateLibraryAccessState();
            }
        });

        function mappingRow(mapping, idx) {
            const presetOptions = presets.map(p => `<option value="${JG.esc(p.id)}" ${p.id === mapping.policy_preset_id ? 'selected' : ''}>${JG.esc(p.name || p.id)}</option>`).join('');
            
            return `<tr>
                <td><input type="text" class="jg-input jg-input-sm" data-field="group_name" value="${JG.esc(mapping.group_name || '')}" placeholder="${JG.esc(i18n.groupNamePlaceholder)}"></td>
                <td>
                    <select class="jg-input jg-input-sm jg-select-premium py-0" data-field="source">
                        <option value="internal" ${mapping.source === 'internal' ? 'selected' : ''}>${JG.esc(i18n.sourceInternal)}</option>
                        <option value="ldap" ${mapping.source === 'ldap' ? 'selected' : ''}>${JG.esc(i18n.sourceLdap)}</option>
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
                // If we are currently loading presets, they will trigger a re-render.
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

        async function loadLibraries() {
            const container = document.getElementById('preset-library-list');
            if (container) {
                container.innerHTML = `<div class="p-4 text-sm text-jg-text-muted">${JG.esc(i18n.librariesLoading)}</div>`;
            }
            const res = await JG.api('/admin/api/automation/libraries');
            librariesLoaded = true;
            if (!res?.success) {
                libraries = [];
                if (container) {
                    container.innerHTML = `<div class="p-4 text-sm text-rose-300">${JG.esc(res?.message || i18n.librariesLoadFailed)}</div>`;
                }
                return;
            }
            libraries = Array.isArray(res.data) ? res.data : [];
        }

        function updateLibraryAccessState() {
            const allFolders = !!document.getElementById('preset-enable-all-folders')?.checked;
            document.querySelectorAll('#preset-library-list tr[data-library-id]').forEach((row) => {
                const accessInput = row.querySelector('.preset-library-access');
                const rowHasAccess = allFolders || !!accessInput?.checked;
                if (accessInput) {
                    accessInput.disabled = allFolders;
                }
                row.classList.toggle('opacity-60', !rowHasAccess);
                row.querySelectorAll('.preset-library-my-media, .preset-library-latest, .preset-library-group, .preset-library-move').forEach((input) => {
                    input.disabled = !rowHasAccess;
                });
            });
            updateLibraryOrderControls();
        }

        function updateLibraryOrderControls() {
            const rows = Array.from(document.querySelectorAll('#preset-library-list tr[data-library-id]'));
            rows.forEach((row, index) => {
                const rowHasAccess = !row.classList.contains('opacity-60');
                const up = row.querySelector('.preset-library-move[data-direction="-1"]');
                const down = row.querySelector('.preset-library-move[data-direction="1"]');
                if (up) up.disabled = !rowHasAccess || index === 0;
                if (down) down.disabled = !rowHasAccess || index === rows.length - 1;
            });
        }

        function moveLibraryRow(button) {
            const row = button.closest('tr');
            const tbody = row?.parentElement;
            if (!row || !tbody || button.disabled) return;

            const direction = Number.parseInt(button.dataset.direction, 10) || 0;
            if (direction < 0 && row.previousElementSibling) {
                tbody.insertBefore(row, row.previousElementSibling);
            }
            if (direction > 0 && row.nextElementSibling) {
                tbody.insertBefore(row.nextElementSibling, row);
            }
            updateLibraryOrderControls();
        }

        function renderHomeSections(homeSections) {
            const container = document.getElementById('preset-home-sections');
            if (!container) return;
            const sections = Array.isArray(homeSections) ? homeSections.slice(0, 10) : DEFAULT_HOME_SECTIONS.slice();
            while (sections.length < 10) sections.push('none');
            container.innerHTML = sections.map((value, idx) => {
                const options = HOME_SECTION_OPTIONS.map(([optionValue, labelKey]) => (
                    `<option value="${JG.esc(optionValue)}" ${optionValue === value ? 'selected' : ''}>${JG.esc(i18n[labelKey] || optionValue)}</option>`
                )).join('');
                return `<div>
                    <label class="jg-label" for="preset-home-section-${idx}">${JG.esc(i18n.homeSectionLabel)} ${idx + 1}</label>
                    <select id="preset-home-section-${idx}" class="jg-input jg-select-premium h-10 bg-black/20 text-sm">${options}</select>
                </div>`;
            }).join('');
        }

        function renderLibraryPicker(preset) {
            const container = document.getElementById('preset-library-list');
            if (!container) return;
            if (!librariesLoaded) {
                container.innerHTML = `<div class="p-4 text-sm text-jg-text-muted">${JG.esc(i18n.librariesLoading)}</div>`;
                return;
            }
            if (!libraries.length) {
                container.innerHTML = `<div class="p-4 text-sm text-jg-text-muted">${JG.esc(i18n.librariesEmpty)}</div>`;
                return;
            }

            const userConfig = preset.user_configuration || defaultUserConfiguration();
            const selected = new Set((preset.enabled_folder_ids || []).map(String));
            const grouped = new Set((userConfig.grouped_folders || []).map(String));
            const myMediaExcludes = new Set((userConfig.my_media_excludes || []).map(String));
            const latestExcludes = new Set((userConfig.latest_items_excludes || []).map(String));
            const libraryID = (library) => String(library.id || library.Id || library.ItemId || '').trim();
            const libraryByID = new Map(libraries.map((library) => [libraryID(library), library]).filter(([id]) => id));
            const seenOrdered = new Set();
            const orderedLibraries = [];
            (userConfig.ordered_views || []).forEach((id) => {
                const normalizedID = String(id).trim();
                const library = libraryByID.get(normalizedID);
                if (library && !seenOrdered.has(normalizedID)) {
                    orderedLibraries.push(library);
                    seenOrdered.add(normalizedID);
                }
            });
            libraries.forEach((library) => {
                const id = libraryID(library);
                if (id && !seenOrdered.has(id)) {
                    orderedLibraries.push(library);
                    seenOrdered.add(id);
                }
            });

            const rows = orderedLibraries.map((library) => {
                const id = String(library.id || library.Id || library.ItemId || '').trim();
                const label = library.name || library.Name || id;
                const type = library.collection_type || library.CollectionType || '';
                return `<tr data-library-id="${JG.esc(id)}" class="border-t border-white/5">
                    <td class="px-3 py-2 min-w-[170px]">
                        <div class="text-sm font-semibold text-jg-text">${JG.esc(label)}</div>
                        <div class="text-[10px] uppercase tracking-widest text-jg-text-muted">${JG.esc(type || id)}</div>
                    </td>
                    <td class="px-3 py-2 text-center"><input type="checkbox" class="preset-library-access form-checkbox w-4 h-4 rounded border-jg-border bg-black/50 accent-jg-accent" ${preset.enable_all_folders || selected.has(id) ? 'checked' : ''}></td>
                    <td class="px-3 py-2 text-center"><input type="checkbox" class="preset-library-my-media form-checkbox w-4 h-4 rounded border-jg-border bg-black/50 accent-jg-accent" title="${JG.esc(i18n.libraryHelpMyMedia || '')}" ${!myMediaExcludes.has(id) ? 'checked' : ''}></td>
                    <td class="px-3 py-2 text-center"><input type="checkbox" class="preset-library-latest form-checkbox w-4 h-4 rounded border-jg-border bg-black/50 accent-jg-accent" title="${JG.esc(i18n.libraryHelpLatest || '')}" ${!latestExcludes.has(id) ? 'checked' : ''}></td>
                    <td class="px-3 py-2 text-center"><input type="checkbox" class="preset-library-group form-checkbox w-4 h-4 rounded border-jg-border bg-black/50 accent-jg-accent" title="${JG.esc(i18n.libraryHelpGroup || '')}" ${grouped.has(id) ? 'checked' : ''}></td>
                    <td class="px-3 py-2">
                        <div class="flex items-center gap-1">
                            <button type="button" class="preset-library-move jg-btn jg-btn-sm jg-btn-ghost h-8 w-8 px-0" data-direction="-1" title="${JG.esc(i18n.libraryMoveUp || '')}" aria-label="${JG.esc(i18n.libraryMoveUp || '')}">&uarr;</button>
                            <button type="button" class="preset-library-move jg-btn jg-btn-sm jg-btn-ghost h-8 w-8 px-0" data-direction="1" title="${JG.esc(i18n.libraryMoveDown || '')}" aria-label="${JG.esc(i18n.libraryMoveDown || '')}">&darr;</button>
                        </div>
                    </td>
                </tr>`;
            }).join('');

            container.innerHTML = `<table class="w-full text-left text-sm">
                <thead class="text-[10px] uppercase tracking-widest text-jg-text-muted bg-white/[0.03]">
                    <tr>
                        <th class="px-3 py-3"></th>
                        <th class="px-3 py-3 text-center">${JG.esc(i18n.libraryColAccess)}</th>
                        <th class="px-3 py-3 text-center" title="${JG.esc(i18n.libraryHelpMyMedia || '')}">${JG.esc(i18n.libraryColMyMedia)}</th>
                        <th class="px-3 py-3 text-center" title="${JG.esc(i18n.libraryHelpLatest || '')}">${JG.esc(i18n.libraryColLatest)}</th>
                        <th class="px-3 py-3 text-center" title="${JG.esc(i18n.libraryHelpGroup || '')}">${JG.esc(i18n.libraryColGroup)}</th>
                        <th class="px-3 py-3">${JG.esc(i18n.libraryColOrder)}</th>
                    </tr>
                </thead>
                <tbody>${rows}</tbody>
            </table>`;
            updateLibraryAccessState();
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
            const preset = normalizePresetSettings(presets[idx] || {});
            presets[idx] = preset;
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
            document.getElementById('preset-enable-all-folders').checked = preset.enable_all_folders !== false;
            renderLibraryPicker(preset);
            renderHomeSections(preset.display_preferences.home_sections);

            const userConfig = preset.user_configuration;
            document.getElementById('preset-hide-played-latest').checked = !!userConfig.hide_played_in_latest;
            document.getElementById('preset-display-missing-episodes').checked = !!userConfig.display_missing_episodes;

            const displayPrefs = preset.display_preferences;
            document.getElementById('preset-screensaver').value = displayPrefs.screensaver || 'none';
            document.getElementById('preset-screensaver-time').value = presetInt(displayPrefs, 'screensaver_time', 180);
            document.getElementById('preset-backdrop-interval').value = presetInt(displayPrefs, 'backdrop_screensaver_interval', 5);
            document.getElementById('preset-slideshow-interval').value = presetInt(displayPrefs, 'slideshow_interval', 5);
            document.getElementById('preset-library-page-size').value = presetInt(displayPrefs, 'library_page_size', 100);
            document.getElementById('preset-nextup-days').value = presetInt(displayPrefs, 'max_days_for_next_up', 365);
            document.getElementById('preset-fast-fadein').checked = displayPrefs.enable_fast_fadein !== false;
            document.getElementById('preset-blurhash').checked = displayPrefs.enable_blurhash !== false;
            document.getElementById('preset-enable-backdrops').checked = !!displayPrefs.enable_backdrops;
            document.getElementById('preset-theme-songs').checked = !!displayPrefs.enable_theme_songs;
            document.getElementById('preset-theme-videos').checked = !!displayPrefs.enable_theme_videos;
            document.getElementById('preset-details-banner').checked = displayPrefs.details_banner !== false;
            document.getElementById('preset-rewatch-nextup').checked = !!displayPrefs.enable_rewatching_next_up;
            document.getElementById('preset-episode-images').checked = displayPrefs.use_episode_images_next_up_resume !== false;
            
            // Sponsorship settings
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
            if (quotaDayEl) quotaDayEl.value = presetInt(preset, 'invite_quota_day', 0);

            const quotaMonthEl = document.getElementById('preset-invite-quota-month');
            if (quotaMonthEl) {
                quotaMonthEl.value = Number.isInteger(preset.invite_quota_month)
                    ? Math.max(0, preset.invite_quota_month)
                    : presetInt(preset, 'invite_quota', 0);
            }

            const maxUsesEl = document.getElementById('preset-invite-max-uses');
            if (maxUsesEl) maxUsesEl.value = presetInt(preset, 'invite_max_uses', 0);

            const linkDaysEl = document.getElementById('preset-invite-link-days');
            if (linkDaysEl) {
                const linkDays = Number.isInteger(preset.invite_link_validity_days)
                    ? Math.max(0, preset.invite_link_validity_days)
                    : (preset.invite_max_link_hours ? Math.max(1, Math.ceil(preset.invite_max_link_hours / 24)) : 0);
                linkDaysEl.value = linkDays;
            }

            const allowLanguageEl = document.getElementById('preset-invite-allow-language');
            if (allowLanguageEl) allowLanguageEl.checked = !!preset.invite_allow_language;
            
            const targetSelect = document.getElementById('preset-target-preset');
            if (targetSelect) {
                targetSelect.innerHTML = `<option value="">${JG.esc(i18n.targetSamePreset)}</option>` + 
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
            presets[idx].template_user_id = '';
            presets[idx].enable_all_folders = !!document.getElementById('preset-enable-all-folders')?.checked;
            const libraryRows = Array.from(document.querySelectorAll('#preset-library-list tr[data-library-id]'));
            const activeLibraryRows = presets[idx].enable_all_folders
                ? libraryRows
                : libraryRows.filter((row) => row.querySelector('.preset-library-access')?.checked);
            presets[idx].enabled_folder_ids = presets[idx].enable_all_folders
                ? []
                : activeLibraryRows
                    .map((row) => row.dataset.libraryId)
                    .filter(Boolean);
            presets[idx].user_configuration = {
                display_missing_episodes: !!document.getElementById('preset-display-missing-episodes')?.checked,
                hide_played_in_latest: !!document.getElementById('preset-hide-played-latest')?.checked,
                ordered_views: activeLibraryRows
                    .map((row) => row.dataset.libraryId)
                    .filter(Boolean),
                grouped_folders: activeLibraryRows
                    .filter((row) => row.querySelector('.preset-library-group')?.checked)
                    .map((row) => row.dataset.libraryId)
                    .filter(Boolean),
                my_media_excludes: activeLibraryRows
                    .filter((row) => !row.querySelector('.preset-library-my-media')?.checked)
                    .map((row) => row.dataset.libraryId)
                    .filter(Boolean),
                latest_items_excludes: activeLibraryRows
                    .filter((row) => !row.querySelector('.preset-library-latest')?.checked)
                    .map((row) => row.dataset.libraryId)
                    .filter(Boolean),
            };
            presets[idx].display_preferences = {
                screensaver: document.getElementById('preset-screensaver')?.value || 'none',
                screensaver_time: nonNegativeInt(document.getElementById('preset-screensaver-time')?.value, 180),
                backdrop_screensaver_interval: nonNegativeInt(document.getElementById('preset-backdrop-interval')?.value, 5),
                slideshow_interval: nonNegativeInt(document.getElementById('preset-slideshow-interval')?.value, 5),
                enable_fast_fadein: !!document.getElementById('preset-fast-fadein')?.checked,
                enable_blurhash: !!document.getElementById('preset-blurhash')?.checked,
                enable_backdrops: !!document.getElementById('preset-enable-backdrops')?.checked,
                enable_theme_songs: !!document.getElementById('preset-theme-songs')?.checked,
                enable_theme_videos: !!document.getElementById('preset-theme-videos')?.checked,
                details_banner: !!document.getElementById('preset-details-banner')?.checked,
                library_page_size: nonNegativeInt(document.getElementById('preset-library-page-size')?.value, 100),
                max_days_for_next_up: nonNegativeInt(document.getElementById('preset-nextup-days')?.value, 365),
                enable_rewatching_next_up: !!document.getElementById('preset-rewatch-nextup')?.checked,
                use_episode_images_next_up_resume: !!document.getElementById('preset-episode-images')?.checked,
                home_sections: Array.from({ length: 10 }, (_, index) => document.getElementById(`preset-home-section-${index}`)?.value || 'none'),
            };
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
            if (maxUsesEl) presets[idx].invite_max_uses = nonNegativeInt(maxUsesEl.value, 0);

            const linkDaysEl = document.getElementById('preset-invite-link-days');
            const linkDays = linkDaysEl ? nonNegativeInt(linkDaysEl.value, 0) : 0;
            presets[idx].invite_link_validity_days = linkDays;
            presets[idx].invite_max_link_hours = linkDays > 0 ? linkDays * 24 : 0;

            const allowLanguageEl = document.getElementById('preset-invite-allow-language');
            if (allowLanguageEl) presets[idx].invite_allow_language = allowLanguageEl.checked;
            
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
                const presetLabel = String(p.name || p.id || i18n.defaultPresetName).trim();

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
                        group_name: `${presetLabel} ${i18n.mappingLdapUsersSuffix}`,
                        source: 'ldap',
                        ldap_group_dn: p._ldap_dn,
                        policy_preset_id: p.id
                    });
                }

                // LDAP sponsorship group mapping (if defined)
                if (p._ldap_dn_inviter) {
                    mappingsPayload.push({
                        group_name: `${presetLabel} ${i18n.mappingLdapInviterSuffix}`,
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
            const taskTypeLabels = {
                sync_users: i18n.taskTypeSyncUsers || i18n.manualSyncUsers || 'Sync users',
                sync_ldap_users: i18n.taskTypeSyncLdapUsers || i18n.manualSyncLdap || 'Sync LDAP users',
                cleanup_resets: i18n.taskTypeCleanupResets || i18n.manualCleanupResets || 'Clean reset links',
                create_backup: i18n.taskTypeCreateBackupAdvanced || i18n.manualBackupNow || 'Backup (advanced)',
            };
            const renderTaskTypeCell = (task) => {
                const taskType = String(task.task_type || '').trim();
                const label = taskTypeLabels[taskType] || taskType || '-';
                const badge = taskType === 'create_backup'
                    ? `<span class="ml-2 rounded-full border border-amber-400/30 bg-amber-500/10 px-2 py-0.5 text-[9px] font-black uppercase tracking-[0.16em] text-amber-200">${JG.esc(i18n.taskTypeAdvancedBadge || 'Advanced')}</span>`
                    : '';
                return `<div class="flex flex-wrap items-center gap-1.5"><span class="text-xs font-semibold text-jg-text">${JG.esc(label)}</span>${badge}</div><code class="mt-1 block text-[10px] text-jg-text-muted/70">${JG.esc(taskType)}</code>`;
            };
            tbody.innerHTML = tasks.map((task) => `<tr class="hover:bg-white/[0.02] transition-colors border-b border-jg-border last:border-none">
            <td class="px-6 py-4 font-bold text-jg-text">${JG.esc(task.name || '')}</td>
            <td class="px-6 py-4">${renderTaskTypeCell(task)}</td>
            <td class="px-6 py-4 text-jg-text font-medium">${String(task.hour).padStart(2, '0')}:${String(task.minute).padStart(2, '0')} ${task.enabled ? `<span class="bg-emerald-500/10 text-emerald-500 text-[10px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider ml-2">${JG.esc(i18n.statusOn)}</span>` : `<span class="bg-white/5 text-jg-text-muted text-[10px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider ml-2">${JG.esc(i18n.statusOff)}</span>`}</td>
            <td class="px-6 py-4"><code class="text-xs text-jg-text-muted opacity-60">${JG.esc(task.payload || '-')}</code></td>
            <td class="px-6 py-4 text-sm text-jg-text-muted">${JG.esc(task.last_run_at || '-')}</td>
            <td class="px-6 py-4 text-right">
                <div class="flex justify-end gap-2">
                    <button class="jg-btn jg-btn-sm jg-btn-ghost hover:bg-white/10" data-action="task-run" data-id="${task.id}">${JG.esc(i18n.runNow)}</button>
                    <button class="jg-btn jg-btn-sm jg-btn-ghost hover:bg-white/10" data-action="task-edit" data-id="${task.id}">${JG.esc(i18n.editLabel)}</button>
                    <button class="jg-btn jg-btn-sm jg-btn-ghost hover:bg-white/10" data-action="task-toggle" data-id="${task.id}">${task.enabled ? JG.esc(i18n.disable) : JG.esc(i18n.enable)}</button>
                    <button class="jg-btn jg-btn-sm jg-btn-danger/80 hover:bg-jg-danger transition-colors" data-action="task-delete" data-id="${task.id}">${JG.esc(i18n.deleteLabel)}</button>
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
                user_configuration: defaultUserConfiguration(),
                display_preferences: defaultDisplayPreferences(),
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
                invite_max_uses: 0,
                invite_link_validity_days: 0,
                invite_max_link_hours: 0,
                invite_allow_language: false,
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
                const agreed = await confirmAction(i18n.deleteLabel, i18n.presetDeleteConfirm);
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
                    JG.toast(res.message || i18n.presetDeleteFailed, 'error');
                    await loadPresets();
                    return;
                }
                
                // Also update mappings by just sending the alive ones
                const mappingsPayload = [];
                presets.forEach(p => {
                    const presetLabel = String(p.name || p.id || i18n.defaultPresetName).trim();

                    mappingsPayload.push({
                        group_name: presetLabel,
                        source: 'internal',
                        ldap_group_dn: '',
                        policy_preset_id: p.id
                    });
                    if (p._ldap_dn) {
                        mappingsPayload.push({
                            group_name: `${presetLabel} ${i18n.mappingLdapUsersSuffix}`,
                            source: 'ldap',
                            ldap_group_dn: p._ldap_dn,
                            policy_preset_id: p.id
                        });
                    }
                    if (p._ldap_dn_inviter) {
                        mappingsPayload.push({
                            group_name: `${presetLabel} ${i18n.mappingLdapInviterSuffix}`,
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

                JG.toast(i18n.presetDeleted, 'success');
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
            JG.toast(id ? i18n.taskUpdated : i18n.taskCreated, 'success');
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
                const agreed = await confirmAction(i18n.deleteLabel, i18n.taskDeleteConfirm);
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

        document.getElementById('btn-task-quick-sync-users')?.addEventListener('click', async () => {
            await runQuickTask('sync_users', i18n.manualSyncUsers);
        });
        document.getElementById('btn-task-quick-sync-ldap')?.addEventListener('click', async () => {
            await runQuickTask('sync_ldap_users', i18n.manualSyncLdap);
        });
        document.getElementById('btn-task-quick-cleanup')?.addEventListener('click', async () => {
            await runQuickTask('cleanup_resets', i18n.manualCleanupResets);
        });
        document.getElementById('btn-task-quick-backup')?.addEventListener('click', async () => {
            await runQuickTask('create_backup', i18n.manualBackupNow);
        });

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
            await loadLibraries();
            await loadMappings();
            await loadTasks();
        })();
    });
})();
