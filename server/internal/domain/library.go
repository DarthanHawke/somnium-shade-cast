package domain

import (
	"strings"
	"time"
	"unicode"
)

type EnrichStatus string

const (
	EnrichPending EnrichStatus = "pending"
	EnrichDone    EnrichStatus = "done"
	EnrichFailed  EnrichStatus = "failed"
	EnrichManual  EnrichStatus = "manual"
)

type Artist struct {
	ID          ID              `db:"id"`
	PublicID    PublicID        `db:"public_id"`
	Name        string          `db:"name"`
	SortName    string          `db:"sort_name"`
	NormName    string          `db:"norm_name"`
	MBID        string          `db:"mbid"`
	LastfmURL   string          `db:"lastfm_url"`
	ImageBlobID *ID             `db:"image_blob_id"`
	Bio         string          `db:"bio"`
	Enrich      EnrichStatus    `db:"enrich_status"`
	EnrichedAt  *UnixTimeMillis `db:"enriched_at"`
	CreatedAt   UnixTimeMillis  `db:"created_at"`
	UpdatedAt   UnixTimeMillis  `db:"updated_at"`
}

func NewArtist(name string) (*Artist, error) {
	if name == "" {
		return nil, Invalid("title", "required")
	}

	return &Artist{
		PublicID:  NewPublicID(),
		Name:      name,
		NormName:  Normalize(name),
		SortName:  NormalizeSort(name),
		CreatedAt: UnixTimeMillis(time.Now()),
		UpdatedAt: UnixTimeMillis(time.Now()),
	}, nil
}

type Album struct {
	ID          ID              `db:"id"`
	PublicID    PublicID        `db:"public_id"`
	ArtistID    ID              `db:"artist_id"`
	Title       string          `db:"title"`
	NormTitle   string          `db:"norm_title"`
	Year        *int            `db:"year"`
	ReleaseDate *UnixTimeMillis `db:"release_date"`
	MBID        string          `db:"mbid"`
	LastfmURL   string          `db:"lastfm_url"`
	CoverBlobID *ID             `db:"cover_blob_id"`
	TotalTracks *int            `db:"total_tracks"`
	IsComplete  bool            `db:"is_complete"`
	Enrich      EnrichStatus    `db:"enrich_status"`
	EnrichedAt  *UnixTimeMillis `db:"enriched_at"`
	CreatedAt   UnixTimeMillis  `db:"created_at"`
	UpdatedAt   UnixTimeMillis  `db:"updated_at"`
}

func NewAlbum(artistID ID, title string, year *int, coverBlobID *ID) (*Album, error) {
	if title == "" {
		return nil, Invalid("title", "required")
	}

	return &Album{
		PublicID:    NewPublicID(),
		ArtistID:    artistID,
		Title:       title,
		NormTitle:   Normalize(title),
		Year:        year,
		CoverBlobID: coverBlobID,
		CreatedAt:   UnixTimeMillis(time.Now()),
		UpdatedAt:   UnixTimeMillis(time.Now()),
	}, nil
}

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

	articleMap := make(map[string]bool)
	for _, langArticles := range articles {
		for _, art := range langArticles {
			articleMap[strings.ToLower(art)] = true
		}
	}

	var filtered []string
	for _, word := range words {
		if !articleMap[word] {
			filtered = append(filtered, word)
		}
	}

	return strings.Join(filtered, "_")
}

// normalizeSort - нормализация для сортировки
// "The Beatles" - "Beatles, The"
func NormalizeSort(s string) string {
	s = strings.TrimSpace(s)

	var allArticles []string
	for _, langArticles := range articles {
		for _, art := range langArticles {
			allArticles = append(allArticles, art)
		}
	}

	sortArticlesByLength(allArticles)

	lowerS := strings.ToLower(s)

	for _, article := range allArticles {
		lowerArticle := strings.ToLower(article)
		if strings.HasPrefix(lowerS, lowerArticle+" ") {
			rest := strings.TrimSpace(s[len(article):])
			return rest + ", " + strings.TrimSpace(article)
		}
	}

	words := strings.Fields(s)
	if len(words) > 1 {
		return words[len(words)-1] + ", " + strings.Join(words[:len(words)-1], " ")
	}
	return s
}

// sortArticlesByLength - сортировка артиклей по длине
func sortArticlesByLength(articles []string) {
	for i := range articles {
		for j := i + 1; j < len(articles); j++ {
			if len(articles[i]) < len(articles[j]) {
				articles[i], articles[j] = articles[j], articles[i]
			}
		}
	}
}
