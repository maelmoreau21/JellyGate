package render

import (
	"path/filepath"
	"testing"
)

func TestNewEngineLoadsAllTemplates(t *testing.T) {
	templatesDir := filepath.Join("..", "..", "web", "templates")
	localesDir := filepath.Join("..", "..", "web", "i18n")

	engine, err := NewEngine(templatesDir, localesDir)
	if err != nil {
		t.Fatalf("Failed to initialize template render engine: %v", err)
	}

	if engine == nil {
		t.Fatalf("Expected engine to not be nil")
	}
}
