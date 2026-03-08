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
            document.getElementById('delete-modal').style.display = '';
        }

        function closeDeleteModal() {
            pendingDeleteInvitationID = 0;
            document.getElementById('delete-modal').style.display = 'none';
        }

        function openCreateModal() {
            document.getElementById('create-modal').style.display = '';
            applyInvitationPolicyUI();
        }

        function closeCreateModal() {
            document.getElementById('create-modal').style.display = 'none';
            document.getElementById('create-form').reset();
            applyInvitationPolicyUI();
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

            if (userExpiryEnabled && userExpiryAt) {
                const expiryTimestamp = Date.parse(userExpiryAt);
                if (Number.isNaN(expiryTimestamp) || expiryTimestamp <= Date.now()) {
                    btn.disabled = false;
                    btn.innerHTML = createBtnLabel();
                    JG.toast(i18n.userExpiryFuture, 'error');
                    return;
                }
            }

            const payload = {
                label: '',
                max_uses: maxUses,
                expires_at: document.getElementById('inv-expires-link').value || '',
                apply_user_expiry: userExpiryEnabled,
                disable_after_days: userExpiryEnabled ? userExpiryDays : 0,
                user_expiry_days: userExpiryEnabled ? userExpiryDays : 0,
                user_expires_at: userExpiryEnabled ? userExpiryAt : '',
                new_user_can_invite: grantInvite,
                send_to_email: document.getElementById('inv-email').value,
                email_message: '',
                group_name: '',
                forced_username: document.getElementById('inv-forced-user').value,
                libraries: [],
            };

            const res = await JG.api('/admin/api/invitations', {
                method: 'POST',
                body: JSON.stringify(payload),
            });

            btn.disabled = false;
            btn.innerHTML = createBtnLabel();

            if (res.success) {
                const createdLink = (res.data && res.data.url) ? res.data.url : '';
                if (createdLink) {
                    await copyLinkToClipboard(createdLink);
                }
                JG.toast(i18n.created, 'success');
                closeCreateModal();
                loadInvitations();
                loadSponsorStats();
            } else {
                JG.toast(res.message || i18n.unknownError, 'error');
            }
        }

        const userExpiryToggle = document.getElementById('inv-user-expiry-enabled');
        const userExpiryDaysField = document.getElementById('inv-user-expiry-days');
        const userExpiryAtField = document.getElementById('inv-user-expiry-at');
        if (userExpiryToggle && userExpiryDaysField) {
            userExpiryToggle.addEventListener('change', () => {
                userExpiryDaysField.disabled = !userExpiryToggle.checked;
                if (userExpiryAtField) {
                    userExpiryAtField.disabled = !userExpiryToggle.checked;
                }
            });
        }

        document.querySelectorAll('.modal-overlay').forEach((overlay) => {
            overlay.addEventListener('click', (event) => {
                if (event.target === overlay) {
                    closeCreateModal();
                    closeDeleteModal();
                }
            });
        });

        document.getElementById('btn-open-create-modal')?.addEventListener('click', openCreateModal);
        document.getElementById('btn-close-create-modal-top')?.addEventListener('click', closeCreateModal);
        document.getElementById('btn-close-create-modal-bottom')?.addEventListener('click', closeCreateModal);
        document.getElementById('btn-close-delete-modal')?.addEventListener('click', closeDeleteModal);

        document.getElementById('btn-scroll-invitations')?.addEventListener('click', () => {
            const target = document.getElementById('invites-tbody');
            if (target) {
                target.scrollIntoView({ behavior: 'smooth', block: 'start' });
            }
        });

        document.getElementById('create-form')?.addEventListener('submit', submitCreate);

        document.getElementById('delete-confirm-btn')?.addEventListener('click', async () => {
            if (!pendingDeleteInvitationID) {
                return;
            }
            const res = await JG.api(`/admin/api/invitations/${pendingDeleteInvitationID}`, { method: 'DELETE' });
            if (res.success) {
                JG.toast(i18n.deleted, 'success');
                closeDeleteModal();
                loadInvitations();
                loadSponsorStats();
            }
        });

        document.getElementById('invites-tbody')?.addEventListener('click', async (event) => {
            const copyButton = event.target.closest('.action-copy-link');
            if (copyButton) {
                await copyLinkToClipboard(decodeURIComponent(copyButton.dataset.link || ''));
                return;
            }

            const deleteButton = event.target.closest('.action-delete-invite');
            if (deleteButton) {
                confirmDelete(Number(deleteButton.dataset.id || '0'));
            }
        });

        const toggle = document.getElementById('sidebar-toggle');
        if (toggle) {
            toggle.addEventListener('click', () => {
                const sidebar = document.getElementById('sidebar');
                if (sidebar) sidebar.classList.toggle('open');
            });
        }

        loadInvitations();
        loadSponsorStats();
        applyInvitationPolicyUI();
    });
})();