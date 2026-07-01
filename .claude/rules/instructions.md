# JellyGate - Instructions For AI Agents
<p align="center">
  <img src="../../logo.svg" width="128" height="128" alt="JellyGate Logo">
</p>

Last updated: 2026-05-26 (Final Version)

Read this document before changing the project.

## Canonical Stack

- Backend: Go 1.26+, `net/http`, Chi v5
- Templates: `html/template`
- Frontend: HTML, local Tailwind build, vanilla JS, custom CSS
- Database: SQLite (`modernc.org/sqlite`) or PostgreSQL
- LDAP: `go-ldap/ldap/v3`
- Jellyfin: REST API
- Email: `wneessen/go-mail`
- Notifications: Discord, Telegram, Matrix
- CI/CD: GitHub Actions, Docker Buildx, GHCR

## Working Rules

- **Final Version**: the project is in its final phase. Prioritize stability, performance, and visual consistency.
- Treat the "Project Context" section below as the authority before any decision, code change, or PR proposal.
- Discover project conventions first, then keep changes aligned with the existing stack.
- Do not modify the "Project Context" section without explicit approval.
- Link to existing documentation rather than duplicating it.
- **Docker First**: Docker is the only officially recommended production path. Verify deployment-impacting changes in `docker-compose.yml` and the `Dockerfile`.
- Preserve i18n compatibility: after any template or label change, check every `web/i18n/*.json` file and run `go run ./cmd/i18ncheck` when relevant.
- Recommended local validation: run `go build ./...` and `go test ./...` after Go changes; for CSS, run `npm run build:css`.
- Save JSON files as UTF-8 without BOM.
- Never commit secrets in clear text.
- Do not commit or push changes unless the user explicitly asks.

## Project Map

- `cmd/generate_session/`: secret generation tool
- `cmd/i18ncheck/`: i18n CI check
- `cmd/i18ncoverage/`: i18n coverage stats
- `cmd/jellygate/`: HTTP entrypoint
- `internal/backup/`: backup / restore
- `internal/config/`: runtime config and domain structs
- `internal/database/`: migrations, SQL access, settings
- `internal/handlers/`: admin/public pages and API
- `internal/integrations/`: third-party provisioning
- `internal/jellyfin/`: Jellyfin client
- `internal/ldap/`: LDAP / AD client
- `internal/mail/`: SMTP mailer
- `internal/middleware/`: auth, i18n, security, rate limit
- `internal/notify/`: webhooks
- `internal/render/`: rendering + translations
- `internal/scheduler/`: periodic tasks
- `internal/session/`: signed cookies
- `scripts/build-css.js`: Tailwind build
- `scripts/i18n_inspect.js`: manual i18n audit
- `scripts/run_screenshots.ps1`: screenshots tool
- `scripts/screenshots.js`: screenshots engine
- `web/i18n/`: locale JSON
- `web/static/`: css, js, favicon
- `web/templates/`: pages, layouts, emails

## Quick Commands

- `npm run build:css`
- `go build ./...`
- `go test ./...`
- `go run ./cmd/i18ncheck`

## Prompt Examples

- "Before any change, propose a 3-step plan and list the files to edit."
- "Prepare the proposed diff for a PR, list tests to run, and validation commands."
- "Check i18n parity for the key 'invite-policy-summary' and propose fixes if keys are missing."

## Expected Behavior

- Stay brief, factual, and action-oriented.
- Ask clarifying questions for any impactful change.
- Propose PRs or patches when possible and list validation commands.

## Notes

- This file follows the bootstrap described in `init.prompt.md`: discover conventions, explore the code, generate or merge, and iterate with feedback.
- If you want additional rules such as git hooks, commit conventions, or CI checks, add them here.

---

<!-- Imported content: Project Context (project_context.md) -->

# JellyGate - Project Context

> Last updated: 2026-05-26
> Version: 1.3.0
> Author: Mael Moreau

