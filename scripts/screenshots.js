#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');
const puppeteer = require('puppeteer');

const argv = require('minimist')(process.argv.slice(2));
const host = argv.host || process.env.JELLYGATE_HOST || process.env.JELLYGATE_URL || 'http://localhost:8097';
let cookie = argv.cookie || process.env.SESSION_COOKIE || '';
const outdir = argv.outdir || path.join(__dirname, '..', 'docs', 'screenshots');

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

    const browser = await puppeteer.launch({
        headless: 'new',
        protocolTimeout: 60000,
        args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage', '--disable-gpu']
    });
    try {
        const page = await browser.newPage();
        page.on('console', msg => console.log('BROWSER LOG:', msg.text()));
        page.on('pageerror', err => console.error('BROWSER PAGE ERROR:', err.toString()));
        await page.setViewport({ width: 1440, height: 900 });
        const urlObj = new URL(host);
        const domain = urlObj.hostname;
        const secure = urlObj.protocol === 'https:';

        // First capture the login page (unauthenticated)
        const loginTarget = new URL('/admin/login', host).toString();
        console.log('Capturing login page:', loginTarget);
        await page.goto(loginTarget, { waitUntil: 'networkidle2', timeout: 30000 });
        try {
            await page.waitForSelector('form', { timeout: 10000 });
        } catch (e) {}
        await new Promise(resolve => setTimeout(resolve, 2000));
        const loginOutPath = path.join(outdir, 'login.png');
        await page.screenshot({ path: loginOutPath, fullPage: false });
        console.log('Saved:', loginOutPath);

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
            { path: '/admin/', name: 'dashboard.png', waitFor: 'h2', delay: 18000 },
            { path: '/admin/users', name: 'users.png', waitFor: '#users-tbody', delay: 18000 },
            { path: '/admin/invitations', name: 'invitations.png', waitFor: 'table', delay: 2000 },
            { path: '/admin/logs', name: 'logs.png', waitFor: 'table', delay: 2000 },
            { path: '/admin/settings', name: 'settings.png', waitFor: 'form', delay: 2000 },
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
            // Wait for dynamic AJAX content to load and render
            const waitTime = p.delay || 3500;
            await new Promise(resolve => setTimeout(resolve, waitTime));
            const outPath = path.join(outdir, p.name);
            await page.screenshot({ path: outPath, fullPage: false });
            console.log('Saved:', outPath);
        }
    } finally {
        await browser.close();
    }
})();
