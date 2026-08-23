(() => {
    const config = window.JGPageInvitations || {};
    const i18n = config.i18n || {};
    const uiLocale = config.uiLocale || undefined;
    const inviteBaseURL = (String(config.inviteBaseURL || '').trim() || window.location.origin);
    const isAdmin = !!config.isAdmin;
    const allowInviterGrant = !!config.allowInviterGrant;
    const allowInviterUserExpiry = !!config.allowInviterUserExpiry;
    const allowIgnoreLimits = !!config.allowIgnoreLimits;
    const inviterMaxUses = Number(config.inviterMaxUses || 0);
    const limitLinkValidityDays = Number(config.limitLinkValidityDays || 0) || Math.max(0, Math.ceil(Number(config.inviterMaxLinkHours || 0) / 24));
    const inviterMaxLinkHours = Number(config.inviterMaxLinkHours || 0);
    const inviterQuotaDay = Number(config.inviterQuotaDay || 0);
    const inviterQuotaMonth = Number(config.inviterQuotaMonth || 0);
    const limitUserExpiryDays = Number(config.limitUserExpiryDays || 0);
    const defaultDisableAfterDays = Number(config.defaultDisableAfterDays || 0);
    const targetPresetID = String(config.targetPresetID || '').trim();
    const allowedTargetPresetIDs = String(config.allowedTargetPresetIDs || '').split(',').map((v) => v.trim()).filter(Boolean);
    const canCreateTemporaryInvitations = !!config.canCreateTemporaryInvitations;
    const allowedTemporaryPresetIDs = String(config.allowedTemporaryPresetIDs || '').split(',').map((v) => v.trim()).filter(Boolean);
    const defaultTemporaryDurationDays = Number(config.defaultTemporaryDurationDays || 0);
    const maxTemporaryDurationDays = Number(config.maxTemporaryDurationDays || 0);
    const defaultLang = normalizeLangTag(config.defaultLang || 'fr') || 'fr';

    function normalizeLangTag(raw) {
        const value = String(raw || '').trim().toLowerCase().replace(/_/g, '-');
        if (!value) return '';
        if (value === 'pt' || value.startsWith('pt-')) return 'pt-br';
        if (value.includes('-')) {
            const base = value.split('-')[0];
            if (base === 'pt') return 'pt-br';
            if (base) return base;
        }
        return value;
    }

    document.addEventListener('DOMContentLoaded', () => {
        let currentPage = 1;
        let itemsPerPage = 25;
        let totalPages = 1;
        let pendingDeleteInvitationID = 0;
        let invitationPresets = [];

        function fmt(template, vars) {
            return String(template || '').replace(/\{(\w+)\}/g, (_, key) => (vars && key in vars ? String(vars[key]) : ''));
        }

        function createBtnLabel() {
            return `<svg class="w-4 h-4 mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" /></svg>${JG.esc(i18n.createLink || 'Créer le lien')}`;
        }

        async function copyLinkToClipboard(link, btnEl) {
            const ok = await JG.copyText(link);
            if (ok) {
                JG.toast(i18n.linkCopied || 'Lien copié dans le presse-papier !', 'success');
                if (btnEl) {
                    const origHtml = btnEl.innerHTML;
                    btnEl.innerHTML = `<svg class="w-4 h-4 text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path></svg><span>Copié !</span>`;
                    setTimeout(() => {
                        btnEl.innerHTML = origHtml;
                    }, 2000);
                }
            } else {
                JG.toast(i18n.copyUnavailable || 'Impossible de copier', 'error');
            }
            return ok;
        }

        function updateForcedUsernameState() {
            const maxUsesInput = document.getElementById('inv-uses');
            const forcedUserInput = document.getElementById('inv-forced-user');
            const forcedUserWrap = document.getElementById('inv-forced-user-wrap');
            const forcedUserHelp = document.getElementById('inv-forced-user-help');
            if (!maxUsesInput || !forcedUserInput) return;
            
            const maxUses = parseInt(maxUsesInput.value, 10);
            const isAllowed = maxUses === 1;
            
            forcedUserInput.disabled = !isAllowed;
            if (!isAllowed) forcedUserInput.value = '';
            
            if (forcedUserWrap) {
                forcedUserWrap.classList.toggle('opacity-40', !isAllowed);
                forcedUserWrap.classList.toggle('pointer-events-none', !isAllowed);
            }
            
            if (forcedUserHelp) {
                if (!isAllowed) {
                    forcedUserHelp.textContent = i18n.forcedUsernameLimitHint || 'Disponible uniquement pour les invitations à usage unique (1).';
                    forcedUserHelp.classList.add('text-amber-500');
                } else {
                    forcedUserHelp.textContent = i18n.forcedUsernameHelp || 'Optionnel : force le nom d\'utilisateur pré-rempli.';
                    forcedUserHelp.classList.remove('text-amber-500');
                }
            }
        }

        function updateLivePreviewCard() {
            const select = document.getElementById('inv-policy-preset');
            const preset = invitationPresets.find((item) => item.id === select?.value);
            
            const presetNameEl = document.getElementById('preview-card-preset');
            const libEl = document.getElementById('preview-card-libraries');
            if (presetNameEl) {
                presetNameEl.textContent = preset ? (preset.name || preset.id) : (i18n.profileGlobal || 'Profil global JellyGate');
            }
            if (libEl) {
                if (!preset || preset.enable_all_folders) {
                    libEl.textContent = i18n.profileAllLibraries || 'Toutes les bibliothèques';
                } else {
                    const count = (preset.enabled_folder_ids || []).length;
                    libEl.textContent = `${count} bibliothèque(s) accessible(s)`;
                }
            }

            // Uses hint & badge
            const usesInput = document.getElementById('inv-uses');
            const usesHint = document.getElementById('uses-badge-hint');
            const usesEl = document.getElementById('preview-card-uses');
            if (usesInput) {
                const u = parseInt(usesInput.value, 10);
                const txt = u === 1 ? '1 max' : (u > 1 ? `${u} max` : 'Illimité');
                if (usesHint) usesHint.textContent = txt;
                if (usesEl) usesEl.textContent = u === 1 ? '1 (Usage unique)' : (u > 1 ? `${u} utilisations` : (i18n.unlimited || 'Illimité'));
            }

            // Expiry hint & badge
            const daysInput = document.getElementById('inv-expiry-days');
            const expiryHint = document.getElementById('expiry-badge-hint');
            const expiryEl = document.getElementById('preview-card-expiry');
            if (daysInput) {
                const d = parseInt(daysInput.value, 10);
                const txt = d === 1 ? '24h' : (d > 1 ? `${d} jours` : 'Permanent');
                if (expiryHint) expiryHint.textContent = txt;
                if (expiryEl) expiryEl.textContent = d === 1 ? '24 heures' : (d > 1 ? `${d} jours` : 'Permanent (sans limite)');
            }

            // Account type
            const tempInput = document.getElementById('inv-is-temporary');
            const durationInput = document.getElementById('inv-account-duration-days');
            const accountTypeEl = document.getElementById('preview-card-account-type');
            if (accountTypeEl) {
                if (tempInput && tempInput.checked) {
                    const dur = durationInput?.value || '30';
                    accountTypeEl.textContent = `Compte temporaire (${dur} jours d'accès)`;
                } else {
                    accountTypeEl.textContent = 'Compte permanent';
                }
            }
        }

        function applySelectedInvitePreset() {
            const select = document.getElementById('inv-policy-preset');
            const summary = document.getElementById('inv-profile-summary');
            const tagsContainer = document.getElementById('inv-preset-tags');
            const preset = invitationPresets.find((item) => item.id === select?.value);
            
            if (tagsContainer) {
                if (!preset) {
                    tagsContainer.innerHTML = `<span class="px-2.5 py-0.5 rounded-lg bg-white/5 border border-white/10 text-xs text-slate-300 font-medium">Profil par défaut</span>`;
                } else {
                    const tags = [];
                    if (preset.enable_all_folders) {
                        tags.push(`<span class="px-2.5 py-0.5 rounded-lg bg-emerald-500/10 border border-emerald-500/20 text-xs text-emerald-400 font-semibold">Toutes bibliothèques</span>`);
                    } else {
                        const count = (preset.enabled_folder_ids || []).length;
                        tags.push(`<span class="px-2.5 py-0.5 rounded-lg bg-cyan-500/10 border border-cyan-500/20 text-xs text-cyan-300 font-semibold">${count} bibliothèque(s)</span>`);
                    }
                    if (preset.is_administrator) {
                        tags.push(`<span class="px-2.5 py-0.5 rounded-lg bg-amber-500/10 border border-amber-500/20 text-xs text-amber-300 font-semibold">Admin</span>`);
                    }
                    if (preset.can_invite) {
                        tags.push(`<span class="px-2.5 py-0.5 rounded-lg bg-purple-500/10 border border-purple-500/20 text-xs text-purple-300 font-semibold">Parrain</span>`);
                    }
                    if (preset.is_temporary || preset.disable_after_days > 0) {
                        tags.push(`<span class="px-2.5 py-0.5 rounded-lg bg-rose-500/10 border border-rose-500/20 text-xs text-rose-300 font-semibold">Temporaire (${preset.disable_after_days || preset.default_account_duration_days || 30}j)</span>`);
                    }
                    tagsContainer.innerHTML = tags.join(' ');
                }
            }

            if (summary) {
                if (!preset) {
                    summary.textContent = 'Applique les règles générales et bibliothèques configurées dans JellyGate.';
                } else {
                    summary.textContent = preset.description || `Configuration issue du modèle « ${preset.name || preset.id} ».`;
                }
            }

            updateTemporaryInvitationState(preset);
            updateLivePreviewCard();
        }

        function updateTemporaryInvitationState(preset) {
            const tempInput = document.getElementById('inv-is-temporary');
            const durationInput = document.getElementById('inv-account-duration-days');
            const tempConfigBox = document.getElementById('temp-account-config');
            const help = document.getElementById('inv-temp-help');
            if (!tempInput || !durationInput) return;

            const profileIsTemporary = !!preset?.is_temporary;
            const allowedBySponsor = isAdmin || canCreateTemporaryInvitations;
            const selectedID = String(document.getElementById('inv-policy-preset')?.value || targetPresetID || '').trim();
            const allowedProfile = isAdmin || !selectedID || allowedTemporaryPresetIDs.includes(selectedID);
            const defaultDays = Number(preset?.default_account_duration_days || preset?.disable_after_days || defaultTemporaryDurationDays || 30);
            const maxDays = Number(preset?.max_account_duration_days || maxTemporaryDurationDays || 0);

            if (profileIsTemporary) tempInput.checked = true;
            tempInput.disabled = profileIsTemporary || !allowedBySponsor || !allowedProfile;
            durationInput.disabled = !tempInput.checked || tempInput.disabled;
            
            if (tempConfigBox) {
                tempConfigBox.classList.toggle('hidden', !tempInput.checked);
            }

            if (tempInput.checked && (!durationInput.value || durationInput.value === '30')) {
                durationInput.value = String(defaultDays || 30);
            }
            if (maxDays > 0 && Number(durationInput.value || 0) > maxDays) {
                durationInput.value = String(maxDays);
            }
            if (help) {
                const parts = [];
                if (!allowedBySponsor) parts.push('Votre profil ne permet pas les invitations temporaires.');
                if (allowedBySponsor && !allowedProfile) parts.push('Ce profil cible n\'est pas autorisé comme temporaire.');
                if (maxDays > 0) parts.push(`Maximum ${maxDays} jour(s).`);
                help.textContent = parts.join(' ');
            }
        }

        function applyInvitationPolicyUI() {
            const usesInput = document.getElementById('inv-uses');
            const linkDaysInput = document.getElementById('inv-expiry-days');
            const canInviteInput = document.getElementById('inv-new-user-can-invite');
            const canInviteWrap = document.getElementById('inv-can-invite-wrap');
            const ignoreLinkWrap = document.getElementById('inv-ignore-link-limit-wrap');
            const ignoreLinkInput = document.getElementById('inv-ignore-link-limit');
            const usesHelp = document.getElementById('inv-uses-help');
            const linkHelp = document.getElementById('inv-link-expiry-help');
            const canInviteHelp = document.getElementById('inv-can-invite-help');

            if (!isAdmin) {
                if (usesInput && inviterMaxUses > 0) {
                    usesInput.max = String(inviterMaxUses);
                    if (Number(usesInput.value || 0) > inviterMaxUses || Number(usesInput.value || 0) <= 0) {
                        usesInput.value = String(inviterMaxUses);
                    }
                    if (usesHelp) usesHelp.textContent = fmt(i18n.usesHelpLimited, { n: inviterMaxUses });
                }
                if (linkDaysInput && limitLinkValidityDays > 0) {
                    if (Number(linkDaysInput.value || 0) <= 0 || Number(linkDaysInput.value || 0) > limitLinkValidityDays) {
                        linkDaysInput.value = String(limitLinkValidityDays);
                    }
                    if (linkHelp) linkHelp.textContent = fmt(i18n.linkHelpLimited, { n: inviterMaxLinkHours || (limitLinkValidityDays * 24) });
                }
                if (ignoreLinkWrap) ignoreLinkWrap.classList.add('hidden');
                if (ignoreLinkInput) {
                    ignoreLinkInput.checked = false;
                    ignoreLinkInput.disabled = true;
                }
                if (canInviteInput) {
                    canInviteInput.disabled = !allowInviterGrant;
                    if (!allowInviterGrant) canInviteInput.checked = false;
                }
                if (canInviteWrap && !allowInviterGrant) {
                    canInviteWrap.classList.add('opacity-40', 'pointer-events-none');
                }
                if (canInviteHelp && !allowInviterGrant) {
                    canInviteHelp.textContent = i18n.invitePolicyLimited || 'Droit de parrainage non accordé sur votre profil.';
                }
            } else {
                if (ignoreLinkWrap) ignoreLinkWrap.classList.remove('hidden');
                if (ignoreLinkInput) ignoreLinkInput.disabled = false;
                if (canInviteInput) canInviteInput.disabled = false;
                if (canInviteWrap) canInviteWrap.classList.remove('opacity-40', 'pointer-events-none');
            }

            syncPillButtons();
            updateForcedUsernameState();
            updateLivePreviewCard();
        }

        function syncPillButtons() {
            const usesVal = String(document.getElementById('inv-uses')?.value ?? '');
            document.querySelectorAll('#quick-uses-pills .quick-pill, [data-pill-group="uses"] .quick-pill').forEach((pill) => {
                pill.classList.toggle('active', pill.dataset.value === usesVal);
            });

            const daysVal = String(document.getElementById('inv-expiry-days')?.value ?? '');
            document.querySelectorAll('#quick-days-pills .quick-pill, [data-pill-group="days"] .quick-pill').forEach((pill) => {
                pill.classList.toggle('active', pill.dataset.value === daysVal);
            });
        }

        // Direct event binding for quick pills
        function setupQuickPills() {
            document.querySelectorAll('.quick-pill').forEach((pill) => {
                pill.addEventListener('click', (e) => {
                    e.preventDefault();
                    const parent = pill.closest('#quick-uses-pills, #quick-days-pills, [data-pill-group]');
                    if (!parent) return;
                    
                    parent.querySelectorAll('.quick-pill').forEach((p) => p.classList.remove('active'));
                    pill.classList.add('active');
                    
                    const val = pill.dataset.value;
                    const isUses = parent.id === 'quick-uses-pills' || parent.dataset.pillGroup === 'uses';
                    const isDays = parent.id === 'quick-days-pills' || parent.dataset.pillGroup === 'days';
                    
                    if (isUses) {
                        const usesInput = document.getElementById('inv-uses');
                        if (usesInput) {
                            usesInput.value = val;
                            usesInput.dispatchEvent(new Event('input', { bubbles: true }));
                            usesInput.dispatchEvent(new Event('change', { bubbles: true }));
                        }
                    } else if (isDays) {
                        const daysInput = document.getElementById('inv-expiry-days');
                        if (daysInput) {
                            daysInput.value = val;
                            daysInput.dispatchEvent(new Event('input', { bubbles: true }));
                            daysInput.dispatchEvent(new Event('change', { bubbles: true }));
                        }
                    }
                });
            });
        }

        async function loadInviteWizardData() {
            const presetsRes = await JG.api('/admin/api/automation/presets');
            invitationPresets = Array.isArray(presetsRes?.data) ? presetsRes.data : [];

            const select = document.getElementById('inv-policy-preset');
            if (select) {
                let visiblePresets = invitationPresets;
                if (!isAdmin) {
                    const allowed = new Set(allowedTargetPresetIDs.length ? allowedTargetPresetIDs : [targetPresetID].filter(Boolean));
                    visiblePresets = invitationPresets.filter((preset) => allowed.has(preset.id));
                    if (!visiblePresets.length) {
                        visiblePresets = Array.from(allowed).map((id) => ({ id, name: id }));
                    }
                }
                select.innerHTML = (isAdmin ? `<option value="">${JG.esc(i18n.profileGlobal || 'Politique globale JellyGate')}</option>` : '') + visiblePresets.map((preset) => {
                    const adminSuffix = preset.is_administrator ? ' · admin' : '';
                    return `<option value="${JG.esc(preset.id || '')}">${JG.esc((preset.name || preset.id || 'Profil') + adminSuffix)}</option>`;
                }).join('');
                if (!isAdmin && targetPresetID) select.value = targetPresetID;
            }
            applySelectedInvitePreset();
        }

        async function loadInvitations() {
            const search = encodeURIComponent(document.getElementById('search-invites')?.value || '');
            const status = encodeURIComponent(document.getElementById('filter-status')?.value || 'all');
            const res = await JG.api(`/admin/api/invitations?page=${currentPage}&limit=${itemsPerPage}&search=${search}&status=${status}`);
            if (res.success && res.data) {
                const invitations = res.data.invitations || [];
                const meta = res.data.meta || {};
                
                totalPages = meta.total_pages || 1;
                currentPage = meta.page || 1;
                
                renderInvitations(invitations);
                renderPagination(meta);
            } else {
                JG.toast(i18n.loadError || 'Erreur de chargement des invitations', 'error');
            }
        }

        function renderPagination(meta) {
            const info = document.getElementById('pagination-info');
            if (info) {
                info.textContent = `Page ${meta.page} / ${meta.total_pages}`;
            }

            const prevBtn = document.getElementById('prev-page');
            const nextBtn = document.getElementById('next-page');
            if (prevBtn) prevBtn.disabled = meta.page <= 1;
            if (nextBtn) nextBtn.disabled = meta.page >= meta.total_pages;

            const pageNumbers = document.getElementById('page-numbers');
            if (pageNumbers) {
                let html = '';
                const start = Math.max(1, meta.page - 2);
                const end = Math.min(meta.total_pages, meta.page + 2);
                
                for (let i = start; i <= end; i++) {
                    const activeClass = i === meta.page ? 'bg-jg-accent text-always-white shadow-lg shadow-jg-accent/20' : 'bg-jg-bg-secondary text-jg-text-muted hover:text-jg-text border border-jg-border';
                    html += `<button class="w-8 h-8 flex items-center justify-center rounded-lg font-bold text-xs transition-all page-btn ${activeClass}" data-page="${i}">${i}</button>`;
                }
                pageNumbers.innerHTML = html;
            }
        }

        async function loadSponsorStats() {
            const res = await JG.api('/admin/api/invitations/stats');
            if (!res || !res.success || !res.data) return;

            const data = res.data;
            const stats = data.stats || data;
            
            const fields = {
                'sponsor-total-links': stats.total_links,
                'sponsor-active-links': stats.active_links,
                'sponsor-closed-links': stats.closed_links,
                'sponsor-conversions': stats.conversions,
                'sponsor-conversion-rate': (stats.conversion_rate || 0).toFixed(1) + '%'
            };

            for (const [id, val] of Object.entries(fields)) {
                const el = document.getElementById(id);
                if (el) el.textContent = String(val);
            }
        }

        function renderInvitations(list) {
            const tbody = document.getElementById('invites-tbody');
            if (!tbody) return;
            
            if (list.length === 0) {
                tbody.innerHTML = `<tr><td colspan="6" class="text-center text-jg-text-muted py-12 font-medium">${JG.esc(i18n.noActiveInvitations || 'Aucune invitation trouvée')}</td></tr>`;
                return;
            }

            tbody.innerHTML = list.map((invitation) => {
                const link = invitation.invite_url || `${inviteBaseURL}/invite/${invitation.code}`;
                const authLink = invitation.authentik_enrollment_url || '';
                const primaryLink = authLink || link;
                const expDate = invitation.expires_at ? new Date(invitation.expires_at).toLocaleDateString(uiLocale) : '—';
                const profile = invitation.jellyfin_profile || {};
                
                const expiryLabel = profile.user_expires_at 
                    ? fmt(i18n.expiresOn, { date: new Date(profile.user_expires_at).toLocaleString(uiLocale) })
                    : (profile.user_expiry_days > 0 ? fmt(i18n.disableAfterDays, { n: profile.user_expiry_days }) : (i18n.unlimited || 'Illimité'));
                
                const deleteLabel = profile.delete_after_days > 0 ? fmt(i18n.deleteAfterDays, { n: profile.delete_after_days }) : (i18n.noDeletePlanned || 'Permanent');
                const roleLabel = profile.can_invite ? (i18n.roleCanInvite || 'Parrain') : (i18n.roleStandard || 'Standard');
                const groupLabel = profile.group_name ? fmt(i18n.groupPrefix, { group: profile.group_name }) : (i18n.groupDefault || 'Défaut');
                const inviteLang = normalizeLangTag(invitation.preferred_lang || '') || defaultLang;
                
                const isOver = (invitation.max_uses > 0 && invitation.used_count >= invitation.max_uses) || (invitation.expires_at && new Date(invitation.expires_at) < new Date());
                const badge = isOver 
                    ? `<span class="badge badge-danger">${JG.esc(i18n.badgeExpired || 'Expiré')}</span>` 
                    : `<span class="badge badge-success">${JG.esc(i18n.badgeActive || 'Actif')}</span>`;

                return `<tr class="${isOver ? 'opacity-40' : 'hover:bg-white/[0.02] transition-colors'}">
                    <td class="px-5 py-4">
                        <div class="flex items-center gap-2.5">
                            <code class="px-2.5 py-1 bg-black/40 border border-purple-500/30 rounded-lg text-purple-300 font-mono font-bold text-xs tracking-wider select-all shadow-inner">${invitation.code}</code>
                            <div class="flex items-center gap-1">
                                <button class="jg-btn-icon action-copy-link" data-link="${encodeURIComponent(primaryLink)}" title="${JG.esc(i18n.copyFullLinkTitle || 'Copier le lien d\'invitation')}">
                                    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
                                </button>
                                ${authLink ? `
                                <button class="jg-btn-icon action-copy-authentik text-purple-400 hover:text-purple-300 hover:bg-purple-500/20" data-auth-link="${encodeURIComponent(authLink)}" title="Copier le lien direct Authentik">
                                    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z"></path></svg>
                                </button>
                                ` : ''}
                                <button class="jg-btn-icon jg-btn-icon-accent action-qr-code" data-link="${encodeURIComponent(primaryLink)}" title="Afficher le Code QR">
                                    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v1m6 11h2m-6 0h-2v4m0-11v3m0 0h.01M12 12h4.01M16 20h4M4 12h4m12 0h.01M5 8h2a1 1 0 001-1V5a1 1 0 00-1-1H5a1 1 0 00-1 1v2a1 1 0 001 1zm14 0h2a1 1 0 001-1V5a1 1 0 00-1-1h-2a1 1 0 00-1 1v2a1 1 0 001 1zM5 20h2a1 1 0 001-1v-2a1 1 0 00-1-1H5a1 1 0 00-1 1v2a1 1 0 001 1z"></path></svg>
                                </button>
                            </div>
                        </div>
                    </td>
                    <td class="px-5 py-4"><div class="flex items-center gap-2"><span class="font-bold text-slate-100 text-sm">${invitation.used_count} / ${invitation.max_uses > 0 ? invitation.max_uses : '∞'}</span> ${badge}</div></td>
                    <td class="px-5 py-4"><span class="badge badge-muted">${expDate}</span></td>
                    <td class="px-5 py-4">
                        <div class="font-bold text-slate-100 text-xs">${JG.esc(expiryLabel)} · <span class="text-purple-400">${JG.esc(roleLabel)}</span></div>
                        <div class="text-[10px] text-slate-400 uppercase tracking-wider mt-1">${JG.esc(deleteLabel)} | ${JG.esc(groupLabel)} | LANG: ${JG.esc(String(inviteLang).toUpperCase())}</div>
                    </td>
                    <td class="px-5 py-4"><span class="badge badge-accent">${JG.esc(invitation.created_by || 'System')}</span></td>
                    <td class="px-5 py-4 text-right">
                        <button class="jg-btn-icon jg-btn-icon-danger action-delete-invite" data-id="${invitation.id}" title="${JG.esc(i18n.deleted || 'Supprimer')}">
                            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg>
                        </button>
                    </td>
                </tr>`;
            }).join('');
        }

        function showInvitationSuccessView(inviteData) {
            const formView = document.getElementById('invite-modal-form-view');
            const successView = document.getElementById('invite-modal-success-view');
            if (formView) formView.classList.add('hidden');
            if (successView) successView.classList.remove('hidden');

            const fullInviteLink = inviteData.invite_url || inviteData.url || `${inviteBaseURL}/invite/${inviteData.code}`;
            const authLink = inviteData.authentik_enrollment_url || '';
            const isAuth = !!(authLink && (inviteData.authentik_enabled !== false));
            const targetLink = isAuth ? authLink : fullInviteLink;

            // Single primary link input
            const linkInput = document.getElementById('created-link-url');
            if (linkInput) linkInput.value = targetLink;

            const linkLabel = document.getElementById('created-link-label');
            if (linkLabel) linkLabel.textContent = isAuth ? 'Lien d\'inscription Authentik' : 'Lien d\'invitation';

            const linkBadge = document.getElementById('created-link-badge');
            if (linkBadge) linkBadge.textContent = isAuth ? 'Authentik' : 'Standard';

            const linkCopyBtn = document.getElementById('created-link-copy-btn');
            if (linkCopyBtn) {
                linkCopyBtn.onclick = () => copyLinkToClipboard(targetLink, linkCopyBtn);
            }

            const openBtn = document.getElementById('created-link-open-btn');
            if (openBtn) {
                openBtn.href = targetLink;
            }

            // Legacy elements
            const authInput = document.getElementById('created-authentik-url');
            if (authInput) authInput.value = authLink || targetLink;
            const authCopyBtn = document.getElementById('created-authentik-copy-btn');
            if (authCopyBtn) authCopyBtn.onclick = () => copyLinkToClipboard(targetLink, authCopyBtn);
            const authOpenBtn = document.getElementById('created-authentik-open-btn');
            if (authOpenBtn) authOpenBtn.href = targetLink;

            // QR Code for the target link
            const qrImg = document.getElementById('created-qr-img');
            if (qrImg) {
                if (window.JGQRCode && typeof window.JGQRCode.toDataURL === 'function') {
                    qrImg.src = window.JGQRCode.toDataURL(targetLink, { size: 280, margin: 2, darkColor: '#09090b', lightColor: '#ffffff' });
                } else {
                    qrImg.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="200" height="200"><text x="10" y="100">QR Code</text></svg>')}`;
                }
            }

            const qrDownloadBtn = document.getElementById('created-qr-download-btn');
            if (qrDownloadBtn) {
                qrDownloadBtn.onclick = () => {
                    const a = document.createElement('a');
                    a.href = qrImg ? qrImg.src : targetLink;
                    a.download = `jellygate-invitation-${inviteData.code}.png`;
                    document.body.appendChild(a);
                    a.click();
                    document.body.removeChild(a);
                };
            }
        }

        function resetCreateModalState() {
            const formView = document.getElementById('invite-modal-form-view');
            const successView = document.getElementById('invite-modal-success-view');
            if (formView) formView.classList.remove('hidden');
            if (successView) successView.classList.add('hidden');

            document.getElementById('create-form')?.reset();
            
            // Set defaults: 1 use, 7 days
            const usesInput = document.getElementById('inv-uses');
            const daysInput = document.getElementById('inv-expiry-days');
            if (usesInput) usesInput.value = '1';
            if (daysInput) daysInput.value = '7';

            applyInvitationPolicyUI();
            applySelectedInvitePreset();
            syncPillButtons();
        }

        async function submitCreate(event) {
            event.preventDefault();
            const btn = document.getElementById('create-btn');
            if (!btn) return;
            btn.disabled = true;
            btn.innerHTML = '<span class="spinner w-4 h-4 border-2 border-white border-t-transparent animate-spin rounded-full inline-block"></span>';

            const maxUsesInput = document.getElementById('inv-uses');
            const expiryDaysInput = document.getElementById('inv-expiry-days');
            const userExpiryEnabledInput = document.getElementById('inv-user-expiry-enabled');
            const userExpiryDaysInput = document.getElementById('inv-user-expiry-days');
            const canInviteInput = document.getElementById('inv-new-user-can-invite');
            const forcedUserInput = document.getElementById('inv-forced-user');
            const emailInput = document.getElementById('inv-email');
            const preferredLangInput = document.getElementById('inv-preferred-lang');
            const ignoreLinkInput = document.getElementById('inv-ignore-link-limit');
            const ignoreUserInput = document.getElementById('inv-ignore-user-expiry-limit');
            const policyPresetInput = document.getElementById('inv-policy-preset');
            const emailMessageInput = document.getElementById('inv-email-message');
            const temporaryInput = document.getElementById('inv-is-temporary');
            const accountDurationInput = document.getElementById('inv-account-duration-days');

            const maxUses = parseInt(maxUsesInput?.value || '0', 10) || 0;
            let expiresInDays = parseInt(expiryDaysInput?.value || '0', 10) || 0;
            const userExpiryEnabled = !!userExpiryEnabledInput?.checked;
            let userExpiryDays = parseInt(userExpiryDaysInput?.value || '0', 10) || 0;
            const grantInvite = !!canInviteInput?.checked;
            const isTemporary = !!temporaryInput?.checked;
            const accountDurationDays = parseInt(accountDurationInput?.value || '0', 10) || 0;
            const forcedUsername = (forcedUserInput?.value || '').trim();
            const ignorePresetLinkExpiry = !!(ignoreLinkInput && !ignoreLinkInput.disabled && ignoreLinkInput.checked);
            const ignorePresetUserExpiry = !!(ignoreUserInput && !ignoreUserInput.disabled && ignoreUserInput.checked);
            const preferredLang = normalizeLangTag(preferredLangInput?.value || '');

            if (!isAdmin && inviterMaxUses > 0 && (maxUses <= 0 || maxUses > inviterMaxUses)) {
                btn.disabled = false;
                btn.innerHTML = createBtnLabel();
                JG.toast(fmt(i18n.invalidMaxUses, { n: inviterMaxUses }), 'error');
                return;
            }

            if (!ignorePresetLinkExpiry && limitLinkValidityDays > 0) {
                if (expiresInDays <= 0) {
                    expiresInDays = limitLinkValidityDays;
                }
                if (!isAdmin && expiresInDays > limitLinkValidityDays) {
                    btn.disabled = false;
                    btn.innerHTML = createBtnLabel();
                    JG.toast(fmt(i18n.maxTtl, { n: inviterMaxLinkHours || (limitLinkValidityDays * 24) }), 'error');
                    return;
                }
            }

            if (userExpiryEnabled && userExpiryDays <= 0) {
                btn.disabled = false;
                btn.innerHTML = createBtnLabel();
                JG.toast(i18n.invalidUserExpiry || 'Durée d\'expiration invalide', 'error');
                return;
            }

            const data = {
                max_uses: maxUses,
                expires_in_days: expiresInDays,
                ignore_preset_link_expiry: ignorePresetLinkExpiry,
                apply_user_expiry: userExpiryEnabled,
                user_expiry_days: userExpiryEnabled ? userExpiryDays : 0,
                is_temporary: isTemporary,
                account_duration_days: isTemporary ? accountDurationDays : 0,
                ignore_preset_user_expiry: ignorePresetUserExpiry,
                new_user_can_invite: grantInvite,
                forced_username: forcedUsername,
                send_to_email: (emailInput?.value || '').trim(),
                preferred_lang: preferredLang,
                policy_preset_id: (policyPresetInput?.value || '').trim(),
                email_message: (emailMessageInput?.value || '').trim(),
            };

            const res = await JG.api('/admin/api/invitations', { method: 'POST', body: JSON.stringify(data) });
            btn.disabled = false;
            btn.innerHTML = createBtnLabel();

            if (res.success && res.data) {
                showInvitationSuccessView(res.data);
                loadInvitations();
                loadSponsorStats();
            } else {
                JG.toast(res.message || i18n.unknownError || 'Erreur lors de la création', 'error');
            }
        }

        async function submitDelete() {
            const res = await JG.api(`/admin/api/invitations/${pendingDeleteInvitationID}`, { method: 'DELETE' });
            if (res.success) {
                JG.toast(i18n.deleted || 'Invitation supprimée', 'success');
                JG.closeModal('delete-modal');
                loadInvitations();
                loadSponsorStats();
            } else {
                JG.toast(res.message || i18n.unknownError, 'error');
            }
        }

        // --- Event Listeners ---
        document.body.addEventListener('click', (e) => {
            const closeTrigger = e.target.closest('[data-modal-close]');
            if (closeTrigger) {
                const modalId = closeTrigger.getAttribute('data-modal-close');
                if (modalId) {
                    JG.closeModal(modalId);
                }
                return;
            }

            // Quick Pill buttons delegated fallback
            const pill = e.target.closest('.quick-pill');
            if (pill) {
                const parent = pill.closest('#quick-uses-pills, #quick-days-pills, [data-pill-group]');
                if (parent) {
                    parent.querySelectorAll('.quick-pill').forEach((p) => p.classList.remove('active'));
                    pill.classList.add('active');
                    const val = pill.dataset.value;
                    const isUses = parent.id === 'quick-uses-pills' || parent.dataset.pillGroup === 'uses';
                    const isDays = parent.id === 'quick-days-pills' || parent.dataset.pillGroup === 'days';

                    if (isUses) {
                        const usesInput = document.getElementById('inv-uses');
                        if (usesInput) {
                            usesInput.value = val;
                            usesInput.dispatchEvent(new Event('input', { bubbles: true }));
                        }
                    } else if (isDays) {
                        const daysInput = document.getElementById('inv-expiry-days');
                        if (daysInput) {
                            daysInput.value = val;
                            daysInput.dispatchEvent(new Event('input', { bubbles: true }));
                        }
                    }
                }
                return;
            }

            // Copy JellyGate link
            const copyBtn = e.target.closest('.action-copy-link');
            if (copyBtn) {
                copyLinkToClipboard(decodeURIComponent(copyBtn.getAttribute('data-link')), copyBtn);
                return;
            }

            // Copy Authentik direct enrollment link
            const authCopyBtn = e.target.closest('.action-copy-authentik');
            if (authCopyBtn) {
                copyLinkToClipboard(decodeURIComponent(authCopyBtn.getAttribute('data-auth-link')), authCopyBtn);
                return;
            }

            // QR Code modal trigger from table
            const qrBtn = e.target.closest('.action-qr-code');
            if (qrBtn) {
                const link = decodeURIComponent(qrBtn.getAttribute('data-link'));
                const img = document.getElementById('qr-code-img');
                const preview = document.getElementById('qr-link-preview');
                if (preview) preview.textContent = link;
                if (img) {
                    if (window.JGQRCode && typeof window.JGQRCode.toDataURL === 'function') {
                        img.src = window.JGQRCode.toDataURL(link, { size: 280, margin: 2, darkColor: '#09090b', lightColor: '#ffffff' });
                    } else {
                        img.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="200" height="200"><text x="10" y="100">QR Code</text></svg>')}`;
                    }
                }
                const qrCopyBtn = document.getElementById('qr-copy-btn');
                if (qrCopyBtn) {
                    qrCopyBtn.onclick = () => {
                        copyLinkToClipboard(link);
                        JG.closeModal('qr-modal');
                    };
                }
                const qrDownloadBtn = document.getElementById('qr-download-btn');
                if (qrDownloadBtn) {
                    qrDownloadBtn.onclick = () => {
                        const a = document.createElement('a');
                        a.href = img ? img.src : link;
                        a.download = 'jellygate-invitation-qr.png';
                        document.body.appendChild(a);
                        a.click();
                        document.body.removeChild(a);
                    };
                }
                JG.openModal('qr-modal');
                return;
            }

            // Create another invitation in success view
            if (e.target.closest('#btn-create-another')) {
                resetCreateModalState();
                return;
            }

            // Finish modal in success view
            if (e.target.closest('#btn-finish-modal')) {
                JG.closeModal('create-modal');
                resetCreateModalState();
                return;
            }

            // Delete modal trigger
            const deleteBtn = e.target.closest('.action-delete-invite');
            if (deleteBtn) {
                pendingDeleteInvitationID = parseInt(deleteBtn.getAttribute('data-id'), 10);
                JG.openModal('delete-modal');
                return;
            }

            if (e.target.id === 'delete-confirm-btn') {
                submitDelete();
                return;
            }

            // Open Create Modal
            if (e.target.closest('.btn-open-create-modal')) {
                resetCreateModalState();
                JG.openModal('create-modal');
                return;
            }

            const pageBtn = e.target.closest('.page-btn');
            if (pageBtn) {
                currentPage = parseInt(pageBtn.getAttribute('data-page'), 10);
                loadInvitations();
                return;
            }

            if (e.target.closest('#prev-page') && currentPage > 1) {
                currentPage--;
                loadInvitations();
                return;
            }

            if (e.target.closest('#next-page') && currentPage < totalPages) {
                currentPage++;
                loadInvitations();
                return;
            }
        });

        const itemsPerPageSelect = document.getElementById('items-per-page');
        if (itemsPerPageSelect) {
            itemsPerPageSelect.addEventListener('change', () => {
                itemsPerPage = parseInt(itemsPerPageSelect.value, 10);
                currentPage = 1;
                loadInvitations();
            });
        }

        const searchInput = document.getElementById('search-invites');
        if (searchInput) {
            let searchTimeout;
            searchInput.addEventListener('input', () => {
                clearTimeout(searchTimeout);
                searchTimeout = setTimeout(() => {
                    currentPage = 1;
                    loadInvitations();
                }, 300);
            });
        }

        const filterStatus = document.getElementById('filter-status');
        if (filterStatus) {
            filterStatus.addEventListener('change', () => {
                currentPage = 1;
                loadInvitations();
            });
        }

        const createForm = document.getElementById('create-form');
        if (createForm) createForm.addEventListener('submit', submitCreate);

        const usesInput = document.getElementById('inv-uses');
        if (usesInput) {
            usesInput.addEventListener('input', () => {
                syncPillButtons();
                updateForcedUsernameState();
                updateLivePreviewCard();
            });
        }

        const daysInput = document.getElementById('inv-expiry-days');
        if (daysInput) {
            daysInput.addEventListener('input', () => {
                syncPillButtons();
                updateLivePreviewCard();
            });
        }

        const tempEnabled = document.getElementById('inv-is-temporary');
        if (tempEnabled) {
            tempEnabled.addEventListener('change', () => {
                const preset = invitationPresets.find((item) => item.id === document.getElementById('inv-policy-preset')?.value);
                updateTemporaryInvitationState(preset);
                updateLivePreviewCard();
            });
        }

        const durationInput = document.getElementById('inv-account-duration-days');
        if (durationInput) {
            durationInput.addEventListener('input', updateLivePreviewCard);
        }

        document.getElementById('inv-policy-preset')?.addEventListener('change', applySelectedInvitePreset);

        setupQuickPills();
        loadInvitations();
        loadSponsorStats();
        loadInviteWizardData();
    });
})();