## 1. Vision

JellyGate is an admin and onboarding portal for Jellyfin servers, designed for self-hosted deployments that want to:

- centralize invitations, account creation, and password resets
- integrate native LDAP / Active Directory
- keep a simple stack deployable as a Go binary or Docker
- expose a modern admin interface without a heavy frontend dependency

The project replaces the jfa-go approach with a tighter integration of Jellyfin, LDAP, SQL persistence, and in-house automation workflows.

## 2. Current stack

- Backend: Go 1.26+, net/http, Chi v5
- Templates: `html/template`
- Frontend: HTML, local Tailwind build, vanilla JS, custom CSS
- Database: SQLite (`modernc.org/sqlite`) or PostgreSQL
- LDAP: `go-ldap/ldap/v3`
- Jellyfin: REST API
- Email: `wneessen/go-mail`
- Notifications: Discord, Telegram, Matrix
- CI/CD: GitHub Actions, Docker Buildx, GHCR

Tailwind CSS is generated locally via `npm run build:css` and served from `web/static/css/tailwind.generated.css`.

## 3. Logical tree

```text
cmd/
    generate_session/        # secret generation tool
    i18ncheck/               # i18n CI check
    i18ncoverage/            # i18n coverage stats
    jellygate/               # HTTP entrypoint
internal/
    backup/                  # backup / restore
    config/                  # runtime config and domain structs
    database/                # migrations, SQL access, settings
    handlers/                # admin/public pages and API
    integrations/            # third-party provisioning
    jellyfin/                # Jellyfin client
    ldap/                    # LDAP / AD client
    mail/                    # SMTP mailer
    middleware/              # auth, i18n, security, rate limit
    notify/                  # webhooks
    render/                  # rendering + translations
    scheduler/               # periodic tasks
    session/                 # signed cookies
scripts/
    build-css.js             # Tailwind build
    i18n_inspect.js          # manual i18n audit
    run_screenshots.ps1      # screenshots tool
    screenshots.js           # screenshots engine (puppeteer)
web/
    i18n/                    # locale JSON
    static/                  # css, js, favicon
    templates/               # pages, layouts, emails
```

## 4. Product capabilities

### 4.1 Invitations

- unique codes with quota, expiry, and label
- Jellyfin profiles tied to the invitation
- group mapping / automation preset mapping
- atomic flow with LDAP/Jellyfin rollback on failure
- email verification configurable at the invitation policy level, enabled by default
- if email verification is enabled, the account is created only after confirming `/verify-email/{code}`
- audit correlation by `request_id`
- invitation email no longer shows an expiry block if no expiry is defined
- help and expiry blocks can be disabled from admin
- a toggle can auto-purge expired or exhausted links
- a "Forced Username" can only be set on single-use invites (`max_uses = 1`) to avoid account creation conflicts
- non-admin users cannot create unlimited invites (`max_uses` must be >= 1)
- technical base ready for a future user sponsorship mode from `My account`
- Signup security: anti-abuse and CAPTCHA settings live in `Admin > Invitations`, not in a generic product center. Dedicated admin API: `GET/POST /admin/api/invitations/security`.
- Local CAPTCHA appears on `/invite/{code}` only when `Anti-abuse enabled` and `Local CAPTCHA` are enabled. CAPTCHA failures, form validation failures, invalid invites, or unavailable usernames are counted per IP; after `max_failures` within `window_minutes`, the IP is blocked for `block_minutes`.
- Do not reintroduce the `Product Center` UI page (`/admin/product`). If an old product module becomes useful, place it in the relevant feature page.

### 4.2 Users

- admin listing
- Jellyfin sync
- account deletion
- access toggle
- targeted communications and admin sends from the `Users` page
- no internal messaging exposed to users: product communication is email only
- personal profile with preferred language and notification preferences
- admin UI refactor in progress, page by page, to unify the interface

