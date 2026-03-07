package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type localeCoverage struct {
	Locale     string   `json:"locale"`
	TotalKeys  int      `json:"total_keys"`
	SameAsBase int      `json:"same_as_base"`
	SameKeys   []string `json:"same_keys,omitempty"`
}

type coverageReport struct {
	BaseLocale string           `json:"base_locale"`
	I18nDir    string           `json:"i18n_dir"`
	Locales    []localeCoverage `json:"locales"`
}

func main() {
	var (
		i18nDir        = flag.String("i18n-dir", "web/i18n", "path to locale json files")
		baseLocale     = flag.String("base-locale", "en", "base locale filename without extension")
		maxSameAsBase  = flag.Int("max-same-as-base", -1, "maximum identical values allowed per locale (-1 disables threshold check)")
		includeSameKey = flag.Bool("include-same-keys", false, "include key list that is identical to base locale")
	)
	flag.Parse()

	locales, err := loadLocales(*i18nDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "i18n coverage failed: %v\n", err)
		os.Exit(1)
	}

	base, ok := locales[*baseLocale]
	if !ok {
		fmt.Fprintf(os.Stderr, "i18n coverage failed: base locale %q not found in %s\n", *baseLocale, *i18nDir)
		os.Exit(1)
	}

	report := coverageReport{
		BaseLocale: *baseLocale,
		I18nDir:    *i18nDir,
		Locales:    make([]localeCoverage, 0, len(locales)-1),
	}

	localeNames := make([]string, 0, len(locales))
	for locale := range locales {
		if locale == *baseLocale {
			continue
		}
		localeNames = append(localeNames, locale)
	}
	sort.Strings(localeNames)

	hasThresholdViolation := false
	for _, locale := range localeNames {
		coverage := calculateCoverage(base, locales[locale], locale, *includeSameKey)
		if *maxSameAsBase >= 0 && coverage.SameAsBase > *maxSameAsBase {
			hasThresholdViolation = true
		}
		report.Locales = append(report.Locales, coverage)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)

	if hasThresholdViolation {
		fmt.Fprintf(os.Stderr, "i18n coverage: one or more locales exceed max-same-as-base=%d\n", *maxSameAsBase)
		os.Exit(2)
	}
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
	for _, file := range files {
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
	}

	return locales, nil
}

func calculateCoverage(base, localeMap map[string]string, locale string, includeSameKeys bool) localeCoverage {
	keys := make([]string, 0, len(base))
	for key := range base {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	sameKeys := make([]string, 0)
	sameCount := 0

	for _, key := range keys {
		baseValue := strings.TrimSpace(base[key])
		localeValue, ok := localeMap[key]
		if !ok {
			continue
		}
		if strings.TrimSpace(localeValue) == baseValue {
			sameCount++
			if includeSameKeys {
				sameKeys = append(sameKeys, key)
			}
		}
	}

	entry := localeCoverage{
		Locale:     locale,
		TotalKeys:  len(localeMap),
		SameAsBase: sameCount,
	}
	if includeSameKeys {
		entry.SameKeys = sameKeys
	}
	return entry
}
