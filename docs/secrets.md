# Secrets & Environment — Guidelines

Do not commit secrets (API keys, private keys, secret tokens) into the repository. Use the following patterns to keep secrets safe locally and in CI:

- Keep example/templated files in the repo: ` .env.example`.
- Keep real secrets in a local file ignored by Git: ` .env.local` (already in `.gitignore`).
- Use the provided pre-commit hook to avoid accidentally committing secrets.

## Generating `JELLYGATE_SECRET_KEY`

Recommended length: 32 bytes (hex encoded → 64 hex chars).

Linux/macOS (openssl):

```bash
openssl rand -hex 32
```

Portable Python (works on Windows):

```bash
python - <<'PY'
import os,binascii
print(binascii.hexlify(os.urandom(32)).decode())
PY
```

Copy the result into ` .env.local` as `JELLYGATE_SECRET_KEY=<generated>`.

## Git hooks

This repository ships a ` .githooks/` folder with a `pre-commit` hook that runs a simple Node script to detect common env keys in staged files. To enable it locally:

```bash
git config core.hooksPath .githooks
# On Unix-like systems, make sure the hook is executable:
chmod +x .githooks/pre-commit
```

On Windows, PowerShell hook (`.githooks/pre-commit.ps1`) is provided and `git config` still applies.

## CI / Production

- Do not store secrets in the repository or in container images.
- Use a secrets manager (HashiCorp Vault, AWS Secrets Manager, Azure Key Vault, etc.) for production secrets.
- In CI, inject secrets as environment variables or via the CI provider's secrets storage.

## Scripts

- `npm run secrets:check` — locally scan staged files for incriminating `JELLYGATE_SECRET_KEY` / `JELLYFIN_API_KEY` entries.
- `npm run hooks:install` — convenience script to set `core.hooksPath` to `.githooks` (runs `git config core.hooksPath .githooks`).
