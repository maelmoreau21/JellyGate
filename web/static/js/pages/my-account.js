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

    function formatDateTime(value) {
        if (!value) {
            return i18n.noExpiry || 'No expiry';
        }
        const date = new Date(value);
        if (Number.isNaN(date.getTime())) {
            return value;
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
        document.getElementById('account-username').textContent = profile.username || '-';
        document.getElementById('account-role').textContent = profile.is_admin ? (i18n.roleAdmin || 'Admin') : (i18n.roleUser || 'User');
        document.getElementById('account-expiry').textContent = formatDateTime(profile.access_expires_at);
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
        document.getElementById('my-opt-telegram').checked = !!profile.opt_in_telegram;
        updateEmailVerification(profile);
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

        document.getElementById('my-account-form')?.addEventListener('submit', saveMyAccount);
        document.getElementById('my-password-form')?.addEventListener('submit', updateMyPassword);
        document.getElementById('email-verification-resend')?.addEventListener('click', resendEmailVerification);
        await loadMyAccount();
    });
})();