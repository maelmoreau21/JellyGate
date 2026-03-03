# 🧠 Project Context: JellyGate

**Last Updated**: 2026-03-03
**Project Identity**: `JellyGate` (formerly JDA-Bridge)

---

## 🎯 Project Objective
JellyGate is a specialized enterprise-grade user provisioning tool for Jellyfin.

**Core goals:**
- ✅ **LDAP/Active Directory Support**: Direct integration with Synology Directory Server and generic LDAP.
- ✅ **Configurable Provisioning**: Choose where users are created (LDAP vs. Jellyfin).
- ✅ **Modernized Infrastructure**: Docker-first approach with full environment variable support.
- 🚧 **UI Modernization**: Iterative improvements to administration and user pages.

---

## 📅 Roadmap & Milestones

### 🏁 Phase 1: Identity & Architecture
- [x] Implement Provider Pattern for identity backends.
- [x] Create `LDAPIdentityProvider` for Synology/Windows AD.
- [x] Add `JFA_USER_BACKEND` environment toggle.

### 🔌 Phase 2: Docker & Config Integration
- [x] Support `PUID`, `GUID`, and `PORT` overrides.
- [x] Implement API Key authentication for Jellyfin.
- [x] Expose all LDAP settings through the Web UI and `config.ini`.
- [x] Fix `/data/config.ini: permission denied` Docker directory ownership issue by shifting directory creation order.

### 🎨 Phase 3: UX & Interface (Current Focus)
- [x] Clean Up Tab Navigation in Admin Settings.
- [x] Improve responsiveness of the sidebar and settings panels.
- [x] **Documentation Overhaul**: New professional `README.md` and branding.

---

## ⚙️ Technical Environment

| Component | Technology |
| :--- | :--- |
| **Backend** | Go (Monolith) |
| **Frontend** | HTML5, TypeScript, Vanilla CSS |
| **Container** | Docker (Distroless final image) |
| **CI/CD** | GitHub Actions (GHCR Publishing) |

---

## 🔒 Security & Best Practices
- **LDAPS Support**: Configurable TLS for directory connections.
- **API Security**: Token-based access for Jellyfin integration.
- **User Segregation**: Process runs as non-root (PUID/GUID support).

---

## 📝 Important Instructions
- **Read This First**: Always consult this file and `README.md` before initiating major changes.
- **Maintain Identity**: Use `JellyGate` as the primary project name.
- **Incremental Changes**: Prioritize non-destructive updates.
