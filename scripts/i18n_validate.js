#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const glob = require('glob');

const i18nDir = path.join(__dirname, '..', 'web', 'i18n');
const files = glob.sync(path.join(i18nDir, '*.json'));
if (!files.length) {
  console.log('No i18n files found in', i18nDir);
  process.exit(0);
}

function hasBOM(buf) {
  return buf && buf.length >= 3 && buf[0] === 0xEF && buf[1] === 0xBB && buf[2] === 0xBF;
}

function containsReplacementChar(str) {
  return str.indexOf('\uFFFD') !== -1 || str.indexOf('\ufffd') !== -1;
}

function hasCyrillic(s) { return /[\u0400-\u04FF]/.test(s); }
function hasHan(s) { return /[\u4e00-\u9fff]/.test(s); }

const results = {};
let anyIssues = false;

files.forEach((f) => {
  const name = path.basename(f);
  const buf = fs.readFileSync(f);
  const info = { issues: [], suspectKeys: [] };

  if (hasBOM(buf)) info.issues.push('BOM');

  const txt = buf.toString('utf8');
  if (containsReplacementChar(txt)) info.issues.push('UTF8_REPLACEMENT_CHAR');

  let obj;
  try {
    obj = JSON.parse(txt);
  } catch (e) {
    info.issues.push('JSON_PARSE_ERROR: ' + e.message);
    results[name] = info;
    anyIssues = true;
    return;
  }

  const locale = name.replace(/\.json$/, '');

  Object.entries(obj).forEach(([k, v]) => {
    if (typeof v !== 'string') return;
    const sample = v.length > 120 ? v.slice(0, 120) + '...' : v;

    if (locale === 'ru') {
      if (!hasCyrillic(v)) {
        if (/[ÃÐâæå]/.test(v) || /[\u00C0-\u00FF]/.test(v)) {
          info.suspectKeys.push({ key: k, reason: 'no_cyrillic + mojibake_chars', sample });
        }
      }
    } else if (locale === 'zh') {
      if (!hasHan(v)) {
        if (/[æåÃ]/.test(v) || /[\u00C0-\u00FF]/.test(v)) {
          info.suspectKeys.push({ key: k, reason: 'no_han + mojibake_chars', sample });
        }
      }
    } else {
      if (containsReplacementChar(v)) info.suspectKeys.push({ key: k, reason: 'replacement_char', sample });
    }
  });

  if (info.issues.length || info.suspectKeys.length) anyIssues = true;
  results[name] = info;
});

const outName = path.join(i18nDir, 'suspect_keys_' + new Date().toISOString().replace(/[:.]/g, '-') + '.json');
fs.writeFileSync(outName, JSON.stringify(results, null, 2), 'utf8');
console.log('i18n validation written to', outName);
if (anyIssues) {
  console.error('i18n validation detected issues (see file).');
  process.exit(2);
}
console.log('i18n validation: no issues detected.');
process.exit(0);
