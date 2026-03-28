//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"html/template"
	"path/filepath"
)

func main() {
	templatesDir := "web/templates"
	layouts, _ := filepath.Glob(filepath.Join(templatesDir, "layouts", "*.html"))

	pages := []string{
		"admin/login.html",
		"admin/dashboard.html",
		"admin/users.html",
		"admin/settings.html",
		"admin/logs.html",
		"admin/invitations.html",
		"admin/automation.html",
		"admin/my_account.html",
	}

	for _, p := range pages {
		pagePath := filepath.Join(templatesDir, p)
		files := append(layouts, pagePath)
		_, err := template.ParseFiles(files...)
		if err != nil {
			fmt.Printf("ERROR in %s: %v\n", p, err)
		} else {
			fmt.Printf("OK: %s\n", p)
		}
	}
}
