//go:build ignore

package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "./data/jellygate.db")
	if err != nil {
		log.Fatalf("❌ Impossible d'ouvrir la base de données: %v", err)
	}
	defer db.Close()

	// 1. Nettoyer les tables existantes pour repartir à propre
	tables := []string{"users", "invitations", "audit_log", "security_events"}
	for _, t := range tables {
		_, err := db.Exec(fmt.Sprintf("DELETE FROM %s", t))
		if err != nil {
			log.Printf("⚠️ Erreur lors du nettoyage de %s: %v", t, err)
		}
	}

	now := time.Now()

	// Helper for nullable datetime strings
	toTimeStr := func(t time.Time) string {
		return t.UTC().Format("2006-01-02 15:04:05")
	}
	toNullTimeStr := func(nt sql.NullTime) interface{} {
		if !nt.Valid {
			return nil
		}
		return toTimeStr(nt.Time)
	}

	// 2. Insérer des utilisateurs
	users := []struct {
		jellyfinID        string
		username          string
		email             string
		emailVerified     bool
		groupName         string
		invitedBy         string
		isActive          bool
		isBanned          bool
		canInvite         bool
		preferredLang     string
		accessExpiresAt   sql.NullTime
		expiredAt         sql.NullTime
		createdAt         time.Time
	}{
		{
			jellyfinID:    "admin-uuid-1234",
			username:      "root",
			email:         "root@jellygate.local",
			emailVerified: true,
			groupName:     "Administrators",
			isActive:      true,
			canInvite:     true,
			preferredLang: "fr",
			createdAt:     now.AddDate(0, 0, -30),
		},
		{
			jellyfinID:    "user-uuid-alpha",
			username:      "User_Alpha",
			email:         "user_alpha@jellygate.local",
			emailVerified: true,
			groupName:     "Friends",
			invitedBy:     "root",
			isActive:      true,
			canInvite:     true,
			preferredLang: "fr",
			createdAt:     now.AddDate(0, 0, -10),
		},
		{
			jellyfinID:    "user-uuid-beta",
			username:      "User_Beta",
			email:         "user_beta@jellygate.local",
			emailVerified: false,
			groupName:     "Family",
			invitedBy:     "root",
			isActive:      true,
			canInvite:     false,
			preferredLang: "en",
			createdAt:     now.AddDate(0, 0, -8),
		},
		{
			jellyfinID:    "user-uuid-gamma",
			username:      "User_Gamma",
			email:         "user_gamma@jellygate.local",
			emailVerified: true,
			groupName:     "Colleagues",
			invitedBy:     "User_Alpha",
			isActive:      false,
			canInvite:     false,
			preferredLang: "en",
			expiredAt:     sql.NullTime{Time: now.AddDate(0, 0, -1), Valid: true},
			createdAt:     now.AddDate(0, 0, -15),
		},
		{
			jellyfinID:    "user-uuid-delta",
			username:      "User_Delta_Banned",
			email:         "user_delta@jellygate.local",
			emailVerified: true,
			groupName:     "Public",
			invitedBy:     "root",
			isActive:      true,
			isBanned:      true,
			canInvite:     false,
			preferredLang: "fr",
			createdAt:     now.AddDate(0, 0, -12),
		},
		{
			jellyfinID:      "user-uuid-epsilon",
			username:        "User_Epsilon_Temp",
			email:           "user_epsilon@jellygate.local",
			emailVerified:   true,
			groupName:       "Guests",
			invitedBy:       "root",
			isActive:        true,
			canInvite:       false,
			preferredLang:   "fr",
			accessExpiresAt: sql.NullTime{Time: now.AddDate(0, 0, 5), Valid: true},
			createdAt:       now.AddDate(0, 0, -2),
		},
	}

	for _, u := range users {
		query := `
			INSERT INTO users (
				jellyfin_id, username, email, email_verified, group_name,
				invited_by, is_active, is_banned, can_invite, preferred_lang,
				access_expires_at, expired_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`
		_, err := db.Exec(query,
			u.jellyfinID, u.username, u.email, u.emailVerified, u.groupName,
			u.invitedBy, u.isActive, u.isBanned, u.canInvite, u.preferredLang,
			toNullTimeStr(u.accessExpiresAt), toNullTimeStr(u.expiredAt), toTimeStr(u.createdAt), toTimeStr(u.createdAt),
		)
		if err != nil {
			log.Fatalf("❌ Erreur lors de l'insertion de l'utilisateur %s: %v", u.username, err)
		}
	}

	// 3. Insérer des invitations
	invitations := []struct {
		code                string
		label               string
		maxUses             int
		usedCount           int
		jellyfinProfile     string
		preferredLang       string
		profileID           string
		profileSnapshot     string
		isTemporary         bool
		accountDurationDays int
		expiresAt           sql.NullTime
		createdBy           string
		createdAt           time.Time
	}{
		{
			code:                "FRIENDS2026",
			label:               "Pack d'invitation Amis",
			maxUses:             10,
			usedCount:           3,
			jellyfinProfile:     "Default Profile",
			preferredLang:       "fr",
			profileID:           "profile-friends",
			profileSnapshot:     "{}",
			isTemporary:         false,
			accountDurationDays: 0,
			expiresAt:           sql.NullTime{Time: now.AddDate(1, 0, 0), Valid: true},
			createdBy:           "root",
			createdAt:           now.AddDate(0, 0, -20),
		},
		{
			code:                "FAMILY-TMP",
			label:               "Accès Temporaire Famille (30 jours)",
			maxUses:             2,
			usedCount:           1,
			jellyfinProfile:     "Family Profile",
			preferredLang:       "fr",
			profileID:           "profile-family",
			profileSnapshot:     "{}",
			isTemporary:         true,
			accountDurationDays: 30,
			expiresAt:           sql.NullTime{Time: now.AddDate(0, 1, 0), Valid: true},
			createdBy:           "root",
			createdAt:           now.AddDate(0, 0, -5),
		},
		{
			code:                "EXPIRED-PROMO",
			label:               "Invitation d'essai expirée",
			maxUses:             5,
			usedCount:           5,
			jellyfinProfile:     "Standard Profile",
			preferredLang:       "en",
			profileID:           "profile-standard",
			profileSnapshot:     "{}",
			isTemporary:         false,
			accountDurationDays: 0,
			expiresAt:           sql.NullTime{Time: now.AddDate(0, 0, -10), Valid: true},
			createdBy:           "root",
			createdAt:           now.AddDate(0, 0, -30),
		},
	}

	for _, inv := range invitations {
		query := `
			INSERT INTO invitations (
				code, label, max_uses, used_count, jellyfin_profile, preferred_lang,
				profile_id, profile_snapshot, is_temporary, account_duration_days,
				expires_at, created_by, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`
		_, err := db.Exec(query,
			inv.code, inv.label, inv.maxUses, inv.usedCount, inv.jellyfinProfile, inv.preferredLang,
			inv.profileID, inv.profileSnapshot, inv.isTemporary, inv.accountDurationDays,
			toNullTimeStr(inv.expiresAt), inv.createdBy, toTimeStr(inv.createdAt),
		)
		if err != nil {
			log.Fatalf("❌ Erreur lors de l'insertion de l'invitation %s: %v", inv.code, err)
		}
	}

	// 4. Insérer des logs d'audit
	auditLogs := []struct {
		action    string
		actor     string
		target    string
		details   string
		createdAt time.Time
	}{
		{
			action:    "invite.create",
			actor:     "root",
			target:    "FRIENDS2026",
			details:   "Création du code d'invitation FRIENDS2026 (max: 10 utilisations)",
			createdAt: now.AddDate(0, 0, -20),
		},
		{
			action:    "user.create",
			actor:     "FRIENDS2026",
			target:    "User_Alpha",
			details:   "Compte créé automatiquement via le code d'invitation FRIENDS2026",
			createdAt: now.AddDate(0, 0, -10),
		},
		{
			action:    "user.ban",
			actor:     "root",
			target:    "User_Delta_Banned",
			details:   "Utilisateur banni pour violation des conditions d'utilisation",
			createdAt: now.AddDate(0, 0, -6),
		},
		{
			action:    "invite.create",
			actor:     "root",
			target:    "FAMILY-TMP",
			details:   "Création du code d'invitation FAMILY-TMP (temporaire, 30 jours)",
			createdAt: now.AddDate(0, 0, -5),
		},
		{
			action:    "user.create",
			actor:     "FAMILY-TMP",
			target:    "User_Beta",
			details:   "Compte créé automatiquement via le code d'invitation FAMILY-TMP",
			createdAt: now.AddDate(0, 0, -8),
		},
		{
			action:    "settings.update",
			actor:     "root",
			target:    "smtp",
			details:   "Configuration du serveur SMTP de messagerie",
			createdAt: now.AddDate(0, 0, -4),
		},
		{
			action:    "user.disable",
			actor:     "root",
			target:    "User_Gamma",
			details:   "Compte désactivé suite à l'expiration de la période d'accès",
			createdAt: now.AddDate(0, 0, -1),
		},
	}

	for _, audit := range auditLogs {
		query := `
			INSERT INTO audit_log (action, actor, target, details, created_at)
			VALUES (?, ?, ?, ?, ?)
		`
		_, err := db.Exec(query, audit.action, audit.actor, audit.target, audit.details, toTimeStr(audit.createdAt))
		if err != nil {
			log.Fatalf("❌ Erreur lors de l'insertion du log d'audit: %v", err)
		}
	}

	// 5. Insérer des événements de sécurité
	securityEvents := []struct {
		category  string
		eventType string
		severity  string
		actor     string
		target    string
		ip        string
		message   string
		metadata  string
		resolved  bool
		createdAt time.Time
	}{
		{
			category:  "admin_login",
			eventType: "admin.login.success",
			severity:  "info",
			actor:     "root",
			ip:        "127.0.0.1",
			message:   "Connexion réussie de l'administrateur root",
			metadata:  "{}",
			resolved:  false,
			createdAt: now.AddDate(0, 0, -3),
		},
		{
			category:  "admin_login",
			eventType: "admin.login.failed",
			severity:  "warning",
			actor:     "admin",
			ip:        "192.168.10.11",
			message:   "Tentative de connexion échouée (mot de passe incorrect)",
			metadata:  "{}",
			resolved:  false,
			createdAt: now.AddDate(0, 0, -2),
		},
		{
			category:  "security",
			eventType: "rate_limit.exceeded",
			severity:  "warning",
			ip:        "198.51.100.72",
			message:   "Limite de requêtes dépassée sur la route /admin/login",
			metadata:  `{"requests_count":60,"window_seconds":60}`,
			resolved:  true,
			createdAt: now.AddDate(0, 0, -1),
		},
	}

	for _, se := range securityEvents {
		query := `
			INSERT INTO security_events (
				category, event_type, severity, actor, target, ip, message, metadata, resolved, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`
		_, err := db.Exec(query,
			se.category, se.eventType, se.severity, se.actor, se.target, se.ip, se.message, se.metadata, se.resolved, toTimeStr(se.createdAt),
		)
		if err != nil {
			log.Fatalf("❌ Erreur lors de l'insertion de l'événement de sécurité: %v", err)
		}
	}

	fmt.Println("🚀 Base de données SQLite JellyGate alimentée avec succès avec des données réalistes (dates au format SQLite standard) !")
}