### 4.3 Password reset

- public request page
- temporary token/code
- Jellyfin + LDAP update
- anti-enumeration in user-facing messages

### 4.4 Contact channel verification

- verified / pending status exposed on the user profile
- public link `/verify-email/{code}` with handling for valid / expired / already used / invalid
- initial send on signup and resend from `My account`
- for public signups, a temporary record is kept until confirmation to avoid account creation before email validation
- address changes stored in `pending_email` until confirmed
- verification email HTML body and subject are configurable from admin
- historical policy: accounts that existed before this feature, with an email and no pending verification, are marked verified once at startup
- target goal: extend the same verification model to Discord / Telegram / Matrix later

### 4.5 Automation and home server

- Jellyfin presets
- LDAP group mappings / functional group mappings
- scheduled tasks
- optional Jellyseerr provisioning
- Jellyfin presets are now explicit and do not rely on cloning a template user. A preset can define rights, accessible libraries, Jellyfin home, and Jellyfin Web display preferences.
- The legacy JSON field `template_user_id` remains accepted for read compatibility, but must not be used for new presets or exposed in the UI.
- Jellyfin preset blocks to preserve:
  - `user_configuration`: missing episodes, recently added behavior, library order, grouped folders, "My media" and "Latest" exclusions.
  - `display_preferences`: screensaver, timeouts, animations, blur, backgrounds, themes, detail banner, page size, "Continue Watching" delay, episode images, and home sections 1 to 10.
- Added admin endpoint: `GET /admin/api/automation/libraries`, which exposes libraries from Jellyfin `/Library/VirtualFolders`.
- In UI `Automation > Presets`, settings are organized in collapsible panels `Access` and `Personalization`. The library table uses:
  - `Access`: grant or remove Jellyfin access to the library.
  - `Home: My media`: show this library in "My media" on the home page; does not change access.
  - `Home: Latest`: allow this library in latest items.
  - `Group by type`: feeds `GroupedFolders` to group folders in Jellyfin views by type (Movies, Series, Music, etc.).
  - `Position`: library order via Move Up/Down buttons; do not reintroduce a numeric input because duplicates made ordering ambiguous.
- When a library is not accessible in the preset, home/group/position options must remain disabled and must not be sent in `ordered_views`, `grouped_folders`, `my_media_excludes`, or `latest_items_excludes`.
- Applying a Jellyfin preset uses three client calls: `POST /Users/{userId}/Policy`, `POST /Users/Configuration?userId=...`, then `GET/POST /DisplayPreferences/usersettings?userId=...&client=emby`.

### 4.6 Email templates

- `Settings > Email templates` page focused on the only templates that are useful
- each template is edited via a collapsible block, avoiding long hard-to-scan pages
- "Plain messages without tags" editors now accept template variables like `{{.Username}}`, `{{.Email}}`, `{{.ExpiryDate}}`, `{{.InviteLink}}`, or `{{.JellyfinURL}}`
- a "click to insert" selector lets users add variables in no-code messages
- default plain messages are more personalized and stay injected into shared layout and system blocks
- expiry reminders use a single coherent template, regardless of the reminder tier
- keep an activation panel to cleanly disable certain automated emails
- help content before/after signup is simplified to avoid internal documentation style messages
- no expiry adjustment email is sent when a user expiry is removed
- product behavior favors useful-only sending, no "Not defined" fragments

### 4.7 Audit and observability

- SQL `audit_log`
- advanced filters on logs API
- CSV / JSON export
- `request_id` extraction and display

### 4.8 i18n

- JSON locales under `web/i18n`
- detection by `lang` cookie, then `Accept-Language`, then `default_lang`
- fallback `requested lang -> en -> fr`
- CI command `go run ./cmd/i18ncheck`
- navigation and `Email templates` labels normalized across main admin locales

### 4.9 Validated product roadmap

