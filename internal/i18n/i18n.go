package i18n

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	_ "embed"
)

//go:embed locales/en.json
var enJSON []byte

//go:embed locales/es.json
var esJSON []byte

var (
	once      sync.Once
	catalogs  map[string]map[string]string
	loadError error
)

func load() {
	catalogs = map[string]map[string]string{}
	for lang, raw := range map[string][]byte{"en": enJSON, "es": esJSON} {
		m := map[string]string{}
		if err := json.Unmarshal(raw, &m); err != nil {
			loadError = fmt.Errorf("i18n %s: %w", lang, err)
			return
		}
		catalogs[lang] = m
	}
}

// T returns the translated string for lang and key, with {name} replacements.
func T(lang, key string, vars map[string]string) string {
	once.Do(load)
	if loadError != nil {
		return key
	}
	if lang != "es" && lang != "en" {
		lang = "en"
	}
	s, ok := catalogs[lang][key]
	if !ok {
		s = catalogs["en"][key]
	}
	if s == "" {
		return key
	}
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

// NormalizeLang returns "es" or "en".
func NormalizeLang(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if strings.HasPrefix(lang, "es") {
		return "es"
	}
	return "en"
}
