package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/maelmoreau21/JellyGate/internal/i18nreport"
)

func main() {
	report, err := i18nreport.Generate("web/templates", "web/i18n", "en")
	if err != nil {
		fmt.Fprintf(os.Stderr, "i18n check failed: %v\n", err)
		os.Exit(1)
	}

	hasError := false
	for _, locale := range report.Locales {
		if len(locale.MissingTemplateKeys) > 0 || len(locale.MissingFromBase) > 0 || len(locale.PlaceholderMismatches) > 0 || len(locale.FallbackValues) > 0 {
			hasError = true
			break
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)

	if hasError {
		fmt.Fprintln(os.Stderr, "i18n check: missing locale keys/placeholders/fallback issues detected")
		os.Exit(2)
	}
}
