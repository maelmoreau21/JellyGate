/**
 * JellyGate — app.js
 * Vanilla JS utilities for the admin dashboard and public pages.
 *
 * Global namespace: JG
 * Provides: api(), toast(), esc(), closeModal()
 */

window.JG = window.JG || {};

// ── API helper ──────────────────────────────────────────────────────────────

/**
 * Fetch wrapper for JSON API calls.
 * @param {string} url - API endpoint
 * @param {object} opts - fetch options (method, body, etc.)
 * @returns {Promise<object>} Parsed JSON response
 */
JG.api = async function (url, opts = {}) {
    try {
        const isFormData = (typeof FormData !== 'undefined') && (opts.body instanceof FormData);

        const mergedHeaders = {
            ...(opts.headers || {}),
        };
        if (!isFormData && !mergedHeaders['Content-Type']) {
            mergedHeaders['Content-Type'] = 'application/json';
        }

        const config = {
            credentials: 'same-origin',
            ...opts,
            headers: mergedHeaders,
        };

        const resp = await fetch(url, config);

        if (resp.status === 401 || resp.status === 403) {
            window.location.href = '/admin/login';
            return { success: false, message: 'Session expirée' };
        }

        const contentType = (resp.headers.get('content-type') || '').toLowerCase();
        const finalURL = (resp.url || '').toLowerCase();

        if (resp.redirected && finalURL.includes('/admin/login')) {
            window.location.href = '/admin/login';
            return { success: false, message: 'Session expirée' };
        }

        if (!contentType.includes('application/json')) {
            const payloadText = await resp.text();
            return {
                success: false,
                message: resp.ok ? 'Réponse inattendue du serveur' : `Erreur HTTP ${resp.status}`,
                status: resp.status,
                raw: payloadText,
            };
        }

        const payloadText = await resp.text();
        try {
            return JSON.parse(payloadText);
        } catch {
            return {
                success: false,
                message: 'Réponse JSON invalide du serveur',
                status: resp.status,
                raw: payloadText,
            };
        }
    } catch (err) {
        console.error('[JG.api]', url, err);
        return { success: false, message: 'Erreur réseau' };
    }
};

// ── Toast notifications ─────────────────────────────────────────────────────

/**
 * Show a toast notification.
 * @param {string} message - Text to display
 * @param {string} type - 'success' | 'error' | 'info'
 * @param {number} duration - Auto-dismiss in ms (default 4000)
 */
JG.toast = function (message, type = 'info', duration = 4000) {
    const container = document.getElementById('toast-container');
    if (!container) return;

    const el = document.createElement('div');
    el.className = `toast toast-${type}`;
    el.textContent = message;
    container.appendChild(el);

    setTimeout(() => {
        el.style.animation = 'toastOut 0.3s ease-in forwards';
        setTimeout(() => el.remove(), 300);
    }, duration);
};

// ── HTML escaping ───────────────────────────────────────────────────────────

/**
 * Escape HTML special characters to prevent XSS.
 * @param {string} str - Raw string
 * @returns {string} Escaped string
 */
JG.esc = function (str) {
    if (!str) return '';
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
};

// ── Clipboard helper ───────────────────────────────────────────────────────

/**
 * Copy plain text with modern API + legacy fallback.
 * @param {string} text - Text to copy.
 * @returns {Promise<boolean>} True if copy likely succeeded.
 */
JG.copyText = async function (text) {
    const value = String(text || '');
    if (!value) return false;

    try {
        if (navigator.clipboard && window.isSecureContext) {
            await navigator.clipboard.writeText(value);
            return true;
        }
    } catch {
        // Fall back to textarea/execCommand below.
    }

    try {
        const ta = document.createElement('textarea');
        ta.value = value;
        ta.setAttribute('readonly', '');
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        ta.style.pointerEvents = 'none';
        document.body.appendChild(ta);
        ta.focus();
        ta.select();
        ta.setSelectionRange(0, value.length);
        const ok = document.execCommand('copy');
        document.body.removeChild(ta);
        if (ok) return true;
    } catch {
        // Last resort prompt below.
    }

    try {
        window.prompt('Copie manuelle (Ctrl+C):', value);
        return false;
    } catch {
        return false;
    }
};

// ── Modal helpers ───────────────────────────────────────────────────────────

/**
 * Close the currently open modal.
 */
JG.closeModal = function () {
    const modal = document.getElementById('delete-modal');
    if (modal) modal.style.display = 'none';
};

// ── Language switcher ───────────────────────────────────────────────────────

/**
 * Set the language cookie and reload.
 * @param {string} lang - Supported language code
 */
JG.setLang = function (lang) {
    const raw = String(lang || '').trim().toLowerCase().replace(/_/g, '-');
    if (!raw) return;

    const normalized = raw === 'pt' ? 'pt-br' : raw;
    document.cookie = `lang=${normalized};path=/;max-age=31536000;SameSite=Lax`;
    window.location.reload();
};

// ── Keyboard shortcuts ──────────────────────────────────────────────────────

document.addEventListener('keydown', (e) => {
    // Escape closes modals
    if (e.key === 'Escape') {
        JG.closeModal();
    }
});

// ── Auto-hide flash messages ────────────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
    const langSwitcher = document.getElementById('lang-switcher');
    if (langSwitcher) {
        const sidebarFooter = document.querySelector('.jg-sidebar .jg-sidebar-footer');
        if (sidebarFooter) {
            langSwitcher.classList.add('jg-lang-switcher-sidebar');
            sidebarFooter.appendChild(langSwitcher);
        } else {
            langSwitcher.style.display = 'none';
        }
    }

    // Auto-focus first input in forms
    const firstInput = document.querySelector('form .jg-input');
    if (firstInput && !firstInput.value) {
        firstInput.focus();
    }
});
