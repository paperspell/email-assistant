// Package i18n renders the strings the bot sends to Telegram in the user's
// language. Only user-facing Telegram text is translated: CLI prompts and log
// lines stay in English, where a single language keeps support and grep simple.
package i18n

import "strings"

// Supported lists the locales shipped with the binary, matching the languages
// the Paperspell mobile app is translated into. English is first and is the
// fallback for anything else.
var Supported = []string{"en", "be", "de", "es", "fr", "he", "it", "kk", "pl", "pt", "ru", "uk"}

// legacyNames maps the free-text language values the settings used to hold
// ("Russian") onto locale codes, so an existing install keeps its language
// without the owner re-running setup.
var legacyNames = map[string]string{
	"english":    "en",
	"belarusian": "be",
	"german":     "de",
	"deutsch":    "de",
	"spanish":    "es",
	"español":    "es",
	"french":     "fr",
	"français":   "fr",
	"hebrew":     "he",
	"italian":    "it",
	"italiano":   "it",
	"kazakh":     "kk",
	"polish":     "pl",
	"polski":     "pl",
	"portuguese": "pt",
	"português":  "pt",
	"russian":    "ru",
	"русский":    "ru",
	"ukrainian":  "uk",
	"українська": "uk",
}

// ResolveLocale maps a configured notification.language onto a supported locale
// code. It accepts a code ("ru"), a region-qualified code ("ru-RU"), or one of
// the language names earlier versions stored. Anything unrecognised — including
// an empty setting — falls back to English rather than failing: a wrong-language
// notification is still a delivered notification.
func ResolveLocale(setting string) string {
	s := strings.ToLower(strings.TrimSpace(setting))
	if s == "" {
		return "en"
	}
	if code, ok := legacyNames[s]; ok {
		return code
	}
	// "ru-RU" and "ru_RU" both carry the language in the first segment.
	if i := strings.IndexAny(s, "-_"); i > 0 {
		s = s[:i]
	}
	for _, sup := range Supported {
		if s == sup {
			return s
		}
	}
	return "en"
}

// LanguageName returns the English name of a locale, for the LLM prompt that
// asks for a summary in the user's language.
func LanguageName(locale string) string {
	switch locale {
	case "be":
		return "Belarusian"
	case "de":
		return "German"
	case "es":
		return "Spanish"
	case "fr":
		return "French"
	case "he":
		return "Hebrew"
	case "it":
		return "Italian"
	case "kk":
		return "Kazakh"
	case "pl":
		return "Polish"
	case "pt":
		return "Portuguese"
	case "ru":
		return "Russian"
	case "uk":
		return "Ukrainian"
	default:
		return "English"
	}
}
