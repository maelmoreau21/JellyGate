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
	page := filepath.Join(templatesDir, "admin/login.html")

	files := append(layouts, page)
	tmpl, err := template.ParseFiles(files...)
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		return
	}

	fmt.Print("Templates found: ")
	for _, t := range tmpl.Templates() {
		fmt.Printf("[%s] ", t.Name())
	}
	fmt.Println()
}
