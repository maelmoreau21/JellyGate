# Guide de Durcissement Opérationnel & Sécurité — JellyGate

Ce document fournit le référentiel d'application et de configuration pour sécuriser l'ensemble de la chaîne de services : **JellyGate**, **Authentik**, **Jellyfin** et le **Proxy Inverse (Nginx / Caddy)**.

---

## 1. Authentik (Fournisseur d'Identité & SSO)

Authentik assure l'authentification centrale (OIDC PKCE) et le flux d'inscription par invitation (`itoken`).

### A. Mise à niveau vers la version sécurisée
Pour corriger les CVE critiques récentes (CVE-2026-42849, CVE-2026-72534, CVE-2026-49443, CVE-2026-47201, CVE-2026-40165), mettez à jour votre image Docker vers **Authentik 2026.5.7 ou supérieure** :

```yaml
# docker-compose.yml (Authentik)
services:
  server:
    image: ghcr.io/goauthentik/server:2026.5.7
    restart: unless-stopped
    command: server
  worker:
    image: ghcr.io/goauthentik/server:2026.5.7
    restart: unless-stopped
    command: worker
```

Exécutez :
```bash
docker compose pull
docker compose up -d
```

### B. Mitigation SCIM (Élévation de privilèges)
Si vous n'utilisez pas de synchronisation SCIM avec un tiers cloud (Azure AD, Okta) :
- Accédez à **Authentik Admin Interface > Directory > SCIM**.
- Supprimez ou désactivez tout token ou mapping SCIM non utilisé pour neutraliser la surface d'attaque.

---

## 2. Jellyfin (Serveur Média & Outpost LDAP)

### A. Mise à jour de version
Mettez à jour votre conteneur vers **Jellyfin 10.11.10+** pour corriger les failles XSS (CVE-2026-49220), SSRF (CVE-2026-35032) et DoS/Spoofing (CVE-2025-32012) :
```yaml
services:
  jellyfin:
    image: jellyfin/jellyfin:10.11.10
```

### B. Configuration obligatoire des « Known Proxies »
Par défaut, si Jellyfin ne connaît pas le proxy inverse, il considère toutes les connexions externes comme locales (LAN), ce qui expose des privilèges réseau indus.

1. **Via le tableau de bord Jellyfin :**
   - Rendez-vous dans **Tableau de bord > Réseau**.
   - Dans le champ **Adresses IP des proxys inverses connus**, renseignez l'adresse IP de votre proxy ou les plages sous-réseau Docker (ex: `127.0.0.1, 172.16.0.0/12, 10.0.0.0/8`).
2. **Ou directement dans `system.xml` (`/config/system.xml`) :**
   ```xml
   <KnownProxies>
       <string>127.0.0.1</string>
       <string>::1</string>
       <string>172.16.0.0/12</string>
   </KnownProxies>
   ```

### C. Restriction de la gestion LiveTV
La faille SSRF de Jellyfin s'appuie sur `EnableLiveTvManagement`.
Dans JellyGate (*Administration > Automatisation / Profils*) :
- Veillez à ce que l'option **"Gérer la Live TV"** reste **décochée** pour les invités et utilisateurs standards.

---

## 3. JellyGate (Portail & Contrôle d'Accès)

### A. Absence d'injection LDAP (Clarification d'architecture)
JellyGate **ne contient aucun client LDAP direct, aucun bind LDAP, ni aucune requête LDAP**. L'identité est entièrement confiée à Authentik (SSOT) via OIDC. Jellyfin interroge quant à lui l'Outpost LDAP d'Authentik. JellyGate ne présente donc aucune surface d'attaque aux injections LDAP.

### B. Gestion des Secrets (`JELLYGATE_SECRET`)
- **Validation stricte au démarrage :** JellyGate refuse de démarrer si `JELLYGATE_SECRET` est absent, fait moins de 32 caractères, ou s'il utilise une valeur d'exemple prévisible (`change_this_to_a_secure_random_32_character_string`).
- **Génération d'une clé cryptographiquement sûre :**
  ```bash
  openssl rand -hex 32
  ```
