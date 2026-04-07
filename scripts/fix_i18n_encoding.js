#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const i18nDir = path.join(process.cwd(), 'web', 'i18n');
const files = fs.readdirSync(i18nDir).filter(f => f.endsWith('.json'));

function countMatches(s, re) { return (s.match(re) || []).length; }

for (const f of files) {
  const p = path.join(i18nDir, f);
  let rawBuffer = fs.readFileSync(p);
  let obj = null;
  try {
    obj = JSON.parse(rawBuffer.toString('utf8').replace(/^\uFEFF/, ''));
  } catch (e) {
    try {
      obj = JSON.parse(rawBuffer.toString('binary'));
    } catch (e2) {
      try {
        obj = JSON.parse(Buffer.from(rawBuffer, 'binary').toString('utf8'));
      } catch (e3) {
        console.error('PARSE_FAIL', f, e3.message);
        continue;
      }
    }
  }

  function fixValue(v) {
    if (typeof v !== 'string') return v;
    const orig = v;
    const decoded = Buffer.from(v, 'binary').toString('utf8');
    const origScore = countMatches(orig, /[\u0400-\u04FF\u4E00-\u9FFF]/g);
    const decScore = countMatches(decoded, /[\u0400-\u04FF\u4E00-\u9FFF]/g);
    const origMoj = countMatches(orig, /Ã|Ã©|Ã¤|Ã¼|Ð|Ñ|Â|æ|ç|å|ä|Å|è|é|í|Ã¯/g);
    const decMoj = countMatches(decoded, /Ã|Ã©|Ã¤|Ã¼|Ð|Ñ|Â|æ|ç|å|ä|Å|è|é|í|Ã¯/g);
    if (decScore > origScore) return decoded;
    if (decScore === origScore && decMoj < origMoj) return decoded;
    return orig;
  }

  function traverse(o) {
    if (typeof o === 'string') return fixValue(o);
    if (Array.isArray(o)) return o.map(traverse);
    if (o && typeof o === 'object') {
      const out = {};
      for (const k of Object.keys(o)) out[k] = traverse(o[k]);
      return out;
    }
    return o;
  }

  const fixed = traverse(obj);
  const outPath = path.join(i18nDir, f.replace('.json', '.fixed.json'));
  fs.writeFileSync(outPath, JSON.stringify(fixed, null, 4), 'utf8');
  console.log('WROTE', outPath);
}
