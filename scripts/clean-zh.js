#!/usr/bin/env node
const fs = require('fs');
const zhFile = 'web/i18n/zh.json';
const enFile = 'web/i18n/en.json';
if (!fs.existsSync(zhFile)) { console.error('zh.json not found'); process.exit(1); }
if (!fs.existsSync(enFile)) { console.error('en.json not found'); process.exit(1); }
const zh = JSON.parse(fs.readFileSync(zhFile, 'utf8'));
const en = JSON.parse(fs.readFileSync(enFile, 'utf8'));
const corruptRe = /锟|\\u00|\\u001|�|\u001|\u00/;
let replaced = 0;
for (const k of Object.keys(en)) {
  const v = zh[k];
  if (typeof v !== 'string' || corruptRe.test(v)) {
    zh[k] = en[k];
    replaced++;
  }
}
fs.writeFileSync(zhFile, JSON.stringify(zh, null, 4), 'utf8');
console.log('cleaned', zhFile, 'replaced with English fallback for', replaced, 'keys');
