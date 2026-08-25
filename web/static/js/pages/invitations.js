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

        function getEffectiveMaxUses() {
            const usesSelect = document.getElementById('inv-uses-select');
            const customInput = document.getElementById('inv-uses');
            if (usesSelect && usesSelect.value !== 'custom') {
                return parseInt(usesSelect.value, 10);
            }
            return customInput ? parseInt(customInput.value || '1', 10) : 1;
        }

        function getEffectiveExpiryDays() {
            const expirySelect = document.getElementById('inv-expiry-select');
            const customInput = document.getElementById('inv-expiry-days');
            if (expirySelect && expirySelect.value !== 'custom') {
                return parseInt(expirySelect.value, 10);
            }
            return customInput ? parseInt(customInput.value || '7', 10) : 7;
        }

        function updateForcedUsernameState() {
            const maxUses = getEffectiveMaxUses();
            const forcedNameInput = document.getElementById('inv-forced-name');
            const forcedUserInput = document.getElementById('inv-forced-user');
            const forcedIdentityWrap = document.getElementById('inv-forced-identity-wrap');
            const forcedUserHelp = document.getElementById('inv-forced-user-help');
            
            const isAllowed = maxUses === 1;
            
            if (forcedNameInput) {
                forcedNameInput.disabled = !isAllowed;
                if (!isAllowed) forcedNameInput.value = '';
            }
            if (forcedUserInput) {
                forcedUserInput.disabled = !isAllowed;
                if (!isAllowed) forcedUserInput.value = '';
            }
            
            if (forcedIdentityWrap) {
                forcedIdentityWrap.classList.toggle('opacity-40', !isAllowed);
                forcedIdentityWrap.classList.toggle('pointer-events-none', !isAllowed);
            }
            
            if (forcedUserHelp) {
                if (!isAllowed) {
                    forcedUserHelp.textContent = i18n.forcedUsernameLimitHint || 'Disponible uniquement pour les invitations à usage unique (1).';
                    forcedUserHelp.classList.add('text-amber-400');
                } else {
                    forcedUserHelp.textContent = i18n.forcedUsernameHelp || 'Optionnel : nom d\'affichage et identifiant pré-remplis pour cet invité.';
                    forcedUserHelp.classList.remove('text-amber-400');
                }
            }
        }

        function applySelectedInvitePreset() {
            const select = document.getElementById('inv-policy-preset');
            const summary = document.getElementById('inv-profile-summary');
            const badge = document.getElementById('inv-preset-badge');
            const preset = invitationPresets.find((item) => item.id === select?.value);
            
            if (badge) {
                badge.textContent = preset ? (preset.name || preset.id) : 'Politique globale';
            }

            if (summary) {
                if (!preset) {
                    summary.textContent = 'Accès standard configuré par défaut sur JellyGate.';
                } else {
                    const libText = preset.enable_all_folders
                        ? (i18n.profileAllLibraries || 'Toutes les bibliothèques accessibles')
                        : `${(preset.enabled_folder_ids || []).length} bibliothèque(s) autorisée(s)`;
                    summary.textContent = `${preset.name || preset.id} : ${libText}.`;
                }
            }
        }

        function applyInvitationPolicyUI() {
            const select = document.getElementById('inv-policy-preset');
            if (!select) return;

            let allowed = invitationPresets;
            if (!isAdmin && allowedTargetPresetIDs.length > 0) {
                allowed = allowed.filter((item) => allowedTargetPresetIDs.includes(item.id));
            }

            const current = select.value;
            select.innerHTML = '<option value="">Politique globale JellyGate</option>';
            allowed.forEach((p) => {
                const opt = document.createElement('option');
                opt.value = p.id;
                opt.textContent = p.name || p.id;
                if (targetPresetID && p.id === targetPresetID) opt.selected = true;
                select.appendChild(opt);
            });

            if (current && allowed.some((p) => p.id === current)) {
                select.value = current;
            }
        }

        async function loadInviteWizardData() {
            try {
                const res = await JG.api('/admin/api/automation/presets');
                if (res && res.success && Array.isArray(res.data)) {
                    invitationPresets = res.data;
                    applyInvitationPolicyUI();
                    applySelectedInvitePreset();
                }
            } catch (err) {
                console.warn('[Invitations] Presets non chargés:', err);
            }
        }

        async function loadInvitations() {
            const tbody = document.getElementById('invites-tbody');
            if (!tbody) return;

            const searchInput = document.getElementById('search-invites');
            const filterStatus = document.getElementById('filter-status');
            const search = (searchInput?.value || '').trim();
            const status = filterStatus?.value || 'all';

            tbody.innerHTML = `
                <tr>
                    <td colspan="6" class="text-center py-16">
                        <div class="flex flex-col items-center gap-3">
                            <span class="spinner w-8 h-8 border-2 border-purple-500 border-t-transparent animate-spin rounded-full"></span>
                            <span class="text-xs text-slate-400 animate-pulse">${JG.esc(i18n.loading || 'Chargement...')}</span>
                        </div>
                    </td>
                </tr>
            `;

            try {
                const params = new URLSearchParams({
                    page: String(currentPage),
                    limit: String(itemsPerPage),
                    search: search,
                    status: status,
                });

                const res = await JG.api(`/admin/api/invitations?${params.toString()}`);
                if (!res.success) {
                    tbody.innerHTML = `<tr><td colspan="6" class="text-center py-8 text-rose-400 text-xs">${JG.esc(res.message || i18n.loadError || 'Erreur')}</td></tr>`;
                    return;
                }

                const items = Array.isArray(res.data) ? res.data : [];
                if (items.length === 0) {
                    tbody.innerHTML = `
                        <tr>
                            <td colspan="6" class="text-center py-16 text-slate-400">
                                <div class="flex flex-col items-center gap-2">
                                    <svg class="w-10 h-10 text-slate-600 mb-1" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 5v2m0 4v2m0 4v2M5 5a2 2 0 00-2 2v3a2 2 0 110 4v3a2 2 0 002 2h14a2 2 0 002-2v-3a2 2 0 110-4V7a2 2 0 00-2-2H5z" /></svg>
                                    <span class="text-sm font-semibold">${JG.esc(i18n.noActiveInvitations || 'Aucune invitation trouvée')}</span>
                                    <span class="text-xs text-slate-500">Créez votre premier lien d'invitation avec le bouton ci-dessus.</span>
                                </div>
                            </td>
                        </tr>
                    `;
                    updatePagination(0, 1, 1);
                    return;
                }

                const pg = res.pagination || {};
                totalPages = pg.pages || 1;
                updatePagination(pg.total || items.length, pg.page || 1, totalPages);

                tbody.innerHTML = items.map((inv) => renderInvitationRow(inv)).join('');
            } catch (err) {
                console.error('[Invitations] Load error:', err);
                tbody.innerHTML = `<tr><td colspan="6" class="text-center py-8 text-rose-400 text-xs">${JG.esc(i18n.loadError || 'Erreur')}</td></tr>`;
            }
        }

        function renderInvitationRow(inv) {
            const code = String(inv.code || '');
            const directLink = inv.invite_url || `${inviteBaseURL}/invite/${code}`;
            const authLink = inv.authentik_enrollment_url || '';
            const isAuth = !!(authLink && (inv.authentik_enabled !== false));
            const activeLink = isAuth ? authLink : directLink;

            // Uses badge
            const maxUses = Number(inv.max_uses || 0);
            const usedCount = Number(inv.used_count || 0);
            let usesHtml = '';
            if (maxUses === 0) {
                usesHtml = `<span class="px-2.5 py-1 rounded-full bg-cyan-500/10 text-cyan-300 border border-cyan-500/20 text-xs font-bold font-mono">${usedCount} / ∞</span>`;
            } else {
                const pct = Math.min(100, Math.round((usedCount / maxUses) * 100));
                const colorCls = pct >= 100 ? 'bg-rose-500 text-rose-200' : 'bg-purple-500 text-purple-200';
                usesHtml = `
                    <div class="space-y-1">
                        <div class="flex items-center justify-between text-xs font-mono font-bold">
                            <span class="${usedCount >= maxUses ? 'text-rose-400' : 'text-slate-200'}">${usedCount}/${maxUses}</span>
                            <span class="text-[10px] text-slate-500">${pct}%</span>
                        </div>
                        <div class="w-24 h-1.5 rounded-full bg-black/40 overflow-hidden">
                            <div class="h-full rounded-full ${colorCls}" style="width: ${pct}%"></div>
                        </div>
                    </div>
                `;
            }

            // Expiry status
            let expiryHtml = '';
            if (inv.expires_at) {
                const expDate = new Date(inv.expires_at);
                const isPast = expDate < new Date();
                const formatted = JG.formatDate ? JG.formatDate(inv.expires_at) : expDate.toLocaleDateString();
                if (isPast) {
                    expiryHtml = `<span class="text-xs text-rose-400 font-bold flex items-center gap-1"><svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>Expiré</span>`;
                } else {
                    expiryHtml = `<span class="text-xs text-slate-300 font-medium">${formatted}</span>`;
                }
            } else {
                expiryHtml = `<span class="text-xs text-emerald-400 font-bold">Permanent</span>`;
            }

            // Profile & temporary
            let profileHtml = `<div class="text-xs font-bold text-slate-200 truncate">${JG.esc(inv.profile_id || 'Politique globale')}</div>`;
            if (inv.is_temporary && inv.account_duration_days > 0) {
                profileHtml += `<div class="text-[11px] text-amber-400 font-semibold mt-0.5">Compte temp. (${inv.account_duration_days}j)</div>`;
            }

            // Sponsor
            const sponsorName = inv.created_by || '(système)';

            return `
                <tr class="hover:bg-white/[0.02] transition-colors group">
                    <td class="py-3.5 px-6">
                        <div class="flex items-center gap-2.5">
                            <button type="button" class="action-copy-link p-1.5 rounded-lg bg-white/5 hover:bg-purple-600/30 text-purple-300 transition-all active:scale-95" data-link="${encodeURIComponent(activeLink)}" title="Copier le lien direct">
                                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
                            </button>
                            <span class="font-mono text-xs font-bold text-purple-200 select-all">${JG.esc(code)}</span>
                            ${isAuth ? '<span class="text-[10px] px-1.5 py-0.5 rounded bg-purple-500/20 text-purple-300 border border-purple-500/30 font-bold">Authentik</span>' : ''}
                        </div>
                    </td>
                    <td class="py-3.5 px-4">${usesHtml}</td>
                    <td class="py-3.5 px-4">${expiryHtml}</td>
                    <td class="py-3.5 px-4">${profileHtml}</td>
                    <td class="py-3.5 px-4">
                        <span class="text-xs text-slate-300 font-semibold">${JG.esc(sponsorName)}</span>
                    </td>
                    <td class="py-3.5 px-6 text-right">
                        <div class="flex items-center justify-end gap-1.5">
                            <button type="button" class="action-qr-code p-2 rounded-xl text-slate-400 hover:text-white hover:bg-white/10 transition-colors" data-link="${encodeURIComponent(activeLink)}" title="Afficher le QR Code">
                                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v1m6 11h2m-6 0h-2v4m0-11v3m0 0h.01M12 12h4.01M16 20h4M4 12h4m12 0h.01M5 8h2a1 1 0 001-1V5a1 1 0 00-1-1H5a1 1 0 00-1 1v2a1 1 0 001 1zm14 0h2a1 1 0 001-1V5a1 1 0 00-1-1h-2a1 1 0 00-1 1v2a1 1 0 001 1zM5 20h2a1 1 0 001-1v-2a1 1 0 00-1-1H5a1 1 0 00-1 1v2a1 1 0 001 1z" /></svg>
                            </button>
                            <button type="button" class="action-delete-invite p-2 rounded-xl text-slate-400 hover:text-rose-400 hover:bg-rose-500/10 transition-colors" data-id="${inv.id}" data-code="${JG.esc(code)}" title="Supprimer l'invitation">
                                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                            </button>
                        </div>
                    </td>
                </tr>
            `;
        }

        async function loadSponsorStats() {
            try {
                const res = await JG.api('/admin/api/invitations/stats');
                if (res.success && res.data) {
                    const st = res.data;
                    const elTotal = document.getElementById('sponsor-total-links');
                    const elActive = document.getElementById('sponsor-active-links');
                    const elClosed = document.getElementById('sponsor-closed-links');
                    const elConversions = document.getElementById('sponsor-conversions');
                    const elRate = document.getElementById('sponsor-conversion-rate');

                    if (elTotal) elTotal.textContent = String(st.total_links || 0);
                    if (elActive) elActive.textContent = String(st.active_links || 0);
                    if (elClosed) elClosed.textContent = String(st.closed_links || 0);
                    if (elConversions) elConversions.textContent = String(st.conversions || 0);
                    if (elRate) elRate.textContent = `(${Number(st.conversion_rate || 0).toFixed(1)}%)`;
                }
            } catch (err) {
                console.warn('[Invitations] Stats error:', err);
            }
        }

        function updatePagination(total, page, pages) {
            currentPage = page;
            totalPages = pages;

            const prevBtn = document.getElementById('prev-page');
            const nextBtn = document.getElementById('next-page');
            const info = document.getElementById('pagination-info');
            const numbersContainer = document.getElementById('page-numbers');

            if (prevBtn) prevBtn.disabled = currentPage <= 1;
            if (nextBtn) nextBtn.disabled = currentPage >= totalPages;
            if (info) info.textContent = `Page ${currentPage} / ${totalPages} (${total} total)`;

            if (numbersContainer) {
                numbersContainer.innerHTML = '';
                for (let i = 1; i <= totalPages; i++) {
                    if (i === 1 || i === totalPages || (i >= currentPage - 1 && i <= currentPage + 1)) {
                        const btn = document.createElement('button');
                        btn.className = `w-8 h-8 flex items-center justify-center rounded-lg text-xs font-bold transition-all ${
                            i === currentPage ? 'bg-purple-600 text-white' : 'bg-white/5 text-slate-400 hover:text-white hover:bg-white/10'
                        }`;
                        btn.textContent = String(i);
                        btn.onclick = () => {
                            currentPage = i;
                            loadInvitations();
                        };
                        numbersContainer.appendChild(btn);
                    }
                }
            }
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

            // QR Code
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
            
            const usesSelect = document.getElementById('inv-uses-select');
            const usesInput = document.getElementById('inv-uses');
            if (usesSelect) {
                usesSelect.value = '1';
                if (usesInput) usesInput.classList.add('hidden');
            }

            const expirySelect = document.getElementById('inv-expiry-select');
            const expiryInput = document.getElementById('inv-expiry-days');
            if (expirySelect) {
                expirySelect.value = '7';
                if (expiryInput) expiryInput.classList.add('hidden');
            }

            const tempConfig = document.getElementById('temp-account-config');
            if (tempConfig) tempConfig.classList.add('hidden');

            applyInvitationPolicyUI();
            applySelectedInvitePreset();
            updateForcedUsernameState();
        }

        async function submitCreate(event) {
            event.preventDefault();
            const btn = document.getElementById('create-btn');
            if (!btn) return;
            btn.disabled = true;
            btn.innerHTML = '<span class="spinner w-4 h-4 border-2 border-white border-t-transparent animate-spin rounded-full inline-block"></span>';

            const maxUses = getEffectiveMaxUses();
            const expiresInDays = getEffectiveExpiryDays();
            const isTemporary = !!document.getElementById('inv-is-temporary')?.checked;
            const accountDurationDays = parseInt(document.getElementById('inv-account-duration-days')?.value || '30', 10) || 30;
            const forcedName = (document.getElementById('inv-forced-name')?.value || '').trim();
            const forcedUsername = (document.getElementById('inv-forced-user')?.value || '').trim();
            const email = (document.getElementById('inv-email')?.value || '').trim();
            const preferredLang = normalizeLangTag(document.getElementById('inv-preferred-lang')?.value || '');
            const policyPresetID = (document.getElementById('inv-policy-preset')?.value || '').trim();
            const emailMessage = (document.getElementById('inv-email-message')?.value || '').trim();

            if (!isAdmin && inviterMaxUses > 0 && (maxUses <= 0 || maxUses > inviterMaxUses)) {
                btn.disabled = false;
                btn.innerHTML = '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" /></svg><span>' + JG.esc(i18n.createLink || 'Créer le lien') + '</span>';
                JG.toast(fmt(i18n.invalidMaxUses, { n: inviterMaxUses }), 'error');
                return;
            }

            const data = {
                max_uses: maxUses,
                expires_in_days: expiresInDays,
                is_temporary: isTemporary,
                account_duration_days: isTemporary ? accountDurationDays : 0,
                forced_name: forcedName,
                forced_username: forcedUsername,
                send_to_email: email,
                preferred_lang: preferredLang,
                policy_preset_id: policyPresetID,
                email_message: emailMessage,
            };

            try {
                const res = await JG.api('/admin/api/invitations', { method: 'POST', body: JSON.stringify(data) });
                btn.disabled = false;
                btn.innerHTML = '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" /></svg><span>' + JG.esc(i18n.createLink || 'Créer le lien') + '</span>';

                if (res.success && res.data) {
                    showInvitationSuccessView(res.data);
                    loadInvitations();
                    loadSponsorStats();
                } else {
                    JG.toast(res.message || i18n.unknownError || 'Erreur lors de la création', 'error');
                }
            } catch (err) {
                btn.disabled = false;
                btn.innerHTML = '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" /></svg><span>' + JG.esc(i18n.createLink || 'Créer le lien') + '</span>';
                JG.toast('Erreur de communication avec le serveur', 'error');
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

        // --- Event Listeners Delegation ---
        document.body.addEventListener('click', (e) => {
            const closeTrigger = e.target.closest('[data-modal-close]');
            if (closeTrigger) {
                const modalId = closeTrigger.getAttribute('data-modal-close');
                if (modalId) {
                    JG.closeModal(modalId);
                }
                return;
            }

            const openCreateBtn = e.target.closest('.btn-open-create-modal');
            if (openCreateBtn) {
                resetCreateModalState();
                JG.openModal('create-modal');
                return;
            }

            const copyBtn = e.target.closest('.action-copy-link');
            if (copyBtn) {
                copyLinkToClipboard(decodeURIComponent(copyBtn.getAttribute('data-link')), copyBtn);
                return;
            }

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
                        a.download = `jellygate-qr.png`;
                        document.body.appendChild(a);
                        a.click();
                        document.body.removeChild(a);
                    };
                }
                JG.openModal('qr-modal');
                return;
            }

            const delBtn = e.target.closest('.action-delete-invite');
            if (delBtn) {
                pendingDeleteInvitationID = Number(delBtn.getAttribute('data-id'));
                const code = delBtn.getAttribute('data-code') || '';
                const txt = document.getElementById('delete-modal-text');
                if (txt) txt.textContent = `Voulez-vous vraiment supprimer l'invitation ${code} ? Les liens existants ne fonctionneront plus.`;
                JG.openModal('delete-modal');
                return;
            }
        });

        // Form selects toggles
        const usesSelect = document.getElementById('inv-uses-select');
        if (usesSelect) {
            usesSelect.addEventListener('change', () => {
                const customInput = document.getElementById('inv-uses');
                if (customInput) {
                    customInput.classList.toggle('hidden', usesSelect.value !== 'custom');
                    if (usesSelect.value !== 'custom') {
                        customInput.value = usesSelect.value;
                    }
                }
                updateForcedUsernameState();
            });
        }

        const customUsesInput = document.getElementById('inv-uses');
        if (customUsesInput) {
            customUsesInput.addEventListener('input', updateForcedUsernameState);
        }

        const expirySelect = document.getElementById('inv-expiry-select');
        if (expirySelect) {
            expirySelect.addEventListener('change', () => {
                const customInput = document.getElementById('inv-expiry-days');
                if (customInput) {
                    customInput.classList.toggle('hidden', expirySelect.value !== 'custom');
                    if (expirySelect.value !== 'custom') {
                        customInput.value = expirySelect.value;
                    }
                }
            });
        }

        const tempCheckbox = document.getElementById('inv-is-temporary');
        if (tempCheckbox) {
            tempCheckbox.addEventListener('change', () => {
                const tempConfig = document.getElementById('temp-account-config');
                if (tempConfig) tempConfig.classList.toggle('hidden', !tempCheckbox.checked);
            });
        }

        document.getElementById('inv-policy-preset')?.addEventListener('change', applySelectedInvitePreset);

        const createForm = document.getElementById('create-form');
        if (createForm) createForm.addEventListener('submit', submitCreate);

        document.getElementById('delete-confirm-btn')?.addEventListener('click', submitDelete);

        document.getElementById('btn-create-another')?.addEventListener('click', () => {
            resetCreateModalState();
        });

        document.getElementById('btn-finish-modal')?.addEventListener('click', () => {
            JG.closeModal('create-modal');
        });

        // Search & Filter
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

        const itemsPerPageSelect = document.getElementById('items-per-page');
        if (itemsPerPageSelect) {
            itemsPerPageSelect.addEventListener('change', () => {
                itemsPerPage = parseInt(itemsPerPageSelect.value, 10) || 25;
                currentPage = 1;
                loadInvitations();
            });
        }

        document.getElementById('prev-page')?.addEventListener('click', () => {
            if (currentPage > 1) {
                currentPage--;
                loadInvitations();
            }
        });

        document.getElementById('next-page')?.addEventListener('click', () => {
            if (currentPage < totalPages) {
                currentPage++;
                loadInvitations();
            }
        });

        // Authentik Sync Button
        const syncAuthBtn = document.getElementById('btn-sync-authentik');
        if (syncAuthBtn) {
            syncAuthBtn.addEventListener('click', async () => {
                const icon = document.getElementById('sync-authentik-icon');
                if (icon) icon.classList.add('animate-spin');
                syncAuthBtn.disabled = true;
                try {
                    const res = await JG.api('/admin/api/invitations/sync-authentik', { method: 'POST' });
                    if (res && res.success) {
                        JG.toast(res.message || 'Synchronisation Authentik réussie !', 'success');
                        loadInvitations();
                        loadSponsorStats();
                    } else {
                        JG.toast((res && (res.error || res.message)) || 'Erreur de synchronisation Authentik', 'error');
                    }
                } catch (err) {
                    console.error('[SyncAuthentik]', err);
                    JG.toast('Erreur de communication avec le serveur', 'error');
                } finally {
                    if (icon) icon.classList.remove('animate-spin');
                    syncAuthBtn.disabled = false;
                }
            });
        }

        // Security Panel Toggle & Form
        const toggleSecBtn = document.getElementById('btn-toggle-security-panel');
        if (toggleSecBtn) {
            toggleSecBtn.addEventListener('click', () => {
                const panel = document.getElementById('invite-security-panel');
                const chevron = document.getElementById('security-chevron');
                if (panel) {
                    const isHidden = panel.classList.toggle('hidden');
                    if (chevron) chevron.style.transform = isHidden ? 'rotate(0deg)' : 'rotate(180deg)';
                }
            });
        }

        const secForm = document.getElementById('invite-security-form');
        if (secForm) {
            // Load current security settings
            (async () => {
                try {
                    const res = await JG.api('/admin/api/invitations/security');
                    if (res && res.success && res.data) {
                        const sec = res.data;
                        const elEn = document.getElementById('invite-security-enabled');
                        const elCap = document.getElementById('invite-security-captcha');
                        const elMax = document.getElementById('invite-security-max-failures');
                        const elWin = document.getElementById('invite-security-window');
                        const elBlk = document.getElementById('invite-security-block');
                        if (elEn) elEn.checked = !!sec.enabled;
                        if (elCap) elCap.checked = !!sec.require_captcha_on_fail;
                        if (elMax) elMax.value = sec.max_failures_window || 5;
                        if (elWin) elWin.value = sec.window_minutes || 15;
                        if (elBlk) elBlk.value = sec.block_duration_minutes || 60;
                    }
                } catch (err) {
                    console.warn('[Invitations] Security load error:', err);
                }
            })();

            secForm.addEventListener('submit', async (e) => {
                e.preventDefault();
                const payload = {
                    enabled: !!document.getElementById('invite-security-enabled')?.checked,
                    require_captcha_on_fail: !!document.getElementById('invite-security-captcha')?.checked,
                    max_failures_window: parseInt(document.getElementById('invite-security-max-failures')?.value || '5', 10) || 5,
                    window_minutes: parseInt(document.getElementById('invite-security-window')?.value || '15', 10) || 15,
                    block_duration_minutes: parseInt(document.getElementById('invite-security-block')?.value || '60', 10) || 60,
                };
                try {
                    const res = await JG.api('/admin/api/invitations/security', { method: 'POST', body: JSON.stringify(payload) });
                    if (res && res.success) {
                        JG.toast(i18n.securitySaved || 'Sécurité des invitations enregistrée', 'success');
                    } else {
                        JG.toast(res.message || i18n.securitySaveFailed || 'Erreur lors de la sauvegarde', 'error');
                    }
                } catch (err) {
                    JG.toast('Erreur de communication avec le serveur', 'error');
                }
            });
        }

        // Initial loading
        loadInvitations();
        loadSponsorStats();
        loadInviteWizardData();
    });
})();
