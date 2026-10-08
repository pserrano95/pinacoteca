package slug

import (
	"regexp"
	"strings"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// translit maps common Latin-1 letters to ASCII for readable folder names.
var translit = map[rune]string{
	'á': "a", 'à': "a", 'ä': "a", 'â': "a", 'ã': "a",
	'é': "e", 'è': "e", 'ë': "e", 'ê': "e",
	'í': "i", 'ì': "i", 'ï': "i", 'î': "i",
	'ó': "o", 'ò': "o", 'ö': "o", 'ô': "o", 'õ': "o",
	'ú': "u", 'ù': "u", 'ü': "u", 'û': "u",
	'ñ': "n", 'ç': "c",
	'Á': "a", 'À': "a", 'Ä': "a", 'Â': "a", 'Ã': "a",
	'É': "e", 'È': "e", 'Ë': "e", 'Ê': "e",
	'Í': "i", 'Ì': "i", 'Ï': "i", 'Î': "i",
	'Ó': "o", 'Ò': "o", 'Ö': "o", 'Ô': "o", 'Õ': "o",
	'Ú': "u", 'Ù': "u", 'Ü': "u", 'Û': "u",
	'Ñ': "n", 'Ç': "c",
}

// Make turns a human name or title into a URL- and filesystem-safe slug.
func Make(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if rep, ok := translit[r]; ok {
			b.WriteString(rep)
			continue
		}
		b.WriteRune(r)
	}
	result := strings.ToLower(b.String())
	result = nonAlnum.ReplaceAllString(result, "-")
	result = strings.Trim(result, "-")
	if result == "" {
		return "untitled"
	}
	return result
}
