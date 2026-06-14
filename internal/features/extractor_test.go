package features

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/paperspell/email-assistant/internal/email"
)

func msg(subject, from, inReplyTo, listUnsub, precedence string) email.Message {
	return email.Message{
		UID:             1,
		Subject:         subject,
		FromEmail:       from,
		Date:            time.Now(),
		InReplyTo:       inReplyTo,
		ListUnsubscribe: listUnsub,
		Precedence:      precedence,
	}
}

func TestExtract_Domain(t *testing.T) {
	f := Extract(msg("hi", "user@example.com", "", "", ""), 0, 0, 0)
	assert.Equal(t, "example.com", f.FromDomain)
}

func TestExtract_IsReply(t *testing.T) {
	f := Extract(msg("re: hi", "a@b.com", "<msg123@b.com>", "", ""), 0, 0, 0)
	assert.True(t, f.IsReply)
}

func TestExtract_Newsletter(t *testing.T) {
	f := Extract(msg("Sale!", "news@shop.com", "", "<unsub@shop.com>", "bulk"), 0, 0, 0)
	assert.True(t, f.HasListUnsubscribe)
	assert.True(t, f.IsBulkPrecedence)
}

func TestExtract_UrgentKeyword_EN(t *testing.T) {
	f := Extract(msg("URGENT: action required", "a@b.com", "", "", ""), 0, 0, 0)
	assert.True(t, f.HasUrgentKeyword)
}

func TestExtract_UrgentKeyword_PL(t *testing.T) {
	f := Extract(msg("Pilny: odpowiedz", "a@b.com", "", "", ""), 0, 0, 0)
	assert.True(t, f.HasUrgentKeyword)
}

func TestExtract_UrgentKeyword_RU(t *testing.T) {
	f := Extract(msg("Срочно: подтвердите", "a@b.com", "", "", ""), 0, 0, 0)
	assert.True(t, f.HasUrgentKeyword)
}

func TestExtract_MeetingKeyword_EN(t *testing.T) {
	f := Extract(msg("Meeting tomorrow at 10am", "a@b.com", "", "", ""), 0, 0, 0)
	assert.True(t, f.HasMeetingKeyword)
}

func TestExtract_MeetingKeyword_PL(t *testing.T) {
	f := Extract(msg("Spotkanie WOPFU", "teacher@school.pl", "", "", ""), 0, 0, 0)
	assert.True(t, f.HasMeetingKeyword)
}

func TestExtract_InvoiceKeyword_EN(t *testing.T) {
	f := Extract(msg("Invoice #1234 payment due", "billing@company.com", "", "", ""), 0, 0, 0)
	assert.True(t, f.HasInvoiceKeyword)
}

func TestExtract_InvoiceKeyword_PL(t *testing.T) {
	f := Extract(msg("Faktura nr 5678", "a@b.com", "", "", ""), 0, 0, 0)
	assert.True(t, f.HasInvoiceKeyword)
}

func TestExtract_SecurityKeyword(t *testing.T) {
	f := Extract(msg("Verify your password", "security@bank.com", "", "", ""), 0, 0, 0)
	assert.True(t, f.HasSecurityKeyword)
}

func TestExtract_GovernmentDomain(t *testing.T) {
	f := Extract(msg("Notice", "noreply@tax.gov", "", "", ""), 0, 0, 0)
	assert.True(t, f.HasGovernmentKeyword)
}

func TestExtract_SchoolDomain(t *testing.T) {
	f := Extract(msg("Notice", "admin@mit.edu", "", "", ""), 0, 0, 0)
	assert.True(t, f.HasSchoolKeyword)
}

func TestExtract_NoKeywords(t *testing.T) {
	f := Extract(msg("Hello there", "friend@gmail.com", "", "", ""), 0, 0, 0)
	assert.False(t, f.HasUrgentKeyword)
	assert.False(t, f.HasMeetingKeyword)
	assert.False(t, f.HasInvoiceKeyword)
	assert.False(t, f.IsReply)
	assert.False(t, f.HasListUnsubscribe)
}

func TestExtract_SenderHistory(t *testing.T) {
	f := Extract(msg("hi", "a@b.com", "", "", ""), 30, 20, 5)
	assert.Equal(t, 30, f.SenderScore)
	assert.Equal(t, 20, f.DomainScore)
	assert.Equal(t, 5, f.SenderSeenCount)
}
