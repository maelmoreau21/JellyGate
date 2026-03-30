#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');
const puppeteer = require('puppeteer');

const argv = require('minimist')(process.argv.slice(2));
const host = argv.host || process.env.JELLYGATE_HOST || process.env.JELLYGATE_URL || 'http://localhost:8097';
let cookie = argv.cookie || process.env.SESSION_COOKIE || '';
const outdir = argv.outdir || path.join(__dirname, '..', 'tmp', 'screenshots');

async function ensureOutDir() {
    fs.mkdirSync(outdir, { recursive: true });
}

function tryGenerateCookie() {
    try {
        const out = execSync('go run ./cmd/generate_session', { encoding: 'utf8', stdio: ['pipe', 'pipe', 'inherit'] });
        return out.trim();
    } catch (e) {
        console.error('Could not auto-generate cookie via go run ./cmd/generate_session');
        return '';
    }
}

(async () => {
    if (!cookie) {
        console.log('SESSION_COOKIE not provided, attempting to generate via `go run ./cmd/generate_session`...');
        cookie = tryGenerateCookie();
        if (!cookie) {
            console.error('No session cookie available. Provide via --cookie or SESSION_COOKIE env var.');
            process.exit(2);
        }
    }

    await ensureOutDir();

    const browser = await puppeteer.launch({ headless: true });
    try {
        const page = await browser.newPage();
        const urlObj = new URL(host);
        const domain = urlObj.hostname;
        const secure = urlObj.protocol === 'https:';

        // Set cookie for the site so we can access admin pages
        await page.setCookie({
            name: 'jellygate_session',
            value: cookie,
            domain: domain,
            path: '/',
            httpOnly: false,
            secure: secure,
        });

        const pages = [
            { path: '/admin/login', name: 'login.png', waitFor: 'form' },
            { path: '/admin/users', name: 'users.png', waitFor: '#users-tbody' },
        ];

        for (const p of pages) {
            const target = new URL(p.path, host).toString();
            console.log('Capturing', target);
            await page.goto(target, { waitUntil: 'networkidle2', timeout: 30000 });
            if (p.waitFor) {
                try {
                    await page.waitForSelector(p.waitFor, { timeout: 10000 });
                } catch (e) {
                    // ignore
                }
            }
            const outPath = path.join(outdir, p.name);
            await page.screenshot({ path: outPath, fullPage: true });
            console.log('Saved:', outPath);
        }
    } finally {
        await browser.close();
    }
})();
