(() => {
    const config = window.JGPageInvite || {};
    const t = config.i18n || {};

    (function initInviteForm() {
        const form = document.getElementById('invite-form');
        if (!form) {
            return;
        }

        const password = document.getElementById('password');
        const confirmPassword = document.getElementById('password_confirm');
        const matchMessage = document.getElementById('password-match-msg');
        const policyMessage = document.getElementById('password-policy-msg');
        const usernameMsg = document.getElementById('username-msg');
        const emailMsg = document.getElementById('email-msg');
        const inputs = form.querySelectorAll('.jg-input');
        const policy = {
            usernameMinLength: parseInt(form.dataset.usernameMinLength || '3', 10) || 3,
            usernameMaxLength: parseInt(form.dataset.usernameMaxLength || '32', 10) || 32,
            requireEmail: (form.dataset.requireEmail || '').toLowerCase() === 'true',
            minLength: parseInt(form.dataset.passwordMinLength || '8', 10) || 8,
            maxLength: parseInt(form.dataset.passwordMaxLength || '128', 10) || 128,
            requireUpper: (form.dataset.passwordRequireUpper || '').toLowerCase() === 'true',
            requireLower: (form.dataset.passwordRequireLower || '').toLowerCase() === 'true',
            requireDigit: (form.dataset.passwordRequireDigit || '').toLowerCase() === 'true',
            requireSpecial: (form.dataset.passwordRequireSpecial || '').toLowerCase() === 'true',
        };

        function withCount(template, n) {
            return String(template || '').replace('{n}', String(n));
        }

        function passwordPolicyErrors(value) {
            const errors = [];
            if (value.length < policy.minLength) {
                errors.push(withCount(t.password_rule_at_least || 'at least {n} characters', policy.minLength));
            }
            if (value.length > policy.maxLength) {
                errors.push(withCount(t.password_rule_at_most || 'at most {n} characters', policy.maxLength));
            }
            if (policy.requireUpper && !/[A-Z]/.test(value)) {
                errors.push(t.password_rule_upper || 'one uppercase letter');
            }
            if (policy.requireLower && !/[a-z]/.test(value)) {
                errors.push(t.password_rule_lower || 'one lowercase letter');
            }
            if (policy.requireDigit && !/[0-9]/.test(value)) {
                errors.push(t.password_rule_digit || 'one digit');
            }
            if (policy.requireSpecial && !/[^A-Za-z0-9]/.test(value)) {
                errors.push(t.password_rule_special || 'one special character');
            }
            return errors;
        }

        function validateUsername(input) {
            if (!input) return true;
            const rawVal = input.value || '';
            const value = rawVal.trim();
            if (!value) {
                input.classList.remove('is-valid', 'is-invalid');
                if (usernameMsg) {
                    usernameMsg.className = 'text-xs mt-1 text-slate-500 hidden';
                    usernameMsg.textContent = '';
                }
                return false;
            }
            if (/\s/.test(rawVal)) {
                input.classList.remove('is-valid');
                input.classList.add('is-invalid');
                if (usernameMsg) {
                    usernameMsg.className = 'text-xs mt-1 text-red-400';
                    usernameMsg.textContent = 'Les espaces ne sont pas autorisés dans le nom d\'utilisateur';
                }
                return false;
            }
            if (value.length < policy.usernameMinLength) {
                input.classList.remove('is-valid');
                input.classList.add('is-invalid');
                if (usernameMsg) {
                    usernameMsg.className = 'text-xs mt-1 text-red-400';
                    usernameMsg.textContent = `Doit contenir au moins ${policy.usernameMinLength} caractères`;
                }
                return false;
            }
            if (value.length > policy.usernameMaxLength) {
                input.classList.remove('is-valid');
                input.classList.add('is-invalid');
                if (usernameMsg) {
                    usernameMsg.className = 'text-xs mt-1 text-red-400';
                    usernameMsg.textContent = `Doit contenir au maximum ${policy.usernameMaxLength} caractères`;
                }
                return false;
            }
            input.classList.remove('is-invalid');
            input.classList.add('is-valid');
            if (usernameMsg) {
                usernameMsg.className = 'text-xs mt-1 text-emerald-400';
                usernameMsg.textContent = '✓ Nom d\'utilisateur valide';
            }
            return true;
        }

        function validateEmail(input) {
            if (!input) return true;
            const value = (input.value || '').trim();
            const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
            if (!value) {
                if (policy.requireEmail) {
                    input.classList.remove('is-valid');
                    input.classList.add('is-invalid');
                    if (emailMsg) {
                        emailMsg.className = 'text-xs mt-1 text-red-400';
                        emailMsg.textContent = 'L\'adresse email est obligatoire';
                    }
                    return false;
                }
                input.classList.remove('is-valid', 'is-invalid');
                if (emailMsg) {
                    emailMsg.className = 'text-xs mt-1 text-slate-500 hidden';
                    emailMsg.textContent = '';
                }
                return true;
            }
            if (!emailRegex.test(value)) {
                input.classList.remove('is-valid');
                input.classList.add('is-invalid');
                if (emailMsg) {
                    emailMsg.className = 'text-xs mt-1 text-red-400';
                    emailMsg.textContent = 'Format d\'adresse email invalide (ex: nom@domaine.com)';
                }
                return false;
            }
            input.classList.remove('is-invalid');
            input.classList.add('is-valid');
            if (emailMsg) {
                emailMsg.className = 'text-xs mt-1 text-emerald-400';
                emailMsg.textContent = '✓ Adresse email valide';
            }
            return true;
        }

        inputs.forEach((input) => {
            input.addEventListener('input', () => {
                if (input.id === 'password') {
                    const errors = passwordPolicyErrors(input.value || '');
                    if (!input.value) {
                        input.classList.remove('is-valid', 'is-invalid');
                        if (policyMessage) {
                            policyMessage.className = 'text-xs mt-1 text-slate-500';
                            policyMessage.textContent = '';
                        }
                    } else if (errors.length === 0) {
                        input.classList.remove('is-invalid');
                        input.classList.add('is-valid');
                        if (policyMessage) {
                            policyMessage.textContent = `✓ ${t.password_policy_ok || 'Password rules satisfied'}`;
                            policyMessage.className = 'text-xs mt-1 text-emerald-400';
                        }
                    } else {
                        input.classList.remove('is-valid');
                        input.classList.add('is-invalid');
                        if (policyMessage) {
                            policyMessage.textContent = `${t.password_policy_missing || 'Missing requirements'}: ${errors.join(', ')}`;
                            policyMessage.className = 'text-xs mt-1 text-red-400';
                        }
                    }
                    return;
                }

                if (input.id === 'username') {
                    validateUsername(input);
                    return;
                }

                if (input.id === 'email') {
                    validateEmail(input);
                    return;
                }

                if (input.validity.valid && input.value) {
                    input.classList.remove('is-invalid');
                    input.classList.add('is-valid');
                } else if (input.value) {
                    input.classList.remove('is-valid');
                    input.classList.add('is-invalid');
                } else {
                    input.classList.remove('is-valid', 'is-invalid');
                }
            });
        });

        function checkMatch() {
            if (!password || !confirmPassword || !matchMessage) {
                return;
            }
            if (!confirmPassword.value) {
                matchMessage.classList.add('hidden');
                return;
            }
            matchMessage.classList.remove('hidden');
            if (password.value === confirmPassword.value) {
                matchMessage.textContent = `✓ ${t.password_match || 'Passwords match'}`;
                matchMessage.className = 'text-xs mt-1 text-emerald-400';
                confirmPassword.classList.remove('is-invalid');
                confirmPassword.classList.add('is-valid');
            } else {
                matchMessage.textContent = `✗ ${t.password_mismatch || 'Passwords do not match'}`;
                matchMessage.className = 'text-xs mt-1 text-red-400';
                confirmPassword.classList.remove('is-valid');
                confirmPassword.classList.add('is-invalid');
            }
        }

        if (password && confirmPassword) {
            password.addEventListener('input', checkMatch);
            confirmPassword.addEventListener('input', checkMatch);
        }

        form.addEventListener('submit', (event) => {
            const usernameEl = document.getElementById('username');
            if (usernameEl && !usernameEl.readOnly) {
                if (!validateUsername(usernameEl)) {
                    event.preventDefault();
                    usernameEl.focus();
                    return;
                }
            }

            const emailEl = document.getElementById('email');
            if (emailEl) {
                if (!validateEmail(emailEl)) {
                    event.preventDefault();
                    emailEl.focus();
                    return;
                }
            }

            if (password && confirmPassword) {
                const passwordErrors = passwordPolicyErrors(password.value || '');
                if (passwordErrors.length > 0 || password.value !== confirmPassword.value) {
                    event.preventDefault();
                    if (passwordErrors.length > 0 && policyMessage) {
                        policyMessage.textContent = `${t.password_policy_missing || 'Missing requirements'}: ${passwordErrors.join(', ')}`;
                        policyMessage.className = 'text-xs mt-1 text-red-400';
                        password.focus();
                    } else if (password.value !== confirmPassword.value) {
                        confirmPassword.focus();
                    }
                    return;
                }
            }

            const submitButton = document.getElementById('submit-btn');
            if (submitButton) {
                submitButton.disabled = true;
                submitButton.innerHTML = `<span class="spinner"></span> ${t.submitting || 'Submitting'}`;
            }
        });
    })();
})();
