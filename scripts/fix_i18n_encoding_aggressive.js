#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
let iconv;
try { iconv = require('iconv-lite'); } catch (e) {
  console.error('Missing dependency: please run `npm install iconv-lite` before using this script.');
  process.exit(2);
}

const i18nDir = path.join(process.cwd(), 'web', 'i18n');
const files = fs.readdirSync(i18nDir).filter(f => f.endsWith('.json') && !f.endsWith('.fixed.json') && !f.endsWith('.fixed.aggressive.json'));

function countMatches(s, re) { return (s.match(re) || []).length; }

function chooseBestForLocale(orig, locale) {
  const buf = Buffer.from(orig, 'binary');
  const candidates = [];

  // plain reinterpretation as UTF-8
  candidates.push({ name: 'utf8', text: buf.toString('utf8') });

  // preferred encodings by locale
  const encs = [];
  if (locale === 'ru' || locale.startsWith('ru')) encs.push('windows-1251', 'cp1251', 'win1251');
  if (locale === 'zh' || locale.startsWith('zh')) encs.push('gb18030', 'gbk', 'gb2312');

  // add some fallbacks
  encs.push('windows-1252', 'latin1', 'iso-8859-1');

  for (const e of encs) {
    try { candidates.push({ name: e, text: iconv.decode(buf, e) }); } catch (e) { /* ignore */ }
  }

  // scoring: prefer cyrillic or cjk, penalize mojibake-like tokens
  const scored = candidates.map(c => {
    const text = c.text || '';
    const cyr = countMatches(text, /[\u0400-\u04FF]/g);
    const cjk = countMatches(text, /[\u4E00-\u9FFF]/g);
    const moj = countMatches(text, /Ã|Ð|Ñ|Â|æ|ç|å|ä|Å|è|é|í|Ã¯|�/g);
    const score = cyr * 2 + cjk * 2 - moj;
    return { ...c, cyr, cjk, moj, score };
  });

  scored.sort((a, b) => b.score - a.score);
  const best = scored[0] || { text: orig, moj: 999 };

  const origMoj = countMatches(orig, /Ã|Ð|Ñ|Â|æ|ç|å|ä|Å|è|é|í|Ã¯|�/g);
  if ((best.cyr > 0 || best.cjk > 0) || best.moj < origMoj) return best.text;
  return orig;
}

function traverse(o, locale, stats) {
  if (typeof o === 'string') {
    const out = chooseBestForLocale(o, locale);
    if (out !== o) stats.changed++;
    return out;
  }
  if (Array.isArray(o)) return o.map(x => traverse(x, locale, stats));
  if (o && typeof o === 'object') {
    const out = {};
    for (const k of Object.keys(o)) out[k] = traverse(o[k], locale, stats);
    return out;
  }
  return o;
}

for (const f of files) {
  const p = path.join(i18nDir, f);
  const locale = f.replace('.json', '');
  let rawBuffer = fs.readFileSync(p);
  let obj;
  try {
    obj = JSON.parse(rawBuffer.toString('utf8').replace(/^\uFEFF/, ''));
  } catch (e) {
    try { obj = JSON.parse(rawBuffer.toString('binary')); } catch (e2) { console.error('PARSE_FAIL', f); continue; }
  }
  const stats = { changed: 0 };
  const fixed = traverse(obj, locale, stats);
  const outPath = path.join(i18nDir, locale + '.fixed.aggressive.json');
  fs.writeFileSync(outPath, JSON.stringify(fixed, null, 4), 'utf8');
  console.log('WROTE', outPath, 'changedKeys=', stats.changed);
}
