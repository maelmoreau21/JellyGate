(() => {
    const config = window.JGPageMyAccount || {};
    const uiLocale = config.uiLocale || undefined;
    const languageLabels = config.languageLabels || {};
    const i18n = config.i18n || {};

    function updateEmailVerification(profile) {
        const statusEl = document.getElementById('email-verification-status');
        const helpEl = document.getElementById('email-verification-help');
        const pendingEl = document.getElementById('email-pending-value');
        const resendBtn = document.getElementById('email-verification-resend');
        if (!statusEl || !helpEl || !pendingEl || !resendBtn) {
            return;
        }

        const email = String(profile.email || '').trim();
        const pending = String(profile.pending_email || '').trim();
        const verified = !!profile.email_verified && !pending;

        if (!email && !pending) {
            statusEl.textContent = i18n.emailStatusMissing || 'Missing';
            helpEl.textContent = i18n.emailVerificationMissing || '';
            pendingEl.classList.add('hidden');
            pendingEl.textContent = '';
            resendBtn.disabled = true;
            resendBtn.classList.add('opacity-50', 'cursor-not-allowed');
            return;
        }

        resendBtn.disabled = verified;
        resendBtn.classList.toggle('opacity-50', verified);
        resendBtn.classList.toggle('cursor-not-allowed', verified);

        if (pending) {
            statusEl.textContent = i18n.emailStatusPending || 'Pending';
            helpEl.textContent = i18n.emailVerificationPending || '';
            pendingEl.textContent = `${i18n.emailPendingLabel || ''} ${pending}`.trim();
            pendingEl.classList.remove('hidden');
            return;
        }

        pendingEl.classList.add('hidden');
        pendingEl.textContent = '';

        if (verified) {
            statusEl.textContent = i18n.emailStatusVerified || 'Verified';
            helpEl.textContent = i18n.emailVerificationOk || '';
            return;
        }

        statusEl.textContent = i18n.emailStatusUnverified || 'Unverified';
        helpEl.textContent = i18n.emailVerificationHelp || '';
    }

    function formatDateTime(value, createdAt) {
        if (!value) {
            return i18n.noExpiry || 'No expiry';
        }
        const date = new Date(value);
        if (Number.isNaN(date.getTime())) {
            return value;
        }
        
        // Update remaining time badge and meter
        const now = new Date();
        const start = createdAt ? new Date(createdAt) : now;
        const totalMs = date.getTime() - start.getTime();
        const diffMs = date.getTime() - now.getTime();
        
        const remainingEl = document.getElementById('account-remaining');
        const meterWrap = document.getElementById('account-expiry-meter-wrap');
        const meterEl = document.getElementById('account-expiry-meter');

        if (remainingEl) {
            if (diffMs > 0) {
                const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
                const diffHours = Math.floor((diffMs % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60));
                
                let text = '';
                if (diffDays > 0) {
                    text = `${diffDays}j ${diffHours}h restantes`;
                } else {
                    text = `${diffHours}h restantes`;
                }
                
                remainingEl.textContent = text;
                remainingEl.classList.remove('hidden');
                
                if (meterWrap && meterEl && totalMs > 0) {
                    const percent = Math.max(0, Math.min(100, (diffMs / totalMs) * 100));
                    meterEl.style.width = `${percent}%`;
                    meterWrap.classList.remove('hidden');
                }

                // Alert colors
                if (diffDays < 2) {
                    remainingEl.classList.add('bg-rose-500/10', 'text-rose-500');
                    remainingEl.classList.remove('bg-jg-accent/10', 'text-jg-accent');
                    if (meterEl) meterEl.classList.add('!bg-rose-500');
                } else {
                    remainingEl.classList.remove('bg-rose-500/10', 'text-rose-500');
                    remainingEl.classList.add('bg-jg-accent/10', 'text-jg-accent');
                    if (meterEl) meterEl.classList.remove('!bg-rose-500');
                }
            } else {
                remainingEl.classList.add('hidden');
                if (meterWrap) meterWrap.classList.add('hidden');
            }
        }

        return date.toLocaleString(uiLocale);
    }

    async function loadMyAccount() {
        const res = await JG.api('/admin/api/users/me');
        if (!res.success) {
            JG.toast(res.message || i18n.loadError || 'Load failed', 'error');
            return;
        }

        const profile = res.data || {};
        const username = profile.username || '-';
        document.getElementById('account-username').textContent = username;
        document.getElementById('account-initial').textContent = username.charAt(0).toUpperCase();

        if (profile.id) {
            const avatarUrl = `/admin/api/users/${profile.id}/avatar?t=${Date.now()}`;
            const avatarImg = document.getElementById('account-avatar-img');
            const initial = document.getElementById('account-initial');
            
            if (avatarImg) {
                avatarImg.src = avatarUrl;
                avatarImg.onload = () => {
                    avatarImg.classList.remove('hidden');
                    if (initial) initial.classList.add('hidden');
                };
                avatarImg.onerror = () => {
                    avatarImg.classList.add('hidden');
                    if (initial) initial.classList.remove('hidden');
                };
            }
        }

        document.getElementById('account-role').textContent = profile.is_admin ? (i18n.roleAdmin || 'Admin') : (i18n.roleUser || 'User');
        document.getElementById('account-expiry').textContent = formatDateTime(profile.access_expires_at, profile.created_at);
        document.getElementById('account-email-summary').textContent = profile.email || '-';
        document.getElementById('account-language').textContent = languageLabels[String(profile.preferred_lang || '').toLowerCase()] || languageLabels[''] || '';

        document.getElementById('my-email').value = profile.email || '';
        document.getElementById('my-discord').value = profile.contact_discord || '';
        document.getElementById('my-telegram').value = profile.contact_telegram || '';
        document.getElementById('my-lang').value = profile.preferred_lang || '';
        document.getElementById('my-notify-expiry').checked = profile.notify_expiry_reminder !== false;
        document.getElementById('my-notify-events').checked = profile.notify_account_events !== false;
        document.getElementById('my-opt-email').checked = profile.opt_in_email !== false;
        document.getElementById('my-opt-discord').checked = !!profile.opt_in_discord;
        document.getElementById('my-notify-events').checked = profile.notify_account_events !== false;
        
        const optEmail = document.getElementById('my-opt-email');
        if (optEmail) optEmail.checked = profile.opt_in_email !== false;

        updateEmailVerification(profile);
        loadSponsorships();
    }

    async function loadSponsorships() {
        const tbody = document.getElementById('sponsorship-tbody');
        if (!tbody) return;

        try {
            const res = await JG.api('/admin/api/users/me/invitations');
            if (!res.success) return;

            const list = res.data || [];
            if (list.length === 0) {
                tbody.innerHTML = `<tr><td colspan="4" class="px-4 py-8 text-center text-jg-text-muted opacity-40 italic text-sm">Aucun lien généré</td></tr>`;
                return;
            }

            tbody.innerHTML = list.map(inv => {
                const inviteUrl = `${window.location.origin}/invite/${inv.code}`;
                return `
                <tr class="hover:bg-white/[0.04] transition-colors group">
                    <td class="px-6 py-4">
                        <div class="flex items-center gap-3">
                            <span class="font-mono text-xs text-jg-accent font-bold tracking-wider">${inv.code}</span>
                            <button class="btn-copy-link opacity-0 group-hover:opacity-100 p-1.5 hover:bg-jg-accent/10 rounded-lg text-jg-accent transition-all" data-url="${inviteUrl}">
                                <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" /></svg>
                            </button>
                        </div>
                    </td>
                    <td class="px-6 py-4">
                        <div class="flex items-center gap-2">
                            <span class="text-sm text-jg-text font-medium">${inv.used_count}</span>
                            <span class="text-[10px] text-jg-text-muted">/ ${inv.max_uses || '∞'}</span>
                        </div>
                    </td>
                    <td class="px-6 py-4 text-xs text-jg-text-muted">
                        ${inv.expires_at ? new Date(inv.expires_at).toLocaleDateString(uiLocale, { day: '2-digit', month: 'short' }) : '<span class="opacity-30">∞</span>'}
                    </td>
                    <td class="px-6 py-4 text-right">
                        <button class="btn-delete-sponsor jg-btn jg-btn-ghost jg-btn-danger w-9 h-9 p-0 flex items-center justify-center rounded-xl hover:bg-rose-500/10" data-code="${inv.code}">
                            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                        </button>
                    </td>
                </tr>
            `;}).join('');

            tbody.querySelectorAll('.btn-copy-link').forEach(btn => {
                btn.onclick = () => {
                    const url = btn.dataset.url;
                    navigator.clipboard.writeText(url).then(() => {
                        JG.toast('Lien copié !', 'success');
                    });
                };
            });

            tbody.querySelectorAll('.btn-delete-sponsor').forEach(btn => {
                btn.onclick = async () => {
                    const code = btn.dataset.code;
                    if (!confirm('Supprimer ce lien d\'invitation ?')) return;
                    const delRes = await JG.api(`/admin/api/invitations/${code}`, { method: 'DELETE' });
                    if (delRes.success) {
                        JG.toast('Lien supprimé', 'success');
                        loadSponsorships();
                    }
                };
            });

        } catch (err) {
            console.error(err);
        }
    }

    async function createSponsorship() {
        const res = await JG.api('/admin/api/users/me/invitations', { method: 'POST' });
        if (res.success) {
            JG.toast('Lien de parrainage généré', 'success');
            loadSponsorships();
            return;
        }
        JG.toast(res.message || 'Erreur lors de la génération', 'error');
    }

    async function handleAvatarUpload(event) {
        const file = event.target.files[0];
        if (!file) return;

        const formData = new FormData();
        formData.append('image', file);

        try {
            const res = await fetch('/admin/api/users/me/avatar', {
                method: 'POST',
                body: formData,
                headers: {
                   'X-CSRF-Token': document.querySelector('meta[name="csrf-token"]')?.content || ""
                }
            });
            const data = await res.json();
            if (data.success) {
                JG.toast('Photo de profil mise à jour', 'success');
                loadMyAccount();
            } else {
                JG.toast(data.message || 'Erreur lors de l\'upload', 'error');
            }
        } catch (err) {
            JG.toast('Erreur réseau', 'error');
        }
    }

    async function saveMyAccount(event) {
        event.preventDefault();
        const payload = {
            email: document.getElementById('my-email').value.trim(),
            contact_discord: document.getElementById('my-discord').value.trim(),
            contact_telegram: document.getElementById('my-telegram').value.trim(),
            preferred_lang: document.getElementById('my-lang').value,
            notify_expiry_reminder: document.getElementById('my-notify-expiry').checked,
            notify_account_events: document.getElementById('my-notify-events').checked,
            opt_in_email: document.getElementById('my-opt-email').checked,
            opt_in_discord: document.getElementById('my-opt-discord').checked,
            opt_in_telegram: document.getElementById('my-opt-telegram').checked,
        };

        const res = await JG.api('/admin/api/users/me', {
            method: 'PATCH',
            body: JSON.stringify(payload),
        });

        if (res.success) {
            JG.toast(res.message || i18n.saved || 'Saved', 'success');
            const preferred = String(payload.preferred_lang || '').trim().toLowerCase();
            if (preferred) {
                document.cookie = `lang=${preferred};path=/;max-age=31536000;SameSite=Lax`;
            } else {
                document.cookie = 'lang=;path=/;max-age=0;SameSite=Lax';
            }
            window.location.reload();
            return;
        }
        JG.toast(res.message || i18n.saveError || 'Save failed', 'error');
    }

    async function updateMyPassword(event) {
        event.preventDefault();

        const password = document.getElementById('my-password').value;
        const confirmPassword = document.getElementById('my-password-confirm').value;
        if (password.length < 8) {
            JG.toast(i18n.passwordTooShort || 'Password too short', 'error');
            return;
        }
        if (password !== confirmPassword) {
            JG.toast(i18n.passwordMismatch || 'Password mismatch', 'error');
            return;
        }

        const res = await JG.api('/admin/api/users/me/password', {
            method: 'POST',
            body: JSON.stringify({
                current_password: 'not_needed_by_admin_token',
                new_password: password,
            }),
        });

        if (res.success) {
            JG.toast(res.message || i18n.passwordUpdated || 'Password updated', 'success');
            document.getElementById('my-password-form').reset();
            return;
        }
        JG.toast(res.message || i18n.passwordUpdateError || 'Password update failed', 'error');
    }

    async function resendEmailVerification() {
        const btn = document.getElementById('email-verification-resend');
        if (!btn || btn.disabled) {
            return;
        }
        btn.disabled = true;
        let restoreButton = true;

        try {
            const res = await JG.api('/admin/api/users/me/email-verification/resend', {
                method: 'POST',
            });
            if (res.success) {
                JG.toast(res.message || i18n.emailVerificationSent || 'Verification sent', 'success');
                await loadMyAccount();
                restoreButton = false;
                return;
            }
            JG.toast(res.message || i18n.emailVerificationSendError || 'Send failed', 'error');
        } finally {
            if (restoreButton) {
                btn.disabled = false;
            }
        }
    }

    document.addEventListener('DOMContentLoaded', async () => {
        document.querySelectorAll('[data-scroll-target]').forEach((btn) => {
            btn.addEventListener('click', () => {
                const target = document.getElementById(btn.dataset.scrollTarget || '');
                if (target) {
                    target.scrollIntoView({ behavior: 'smooth', block: 'start' });
                }
            });
        });

        const toggle = document.getElementById('sidebar-toggle');
        if (toggle) {
            toggle.addEventListener('click', () => {
                const sidebar = document.getElementById('sidebar');
                if (sidebar) {
                    sidebar.classList.toggle('open');
                }
            });
        }

        document.getElementById('my-password-form')?.addEventListener('submit', updateMyPassword);
        document.getElementById('email-verification-resend')?.addEventListener('click', resendEmailVerification);
        document.getElementById('create-sponsor-link-btn')?.addEventListener('click', createSponsorship);
        document.getElementById('avatar-upload')?.addEventListener('change', handleAvatarUpload);
        await loadMyAccount();
    });
})();