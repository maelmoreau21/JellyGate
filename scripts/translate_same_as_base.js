#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const https = require('https');

const I18N_DIR = path.join(__dirname, '..', 'web', 'i18n');
const BASE_LOCALE = 'en';
const TARGET_LOCALES = ['de', 'es', 'fr', 'it', 'nl', 'pl', 'pt-br', 'ru', 'zh'];
const TARGET_LANG_MAP = {
    de: 'de',
    es: 'es',
    fr: 'fr',
    it: 'it',
    nl: 'nl',
    pl: 'pl',
    'pt-br': 'pt',
    ru: 'ru',
    zh: 'zh-CN',
};

const INVARIANT_VALUES = new Set([
    'Jellyfin',
    'LDAP',
    'SMTP',
    'JSON',
    'ID',
    'CSV',
    'API',
    'Discord',
    'Telegram',
    'Matrix',
    'Jellyseerr',
    'JellyTrack',
    'Jellytrack',
]);

const CONCURRENCY = Number(process.env.I18N_TRANSLATE_CONCURRENCY || '6');
const REQUEST_DELAY_MS = Number(process.env.I18N_TRANSLATE_DELAY_MS || '80');
const MAX_RETRIES = Number(process.env.I18N_TRANSLATE_RETRIES || '3');

function sleep(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms));
}

function readJSON(filePath) {
    return JSON.parse(fs.readFileSync(filePath, 'utf8'));
}

function writeJSON(filePath, value) {
    fs.writeFileSync(filePath, `${JSON.stringify(value, null, 4)}\n`, 'utf8');
}

function protectSegments(text) {
    let protectedText = text;
    const tokens = [];

    const patterns = [
        /\{\{[^{}]+\}\}/g,
        /\{[a-zA-Z0-9_.-]+\}/g,
        /%(\[[0-9]+\])?[+#0\- ]*(\d+|\*)?(\.(\d+|\*))?[bcdeEfFgGosqvxXtp]/g,
        /<\/?.*?>/g,
        /(\r\n|\n|\r)/g,
    ];

    for (const pattern of patterns) {
        protectedText = protectedText.replace(pattern, (match) => {
            const token = `ZXQPH${tokens.length}ZXQ`;
            tokens.push({ token, value: match });
            return token;
        });
    }

    return { protectedText, tokens };
}

function restoreSegments(text, tokens) {
    let restored = text;
    for (const { token, value } of tokens) {
        restored = restored.split(token).join(value);
    }
    return restored;
}

function fetchJSON(url, timeoutMs = 20000) {
    return new Promise((resolve, reject) => {
        const req = https.get(
            url,
            {
                headers: {
                    'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36',
                    Accept: 'application/json',
                },
            },
            (res) => {
                let data = '';
                res.setEncoding('utf8');
                res.on('data', (chunk) => {
                    data += chunk;
                });
                res.on('end', () => {
                    if (res.statusCode < 200 || res.statusCode > 299) {
                        reject(new Error(`HTTP ${res.statusCode}: ${data.slice(0, 300)}`));
                        return;
                    }
                    try {
                        resolve(JSON.parse(data));
                    } catch (err) {
                        reject(new Error(`Invalid JSON response: ${err.message}`));
                    }
                });
            }
        );

        req.on('error', reject);
        req.setTimeout(timeoutMs, () => {
            req.destroy(new Error(`Request timeout after ${timeoutMs}ms`));
        });
    });
}

async function translateWithRetry(sourceText, targetLang) {
    if (!sourceText || !sourceText.trim()) {
        return sourceText;
    }

    const { protectedText, tokens } = protectSegments(sourceText);

    const url = `https://translate.googleapis.com/translate_a/single?client=gtx&sl=en&tl=${encodeURIComponent(targetLang)}&dt=t&q=${encodeURIComponent(protectedText)}`;

    let lastError = null;
    for (let attempt = 1; attempt <= MAX_RETRIES; attempt++) {
        try {
            const body = await fetchJSON(url);
            const translated = Array.isArray(body?.[0])
                ? body[0].map((part) => (Array.isArray(part) ? part[0] : '')).join('')
                : protectedText;

            return restoreSegments(translated, tokens);
        } catch (err) {
            lastError = err;
            const backoff = Math.min(1500, 250 * attempt);
            await sleep(backoff);
        }
    }

    throw lastError;
}

function shouldSkipTranslation(sourceText) {
    if (!sourceText || !sourceText.trim()) {
        return true;
    }

    if (INVARIANT_VALUES.has(sourceText.trim())) {
        return true;
    }

    if (/^[A-Z0-9_\-./ ]{1,8}$/.test(sourceText.trim())) {
        return true;
    }

    return false;
}

async function runQueue(tasks, worker, concurrency) {
    const queue = tasks.slice();
    const workers = [];

    for (let i = 0; i < concurrency; i++) {
        workers.push(
            (async () => {
                while (queue.length > 0) {
                    const next = queue.shift();
                    if (!next) {
                        return;
                    }
                    await worker(next);
                }
            })()
        );
    }

    await Promise.all(workers);
}

async function main() {
    const basePath = path.join(I18N_DIR, `${BASE_LOCALE}.json`);
    const base = readJSON(basePath);

    const localeMaps = {};
    for (const locale of TARGET_LOCALES) {
        localeMaps[locale] = readJSON(path.join(I18N_DIR, `${locale}.json`));
    }

    const tasks = [];
    for (const locale of TARGET_LOCALES) {
        const localeMap = localeMaps[locale];
        for (const [key, enValue] of Object.entries(base)) {
            if (!(key in localeMap)) {
                continue;
            }
            if (localeMap[key] !== enValue) {
                continue;
            }
            if (shouldSkipTranslation(enValue)) {
                continue;
            }
            tasks.push({ locale, key, source: enValue });
        }
    }

    console.log(`[i18n-translate] tasks=${tasks.length} locales=${TARGET_LOCALES.length} concurrency=${CONCURRENCY}`);

    const cache = new Map();
    const stats = Object.fromEntries(TARGET_LOCALES.map((l) => [l, { translated: 0, skipped: 0, failed: 0 }]));
    const failures = [];
    let done = 0;

    await runQueue(
        tasks,
        async (task) => {
            const localeCode = TARGET_LANG_MAP[task.locale] || task.locale;
            const cacheKey = `${localeCode}::${task.source}`;

            try {
                let translated = cache.get(cacheKey);
                if (!translated) {
                    translated = await translateWithRetry(task.source, localeCode);
                    cache.set(cacheKey, translated);
                    await sleep(REQUEST_DELAY_MS);
                }

                if (!translated || translated.trim() === task.source.trim()) {
                    stats[task.locale].skipped += 1;
                } else {
                    localeMaps[task.locale][task.key] = translated;
                    stats[task.locale].translated += 1;
                }
            } catch (err) {
                stats[task.locale].failed += 1;
                failures.push({ locale: task.locale, key: task.key, error: err.message });
            }

            done += 1;
            if (done % 100 === 0 || done === tasks.length) {
                console.log(`[i18n-translate] progress ${done}/${tasks.length}`);
            }
        },
        CONCURRENCY
    );

    for (const locale of TARGET_LOCALES) {
        writeJSON(path.join(I18N_DIR, `${locale}.json`), localeMaps[locale]);
    }

    console.log('[i18n-translate] done');
    console.log(JSON.stringify({ stats, failures: failures.slice(0, 50), totalFailures: failures.length }, null, 2));

    if (failures.length > 0) {
        process.exitCode = 2;
    }
}

main().catch((err) => {
    console.error('[i18n-translate] fatal:', err);
    process.exit(1);
});
