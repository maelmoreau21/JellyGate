#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const root = path.resolve(__dirname, '..');
const i18nDir = path.join(root, 'web', 'i18n');
const outDir = path.join(root, 'i18n-export');
if (!fs.existsSync(outDir)) fs.mkdirSync(outDir, { recursive: true });

const files = fs.readdirSync(i18nDir).filter(f => f.endsWith('.json') && !f.includes('.bak'));
const enFile = 'en.json';
if (!files.includes(enFile)) { console.error('en.json missing in', i18nDir); process.exit(1); }
const en = JSON.parse(fs.readFileSync(path.join(i18nDir, enFile), 'utf8'));
const allKeys = Object.keys(en).sort();

const corruptRe = /锟|\\u00|\\u001|�|\\u001|\\u00/;

const summary = {};
for (const file of files) {
  const locale = path.basename(file, '.json');
  const data = JSON.parse(fs.readFileSync(path.join(i18nDir, file), 'utf8'));
  const out = [];
  out.push('key,english,' + locale + ',status');
  let counts = { ok: 0, untranslated: 0, corrupt: 0, missing: 0 };
  for (const k of allKeys) {
    const enVal = en[k] !== undefined ? en[k] : '';
    const locVal = data[k] !== undefined ? data[k] : '';
    let status = 'ok';
    if (!locVal || String(locVal).trim() === '') status = 'missing';
    else if (String(locVal) === String(enVal)) status = 'untranslated';
    else if (corruptRe.test(String(locVal))) status = 'corrupt';
    counts[status] = (counts[status] || 0) + 1;
    function esc(s) { if (s === undefined || s === null) return '""'; return '"' + String(s).replace(/"/g, '""') + '"'; }
    out.push([k, enVal, locVal, status].map(esc).join(','));
  }
  fs.writeFileSync(path.join(outDir, locale + '.csv'), out.join('\n'), 'utf8');
  summary[locale] = counts;
  console.log(locale, counts);
}
fs.writeFileSync(path.join(outDir, 'summary.json'), JSON.stringify(summary, null, 2), 'utf8');
console.log('wrote csvs to', outDir);
