#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const glob = require('glob');
let iconv;
try { iconv = require('iconv-lite'); } catch (e) { console.error('Please `npm install iconv-lite` first'); process.exit(2); }

function countMatches(s, re) { return (s.match(re) || []).length; }

// pattern that commonly appears when UTF-8 was mis-decoded as latin1/win1252
const mojPattern = /Ã|Â|â|â†’|â€”|â€¦/;
const accentedRe = /[àâäáéèêëíìîïóòôöúùûüçÀÂÄÉÈÊËÍÌÎÏÓÒÔÖÚÙÛÜÇœŒŸÿ]/g;

const files = glob.sync('**/*.go', { ignore: ['**/vendor/**', '**/node_modules/**'] });
let changed = 0;
for (const f of files) {
  const buf = fs.readFileSync(f);
  const asUtf8 = buf.toString('utf8');
  if (!mojPattern.test(asUtf8)) continue; // likely clean

  // try windows-1252 and iso-8859-1
  const win = iconv.decode(buf, 'windows-1252');
  const iso = iconv.decode(buf, 'iso-8859-1');

  const candidates = [ { name: 'utf8', text: asUtf8 }, { name: 'win1252', text: win }, { name: 'iso8859-1', text: iso } ];

  const scored = candidates.map(c => {
    const accents = countMatches(c.text, accentedRe);
    const moj = countMatches(c.text, /Ã|Â|â|â†’|â€”|â€¦|Â©|Ã©|Ã¨|Ãª|Ã»/g);
    const score = accents * 3 - moj;
    return { ...c, accents, moj, score };
  }).sort((a,b) => b.score - a.score);

  const best = scored[0];
  // prefer candidate if it reduces mojibake while keeping/increasing accents
  if (best.name !== 'utf8' && (best.moj < scored.find(s=>s.name==='utf8').moj || best.accents > scored.find(s=>s.name==='utf8').accents)) {
    const bak = f + '.bak_auto';
    if (!fs.existsSync(bak)) fs.writeFileSync(bak, buf);
    fs.writeFileSync(f, best.text, 'utf8');
    console.log('FIXED', f, '->', best.name, 'accents=', best.accents, 'moj=', best.moj);
    changed++;
  }
}

console.log('Done. files changed=', changed);
