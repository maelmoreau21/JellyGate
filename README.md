# 🚪 JellyGate

**The ultimate bridge between Jellyfin and your LDAP/AD Directory.**

[![Docker Pulls](https://img.shields.io/docker/pulls/hrfee/jfa-go?label=docker&style=for-the-badge)](https://hub.docker.com/r/hrfee/jfa-go)
[![GitHub release](https://img.shields.io/github/v/release/maelmoreau21/JellyGate?style=for-the-badge)](https://github.com/maelmoreau21/JellyGate/releases)
[![License](https://img.shields.io/github/license/hrfee/jfa-go?style=for-the-badge)](LICENSE)

---

### ✨ What is JellyGate?

JellyGate is a powerful fork of `jfa-go` designed to provide a robust, modern, and highly configurable user management system for **Jellyfin**. 

Whether you're running a small home server or a large organization, JellyGate bridges the gap between your media server and your existing user directory (LDAP/Active Directory), making account creation, management, and password resets a breeze.

---

### 🚀 Key Features

| Feature | Description |
| :--- | :--- |
| **🔀 Dual Backend** | Seamlessly switch between Jellyfin's internal database and **LDAP/Active Directory**. |
| **🔗 Smart Invites** | Create expiring, multi-use, or single-use invite links with custom Jellyfin profiles. |
| **🔐 Self-Service** | Empower users to reset their own passwords and manage their profiles. |
| **📢 Global Messaging** | Reach your users via Email, Discord, Telegram, or Matrix with Markdown support. |
| **📂 Bulk Management** | Enable, disable, or delete users at scale with a clean, modern UI. |
| **🛠️ Modern UI** | Refined administration interface for better ergonomics and usability. |

---

### 🐳 Quick Start with Docker

The fastest way to get JellyGate up and running is via Docker Compose.

```yaml
services:
  jellygate:
    image: ghcr.io/maelmoreau21/jellygate:latest
    container_name: jellygate
    restart: unless-stopped
    environment:
      - PUID=1000
      - PGID=1000
      - PORT=8056
      - JFA_USER_BACKEND=ldap # or 'jellyfin'
      - JFA_JELLYFIN_SERVER=http://jellyfin:8096
      - JFA_JELLYFIN_API_KEY=your_jellyfin_api_key
    ports:
      - "8056:8056"
    volumes:
      - ./data:/data
      - /etc/localtime:/etc/localtime:ro
```

> [!TIP]
> Check our [Migration Guide](MIGRATION_LDAP.md) for a smooth transition to LDAP mode.

---

### 📖 Documentation & Support

- **LDAP Setup**: See the [MIGRATION_LDAP.md](MIGRATION_LDAP.md) for Synology AD/LDAP configuration.
- **Project History**: Read our [Project Context](project_context.md) for development milestones.
- **Original Project**: Based on the excellent work by [hrfee](https://github.com/hrfee/jfa-go).

---

### 🤝 Contributing

Contributions are welcome! Please check the issues or submit a pull request.

---

<p align="center">
  Made with ❤️ for the Jellyfin community.
</p>
