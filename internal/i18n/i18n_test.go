package i18n

import (
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveLocale(t *testing.T) {
	tests := map[string]string{
		"ru":      "ru",
		"RU":      "ru",
		"ru-RU":   "ru",
		"ru_RU":   "ru",
		"Russian": "ru", // значение, которое хранили прежние версии
		"русский": "ru",
		"Polish":  "pl",
		"":        "en",
		"klingon": "en",
	}

	for in, want := range tests {
		assert.Equal(t, want, ResolveLocale(in), "вход %q", in)
	}
}

func TestPrinter_FallsBackToEnglish(t *testing.T) {
	p, err := NewPrinter("klingon")
	require.NoError(t, err)

	assert.Equal(t, "Subject", p.T("field_subject"))
}

func TestPrinter_Russian(t *testing.T) {
	p, err := NewPrinter("ru")
	require.NoError(t, err)

	assert.Equal(t, "Тема", p.T("field_subject"))
	assert.Equal(t, "Важность: важно (оценка 82)",
		p.T("notification_importance", "Level", p.T("level_important"), "Score", 82))
}

func TestPrinter_UnknownIDReturnsID(t *testing.T) {
	p, err := NewPrinter("en")
	require.NoError(t, err)

	// Отсутствующий ключ не должен ронять отправку уведомления.
	assert.Equal(t, "no_such_key", p.T("no_such_key"))
}

func TestPrinter_PluralForms(t *testing.T) {
	ru, err := NewPrinter("ru")
	require.NoError(t, err)

	// Русский требует трёх форм — ради этого и взята библиотека с правилами CLDR.
	assert.Contains(t, ru.N("digest_filtered", 1), "1 письмо")
	assert.Contains(t, ru.N("digest_filtered", 3), "3 письма")
	assert.Contains(t, ru.N("digest_filtered", 7), "7 писем")

	en, err := NewPrinter("en")
	require.NoError(t, err)
	assert.Contains(t, en.N("digest_filtered", 3), "+3 filtered")
}

func TestCatalogs_AreComplete(t *testing.T) {
	// Дыра в переводе должна ронять сборку, а не всплывать у пользователя.
	english := messageIDs(t, "en")
	require.NotEmpty(t, english)

	for _, loc := range Supported {
		ids := messageIDs(t, loc)
		for id := range english {
			assert.Contains(t, ids, id, "в каталоге %s нет ключа %s", loc, id)
		}
		for id := range ids {
			assert.Contains(t, english, id, "в каталоге %s лишний ключ %s", loc, id)
		}
	}
}

// messageIDs reads the ids declared in one catalog.
func messageIDs(t *testing.T, locale string) map[string]struct{} {
	t.Helper()
	raw, err := catalogs.ReadFile("locales/" + locale + ".toml")
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, tomlUnmarshal(raw, &parsed))

	ids := make(map[string]struct{}, len(parsed))
	for id := range parsed {
		ids[id] = struct{}{}
	}
	return ids
}

// The re-auth alert is sent with HTML parse mode and passes <code> markup
// through a template placeholder. If the catalog templates were HTML-escaping,
// the tags would reach Telegram as literal text.
func TestPrinter_TemplateDoesNotEscapeHTML(t *testing.T) {
	p, err := NewPrinter("en")
	assert.NoError(t, err)
	out := p.T("reauth_step_edit", "Command", "<code>email-agent account edit a@b.com</code>")
	assert.Contains(t, out, "<code>email-agent account edit a@b.com</code>")
	assert.NotContains(t, out, "&lt;code&gt;")
}

// Plural forms differ per language: Polish and the East Slavic locales need
// one/few/many/other, Hebrew needs two, Kazakh only one/other. A catalog missing
// a form its language requires does not fail to load — go-i18n simply cannot
// localize that count, and the id leaks into the message. This walks every
// locale, every plural message and the counts that select each CLDR category.
func TestCatalogs_HaveEveryPluralFormTheyNeed(t *testing.T) {
	pluralIDs := []string{
		"digest_filtered", "digest_promoted",
		"digest_marked_read", "digest_moved_trash",
	}
	counts := []int{0, 1, 2, 3, 5, 7, 11, 21, 22, 25, 100, 101}

	for _, loc := range Supported {
		p, err := NewPrinter(loc)
		require.NoError(t, err)
		for _, id := range pluralIDs {
			for _, n := range counts {
				got := p.N(id, n)
				assert.NotEqual(t, id, got,
					"locale %s has no plural form for %s at count %d", loc, id, n)
				assert.Contains(t, got, strconv.Itoa(n),
					"locale %s dropped the count from %s at %d", loc, id, n)
			}
		}
	}
}

// A translation that drops a placeholder does not fail to load — it silently
// renders a message with the data missing, e.g. an importance line with no
// score. Every catalog must carry exactly the placeholders English declares.
func TestCatalogs_PreservePlaceholders(t *testing.T) {
	want := placeholders(t, "en")
	require.NotEmpty(t, want)

	for _, loc := range Supported {
		if loc == "en" {
			continue
		}
		got := placeholders(t, loc)
		for id, ph := range want {
			assert.Equal(t, ph, got[id],
				"locale %s: placeholders of %s differ from English", loc, id)
		}
	}
}

// Slash commands are typed by the user into Telegram, so they must survive
// translation verbatim — a localized "/ważne" would simply not work.
func TestCatalogs_KeepCommandsLiteral(t *testing.T) {
	for _, loc := range Supported {
		p, err := NewPrinter(loc)
		require.NoError(t, err)
		for _, id := range []string{"digest_keep_hint", "digest_reply_hint", "digest_important_usage"} {
			assert.Contains(t, p.T(id), "/important",
				"locale %s dropped the /important command from %s", loc, id)
		}
	}
}

// placeholders maps each message id to the sorted set of {{.Name}} templates it
// uses, across every plural form.
func placeholders(t *testing.T, locale string) map[string][]string {
	t.Helper()
	data, err := catalogs.ReadFile("locales/" + locale + ".toml")
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, tomlUnmarshal(data, &raw))

	re := regexp.MustCompile(`{{\s*\.(\w+)\s*}}`)
	out := make(map[string][]string, len(raw))
	for id, v := range raw {
		seen := map[string]bool{}
		switch m := v.(type) {
		case map[string]any:
			for _, form := range m {
				if s, ok := form.(string); ok {
					for _, g := range re.FindAllStringSubmatch(s, -1) {
						seen[g[1]] = true
					}
				}
			}
		case string:
			for _, g := range re.FindAllStringSubmatch(m, -1) {
				seen[g[1]] = true
			}
		}
		names := make([]string, 0, len(seen))
		for n := range seen {
			names = append(names, n)
		}
		sort.Strings(names)
		out[id] = names
	}
	return out
}
