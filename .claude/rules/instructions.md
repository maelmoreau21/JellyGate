# CLAUDE — Consignes d'agent et Contexte du projet JellyGate
<p align="center">
  <img src="../../logo.svg" width="128" height="128" alt="JellyGate Logo">
</p>

Dernière mise à jour : 2026-04-26 (Version Finale)

Objectif
- Fournir des directives concises et exploitables pour un assistant Claude travaillant sur ce dépôt, et rassembler le contexte produit/technique dans un seul fichier.

Règles essentielles
- **Version Finale** : Le projet est en phase finale. Prioriser la stabilité, la performance et la cohérence visuelle.
- Lire et utiliser la section "Contexte du projet" (plus bas dans ce fichier) comme source d'autorité avant toute décision, modification de code, ou proposition de PR.
- Découvrir d'abord les conventions du projet : Go 1.26+, `html/template`, i18n JSON sous `web/i18n`, Tailwind CSS (build local via `npm run build:css`).
- Proposer un plan succinct (3–6 étapes) pour toute tâche non triviale et utiliser la TODO/tool de suivi pour garder la trace des étapes.
- Ne pas modifier la section "Contexte du projet" sans accord explicite : proposer les changements en commentaire ou en PR.
- Principe "Link, don't embed" : lier la documentation existante plutôt que la dupliquer.
- **Docker First** : L'installation via **Docker** est la seule méthode officiellement recommandée pour la production. Toute modification impactant le déploiement doit être impérativement vérifiée dans `docker-compose.yml` et le `Dockerfile`.
- Préserver la compatibilité i18n : après toute modification de templates ou labels, vérifier chaque fichier `web/i18n/*.json` (10 langues : fr, en, de, es, it, nl, pl, pt-br, ru, zh) et lancer `go run ./cmd/i18ncheck` si pertinent.
- Tests et validations locales recommandés : exécuter `go build ./...` et `go test ./...` après modifications Go ; pour le CSS, `npm run build:css`.
- Encodage : sauvegarder les fichiers JSON en UTF-8 sans BOM (important pour `zh.json`).
- Sécurité : ne jamais committer de secrets en clair dans le dépôt.

- Ne pas committer ni pousser de modifications sur le dépôt, sauf demande explicite de l'utilisateur. Cela inclut les commits, pushes et la création de tags : n'agissez pas sur le dépôt sans instruction claire.

Commandes rapides utiles
- `npm run build:css`
- `go build ./...`
- `go test ./...`
- `go run ./cmd/i18ncheck`

Exemples de prompts (modèles)
- "Avant toute modification, propose un plan en 3 étapes et liste les fichiers à modifier."
- "Prépare le diff proposé pour une PR, liste les tests à exécuter et les commandes de validation." 
- "Vérifie la parité i18n pour la clé 'invite-policy-summary' et propose des corrections si des clés manquent."

Comportement attendu
- Reste bref, factuel et orienté action.
- Pose des questions de clarification pour tout changement impactant.
- Proposer PRs/patches quand possible et énumérer les commandes de validation.

Notes
- Ce fichier suit le bootstrap décrit dans `init.prompt.md`: découvrir les conventions, explorer le code, générer ou fusionner (préférer les liens), et itérer avec retours.

Si vous souhaitez des règles supplémentaires (hooks git, conventions de commit, checks CI), indiquez lesquelles et je les ajouterai ici.

---

<!-- Contenu importé : Project Context (project_context.md) -->

# JellyGate — Project Context

> Dernière mise à jour : 2026-04-06
> Version : 1.1.14
> Auteur : Mael Moreau

## 1. Vision

JellyGate est un portail d'administration et d'onboarding pour serveurs Jellyfin, pensé pour des déploiements self-hosted qui veulent :

- centraliser invitations, création de comptes et reset mot de passe
- intégrer nativement LDAP / Active Directory
- garder une stack simple à déployer en binaire Go ou Docker
- exposer une interface admin moderne sans dépendance frontend lourde

Le projet remplace l'approche jfa-go par une intégration plus directe avec Jellyfin, LDAP, la persistance SQL et les workflows d'automatisation maison.

## 2. Stack actuelle

