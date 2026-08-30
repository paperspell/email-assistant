package i18n

import (
	"embed"
	"fmt"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"golang.org/x/text/language"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
)

//go:embed locales/*.toml
var catalogs embed.FS

var (
	bundleOnce sync.Once
	bundle     *goi18n.Bundle
	bundleErr  error
)

// load parses every embedded catalog once. Catalogs ship inside the binary, so a
// deployment cannot end up with a missing translation file.
// tomlUnmarshal is the parser the bundle uses, exposed for the completeness test.
var tomlUnmarshal = toml.Unmarshal

func load() (*goi18n.Bundle, error) {
	bundleOnce.Do(func() {
		b := goi18n.NewBundle(language.English)
		b.RegisterUnmarshalFunc("toml", toml.Unmarshal)
		for _, loc := range Supported {
			if _, err := b.LoadMessageFileFS(catalogs, "locales/"+loc+".toml"); err != nil {
				bundleErr = fmt.Errorf("load catalog %s: %w", loc, err)
				return
			}
		}
		bundle = b
	})
	return bundle, bundleErr
}

// Printer renders messages in one locale.
type Printer struct {
	loc *goi18n.Localizer
}

var (
	englishOnce sync.Once
	english     *Printer
)

// English returns the fallback printer. It is also what a nil Printer resolves
// to, so a call site that was never given one degrades to English text rather
// than printing raw message ids at the user.
func English() *Printer {
	englishOnce.Do(func() {
		b, err := load()
		if err != nil {
			// Catalogs are embedded and covered by the completeness test, so a
			// failure here is a build-time mistake, not a runtime condition.
			english = &Printer{}
			return
		}
		english = &Printer{loc: goi18n.NewLocalizer(b, "en")}
	})
	return english
}

// localizer resolves the receiver to a usable localizer, tolerating a nil
// Printer.
func (p *Printer) localizer() *goi18n.Localizer {
	if p != nil && p.loc != nil {
		return p.loc
	}
	if e := English(); e != nil {
		return e.loc
	}
	return nil
}

// NewPrinter returns a Printer for the given locale, falling back to English for
// anything unsupported.
func NewPrinter(locale string) (*Printer, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	return &Printer{loc: goi18n.NewLocalizer(b, ResolveLocale(locale), "en")}, nil
}

// T renders the message with the given id. Named arguments fill template
// placeholders. A missing id returns the id itself rather than an error: a
// notification with one untranslated label is worth more than no notification,
// and the catalog-completeness test keeps ids from going missing in the first
// place.
func (p *Printer) T(id string, args ...any) string {
	loc := p.localizer()
	if loc == nil {
		return id
	}
	cfg := &goi18n.LocalizeConfig{MessageID: id}
	if len(args) > 0 {
		cfg.TemplateData = templateData(args...)
	}
	s, err := loc.Localize(cfg)
	if err != nil {
		return id
	}
	return s
}

// N renders a message that varies with a count, using the plural rules of the
// locale — three forms in Russian and Polish, two in English.
func (p *Printer) N(id string, count int, args ...any) string {
	loc := p.localizer()
	if loc == nil {
		return id
	}
	data := templateData(args...)
	data["Count"] = count
	s, err := loc.Localize(&goi18n.LocalizeConfig{
		MessageID:    id,
		PluralCount:  count,
		TemplateData: data,
	})
	if err != nil {
		return id
	}
	return s
}

// templateData turns alternating key/value arguments into a template map.
func templateData(args ...any) map[string]any {
	data := make(map[string]any, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		data[key] = args[i+1]
	}
	return data
}

// EmailLanguage renders the detected language of an incoming email in the
// user's own language. The detector emits ISO 639-1 codes for a fixed set of
// languages, all of which have a catalog entry; an unmapped code falls back to
// its uppercased form ("sv") rather than to the raw message id. "und" means
// detection failed, which is not worth a line in the notification.
func (p *Printer) EmailLanguage(code string) string {
	if code == "" || code == "und" {
		return ""
	}
	id := "lang_" + code
	if name := p.T(id); name != id {
		return name
	}
	return strings.ToUpper(code)
}
