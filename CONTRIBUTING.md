# Contributing to JellyGate

Merci pour votre intérêt pour JellyGate.

Important :

- Ne pas committer ni pousser de modifications sur le dépôt, sauf demande explicite de l'utilisateur. Les agents et contributeurs doivent préparer un patch/PR pour revue et attendre une instruction explicite avant d'exécuter des commits, pushes ou la création de tags.

Procédure recommandée :

1. Ouvrir une issue décrivant le changement proposé.
2. Préparer un patch ou une branche de travail locale et proposer une PR pour revue.
3. Attendre validation avant de merger ou de pousser des tags de release.

Vérifications locales utiles :

```bash
npm run build:css
go build ./...
go test ./...
go run ./cmd/i18ncheck
```

Merci de respecter ces consignes pour garder le dépôt propre et sûr.
