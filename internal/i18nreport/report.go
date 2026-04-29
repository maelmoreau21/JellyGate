package i18nreport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	templateKeyPattern   = regexp.MustCompile(`\.T\s+"([^"]+)"`)
	placeholderPattern   = regexp.MustCompile(`\{[a-zA-Z0-9_]+\}`)
	fallbackValuePattern = regexp.MustCompile(`^\[[^\]]+\]$`)
	localeFilePattern    = regexp.MustCompile(`^[a-z]{2}(?:-[a-z]{2})?\.json$`)
)

type LocaleReport struct {
	Locale                string   `json:"locale"`
	TotalKeys             int      `json:"total_keys"`
	MissingTemplateKeys   []string `json:"missing_template_keys"`
	MissingFromBase       []string `json:"missing_from_base"`
	PlaceholderMismatches []string `json:"placeholder_mismatches"`
	FallbackValues        []string `json:"fallback_values"`
}

type Report struct {
	TemplateKeyCount int            `json:"template_key_count"`
	BaseLocale       string         `json:"base_locale"`
	BaseKeyCount     int            `json:"base_key_count"`
	Locales          []LocaleReport `json:"locales"`
}

func Generate(templatesDir, i18nDir, baseLocale string) (*Report, error) {
	templateKeys, err := collectTemplateKeys(templatesDir)
	if err != nil {
		return nil, err
	}

	locales, err := loadLocales(i18nDir)
	if err != nil {
		return nil, err
	}

	base := locales[baseLocale]
	if base == nil {
		return nil, fmt.Errorf("base locale %q not found", baseLocale)
	}

	report := &Report{
		TemplateKeyCount: len(templateKeys),
		BaseLocale:       baseLocale,
		BaseKeyCount:     len(base),
		Locales:          make([]LocaleReport, 0, len(locales)),
	}

	localeNames := make([]string, 0, len(locales))
	for locale := range locales {
		localeNames = append(localeNames, locale)
	}
	sort.Strings(localeNames)

	for _, locale := range localeNames {
		keys := locales[locale]
		entry := LocaleReport{
			Locale:                locale,
			TotalKeys:             len(keys),
			MissingTemplateKeys:   missingKeys(templateKeys, keys),
			MissingFromBase:       missingBaseKeys(base, keys),
			PlaceholderMismatches: placeholderMismatches(base, keys),
			FallbackValues:        fallbackValues(keys),
		}
		report.Locales = append(report.Locales, entry)
	}

	return report, nil
}

func collectTemplateKeys(templatesDir string) ([]string, error) {
	keys := map[string]struct{}{}

	err := filepath.Walk(templatesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".html") {
			return nil
		}

		data, readErr := os.ReadFile(path) // #nosec G304,G122 -- path comes from filepath.Walk over the configured templates directory.
		if readErr != nil {
			return readErr
		}

		matches := templateKeyPattern.FindAllStringSubmatch(string(data), -1)
		for _, match := range matches {
			if len(match) >= 2 {
				keys[strings.TrimSpace(match[1])] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect template keys: %w", err)
	}

	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	sort.Strings(result)
	return result, nil
}

func loadLocales(i18nDir string) (map[string]map[string]string, error) {
	files, err := filepath.Glob(filepath.Join(i18nDir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("glob locales: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no locale files found in %s", i18nDir)
	}

	locales := make(map[string]map[string]string, len(files))
	loaded := 0
	for _, file := range files {
		if !localeFilePattern.MatchString(strings.ToLower(filepath.Base(file))) {
			continue
		}

		data, readErr := os.ReadFile(file)
		if readErr != nil {
			return nil, fmt.Errorf("read locale %s: %w", file, readErr)
		}
		kv := map[string]string{}
		if unmarshalErr := json.Unmarshal(data, &kv); unmarshalErr != nil {
			return nil, fmt.Errorf("parse locale %s: %w", file, unmarshalErr)
		}
		locale := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		locales[locale] = kv
		loaded++
	}

	if loaded == 0 {
		return nil, fmt.Errorf("no active locale files matched in %s", i18nDir)
	}
	return locales, nil
}

func missingKeys(templateKeys []string, localeMap map[string]string) []string {
	missing := make([]string, 0)
	for _, key := range templateKeys {
		if _, ok := localeMap[key]; !ok {
			missing = append(missing, key)
		}
	}
	return missing
}

func missingBaseKeys(base, localeMap map[string]string) []string {
	missing := make([]string, 0)
	for key := range base {
		if _, ok := localeMap[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}

func placeholderMismatches(base, localeMap map[string]string) []string {
	mismatches := make([]string, 0)
	for key, baseValue := range base {
		localeValue, ok := localeMap[key]
		if !ok {
			continue
		}
		baseSet := placeholderSet(baseValue)
		localeSet := placeholderSet(localeValue)
		if !sameSet(baseSet, localeSet) {
			mismatches = append(mismatches, key)
		}
	}
	sort.Strings(mismatches)
	return mismatches
}

func fallbackValues(localeMap map[string]string) []string {
	keys := make([]string, 0)
	for key, value := range localeMap {
		if fallbackValuePattern.MatchString(strings.TrimSpace(value)) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func placeholderSet(value string) map[string]struct{} {
	matches := placeholderPattern.FindAllString(value, -1)
	set := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		set[match] = struct{}{}
	}
	return set
}

func sameSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if _, ok := b[key]; !ok {
			return false
		}
	}
	return true
}
