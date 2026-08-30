package features

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainsAnyWord_WholeWordsOnly(t *testing.T) {
	kw := []string{"charged", "чек", "opłata"}

	// Совпадение только по целому слову.
	assert.True(t, containsAnyWord("You were charged $12", kw))
	assert.True(t, containsAnyWord("Кассовый чек за поездку", kw))
	assert.True(t, containsAnyWord("Twoja opłata za usługi", kw))

	// Внутри более длинного слова — не срабатывает.
	assert.False(t, containsAnyWord("Patient discharged from hospital", kw))
	assert.False(t, containsAnyWord("Opłatach nie ma", kw))
}

func TestExtract_MoneyKeyword(t *testing.T) {
	cases := []struct {
		subject string
		want    bool
	}{
		{"GitHub Invoice INV152315913", true},
		{"Potwierdzenie Twojej płatności", true},
		{"Списание по карте", true},
		{"Ihre Rechnung für August", true},
		{"Patient discharged today", false},
		{"Weekly product update", false},
	}

	for _, tc := range cases {
		got := Extract(msg(tc.subject, "a@b.com", "", "", ""), 0, 0, 0).HasMoneyKeyword
		assert.Equal(t, tc.want, got, "subject %q", tc.subject)
	}
}

func TestExtract_MoneyKeyword_BulkPrecedenceIsSeparate(t *testing.T) {
	// Признак денег и признак массовой рассылки независимы: решение об обходе
	// порога принимает планировщик, комбинируя оба. Здесь же проверяется, что
	// Precedence разбирается канонически — с учётом регистра и пробелов.
	f := Extract(msg("Invoice for August", "a@b.com", "", "", "  Bulk "), 0, 0, 0)

	assert.True(t, f.HasMoneyKeyword)
	assert.True(t, f.IsBulkPrecedence, "Bulk с пробелами и заглавной должен считаться массовой рассылкой")
}
