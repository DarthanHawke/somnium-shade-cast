package domain

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// артикли разных языков
var articles = map[string][]string{
	"en": {"the", "a", "an"},
	"fr": {"le", "la", "les", "l'", "un", "une", "des"},
	"de": {"der", "die", "das", "ein", "eine"},
	"es": {"el", "la", "los", "las", "un", "una", "unos", "unas"},
	"it": {"il", "lo", "la", "i", "gli", "le", "l'", "un", "uno", "una"},
	"pt": {"o", "a", "os", "as", "um", "uma", "uns", "umas"},
	"nl": {"de", "het", "een"},
}

// Normalize -  возвращает строку в нижнем регистре, слова через "_"
func Normalize(s string) string {
	s = norm.NFKC.String(s)

	s = strings.ToLower(s)

	var words []string
	var currentWord strings.Builder

	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'' {
			currentWord.WriteRune(r)
		} else if currentWord.Len() > 0 {
			words = append(words, currentWord.String())
			currentWord.Reset()
		}
	}

	if currentWord.Len() > 0 {
		words = append(words, currentWord.String())
	}

	if len(words) == 0 {
		return strings.TrimSpace(s)
	}

	return strings.Join(words, "_")
}

// NormalizeSort переносит отдельный начальный артикль в конец имени.
func NormalizeSort(s string) string {
	s = strings.TrimSpace(norm.NFKC.String(s))
	words := strings.Fields(s)
	if len(words) < 2 {
		return s
	}

	first := words[0]
	for _, langArticles := range articles {
		for _, article := range langArticles {
			if strings.EqualFold(first, article) {
				rest := strings.TrimSpace(s[len(first):])
				return rest + ", " + first
			}
		}
	}
	return s
}
