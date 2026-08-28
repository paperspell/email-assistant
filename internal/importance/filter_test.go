package importance

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/features"
)

func feats(opts ...func(*features.EmailFeatures)) features.EmailFeatures {
	f := features.EmailFeatures{FromEmail: "a@b.com", FromDomain: "b.com"}
	for _, o := range opts {
		o(&f)
	}
	return f
}

func withNewsletter(f *features.EmailFeatures) {
	f.HasListUnsubscribe = true
	f.IsBulkPrecedence = true
}

func withUrgent(f *features.EmailFeatures)   { f.HasUrgentKeyword = true }
func withMeeting(f *features.EmailFeatures)  { f.HasMeetingKeyword = true }
func withInvoice(f *features.EmailFeatures)  { f.HasInvoiceKeyword = true }
func withSecurity(f *features.EmailFeatures) { f.HasSecurityKeyword = true }
func withGovt(f *features.EmailFeatures)     { f.HasGovernmentKeyword = true }
func withReply(f *features.EmailFeatures)    { f.IsReply = true }
func withUnknown(f *features.EmailFeatures)  { f.SenderSeenCount = 0 }
func withKnown(f *features.EmailFeatures)    { f.SenderSeenCount = 5 }

// --- Score tests ---

func TestScore_Newsletter_IsIgnore(t *testing.T) {
	score, _ := Score(feats(withNewsletter))
	assert.Equal(t, domain.LevelIgnore, Level(score))
}

func TestScore_GovernmentKeyword_IsImportant(t *testing.T) {
	score, _ := Score(feats(withGovt, withKnown))
	assert.GreaterOrEqual(t, score, 70)
}

func TestScore_SecurityKeyword_IsAtLeastMaybe(t *testing.T) {
	score, _ := Score(feats(withSecurity))
	assert.GreaterOrEqual(t, score, 30)
}

func TestScore_MeetingAndReply_IsImportant(t *testing.T) {
	score, _ := Score(feats(withMeeting, withReply, withKnown))
	assert.Equal(t, domain.LevelImportant, Level(score))
}

func TestScore_InvoiceKeyword_IsAtLeastMaybe(t *testing.T) {
	score, _ := Score(feats(withInvoice))
	assert.GreaterOrEqual(t, score, 30)
}

func TestScore_NoSignals_UnknownSender_IsIgnoreOrMaybe(t *testing.T) {
	score, _ := Score(feats(withUnknown))
	assert.LessOrEqual(t, score, 30)
}

func TestScore_Clamp_Max(t *testing.T) {
	score, _ := Score(feats(withGovt, withUrgent, withSecurity, withMeeting, withReply, withKnown))
	assert.LessOrEqual(t, score, 100)
}

func TestScore_Clamp_Min(t *testing.T) {
	score, _ := Score(feats(withNewsletter, withUnknown))
	assert.GreaterOrEqual(t, score, 0)
}

func TestScore_HasReasons(t *testing.T) {
	_, reasons := Score(feats(withMeeting, withReply))
	assert.NotEmpty(t, reasons)
}

// --- Level tests ---

func TestLevel_Critical(t *testing.T) {
	assert.Equal(t, domain.LevelCritical, Level(90))
	assert.Equal(t, domain.LevelCritical, Level(100))
}

func TestLevel_Important(t *testing.T) {
	assert.Equal(t, domain.LevelImportant, Level(70))
	assert.Equal(t, domain.LevelImportant, Level(89))
}

func TestLevel_Maybe(t *testing.T) {
	assert.Equal(t, domain.LevelMaybe, Level(30))
	assert.Equal(t, domain.LevelMaybe, Level(69))
}

func TestLevel_Ignore(t *testing.T) {
	assert.Equal(t, domain.LevelIgnore, Level(0))
	assert.Equal(t, domain.LevelIgnore, Level(29))
}

// --- Category tests ---

func TestCategory_Government(t *testing.T) {
	assert.Equal(t, domain.CategoryGovernment, Category(feats(withGovt)))
}

func TestCategory_Security(t *testing.T) {
	assert.Equal(t, domain.CategorySecurity, Category(feats(withSecurity)))
}

func TestCategory_Finance(t *testing.T) {
	assert.Equal(t, domain.CategoryFinance, Category(feats(withInvoice)))
}

func TestCategory_Marketing(t *testing.T) {
	assert.Equal(t, domain.CategoryMarketing, Category(feats(withNewsletter)))
}

func TestCategory_Other(t *testing.T) {
	assert.Equal(t, domain.CategoryOther, Category(feats()))
}

// withUnsubscribeOnly marks mail from bulk-capable infrastructure that is not a
// bulk send: banks, government portals and invoicing systems set List-Unsubscribe
// without Precedence: bulk.
func withUnsubscribeOnly(f *features.EmailFeatures) { f.HasListUnsubscribe = true }

func TestScore_TransactionalWithUnsubscribe_ReachesMaybe(t *testing.T) {
	// Регрессия: раньше -40 за List-Unsubscribe обнуляло базовые +40, счёт
	// падал до нуля, и письмо отсеивалось до вызова LLM.
	score, _ := Score(feats(withUnsubscribeOnly, withInvoice))

	assert.GreaterOrEqual(t, score, 30, "счёт заказа/счёта с заголовком отписки")
	assert.Equal(t, domain.LevelMaybe, Level(score))
}

func TestScore_GovernmentWithUnsubscribe_StaysAtLeastMaybe(t *testing.T) {
	score, _ := Score(feats(withUnsubscribeOnly, withGovt))

	assert.GreaterOrEqual(t, score, 30)
}

func TestScore_RealNewsletter_StaysIgnore(t *testing.T) {
	// Настоящая рассылка несёт оба признака и по-прежнему отсеивается.
	score, _ := Score(feats(withNewsletter))

	assert.Equal(t, domain.LevelIgnore, Level(score))
	assert.LessOrEqual(t, score, 5)
}

func TestScore_UnsubscribeAloneWithoutSignals_StaysIgnore(t *testing.T) {
	// Без единого положительного сигнала письмо всё ещё ниже порога.
	score, _ := Score(feats(withUnsubscribeOnly, withUnknown))

	assert.Equal(t, domain.LevelIgnore, Level(score))
}
