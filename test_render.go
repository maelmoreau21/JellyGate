//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
)

func main() {
	dir := "web/templates"
	var layouts []string
	var pages []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".html" {
			return nil
		}

		rel, _ := filepath.Rel(dir, path)
		if filepath.Dir(rel) == "layouts" {
			layouts = append(layouts, path)
		} else {
			pages = append(pages, path)
		}
		return nil
	})

	if err != nil {
		fmt.Printf("Error walking templates: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d layouts and %d pages\n", len(layouts), len(pages))

	success := true
	for _, page := range pages {
		name, _ := filepath.Rel(dir, page)
		files := append(layouts, page)

		t, err := template.ParseFiles(files...)
		if err != nil {
			fmt.Printf("FAIL: %s -> %v\n", name, err)
			success = false
		} else {
			fmt.Printf("OK: %s (defined templates: %v)\n", name, listTemplates(t))
		}
	}

	if !success {
		os.Exit(1)
	}
	fmt.Println("All templates parsed successfully!")
}

func listTemplates(t *template.Template) []string {
	var names []string
	for _, tmpl := range t.Templates() {
		names = append(names, tmpl.Name())
	}
	return names
}