- user sponsorship from `My account` with quotas, lifetime, and sponsor -> invite traceability
- mandatory or configurable email verification by instance policy
- direct admin user creation with full preset, expiry, and welcome message
- manual task center to run housekeeping, Jellyfin sync, integration sync, and backups
- deeper Jellyseerr integration: profile sync, notification prefs, manual resync
- customizable product content for onboarding, post-signup help, and reusable messages
- enriched user timeline based on existing audit log

### 4.10 Improvement backlog inspired by jfa-go

- invitation anti-abuse: CAPTCHA and temporary block are integrated in `Admin > Invitations`; improvements only in metrics/risk, without recreating a "Product Center"
- content studio: Markdown editor with preview for invitation, welcome, emails, and user account
- first-run assistant: guided Jellyfin, SMTP, public URL, security, and backup setup
- Discord / Telegram / Matrix linking on user side with validation
- richer admin timeline: invites, signups, presets, expiries, and admin actions
- pre-apply simulator: preview changes before preset or bulk action
- portal health page: Jellyfin, SMTP, Jellyseerr, webhooks, backups, public URL, and i18n diagnostics
- advanced sponsorship: sponsor stats, personal links, quotas, expiry, and sponsor -> invite tracking
- lifecycle rules: inactivity, disable, deferred delete, and automated reminders
- clean history and audit: filterable journal, CSV/JSON export, before/after details
- Ombi remains excluded from the requested backlog

## 5. Important routes

### Public

| Method | Route | Usage |
| --- | --- | --- |
| GET | `/invite/{code}` | signup page |
| POST | `/invite/{code}` | signup validation |
| GET | `/reset` | password reset request page |
| POST | `/reset/request` | reset issuance |
| GET | `/reset/{code}` | new password form |
| POST | `/reset/{code}` | reset submit |
| GET | `/verify-email/{code}` | email verification |

### Admin UI

| Method | Route | Usage |
| --- | --- | --- |
| GET | `/admin/login` | login |
| POST | `/admin/login` | authentication |
| POST | `/admin/logout` | logout |
| GET | `/admin/` | dashboard |
| GET | `/admin/users` | users |
| GET | `/admin/invitations` | invitations |
| GET | `/admin/settings` | settings |
| GET | `/admin/logs` | logs |
| GET | `/admin/automation` | automation |
| GET | `/admin/my-account` | user profile |

### Admin API

| Prefix | Description |
| --- | --- |
| `/admin/api/users` | user management |
| `/admin/api/invitations` | invitation CRUD, sponsor stats, anti-abuse security (`/security`) |
| `/admin/api/settings` | application settings |
| `/admin/api/backups` | backups |
| `/admin/api/logs` | audit logs and exports |
| `/admin/api/automation` | presets, mappings, tasks |

## 6. Database

Main tables:

- `users`
- `invitations`
- `pending_invite_signups`
- `password_resets`
- `email_verifications`
- `settings`
- `audit_log`

The project supports SQLite and PostgreSQL. SQLite is the simplest deployment target. PostgreSQL is useful when you need to separate persistence or scale the service.

## 7. Security

### 7.1 Measures in place

- admin auth delegated to Jellyfin
- HMAC-SHA256 signed session cookies
- CSRF middleware for mutable admin routes
- in-memory rate limiting middleware on login/invite/reset
- centralized HTTP headers: CSP, conditional HSTS, `X-Frame-Options`, `X-Content-Type-Options`, `Referrer-Policy`
- logging of sensitive actions

### 7.2 Open gaps

- `Secure` cookies still depend on `r.TLS != nil` on some paths and no uniform TLS proxy strategy
- LDAP/SMTP/Webhook secrets stored in clear text in `settings`
- `DB_SSLMODE=disable` remains the default for PostgreSQL
- no significant business test suite yet

## 8. User experience

The interface currently follows these principles:

