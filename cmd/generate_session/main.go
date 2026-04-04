package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
)

// main génère une clé secrète aléatoire de 32 octets (64 caractères hex)
// utilisable pour la variable JELLYGATE_SECRET_KEY.
func main() {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("❌ Erreur lors de la génération de la clé : %v", err)
	}
	fmt.Println(hex.EncodeToString(b))
}
