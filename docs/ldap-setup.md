# LDAP Configuration with JellyGate

This guide explains how to properly configure LDAP with JellyGate, either in `hybrid` mode (LDAP + Jellyfin) or `ldap_only`.

## 1. Prerequisites

- A LDAP/AD directory accessible from JellyGate.
- A LDAP service account (bind) with read and user creation permissions (if provisioning is active).
- Jellyfin installed and reachable (`hybrid` mode).
- Target LDAP groups already created, for example:
  - `jellyfin` (standard users)
  - `jellyfin-Parrainage` (users authorized to invite)
  - `jellyfin-administrateur` (LDAP admin accounts, if used)

## 2. Configure LDAP in JellyGate

In `Admin -> Settings -> LDAP`:

1. Enable `LDAP`.
2. Fill in `Host`, `Port`, `Bind DN`, `Bind password`, `Base DN`.
3. Leave `username_attribute`, `user_object_class`, `group_member_attr` on `auto` unless you have specific needs.
4. Choose the mode:
   - `hybrid`: creates both LDAP + Jellyfin accounts
   - `ldap_only`: creates LDAP accounts only (no local Jellyfin account)
5. Save and then use the test buttons:
   - Test LDAP connection
   - Test LDAP user search
   - Test Jellyfin authentication via LDAP plugin (if Jellyfin LDAP is configured)

## 3. Associate LDAP Groups via Automation

LDAP group assignment is no longer configured under `Settings -> LDAP`.

Instead, use `Admin -> Automation -> Presets`:

1. Open or create a preset.
2. Fill in `LDAP User Group (optional)`:
   - Example: `CN=jellyfin,OU=Groups,DC=example,DC=com`
3. If the preset can invite (`Can invite`), also fill in `LDAP Sponsor Group (optional)`:
   - Example: `CN=jellyfin-Parrainage,OU=Groups,DC=example,DC=com`
4. Save the presets.

JellyGate automatically generates the corresponding `LDAP -> preset` mappings.

## 4. Provisioning Behavior

Upon registration via invitation:

- JellyGate creates the LDAP account.
- The account is added to the Jellyfin user group by default.
- LDAP groups linked to the preset (Automation) are then added.
- If the invitation profile allows sponsorship (`can_invite`), the LDAP role `inviter` is applied.

In `hybrid` mode, the preset's Jellyfin permissions are also applied.

## 5. Best Practices

- Always start with a minimal "standard" preset.
- Only enable `can_invite` for sponsorship profiles.
- Use complete DNs (`CN=...,OU=...,DC=...`) to avoid ambiguity.
- Verify invitation quotas and validity limits in presets.

## 6. Quick Troubleshooting

- LDAP group addition failure:
  - Check `group_member_attr` (auto/member/memberUid) and the LDAP schema.
  - Verify that the service account has permission to modify groups.
- User created but no Jellyfin permissions:
  - Check the target preset and `LDAP -> preset` mappings.
  - In `ldap_only` mode, it is normal that a local Jellyfin account is not created.
- LDAP authentication failing in Jellyfin:
  - Check the Jellyfin LDAP plugin configuration and the Jellyfin URL in JellyGate.

## 7. Recommended Validation After Modification

```bash
npm run build:css
go build ./...
go test ./...
go run ./cmd/i18ncheck
```
