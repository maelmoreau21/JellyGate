# ARCHITECTURE JELLYGATE — INTEGRATION AUTHENTIK

## Vue d'ensemble

JellyGate s'appuie sur **Authentik** comme source unique de vérité pour l'identité des utilisateurs, la gestion des mots de passe, l'authentification multifacteur (MFA) et les groupes d'accès.

- **Authentik** : Source unique de vérité pour l'identité (Users, Passwords, MFA, Groups, OIDC Provider, Outpost LDAP).
- **JellyGate** : Source unique de vérité pour la logique métier (Parrainage, Quotas, Invitations, Dashboard, Audit, Notifications).
- **Jellyfin** : Application consommatrice de l'identité Authentik (Authentifiée via l'Outpost LDAP d'Authentik).

```text
                         ┌─────────────────────┐
                         │      AUTHENTIK      │
                         │                     │
                         │ Users               │
                         │ Passwords           │
                         │ Emails              │
                         │ MFA                 │
                         │ Groups              │
                         │ OIDC Provider       │
                         │ Enrollment Flow     │
                         │ Stage Invitations   │
                         └─────────┬───────────┘
                                   │
                     ┌─────────────┴─────────────┐
                     │                           │
                   OIDC                         LDAP
                     │                           │
                     ▼                           ▼
              ┌──────────────┐           ┌──────────────┐
              │   JELLYGATE  │           │   JELLYFIN   │
              │              │           │              │
              │ Parrainage   │           │ Authentifié  │
              │ Invitations  │           │ via Authentik│
              │ Quotas       │           │ LDAP Outpost │
              │ Dashboard    │           └──────────────┘
              │ Admin        │
              │ Audit        │
              │ Notifications│
              └──────┬───────┘
                     │
                     │ Authentik REST API
                     ▼
              ┌──────────────┐
              │ Invitations  │
              │ Enrollment   │
              │ Core Users   │
              └──────────────┘
```

## Isolation des Responsabilités

- **Aucun client LDAP dans JellyGate** : JellyGate ne réalise aucun bind, recherche ou écriture LDAP. L'Outpost LDAP est une infrastructure native Authentik.
- **Aucune création d'utilisateur direct dans Jellyfin** : JellyGate ne crée ni ne supprime les utilisateurs dans Jellyfin. Jellyfin synchronise ses utilisateurs via l'Outpost LDAP d'Authentik.
- **Identifiant Unique** : L'identifiant utilisateur permanent dans JellyGate est la claim OIDC `sub` (`authentik_id`). Les usernames et emails ne sont que des caches synchronisés JIT.
