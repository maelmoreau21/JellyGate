# JellyGate — Project Context

> Dernière mise à jour : 2026-03-07
> Version : 0.1.0-alpha
> Auteur : Mael Moreau

## 1. Vision

JellyGate est un portail d'administration et d'onboarding pour serveurs Jellyfin, pensé pour des déploiements self-hosted qui veulent:

- centraliser invitations, création de comptes et reset mot de passe
- intégrer nativement LDAP / Active Directory
- garder une stack simple à déployer en binaire Go ou Docker
- exposer une interface admin moderne sans dépendance frontend lourde

Le projet remplace l'approche jfa-go par une intégration plus directe avec Jellyfin, LDAP, la persistance SQL et les workflows d'automatisation maison.

## 2. Stack actuelle

| Domaine | Technologie |
|---|---|
| Backend | Go 1.22+, net/http, Chi v5 |
| Templates | `html/template` |
| Frontend | HTML, Tailwind build local, JS vanilla, CSS custom |
| Base | SQLite (`modernc.org/sqlite`) ou PostgreSQL |
| LDAP | `go-ldap/ldap/v3` |
| Jellyfin | API REST |
| Email | `wneessen/go-mail` |
| Notifications | Discord, Telegram, Matrix |
| CI/CD | GitHub Actions, Docker Buildx, GHCR |

Le CSS Tailwind est généré localement via `npm run build:css` et servi depuis `web/static/css/tailwind.generated.css`.

## 3. Arborescence logique

```text
cmd/
  i18ncheck/               # check i18n CI
  jellygate/               # point d'entrée HTTP
internal/
  backup/                  # sauvegarde / restauration
  config/                  # config runtime et structs métiers
  database/                # migrations, accès SQL, settings
  handlers/                # pages et API admin/public
  i18nreport/              # rapport qualité des traductions
  integrations/            # provisioning tiers
  jellyfin/                # client Jellyfin
  ldap/                    # client LDAP / AD
  mail/                    # mailer SMTP
  middleware/              # auth, i18n, sécurité, rate limit
  notify/                  # webhooks
  render/                  # moteur de rendu + traduction
  scheduler/               # tâches périodiques
  session/                 # cookies signés
web/
  i18n/                    # locales JSON
  static/                  # css, js, favicon
  templates/               # pages, layouts, emails
```

## 4. Capacités produit

### 4.1 Invitations

- codes uniques avec quota, expiration et label
- profils Jellyfin associés à l'invitation
- mapping groupe/preset d'automatisation
- flux atomique avec rollback LDAP/Jellyfin si une étape échoue
- corrélation audit par `request_id`
- base technique prête pour un futur mode parrainage utilisateur depuis `Mon compte`

### 4.2 Utilisateurs

- listing admin
- synchronisation Jellyfin
- suppression compte
- toggle d'accès
- profil personnel avec langue préférée et préférences de notification
- refonte admin en cours page par page pour homogénéiser toute l'interface

### 4.3 Réinitialisation mot de passe

- page publique de demande
- token/code temporaire
- update Jellyfin + LDAP
- anti-énumération côté message utilisateur

### 4.4 Vérification des canaux de contact

- statut vérifié / en attente exposé sur le profil utilisateur
- lien public `/verify-email/{code}` avec gestion des états valide / expiré / déjà utilisé / invalide
- envoi initial au signup et renvoi depuis `Mon compte`
- changement d'adresse géré via `pending_email` jusqu'à confirmation
- corps HTML et sujet de l'e-mail de vérification configurables depuis l'admin
- politique historique: les comptes déjà présents avant cette feature, avec e-mail existant et sans vérification en cours, sont marqués vérifiés une seule fois au démarrage
- objectif cible: étendre ensuite le même modèle de vérification à Discord / Telegram / Matrix

### 4.5 Automatisation et home server

- presets Jellyfin
- mappings groupes LDAP / groupes fonctionnels
- tâches planifiées
- provisioning Jellyseerr / Ombi optionnel

### 4.6 Audit et observabilité

- `audit_log` SQL
- filtres avancés sur l'API logs
- export CSV / JSON
- extraction et affichage `request_id`

### 4.7 i18n

- locales JSON sous `web/i18n`
- détection par cookie `lang`, puis `Accept-Language`, puis `default_lang`
- fallback `lang demandée -> en -> fr`
- commande CI `go run ./cmd/i18ncheck`

### 4.8 Roadmap produit validée

