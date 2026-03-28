//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
)

type TemplateData struct {
	Lang        string
	AppVersion  string
	ScriptNonce string
	Section     string
	Title       string
}

func (d *TemplateData) T(key string) string {
	return "[" + key + "]"
}

func main() {
	templatesDir := "web/templates"
	layouts, _ := filepath.Glob(filepath.Join(templatesDir, "layouts", "*.html"))

	page := filepath.Join(templatesDir, "admin", "login.html")
	files := append(layouts, page)

	tmpl, err := template.ParseFiles(files...)
	if err != nil {
		fmt.Printf("PARSE ERROR: %v\n", err)
		os.Exit(1)
	}

	td := &TemplateData{
		Lang:        "fr",
		AppVersion:  "1.0.0",
		ScriptNonce: "abc",
		Section:     "login",
	}

	err = tmpl.Execute(os.Stdout, td)
	if err != nil {
		fmt.Printf("\nEXECUTE ERROR: %v\n", err)
		os.Exit(1)
	}
}
