# GUIDE DE MIGRATION VERS AUTHENTIK (JELLYGATE V2)

## Stratégie de Migration

La migration vers Authentik conserve l'intégralité des données historiques (comptes, invitations, parrainages, journaux d'audit) sans perte de données.

### 1. Migrations de Base de Données Automatiques
Au démarrage de la version v2, JellyGate exécute automatiquement les migrations SQL :
- Ajout de la colonne `authentik_id` (TEXT UNIQUE) dans la table `users`.
- Ajout de la colonne `authentik_invitation_id` et des statuts d'invitation dans la table `invitations`.
- Nettoyage des anciennes tables de réinitialisation de mot de passe local.

### 2. Rapprochement JIT des Comptes Utilisateurs
Lorsqu'un utilisateur historique se connecte pour la première fois via OIDC :
1. JellyGate reçoit le claim OIDC `sub`, `preferred_username` et `email` d'Authentik.
2. JellyGate recherche d'abord une correspondance exacte par `authentik_id`.
3. S'il ne le trouve pas, JellyGate effectue un rapprochement par `username` puis par `email`.
4. Une fois le compte historique identifié, JellyGate verrouille l'association en écrivant `authentik_id` = `sub` dans la base SQL.
5. Les connexions ultérieures s'effectuent directement via `authentik_id`.
