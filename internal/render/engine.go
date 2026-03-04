// Package render fournit le moteur de templates HTML pour JellyGate.
//
// Charge tous les templates depuis web/templates/ au démarrage,
// charge les traductions depuis web/i18n/{lang}.json,
// et fournit une interface simple pour le rendu avec i18n contextuel.
//
// La fonction T() utilise la langue du contexte de requête et effectue
// un fallback automatique sur le français si une clé est absente.
package render

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ── Translations ────────────────────────────────────────────────────────────

// Translations est un dictionnaire de traductions clé → valeur.
type Translations map[string]string

// ── TemplateData ────────────────────────────────────────────────────────────

// TemplateData est la structure passée à tous les templates.
// Le champ T (translate) est une fonction qui retourne la traduction d'une clé
// dans la langue courante, avec fallback sur le français.
type TemplateData struct {
	Lang    string
	engine  *Engine
	Data    map[string]interface{} // Données arbitraires
	Error   string
	Session interface{}

	// Raccourcis pour les templates (évite .Data.XXX dans les templates)
	Invitation          interface{}
	AdminUsername       string
	ShowNewPasswordForm bool
	ResetCode           string
	SuccessMessage      string
}

// T retourne la traduction pour la clé donnée, dans la langue de ce contexte.
func (d *TemplateData) T(key string) string {
	if d.engine != nil {
		return d.engine.Translate(d.Lang, key)
	}
	return "[" + key + "]"
}

// ── Engine ──────────────────────────────────────────────────────────────────

// Engine est le moteur de templates HTML avec support i18n.
type Engine struct {
	templates    map[string]*template.Template
	translations map[string]Translations // lang → {key → value}
	fallbackLang string                  // Langue de fallback (fr)
	mu           sync.RWMutex
	dir          string
}

// NewEngine crée un nouveau moteur de templates.
// Charge les layouts, les pages et les traductions.
//
// Paramètres :
//   - templatesDir : dossier des templates (web/templates)
//   - i18nDir      : dossier des traductions (web/i18n)
func NewEngine(templatesDir, i18nDir string) (*Engine, error) {
	e := &Engine{
		templates:    make(map[string]*template.Template),
		translations: make(map[string]Translations),
		fallbackLang: "fr",
		dir:          templatesDir,
	}

	// Charger les traductions
	if err := e.loadTranslations(i18nDir); err != nil {
		return nil, fmt.Errorf("render.NewEngine: %w", err)
	}

	// Charger les templates
	if err := e.loadTemplates(); err != nil {
		return nil, fmt.Errorf("render.NewEngine: %w", err)
	}

	return e, nil
}

// ── Traductions ─────────────────────────────────────────────────────────────

// loadTranslations charge tous les fichiers {lang}.json depuis i18nDir.
func (e *Engine) loadTranslations(i18nDir string) error {
	if _, err := os.Stat(i18nDir); os.IsNotExist(err) {
		slog.Warn("Dossier i18n introuvable, aucune traduction chargée", "dir", i18nDir)
		return nil
	}

	files, err := filepath.Glob(filepath.Join(i18nDir, "*.json"))
	if err != nil {
		return fmt.Errorf("erreur glob i18n: %w", err)
	}

	for _, file := range files {
		// Extraire le code de langue du nom de fichier (ex: "fr.json" → "fr")
		base := filepath.Base(file)
		lang := strings.TrimSuffix(base, filepath.Ext(base))

		data, err := os.ReadFile(file)
		if err != nil {
			slog.Warn("Erreur de lecture du fichier i18n", "file", file, "error", err)
			continue
		}

		var trans Translations
		if err := json.Unmarshal(data, &trans); err != nil {
			slog.Warn("Erreur de parsing JSON i18n", "file", file, "error", err)
			continue
		}

		e.translations[lang] = trans
		slog.Info("Traductions chargées", "lang", lang, "keys", len(trans))
	}

	return nil
}

// Translate retourne la traduction d'une clé dans la langue demandée.
// Si la clé n'existe pas dans la langue demandée, effectue un fallback
// sur le français. Si la clé n'existe nulle part, retourne "[clé]".
func (e *Engine) Translate(lang, key string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// 1. Chercher dans la langue demandée
	if trans, ok := e.translations[lang]; ok {
		if v, ok := trans[key]; ok {
			return v
		}
	}

	// 2. Fallback sur le français
	if lang != e.fallbackLang {
		if trans, ok := e.translations[e.fallbackLang]; ok {
			if v, ok := trans[key]; ok {
				return v
			}
		}
	}

	// 3. Clé brute (visible en dev)
	return "[" + key + "]"
}

// NewTemplateData crée une TemplateData liée à une langue.
// La fonction T() résout automatiquement les clés dans la bonne langue
// avec fallback sur le français.
func (e *Engine) NewTemplateData(lang string) *TemplateData {
	return &TemplateData{
		Lang:   lang,
		engine: e,
		Data:   make(map[string]interface{}),
	}
}

// ── Templates ───────────────────────────────────────────────────────────────

// loadTemplates charge tous les templates (layouts + pages).
func (e *Engine) loadTemplates() error {
	layoutsDir := filepath.Join(e.dir, "layouts")
	layoutPattern := filepath.Join(layoutsDir, "*.html")

	// Chercher les layouts
	layouts, err := filepath.Glob(layoutPattern)
	if err != nil {
		return fmt.Errorf("erreur glob layouts: %w", err)
	}

	// Chercher toutes les pages (récursivement)
	var pages []string
	err = filepath.Walk(e.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".html") {
			return nil
		}
		// Ignorer les layouts et les emails
		rel, _ := filepath.Rel(e.dir, path)
		if strings.HasPrefix(rel, "layouts") || strings.HasPrefix(rel, "emails") {
			return nil
		}
		pages = append(pages, path)
		return nil
	})
	if err != nil {
		return fmt.Errorf("erreur walk templates: %w", err)
	}

	// Compiler chaque page avec les layouts
	for _, page := range pages {
		// Nom relatif comme clé (ex: "invite.html", "admin/dashboard.html")
		name, _ := filepath.Rel(e.dir, page)
		name = filepath.ToSlash(name) // Normaliser sur Windows

		// Combiner layouts + page
		files := append(layouts, page)

		tmpl, err := template.ParseFiles(files...)
		if err != nil {
			slog.Warn("Erreur de compilation template (ignoré)",
				"template", name,
				"error", err,
			)
			continue
		}

		e.mu.Lock()
		e.templates[name] = tmpl
		e.mu.Unlock()

		slog.Debug("Template chargé", "name", name)
	}

	slog.Info("Templates HTML chargés",
		"count", len(e.templates),
		"dir", e.dir,
	)

	return nil
}

// Render exécute un template avec les données fournies.
func (e *Engine) Render(w io.Writer, name string, data *TemplateData) error {
	e.mu.RLock()
	tmpl, ok := e.templates[name]
	e.mu.RUnlock()

	if !ok {
		return fmt.Errorf("render: template %q introuvable", name)
	}

	return tmpl.Execute(w, data)
}
