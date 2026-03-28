#!/usr/bin/env node
const fs = require('fs');
const bak = 'web/i18n/zh.json.bak_auto';
const enfile = 'web/i18n/en.json';
const out = 'web/i18n/zh.json';
if (!fs.existsSync(bak)) { console.error('Backup not found:', bak); process.exit(1); }
let txt = fs.readFileSync(bak, 'utf8');
let en;
try { en = JSON.parse(fs.readFileSync(enfile, 'utf8')); } catch (e) { console.error('Cannot read en.json:', e.message); process.exit(1); }
const zh = Object.assign({}, en);
const keyRegex = /"([^"\\]+)"\s*:/g;
let m;
const keys = [];
while ((m = keyRegex.exec(txt)) !== null) keys.push({ key: m[1], pos: m.index });
function tryFix(raw) {
  if (!raw) return null;
  let v = raw.replace(/[\u0000-\u001f]/g, '').trim();
  const candidates = [v];
  try { candidates.push(Buffer.from(v, 'binary').toString('utf8')); } catch (e) {}
  try { candidates.push(Buffer.from(Buffer.from(v, 'utf8').toString('binary'), 'binary').toString('utf8')); } catch (e) {}
  try { candidates.push(Buffer.from(v, 'latin1').toString('utf8')); } catch (e) {}
  for (const c of candidates) if (/[\u4e00-\u9fff]/.test(c)) return c;
  return null;
}
let count = 0, extracted = 0;
for (const k of keys) {
  const colon = txt.indexOf(':', k.pos);
  if (colon < 0) continue;
  let i = colon + 1; while (i < txt.length && /\s/.test(txt[i])) i++;
  if (txt[i] !== '"') continue; i++;
  let val = '';
  while (i < txt.length) {
    const ch = txt[i];
    if (ch === '\\') { val += ch; i++; if (i < txt.length) { val += txt[i]; i++; } }
    else if (ch === '"') { i++; break; }
    else { val += ch; i++; }
  }
  count++;
  const fixed = tryFix(val);
  if (fixed) {
    zh[k.key] = fixed;
    extracted++;
    console.log('fixed', k.key, '=>', fixed.slice(0,60).replace(/\n/g, ' '));
  }
}
fs.writeFileSync(out, JSON.stringify(zh, null, 4), { encoding: 'utf8' });
console.log('wrote', out, 'keysFound', count, 'extractedChinese', extracted);