- parrainage utilisateur depuis `Mon compte` avec quotas, durée de vie et traçabilité sponsor -> invité
- vérification d'e-mail obligatoire ou configurable selon la politique d'instance
- création directe d'utilisateur côté admin avec preset complet, expiration et message de bienvenue
- centre de tâches manuelles pour lancer housekeeping, sync Jellyfin, sync intégrations et sauvegardes
- intégration Jellyseerr plus profonde: sync profil, préférences de notification et resync manuel
- contenu produit personnalisable depuis l'admin pour onboarding, aide post-inscription et messages réutilisables
- timeline utilisateur enrichie basée sur l'audit log existant

## 5. Routes importantes

### Public

| Méthode | Route | Usage |
|---|---|---|
| GET | `/invite/{code}` | page d'inscription |
| POST | `/invite/{code}` | validation inscription |
| GET | `/reset` | page de demande reset |
| POST | `/reset/request` | émission du reset |
| GET | `/reset/{code}` | formulaire nouveau mot de passe |
| POST | `/reset/{code}` | soumission reset |
| GET | `/verify-email/{code}` | validation d'adresse e-mail |

### Admin UI

| Méthode | Route | Usage |
|---|---|---|
| GET | `/admin/login` | login |
| POST | `/admin/login` | authentification |
| POST | `/admin/logout` | logout |
| GET | `/admin/` | dashboard |
| GET | `/admin/users` | utilisateurs |
| GET | `/admin/invitations` | invitations |
| GET | `/admin/messages` | messages |
| GET | `/admin/settings` | paramètres |
| GET | `/admin/logs` | journaux |
| GET | `/admin/automation` | automatisation |
| GET | `/admin/my-account` | profil utilisateur |

### Admin API

| Préfixe | Description |
|---|---|
| `/admin/api/users` | gestion utilisateurs |
| `/admin/api/invitations` | CRUD invitations |
| `/admin/api/messages` | centre de messages |
| `/admin/api/settings` | paramètres applicatifs |
| `/admin/api/backups` | sauvegardes |
| `/admin/api/logs` | audit logs et exports |
| `/admin/api/automation` | presets, mappings, tâches |

## 6. Base de données

Tables principales:

- `users`
- `invitations`
- `password_resets`
- `email_verifications`
- `settings`
- `audit_log`

Le projet supporte SQLite et PostgreSQL. SQLite reste la cible de déploiement la plus simple. PostgreSQL est utile quand on veut séparer la persistance ou scaler le service.

## 7. Sécurité

### 7.1 Mesures en place

- authentification admin déléguée à Jellyfin
- cookies de session signés HMAC-SHA256
- middleware CSRF pour les routes admin mutables
- middleware de rate limiting mémoire sur login/invite/reset
- headers HTTP centralisés: CSP, HSTS conditionnel, `X-Frame-Options`, `X-Content-Type-Options`, `Referrer-Policy`
- journalisation des actions sensibles

### 7.2 Écarts encore ouverts

- cookies `Secure` encore dépendants de `r.TLS != nil` sur certains chemins et pas de stratégie proxy TLS uniforme
- secrets LDAP/SMTP/Webhooks stockés en clair dans `settings`
- `DB_SSLMODE=disable` reste le défaut PostgreSQL
- pas encore de suite de tests métier significative

## 8. Expérience utilisateur

L'interface suit actuellement ces principes:

- fond noir conservé
- pages publiques centrées et simples
- sidebar admin fixe
- actions fréquentes mises en avant
- sélecteur de langue visible hors sidebar et intégré à l'admin quand la sidebar existe

Le design system partagé est porté par `web/static/css/custom.css` et `web/templates/layouts/base.html`.

## 9. CI / Docker

Le workflow `docker-publish.yml` publie une image multi-arch:

- `linux/amd64`
- `linux/arm64`

Tags conservés:

- `latest`
- `vX.Y.Z`

Le workflow exécute aussi le check i18n via `cmd/i18ncheck` pour empêcher l'introduction de clés manquantes, placeholders incohérents ou valeurs fallback.

## 10. Commandes de validation

```bash
npm run build:css
go build ./...
go test ./...
go run ./cmd/i18ncheck
```

## 11. Points d'attention pour les prochaines évolutions

- améliorer la qualité réelle des traductions non `fr`/`en`
- finir le durcissement proxy HTTPS / cookies sécurisés
- chiffrer les secrets stockés en base
- ajouter des tests de handlers et de flux invitation/reset
- étendre la vérification d'e-mail vers une politique d'instance configurable plus fine
- ouvrir la voie au parrainage utilisateur et à la création directe d'utilisateur par l'admin

## 12. Priorités produit court terme

1. Parrainage utilisateur depuis `Mon compte`
2. Création directe d'utilisateur côté admin
3. Centre de tâches manuelles
4. Intégration Jellyseerr enrichie
5. Politique avancée de vérification d'e-mail