- **Compte administrateur local de secours :** S'il est activé (`JELLYGATE_LOCAL_ADMIN_PASSWORD`), le mot de passe doit comporter **au moins 12 caractères** et ne pas être trivial.

### C. Proxys de Confiance (`JELLYGATE_TRUSTED_PROXIES`)
JellyGate implémente un filtre anti-spoofing d'adresses IP (`TrustedProxyRealIP`). Seuls les sauts provenant d'IPs de confiance peuvent transmettre `X-Forwarded-For` ou `X-Real-IP`.

Variables d'environnement dans `.env` :
```ini
# Active l'écoute des headers de proxy
JELLYGATE_TRUST_PROXY_HEADERS=true

# Liste des CIDRs/IPs de confiance autorisés comme proxy (défaut : loopback + RFC1918)
JELLYGATE_TRUSTED_PROXIES=127.0.0.1/32,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7
```

### D. En-têtes HTTP de Sécurité Nativement Actifs
JellyGate injecte automatiquement sur chaque réponse :
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Strict-Transport-Security: max-age=31536000; includeSubDomains; preload` (en HTTPS)
- `Permissions-Policy: geolocation=(), microphone=(), camera=(), payment=(), usb=(), display-capture=()`
- `Content-Security-Policy`: CSP stricte avec nonces cryptographiques dynamiques par page (`'nonce-...'`).
- `StaticFileFilter`: blocage automatique de tout fichier caché (`.*`), configuration (`.env*`), script ou sauvegarde (`*.bak`, `*.sql`, `*.db`).

---

## 4. Configurations Reverse Proxy Durcies

### Option 1 : Configuration Nginx durcie
```nginx
# Limitation de débit : 15 requêtes/s par IP
limit_req_zone $binary_remote_addr zone=jg_limit:10m rate=15r/s;

server {
    listen 443 ssl http2;
    server_name gate.example.com;

    ssl_certificate /etc/letsencrypt/live/gate.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/gate.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;

    # En-têtes de transport stricts
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-Frame-Options "DENY" always;

    # Interdiction d'accès aux fichiers cachés
    location ~ /\. {
        deny all;
        access_log off;
        log_not_found off;
    }

    location / {
        limit_req zone=jg_limit burst=25 nodelay;

        proxy_pass http://127.0.0.1:8097;
        proxy_http_version 1.1;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
    }
}
```

### Option 2 : Configuration Caddy durcie
```caddy
gate.example.com {
    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        X-Content-Type-Options "nosniff"
        X-Frame-Options "DENY"
        Referrer-Policy "strict-origin-when-cross-origin"
        Permissions-Policy "camera=(), microphone=(), geolocation=(), payment=(), usb=()"
        -Server
    }

    reverse_proxy 127.0.0.1:8097 {
        header_up Host {host}
        header_up X-Real-IP {remote_host}
        header_up X-Forwarded-For {remote_host}
        header_up X-Forwarded-Proto {scheme}
    }
}
```

---

## 5. Gestion des Fichiers Sensibles & Permissions

1. **Droits d'accès au fichier `.env` sur le serveur hôte :**
   ```bash
   chmod 600 .env
   chown 1000:1000 .env # ou l'utilisateur Docker dédié
   ```
2. **Conteneur Docker en lecture seule :**
   Le `docker-compose.yml` de JellyGate est déjà configuré avec :
   - `read_only: true`
   - `security_opt: [no-new-privileges:true]`
   - `cap_drop: [ALL]`
   - `user: "1000:1000"` (non-root)
3. **Journaux d'audit et logs système :**
   - Les fichiers de logs (`/data/logs/jellygate-*.log`) sont créés en mode `0600`.
   - Aucun mot de passe ni token secret n'est persisté dans la table `audit_log` ou dans les fichiers de logs.
