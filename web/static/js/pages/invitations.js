(() => {
    const config = window.JGPageInvitations || {};
    const i18n = config.i18n || {};
    const uiLocale = config.uiLocale || undefined;
    const inviteBaseURL = (String(config.inviteBaseURL || '').trim() || window.location.origin);
    const isAdmin = !!config.isAdmin;
    const allowInviterGrant = !!config.allowInviterGrant;
    const allowInviterUserExpiry = !!config.allowInviterUserExpiry;
    const inviterMaxUses = Number(config.inviterMaxUses || 0);
    const inviterMaxLinkHours = Number(config.inviterMaxLinkHours || 0);
    const inviterQuotaDay = Number(config.inviterQuotaDay || 0);
    const inviterQuotaWeek = Number(config.inviterQuotaWeek || 0);
    const inviterQuotaMonth = Number(config.inviterQuotaMonth || 0);
    const defaultDisableAfterDays = Number(config.defaultDisableAfterDays || 0);

    document.addEventListener('DOMContentLoaded', () => {
        let allInvitations = [];
        let pendingDeleteInvitationID = 0;

        function fmt(template, vars) {
            return String(template || '').replace(/\{(\w+)\}/g, (_, key) => (vars && key in vars ? String(vars[key]) : ''));
        }

        function createBtnLabel() {
            return `<svg class="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" /></svg>${JG.esc(i18n.createLink)}`;
        }

        function getBaseURL() {
            return inviteBaseURL;
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
            const policyI18n = i18n.policy || {};
            
            forcedUserInput.disabled = !isAllowed;
            if (!isAllowed) forcedUserInput.value = '';
            
            if (forcedUserWrap) {
                forcedUserWrap.classList.toggle('opacity-40', !isAllowed);
                forcedUserWrap.classList.toggle('pointer-events-none', !isAllowed);
            }
            
            if (forcedUserHelp) {
                if (!isAllowed) {
                    forcedUserHelp.textContent = policyI18n.forced_username_limit_hint || "Le nom reserve n est disponible que pour les liens a usage unique (max = 1).";
                    forcedUserHelp.classList.add('text-amber-500');
                } else {
                    forcedUserHelp.textContent = i18n.forced_username_help || "";
                    forcedHelp.classList.remove('text-amber-500');
                }
            }
        }

        function applyInvitationPolicyUI() {
            const summary = document.getElementById('invite-policy-summary');
            const usesHelp = document.getElementById('inv-uses-help');
            const linkHelp = document.getElementById('inv-link-expiry-help');
            const canInviteWrap = document.getElementById('inv-can-invite-wrap');
            const canInviteHelp = document.getElementById('inv-can-invite-help');
            const canInviteCheckbox = document.getElementById('inv-new-user-can-invite');
            const expiryEnabled = document.getElementById('inv-user-expiry-enabled');
            const expiryDays = document.getElementById('inv-user-expiry-days');
            const expiryAt = document.getElementById('inv-user-expiry-at');

            const canGrantInvite = isAdmin || allowInviterGrant;
            const canSetUserExpiry = isAdmin || allowInviterUserExpiry;

            if (summary) {
                const parts = [];
                parts.push(fmt(i18n.baseLinks, { url: getBaseURL() }));
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
                linkHelp.textContent = (!isAdmin && inviterMaxLinkHours > 0)
                    ? fmt(i18n.linkHelpLimited, { n: inviterMaxLinkHours })
                    : i18n.linkHelpDefault;
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

            if (expiryEnabled && expiryDays) {
                expiryEnabled.disabled = !canSetUserExpiry;
                if (!canSetUserExpiry) {
                    expiryEnabled.checked = defaultDisableAfterDays > 0;
                    expiryDays.disabled = true;
                    expiryDays.value = defaultDisableAfterDays > 0 ? defaultDisableAfterDays : 30;
                    if (expiryAt) {
                        expiryAt.disabled = true;
                        expiryAt.value = '';
                    }
                } else {
                    expiryEnabled.checked = defaultDisableAfterDays > 0;
                    expiryDays.value = defaultDisableAfterDays > 0 ? defaultDisableAfterDays : 30;
                    expiryDays.disabled = !expiryEnabled.checked;
                    if (expiryAt) {
                        expiryAt.disabled = !expiryEnabled.checked;
                    }
                }
            }
        }

        async function loadInvitations() {
            const res = await JG.api('/admin/api/invitations');
            if (res.success) {
                allInvitations = res.data || [];
                renderInvitations(allInvitations);
            } else {
                JG.toast(i18n.loadError, 'error');
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
            document.getElementById('sponsor-total-links').textContent = String(data.total_links || 0);
            document.getElementById('sponsor-active-links').textContent = String(data.active_links || 0);
            document.getElementById('sponsor-closed-links').textContent = String(data.closed_links || 0);
            document.getElementById('sponsor-conversions').textContent = String(data.conversions || 0);
            document.getElementById('sponsor-conversion-rate').textContent = `${Number(data.conversion_rate || 0).toFixed(1)}%`;

            const generatedAt = document.getElementById('sponsor-stats-generated-at');
            if (generatedAt) {
                generatedAt.textContent = data.generated_at ? fmt(i18n.statsUpdatedAt, { at: new Date(data.generated_at).toLocaleString(uiLocale) }) : '--';
            }

            const sponsors = Array.isArray(data.by_sponsor) ? data.by_sponsor : [];
            if (!tbody) {
                return;
            }
            if (sponsors.length === 0) {
                tbody.innerHTML = `<tr><td colspan="7" class="text-center text-slate-500 py-8">${JG.esc(i18n.noSponsorData)}</td></tr>`;
                return;
            }

            tbody.innerHTML = sponsors.map((item) => {
                const rate = Number(item.conversion_rate || 0).toFixed(1);
                return `<tr>
                    <td class="font-medium text-slate-200">${JG.esc(item.sponsor || i18n.unknownSponsor)}</td>
                    <td>${JG.esc(String(item.created_links || 0))}</td>
                    <td><span class="badge badge-success">${JG.esc(String(item.active_links || 0))}</span></td>
                    <td><span class="badge badge-danger">${JG.esc(String(item.closed_links || 0))}</span></td>
                    <td>${JG.esc(String(item.total_uses || 0))}</td>
                    <td>${JG.esc(String(item.conversions || 0))}</td>
                    <td>${JG.esc(rate)}%</td>
                </tr>`;
            }).join('');
        }

        function renderInvitations(list) {
            const tbody = document.getElementById('invites-tbody');
            if (!tbody) {
                return;
            }
            if (list.length === 0) {
                tbody.innerHTML = `<tr><td colspan="6" class="text-center text-slate-500 py-12">${JG.esc(i18n.noActiveInvitations)}</td></tr>`;
                return;
            }

            tbody.innerHTML = list.map((invitation) => {
                const link = `${getBaseURL()}/invite/${invitation.code}`;
                const expDate = invitation.expires_at ? new Date(invitation.expires_at).toLocaleDateString(uiLocale) : '—';

                const rawProfile = invitation.jellyfin_profile || {};
                const disableAfter = Number(rawProfile.disable_after_days || rawProfile.user_expiry_days || 0);
                const absoluteUserExpiry = String(rawProfile.user_expires_at || '').trim();
                const deleteAfter = Number(rawProfile.delete_after_days || 0);
                const groupName = String(rawProfile.group_name || '').trim();
                let absoluteUserExpiryLabel = '';
                if (absoluteUserExpiry) {
                    const parsedAbs = new Date(absoluteUserExpiry);
                    absoluteUserExpiryLabel = Number.isNaN(parsedAbs.getTime()) ? absoluteUserExpiry : parsedAbs.toLocaleString(uiLocale);
                }
                const userExpiry = absoluteUserExpiry
                    ? fmt(i18n.expiresOn, { date: absoluteUserExpiryLabel })
                    : (disableAfter > 0 ? fmt(i18n.disableAfterDays, { n: disableAfter }) : i18n.unlimited);
                const deleteLabel = deleteAfter > 0 ? fmt(i18n.deleteAfterDays, { n: deleteAfter }) : i18n.noDeletePlanned;
                const inviteRole = rawProfile.can_invite ? i18n.roleCanInvite : i18n.roleStandard;
                const groupLabel = groupName ? fmt(i18n.groupPrefix, { group: groupName }) : i18n.groupDefault;
                const userExp = `<div>${JG.esc(userExpiry)} · ${JG.esc(inviteRole)}</div><div class="text-xs text-slate-500">${JG.esc(deleteLabel)} · ${JG.esc(groupLabel)}</div>`;

                const usesStr = invitation.max_uses > 0 ? `${invitation.used_count} / ${invitation.max_uses}` : `${invitation.used_count} / ${i18n.unlimited}`;
                const isOver = (invitation.max_uses > 0 && invitation.used_count >= invitation.max_uses) || (invitation.expires_at && new Date(invitation.expires_at) < new Date());
                const overBadge = isOver ? `<span class="ml-2 badge badge-danger">${JG.esc(i18n.badgeExpired)}</span>` : `<span class="ml-2 badge badge-success">${JG.esc(i18n.badgeActive)}</span>`;

                return `<tr class="${isOver ? 'opacity-50 grayscale' : ''}">
                    <td>
                        <div class="flex items-center gap-2">
                            <code class="px-2 py-1 bg-black/40 rounded text-purple-300 select-all">${invitation.code}</code>
                            <button class="text-slate-400 hover:text-white action-copy-link" data-link="${encodeURIComponent(link)}" title="${JG.esc(i18n.copyFullLinkTitle)}">
                                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
                            </button>
                        </div>
                    </td>
                    <td class="font-mono text-sm">${usesStr} ${overBadge}</td>
                    <td class="text-sm text-slate-400">${expDate}</td>
                    <td class="text-sm border-l border-white/5 pl-2">${userExp}</td>
                    <td class="text-sm text-slate-400">${JG.esc(i18n.emailManual)}</td>
                    <td class="text-right">
                        <button class="jg-btn jg-btn-sm jg-btn-danger action-delete-invite" data-id="${invitation.id}">
                            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg>
                        </button>
                    </td>
                </tr>`;
            }).join('');
        }

        function confirmDelete(id) {
            pendingDeleteInvitationID = id;
            JG.openModal('delete-modal');
        }

        function closeDeleteModal() {
            pendingDeleteInvitationID = 0;
            JG.closeModal('delete-modal');
        }

        function openCreateModal() {
            JG.openModal('create-modal');
            applyInvitationPolicyUI();
            updateForcedUsernameState();
        }

        function closeCreateModal() {
            JG.closeModal('create-modal');
            document.getElementById('create-form').reset();
            applyInvitationPolicyUI();
            updateForcedUsernameState();
        }

        async function submitCreate(event) {
            event.preventDefault();
            const btn = document.getElementById('create-btn');
            btn.disabled = true;
            btn.innerHTML = '<span class="spinner"></span>';

            const maxUses = parseInt(document.getElementById('inv-uses').value, 10) || 0;
            const userExpiryEnabled = !!document.getElementById('inv-user-expiry-enabled').checked;
            const userExpiryDays = parseInt(document.getElementById('inv-user-expiry-days').value, 10) || 0;
            const userExpiryAt = (document.getElementById('inv-user-expiry-at').value || '').trim();
            const grantInvite = !!document.getElementById('inv-new-user-can-invite').checked;

            const forcedUsername = document.getElementById('inv-forced-user').value;

            if (!isAdmin && inviterMaxUses > 0 && (maxUses <= 0 || maxUses > inviterMaxUses)) {
                btn.disabled = false;
                btn.innerHTML = createBtnLabel();
                JG.toast(fmt(i18n.invalidMaxUses, { n: inviterMaxUses }), 'error');
                return;
            }

            if (userExpiryEnabled && !userExpiryAt && userExpiryDays <= 0) {
                btn.disabled = false;
                btn.innerHTML = createBtnLabel();
                JG.toast(i18n.invalidUserExpiry, 'error');
                return;
            }

            if (forcedUsername && maxUses !== 1) {
                btn.disabled = false;
                btn.innerHTML = createBtnLabel();
                const errorText = i18n.policy?.forced_username_limit_error || "Le nom reserve necessite un lien a usage unique (max = 1).";
                JG.toast(errorText, 'error');
                return;
            }

            const data = {
                max_uses: maxUses,
                expires_at: (document.getElementById('inv-link-expiry').value || '').trim(),
                email: (document.getElementById('inv-email').value || '').trim(),
                forced_username: forcedUsername,
                jellyfin_profile: {
                    preset_id: parseInt(document.getElementById('inv-preset').value, 10) || 0,
                    group_name: (document.getElementById('inv-group').value || '').trim(),
                    can_invite: grantInvite,
                    user_expiry_days: userExpiryDays,
                    user_expires_at: userExpiryAt,
                    delete_after_days: parseInt(document.getElementById('inv-delete-after').value, 10) || 0
                }
            };

            const res = await JG.api('/admin/api/invitations', {
                method: 'POST',
                body: JSON.stringify(data)
            });
            btn.disabled = false;
            btn.innerHTML = createBtnLabel();

            if (res.success) {
                JG.toast(i18n.invitationCreated, 'success');
                closeCreateModal();
                loadInvitations();
                loadSponsorStats();
            } else {
                JG.toast(res.error || i18n.unknownError, 'error');
            }
        }

        async function submitDelete() {
            if (pendingDeleteInvitationID <= 0) return;
            const res = await JG.api(`/admin/api/invitations/${pendingDeleteInvitationID}`, {
                method: 'DELETE'
            });
            if (res.success) {
                JG.toast(i18n.invitationDeleted, 'success');
                closeDeleteModal();
                loadInvitations();
                loadSponsorStats();
            } else {
                JG.toast(res.error || i18n.unknownError, 'error');
            }
        }

        // --- Listeners (CSP Compliant) ---
        document.body.addEventListener('click', (e) => {
            const btnOpen = e.target.closest('.btn-open-create-modal');
            if (btnOpen) {
                openCreateModal();
                return;
            }

            const btnScroll = e.target.closest('.btn-scroll-invitations');
            if (btnScroll) {
                const target = document.getElementById('all-invitations-section');
                if (target) target.scrollIntoView({ behavior: 'smooth' });
                return;
            }

            const btnCloseModal = e.target.closest('[data-modal-close]');
            if (btnCloseModal) {
                const modalId = btnCloseModal.getAttribute('data-modal-close');
                if (modalId === 'create-modal') closeCreateModal();
                else if (modalId === 'delete-modal') closeDeleteModal();
                else JG.closeModal(modalId);
                return;
            }

            const btnCopy = e.target.closest('.action-copy-link');
            if (btnCopy) {
                const link = decodeURIComponent(btnCopy.getAttribute('data-link'));
                copyLinkToClipboard(link);
                return;
            }

            const btnDelete = e.target.closest('.action-delete-invite');
            if (btnDelete) {
                const id = parseInt(btnDelete.getAttribute('data-id'), 10);
                confirmDelete(id);
                return;
            }

            const btnConfirmDelete = e.target.closest('#confirm-delete-btn');
            if (btnConfirmDelete) {
                submitDelete();
                return;
            }
        });

        const createForm = document.getElementById('create-form');
        if (createForm) {
            createForm.addEventListener('submit', submitCreate);
        }

        const maxUsesInput = document.getElementById('inv-uses');
        const forcedUserInput = document.getElementById('inv-forced-user');

        if (maxUsesInput) {
            maxUsesInput.addEventListener('input', updateForcedUsernameState);
            maxUsesInput.addEventListener('change', updateForcedUsernameState);
        }

        if (forcedUserInput) {
            forcedUserInput.addEventListener('input', () => {
                const maxUses = parseInt(maxUsesInput?.value, 10);
                if (maxUses !== 1 && forcedUserInput.value.length > 0) {
                    const errorText = i18n.policy?.forced_username_limit_error || "Le nom reserve necessite un lien a usage unique (max = 1).";
                    JG.showNotification(errorText, 'error');
                    forcedUserInput.value = '';
                }
            });
        }

        const expiryEnabled = document.getElementById('inv-user-expiry-enabled');
        if (expiryEnabled) {
            expiryEnabled.addEventListener('change', () => {
                const days = document.getElementById('inv-user-expiry-days');
                const at = document.getElementById('inv-user-expiry-at');
                if (days) days.disabled = !expiryEnabled.checked;
                if (at) at.disabled = !expiryEnabled.checked;
            });
        }

        loadInvitations();
        loadSponsorStats();
    });
})();