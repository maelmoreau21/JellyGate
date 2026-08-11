# SYSTEME DE PARRAINAGE ET QUOTAS JELLYGATE

## Moteur de Parrainage

Le parrainage est une fonctionnalité centrale de JellyGate permettant aux utilisateurs autorisés d'inviter leurs proches tout en évitant les abus.

### Concepts clés :
- **Parrain (`sponsor`)** : Utilisateur disposant du droit d'inviter (`can_invite = true`).
- **Filleul (`godchild`)** : Compte créé via l'invitation d'un parrain.
- **Table `referrals`** : Conserve la liaison historique permanente entre le parrain et le filleul (`sponsor_user_id`, `godchild_user_id`, `godchild_authentik_id`).
- **Quotas** :
  - Quota par défaut défini par le profil d'invitation.
  - Quota personnalisé (`custom_quota`).
  - Quota bonus (`bonus_quota`) attribué par un administrateur.
  - Quota malus (`malus_quota`) réduit en cas d'abus.

## cycle de vie du parrainage

1. Le parrain génère un lien d'invitation.
2. Une ligne `referrals` est enregistrée avec l'état `pending`.
3. Le filleul crée son compte sur Authentik et se connecte via OIDC à JellyGate.
4. `SyncOIDCUser` détecte le parrainage lié à l'invitation, enregistre l'UUID Authentik du filleul (`godchild_authentik_id` = `sub`), et passe l'état du parrainage à `active`.
5. Les statistiques du parrain sont automatiquement mises à jour dans son tableau de bord (`/admin/my-account`).
