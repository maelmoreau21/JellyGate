# JellyGate — Rapport d'Audit Architectural avant Migration Authentik

**Document :** Rapport d'Audit Architectural & Plan Stratégique de Migration  
**Projet :** JellyGate (`github.com/maelmoreau21/JellyGate`)  
**Version du document :** 1.0  
**Auteur :** Architecte Logiciel Senior (Go, React, OIDC, LDAP, Authentik, Self-Hosted)  
**Date :** Août 2026  

---

## Sommaire

1. [Architecture Actuelle](#1-architecture-actuelle)
   * 1.1 [Vue d'ensemble et technologies](#11-vue-densemble-et-technologies)
   * 1.2 [Inventaire et responsabilités des modules](#12-inventaire-et-responsabilités-des-modules)
   * 1.3 [Flux de données actuels](#13-flux-de-données-actuels)
2. [Inventaire Complet des Fonctionnalités](#2-inventaire-complet-des-fonctionnalités)
3. [Analyse Approfondie de la Base de Données](#3-analyse-approfondie-de-la-base-de-données)
   * 3.1 [Schéma de données actuel](#31-schéma-de-données-actuel)
   * 3.2 [Analyse table par table](#32-analyse-table-par-table)
4. [Analyse de l'Authentification Actuelle](#4-analyse-de-lauthentification-actuelle)
   * 4.1 [Procédure de connexion](#41-procédure-de-connexion)
   * 4.2 [Moteur de session](#42-moteur-de-session)
   * 4.3 [Intégration LDAP / Active Directory](#43-intégration-ldap--active-directory)
   * 4.4 [Intégration Jellyfin](#44-intégration-jellyfin)
   * 4.5 [Représentation des utilisateurs](#45-représentation-des-utilisateurs)
5. [Architecture Cible (Authentik + JellyGate + Jellyfin)](#5-architecture-cible-authentik--jellygate--jellyfin)
   * 5.1 [Principes directeurs](#51-principes-directeurs)
   * 5.2 [Schéma de la cible](#52-schéma-de-la-cible)
   * 5.3 [Flux d'authentification OIDC](#53-flux-dauthentification-oidc)
   * 5.4 [Flux d'authentification Jellyfin via LDAP Outpost](#54-flux-dauthentification-jellyfin-via-ldap-outpost)
   * 5.5 [Flux de parrainage et provisioning API Authentik](#55-flux-de-parrainage-et-provisioning-api-authentik)
6. [Plan de Migration Détaillé (Sans Interruption)](#6-plan-de-migration-détaillé-sans-interruption)
   * 6.1 [Phase 1 : Infrastructure Authentik & LDAP Outpost](#61-phase-1--infrastructure-authentik--ldap-outpost)
   * 6.2 [Phase 2 : Migration des Identités Existantes](#62-phase-2--migration-des-identités-existantes)
   * 6.3 [Phase 3 : Reconfiguration de Jellyfin](#63-phase-3--reconfiguration-de-jellyfin)
   * 6.4 [Phase 4 : Implémentation OIDC dans JellyGate](#64-phase-4--implémentation-oidc-dans-jellygate)
   * 6.5 [Phase 5 : Provisioning via API Authentik](#65-phase-5--provisioning-via-api-authentik)
   * 6.6 [Phase 6 : Nettoyage et Dépréciation](#66-phase-6--nettoyage-et-dépréciation)
7. [Analyse des Risques et Plan d'Atténuation](#7-analyse-des-risques-et-plan-datténuation)
8. [Décision Finale et Recommandations](#8-décision-finale-et-recommandations)

---

## 1. Architecture Actuelle

### 1.1 Vue d'ensemble et technologies

JellyGate est une application monolithique modulaire écrite en **Go (version 1.26+)**. Elle sert de portail d'invitation, de gestion d'utilisateurs, de parrainage et de contrôle d'accès autour du serveur média **Jellyfin**.

#### Pile technologique actuelle :
* **Langage & Framework Backend :** Go 1.26 avec le routeur HTTP `github.com/go-chi/chi/v5`.
* **Base de données :** Abstraction SQL personnalisée sans ORM, supportant dynamiquement :
  * **SQLite** via le driver Go pur `modernc.org/sqlite` avec mode WAL (`PRAGMA journal_mode=WAL`),
  * **PostgreSQL** (version 9.6+) via le driver `github.com/jackc/pgx/v5`.
* **Rendu UI Frontend :** Rendu côté serveur (SSR) via le package Go `html/template`, stylisé avec du CSS personnalisé et Tailwind CSS, enrichi avec une gestion i18n multilingue dynamique (`web/i18n`).
* **Protocoles d'Annuaire & Intégrations :**
  * **LDAP / Active Directory :** Client LDAPS direct via `github.com/go-ldap/ldap/v3`.
  * **Jellyfin REST API :** Client HTTP REST sur mesure.
  * **Service Mail :** Client SMTP via `github.com/wneessen/go-mail`.
  * **Intégrations tierces :** Client REST pour Jellyseerr.

---

### 1.2 Inventaire et responsabilités des modules

La structure du répertoire `internal/` fait preuve d'une excellente séparation des responsabilités :

```
JellyGate Workspace Structure
├── cmd/
│   └── jellygate/              # Point d'entrée main.go, chargement des services, wiring des dépendances, graceful shutdown
├── internal/
│   ├── config/                 # Gestion de la configuration globale (variables d'environnement, validation, TLS)
│   ├── database/               # Layer d'accès SQL dual (SQLite / Postgres), migrations, KV store de paramètres
│   ├── session/                # Moteur de session sécurisé HMAC-SHA256 (cookie jellygate_session)
│   ├── middleware/             # Middlewares Chi (CSRF, RateLimit, SecurityHeaders, LogPanics, DetectLanguage, Auth)
│   ├── handlers/               # Contrôleurs HTTP (Auth, Admin, Invitations, User self-service, PasswordReset, Backup, Settings)
│   ├── jellyfin/               # Client API REST Jellyfin (Users/New, Policy, Libraries, AuthenticateByName)
│   ├── ldap/                   # Client LDAPS Active Directory / OpenLDAP (création comptes, unicodePwd, UAC, groupes)
│   ├── mail/                   # Client SMTP (go-mail) et générateur de templates d'emails transactionnels
│   ├── notify/                 # Moteur de notifications multi-canaux (Webhooks, Discord, Telegram, Matrix, In-App)
│   ├── integrations/           # Client de provisioning pour applications tierces (Jellyseerr API)
│   ├── scheduler/              # Service de tâches planifiées (cron) en arrière-plan
│   ├── backup/                 # Service d'export/import/restauration de sauvegardes ZIP
│   └── render/                 # Moteur de templates HTML et d'internationalisation (i18n)
└── web/                        # Templates HTML, assets statiques CSS/JS, email_templates, i18n JSON
```

#### Rôles détaillés des composants :
* **`cmd/jellygate/main.go`** : Instancie le logger structuré `slog`, charge la configuration, initialise la base de données, lance les migrations, instancie les clients de service (`jellyfin`, `ldap`, `mail`, `notify`, `integrations`), enregistre les middlewares, définit le routing Chi et gère le graceful shutdown (SIGINT/SIGTERM).
* **`database/database.go` & `settings.go`** : Fournit une connexion SQL unifiée (`DB`), réécrit dynamiquement les requêtes préparées pour PostgreSQL (`$1`, `$2`, `ON CONFLICT DO NOTHING`) et SQLite (`?`, `INSERT OR IGNORE`), exécute les migrations et gère la table clé-valeur `settings`.
* **`handlers/auth.go` & `session/session.go`** : Gère la page de connexion admin/user, authentifie l'utilisateur via l'API REST Jellyfin, vérifie les accès LDAP optionnels, et délivre le cookie signé `jellygate_session`.
* **`handlers/invitations.go` & `invite_abuse.go`** : Gère le cycle de vie complet des invitations : génération de codes, validation de quota des parrains, règles d'expiration, restrictions d'IP et adresses email jetables, et création atomique du compte dans Jellyfin, LDAP et la DB JellyGate.
* **`handlers/admin_my_invitations.go` & `admin_my_account.go`** : Fournit le tableau de bord utilisateur (self-service), permettant à chaque compte d'accéder à son arbre de parrainage, de créer des cartes d'invitation dans la limite de son quota et de consulter ses statistiques.
* **`ldap/client.go`** : Interagit directement avec Active Directory / OpenLDAP via LDAPS. Il gère la création de l'objet `user`, l'encodage du mot de passe en UTF-16LE pour `unicodePwd`, la manipulation du `userAccountControl` (512=activé, 514=désactivé), et l'affectation aux groupes AD (`jellyfin`, `jellyfin-Parrainage`, `jellyfin-administrateur`).
* **`jellyfin/client.go`** : Client HTTP REST pour la gestion des comptes sur Jellyfin (création, suppression, modification de la politique `Policy`, attribution des dossiers/bibliothèques et authentification).

---

### 1.3 Flux de données actuels

```
                  ┌─────────────────────────────────────────┐
                  │          Formulaire d'Inscription       │
                  │             (/invite/{code})            │
                  └────────────────────┬────────────────────┘
                                       │
                                       ▼
                  ┌─────────────────────────────────────────┐
                  │       Contrôle Anti-Abus & Quotas       │
                  │   (invite_abuse.go, database.users)     │
                  └────────────────────┬────────────────────┘
                                       │
            ┌──────────────────────────┼──────────────────────────┐
            ▼                          ▼                          ▼
┌───────────────────────┐  ┌───────────────────────┐  ┌───────────────────────┐
│     Création LDAP     │  │   Création Jellyfin   │  │   Enregistrement DB   │
│  (ldap/client.go)     │  │  (jellyfin/client.go) │  │  (database/database)  │
│  - unicodePwd         │  │  - POST /Users/New    │  │  - invited_by         │
│  - userAccountControl │  │  - Policy & Libraries │  │  - access_expires_at  │
│  - AD Groups          │  └───────────────────────┘  └───────────────────────┘
└───────────────────────┘
```

---

## 2. Inventaire Complet des Fonctionnalités

Le tableau ci-dessous classe l'intégralité des fonctionnalités de JellyGate selon la politique stricte de migration vers Authentik :

| Fonctionnalité | Module Concerné | Dépendances | Action | Raison / Justification |
| :--- | :--- | :--- | :---: | :--- |
| **Authentification Utilisateur / Admin** | `handlers/auth.go`, `session/` | Jellyfin API, LDAP | **MIGRATE TO AUTHENTIK** / **REFACTOR** | L'authentification primaire (login/password) est transférée à Authentik via OIDC (PKCE). JellyGate devient un RP OIDC. |
| **Gestion du MFA / Passkeys** | N/A (non présent dans JellyGate) | N/A | **MIGRATE TO AUTHENTIK** | Géré de façon native et centralisée par Authentik (TOTP, WebAuthn, FIDO2). |
| **Stockage des identifiants & Mots de passe** | `handlers/password_reset.go`, `ldap` | LDAP, Jellyfin API | **MIGRATE TO AUTHENTIK** | Authentik devient la source unique de vérité (SSOT) pour l'identité et les mots de passe. |
| **Self-Service Changement de Mot de Passe** | `handlers/admin_my_account.go` | LDAP, Jellyfin API | **MIGRATE TO AUTHENTIK** | Les utilisateurs sont redirigés vers le portail utilisateur Authentik pour modifier leurs identifiants. |
| **Réinitialisation de mot de passe (Reset Token)** | `handlers/password_reset.go` | SMTP, DB (`password_resets`) | **MIGRATE TO AUTHENTIK** | Authentik prend en charge les flux d'oubli et de réinitialisation de mot de passe par email. |
| **Vérification d'adresse email** | `handlers/email_verification.go` | SMTP, DB (`email_verifications`) | **MIGRATE TO AUTHENTIK** | La validation des adresses email s'effectue dans les étapes de flux d'inscription Authentik. |
| **Gestion & Écriture directe dans LDAP** | `internal/ldap` | LDAPS / Active Directory | **REMOVE** | JellyGate ne doit plus écrire dans LDAP. Authentik expose un **Outpost LDAP** pour la lecture par Jellyfin. |
| **Système de Parrainage (Arbre Parrain-Filleul)** | `handlers/admin_my_invitations.go`, `invitations.go` | DB (`users.invited_by`) | **KEEP** | **Logique métier cœur JellyGate.** Conserve la relation parent-enfant (`invited_by`), la généalogie et les statistiques. |
| **Quotas d'invitation & Gestion des Cartes** | `handlers/invitations.go`, `invitation_security.go` | DB (`invitations`, `users.can_invite`) | **KEEP** | **Logique métier cœur JellyGate.** Gère les codes d'invitation, le nombre maximum d'utilisations et la limite par parrain. |
| **Inscription sur Code d'Invitation** | `handlers/invitations.go` | Authentik REST API | **REFACTOR** | Lorsqu'un filleul s'inscrit, JellyGate valide le code/quota, puis crée le compte dans Authentik via l'API REST Authentik. |
| **Comptes Temporaires & Expiration Programmée** | `handlers/admin_users_api.go`, `scheduler` | DB (`users.access_expires_at`), Authentik API | **KEEP** / **REFACTOR** | JellyGate conserve la logique métier d'expiration temporaire, mais déclenche la désactivation/suppression dans Authentik via API. |
| **Dashboard Utilisateur (Self-service parrainage)** | `handlers/admin_my_account.go`, `admin_my_invitations.go` | DB, Render | **KEEP** | Le tableau de bord utilisateur JellyGate reste la vitrine du système de parrainage et des quotas (authentifié via OIDC). |
| **Dashboard Administration & Statistiques** | `handlers/admin.go`, `admin_users_api.go` | DB, Render | **KEEP** | Le panneau d'administration conserve la vue d'ensemble des utilisateurs, des invitations, des logs d'audit et de la sécurité. |
| **Moteur de Règles Anti-Abus** | `handlers/invite_abuse.go` | RateLimiter, Disposable Emails | **KEEP** | Filtre les tentatives d'inscription malveillantes sur les formulaires JellyGate avant la création d'identité dans Authentik. |
| **Audit Métier & Logs de Sécurité** | `handlers/security_events.go`, `database` | DB (`audit_log`, `security_events`) | **KEEP** | Conserve l'historique complet des actions métier (génération de liens, consommations de quotas, actions administrateur). |
| **Notifications Multi-Canaux (Webhooks, Discord, Telegram, Matrix)** | `internal/notify`, `scheduler` | HTTP Clients, DB | **KEEP** | Envoi des alertes lors de l'utilisation d'une invitation, de l'expiration d'un compte ou de campagnes d'annonces. |
| **Provisioning d'Apps Tierces (Jellyseerr)** | `internal/integrations` | Jellyseerr REST API | **KEEP** | Inscription automatique des nouveaux comptes sur Jellyseerr lors de la validation d'une invitation. |
| **Sauvegarde & Restauration DB** | `internal/backup` | ZIP, SQL | **KEEP** | Sauvegarde et restauration des données métier spécifiques à JellyGate. |
| **Attribution des Droits / Presets Jellyfin** | `handlers/automation.go` | Jellyfin API, Authentik API | **REFACTOR** | L'Outpost LDAP d'Authentik transmet les groupes aux instances Jellyfin. JellyGate gère les presets et applique les groupes Authentik via API. |

---

## 3. Analyse Approfondie de la Base de Données

### 3.1 Schéma de données actuel

Le schéma de données JellyGate est composé de **11 tables** gérées via SQL natif.

```
┌────────────────────────────────────────┐         ┌────────────────────────────────────────┐
│                 users                  │1       *│              invitations               │
├────────────────────────────────────────┼─────────┼────────────────────────────────────────┤
│ id (PK, AUTOINCREMENT/BIGSERIAL)       │         │ id (PK, AUTOINCREMENT/BIGSERIAL)       │
│ jellyfin_id (TEXT, UNIQUE)             │         │ code (TEXT, UNIQUE, NOT NULL)          │
│ username (TEXT, UNIQUE, NOT NULL)      │         │ label (TEXT)                           │
│ email (TEXT)                           │         │ max_uses (INTEGER, DEFAULT 1)          │
│ email_verified (BOOLEAN, DEFAULT 0)    │         │ used_count (INTEGER, DEFAULT 0)        │
│ pending_email (TEXT, DEFAULT '')       │         │ jellyfin_profile (TEXT)                │
│ ldap_dn (TEXT)                         │         │ preferred_lang (TEXT, DEFAULT '')      │
│ group_name (TEXT, DEFAULT '')          │         │ profile_id (TEXT, DEFAULT '')          │
│ contact_discord / telegram / matrix    │         │ profile_snapshot (TEXT, DEFAULT '')    │
│ opt_in_email / discord / telegram...   │         │ is_temporary (BOOLEAN, DEFAULT 0)      │
│ preset_id (TEXT)                       │         │ account_duration_days (INTEGER, DEF 0) │
│ invited_by (TEXT)                      │         │ expires_at (DATETIME)                  │
│ is_active / is_banned / can_invite     │         │ created_by (TEXT)                      │
│ access_expires_at / delete_at          │         │ created_at (DATETIME, DEFAULT NOW)     │
│ expiry_action / delete_after_days      │         └────────────────────────────────────────┘
│ profile_apply_status / error           │
│ created_at / updated_at                │         ┌────────────────────────────────────────┐
└────────────────────────────────────────┘         │                settings                │
                                                   ├────────────────────────────────────────┤
┌────────────────────────────────────────┐         │ key (TEXT, PRIMARY KEY)                │
│             password_resets            │         │ value (TEXT)                           │
├────────────────────────────────────────┼─────────│ updated_at (DATETIME, DEFAULT NOW)     │
│ id / user_id / code / expires_at / used│         └────────────────────────────────────────┘
└────────────────────────────────────────┘
                                                   ┌────────────────────────────────────────┐
┌────────────────────────────────────────┐         │               audit_log                │
│           email_verifications          │         ├────────────────────────────────────────┤
├────────────────────────────────────────┼─────────│ id / action / actor / target / details │
│ id / user_id / email / code / expires  │         └────────────────────────────────────────┘
└────────────────────────────────────────┘
                                                   ┌────────────────────────────────────────┐
┌────────────────────────────────────────┐         │            security_events             │
│        pending_invite_signups          │         ├────────────────────────────────────────┤
├────────────────────────────────────────┼─────────│ id / category / event_type / severity..│
│ id / code / invitation_code / username │         └────────────────────────────────────────┘
│ email / password_ciphertext / expires  │
└────────────────────────────────────────┘         ┌────────────────────────────────────────┐
                                                   │   user_messages / user_message_reads   │
                                                   └────────────────────────────────────────┘
```

---

### 3.2 Analyse table par table

#### 1. Table `users`
* **Utilité actuelle :** Répertoire principal des utilisateurs enregistrés dans JellyGate, assurant la correspondance avec l'ID Jellyfin (`jellyfin_id`), le DN LDAP (`ldap_dn`), le parrain (`invited_by`), les permissions d'inviter (`can_invite`), et les paramètres de fin de compte temporaire.
* **Données qui doivent rester (KEEP) :** `id`, `username`, `invited_by`, `can_invite`, `access_expires_at`, `delete_at`, `expiry_action`, `expiry_delete_after_days`, `expired_at`, `contact_discord`, `contact_telegram`, `contact_matrix`, `opt_in_email`, `opt_in_discord`, `opt_in_telegram`, `opt_in_matrix`, `preferred_lang`, `preset_id`, `is_active`, `is_banned`, `profile_apply_status`, `profile_apply_error`, `created_at`, `updated_at`.
* **Données qui deviennent inutiles :** `ldap_dn` (remplacé par l'ID Authentik), `pending_email` et `email_verification_sent_at` (la vérification d'email étant gérée par les flows d'Authentik).
* **Données à migrer / adapter :**
  * Ajout de la colonne `authentik_id TEXT UNIQUE` (contient l'UUID unique de l'utilisateur dans Authentik, correspondant au claim `sub` de l'ID Token OIDC).
  * La colonne `jellyfin_id` devient secondaire/optionnelle.
* **Risques de migration :** Risque de rupture de l'arbre de parrainage (`invited_by`) si les noms d'utilisateurs changent lors de l'importation dans Authentik. Atténuation : liaison par l'UUID `authentik_id`.

#### 2. Table `invitations`
* **Utilité actuelle :** Stocke les jetons/codes d'invitation, le créateur (`created_by`), le quota d'utilisations (`max_uses`, `used_count`), la durée d'expiration des comptes créés via cette invitation et le profil associé.
* **Données qui doivent rester (KEEP) :** **100% des colonnes.** (`id`, `code`, `label`, `max_uses`, `used_count`, `preferred_lang`, `profile_id`, `profile_snapshot`, `is_temporary`, `account_duration_days`, `expires_at`, `created_by`, `created_at`).
* **Données qui deviennent inutiles :** Aucune.
* **Données à migrer :** Le champ `profile_id` / `jellyfin_profile` sera interprété comme un ID de groupe Authentik ou un Preset JellyGate au lieu d'un profil Jellyfin direct.
* **Risques de migration :** Aucun risque majeur.

#### 3. Table `password_resets`
* **Utilité actuelle :** Stockage des tokens de réinitialisation de mot de passe par email.
* **Données qui doivent rester :** Aucune.
* **Données qui deviennent inutiles :** **Table totalement obsolète.**
* **Données à migrer :** Aucune.
* **Risques de migration :** Aucun (la table peut être dépréciée ou supprimée).

#### 4. Table `email_verifications`
* **Utilité actuelle :** Jetons de vérification d'adresse email post-inscription.
* **Données qui doivent rester :** Aucune.
* **Données qui deviennent inutiles :** **Table totalement obsolète** (gérée par Authentik).
* **Données à migrer :** Aucune.
* **Risques de migration :** Aucun.

#### 5. Table `pending_invite_signups`
* **Utilité actuelle :** Zone de transit temporaire des inscriptions en attente de confirmation par email avant création du compte dans Jellyfin/LDAP.
* **Données qui doivent rester / Refactor :** Peut être conservée si JellyGate effectue une pré-validation anti-abus avant de déclencher l'API Authentik.
* **Données qui deviennent inutiles :** `password_ciphertext` (JellyGate ne doit plus stocker ou manipuler des mots de passe chiffrés).

#### 6. Table `settings`
* **Utilité actuelle :** Stockage clé-valeur de la configuration système (SMTP, Webhooks, LDAP, sessions, backups).
* **Données qui doivent rester :** Configurations SMTP, Webhooks, Sauvegardes, Langue par défaut, Presets.
* **Données qui deviennent inutiles :** `ldap_host`, `ldap_port`, `ldap_bind_dn`, `ldap_bind_password`, `ldap_user_base_dn`.
* **Données à migrer / ajouter :** `authentik_url`, `authentik_api_token`, `oidc_client_id`, `oidc_client_secret`, `oidc_issuer`.

#### 7. Tables `audit_log`, `security_events`, `user_messages`, `user_message_reads`, `scheduled_tasks`
* **Utilité actuelle :** Journaux d'audit métier, événements de sécurité, annonces in-app et tâches d'arrière-plan.
* **Données qui doivent rester :** **100% conservées.** La tâche cron `sync_users` sera adaptée pour synchroniser l'état des comptes avec l'API Authentik.

---

## 4. Analyse de l'Authentification Actuelle

### 4.1 Procédure de connexion

L'authentification actuelle de JellyGate s'effectue dans `internal/handlers/auth.go` :

1. L'utilisateur saisit son `username` et son `password` sur la page `/admin/login`.
2. JellyGate transmet les identifiants au client Jellyfin via `h.jfClient.AuthenticateByName(username, password)`.
3. Jellyfin exécute l'authentification (en interne ou via son propre plugin LDAP) et renvoie l'objet utilisateur Jellyfin contenant l'ID Jellyfin et la politique `Policy.IsAdministrator`.
4. Si l'intégration LDAP est activée dans la configuration JellyGate (`ldapCfg.Enabled`), JellyGate exécute une seconde validation en interrogeant Active Directory via `ldapClient.ResolveUserAccess(username)` pour vérifier l'existence du compte et l'appartenance au groupe administrateur (`ldapIsAdmin`).
5. Si la réponse est favorable, JellyGate génère une session et positionne le cookie `jellygate_session`.

---

### 4.2 Moteur de session

La session JellyGate (`internal/session/session.go`) est basée sur un cookie sécurisé :
* **Format du Cookie :** `base64(payload).base64(hmac_signature)`
* **Structure du Payload :**
  ```go
  type Payload struct {
      UserID   string `json:"uid"` // ID Jellyfin
      Username string `json:"usr"` // Nom d'utilisateur
      IsAdmin  bool   `json:"adm"` // Statut Administrateur
      Exp      int64  `json:"exp"` // Timestamp Expiration
      Iat      int64  `json:"iat"` // Timestamp Création
  }
  ```
* **Sécurité :** Cookie `HttpOnly`, `SameSite=Strict`, `Secure` (si HTTPS). Validation de signature par HMAC-SHA256 avec la clé secrète `cfg.SecretKey`. Contrôle de révocation globale via le paramètre DB `auth_session`.

---

### 4.3 Intégration LDAP / Active Directory

JellyGate agit actuellement en **maître d'écriture direct sur LDAP** (`internal/ldap/client.go`) :
* Connexion TLS/LDAPS sur le port 636.
* Création de l'objet `user` dans Active Directory avec les classes `top`, `person`, `organizationalPerson`, `user`.
* Modification de l'attribut `unicodePwd` (mot de passe encodé en UTF-16LE entre guillemets).
* Activation du compte via le masque binaire `userAccountControl` (512 pour actif, 514 pour désactivé).
* Affectation aux groupes AD (`jellyfin`, `jellyfin-Parrainage`, `jellyfin-administrateur`).

---

### 4.4 Intégration Jellyfin

JellyGate interagit directement avec l'API REST Jellyfin (`internal/jellyfin/client.go`) :
* Création d'utilisateurs (`POST /Users/New`).
* Application de profils d'accès (`POST /Users/{Id}/Policy`).
* Récupération des bibliothèques virtuelles (`GET /Library/VirtualFolders`).

---

### 4.5 Représentation des utilisateurs

Chaque utilisateur est modélisé par un enregistrement dans la table `users` de JellyGate, identifié de façon unique par son `username` et son `jellyfin_id`.

---

## 5. Architecture Cible (Authentik + JellyGate + Jellyfin)

### 5.1 Principes directeurs

1. **Source Unique de Vérité (SSOT) :** Authentik gère à 100% les identités, adresses email, mots de passe, MFA, groupes et protocoles OIDC/LDAP.
2. **JellyGate = Application Métier :** JellyGate conserve l'intégralité de sa logique à valeur ajoutée (parrainages, quotas, arbre de parrainage, cartes d'invitation, règles anti-abus, expiration de comptes temporaires, notifications).
3. **Outpost LDAP Authentik :** Jellyfin s'authentifie auprès de l'Outpost LDAP d'Authentik. JellyGate n'interagit plus directement avec LDAP.

---

### 5.2 Schéma de la cible

```
                                  ┌───────────────────────────┐
                                  │         Utilisateur       │
                                  └─────────────┬─────────────┘
                                                │
                     ┌──────────────────────────┼──────────────────────────┐
                     │ (Navigation Web)         │ (Connexion Média)        │
                     ▼                          ▼                          ▼
            ┌─────────────────┐        ┌─────────────────┐        ┌─────────────────┐
            │    JellyGate    │        │    Authentik    │        │    Jellyfin     │
            │   (Métier App)  │        │   (Source ID)   │        │ (Serveur Média) │
            └────────┬────────┘        └────────┬────────┘        └────────┬────────┘
                     │                          │                          │
                     │  1. Redirection OIDC     │                          │
                     ├─────────────────────────►│                          │
                     │  2. Token Exchange PKCE  │                          │
                     │◄─────────────────────────┤                          │
                     │                          │                          │
                     │                          │  LDAP Bind / Search      │
                     │                          │◄─────────────────────────┤
                     │                          │  (Authentik Outpost)     │
                     │                          │                          │
                     │ 3. REST API Provisioning │                          │
                     ├─────────────────────────►│                          │
                     │   (Create User / Group)  │                          │
```

---

### 5.3 Flux d'authentification OIDC (JellyGate ◄► Authentik)

1. L'utilisateur tente d'accéder à JellyGate et clique sur "Connexion".
2. JellyGate génère un `code_verifier` PKCE et son `code_challenge` SHA-256, puis redirige le navigateur vers Authentik :
   `GET /application/o/authorize/?client_id=...&response_type=code&scope=openid+profile+email+groups&code_challenge=...`
3. L'utilisateur s'authentifie sur Authentik (login, mot de passe, MFA).
4. Authentik redirige l'utilisateur vers le callback JellyGate : `GET /auth/callback?code=...&state=...`.
5. JellyGate échange le `code` contre un `id_token` et un `access_token` sur l'endpoint `/application/o/token/` d'Authentik en fournissant le `code_verifier`.
6. JellyGate valide le JWT `id_token`, extrait le claim `sub` (UUID Authentik), le `preferred_username`, l'email, et la liste des `groups` (pour déterminer si l'utilisateur est admin).
7. JellyGate crée ou met à jour l'enregistrement dans sa table `users`, puis positionne le cookie de session interne `jellygate_session`.

---

### 5.4 Flux d'authentification Jellyfin via LDAP Outpost

1. L'utilisateur se connecte sur un client Jellyfin.
2. Le plugin LDAP de Jellyfin interroge l'**Outpost LDAP d'Authentik** (port 389/636).
3. Authentik valide l'identité et transmet les groupes virtuels de l'utilisateur.

---

### 5.5 Flux de parrainage et provisioning API Authentik

```
[Filleul] ──► Formulaire /invite/{code} ──► JellyGate (Vérifie Quota & Anti-Abus)
                                                  │
                                                  ▼
                                      Appel REST API Authentik
                                    POST /api/v3/core/users/
                                                  │
                                                  ▼
                                  Création Utilisateur + Groupes
                                                  │
                                                  ▼
                                Enregistrement DB JellyGate (invited_by)
```

1. Le filleul soumet le formulaire d'invitation sur JellyGate (`/invite/{code}`).
2. JellyGate valide le code d'invitation, vérifie le quota du parrain et exécute les règles anti-abus.
3. JellyGate appelle l'**API REST Authentik** (`POST /api/v3/core/users/`) avec le jeton d'API admin JellyGate pour créer le compte dans Authentik (attribution du username, email, et groupes Authentik correspondant au profil).
4. JellyGate enregistre l'utilisateur dans sa table locale `users` en renseignant `authentik_id` et la liaison `invited_by`.

---

## 6. Plan de Migration Détaillé (Sans Interruption)

```
Phase 1: Infrastructure Authentik & LDAP Outpost
  └─► Phase 2: Import & Sync des Identités
        └─► Phase 3: Transition Jellyfin LDAP Outpost
              └─► Phase 4: Implémentation OIDC dans JellyGate
                    └─► Phase 5: Provisioning API Authentik
                          └─► Phase 6: Nettoyage & Dépréciation
```

### 6.1 Phase 1 : Infrastructure Authentik & LDAP Outpost
* Déploiement d'Authentik (Docker Compose ou Kubernetes).
* Création de la Provider OIDC et de l'Application JellyGate dans Authentik.
* Déploiement de l'Outpost LDAP Authentik relié aux instances Jellyfin.

### 6.2 Phase 2 : Migration des Identités Existantes
* Écriture d'un script d'importation (Go ou Python) lisant les utilisateurs existants de JellyGate/Active Directory et créant les comptes correspondants dans Authentik via l'API REST.
* Envoi d'un mail de réinitialisation/définition de mot de passe Authentik aux utilisateurs importés, OU utilisation du Stage de Migration LDAP d'Authentik.

### 6.3 Phase 3 : Reconfiguration de Jellyfin
* Configuration du plugin LDAP Jellyfin pour pointer vers l'Outpost LDAP d'Authentik.
* Recette d'authentification sur Jellyfin pour valider que tous les utilisateurs accèdent à leurs contenus.

### 6.4 Phase 4 : Implémentation OIDC dans JellyGate
* Création du module OIDC dans JellyGate (`internal/oidc`).
* Ajout de la colonne `authentik_id` dans la table `users` de JellyGate.
* Implémentation du handler de callback `/auth/callback` et de la liaison automatique basée sur l'email/username lors de la première connexion.

### 6.5 Phase 5 : Provisioning via API Authentik
* Création du package `internal/authentik` dans JellyGate.
* Remplacement des appels `internal/ldap` lors de la validation d'invitation par les appels API Authentik.
* Adaptation du scheduler d'expiration temporaire pour désactiver/supprimer les comptes dans Authentik via l'API.

### 6.6 Phase 6 : Nettoyage et Dépréciation
* Suppression du package `internal/ldap`.
* Suppression des formulaires et handlers de réinitialisation de mot de passe obsolètes dans JellyGate.
* Nettoyage du schéma SQL (dépréciation des tables `password_resets` et `email_verifications`).

---

## 7. Analyse des Risques et Plan d'Atténuation

| Risque | Impact | Probabilité | Plan d'Atténuation & Stratégie |
| :--- | :---: | :---: | :--- |
| **Désynchronisation des identifiants (Username mismatch)** | **Élevé** | **Moyenne** | Associer les comptes existants lors du premier login OIDC en croisant le nom d'utilisateur et l'email vérifié, puis stocker le `authentik_id` (UUID immuable). |
| **Non-exportabilité des mots de passe AD/LDAP** | **Élevé** | **Haute** | Déclencher un flow de définition de mot de passe via Authentik lors de la première connexion, ou activer la source de migration LDAP Authentik. |
| **Rupture de l'arbre de parrainage (`invited_by`)** | **Élevé** | **Faible** | Conserver la table `users` et la colonne `invited_by` intactes dans JellyGate. La liaison repose sur les clés primaires JellyGate et l'UUID `authentik_id`. |
| **Indisponibilité de l'Outpost LDAP Authentik** | **Moyen** | **Faible** | Déployer l'Outpost LDAP Authentik avec des répliques ou un redémarrage automatique conteneurisé. |
| **Attaques CSRF sur le callback OIDC** | **Moyen** | **Faible** | Implémenter systématiquement le protocole **PKCE** (`code_challenge` / `code_verifier`) et un cookie `state` signé cryptographiquement. |
| **Évolution de l'API REST v3 Authentik** | **Faible** | **Faible** | Encapsuler tous les appels Authentik dans le package isolé `internal/authentik` avec un typage strict et une gestion d'erreurs complète. |
| **Rollback en cas d'échec de migration** | **Élevé** | **Faible** | Réaliser un snapshot/backup complet de la base JellyGate (`jellygate.db` ou PostgreSQL dump) avant la Phase 4. JellyGate conservant ses tables métier, un rollback vers l'ancienne version reste possible. |

---

## 8. Décision Finale et Recommandations

### Recommandation Stratégique : **Refactor Structuré du Projet Actuel**

#### Pourquoi éliminer la réécriture complète (*Rewrite*) ?
Une réécriture intégrale représenterait un coût important sans bénéfice réel :
1. L'architecture backend de JellyGate (Go 1.26, Chi v5, SQL natif dual SQLite/Postgres) est moderne, performante et propre.
2. Les sous-systèmes métier (moteur de parrainage, quotas, anti-abus, notifications multi-canaux, scheduler, backup, rendu HTML & i18n) sont **100% fonctionnels** et indépendants d'Authentik.

#### Pourquoi éliminer un Fork ?
Vous êtes le propriétaire du dépôt `github.com/maelmoreau21/JellyGate`. Un fork créerait une dette technique et une dispersion inutile.

#### Feuille de route du Refactor :
1. Créer le package `internal/authentik` (client API REST Authentik pour la création et la désactivation d'utilisateurs).
2. Créer le package `internal/oidc` (gestion du flux OIDC Authorization Code + PKCE).
3. Supprimer le package `internal/ldap` une fois la bascule effectuée.
4. Conserver l'intégralité du code métier JellyGate (parrainage, quotas, notifications, dashboard).

---

> [!IMPORTANT]
> **Prochaine étape conseillée :**  
> Valider la stratégie de création d'utilisateurs via l'API Authentik (définition directe du mot de passe vs envoi d'un email de bienvenue par Authentik) avant d'engager le développement du package `internal/authentik`.
