const fs = require('fs');
const file = 'web/templates/admin/automation.html';
let content = fs.readFileSync(file, 'utf8');

// Replace the Th for ID
content = content.replace(/<th class="px-6 py-4">{{ \.T "automation_col_id" }}<\/th>\s*/, '');

fs.writeFileSync(file, content);
console.log('Done');
