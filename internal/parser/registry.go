package parser

import (
	"path/filepath"
	"strings"
)

// Registry maps languages and file extensions to extractors.
type Registry struct {
	extractors map[string]Extractor // language name -> extractor
	extMap     map[string]string    // file extension (with dot) -> language name
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		extractors: make(map[string]Extractor),
		extMap:     make(map[string]string),
	}
}

// Register adds an extractor and maps its extensions.
func (r *Registry) Register(e Extractor) {
	lang := e.Language()
	r.extractors[lang] = e
	for _, ext := range e.Extensions() {
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		r.extMap[ext] = lang
	}
}

// GetByLanguage returns the extractor for the given language name.
func (r *Registry) GetByLanguage(lang string) (Extractor, bool) {
	e, ok := r.extractors[lang]
	return e, ok
}

// GetByExtension returns the extractor for the given file extension.
func (r *Registry) GetByExtension(ext string) (Extractor, bool) {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	lang, ok := r.extMap[ext]
	if !ok {
		return nil, false
	}
	return r.extractors[lang], true
}

// DetectLanguage determines the language for a file path by extension.
func (r *Registry) DetectLanguage(filePath string) (string, bool) {
	ext := filepath.Ext(filePath)
	lang, ok := r.extMap[ext]
	return lang, ok
}

// SupportedLanguages returns all registered language names.
func (r *Registry) SupportedLanguages() []string {
	langs := make([]string, 0, len(r.extractors))
	for lang := range r.extractors {
		langs = append(langs, lang)
	}
	return langs
}