- Backend : Go 1.26+, net/http, Chi v5
- Templates : `html/template`
- Frontend : HTML, Tailwind build local, JS vanilla, CSS custom
- Base : SQLite (`modernc.org/sqlite`) ou PostgreSQL
- LDAP : `go-ldap/ldap/v3`
- Jellyfin : API REST
- Email : `wneessen/go-mail`
- Notifications : Discord, Telegram, Matrix
- CI/CD : GitHub Actions, Docker Buildx, GHCR

Le CSS Tailwind est généré localement via `npm run build:css` et servi depuis `web/static/css/tailwind.generated.css`.

## 3. Arborescence logique

```text
cmd/
	generate_session/        # outil de generation de secret
	i18ncheck/               # check i18n CI
	i18ncoverage/            # stats couverture i18n
	jellygate/               # point d'entrée HTTP
internal/
	backup/                  # sauvegarde / restauration
	config/                  # config runtime et structs métiers
	database/                # migrations, accès SQL, settings
	handlers/                # pages et API admin/public
	integrations/            # provisioning tiers
	jellyfin/                # client Jellyfin
	ldap/                    # client LDAP / AD
	mail/                    # mailer SMTP
	middleware/              # auth, i18n, sécurité, rate limit
	notify/                  # webhooks
	render/                  # moteur de rendu + traduction
	scheduler/               # tâches périodiques
	session/                 # cookies signés
scripts/
	build-css.js             # build Tailwind
	i18n_inspect.js          # audit manuel i18n
	run_screenshots.ps1      # outil de screenshots
	screenshots.js           # moteur screenshots (puppeteer)
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
- vérification d'e-mail configurable au niveau de la politique d'invitation, activée par défaut
- si la vérification d'e-mail est activée, le compte n'est créé qu'après confirmation du lien public `/verify-email/{code}`
- corrélation audit par `request_id`
- l'e-mail d'invitation n'ajoute plus de bloc d'expiration si aucun délai n'est défini
- les blocs d'aide et d'expiration peuvent être désactivés depuis l'admin
- un toggle permet de purger automatiquement les liens expirés ou épuisés
- un "Nom d'utilisateur réservé" (Forced Username) ne peut être défini que pour les invitations à usage unique (`max_uses = 1`) afin d'éviter les conflits de création de compte
- les utilisateurs non-admins ne peuvent plus créer d'invitations à usage illimité (`max_uses` doit être >= 1)
- base technique prête pour un futur mode parrainage utilisateur depuis `Mon compte`

### 4.2 Utilisateurs

- listing admin
- synchronisation Jellyfin
- suppression compte
- toggle d'accès
- communication ciblée et envois admin centralisés depuis la page `Utilisateurs`
- aucune messagerie interne exposée aux utilisateurs: la communication produit se fait uniquement par e-mail
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
- pour les inscriptions publiques, un enregistrement temporaire est conservé jusqu'à confirmation afin d'éviter toute création de compte avant validation de l'e-mail
- changement d'adresse géré via `pending_email` jusqu'à confirmation
- corps HTML et sujet de l'e-mail de vérification configurables depuis l'admin
- politique historique: les comptes déjà présents avant cette feature, avec e-mail existant et sans vérification en cours, sont marqués vérifiés une seule fois au démarrage
- objectif cible: étendre ensuite le même modèle de vérification à Discord / Telegram / Matrix

### 4.5 Automatisation et home server

- presets Jellyfin
- mappings groupes LDAP / groupes fonctionnels
- tâches planifiées
- provisioning Jellyseerr optionnel

### 4.6 Modeles e-mail

- page `Parametres > Modeles e-mail` recentrée sur les seuls modèles utiles au produit
- chaque modèle est édité via un bloc déroulant, ce qui évite les longues pages difficiles à relire
- les éditeurs "Messages simples sans balises" acceptent désormais les variables de template comme `{{.Username}}`, `{{.Email}}`, `{{.ExpiryDate}}`, `{{.InviteLink}}` ou `{{.JellyfinURL}}`
- un sélecteur `cliquer pour inserer` permet d'ajouter directement les variables dans chaque message no-code
- les messages simples par défaut sont plus personnalisés et restent injectés automatiquement dans l'habillage partagé et les blocs système
- les rappels d'expiration utilisent un seul modèle cohérent, quel que soit le palier de rappel choisi
- panneau d'activation conservé pour couper proprement certains e-mails automatiques
- contenu d'aide avant/apres inscription simplifie pour eviter les messages type documentation interne
- aucun e-mail d'ajustement d'expiration n'est envoyé quand une expiration utilisateur est retirée
- le comportement produit privilègie l'envoi utile uniquement, sans fragments "Non definie"

### 4.7 Audit et observabilite

- `audit_log` SQL
- filtres avancés sur l'API logs
- export CSV / JSON
- extraction et affichage `request_id`

### 4.8 i18n

- locales JSON sous `web/i18n`
- détection par cookie `lang`, puis `Accept-Language`, puis `default_lang`
- fallback `lang demandée -> en -> fr`
- commande CI `go run ./cmd/i18ncheck`
- labels de navigation et de `Modeles e-mail` homogenises sur les locales admin principales

### 4.9 Roadmap produit validee

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
| --- | --- | --- |
| GET | `/invite/{code}` | page d'inscription |
| POST | `/invite/{code}` | validation inscription |
| GET | `/reset` | page de demande reset |
| POST | `/reset/request` | émission du reset |
| GET | `/reset/{code}` | formulaire nouveau mot de passe |
| POST | `/reset/{code}` | soumission reset |
| GET | `/verify-email/{code}` | validation d'adresse e-mail |

### Admin UI

| Méthode | Route | Usage |
| --- | --- | --- |
| GET | `/admin/login` | login |
| POST | `/admin/login` | authentification |
| POST | `/admin/logout` | logout |
| GET | `/admin/` | dashboard |
| GET | `/admin/users` | utilisateurs |
| GET | `/admin/invitations` | invitations |
| GET | `/admin/settings` | paramètres |
| GET | `/admin/logs` | journaux |
| GET | `/admin/automation` | automatisation |
| GET | `/admin/my-account` | profil utilisateur |

### Admin API

| Préfixe | Description |
| --- | --- |
| `/admin/api/users` | gestion utilisateurs |
| `/admin/api/invitations` | CRUD invitations |
| `/admin/api/settings` | paramètres applicatifs |
| `/admin/api/backups` | sauvegardes |
| `/admin/api/logs` | audit logs et exports |
| `/admin/api/automation` | presets, mappings, tâches |

## 6. Base de données

Tables principales:

- `users`
- `invitations`
- `pending_invite_signups`
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

- fond noir conservé avec des touches de modernité (dégradés Cyan/Émeraude)
- pages- **Modales Fantômes** : Résolu (CSS `display: none` par défaut + JS helpers).
- **Navigation Encombrée** : Résolu (Sidebar rétractable + Tab system).
- **Audit Log** : Refonte de la page `logs.html` avec filtres horizontaux.
 pour réduire la densité d'information au premier écran
- la navigation admin ne propose plus de centre de messages, les communications partant uniquement par e-mail depuis `Utilisateurs`
- sur l'écran de connexion, le selecteur de langue et le bouton de thème sont regroupés sous la carte de connexion pour un aspect épuré et moderne.

Le design system partagé est porté par `web/static/css/custom.css` et `web/templates/layouts/base.html`.
- **Select Premium** : Utiliser la classe `jg-select-premium` pour les menus déroulants afin de bénéficier de l'accentuation Cyan-Émeraude et de la flèche personnalisée.
- **Conformité CSP** : Interdiction des gestionnaires d'événements inline (`onclick`, etc.). Utiliser des écouteurs d'événements dans les fichiers `.js` correspondants et des attributs `data-modal` pour les interactions de modales.
- **Modales** : Utiliser `JG.openModal(id)` et `JG.closeModal(id)` pour gérer l'affichage via les classes `hidden` et `open`.

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
go run ./cmd/i18ncoverage --max-same-as-base 195
docker build -t jellygate:local .
```

