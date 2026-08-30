package telegram

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/i18n"
)

var testClassification = domain.Classification{
	Level:    domain.LevelImportant,
	Category: domain.CategoryWork,
	Score:    75,
	Reason: []string{
		"baseline: +40", "urgent keyword in subject: +25",
		"meeting keyword in subject: +20", "unknown sender: -10",
	},
}

func TestFormatMessage_WithName(t *testing.T) {
	e := domain.Email{
		FromName:  "Alice Smith",
		FromEmail: "alice@example.com",
		Subject:   "Project update",
		Date:      time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC),
	}
	msg := formatMessage(i18n.English(), e, testClassification, "Work", "work@acme.com")
	// Angle brackets around the address are HTML-escaped for Telegram's parser.
	assert.Contains(t, msg, "Alice Smith &lt;alice@example.com&gt;")
	assert.Contains(t, msg, "Project update")
	assert.Contains(t, msg, "New email")
	assert.Contains(t, msg, "important")
	assert.Contains(t, msg, "75")
	// Importance is emphasised in bold with an icon for the level.
	assert.Contains(t, msg, "<b>🟠 Importance: important (score 75)</b>")
	// The source account is labelled with name and address (escaped).
	assert.Contains(t, msg, "Account: Work &lt;work@acme.com&gt;")
}

func TestFormatMessage_AccountLabelVariants(t *testing.T) {
	e := domain.Email{FromEmail: "x@y.com", Date: time.Now()}

	// Name + email → "Name <email>".
	withBoth := formatMessage(i18n.English(), e, testClassification, "Work", "work@acme.com")
	assert.Contains(t, withBoth, "Account: Work &lt;work@acme.com&gt;")

	// Email only (no name) → email alone, so accounts stay distinguishable.
	emailOnly := formatMessage(i18n.English(), e, testClassification, "", "work@acme.com")
	assert.Contains(t, emailOnly, "Account: work@acme.com")
	assert.NotContains(t, emailOnly, "&lt;")

	// Neither → no account line.
	neither := formatMessage(i18n.English(), e, testClassification, "", "")
	assert.NotContains(t, neither, "Account:")
}

func TestFormatMessage_WithoutName(t *testing.T) {
	e := domain.Email{
		FromEmail: "bob@example.com",
		Subject:   "Hello",
		Date:      time.Now(),
	}
	msg := formatMessage(i18n.English(), e, testClassification, "", "")
	assert.Contains(t, msg, "bob@example.com")
	// A bare address is not wrapped in angle brackets (escaped or otherwise).
	assert.NotContains(t, msg, "&lt;")
}

func TestFormatMessage_EmptySubject(t *testing.T) {
	e := domain.Email{
		FromEmail: "x@y.com",
		Date:      time.Now(),
	}
	assert.NotPanics(t, func() { formatMessage(i18n.English(), e, testClassification, "", "") })
}

func TestFormatMessage_ShowsSummaryWhenPresent(t *testing.T) {
	e := domain.Email{FromEmail: "x@y.com", Date: time.Now()}
	c := domain.Classification{
		Level:   domain.LevelImportant,
		Score:   80,
		Summary: "Quarterly budget review needs approval.",
	}
	msg := formatMessage(i18n.English(), e, c, "", "")
	assert.Contains(t, msg, "Summary: Quarterly budget review needs approval.")
	assert.NotContains(t, msg, "Why:")
}

func TestFormatMessage_ShowsReasonsWhenNoSummary(t *testing.T) {
	e := domain.Email{FromEmail: "x@y.com", Date: time.Now()}
	msg := formatMessage(i18n.English(), e, testClassification, "", "")
	assert.Contains(t, msg, "Why:")
	assert.Contains(t, msg, "baseline: +40")
}

func TestFormatMessage_Layout(t *testing.T) {
	e := domain.Email{
		Subject: "Payment receipt", FromEmail: "billing@github.com", FromName: "GitHub",
		Date: time.Date(2026, 8, 28, 9, 30, 0, 0, time.UTC), Language: "pl",
	}
	c := domain.Classification{Level: domain.LevelImportant, Score: 82, Summary: "Списание за подписку."}

	out := formatMessage(i18n.English(), e, c, "anovikau@gmail.com", "anovikau@gmail.com")

	// Тема идёт сразу за строкой важности, отдельным блоком.
	assert.Regexp(t, `Importance: important \(score 82\)</b>\n\nSubject: Payment receipt\n\n`, out)
	// Адрес аккаунта не задваивается, когда имя совпадает с адресом.
	assert.Contains(t, out, "Account: anovikau@gmail.com\n")
	assert.NotContains(t, out, "anovikau@gmail.com &lt;anovikau@gmail.com&gt;")
	// Язык оригинала — отдельной строкой, резюме — через пустую строку.
	assert.Contains(t, out, "Original language: Polish")
	assert.Contains(t, out, "\n\nSummary: Списание за подписку.")
}

func TestFormatMessage_UnknownLanguageOmitsLine(t *testing.T) {
	e := domain.Email{Subject: "s", FromEmail: "a@b.com", Date: time.Now(), Language: "und"}
	c := domain.Classification{Level: domain.LevelMaybe, Score: 40, Summary: "x"}

	out := formatMessage(i18n.English(), e, c, "", "a@b.com")

	assert.NotContains(t, out, "Original language:")
}

func TestFormatMessage_Russian(t *testing.T) {
	ru, err := i18n.NewPrinter("ru")
	require.NoError(t, err)

	e := domain.Email{
		Subject: "Payment receipt", FromEmail: "billing@github.com",
		Date: time.Date(2026, 8, 28, 9, 30, 0, 0, time.UTC), Language: "pl",
	}
	c := domain.Classification{Level: domain.LevelImportant, Score: 82, Summary: "Списание за подписку."}

	out := formatMessage(ru, e, c, "", "anovikau@gmail.com")

	assert.Contains(t, out, "📧 Новое письмо")
	assert.Contains(t, out, "Важность: важно (оценка 82)")
	assert.Contains(t, out, "Тема: Payment receipt")
	assert.Contains(t, out, "Аккаунт: anovikau@gmail.com")
	// Язык оригинала тоже на языке пользователя, а не "Polish".
	assert.Contains(t, out, "Язык оригинала: польский")
	assert.Contains(t, out, "Резюме: Списание за подписку.")
	// Английские подписи не должны просачиваться.
	assert.NotContains(t, out, "Importance:")
	assert.NotContains(t, out, "Subject:")
}

func TestFormatMessage_NilPrinterFallsBackToEnglish(t *testing.T) {
	// Забытая проводка Printer должна давать английский текст, а не сырые
	// идентификаторы строк вроде "field_subject".
	e := domain.Email{Subject: "s", FromEmail: "a@b.com", Date: time.Now(), Language: "ru"}

	out := formatMessage(nil, e, testClassification, "", "a@b.com")

	assert.Contains(t, out, "Subject: s")
	assert.Contains(t, out, "Original language: Russian")
	assert.NotContains(t, out, "field_subject")
	assert.NotContains(t, out, "notification_importance")
}

func TestFormatMessage_RomanianIsNamed(t *testing.T) {
	// Детектор умеет румынский, а прежняя таблица — нет: язык печатался как "RO".
	e := domain.Email{Subject: "s", FromEmail: "a@b.com", Date: time.Now(), Language: "ro"}

	out := formatMessage(i18n.English(), e, testClassification, "", "a@b.com")

	assert.Contains(t, out, "Original language: Romanian")
}
