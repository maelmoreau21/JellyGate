(() => {
    const state = {
        presets: [],
        libraries: [],
        selected: 0,
    };

    const els = {};
    const qs = (id) => document.getElementById(id);
    const num = (id) => Math.max(0, parseInt(qs(id)?.value || '0', 10) || 0);

    function hydrateEls() {
        [
            'profiles-list', 'profiles-count', 'profiles-save-btn', 'profile-new-btn', 'profile-form',
            'profile-index', 'profile-id', 'profile-name', 'profile-description', 'profile-admin',
            'profile-all-folders', 'profile-download', 'profile-remote', 'profile-libraries',
            'profile-disable-days', 'profile-sessions', 'profile-bitrate', 'profile-can-invite',
            'profile-target-preset', 'profile-quota-day', 'profile-quota-month', 'profile-link-days',
            'profile-max-uses', 'profile-delete-btn', 'profile-editor-title', 'profile-editor-subtitle'
        ].forEach((id) => { els[id] = qs(id); });
    }

    function presetName(preset) {
        return preset?.name || preset?.id || 'Profil';
    }

    function renderList() {
        if (!els['profiles-list']) return;
        els['profiles-count'].textContent = String(state.presets.length);
        els['profiles-list'].innerHTML = state.presets.map((preset, index) => {
            const active = index === state.selected ? 'active' : '';
            const libs = preset.enable_all_folders ? 'Toutes bibliothèques' : `${(preset.enabled_folder_ids || []).length} bibliothèque(s)`;
            const admin = preset.is_administrator ? '<span class="jg-ds-tag danger">Admin</span>' : '';
            const invite = preset.can_invite ? '<span class="jg-ds-tag">Parrain</span>' : '';
            return `<button type="button" class="jg-profile-card ${active}" data-index="${index}">
                <span>
                    <strong>${JG.esc(presetName(preset))}</strong>
                    <small>${JG.esc(preset.description || libs)}</small>
                </span>
                <span class="jg-profile-badges">${admin}${invite}</span>
            </button>`;
        }).join('');
    }

    function renderTargetOptions() {
        if (!els['profile-target-preset']) return;
        els['profile-target-preset'].innerHTML = '<option value="">Aucun</option>' + state.presets.map((preset) => {
            return `<option value="${JG.esc(preset.id || '')}">${JG.esc(presetName(preset))}</option>`;
        }).join('');
    }

    function renderLibraryPicker(preset) {
        if (!els['profile-libraries']) return;
        if (!state.libraries.length) {
            els['profile-libraries'].innerHTML = '<div class="text-sm text-jg-text-muted">Aucune bibliothèque Jellyfin chargée.</div>';
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
        renderTargetOptions();
        els['profile-index'].value = String(state.selected);
        els['profile-id'].value = preset.id || '';
        els['profile-name'].value = preset.name || '';
        els['profile-description'].value = preset.description || '';
        els['profile-admin'].checked = !!preset.is_administrator;
        els['profile-all-folders'].checked = !!preset.enable_all_folders;
        els['profile-download'].checked = !!preset.enable_download;
        els['profile-remote'].checked = preset.enable_remote_access !== false;
        els['profile-disable-days'].value = String(preset.disable_after_days || 0);
        els['profile-sessions'].value = String(preset.max_sessions || 0);
        els['profile-bitrate'].value = String(preset.bitrate_limit || 0);
        els['profile-can-invite'].checked = !!preset.can_invite;
        els['profile-target-preset'].value = preset.target_preset_id || '';
        els['profile-quota-day'].value = String(preset.invite_quota_day || 0);
        els['profile-quota-month'].value = String(preset.invite_quota_month || 0);
        els['profile-link-days'].value = String(preset.invite_link_validity_days || 0);
        els['profile-max-uses'].value = String(preset.invite_max_uses || 0);
        els['profile-editor-title'].textContent = presetName(preset);
        els['profile-editor-subtitle'].textContent = preset.id ? `ID ${preset.id}` : 'Nouveau profil';
        renderLibraryPicker(preset);
        renderList();
    }

    function collectForm() {
        const index = parseInt(els['profile-index'].value || '0', 10) || 0;
        const current = state.presets[index] || {};
        const enabledFolderIDs = Array.from(els['profile-libraries'].querySelectorAll('input:checked')).map((input) => input.value).filter(Boolean);
        const id = (els['profile-id'].value || '').trim().toLowerCase().replace(/[^a-z0-9_-]+/g, '-').replace(/^-+|-+$/g, '');
        return {
            ...current,
            id,
            name: (els['profile-name'].value || '').trim() || id || 'Profil',
            description: (els['profile-description'].value || '').trim(),
            is_administrator: !!els['profile-admin'].checked,
            enable_all_folders: !!els['profile-all-folders'].checked,
            enabled_folder_ids: enabledFolderIDs,
            enable_download: !!els['profile-download'].checked,
            enable_remote_access: !!els['profile-remote'].checked,
            disable_after_days: num('profile-disable-days'),
            max_sessions: num('profile-sessions'),
            bitrate_limit: num('profile-bitrate'),
            can_invite: !!els['profile-can-invite'].checked,
            target_preset_id: (els['profile-target-preset'].value || '').trim(),
            invite_quota_day: num('profile-quota-day'),
            invite_quota_month: num('profile-quota-month'),
            invite_link_validity_days: num('profile-link-days'),
            invite_max_uses: num('profile-max-uses'),
        };
    }

    async function load() {
        const [presetsRes, librariesRes] = await Promise.all([
            JG.api('/admin/api/automation/presets'),
            JG.api('/admin/api/automation/libraries'),
        ]);
        if (!presetsRes?.success) {
            JG.toast(presetsRes?.message || 'Impossible de charger les profils', 'error');
            return;
        }
        state.presets = Array.isArray(presetsRes.data) ? presetsRes.data : [];
        state.libraries = Array.isArray(librariesRes?.data) ? librariesRes.data : [];
        state.selected = 0;
        renderList();
        fillForm();
    }

    async function saveAll() {
        const current = collectForm();
        if (!current.id) {
            JG.toast('Identifiant de profil requis', 'error');
            return;
        }
        state.presets[state.selected] = current;
        const res = await JG.api('/admin/api/automation/presets', {
            method: 'POST',
            body: JSON.stringify(state.presets),
        });
        if (!res?.success) {
            JG.toast(res?.message || 'Sauvegarde impossible', 'error');
            return;
        }
        JG.toast(res.message || 'Profils sauvegardés', 'success');
        await load();
    }

    function addProfile() {
        state.presets.push({
            id: `profil-${state.presets.length + 1}`,
            name: 'Nouveau profil',
            enable_all_folders: true,
            enable_download: false,
            enable_remote_access: true,
            max_sessions: 0,
            bitrate_limit: 0,
            can_invite: false,
        });
        state.selected = state.presets.length - 1;
        fillForm();
    }

    function deleteProfile() {
        if (state.presets.length <= 1) {
            JG.toast('Gardez au moins un profil', 'error');
            return;
        }
        state.presets.splice(state.selected, 1);
        state.selected = Math.max(0, state.selected - 1);
        fillForm();
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

        els['profile-form']?.addEventListener('submit', (event) => {
            event.preventDefault();
            state.presets[state.selected] = collectForm();
            renderList();
            JG.toast('Profil mis à jour dans le brouillon', 'success');
        });
        els['profiles-save-btn']?.addEventListener('click', saveAll);
        els['profile-new-btn']?.addEventListener('click', addProfile);
        els['profile-delete-btn']?.addEventListener('click', deleteProfile);
        els['profile-all-folders']?.addEventListener('change', () => {
            els['profile-libraries']?.classList.toggle('opacity-50', !!els['profile-all-folders'].checked);
        });
    });
})();