### Lancer localement (mode développement)

Pour exécuter l'application localement sans dépendances externes (base SQLite), copiez/éditez `.env.local` puis lancez :

```bash
# Installer les dépendances CSS si nécessaire
npm install
# Générer le CSS Tailwind
npm run build:css

# Vérifier que .env.local contient DB_TYPE=sqlite et JELLYGATE_PORT=8097
# Démarrer l'application (utilise .env.local si vous l'avez exporté dans l'environnement)
go run ./cmd/jellygate

# Ensuite ouvrez http://localhost:8097/admin/login dans votre navigateur
```

Remarques:
- Si vous utilisez Windows PowerShell, vous pouvez charger les variables de `.env.local` avec un outil comme `direnv` ou définir manuellement les variables d'environnement avant d'exécuter `go run`.
- Le fichier `web/static/css/tailwind.generated.css` est déjà présent dans le dépôt; si l'interface apparaît noire ou vide, reconstruisez le CSS avec `npm run build:css` puis rechargez la page.

## 11. Points d'attention pour les prochaines évolutions

- améliorer la qualité réelle des traductions non `fr`/`en`
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

## 13. Mise à jour récente

- **Version 1.1.8** : Refonte de la page `Utilisateurs` (2026-03-28) — amélioration de la lisibilité, meilleure réactivité sur petits écrans, et correction de quelques `id` manquants nécessaires au JS (`bulk-selected-count`, `delete-modal-text`, `timeline-subtitle`). Compatibilité avec `web/static/js/pages/users.js` préservée ; relancer l'audit i18n après modifications de contenu.

