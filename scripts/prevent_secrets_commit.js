#!/usr/bin/env node
const fs = require('fs');
const { execSync } = require('child_process');

function listStagedFiles() {
  try {
    const out = execSync('git diff --cached --name-only --diff-filter=ACM', { encoding: 'utf8' });
    return out.split(/\r?\n/).filter(Boolean);
  } catch (e) {
    // Not a git repo or no staged files
    return [];
  }
}

const files = listStagedFiles();
if (!files.length) process.exit(0);

const secrets = [
  { key: 'JELLYGATE_SECRET_KEY', re: /^JELLYGATE_SECRET_KEY\s*=\s*(.+)$/m },
  { key: 'JELLYFIN_API_KEY', re: /^JELLYFIN_API_KEY\s*=\s*(.+)$/m }
];

const found = [];

files.forEach((file) => {
  if (!fs.existsSync(file)) return;
  const content = fs.readFileSync(file, 'utf8');
  secrets.forEach((s) => {
    const m = content.match(s.re);
    if (m && m[1]) {
      const val = m[1].trim();
      if (!val) return; // empty is fine (placeholder)
      if (/^(<.*>|YOUR_|REPLACE|EXAMPLE|changeme)$/i.test(val)) return;
      found.push({ file, key: s.key, valuePreview: val.length > 40 ? val.slice(0, 40) + '...' : val });
    }
  });
});

if (found.length) {
  console.error('\nAborting commit: secrets detected in staged files:');
  found.forEach((f) => console.error(` - ${f.file}: ${f.key}=${f.valuePreview}`));
  console.error('\nMove secrets to an ignored file (e.g. .env.local), then unstage the secret-containing file.');
  process.exit(1);
}
process.exit(0);
