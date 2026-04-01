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
 * Close a specific modal or the one currently open.
 * @param {string} id - optional ID of the modal to close
 */
JG.closeModal = function (id) {
    if (id) {
        const modal = document.getElementById(id);
        if (modal) {
            modal.classList.remove('open', 'show');
            modal.classList.add('hidden');
            modal.style.display = 'none';
        }
    } else {
        // Fallback for legacy calls or any open modal
        document.querySelectorAll('.modal-overlay').forEach(m => {
            m.classList.remove('open', 'show');
            m.classList.add('hidden');
            m.style.display = 'none';
        });
    }
};

/**
 * Open a specific modal.
 * @param {string} id - ID of the modal to open
 */
JG.openModal = function (id) {
    const modal = document.getElementById(id);
    if (modal) {
        // Important: Remove Tailwinds 'hidden' if present, then add 'open'
        modal.classList.remove('hidden');
        modal.classList.add('open', 'show');
        modal.style.display = 'flex';
    }
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

// ── Auto-hide flash messages & UI Setup ─────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
    // 1. Sidebar Toggle (Modern)
    const mobileMenuBtn = document.getElementById('mobile-menu-toggle');
    const sidebar = document.getElementById('sidebar');
    const backdrop = document.getElementById('sidebar-backdrop');

    if (mobileMenuBtn && sidebar && backdrop) {
        const toggleSidebar = () => {
            sidebar.classList.toggle('open');
            backdrop.classList.toggle('hidden');
            document.body.classList.toggle('overflow-hidden');
        };

        mobileMenuBtn.addEventListener('click', toggleSidebar);
        backdrop.addEventListener('click', toggleSidebar);

        // Close on navigation (if same page anchor)
        sidebar.querySelectorAll('a').forEach(link => {
            link.addEventListener('click', () => {
                if (window.innerWidth < 1024) toggleSidebar();
            });
        });
    }

    // 2. Language switcher placement
    const langSwitcher = document.getElementById('lang-switcher');
    const langTarget = document.getElementById('sidebar-lang-target');
    
    if (langSwitcher) {
        if (langTarget && !document.body.classList.contains('login-page')) {
            langSwitcher.classList.add('jg-lang-switcher-sidebar');
            langSwitcher.classList.remove('jg-lang-switcher-floating');
            langTarget.appendChild(langSwitcher);
        }
    }

    // 2. Custom Select Logic (Universal for Language Switcher & others)
    document.querySelectorAll('.jg-custom-select').forEach(container => {
        const trigger = container.querySelector('.jg-select-trigger');
        const optionsContainer = container.querySelector('.jg-select-options');
        const currentFlag = container.querySelector('.jg-current-flag');
        const currentLabel = container.querySelector('.jg-current-label');

        if (!trigger || !optionsContainer) return;

        // Sync initial state
        const activeOption = optionsContainer.querySelector('.jg-select-option.active');
        if (activeOption) {
            const img = activeOption.querySelector('img');
            const label = activeOption.querySelector('span');
            if (img && currentFlag) currentFlag.src = img.src;
            if (label && currentLabel) currentLabel.textContent = label.textContent;
        }

        // Toggle dropdown
        trigger.addEventListener('click', (e) => {
            e.stopPropagation();
            // Close all other open dropdowns
            document.querySelectorAll('.jg-select-options.show').forEach(el => {
                if (el !== optionsContainer) el.classList.remove('show');
            });
            optionsContainer.classList.toggle('show');
        });

        // Handle option clicks
        optionsContainer.querySelectorAll('.jg-select-option').forEach(opt => {
            opt.addEventListener('click', (e) => {
                e.preventDefault();
                e.stopPropagation();
                
                const val = opt.getAttribute('data-value');
                if (val) {
                    // Visual feedback
                    optionsContainer.querySelectorAll('.jg-select-option').forEach(o => o.classList.remove('active'));
                    opt.classList.add('active');
                    optionsContainer.classList.remove('show');

                    // Global state change
                    if (container.closest('#lang-switcher') || container.closest('.jg-login-lang-container')) {
                        JG.setLang(val);
                    }
                }
            });
        });
    });

    // Global click listener to close dropdowns
    document.addEventListener('click', () => {
        document.querySelectorAll('.jg-select-options.show').forEach(el => el.classList.remove('show'));
    });

    // 4. Sidebar Collapse (Desktop)
    const collapseBtn = document.getElementById('sidebar-collapse-toggle');
    const adminLayout = document.querySelector('.jg-admin-layout');
    
    if (collapseBtn && adminLayout) {
        const toggleCollapse = (force) => {
            const isCollapsed = (force !== undefined) ? force : !adminLayout.classList.contains('is-sidebar-collapsed');
            adminLayout.classList.toggle('is-sidebar-collapsed', isCollapsed);
            localStorage.setItem('jg-sidebar-collapsed', isCollapsed ? 'true' : 'false');
            
            // Rotate icon
            const icon = collapseBtn.querySelector('.collapse-icon');
            if (icon) {
                icon.style.transform = isCollapsed ? 'rotate(180deg)' : 'rotate(0deg)';
            }
        };

        // Load initial state
        const saved = localStorage.getItem('jg-sidebar-collapsed');
        if (saved === 'true') {
            toggleCollapse(true);
        }

        collapseBtn.addEventListener('click', () => toggleCollapse());
    }

    // 5. Tab System Logic
    document.addEventListener('click', (e) => {
        const tabBtn = e.target.closest('.jg-tab-btn');
        if (!tabBtn) return;

        const targetId = tabBtn.getAttribute('data-tab');
        if (!targetId) return;

        const container = tabBtn.closest('.jg-tabs-container') || document;
        
        // Deactivate siblings
        tabBtn.parentElement.querySelectorAll('.jg-tab-btn').forEach(b => b.classList.remove('active'));
        tabBtn.classList.add('active');

        // Show target pane
        container.querySelectorAll('.jg-tab-pane').forEach(p => {
            p.classList.toggle('active', p.id === targetId);
        });
    });

    // 6. Theme setup
    JG.setupThemeToggle();
});

// ── Theme toggle ────────────────────────────────────────────────────────────
JG.setupThemeToggle = function () {
    const btn = document.getElementById('theme-toggle-btn');
    if (!btn) return;

    function applyTheme(theme) {
        const iconContainer = btn.querySelector('#theme-icon-slot') || btn.querySelector('.jg-theme-btn-icon') || btn;
        const labelContainer = btn.querySelector('.jg-nav-label') || btn.querySelector('span:not(#theme-icon-slot)');

        if (theme === 'dark') {
            document.documentElement.classList.add('dark');
            iconContainer.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" /></svg>`;
            if (labelContainer) labelContainer.textContent = "Thème";
            btn.title = "Passer au thème clair";
        } else {
            document.documentElement.classList.remove('dark');
            iconContainer.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" /></svg>`;
            if (labelContainer) labelContainer.textContent = "Thème";
            btn.title = "Passer au thème sombre";
        }
    }

    const savedTheme = localStorage.getItem('jg-theme') || 'dark';
    applyTheme(savedTheme);

    btn.addEventListener('click', (e) => {
        e.preventDefault();
        const isDark = document.documentElement.classList.contains('dark');
        const newTheme = isDark ? 'light' : 'dark';
        localStorage.setItem('jg-theme', newTheme);
        applyTheme(newTheme);
    });
};