- **Version 1.1.9** : Refonte de la page `Automatisation` (2026-03-28) — tables rendues responsive (scroll horizontal via `overflow-x-auto`), tables forcées en `min-w-full` pour éviter l'écrasement des colonnes sur petits écrans, et petites améliorations d'accessibilité/aperçu des tâches. Aucune modification API : compatibilité avec `web/static/js/pages/automation.js` conservée. Effectuer une vérification visuelle après redémarrage.

- **Version 1.1.10** : Refonte de la page `Invitations` (2026-03-28) — tables rendues responsive et `min-w-full` appliqué, ajout d'un résumé de politique d'invitation et de messages d'aide (`invite-policy-summary`, `inv-uses-help`, `inv-link-expiry-help`, `inv-can-invite-help`), correction des boutons rapides pour éviter les `id` dupliqués (ajout de classes utilitaires pour attacher les listeners), et ajout d'un emplacement pour le texte de confirmation de suppression (`delete-modal-text`). Compatibilité fonctionnelle avec `web/static/js/pages/invitations.js` préservée — lancer une QA visuelle après redémarrage.

- **Version 1.1.11** : Correction des boutons et interactions cassés (2026-04-01) — Trois corrections majeures :
  1. **Page Utilisateurs** (`users.js` v3.0.0) : réécriture complète ajoutant tous les event listeners manquants — checkbox « Sélectionner tout » (`check-all`), checkboxes individuelles par ligne, bouton « E-mail en lot » (`btn-open-bulk-email`), ouverture/fermeture du drawer d'actions en lot, changement d'action bulk avec rendu dynamique des champs, gestion complète des actions par ligne (éditer, supprimer, timeline, toggle actif), modal d'édition, modal de suppression, modal timeline, filtres avancés (Jellyfin, invitation, extras) et indicateurs de filtres actifs.
  2. **Page Invitations** (`invitations.html`) : les boutons « Voir tout » et « Nouvelle invitation » utilisaient des `id` (`btn-scroll-invitations`, `btn-open-create-modal`) alors que le JS `invitations.js` attendait des classes CSS (`.btn-scroll-invitations`, `.btn-open-create-modal`). Correction en remplaçant les ID par les classes correspondantes.
  3. **Page Automatisation** (`automation.html`) : les modales de création de tâche (`modal-task-form`) et d'édition de preset (`modal-preset-form`) n'avaient pas la classe `flex` dans leur conteneur, empêchant le centrage correct via `JG.openModal()`. Ajout de `flex` aux classes CSS des deux modales.

- **Version 1.1.12** : Sécurisation et Standardisation UI (2026-04-02)
  - **Sécurité Invitations** : Restriction du champ "Nom d'utilisateur réservé" aux invitations à usage unique (`max_uses = 1`) côté frontend et backend. Interdiction des invitations illimitées pour les non-administrateurs.
  - **Standardisation UI** : Application du style `jg-select-premium` (flèche accentuée teal, options dark mode) aux sélecteurs de type de tâche.
  - **Correctif CSP** : Suppression totale des handlers `onclick` dans `automation.html` et `invitations.html`. Migration vers une délégation d'événements dans `automation.js` et `invitations.js` pour l'ouverture/fermeture des modales.
  - **Robustesse Modales** : Mise à jour de `app.js` (`JG.closeModal`) pour garantir l'ajout systématique de la classe `hidden` et du style `display: none`.