- dark base theme with modern accents (cyan/emerald gradients)
- **Ghost modals**: resolved (CSS `display: none` by default + JS helpers)
- **Cluttered navigation**: resolved (collapsible sidebar + tab system)
- **Audit log**: `logs.html` reworked with horizontal filters to reduce density above the fold
- admin navigation no longer offers a messaging center; communications are email-only from `Users`
- on the login screen, the language selector and theme button are grouped under the login card for a clean, modern look

The shared design system lives in `web/static/css/custom.css` and `web/templates/layouts/base.html`.
- **Premium select**: use the `jg-select-premium` class for dropdowns to get the cyan-emerald accent and custom arrow.
- **CSP compliance**: no inline event handlers (`onclick`, etc.). Use event listeners in the `.js` files and `data-modal` attributes for modal actions.
- **Modals**: use `JG.openModal(id)` and `JG.closeModal(id)` to toggle `hidden` and `open` classes.

## 9. CI / Docker

The `docker-publish.yml` workflow publishes a multi-arch image:

- `linux/amd64`
- `linux/arm64`

Tags retained:

- `latest`
- `vX.Y.Z`

The workflow also runs the i18n check via `cmd/i18ncheck` to prevent missing keys, inconsistent placeholders, or fallback values.

## 10. Validation commands

```bash
npm run build:css
go build ./...
go test ./...
go run ./cmd/i18ncheck
go run ./cmd/i18ncoverage --max-same-as-base 195
docker build -t jellygate:local .
```

### Run locally (development mode)

To run the app locally without external dependencies (SQLite), copy/edit `.env.local` then run:

```bash
# Install CSS dependencies if needed
npm install
# Build Tailwind CSS
npm run build:css

# Ensure .env.local contains DB_TYPE=sqlite and JELLYGATE_PORT=8097
# Start the app (uses .env.local if you exported it in your environment)
go run ./cmd/jellygate

# Then open http://localhost:8097/admin/login in your browser
```

Notes:
- On Windows PowerShell, you can load variables from `.env.local` with a tool like `direnv` or set environment variables manually before running `go run`.
- The file `web/static/css/tailwind.generated.css` is already in the repo; if the UI looks black or empty, rebuild CSS with `npm run build:css` then refresh.

## 11. Watch points for future changes

- improve actual translation quality for non `fr`/`en`
- encrypt secrets stored in the database
- add tests for handlers and invitation/reset flows
- extend email verification into a more granular instance policy
- open the path to user sponsorship and direct admin user creation

## 12. Short-term product priorities

1. CAPTCHA / invitation anti-abuse
2. Markdown content studio
3. First-run assistant
4. Enriched admin timeline
5. Discord / Telegram / Matrix linking

## 13. Recent updates

- **Version 1.1.8**: Users page revamp (2026-03-28) - improved readability, better small-screen responsiveness, and fixes for missing JS `id` values (`bulk-selected-count`, `delete-modal-text`, `timeline-subtitle`). Compatibility with `web/static/js/pages/users.js` preserved; rerun the i18n audit after content changes.

- **Version 1.1.9**: Automation page revamp (2026-03-28) - tables made responsive (horizontal scroll via `overflow-x-auto`), tables forced to `min-w-full` to prevent column squashing on small screens, and minor accessibility/task preview improvements. No API changes: compatibility with `web/static/js/pages/automation.js` preserved. Perform a visual QA after restart.

- **Version 1.1.10**: Invitations page revamp (2026-03-28) - tables made responsive with `min-w-full`, added invitation policy summary and help messages (`invite-policy-summary`, `inv-uses-help`, `inv-link-expiry-help`, `inv-can-invite-help`), fixed quick buttons to avoid duplicate `id` values (added utility classes for listeners), and added a spot for delete confirmation text (`delete-modal-text`). Functional compatibility with `web/static/js/pages/invitations.js` preserved - run visual QA after restart.

