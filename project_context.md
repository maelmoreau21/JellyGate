# Contexte du projet (mémoire de travail)

Dernière mise à jour : 2026-03-02 (lot 4 finalisation LDAP accounts)

## Objectif du fork
Créer un fork de `jfa-go` avec :
- un **mode de provisioning utilisateur configurable** ;
- mode historique (Jellyfin/Jellyseerr) et nouveau mode **LDAP/Active Directory** (Synology Directory Server) ;
- une base plus moderne (menus/UI plus clairs) ;
- une configuration Docker améliorée.

## Exigences utilisateur (résumé)
1. Trouver un meilleur nom de projet (à proposer).
2. Ajouter un paramètre qui définit où les utilisateurs sont créés.
3. En mode LDAP/AD :
   - création d’utilisateurs dans Synology Directory Server ;
   - gestion mot de passe et opérations associées.
4. Connexion Jellyfin via URL + API key dans Docker Compose.
5. Authentification souhaitée sur jfa-go via identifiants Jellyfin (à préciser selon droits admin nécessaires).
6. Support Docker env:
   - `PUID=1000`
   - `GUID=1000`
7. Permettre la surcharge du port via env (exemple `PORT=8056`).
8. Mise à jour/refonte pour robustesse, sécurité, pérennité.
9. **Instruction obligatoire** : lire ce fichier avant chaque action et le mettre à jour lors de changements majeurs.

## Hypothèses de départ
- Le repo est un monolithe Go avec interface web statique + templates.
- Une refonte complète en une seule passe n’est pas réaliste sans plan incrémental ; priorité à une base propre avec points d’extension.

## Plan d’exécution (itératif)
- Phase 1 : cadrage technique + architecture cible (provider pattern pour backend utilisateur).
- Phase 2 : implémentation du paramètre de bascule et abstraction des opérations utilisateur.
- Phase 3 : fournisseur LDAP (Synology AD/LDAP) minimal viable.
- Phase 4 : variables d’environnement Docker (`PUID`, `GUID`, `PORT`) et documentation.
- Phase 5 : modernisation UI/menus ciblée (itérative) sans casser l’existant.

## État d’implémentation actuel (lot 1)
- **Backend identité basculable** :
   - `jellyfin` (par défaut)
   - `ldap` (via config `[identity] backend=ldap` ou env `JFA_USER_BACKEND=ldap`)
- Nouveau module `identity_backend.go` :
   - connexion LDAP/LDAPS ;
   - création d’utilisateur LDAP ;
   - vérification mot de passe ;
   - changement mot de passe LDAP.
- Flux branchés :
   - création utilisateur (`NewUserPostVerification`) ;
   - changement mot de passe utilisateur (`ChangeMyPassword`) ;
   - reset + set password (`ResetSetPassword`, mode interne).
- Variables Docker/runtime :
   - port via `PORT` (et compat `JFA_PORT`) ;
   - uid/gid process via `PUID` + `GUID` (compat `GID`).
- Documentation initiale ajoutée dans `README.md` (variables env + config LDAP).

## État d’implémentation lot 2 (menus/config)
- `config/config-base.yaml` mis à jour avec un nouveau groupe de menu **Identity & Directory**.
- Ajout de la section **Identity Backend** (sélecteur `jellyfin` / `ldap`).
- Ajout de la section **LDAP / Active Directory** (server, bind DN/password, base DN, OU, attributs, TLS).
- Objectif : rendre la bascule LDAP visible et administrable depuis l’interface Settings, avec un menu plus clair.
- Ajout d’un mode de connexion Jellyfin par **API key** (setting `jellyfin.api_key` + env `JFA_JELLYFIN_API_KEY`).
- Ajout des overrides env Docker Compose pour Jellyfin (`JFA_JELLYFIN_SERVER`, `JFA_JELLYFIN_PUBLIC_SERVER`).

## État d’implémentation lot 3 (UX + docker)
- Modernisation visuelle de `html/admin.html` :
   - barre d’onglets plus lisible (icônes + labels) ;
   - structure de mise en page plus propre pour le shell admin.
- Amélioration layout settings (`css/base.css`) :
   - sidebar plus stable et exploitable sur desktop ;
   - panneau principal plus lisible ;
   - navigation d’onglets admin plus homogène.
- Amélioration UX settings (`ts/modules/settings.ts`) :
   - ouverture automatique des groupes parents lors de la sélection d’une section ;
   - scroll automatique vers la section sélectionnée dans la sidebar.
- Compatibilité env étendue (`main.go`) :
   - prise en charge des variantes minuscules : `port`, `puid`, `guid`.
- Ajout d’un exemple compose complet : `docker-compose.example.yml`.

## État d’avancement LDAP admin (nouveau)
- Ajout d’un pont d’identité (`identity_user_bridge.go`) pour résoudre un user par ID quel que soit le backend.
- `GenInternalReset` supporte désormais les IDs LDAP.
- Actions admin supportées pour IDs LDAP :
   - enable/disable utilisateur ;
   - suppression utilisateur ;
   - récupération user par ID.
- `LDAPIdentityProvider` enrichi avec :
   - `DeleteUserByID` ;
   - `SetDisabledByID` (support AD `userAccountControl` + mode attribut configurable).
- Nouveaux paramètres LDAP de disable/enable exposés dans config + UI settings :
   - `disabled_attribute`, `disabled_value`, `enabled_value`.
- Documentation de migration ajoutée : `MIGRATION_LDAP.md`.
- Compatibilité liste/recherche users en mode LDAP :
   - ajout d’un listing LDAP natif (`ListUsers`) ;
   - endpoints `/users`, `/users` (search), `/users/count` compatibles backend LDAP.
- Parité comptes LDAP étendue :
   - `SetAccountsAdmin` compatible backend LDAP ;
   - `ModifyLabels` compatible backend LDAP ;
   - `ModifyEmails` compatible backend LDAP ;
   - `GetLabels` et `GetUserCount` désormais backend-aware.

## Naming (proposition)
- Nom cible proposé pour le fork : **JDA-Bridge**
   - signification : *Jellyfin Directory Accounts Bridge* ;
   - reflète la passerelle Jellyfin + annuaire LDAP/AD.

## Limites connues
- Le cœur historique de jfa-go garde des flux orientés politiques média Jellyfin (notamment settings/profils avancés).
- Le mode LDAP est désormais opérationnel pour provisioning, mot de passe et gestion comptes principale ; les cas extrêmes liés à l’écosystème Jellyfin tiers peuvent nécessiter adaptation.
- La refonte UI complète n’est pas encore faite (prévue en phase dédiée).

## Notes
- Éviter les changements destructifs non demandés.
- Prioriser sécurité, compatibilité et migration progressive.
