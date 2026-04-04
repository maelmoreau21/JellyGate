const fs = require('fs');
const file = 'web/templates/admin/automation.html';
let content = fs.readFileSync(file, 'utf8');

// 1. Remove stat card for mappings
content = content.replace(/<div class="jg-card jg-stat-card">[\s\S]*?id="automation-mappings-count"[\s\S]*?<\/div>/, '');

// 2. Remove tab nav for mappings
content = content.replace(/<button class="jg-tab-btn" data-tab="tab-mappings">[\s\S]*?<\/button>/, '');

// 3. Remove the entire tab pane for mappings
content = content.replace(/<div id="tab-mappings" class="jg-tab-pane">[\s\S]*?<\/section>\s*<\/div>/, '');

// 4. Remove the global save button for presets from the tab header
content = content.replace(/<button id="btn-preset-save" class="jg-btn jg-btn-primary jg-btn-sm">{{ \.T "automation_save" }}<\/button>/, '');

// 5. Update Preset Form: Replace ID field with LDAP Group DN
const idFieldRegex = /<div>\s*<label class="jg-label" for="preset-id">[\s\S]*?<input id="preset-id"[\s\S]*?<\/div>/;
content = content.replace(idFieldRegex, `<div>
                        <label class="jg-label" for="preset-ldap-dn">Groupe LDAP Associé (Optionnel)</label>
                        <input id="preset-ldap-dn" class="jg-input h-11 bg-black/20 text-sm" placeholder='CN=Groupe,DC=...'>
                    </div>`);

// 6. Add Custom Confirmation Modal Global at the bottom
const confirmModal = `
    {{/* ── Confirmation Modal ─────────────────────────────────────── */}}
    <div id="modal-confirm" class="modal-overlay hidden fixed inset-0 z-[200] flex items-center justify-center p-4">
        <div class="fixed inset-0 bg-black/60 backdrop-blur-sm modal-backdrop" data-modal="modal-confirm"></div>
        <div class="jg-card glass-card relative z-10 w-full max-w-sm flex flex-col items-center text-center p-6 border border-red-500/20 shadow-2xl">
            <div class="w-12 h-12 rounded-full bg-red-500/20 text-red-500 flex items-center justify-center mb-4">
                <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" /></svg>
            </div>
            <h2 class="text-xl font-bold mb-2 text-white" id="confirm-modal-title">Confirmer l'action</h2>
            <p class="text-jg-text-muted text-sm mb-6" id="confirm-modal-message">Êtes-vous sûr de vouloir continuer ? Cette action est irréversible.</p>
            <div class="flex gap-3 w-full">
                <button id="btn-confirm-cancel" class="flex-1 jg-btn jg-btn-ghost modal-close-btn" data-modal="modal-confirm">Annuler</button>
                <button id="btn-confirm-action" class="flex-1 jg-btn bg-red-500 hover:bg-red-600 text-white border-0">Confirmer</button>
            </div>
        </div>
    </div>
</div>
`;
content = content.replace("</div>\n\n{{ end }}", confirmModal + "\n{{ end }}");

fs.writeFileSync(file, content);
console.log('Done mutating automation.html');
