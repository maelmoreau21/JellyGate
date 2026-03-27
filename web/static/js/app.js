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
        const csrfToken = JG.getCookie('jg_csrf');
        if (csrfToken && !mergedHeaders['X-CSRF-Token']) {
            mergedHeaders['X-CSRF-Token'] = csrfToken;
        }
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

JG.getCookie = function (name) {
    const key = String(name || '').trim();
    if (!key) return '';

    const escaped = key.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const match = document.cookie.match(new RegExp('(?:^|; )' + escaped + '=([^;]*)'));
    return match ? decodeURIComponent(match[1]) : '';
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
    window.location.assign(window.location.pathname + window.location.search + window.location.hash);
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
        const sidebar = document.querySelector('.jg-sidebar');
        const sidebarFooter = document.querySelector('.jg-sidebar .jg-sidebar-footer');
        if (sidebar) {
            document.body.classList.add('jg-has-sidebar');
        }
        if (sidebarFooter) {
            langSwitcher.classList.add('jg-lang-switcher-sidebar');
            langSwitcher.classList.remove('jg-lang-switcher-floating');
            sidebarFooter.appendChild(langSwitcher);
        }
    }

    const langSelect = document.getElementById('lang-select');
    if (langSelect) {
        langSelect.addEventListener('change', (event) => {
            JG.setLang(event.target && event.target.value);
        });
    }

    // Auto-focus first input in forms
    const firstInput = document.querySelector('form .jg-input');
    if (firstInput && !firstInput.value) {
        firstInput.focus();
    }
    
    // Theme setup
    JG.setupThemeToggle();
});

// ── Theme toggle ────────────────────────────────────────────────────────────
JG.setupThemeToggle = function () {
    const btn = document.getElementById('theme-toggle-btn');
    if (!btn) return;

    function applyTheme(theme) {
        const iconContainer = btn.querySelector('#theme-toggle-icon') || btn;
        const isSidebar = btn.classList.contains('jg-theme-btn');

        if (theme === 'dark') {
            document.documentElement.classList.add('dark');
            iconContainer.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" /></svg>`;
            if (iconContainer === btn && !isSidebar) btn.innerHTML += `<span class="jg-nav-label">Theme</span>`; 
            btn.title = "Passer au thème clair";
        } else {
            document.documentElement.classList.remove('dark');
            iconContainer.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" /></svg>`;
            if (iconContainer === btn && !isSidebar) btn.innerHTML += `<span class="jg-nav-label">Theme</span>`;
            btn.title = "Passer au thème sombre";
        }
    }

    // Default to dark unless explicitly set otherwise
    const savedTheme = localStorage.getItem('jg-theme') || 'dark';
    applyTheme(savedTheme);

    btn.addEventListener('click', () => {
        const isDark = document.documentElement.classList.contains('dark');
        const newTheme = isDark ? 'light' : 'dark';
        localStorage.setItem('jg-theme', newTheme);
        applyTheme(newTheme);
    });
};
