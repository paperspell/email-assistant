package i18n

import (
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
