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
        const defaults = {
            headers: { 'Content-Type': 'application/json' },
            credentials: 'same-origin',
        };
        const config = { ...defaults, ...opts };

        const resp = await fetch(url, config);

        if (resp.status === 401 || resp.status === 403) {
            window.location.href = '/admin/login';
            return { success: false, message: 'Session expirée' };
        }

        const data = await resp.json();
        return data;
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
 * @param {string} lang - 'fr' or 'en'
 */
JG.setLang = function (lang) {
    document.cookie = `lang=${lang};path=/;max-age=31536000;SameSite=Lax`;
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
    // Auto-focus first input in forms
    const firstInput = document.querySelector('form .jg-input');
    if (firstInput && !firstInput.value) {
        firstInput.focus();
    }
});
