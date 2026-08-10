# Backup & Rollback Procedures — JellyGate v2.0.0

This document outlines standard operational procedures for backups, disaster recovery, and system rollback.

---

## 1. Backup Procedure

JellyGate provides both automated background backups and manual export endpoints.

### Automatic Backups (Scheduled)

Automated database backups are run according to the retention schedule configured in **Settings → Backup**. Backups are stored in `$JELLYGATE_DATA_DIR/backups/`.

### Manual Backup via API / Admin UI

1. Log into JellyGate as an administrator.
2. Go to **Settings → Backup & Maintenance**.
3. Click **Create Backup**. A ZIP archive containing `jellygate.db` and configuration metadata will be generated.
4. Alternatively, use the REST API:
   ```bash
   POST /admin/api/backups/create
   ```

### Manual File-Level Backup

For SQLite deployments:
```bash
# Safely snapshot SQLite database
sqlite3 /data/jellygate.db ".backup '/data/backups/jellygate_manual_$(date +%Y%m%d_%H%M%S).db'"
```

For PostgreSQL deployments:
```bash
pg_dump -U jellygate -h localhost jellygate > /data/backups/jellygate_pg_$(date +%Y%m%d_%H%M%S).sql
```

---

## 2. Restore & Disaster Recovery Procedure

### Restoring from Admin UI
1. Go to **Settings → Backup & Maintenance**.
2. Select a backup from the history list or upload a backup ZIP file.
3. Click **Restore**.

### Restoring from Command Line (SQLite)
```bash
docker compose stop jellygate
cp /data/backups/jellygate_manual_YYYYMMDD_HHMMSS.db /data/jellygate.db
docker compose start jellygate
```

---

## 3. Rollback Procedure (v2.0.0 → v1.x Emergency Fallback)

If a critical issue occurs during migration that requires rolling back to v1.x:

1. **Stop v2.0.0 container**:
   ```bash
   docker compose down
   ```

2. **Restore v1.x Database & Environment**:
   ```bash
   cp /data/jellygate.db.bak_v1 /data/jellygate.db
   cp .env.v1.bak .env
   ```

3. **Revert Docker Image Tag**:
   In `docker-compose.yml`, change image to `ghcr.io/maelmoreau21/jellygate:1.6.0`.

4. **Restart Container**:
   ```bash
   docker compose up -d --force-recreate
   ```