- **Version 1.1.11**: Broken buttons/interactions fixed (2026-04-01) - three major fixes:
  1. **Users page** (`users.js` v3.0.0): full rewrite adding all missing event listeners - "Select all" checkbox (`check-all`), per-row checkboxes, "Bulk email" button (`btn-open-bulk-email`), bulk action drawer open/close, bulk action change with dynamic fields, full per-row actions (edit, delete, timeline, toggle active), edit modal, delete modal, timeline modal, advanced filters (Jellyfin, invitation, extras), and active filter indicators.
  2. **Invitations page** (`invitations.html`): "View all" and "New invitation" buttons used `id` values (`btn-scroll-invitations`, `btn-open-create-modal`) while `invitations.js` expected CSS classes (`.btn-scroll-invitations`, `.btn-open-create-modal`). Fixed by replacing IDs with classes.
  3. **Automation page** (`automation.html`): task creation modal (`modal-task-form`) and preset edit modal (`modal-preset-form`) lacked the `flex` class in their containers, preventing centering via `JG.openModal()`. Added `flex` to both modal containers.

- **Version 1.1.12**: Security and UI standardization (2026-04-02)
  - **Invitation security**: "Forced Username" field restricted to single-use invites (`max_uses = 1`) in frontend and backend. Unlimited invites blocked for non-admin users.
  - **UI standardization**: applied `jg-select-premium` style (teal arrow, dark mode options) to task type selectors.
  - **CSP fix**: removed all inline `onclick` handlers in `automation.html` and `invitations.html`. Migrated to event delegation in `automation.js` and `invitations.js` for modal open/close.
  - **Modal robustness**: updated `app.js` (`JG.closeModal`) to always add the `hidden` class and `display: none`.

- **Version 1.1.13**: Cleanup and Docker standardization (2026-04-04)
  - **Repo cleanup**: removed temporary files, logs, binaries, and obsolete test scripts.
  - **Docker standardization**: `docker-compose.yml` now optimized for SQLite by default (no unnecessary Postgres container). Added `docker-compose.postgres.yml` for PostgreSQL installs.
  - **Configuration**: updated `.env` comments and removed unused variables.
  - **Documentation**: refreshed project tree and agent guidance. Docker install is now the officially recommended method and is highlighted.

- **Version 1.3.0 / Agent memory**: Full Jellyfin presets without cloning (2026-04-29)
  - Added JSON blocks `user_configuration` and `display_preferences` to `JellyfinPolicyPreset`, with defaults and normalization in `internal/config/config.go` and `internal/database/settings.go`.
  - The Jellyfin client now applies a preset in three steps: rights/libraries (`Policy`), user configuration (`UserConfiguration`), and Jellyfin Web preferences (`DisplayPreferences.CustomPrefs`).
  - Added `GET /admin/api/automation/libraries`, wired to `/Library/VirtualFolders`, to select accessible libraries in `Automation > Presets`.
  - The preset modal no longer offers "Clone a Jellyfin profile"; `template_user_id` remains JSON legacy compatibility.
  - UI `Automation > Presets`: collapsible sections `Access` and `Personalization`; library table uses `Home: My media`, `Home: Latest`, `Group by type`, and `Position` via Move Up/Down buttons.
  - Important UX fix: do not allow multiple identical ranks for library order; order must match the physical row order in the table.
  - Main files touched: `internal/jellyfin/client.go`, `internal/config/config.go`, `internal/database/settings.go`, `internal/handlers/automation.go`, `internal/handlers/admin.go`, `internal/handlers/invitations.go`, `internal/scheduler/service.go`, `web/templates/admin/automation.html`, `web/static/js/pages/automation.js`, `web/i18n/*.json`.
  - Tests added: `internal/jellyfin/client_test.go` verifies Policy/UserConfiguration/DisplayPreferences payloads and no cloning; `internal/database/settings_presets_test.go` verifies defaults and normalization.
  - Validations run: `go test ./...`, `go build ./...`, `npm run build:css`, `node --check web/static/js/pages/automation.js`, i18n check for Automation keys across 10 languages, `git diff --check`.

