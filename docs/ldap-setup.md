# Configuration LDAP avec JellyGate

Ce guide explique une configuration LDAP propre avec JellyGate, en mode `hybrid` (LDAP + Jellyfin) ou `ldap_only`.

## 1. Prerequis

- Un annuaire LDAP/AD accessible depuis JellyGate.
- Un compte de service LDAP (bind) avec droits de lecture et creation utilisateur (si provisionning actif).
- Jellyfin installe et joignable (mode `hybrid`).
- Groupes LDAP cibles deja crees, par exemple:
  - `jellyfin` (utilisateurs standards)
  - `jellyfin-Parrainage` (utilisateurs autorises a inviter)
  - `jellyfin-administrateur` (comptes admin LDAP, si utilise)

## 2. Parametrer LDAP dans JellyGate

Dans `Admin -> Parametres -> LDAP`:

1. Active `LDAP`.
2. Renseigne `Host`, `Port`, `Bind DN`, `Bind password`, `Base DN`.
3. Garde `username_attribute`, `user_object_class`, `group_member_attr` sur `auto` sauf besoin specifique.
4. Choisis le mode:
   - `hybrid`: cree LDAP + Jellyfin
   - `ldap_only`: cree LDAP uniquement (pas de compte Jellyfin local)
5. Sauvegarde puis utilise les boutons de test:
   - Test connexion LDAP
   - Test recherche utilisateur LDAP
   - Test authentification Jellyfin via plugin LDAP (si Jellyfin LDAP est configure)

## 3. Associer les groupes LDAP via Automatisation

L'affectation de groupes LDAP ne se fait plus dans `Parametres -> LDAP`.

Utilise `Admin -> Automatisation -> Presets`:

1. Ouvre ou cree un preset.
2. Renseigne `Groupe LDAP utilisateurs (optionnel)`:
   - Exemple: `CN=jellyfin,OU=Groups,DC=example,DC=com`
3. Si le preset peut inviter (`Can invite`), renseigne aussi `Groupe LDAP parrainage (optionnel)`:
   - Exemple: `CN=jellyfin-Parrainage,OU=Groups,DC=example,DC=com`
4. Sauvegarde les presets.

JellyGate genere automatiquement les mappings `LDAP -> preset` correspondants.

## 4. Comportement du provisionning

A l'inscription via invitation:

- JellyGate cree le compte LDAP.
- Le compte est ajoute par defaut au groupe utilisateur Jellyfin.
- Les groupes LDAP lies au preset (Automatisation) sont ensuite ajoutes.
- Si le profil d'invitation autorise le parrainage (`can_invite`), le role LDAP `inviter` est applique.

En mode `hybrid`, les droits Jellyfin du preset sont aussi appliques.

## 5. Bonnes pratiques

- Toujours commencer avec un preset "standard" minimal.
- N'activer `can_invite` que pour les profils parrainage.
- Utiliser des DN complets (`CN=...,OU=...,DC=...`) pour eviter les ambiguities.
- Verifier les quotas d'invitation et limites de validite dans les presets.

## 6. Depannage rapide

- Echec ajout groupe LDAP:
  - Verifier `group_member_attr` (auto/member/memberUid) et le schema LDAP.
  - Verifier que le compte de service peut modifier les groupes.
- Utilisateur cree mais pas de droits Jellyfin:
  - Verifier le preset cible et les mappings `LDAP -> preset`.
  - En mode `ldap_only`, c'est normal de ne pas creer un compte Jellyfin local.
- Auth LDAP KO dans Jellyfin:
  - Verifier la conf du plugin LDAP Jellyfin et l'URL Jellyfin dans JellyGate.

## 7. Validation conseillee apres modification

```bash
npm run build:css
go build ./...
go test ./...
go run ./cmd/i18ncheck
```
