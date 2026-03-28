//go:build ignore
// +build ignore

package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	user := "jellygate"
	password := "'R!$$P&5TYM3-lc2@34ug$$'"
	host := "192.168.20.251"
	port := 5433
	dbname := "jellygate"

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", user, password, host, port, dbname)
	fmt.Printf("Testing connection with: %s\n", dsn)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("sql.Open failed: %v", err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		log.Fatalf("db.Ping failed: %v", err)
	}

	fmt.Println("✅ Connection successful!")
}
