package handlers

import (
	"log/slog"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/database"
)

func cleanupClosedInvitationsIfEnabled(db *database.DB) {
	inviteCfg, err := db.GetInvitationProfileConfig()
	if err != nil {
		slog.Warn("Impossible de lire le profil d'invitation pour le nettoyage auto", "error", err)
		return
	}
	if !inviteCfg.AutoDeleteClosedLinks {
		return
	}
	if _, err := db.DeleteClosedInvitations(time.Now()); err != nil {
		slog.Warn("Impossible de supprimer les invitations fermees", "error", err)
	}
}
