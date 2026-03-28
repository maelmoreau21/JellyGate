#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const i18nDir = path.join(process.cwd(), 'web', 'i18n');
const files = fs.readdirSync(i18nDir).filter(f => f.endsWith('.json'));
const enPath = path.join(i18nDir, 'en.json');
let en = {};
try { en = JSON.parse(fs.readFileSync(enPath, 'utf8').replace(/^\uFEFF/, '')); } catch(e) { console.error('EN_PARSE_ERROR', e.message); process.exit(1); }
const data = {};
const allKeysSet = new Set(Object.keys(en));
for (const f of files) {
  const p = path.join(i18nDir, f);
  let raw = fs.readFileSync(p, 'utf8');
  if (raw.charCodeAt(0) === 0xFEFF) raw = raw.slice(1);
  let obj;
  try {
    obj = JSON.parse(raw);
  } catch (e) {
    try {
      const rawLatin1 = fs.readFileSync(p, 'binary');
      const maybe = Buffer.from(rawLatin1, 'binary').toString('utf8');
      obj = JSON.parse(maybe);
    } catch (e2) {
      console.error('PARSE_FAIL', f, e2.message);
      obj = {};
    }
  }
  data[f] = obj;
  Object.keys(obj).forEach(k => allKeysSet.add(k));
}
const allKeys = Array.from(allKeysSet).sort();
const missing = {};
for (const f of files) {
  const keys = Object.keys(data[f] || {});
  const m = allKeys.filter(k => !keys.includes(k));
  if (m.length) missing[f] = m;
}
const zhFixed = {};
if (data['zh.json']) {
  for (const k of Object.keys(data['zh.json'])) {
    const v = data['zh.json'][k];
    if (typeof v === 'string') {
      const decoded = Buffer.from(v, 'binary').toString('utf8');
      if (/[\u4e00-\u9fff]/.test(decoded)) zhFixed[k] = decoded; else zhFixed[k] = v;
    } else {
      zhFixed[k] = v;
    }
  }
}
console.log('---ZH_FIXED---');
console.log(JSON.stringify(zhFixed, null, 4));
console.log('---MISSING_KEYS---');
console.log(JSON.stringify(missing, null, 4));
console.log('---ALL_KEYS---');
console.log(JSON.stringify(allKeys, null, 4));