- **Version 1.1.13** : Grand Nettoyage et Standardisation Docker (2026-04-04)
  - **Cleanup Repo** : Suppression massive des fichiers temporaires, logs, binaires et scripts de test obsolètes pour plus de clarté.
  - **Standardisation Docker** : `docker-compose.yml` désormais optimisé pour SQLite par défaut (plus de conteneur Postgres inutile). Introduction de `docker-compose.postgres.yml` pour les installations PostgreSQL.
  - **Configuration** : Mise à jour de `.env` avec des commentaires plus clairs et suppression des variables inutilisées.
  - **Documentation** : Actualisation de l'arborescence projet et des consignes d'agent. L'installation via Docker est désormais la méthode officiellement recommandée et mise en avant.

### 4.6 Internationalisation (i18n)

- Support de 10 langues : Français, Anglais, Allemand, Espagnol, Italien, Néerlandais, Polonais, Portugais (Brésil), Russe, Chinois (Simplifié).
- Couverture à 100% : Toutes les clés sont synchronisées entre toutes les langues.
- Fallback intelligent : En cas de clé manquante, le moteur tente `lang` -> `en` -> `fr` -> `key`.
- Audit automatisé via `cmd/i18ncheck` pour garantir 0 oubli dans les templates.
- Normalisation des clés en snake_case et suppression des chaînes codées en dur dans les templates.

## i18n — vérification et réparation

- Problème fréquent : les fichiers JSON de traduction doivent être encodés en UTF-8 (sans BOM). Si un fichier est enregistré avec le mauvais encodage, l'interface affichera des caractères illisibles (mojibake) — surtout visible pour `zh.json` (chinois).
- Audit rapide (détecte clés manquantes et propose une réparation partielle) :

```powershell
# depuis la racine du projet
node scripts\\i18n_inspect.js
```

Le script affiche trois blocs : `---ZH_FIXED---` (tentative de décodage des chaînes mojibake pour `zh.json`), `---MISSING_KEYS---` (liste des clés manquantes par fichier) et `---ALL_KEYS---` (ensemble complet des clés détectées).

- Réparer l'encodage `zh.json` (tentative automatique) :

```powershell
# sauvegarde préalable conseillée
node -e "const fs=require('fs');const p='web/i18n/zh.json';let raw=fs.readFileSync(p,'utf8');if(raw.charCodeAt(0)===0xFEFF) raw=raw.slice(1);let obj;try{obj=JSON.parse(raw)}catch(e){const maybe=Buffer.from(fs.readFileSync(p,'binary'),'binary').toString('utf8');obj=JSON.parse(maybe);}Object.keys(obj).forEach(k=>{if(typeof obj[k]==='string'){const dec=Buffer.from(obj[k],'binary').toString('utf8');if(/[\\u4e00-\\u9fff]/.test(dec)) obj[k]=dec;}});fs.writeFileSync(p,JSON.stringify(obj,null,4));console.log('zh.json: attempted repair (review required)');"
```

- Parité des clés : après toute modification de templates (nouvelles `{{ .T "..." }}`), assurez-vous que chaque fichier `web/i18n/*.json` contient les mêmes clés. Pour ajouter des clés manquantes automatiquement, utilisez le résultat `---MISSING_KEYS---` de `i18n_inspect.js` comme feuille de route, puis ajoutez des valeurs de secours (anglais ou langue principale) et demandez des traductions humaines ensuite.

- Conseils d'édition :
	- Toujours sauvegarder les fichiers `.json` en `UTF-8 (no BOM)`.
	- Pour PowerShell 5, évitez `Set-Content -Encoding UTF8` (il écrit un BOM); préférez des éditeurs qui permettent explicitement `UTF-8 without BOM`, ou la commande Node ci-dessus.
	- Après corrections, redémarrez l'application et contrôlez `/admin/my-account` en `zh` pour valider l'affichage.
