package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func readJSONKeys(path string) (map[string]bool, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for k := range m {
		keys[k] = true
	}
	return keys, nil
}

func collectI18n(i18nDir string) (map[string]map[string]bool, map[string]bool, error) {
	langs := map[string]map[string]bool{}
	union := map[string]bool{}
	files, err := filepath.Glob(filepath.Join(i18nDir, "*.json"))
	if err != nil {
		return nil, nil, err
	}
	for _, f := range files {
		base := filepath.Base(f)
		lang := strings.TrimSuffix(base, filepath.Ext(base))
		keys, err := readJSONKeys(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: failed parse %s: %v\n", f, err)
			continue
		}
		langs[lang] = keys
		for k := range keys {
			union[k] = true
		}
	}
	return langs, union, nil
}

func scanForKeys(root string) (map[string]bool, error) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\\.T\s+"([^"]+)"`),   // templates: .T "key"
		regexp.MustCompile(`\\.T\s+'([^']+)'`),   // templates: .T 'key'
		regexp.MustCompile(`\\.T\(\s*"([^"]+)"`), // .T("key")
		regexp.MustCompile(`Translate\([^,]+,\s*"([^"]+)"`),
	}
	used := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".html" && ext != ".js" && ext != ".go" && ext != ".tmpl" {
			return nil
		}
		b, rerr := ioutil.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		s := string(b)
		for _, pat := range patterns {
			for _, m := range pat.FindAllStringSubmatch(s, -1) {
				if len(m) >= 2 {
					used[m[1]] = true
				}
			}
		}
		return nil
	})
	return used, err
}

func keysToSortedList(m map[string]bool) []string {
	arr := make([]string, 0, len(m))
	for k := range m {
		arr = append(arr, k)
	}
	sort.Strings(arr)
	return arr
}

func main() {
	root, _ := os.Getwd()
	i18nDir := filepath.Join(root, "web", "i18n")
	langs, union, err := collectI18n(i18nDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: collecting i18n: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Found %d language files. Total union keys: %d\n", len(langs), len(union))

	// scan templates + js + go for used keys
	usedKeys, err := scanForKeys(filepath.Join(root))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR scanning files for keys: %v\n", err)
		os.Exit(1)
	}

	// report per language missing keys
	o := []string{}
	for lang, keys := range langs {
		missing := []string{}
		for k := range union {
			if !keys[k] {
				missing = append(missing, k)
			}
		}
		sort.Strings(missing)
		o = append(o, fmt.Sprintf("- %s: has %d keys, missing %d", lang, len(keys), len(missing)))
		if len(missing) > 0 {
			// list up to 40 missing keys
			limit := 40
			if len(missing) < limit {
				limit = len(missing)
			}
			fmt.Printf("\nLanguage %s missing %d keys (showing up to %d):\n", lang, len(missing), limit)
			for i := 0; i < limit; i++ {
				fmt.Printf("  %s\n", missing[i])
			}
			if len(missing) > limit {
				fmt.Printf("  ... and %d more\n", len(missing)-limit)
			}
		}
	}

	fmt.Printf("\nSummary per language:\n")
	sort.Strings(o)
	for _, s := range o {
		fmt.Println(s)
	}

	// keys used in templates but not present in union
	usedNotInI18n := []string{}
	for k := range usedKeys {
		if !union[k] {
			usedNotInI18n = append(usedNotInI18n, k)
		}
	}
	sort.Strings(usedNotInI18n)
	if len(usedNotInI18n) > 0 {
		fmt.Printf("\nKeys used in templates/JS but missing from all i18n files (%d):\n", len(usedNotInI18n))
		for _, k := range usedNotInI18n {
			fmt.Printf("  %s\n", k)
		}
	} else {
		fmt.Printf("\nAll keys used in templates/JS are present in the i18n union.\n")
	}

	// languages missing any keys
	langsMissing := []string{}
	for lang := range langs {
		for k := range union {
			if !langs[lang][k] {
				langsMissing = append(langsMissing, lang)
				break
			}
		}
	}
	if len(langsMissing) == 0 {
		fmt.Printf("\nResult: all languages have the full union of keys. Translational coverage is complete.\n")
	} else {
		fmt.Printf("\nResult: %d languages have missing keys. See details above.\n", len(langsMissing))
	}
}
