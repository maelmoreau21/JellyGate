package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
)

func main() {
	i18nDir := "web/i18n"

	files, err := ioutil.ReadDir(i18nDir)
	if err != nil {
		log.Fatalf("Failed to read i18n dir: %v", err)
	}

	// 1. Collect all keys and all data
	allKeys := make(map[string]bool)
	allData := make(map[string]map[string]string)
	var filenames []string

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue
		}
		filenames = append(filenames, file.Name())

		filePath := filepath.Join(i18nDir, file.Name())
		data, err := ioutil.ReadFile(filePath)
		if err != nil {
			log.Fatalf("Failed to read %s: %v", file.Name(), err)
		}

		var m map[string]string
		if err := json.Unmarshal(data, &m); err != nil {
			log.Fatalf("Failed to unmarshal %s: %v", file.Name(), err)
		}
		allData[file.Name()] = m

		for k := range m {
			allKeys[k] = true
		}
	}

	fmt.Printf("Total unique keys found: %d\n", len(allKeys))

	// 2. Propagate missing keys
	for _, fname := range filenames {
		m := allData[fname]
		added := 0
		for k := range allKeys {
			if _, ok := m[k]; !ok {
				// Find a value from another file (priority: en, then fr, then first available)
				val := findValue(k, allData)
				m[k] = val
				added++
			}
		}

		if added > 0 {
			// Sort and write back
			output, err := json.MarshalIndent(m, "", "    ")
			if err != nil {
				log.Fatalf("Failed to marshal %s: %v", fname, err)
			}
			if err := ioutil.WriteFile(filepath.Join(i18nDir, fname), output, 0600); err != nil {
				log.Fatalf("Failed to write %s: %v", fname, err)
			}
			fmt.Printf("Fixed %s: added %d missing keys. Total keys: %d\n", fname, added, len(m))
		} else {
			fmt.Printf("%s is already up to date. Total keys: %d\n", fname, len(m))
		}
	}
}

func findValue(key string, allData map[string]map[string]string) string {
	// Priority: en.json
	if m, ok := allData["en.json"]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	// Fallback: fr.json
	if m, ok := allData["fr.json"]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	// Last resort: any file
	for _, m := range allData {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return "[" + key + "]"
}
