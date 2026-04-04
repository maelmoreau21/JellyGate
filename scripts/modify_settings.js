const fs = require('fs');
const file = 'web/templates/admin/settings.html';
let content = fs.readFileSync(file, 'utf8');

// For textareas with specific classes
content = content.replace(/class="jg-input font-mono text-sm"/g, 'class="jg-input font-mono text-sm bg-black/20 hover:bg-black/40"');
content = content.replace(/class="jg-input" rows="7"/g, 'class="jg-input bg-black/20 hover:bg-black/40" rows="7"');

// For inputs and selects, add bg-black/20 text-sm h-11 and remove existing basic jg-input (if alone)
content = content.replace(/class="jg-input"/g, 'class="jg-input bg-black/20 hover:bg-black/40 text-sm h-11"');

fs.writeFileSync(file, content);
console.log('Done modifying settings.html');
