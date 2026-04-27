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
    const inviterQuotaWeek = Number(config.inviterQuotaWeek || 0);
    const inviterQuotaMonth = Number(config.inviterQuotaMonth || 0);
    const limitUserExpiryDays = Number(config.limitUserExpiryDays || 0);
    const defaultDisableAfterDays = Number(config.defaultDisableAfterDays || 0);
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

        function fmt(template, vars) {
            return String(template || '').replace(/\{(\w+)\}/g, (_, key) => (vars && key in vars ? String(vars[key]) : ''));
        }

        function createBtnLabel() {
            return `<svg class="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" /></svg>${JG.esc(i18n.createLink)}`;
        }

        async function copyLinkToClipboard(link) {
            const ok = await JG.copyText(link);
            if (ok) {
                JG.toast(i18n.linkCopied, 'success');
            } else {
                JG.toast(i18n.copyUnavailable, 'error');
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
                    forcedUserHelp.textContent = i18n.forcedUsernameLimitHint || '';
                    forcedUserHelp.classList.add('text-amber-500');
                } else {
                    forcedUserHelp.textContent = i18n.forcedUsernameHelp || '';
                    forcedUserHelp.classList.remove('text-amber-500');
                }
            }
        }

        function applyInvitationPolicyUI() {
            const summary = document.getElementById('invite-policy-summary');
            const usesHelp = document.getElementById('inv-uses-help');
            const linkHelp = document.getElementById('inv-link-expiry-help');
            const linkDaysInput = document.getElementById('inv-expiry-days');
            const ignoreLinkWrap = document.getElementById('inv-ignore-link-limit-wrap');
            const ignoreLinkInput = document.getElementById('inv-ignore-link-limit');

            const canInviteWrap = document.getElementById('inv-can-invite-wrap');
            const canInviteHelp = document.getElementById('inv-can-invite-help');
            const canInviteCheckbox = document.getElementById('inv-new-user-can-invite');

            const expiryEnabled = document.getElementById('inv-user-expiry-enabled');
            const expiryDays = document.getElementById('inv-user-expiry-days');
            const ignoreUserWrap = document.getElementById('inv-ignore-user-expiry-limit-wrap');
            const ignoreUserInput = document.getElementById('inv-ignore-user-expiry-limit');

            const effectiveUserExpiryDays = limitUserExpiryDays > 0
                ? limitUserExpiryDays
                : (defaultDisableAfterDays > 0 ? defaultDisableAfterDays : 0);

            const canGrantInvite = isAdmin || allowInviterGrant;
            const canSetUserExpiry = isAdmin || allowInviterUserExpiry;

            if (summary) {
                const parts = [];
                parts.push(fmt(i18n.baseLinks, { url: inviteBaseURL }));
                if (!isAdmin && inviterMaxUses > 0) parts.push(fmt(i18n.maxUsesPerLink, { n: inviterMaxUses }));
                if (!isAdmin && inviterMaxLinkHours > 0) parts.push(fmt(i18n.maxTtl, { n: inviterMaxLinkHours }));
                if (!isAdmin && inviterQuotaDay > 0) parts.push(fmt(i18n.quotaDay, { n: inviterQuotaDay }));
                if (!isAdmin && inviterQuotaWeek > 0) parts.push(fmt(i18n.quotaWeek, { n: inviterQuotaWeek }));
                if (!isAdmin && inviterQuotaMonth > 0) parts.push(fmt(i18n.quotaMonth, { n: inviterQuotaMonth }));
                if (!isAdmin && !allowInviterGrant) parts.push(i18n.grantLocked);
                if (!isAdmin && !allowInviterUserExpiry) parts.push(i18n.expiryLocked);
                summary.textContent = parts.join(' • ');
            }

            if (usesHelp) {
                usesHelp.textContent = (!isAdmin && inviterMaxUses > 0)
                    ? fmt(i18n.usesHelpLimited, { n: inviterMaxUses })
                    : i18n.usesHelpDefault;
            }

            if (linkHelp) {
                if (!isAdmin && limitLinkValidityDays > 0) {
                    linkHelp.textContent = fmt(i18n.linkHelpLimited, { n: inviterMaxLinkHours || (limitLinkValidityDays * 24) });
                } else {
                    linkHelp.textContent = i18n.linkHelpDefault;
                }
            }

            if (linkDaysInput) {
                if (!isAdmin && limitLinkValidityDays > 0) {
                    linkDaysInput.value = String(limitLinkValidityDays);
                } else {
                    linkDaysInput.value = '0';
                }
            }

            if (ignoreLinkWrap && ignoreLinkInput) {
                if (limitLinkValidityDays > 0) {
                    ignoreLinkWrap.classList.remove('hidden');
                    ignoreLinkWrap.classList.add('flex');
                    ignoreLinkInput.checked = false;
                    ignoreLinkInput.disabled = !allowIgnoreLimits;
                    ignoreLinkWrap.classList.toggle('opacity-60', !allowIgnoreLimits);
                } else {
                    ignoreLinkWrap.classList.remove('flex');
                    ignoreLinkWrap.classList.add('hidden');
                    ignoreLinkInput.checked = false;
                }
            }

            if (canInviteCheckbox) {
                canInviteCheckbox.checked = false;
                canInviteCheckbox.disabled = !canGrantInvite;
            }
            if (canInviteWrap && !canGrantInvite) {
                canInviteWrap.classList.add('opacity-60');
            }
            if (canInviteWrap && canGrantInvite) {
                canInviteWrap.classList.remove('opacity-60');
            }
            if (canInviteHelp) {
                canInviteHelp.textContent = canGrantInvite ? i18n.inviteEnabledHelp : i18n.invitePolicyLimited;
            }

            if (ignoreUserWrap && ignoreUserInput) {
                if (effectiveUserExpiryDays > 0) {
                    ignoreUserWrap.classList.remove('hidden');
                    ignoreUserWrap.classList.add('flex');
                    ignoreUserInput.checked = false;
                    ignoreUserInput.disabled = !allowIgnoreLimits;
                    ignoreUserWrap.classList.toggle('opacity-60', !allowIgnoreLimits);
                } else {
                    ignoreUserWrap.classList.remove('flex');
                    ignoreUserWrap.classList.add('hidden');
                    ignoreUserInput.checked = false;
                }
            }

            if (expiryEnabled && expiryDays) {
                const fallbackDays = effectiveUserExpiryDays > 0 ? effectiveUserExpiryDays : 30;
                expiryDays.value = String(fallbackDays);

                if (!canSetUserExpiry) {
                    expiryEnabled.checked = effectiveUserExpiryDays > 0;
                    expiryEnabled.disabled = true;
                    expiryDays.disabled = true;
                } else {
                    expiryEnabled.disabled = false;
                    expiryEnabled.checked = effectiveUserExpiryDays > 0;
                    expiryDays.disabled = !expiryEnabled.checked;
                }
            }

            const preferredLangInput = document.getElementById('inv-preferred-lang');
            if (preferredLangInput) {
                const resolved = defaultLang || 'fr';
                preferredLangInput.value = preferredLangInput.querySelector(`option[value="${resolved}"]`) ? resolved : '';
            }

            updateForcedUsernameState();
        }

        async function loadInvitations() {
            const res = await JG.api(`/admin/api/invitations?page=${currentPage}&limit=${itemsPerPage}`);
            if (res.success && res.data) {
                const invitations = res.data.invitations || [];
                const meta = res.data.meta || {};
                
                totalPages = meta.total_pages || 1;
                currentPage = meta.page || 1;
                
                renderInvitations(invitations);
                renderPagination(meta);
            } else {
                JG.toast(i18n.loadError || 'Loading error', 'error');
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
                    const activeClass = i === meta.page ? 'bg-jg-accent text-white shadow-lg shadow-jg-accent/20' : 'bg-jg-bg-secondary text-jg-text-muted hover:text-jg-text border border-jg-border';
                    html += `<button class="w-8 h-8 flex items-center justify-center rounded-lg font-bold text-xs transition-all page-btn" data-page="${i}">${i}</button>`.replace('class="', `class="${activeClass} `);
                }
                pageNumbers.innerHTML = html;
            }
        }

        async function loadSponsorStats() {
            const res = await JG.api('/admin/api/invitations/stats');
            const tbody = document.getElementById('sponsor-stats-body');
            if (!res || !res.success || !res.data) {
                if (tbody) {
                    tbody.innerHTML = `<tr><td colspan="7" class="text-center text-red-300 py-8">${JG.esc(i18n.statsLoadError)}</td></tr>`;
                }
                return;
            }

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

            const generatedAt = document.getElementById('sponsor-stats-generated-at');
            if (generatedAt && stats.generated_at) {
                generatedAt.textContent = fmt(i18n.statsUpdatedAt, { at: new Date(stats.generated_at).toLocaleString(uiLocale) });
            }

            const sponsors = Array.isArray(stats.by_sponsor) ? stats.by_sponsor : [];
            if (!tbody) return;
            
            if (sponsors.length === 0) {
                tbody.innerHTML = `<tr><td colspan="7" class="text-center text-jg-text-muted py-8">${JG.esc(i18n.noSponsorData)}</td></tr>`;
                return;
            }

            tbody.innerHTML = sponsors.map((item) => {
                return `<tr>
                    <td class="px-6 py-4 font-medium text-jg-text">${JG.esc(item.sponsor || i18n.unknownSponsor)}</td>
                    <td class="px-6 py-4">${JG.esc(String(item.created_links || 0))}</td>
                    <td class="px-6 py-4"><span class="px-2 py-0.5 rounded-full bg-emerald-500/10 text-emerald-400 text-[10px] font-black uppercase">${JG.esc(String(item.active_links || 0))}</span></td>
                    <td class="px-6 py-4"><span class="px-2 py-0.5 rounded-full bg-rose-500/10 text-rose-400 text-[10px] font-black uppercase">${JG.esc(String(item.closed_links || 0))}</span></td>
                    <td class="px-6 py-4">${JG.esc(String(item.total_uses || 0))}</td>
                    <td class="px-6 py-4">${JG.esc(String(item.conversions || 0))}</td>
                    <td class="px-6 py-4 font-bold text-jg-accent">${JG.esc((item.conversion_rate || 0).toFixed(1))}%</td>
                </tr>`;
            }).join('');
        }

        function renderInvitations(list) {
            const tbody = document.getElementById('invites-tbody');
            if (!tbody) return;
            
            if (list.length === 0) {
                tbody.innerHTML = `<tr><td colspan="6" class="text-center text-jg-text-muted py-12 font-medium">${JG.esc(i18n.noActiveInvitations)}</td></tr>`;
                return;
            }

            tbody.innerHTML = list.map((invitation) => {
                const link = `${inviteBaseURL}/invite/${invitation.code}`;
                const expDate = invitation.expires_at ? new Date(invitation.expires_at).toLocaleDateString(uiLocale) : '—';
                const profile = invitation.jellyfin_profile || {};
                
                const expiryLabel = profile.user_expires_at 
                    ? fmt(i18n.expiresOn, { date: new Date(profile.user_expires_at).toLocaleString(uiLocale) })
                    : (profile.user_expiry_days > 0 ? fmt(i18n.disableAfterDays, { n: profile.user_expiry_days }) : i18n.unlimited);
                
                const deleteLabel = profile.delete_after_days > 0 ? fmt(i18n.deleteAfterDays, { n: profile.delete_after_days }) : i18n.noDeletePlanned;
                const roleLabel = profile.can_invite ? i18n.roleCanInvite : i18n.roleStandard;
                const groupLabel = profile.group_name ? fmt(i18n.groupPrefix, { group: profile.group_name }) : i18n.groupDefault;
                const inviteLang = normalizeLangTag(invitation.preferred_lang || '') || defaultLang;
                
                const isOver = (invitation.max_uses > 0 && invitation.used_count >= invitation.max_uses) || (invitation.expires_at && new Date(invitation.expires_at) < new Date());
                const badge = isOver 
                    ? `<span class="ml-2 px-2 py-0.5 rounded-full bg-rose-500/10 text-rose-400 text-[10px] font-black uppercase">${JG.esc(i18n.badgeExpired)}</span>` 
                    : `<span class="ml-2 px-2 py-0.5 rounded-full bg-emerald-500/10 text-emerald-400 text-[10px] font-black uppercase">${JG.esc(i18n.badgeActive)}</span>`;

                return `<tr class="${isOver ? 'opacity-40' : 'hover:bg-white/[0.02] transition-colors'}">
                    <td class="px-6 py-4">
                        <div class="flex items-center gap-3">
                            <code class="px-2.5 py-1.5 bg-jg-bg-secondary border border-jg-border rounded-lg text-jg-accent font-black text-xs tracking-wider select-all shadow-inner">${invitation.code}</code>
                            <button class="p-2 rounded-lg bg-jg-bg-secondary border border-jg-border text-jg-text-muted hover:text-jg-text transition-all action-copy-link" data-link="${encodeURIComponent(link)}" title="${JG.esc(i18n.copyFullLinkTitle)}">
                                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
                            </button>
                        </div>
                    </td>
                    <td class="px-6 py-4 font-black text-jg-text">${invitation.used_count} / ${invitation.max_uses > 0 ? invitation.max_uses : '∞'} ${badge}</td>
                    <td class="px-6 py-4 text-xs font-bold text-jg-text-muted uppercase tracking-widest">${expDate}</td>
                    <td class="px-6 py-4 border-l border-jg-border">
                        <div class="font-bold text-jg-text">${JG.esc(expiryLabel)} · <span class="text-jg-accent">${JG.esc(roleLabel)}</span></div>
                        <div class="text-[10px] text-jg-text-muted uppercase tracking-wider mt-1">${JG.esc(deleteLabel)} | ${JG.esc(groupLabel)} | LANG: ${JG.esc(String(inviteLang).toUpperCase())}</div>
                    </td>
                    <td class="px-6 py-4 text-xs font-bold text-jg-text-muted uppercase tracking-widest">${JG.esc(invitation.created_by || 'System')}</td>
                    <td class="px-6 py-4 text-right">
                        <button class="p-2 rounded-lg bg-rose-500/10 text-rose-500 hover:bg-rose-500 hover:text-white transition-all shadow-lg shadow-rose-500/10 action-delete-invite" data-id="${invitation.id}">
                            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg>
                        </button>
                    </td>
                </tr>`;
            }).join('');
        }

        async function submitCreate(event) {
            event.preventDefault();
            const btn = document.getElementById('create-btn');
            btn.disabled = true;
            btn.innerHTML = '<span class="spinner"></span>';

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

            const maxUses = parseInt(maxUsesInput?.value || '0', 10) || 0;
            let expiresInDays = parseInt(expiryDaysInput?.value || '0', 10) || 0;
            const userExpiryEnabled = !!userExpiryEnabledInput?.checked;
            let userExpiryDays = parseInt(userExpiryDaysInput?.value || '0', 10) || 0;
            const grantInvite = !!canInviteInput?.checked;
            const forcedUsername = (forcedUserInput?.value || '').trim();
            const ignorePresetLinkExpiry = !!(ignoreLinkInput && !ignoreLinkInput.disabled && ignoreLinkInput.checked);
            const ignorePresetUserExpiry = !!(ignoreUserInput && !ignoreUserInput.disabled && ignoreUserInput.checked);
            const preferredLang = normalizeLangTag(preferredLangInput?.value || '');

            if (!isAdmin && maxUses <= 0) {
                btn.disabled = false;
                btn.innerHTML = createBtnLabel();
                JG.toast(i18n.invalidMaxUses, 'error');
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
                JG.toast(i18n.invalidUserExpiry, 'error');
                return;
            }

            if (!ignorePresetUserExpiry && limitUserExpiryDays > 0) {
                if (!userExpiryEnabled) {
                    userExpiryDays = limitUserExpiryDays;
                }
                if (!isAdmin && userExpiryDays > limitUserExpiryDays) {
                    btn.disabled = false;
                    btn.innerHTML = createBtnLabel();
                    JG.toast(i18n.expiryLocked, 'error');
                    return;
                }
            }

            const data = {
                max_uses: maxUses,
                expires_in_days: expiresInDays,
                ignore_preset_link_expiry: ignorePresetLinkExpiry,
                apply_user_expiry: userExpiryEnabled,
                user_expiry_days: userExpiryEnabled ? userExpiryDays : 0,
                ignore_preset_user_expiry: ignorePresetUserExpiry,
                new_user_can_invite: grantInvite,
                forced_username: forcedUsername,
                send_to_email: (emailInput?.value || '').trim(),
                preferred_lang: preferredLang,
            };

            const res = await JG.api('/admin/api/invitations', { method: 'POST', body: JSON.stringify(data) });
            btn.disabled = false;
            btn.innerHTML = createBtnLabel();

            if (res.success) {
                JG.toast(i18n.created, 'success');
                JG.closeModal('create-modal');
                document.getElementById('create-form')?.reset();
                loadInvitations();
                loadSponsorStats();
            } else {
                JG.toast(res.message || i18n.unknownError, 'error');
            }
        }

        async function submitDelete() {
            const res = await JG.api(`/admin/api/invitations/${pendingDeleteInvitationID}`, { method: 'DELETE' });
            if (res.success) {
                JG.toast(i18n.deleted, 'success');
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

            const copyBtn = e.target.closest('.action-copy-link');
            if (copyBtn) {
                copyLinkToClipboard(decodeURIComponent(copyBtn.getAttribute('data-link')));
                return;
            }

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

            if (e.target.closest('.btn-open-create-modal')) {
                document.getElementById('create-form')?.reset();
                applyInvitationPolicyUI();
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

        const createForm = document.getElementById('create-form');
        if (createForm) createForm.addEventListener('submit', submitCreate);

        const maxUsesInput = document.getElementById('inv-uses');
        if (maxUsesInput) maxUsesInput.addEventListener('input', updateForcedUsernameState);

        const ignoreLinkInput = document.getElementById('inv-ignore-link-limit');
        if (ignoreLinkInput) {
            ignoreLinkInput.addEventListener('change', () => {
                const linkDaysInput = document.getElementById('inv-expiry-days');
                if (!linkDaysInput) return;
                if (!ignoreLinkInput.checked && limitLinkValidityDays > 0 && (!isAdmin || !allowIgnoreLimits)) {
                    linkDaysInput.value = String(limitLinkValidityDays);
                }
            });
        }

        const expiryEnabled = document.getElementById('inv-user-expiry-enabled');
        if (expiryEnabled) {
            expiryEnabled.addEventListener('change', () => {
                const days = document.getElementById('inv-user-expiry-days');
                if (days) days.disabled = !expiryEnabled.checked;
            });
        }

        loadInvitations();
        loadSponsorStats();
    });
})();
