(() => {
    const config = window.JGPageReset || {};

    (function initResetPasswordMatch() {
        const password = document.getElementById('password');
        const confirmPassword = document.getElementById('password_confirm');
        const message = document.getElementById('password-match-msg');
        if (!password || !confirmPassword || !message) {
            return;
        }

        function checkMatch() {
            if (!confirmPassword.value) {
                message.classList.add('hidden');
                return;
            }
            message.classList.remove('hidden');
            if (password.value === confirmPassword.value) {
                message.textContent = `✓ ${config.passwordMatch || 'Passwords match'}`;
                message.className = 'text-xs mt-1 text-emerald-400';
                confirmPassword.classList.remove('is-invalid');
                confirmPassword.classList.add('is-valid');
            } else {
                message.textContent = `✗ ${config.passwordMismatch || 'Passwords do not match'}`;
                message.className = 'text-xs mt-1 text-red-400';
                confirmPassword.classList.remove('is-valid');
                confirmPassword.classList.add('is-invalid');
            }
        }

        password.addEventListener('input', checkMatch);
        confirmPassword.addEventListener('input', checkMatch);
    })();
})();