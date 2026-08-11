# GESTION DES INVITATIONS JELLYGATE & STAGE TOKENS AUTHENTIK

## Modèle Métier des Invitations

Le système d'invitation est structuré en deux niveaux de responsabilités :

### 1. Invitation Métier (JellyGate)
Gérée dans la table SQL `invitations` de JellyGate :
- Code d'invitation unique
- Quota d'utilisation (`max_uses`, `used_count`)
- Date d'expiration
- Parrain créateur (`created_by_user_id`)
- Préréglage de profil de droits Jellyfin (`profile_id`, `profile_snapshot`)
- Statut : `pending`, `accepted`, `active`, `expired`, `cancelled`, `revoked`.

### 2. Invitation d'Enrollment (Authentik Stage Token)
Gérée sur l'API REST Authentik via `/api/v3/stages/invitation/invitations/` :
- Crée un token à usage unique rattaché au Flow d'Enrollment Authentik.
- Associe les données fixes du parrainage (`sponsor`, `code`).
- Redirige l'invité directement sur la création de compte Authentik.

## Workflow d'Inscription

```text
Parrain
 ↓
JellyGate (Création Invitation)
 ↓
Appel REST API Authentik (Stage Token)
 ↓
Lien transmis à l'Invité (https://jellygate.example.com/invite/{code})
 ↓
Authentik Enrollment Flow (Définition Mot de passe, MFA)
 ↓
Authentik User Write Stage (Ajout au groupe jellyfin-users)
 ↓
Redirect / OIDC Callback vers JellyGate
 ↓
JellyGate JIT Sync & Parrainage activé
```
