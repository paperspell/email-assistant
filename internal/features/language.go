package features

import (
	"strings"
	"sync"

	lingua "github.com/pemistahl/lingua-go"
)

var (
	detector lingua.LanguageDetector
	detOnce  sync.Once
)

var supportedLanguages = []lingua.Language{
	lingua.English,
	lingua.Polish,
	lingua.Russian,
	lingua.Romanian,
	lingua.Italian,
	lingua.Belarusian,
	lingua.Ukrainian,
	lingua.Spanish,
	lingua.Portuguese,
	lingua.French,
	lingua.German,
	lingua.Kazakh,
	lingua.Hebrew,
}

func initDetector() {
	detOnce.Do(func() {
		detector = lingua.NewLanguageDetectorBuilder().
			FromLanguages(supportedLanguages...).
			Build()
	})
}

// DetectLanguage returns the ISO 639-1 code for the detected language of text,
// or "und" (undetermined) when confidence is too low.
func DetectLanguage(text string) string {
	if strings.TrimSpace(text) == "" {
		return "und"
	}
	initDetector()
	lang, exists := detector.DetectLanguageOf(text)
	if !exists {
		return "und"
	}
	return strings.ToLower(lang.IsoCode639_1().String())
}
