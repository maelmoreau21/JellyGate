(() => {
    const config = window.JGPageProfiles || {};
    const i18n = config.i18n || {};
    const state = {
        presets: [],
        libraries: [],
        selected: 0,
        activeTab: 'rights',
        dirty: false,
        hydrating: false,
        search: '',
        filter: 'all',
    };

    const els = {};
    const qs = (id) => document.getElementById(id);
    const t = (key, fallback) => i18n[key] || fallback;
    const num = (id) => Math.max(0, parseInt(qs(id)?.value || '0', 10) || 0);
    const list = (id) => (qs(id)?.value || '')
        .split(/[\n,]+/)
        .map((v) => v.trim())
        .filter(Boolean);
    const setList = (id, values) => {
        const el = qs(id);
        if (el) el.value = (values || []).join('\n');
    };
    const nonNegativeInt = (value, fallback = 0) => {
        const parsed = Number.parseInt(value, 10);
        return Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback;
    };

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
            enable_backdrops: false,
            enable_theme_songs: false,
            enable_theme_videos: false,
            library_page_size: 100,
            home_sections: DEFAULT_HOME_SECTIONS.slice(),
        };
    }

    function normalizePresetSettings(preset) {
        const normalized = { ...(preset || {}) };
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

    function hydrateEls() {
        [
            'profiles-list', 'profiles-count', 'profiles-save-btn', 'profile-new-btn', 'profile-form',
            'profiles-stat-total', 'profiles-stat-admin', 'profiles-stat-sponsor', 'profiles-stat-temporary',
            'profile-search', 'profile-filter-kind', 'profile-drawer', 'profile-drawer-overlay', 'profile-drawer-close',
            'profile-index', 'profile-id', 'profile-name', 'profile-description', 'profile-admin',
            'profile-hidden', 'profile-disabled', 'profile-all-folders', 'profile-download', 'profile-remote',
            'profile-playback', 'profile-audio-transcode', 'profile-video-transcode', 'profile-remux',
            'profile-live-tv', 'profile-live-tv-management', 'profile-public-sharing', 'profile-content-deletion',
            'profile-sync-transcoding', 'profile-syncplay', 'profile-libraries', 'profile-blocked-folders',
            'profile-deletion-folders', 'profile-disable-days', 'profile-delete-days', 'profile-sessions',
            'profile-bitrate', 'profile-parental-rating', 'profile-invalid-login', 'profile-login-lockout',
            'profile-allowed-tags', 'profile-blocked-tags', 'profile-home-sections',
            'profile-hide-played-latest', 'profile-display-missing-episodes', 'profile-backdrops', 'profile-theme-songs',
            'profile-theme-videos', 'profile-page-size', 'profile-can-invite', 'profile-can-temp-invite',
            'profile-target-preset', 'profile-quota-day', 'profile-quota-month', 'profile-link-days',
            'profile-max-uses', 'profile-temp-default', 'profile-temp-max', 'profile-allowed-targets',
            'profile-allowed-temp-targets', 'profile-is-temporary', 'profile-account-default',
            'profile-account-max', 'profile-devices', 'profile-channels', 'profile-authentik-groups', 'profile-ldap-groups',
            'profile-delete-btn', 'profile-editor-title', 'profile-editor-subtitle', 'profile-tabs',
            'profile-active-menu-name', 'profile-dirty-indicator',
        ].forEach((id) => { els[id] = qs(id); });
    }

    function presetName(preset) {
        return preset?.name || preset?.id || t('profileFallback', 'Profil');
    }

    function setDirty(value) {
        if (state.hydrating) return;
        state.dirty = !!value;
        els['profile-dirty-indicator']?.classList.toggle('hidden', !state.dirty);
    }

    function activeTabLabel() {
        const button = Array.from(document.querySelectorAll('[data-profile-tab-target]'))
            .find((node) => node.dataset.profileTabTarget === state.activeTab);
        return button?.querySelector('.jg-profile-tab-label')?.textContent?.trim()
            || t('profileFallback', 'Profil');
    }

    function renderTabs(focusActive = false) {
        if (!document.querySelector('[data-profile-tab-target]')) return;
        document.querySelectorAll('[data-profile-tab]').forEach((panel) => {
            const active = panel.dataset.profileTab === state.activeTab;
            panel.classList.toggle('hidden', !active);
            panel.hidden = !active;
        });
        document.querySelectorAll('[data-profile-tab-target]').forEach((button) => {
            const active = button.dataset.profileTabTarget === state.activeTab;
            button.classList.toggle('active', active);
            button.setAttribute('aria-selected', active ? 'true' : 'false');
            button.setAttribute('tabindex', active ? '0' : '-1');
            if (active && focusActive) button.focus();
        });
        if (els['profile-active-menu-name']) {
            els['profile-active-menu-name'].textContent = activeTabLabel();
        }
    }

    function profileLibrariesLabel(preset) {
        if (preset.enable_all_folders) return t('allLibraries', 'Toutes bibliotheques');
        return t('libraryCount', '{count} bibliotheque(s)').replace('{count}', String((preset.enabled_folder_ids || []).length));
    }

    function updateStats() {
        const presets = state.presets || [];
        const adminCount = presets.filter((preset) => preset.is_administrator).length;
        const sponsorCount = presets.filter((preset) => preset.can_invite || preset.can_create_invitations).length;
        const temporaryCount = presets.filter((preset) => preset.is_temporary).length;
        if (els['profiles-stat-total']) els['profiles-stat-total'].textContent = String(presets.length);
        if (els['profiles-stat-admin']) els['profiles-stat-admin'].textContent = String(adminCount);
        if (els['profiles-stat-sponsor']) els['profiles-stat-sponsor'].textContent = String(sponsorCount);
        if (els['profiles-stat-temporary']) els['profiles-stat-temporary'].textContent = String(temporaryCount);
    }

    function filteredPresets() {
        const term = String(state.search || '').trim().toLowerCase();
        const kind = state.filter || 'all';
        return state.presets
            .map((preset, index) => ({ preset, index }))
            .filter(({ preset }) => {
                if (!term) return true;
                return [preset.id, preset.name, preset.description]
                    .some((value) => String(value || '').toLowerCase().includes(term));
            })
            .filter(({ preset }) => {
                if (kind === 'admin') return !!preset.is_administrator;
                if (kind === 'sponsor') return !!(preset.can_invite || preset.can_create_invitations);
                if (kind === 'temporary') return !!preset.is_temporary;
                return true;
            });
    }

    function renderList() {
        if (!els['profiles-list']) return;
        updateStats();
        const rows = filteredPresets();
        if (els['profiles-count']) {
            els['profiles-count'].textContent = rows.length === state.presets.length
                ? String(state.presets.length)
                : `${rows.length}/${state.presets.length}`;
        }
        if (!rows.length) {
            els['profiles-list'].innerHTML = `<tr><td colspan="5" class="text-center py-16 text-jg-text-muted">${JG.esc(t('noMatch', 'Aucun resultat'))}</td></tr>`;
            return;
        }
        els['profiles-list'].innerHTML = rows.map(({ preset, index }) => {
            const active = index === state.selected ? 'is-selected' : '';
            const libs = profileLibrariesLabel(preset);
            const admin = preset.is_administrator ? `<span class="badge badge-danger">${JG.esc(t('tagAdmin', 'Admin'))}</span>` : '';
            const invite = (preset.can_invite || preset.can_create_invitations) ? `<span class="badge badge-accent">${JG.esc(t('tagSponsor', 'Parrain'))}</span>` : '';
            const temp = preset.is_temporary ? `<span class="badge badge-warning">${JG.esc(t('tagTemporary', 'Temp'))}</span>` : '';
            const rights = [
                preset.enable_remote_access !== false ? t('profiles_field_remote', 'Remote') : '',
                preset.enable_download ? t('tagDownload', 'Download') : '',
                preset.enable_media_playback !== false ? t('profiles_field_playback', 'Lecture') : '',
            ].filter(Boolean).join(' · ') || '—';
            const inviteLabel = (preset.can_invite || preset.can_create_invitations)
                ? `<span class="badge badge-accent">${t('tagSponsor', 'Parrain')}${preset.invite_quota_day ? ` (${preset.invite_quota_day}/j)` : ''}</span>`
                : `<span class="text-xs text-jg-text-muted font-medium">${t('none', 'Aucun')}</span>`;
            return `<tr class="jg-profile-row ${active} transition-colors" data-profile-index="${index}" aria-current="${index === state.selected ? 'true' : 'false'}">
                <td data-label="${JG.esc(t('tableName', 'Profil'))}" class="px-5 py-4">
                    <div class="font-bold text-slate-100 text-sm">${JG.esc(presetName(preset))}</div>
                    <div class="text-xs text-slate-400 mt-0.5">${JG.esc(preset.description || preset.id || '')}</div>
                    <div class="flex items-center gap-1.5 mt-2">${admin}${invite}${temp}</div>
                </td>
                <td data-label="${JG.esc(t('tableAccess', 'Acces'))}" class="px-5 py-4"><span class="badge badge-muted">${JG.esc(rights)}</span></td>
                <td data-label="${JG.esc(t('tableLibraries', 'Bibliotheques'))}" class="px-5 py-4"><span class="badge badge-info">${JG.esc(libs)}</span></td>
                <td data-label="${JG.esc(t('tableInvites', 'Invitations'))}" class="px-5 py-4">${inviteLabel}</td>
                <td data-label="${JG.esc(t('tableActions', 'Actions'))}" class="px-5 py-4 text-right">
                    <button type="button" class="jg-btn-icon profile-row-edit" data-profile-index="${index}" title="${JG.esc(t('edit', 'Modifier'))}">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
                    </button>
                </td>
            </tr>`;
        }).join('');
    }

    function renderTargetOptions() {
        if (!els['profile-target-preset']) return;
        els['profile-target-preset'].innerHTML = `<option value="">${JG.esc(t('none', 'Aucun'))}</option>` + state.presets.map((preset) => {
            return `<option value="${JG.esc(preset.id || '')}">${JG.esc(presetName(preset))}</option>`;
        }).join('');
    }

    function renderLibraryPicker(preset) {
        if (!els['profile-libraries']) return;
        if (!state.libraries.length) {
            els['profile-libraries'].innerHTML = `<div class="p-4 text-sm text-jg-text-muted">${JG.esc(t('noLibraries', 'Aucune bibliotheque Jellyfin chargee.'))}</div>`;
            return;
        }

        const normalized = normalizePresetSettings(preset);
        const userConfig = normalized.user_configuration || defaultUserConfiguration();
        const selected = new Set((normalized.enabled_folder_ids || []).map(String));
        const grouped = new Set((userConfig.grouped_folders || []).map(String));
        const myMediaExcludes = new Set((userConfig.my_media_excludes || []).map(String));
        const latestExcludes = new Set((userConfig.latest_items_excludes || []).map(String));
        const libraryID = (library) => String(library.id || library.Id || library.ItemId || '').trim();
        const libraryByID = new Map(state.libraries.map((library) => [libraryID(library), library]).filter(([id]) => id));
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
        state.libraries.forEach((library) => {
            const id = libraryID(library);
            if (id && !seenOrdered.has(id)) {
                orderedLibraries.push(library);
                seenOrdered.add(id);
            }
        });

        const rows = orderedLibraries.map((library) => {
            const id = libraryID(library);
            const label = library.name || library.Name || id;
            const type = library.collection_type || library.CollectionType || '';
            return `<tr data-library-id="${JG.esc(id)}" class="border-t border-white/5">
                <td class="px-3 py-2 min-w-[170px]">
                    <div class="text-sm font-semibold text-jg-text">${JG.esc(label)}</div>
                    <div class="text-[10px] uppercase tracking-widest text-jg-text-muted">${JG.esc(type || id)}</div>
                </td>
                <td class="px-3 py-2 text-center"><input type="checkbox" class="profile-library-access form-checkbox w-4 h-4 rounded border-jg-border bg-black/50 accent-jg-accent" ${normalized.enable_all_folders || selected.has(id) ? 'checked' : ''}></td>
                <td class="px-3 py-2 text-center"><input type="checkbox" class="profile-library-my-media form-checkbox w-4 h-4 rounded border-jg-border bg-black/50 accent-jg-accent" title="${JG.esc(t('libraryHelpMyMedia', 'Show in My media'))}" ${!myMediaExcludes.has(id) ? 'checked' : ''}></td>
                <td class="px-3 py-2 text-center"><input type="checkbox" class="profile-library-latest form-checkbox w-4 h-4 rounded border-jg-border bg-black/50 accent-jg-accent" title="${JG.esc(t('libraryHelpLatest', 'Show in Recently added'))}" ${!latestExcludes.has(id) ? 'checked' : ''}></td>
                <td class="px-3 py-2 text-center"><input type="checkbox" class="profile-library-group form-checkbox w-4 h-4 rounded border-jg-border bg-black/50 accent-jg-accent" title="${JG.esc(t('libraryHelpGroup', 'Group by type'))}" ${grouped.has(id) ? 'checked' : ''}></td>
                <td class="px-3 py-2">
                    <div class="flex items-center gap-1">
                        <button type="button" class="profile-library-move jg-btn jg-btn-sm jg-btn-ghost h-8 w-8 px-0" data-direction="-1" title="${JG.esc(t('libraryMoveUp', 'Move up'))}" aria-label="${JG.esc(t('libraryMoveUp', 'Move up'))}">&uarr;</button>
                        <button type="button" class="profile-library-move jg-btn jg-btn-sm jg-btn-ghost h-8 w-8 px-0" data-direction="1" title="${JG.esc(t('libraryMoveDown', 'Move down'))}" aria-label="${JG.esc(t('libraryMoveDown', 'Move down'))}">&darr;</button>
                    </div>
                </td>
            </tr>`;
        }).join('');

        els['profile-libraries'].innerHTML = `<table class="w-full text-left text-sm">
            <thead class="text-[10px] uppercase tracking-widest text-jg-text-muted bg-white/[0.03]">
                <tr>
                    <th class="px-3 py-3"></th>
                    <th class="px-3 py-3 text-center">${JG.esc(t('libraryColAccess', 'Access'))}</th>
                    <th class="px-3 py-3 text-center" title="${JG.esc(t('libraryHelpMyMedia', 'Show in My media'))}">${JG.esc(t('libraryColMyMedia', 'Home'))}</th>
                    <th class="px-3 py-3 text-center" title="${JG.esc(t('libraryHelpLatest', 'Show in Recently added'))}">${JG.esc(t('libraryColLatest', 'Latest'))}</th>
                    <th class="px-3 py-3 text-center" title="${JG.esc(t('libraryHelpGroup', 'Group by type'))}">${JG.esc(t('libraryColGroup', 'Group'))}</th>
                    <th class="px-3 py-3">${JG.esc(t('libraryColOrder', 'Order'))}</th>
                </tr>
            </thead>
            <tbody>${rows}</tbody>
        </table>`;
        updateLibraryAccessState();
    }

    function updateLibraryAccessState() {
        const allFolders = !!els['profile-all-folders']?.checked;
        document.querySelectorAll('#profile-libraries tr[data-library-id]').forEach((row) => {
            const accessInput = row.querySelector('.profile-library-access');
            const rowHasAccess = allFolders || !!accessInput?.checked;
            if (accessInput) accessInput.disabled = allFolders;
            row.classList.toggle('opacity-60', !rowHasAccess);
            row.querySelectorAll('.profile-library-my-media, .profile-library-latest, .profile-library-group, .profile-library-move').forEach((input) => {
                input.disabled = !rowHasAccess;
            });
        });
        updateLibraryOrderControls();
    }

    function updateLibraryOrderControls() {
        const rows = Array.from(document.querySelectorAll('#profile-libraries tr[data-library-id]'));
        rows.forEach((row, index) => {
            const rowHasAccess = !row.classList.contains('opacity-60');
            const up = row.querySelector('.profile-library-move[data-direction="-1"]');
            const down = row.querySelector('.profile-library-move[data-direction="1"]');
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

    function currentHomeSectionValues() {
        return Array.from({ length: 10 }, (_, index) => qs(`profile-home-section-${index}`)?.value || 'none');
    }

    function updateHomeSectionControls() {
        document.querySelectorAll('#profile-home-sections [data-home-section-row]').forEach((row, index) => {
            const up = row.querySelector('[data-home-section-direction="-1"]');
            const down = row.querySelector('[data-home-section-direction="1"]');
            if (up) up.disabled = index === 0;
            if (down) down.disabled = index === 9;
        });
    }

    function renderHomeSections(homeSections) {
        const container = els['profile-home-sections'];
        if (!container) return;
        const sections = Array.isArray(homeSections) ? homeSections.slice(0, 10) : DEFAULT_HOME_SECTIONS.slice();
        while (sections.length < 10) sections.push('none');
        container.innerHTML = sections.map((selectedValue, index) => {
            const options = HOME_SECTION_OPTIONS.map(([optionValue, labelKey]) => (
                `<option value="${JG.esc(optionValue)}" ${optionValue === selectedValue ? 'selected' : ''}>${JG.esc(t(labelKey, optionValue))}</option>`
            )).join('');
            return `<div data-home-section-row class="flex items-center gap-2 rounded-lg border border-white/10 bg-black/10 p-2">
                <label class="jg-label mb-0 min-w-[4.5rem]" for="profile-home-section-${index}">${JG.esc(t('homeSectionLabel', 'Section'))} ${index + 1}</label>
                <select id="profile-home-section-${index}" class="jg-input jg-select-premium h-10 bg-black/20 text-sm flex-1">${options}</select>
                <button type="button" class="jg-btn jg-btn-sm jg-btn-ghost h-8 w-8 px-0" data-home-section-direction="-1" title="${JG.esc(t('libraryMoveUp', 'Move up'))}" aria-label="${JG.esc(t('libraryMoveUp', 'Move up'))}">&uarr;</button>
                <button type="button" class="jg-btn jg-btn-sm jg-btn-ghost h-8 w-8 px-0" data-home-section-direction="1" title="${JG.esc(t('libraryMoveDown', 'Move down'))}" aria-label="${JG.esc(t('libraryMoveDown', 'Move down'))}">&darr;</button>
            </div>`;
        }).join('');
        updateHomeSectionControls();
    }

    function moveHomeSection(button) {
        if (!button || button.disabled) return;
        const direction = Number.parseInt(button.dataset.homeSectionDirection || '0', 10) || 0;
        const row = button.closest('[data-home-section-row]');
        const rows = Array.from(document.querySelectorAll('#profile-home-sections [data-home-section-row]'));
        const index = rows.indexOf(row);
        const target = index + direction;
        if (index < 0 || target < 0 || target >= 10) return;
        const values = currentHomeSectionValues();
        [values[index], values[target]] = [values[target], values[index]];
        renderHomeSections(values);
    }

    function fillForm() {
        if (!state.presets[state.selected]) return;
        const preset = normalizePresetSettings(state.presets[state.selected]);
        state.presets[state.selected] = preset;
        state.hydrating = true;
        renderTargetOptions();
        els['profile-index'].value = String(state.selected);
        els['profile-id'].value = preset.id || '';
        els['profile-name'].value = preset.name || '';
        els['profile-description'].value = preset.description || '';
        els['profile-admin'].checked = !!preset.is_administrator;
        els['profile-hidden'].checked = !!preset.is_hidden;
        els['profile-disabled'].checked = !!preset.is_disabled;
        els['profile-all-folders'].checked = !!preset.enable_all_folders;
        els['profile-download'].checked = !!preset.enable_download;
        els['profile-remote'].checked = preset.enable_remote_access !== false;
        els['profile-playback'].checked = preset.enable_media_playback !== false;
        els['profile-audio-transcode'].checked = preset.enable_audio_playback_transcoding !== false;
        els['profile-video-transcode'].checked = preset.enable_video_playback_transcoding !== false;
        els['profile-remux'].checked = preset.enable_playback_remuxing !== false;
        els['profile-live-tv'].checked = !!preset.enable_live_tv_access;
        els['profile-live-tv-management'].checked = !!preset.enable_live_tv_management;
        els['profile-public-sharing'].checked = !!preset.enable_public_sharing;
        els['profile-content-deletion'].checked = !!preset.enable_content_deletion;
        els['profile-sync-transcoding'].checked = !!preset.enable_sync_transcoding;
        els['profile-syncplay'].value = preset.syncplay_access || '';
        els['profile-disable-days'].value = String(preset.disable_after_days || 0);
        els['profile-delete-days'].value = String(preset.delete_after_days || 0);
        els['profile-sessions'].value = String(preset.max_sessions || 0);
        els['profile-bitrate'].value = String(preset.bitrate_limit || 0);
        els['profile-parental-rating'].value = String(preset.max_parental_rating || 0);
        els['profile-invalid-login'].value = String(preset.invalid_login_attempt_count || 0);
        els['profile-login-lockout'].value = String(preset.login_attempts_before_lockout || 0);
        els['profile-can-invite'].checked = !!(preset.can_invite || preset.can_create_invitations);
        els['profile-can-temp-invite'].checked = !!preset.can_create_temporary_invitations;
        els['profile-target-preset'].value = preset.target_preset_id || '';
        els['profile-quota-day'].value = String(preset.invite_quota_day || 0);
        els['profile-quota-month'].value = String(preset.invite_quota_month || preset.invite_quota || 0);
        els['profile-link-days'].value = String(preset.invite_link_validity_days || 0);
        els['profile-max-uses'].value = String(preset.invite_max_uses || 0);
        els['profile-temp-default'].value = String(preset.default_temporary_duration_days || 0);
        els['profile-temp-max'].value = String(preset.max_temporary_duration_days || 0);
        els['profile-is-temporary'].checked = !!preset.is_temporary;
        els['profile-account-default'].value = String(preset.default_account_duration_days || 0);
        els['profile-account-max'].value = String(preset.max_account_duration_days || 0);
        els['profile-editor-title'].textContent = presetName(preset);
        els['profile-editor-subtitle'].textContent = preset.id ? `ID ${preset.id}` : t('newProfileSubtitle', 'Nouveau profil');
        setList('profile-blocked-folders', preset.blocked_media_folders);
        setList('profile-deletion-folders', preset.enable_content_deletion_from_folders);
        setList('profile-allowed-tags', preset.allowed_tags);
        setList('profile-blocked-tags', [...(preset.blocked_tags || []), ...(preset.block_unrated_items || [])]);
        renderHomeSections(preset.display_preferences?.home_sections);
        els['profile-hide-played-latest'].checked = !!preset.user_configuration?.hide_played_in_latest;
        els['profile-display-missing-episodes'].checked = !!preset.user_configuration?.display_missing_episodes;
        els['profile-backdrops'].checked = !!preset.display_preferences?.enable_backdrops;
        els['profile-theme-songs'].checked = !!preset.display_preferences?.enable_theme_songs;
        els['profile-theme-videos'].checked = !!preset.display_preferences?.enable_theme_videos;
        els['profile-page-size'].value = String(preset.display_preferences?.library_page_size || 0);
        setList('profile-allowed-targets', preset.allowed_target_preset_ids);
        setList('profile-allowed-temp-targets', preset.allowed_temporary_preset_ids);
        setList('profile-devices', preset.enabled_devices);
        setList('profile-channels', [...(preset.enabled_channels || []), ...(preset.blocked_channels || []).map((v) => `!${v}`)]);
        setList('profile-authentik-groups', preset.authentik_groups || preset.ldap_groups || []);
        if (els['profile-ldap-groups']) setList('profile-ldap-groups', preset.authentik_groups || preset.ldap_groups || []);
        renderLibraryPicker(preset);
        renderList();
        renderTabs();
        updateLibraryAccessState();
        state.hydrating = false;
    }

    function collectForm() {
        const index = parseInt(els['profile-index'].value || '0', 10) || 0;
        const current = state.presets[index] || {};
        const libraryRows = Array.from(els['profile-libraries']?.querySelectorAll('tr[data-library-id]') || []);
        const enableAllFolders = !!els['profile-all-folders'].checked;
        const activeLibraryRows = enableAllFolders
            ? libraryRows
            : libraryRows.filter((row) => row.querySelector('.profile-library-access')?.checked);
        const enabledFolderIDs = enableAllFolders
            ? []
            : activeLibraryRows.map((row) => row.dataset.libraryId).filter(Boolean);
        const userConfiguration = { ...(current.user_configuration || {}) };
        if (libraryRows.length) {
            userConfiguration.ordered_views = activeLibraryRows.map((row) => row.dataset.libraryId).filter(Boolean);
            userConfiguration.grouped_folders = activeLibraryRows
                .filter((row) => row.querySelector('.profile-library-group')?.checked)
                .map((row) => row.dataset.libraryId)
                .filter(Boolean);
            userConfiguration.my_media_excludes = activeLibraryRows
                .filter((row) => !row.querySelector('.profile-library-my-media')?.checked)
                .map((row) => row.dataset.libraryId)
                .filter(Boolean);
            userConfiguration.latest_items_excludes = activeLibraryRows
                .filter((row) => !row.querySelector('.profile-library-latest')?.checked)
                .map((row) => row.dataset.libraryId)
                .filter(Boolean);
        }
        userConfiguration.display_missing_episodes = !!els['profile-display-missing-episodes']?.checked;
        userConfiguration.hide_played_in_latest = !!els['profile-hide-played-latest']?.checked;
        const homeSections = currentHomeSectionValues();
        const id = (els['profile-id'].value || '').trim().toLowerCase().replace(/[^a-z0-9_-]+/g, '-').replace(/^-+|-+$/g, '');
        const channels = list('profile-channels');
        const blockedChannels = channels.filter((v) => v.startsWith('!')).map((v) => v.slice(1)).filter(Boolean);
        const enabledChannels = channels.filter((v) => !v.startsWith('!'));
        return {
            ...current,
            id,
            name: (els['profile-name'].value || '').trim() || id || t('profileFallback', 'Profil'),
            description: (els['profile-description'].value || '').trim(),
            is_administrator: !!els['profile-admin'].checked,
            is_hidden: !!els['profile-hidden'].checked,
            is_disabled: !!els['profile-disabled'].checked,
            enable_all_folders: enableAllFolders,
            enabled_folder_ids: libraryRows.length ? enabledFolderIDs : (current.enabled_folder_ids || []),
            blocked_media_folders: list('profile-blocked-folders'),
            enable_all_devices: list('profile-devices').length === 0,
            enabled_devices: list('profile-devices'),
            enable_all_channels: enabledChannels.length === 0,
            enabled_channels: enabledChannels,
            blocked_channels: blockedChannels,
            enable_download: !!els['profile-download'].checked,
            enable_media_playback: !!els['profile-playback'].checked,
            enable_audio_playback_transcoding: !!els['profile-audio-transcode'].checked,
            enable_video_playback_transcoding: !!els['profile-video-transcode'].checked,
            enable_playback_remuxing: !!els['profile-remux'].checked,
            enable_remote_access: !!els['profile-remote'].checked,
            enable_live_tv_access: !!els['profile-live-tv'].checked,
            enable_live_tv_management: !!els['profile-live-tv-management'].checked,
            enable_public_sharing: !!els['profile-public-sharing'].checked,
            enable_content_deletion: !!els['profile-content-deletion'].checked,
            enable_content_deletion_from_folders: list('profile-deletion-folders'),
            enable_sync_transcoding: !!els['profile-sync-transcoding'].checked,
            syncplay_access: (els['profile-syncplay'].value || '').trim(),
            invalid_login_attempt_count: num('profile-invalid-login'),
            login_attempts_before_lockout: num('profile-login-lockout'),
            max_sessions: num('profile-sessions'),
            bitrate_limit: num('profile-bitrate'),
            disable_after_days: num('profile-disable-days'),
            delete_after_days: num('profile-delete-days'),
            max_parental_rating: num('profile-parental-rating'),
            allowed_tags: list('profile-allowed-tags'),
            blocked_tags: list('profile-blocked-tags'),
            user_configuration: userConfiguration,
            display_preferences: {
                ...(current.display_preferences || {}),
                home_sections: homeSections.length ? homeSections : (current.display_preferences?.home_sections || DEFAULT_HOME_SECTIONS.slice()),
                enable_backdrops: !!els['profile-backdrops'].checked,
                enable_theme_songs: !!els['profile-theme-songs'].checked,
                enable_theme_videos: !!els['profile-theme-videos'].checked,
                library_page_size: nonNegativeInt(els['profile-page-size']?.value, 0),
            },
            can_invite: !!els['profile-can-invite'].checked,
            can_create_invitations: !!els['profile-can-invite'].checked,
            target_preset_id: (els['profile-target-preset'].value || '').trim(),
            allowed_target_preset_ids: list('profile-allowed-targets'),
            invite_quota_day: num('profile-quota-day'),
            invite_quota_month: num('profile-quota-month'),
            invite_link_validity_days: num('profile-link-days'),
            invite_max_uses: num('profile-max-uses'),
            can_create_temporary_invitations: !!els['profile-can-temp-invite'].checked,
            allowed_temporary_preset_ids: list('profile-allowed-temp-targets'),
            default_temporary_duration_days: num('profile-temp-default'),
            max_temporary_duration_days: num('profile-temp-max'),
            is_temporary: !!els['profile-is-temporary'].checked,
            default_account_duration_days: num('profile-account-default'),
            max_account_duration_days: num('profile-account-max'),
            authentik_groups: els['profile-authentik-groups'] ? list('profile-authentik-groups') : (els['profile-ldap-groups'] ? list('profile-ldap-groups') : (current.authentik_groups || current.ldap_groups || [])),
            ldap_groups: els['profile-authentik-groups'] ? list('profile-authentik-groups') : (els['profile-ldap-groups'] ? list('profile-ldap-groups') : (current.authentik_groups || current.ldap_groups || [])),
        };
    }

    function openDrawer() {
        els['profile-drawer']?.classList.add('open');
        els['profile-drawer-overlay']?.classList.add('open');
        els['profile-drawer']?.setAttribute('aria-hidden', 'false');
    }

    function closeDrawer() {
        els['profile-drawer']?.classList.remove('open');
        els['profile-drawer-overlay']?.classList.remove('open');
        els['profile-drawer']?.setAttribute('aria-hidden', 'true');
    }

    async function load(preferredID = '') {
        const [presetsRes, librariesRes] = await Promise.all([
            JG.api('/admin/api/automation/presets'),
            JG.api('/admin/api/automation/libraries'),
        ]);
        if (!presetsRes?.success) {
            JG.toast(presetsRes?.message || t('loadError', 'Impossible de charger les profils'), 'error');
            return;
        }
        state.presets = Array.isArray(presetsRes.data) ? presetsRes.data : [];
        state.libraries = Array.isArray(librariesRes?.data) ? librariesRes.data : [];
        const preferredIndex = preferredID
            ? state.presets.findIndex((preset) => String(preset.id || '').trim().toLowerCase() === String(preferredID).trim().toLowerCase())
            : -1;
        state.selected = preferredIndex >= 0 ? preferredIndex : Math.min(state.selected, Math.max(0, state.presets.length - 1));
        renderList();
        fillForm();
        setDirty(false);
    }

    async function saveAll() {
        const current = collectForm();
        if (!current.id) {
            JG.toast(t('idRequired', 'Identifiant de profil requis'), 'error');
            return;
        }
        state.presets[state.selected] = current;
        const res = await JG.api('/admin/api/automation/presets', {
            method: 'POST',
            body: JSON.stringify(state.presets),
        });
        if (!res?.success) {
            JG.toast(res?.message || t('saveError', 'Sauvegarde impossible'), 'error');
            return;
        }
        JG.toast(res.message || t('saved', 'Profils sauvegardes'), 'success');
        await load(current.id);
        setDirty(false);
    }

    function addProfile() {
        state.presets.push({
            id: `profil-${state.presets.length + 1}`,
            name: t('newProfile', 'Nouveau profil'),
            enable_all_folders: true,
            enable_all_devices: true,
            enable_all_channels: true,
            enable_download: false,
            enable_remote_access: true,
            enable_media_playback: true,
            enable_audio_playback_transcoding: true,
            enable_video_playback_transcoding: true,
            enable_playback_remuxing: true,
            max_sessions: 0,
            bitrate_limit: 0,
            can_invite: false,
            can_create_invitations: false,
            user_configuration: defaultUserConfiguration(),
            display_preferences: defaultDisplayPreferences(),
        });
        state.selected = state.presets.length - 1;
        fillForm();
        openDrawer();
        setDirty(true);
    }

    function deleteProfile() {
        if (state.presets.length <= 1) {
            JG.toast(t('keepOne', 'Gardez au moins un profil'), 'error');
            return;
        }
        state.presets.splice(state.selected, 1);
        state.selected = Math.max(0, state.selected - 1);
        fillForm();
        setDirty(true);
    }

    function activateTab(button, focusActive = false) {
        if (!button) return;
        state.presets[state.selected] = collectForm();
        state.activeTab = button.dataset.profileTabTarget || 'rights';
        renderTabs(focusActive);
    }

    document.addEventListener('DOMContentLoaded', () => {
        hydrateEls();
        load();

        els['profiles-list']?.addEventListener('click', (event) => {
            const target = event.target.closest('[data-profile-index]');
            if (!target) return;
            state.presets[state.selected] = collectForm();
            state.selected = parseInt(target.dataset.profileIndex || '0', 10) || 0;
            fillForm();
            openDrawer();
        });

        els['profile-tabs']?.addEventListener('click', (event) => {
            const button = event.target.closest('[data-profile-tab-target]');
            activateTab(button);
        });

        els['profile-tabs']?.addEventListener('keydown', (event) => {
            const keys = ['ArrowLeft', 'ArrowRight', 'Home', 'End'];
            if (!keys.includes(event.key)) return;
            const buttons = Array.from(document.querySelectorAll('[data-profile-tab-target]'));
            const current = buttons.findIndex((button) => button.dataset.profileTabTarget === state.activeTab);
            if (current < 0) return;
            event.preventDefault();
            let next = current;
            if (event.key === 'ArrowLeft') next = current <= 0 ? buttons.length - 1 : current - 1;
            if (event.key === 'ArrowRight') next = current >= buttons.length - 1 ? 0 : current + 1;
            if (event.key === 'Home') next = 0;
            if (event.key === 'End') next = buttons.length - 1;
            activateTab(buttons[next], true);
        });

        els['profile-form']?.addEventListener('input', () => setDirty(true));
        els['profile-form']?.addEventListener('change', () => setDirty(true));
        els['profile-form']?.addEventListener('submit', (event) => {
            event.preventDefault();
            saveAll();
        });
        els['profiles-save-btn']?.addEventListener('click', saveAll);
        els['profile-new-btn']?.addEventListener('click', addProfile);
        els['profile-drawer-close']?.addEventListener('click', closeDrawer);
        els['profile-drawer-overlay']?.addEventListener('click', closeDrawer);
        els['profile-search']?.addEventListener('input', () => {
            state.search = els['profile-search'].value || '';
            renderList();
        });
        els['profile-filter-kind']?.addEventListener('change', () => {
            state.filter = els['profile-filter-kind'].value || 'all';
            renderList();
        });
        els['profile-delete-btn']?.addEventListener('click', deleteProfile);
        els['profile-all-folders']?.addEventListener('change', () => {
            updateLibraryAccessState();
        });
        els['profile-libraries']?.addEventListener('click', (event) => {
            const button = event.target.closest('.profile-library-move');
            if (!button) return;
            moveLibraryRow(button);
            setDirty(true);
        });
        els['profile-libraries']?.addEventListener('change', () => {
            updateLibraryAccessState();
            setDirty(true);
        });
        els['profile-home-sections']?.addEventListener('click', (event) => {
            const button = event.target.closest('[data-home-section-direction]');
            if (!button) return;
            moveHomeSection(button);
            setDirty(true);
        });
        els['profile-home-sections']?.addEventListener('change', () => {
            updateHomeSectionControls();
            setDirty(true);
        });
    });
})();
