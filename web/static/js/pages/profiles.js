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

    function hydrateEls() {
        [
            'profiles-list', 'profiles-count', 'profiles-save-btn', 'profile-new-btn', 'profile-form',
            'profile-index', 'profile-id', 'profile-name', 'profile-description', 'profile-admin',
            'profile-hidden', 'profile-disabled', 'profile-all-folders', 'profile-download', 'profile-remote',
            'profile-playback', 'profile-audio-transcode', 'profile-video-transcode', 'profile-remux',
            'profile-live-tv', 'profile-live-tv-management', 'profile-public-sharing', 'profile-content-deletion',
            'profile-sync-transcoding', 'profile-syncplay', 'profile-libraries', 'profile-blocked-folders',
            'profile-deletion-folders', 'profile-disable-days', 'profile-delete-days', 'profile-sessions',
            'profile-bitrate', 'profile-parental-rating', 'profile-invalid-login', 'profile-login-lockout',
            'profile-allowed-tags', 'profile-blocked-tags', 'profile-home-sections', 'profile-ordered-views',
            'profile-grouped-folders', 'profile-home-excludes', 'profile-backdrops', 'profile-theme-songs',
            'profile-theme-videos', 'profile-page-size', 'profile-can-invite', 'profile-can-temp-invite',
            'profile-target-preset', 'profile-quota-day', 'profile-quota-month', 'profile-link-days',
            'profile-max-uses', 'profile-temp-default', 'profile-temp-max', 'profile-allowed-targets',
            'profile-allowed-temp-targets', 'profile-is-temporary', 'profile-account-default',
            'profile-account-max', 'profile-ldap-groups', 'profile-devices', 'profile-channels',
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

    function renderList() {
        if (!els['profiles-list']) return;
        els['profiles-count'].textContent = String(state.presets.length);
        els['profiles-list'].innerHTML = state.presets.map((preset, index) => {
            const active = index === state.selected ? 'active' : '';
            const libs = preset.enable_all_folders
                ? t('allLibraries', 'Toutes bibliotheques')
                : t('libraryCount', '{count} bibliotheque(s)').replace('{count}', String((preset.enabled_folder_ids || []).length));
            const admin = preset.is_administrator ? `<span class="jg-ds-tag danger">${JG.esc(t('tagAdmin', 'Admin'))}</span>` : '';
            const invite = (preset.can_invite || preset.can_create_invitations) ? `<span class="jg-ds-tag">${JG.esc(t('tagSponsor', 'Parrain'))}</span>` : '';
            const temp = preset.is_temporary ? `<span class="jg-ds-tag">${JG.esc(t('tagTemporary', 'Temp'))}</span>` : '';
            return `<button type="button" class="jg-profile-card ${active}" data-index="${index}" aria-current="${index === state.selected ? 'true' : 'false'}">
                <span>
                    <strong>${JG.esc(presetName(preset))}</strong>
                    <small>${JG.esc(preset.description || libs)}</small>
                </span>
                <span class="jg-profile-badges">${admin}${invite}${temp}</span>
            </button>`;
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
            els['profile-libraries'].innerHTML = `<div class="text-sm text-jg-text-muted">${JG.esc(t('noLibraries', 'Aucune bibliotheque Jellyfin chargee.'))}</div>`;
            return;
        }
        const selected = new Set(preset?.enabled_folder_ids || []);
        els['profile-libraries'].innerHTML = state.libraries.map((library) => {
            const id = library.id || library.Id || library.ItemId || '';
            const checked = selected.has(id) ? 'checked' : '';
            return `<label class="jg-library-option">
                <input type="checkbox" value="${JG.esc(id)}" ${checked}>
                <span>${JG.esc(library.name || library.Name || id)}</span>
            </label>`;
        }).join('');
    }

    function fillForm() {
        const preset = state.presets[state.selected];
        if (!preset) return;
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
        setList('profile-home-sections', preset.display_preferences?.home_sections);
        setList('profile-ordered-views', preset.user_configuration?.ordered_views);
        setList('profile-grouped-folders', preset.user_configuration?.grouped_folders);
        setList('profile-home-excludes', [
            ...(preset.user_configuration?.my_media_excludes || []),
            ...(preset.user_configuration?.latest_items_excludes || []),
        ]);
        els['profile-backdrops'].checked = !!preset.display_preferences?.enable_backdrops;
        els['profile-theme-songs'].checked = !!preset.display_preferences?.enable_theme_songs;
        els['profile-theme-videos'].checked = !!preset.display_preferences?.enable_theme_videos;
        els['profile-page-size'].value = String(preset.display_preferences?.library_page_size || 0);
        setList('profile-allowed-targets', preset.allowed_target_preset_ids);
        setList('profile-allowed-temp-targets', preset.allowed_temporary_preset_ids);
        setList('profile-ldap-groups', preset.ldap_groups);
        setList('profile-devices', preset.enabled_devices);
        setList('profile-channels', [...(preset.enabled_channels || []), ...(preset.blocked_channels || []).map((v) => `!${v}`)]);
        renderLibraryPicker(preset);
        renderList();
        renderTabs();
        els['profile-libraries']?.classList.toggle('opacity-50', !!els['profile-all-folders'].checked);
        state.hydrating = false;
    }

    function collectForm() {
        const index = parseInt(els['profile-index'].value || '0', 10) || 0;
        const current = state.presets[index] || {};
        const enabledFolderIDs = Array.from(els['profile-libraries'].querySelectorAll('input:checked')).map((input) => input.value).filter(Boolean);
        const id = (els['profile-id'].value || '').trim().toLowerCase().replace(/[^a-z0-9_-]+/g, '-').replace(/^-+|-+$/g, '');
        const channels = list('profile-channels');
        const blockedChannels = channels.filter((v) => v.startsWith('!')).map((v) => v.slice(1)).filter(Boolean);
        const enabledChannels = channels.filter((v) => !v.startsWith('!'));
        const homeExcludes = list('profile-home-excludes');
        return {
            ...current,
            id,
            name: (els['profile-name'].value || '').trim() || id || t('profileFallback', 'Profil'),
            description: (els['profile-description'].value || '').trim(),
            is_administrator: !!els['profile-admin'].checked,
            is_hidden: !!els['profile-hidden'].checked,
            is_disabled: !!els['profile-disabled'].checked,
            enable_all_folders: !!els['profile-all-folders'].checked,
            enabled_folder_ids: enabledFolderIDs,
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
            user_configuration: {
                ...(current.user_configuration || {}),
                ordered_views: list('profile-ordered-views'),
                grouped_folders: list('profile-grouped-folders'),
                my_media_excludes: homeExcludes,
                latest_items_excludes: homeExcludes,
            },
            display_preferences: {
                ...(current.display_preferences || {}),
                home_sections: list('profile-home-sections'),
                enable_backdrops: !!els['profile-backdrops'].checked,
                enable_theme_songs: !!els['profile-theme-songs'].checked,
                enable_theme_videos: !!els['profile-theme-videos'].checked,
                library_page_size: num('profile-page-size'),
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
            ldap_groups: list('profile-ldap-groups'),
        };
    }

    async function load() {
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
        state.selected = 0;
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
        await load();
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
        });
        state.selected = state.presets.length - 1;
        fillForm();
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
            const card = event.target.closest('.jg-profile-card');
            if (!card) return;
            state.presets[state.selected] = collectForm();
            state.selected = parseInt(card.dataset.index || '0', 10) || 0;
            fillForm();
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
            state.presets[state.selected] = collectForm();
            renderList();
            setDirty(true);
            JG.toast(t('draftUpdated', 'Profil mis a jour dans le brouillon'), 'success');
        });
        els['profiles-save-btn']?.addEventListener('click', saveAll);
        els['profile-new-btn']?.addEventListener('click', addProfile);
        els['profile-delete-btn']?.addEventListener('click', deleteProfile);
        els['profile-all-folders']?.addEventListener('change', () => {
            els['profile-libraries']?.classList.toggle('opacity-50', !!els['profile-all-folders'].checked);
        });
    });
})();
