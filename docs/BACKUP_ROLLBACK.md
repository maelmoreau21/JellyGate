# SAUVEGARDE ET PROCEDURE DE ROLLBACK JELLYGATE

## Sauvegarde Automatisée

JellyGate intègre un service de sauvegarde SQLite / PostgreSQL natif (`internal/backup/service.go`) configurable dans `Administration -> Paramètres -> Sauvegardes`.

- **Emplacement des sauvegardes** : Représenté par des archives Zip horodatées dans le dossier de données (`/data/backups/`).
- **Périodicité** : Tâche planifiée automatique configurable (heure/minute, rétention des 7 dernières archives).

## Procédure de Restauration

1. Accédez à `Administration -> Sauvegardes` (`/admin/settings#backup`).
2. Sélectionnez une archive disponible ou importez un fichier `.zip`.
3. Cliquez sur **[Restaurer]**. JellyGate prépare la restauration et redémarre le processus proprement.

## Rollback d'Urgence

En cas d'incident critique lors d'une mise à jour vers la v2 :
1. Arrêtez le conteneur JellyGate.
2. Remplacez le fichier `data/jellygate.db` par votre copie de sauvegarde pré-migration.
3. Relancez l'image conteneur.