- **Version 1.3.0 / Agent memory**: Product center removal and CAPTCHA move (2026-04-29)
  - The UI `Product Center` page and frontend assets are removed; do not reintroduce `/admin/product` or a generic catch-all panel.
  - Useful invitation anti-abuse settings live in `Admin > Invitations`, admin section `Signup security`.
  - Dedicated API: `GET /admin/api/invitations/security` and `POST /admin/api/invitations/security`, reading/saving only `ProductFeaturesConfig.AntiAbuse` while preserving the rest of the historical config.
  - CAPTCHA conditions: visible on `/invite/{code}` only when `enabled=true` and `captcha=true`; errors are tracked by IP and trigger a temporary block after the configured threshold.
  - Main files touched: `cmd/jellygate/main.go`, `internal/handlers/invitation_security.go`, `web/templates/admin/invitations.html`, `web/static/js/pages/invitations.js`, `web/templates/layouts/admin_shell.html`, `web/i18n/*.json`.
  - Tests added: `internal/handlers/invitation_security_test.go` verifies read/write and normalization of the anti-abuse config.
  - Validations run: `go test ./...`, `go build ./...`, `npm run build:css`, `go run ./cmd/i18ncheck`, `node --check web/static/js/pages/invitations.js`, `git diff --check`.

### 4.6 Internationalization (i18n)

- Supports 10 languages: French, English, German, Spanish, Italian, Dutch, Polish, Portuguese (Brazil), Russian, Chinese (Simplified).
- 100% coverage: all keys are synced across all languages.
- Smart fallback: missing key -> requested lang -> en -> fr -> key.
- Automated audit via `cmd/i18ncheck` to guarantee zero missing template keys.
- Key normalization to snake_case and removal of hardcoded strings in templates.

## i18n - verification and repair

- Common issue: translation JSON files must be UTF-8 (no BOM). If saved with the wrong encoding, the UI will show mojibake, especially in `zh.json`.
- Quick audit (detects missing keys and proposes a partial repair):

```powershell
# from the repo root
node scripts\\i18n_inspect.js
```

The script prints three blocks: `---ZH_FIXED---` (attempted mojibake decode for `zh.json`), `---MISSING_KEYS---` (missing keys per file), and `---ALL_KEYS---` (the full detected key set).

- Repair `zh.json` encoding (automatic attempt):

```powershell
# backup recommended first
node -e "const fs=require('fs');const p='web/i18n/zh.json';let raw=fs.readFileSync(p,'utf8');if(raw.charCodeAt(0)===0xFEFF) raw=raw.slice(1);let obj;try{obj=JSON.parse(raw)}catch(e){const maybe=Buffer.from(fs.readFileSync(p,'binary'),'binary').toString('utf8');obj=JSON.parse(maybe);}Object.keys(obj).forEach(k=>{if(typeof obj[k]==='string'){const dec=Buffer.from(obj[k],'binary').toString('utf8');if(/[\\u4e00-\\u9fff]/.test(dec)) obj[k]=dec;}});fs.writeFileSync(p,JSON.stringify(obj,null,4));console.log('zh.json: attempted repair (review required)');"
```

- Key parity: after any template change (new `{{ .T "..." }}`), ensure every `web/i18n/*.json` has the same keys. To auto-add missing keys, use the `---MISSING_KEYS---` output from `i18n_inspect.js` as a checklist, then add fallback values (English or primary language) and request human translations later.

- Editing tips:
    - Always save `.json` files as UTF-8 (no BOM).
    - For PowerShell 5, avoid `Set-Content -Encoding UTF8` (writes a BOM); prefer editors that explicitly write "UTF-8 without BOM", or the Node command above.
    - After fixes, restart the app and check `/admin/my-account` in `zh` to validate display.