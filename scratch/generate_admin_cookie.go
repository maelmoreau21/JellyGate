package main

import (
	"fmt"
	"time"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

func main() {
	secretKey := "7f8e9a2b5c4d1e3f0a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f"
	now := time.Now()
	payload := session.Payload{
		UserID:   "admin-uuid-1234",
		Username: "root",
		IsAdmin:  true,
		Exp:      now.Add(10 * 365 * 24 * time.Hour).Unix(),
		Iat:      now.Unix(),
	}
	cookie, err := session.Sign(payload, secretKey)
	if err != nil {
		panic(err)
	}
	fmt.Println(cookie)
}
