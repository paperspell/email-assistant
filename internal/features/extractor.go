package features

import (
	"strings"

	"github.com/paperspell/email-assistant/internal/email"
)

// Extract derives EmailFeatures from a message and its known sender/domain history.
func Extract(msg email.Message, senderScore, domainScore, senderSeenCount int) EmailFeatures {
	domain := extractDomain(msg.FromEmail)
	subject := msg.Subject

	f := EmailFeatures{
		FromEmail:  msg.FromEmail,
		FromDomain: domain,
		Language:   DetectLanguage(subject),

		IsReply:            msg.InReplyTo != "",
		HasListUnsubscribe: msg.ListUnsubscribe != "",
		IsBulkPrecedence:   isBulkPrecedence(msg.Precedence),

		HasUrgentKeyword:     containsAny(subject, keywordsUrgent),
		HasMeetingKeyword:    containsAny(subject, keywordsMeeting),
		HasInvoiceKeyword:    containsAny(subject, keywordsInvoice),
		HasSecurityKeyword:   containsAny(subject, keywordsSecurity),
		HasDeadlineKeyword:   containsAny(subject, keywordsDeadline),
		HasInterviewKeyword:  containsAny(subject, keywordsInterview),
		HasGovernmentKeyword: containsAny(subject, keywordsGovernment) || isGovDomain(domain),
		HasSchoolKeyword:     containsAny(subject, keywordsSchool) || isEduDomain(domain),

		SenderScore:     senderScore,
		DomainScore:     domainScore,
		SenderSeenCount: senderSeenCount,
	}

	return f
}

// ExtractDomain returns the domain part of an email address.
func ExtractDomain(emailAddr string) string {
	return extractDomain(emailAddr)
}

func extractDomain(emailAddr string) string {
	parts := strings.SplitN(emailAddr, "@", 2)
	if len(parts) == 2 {
		return strings.ToLower(parts[1])
	}
	return ""
}

func isBulkPrecedence(precedence string) bool {
	p := strings.ToLower(strings.TrimSpace(precedence))
	return p == "bulk" || p == "list" || p == "junk"
}

func isGovDomain(domain string) bool {
	return strings.HasSuffix(domain, ".gov") ||
		strings.HasSuffix(domain, ".gov.pl") ||
		strings.HasSuffix(domain, ".gov.ua") ||
		strings.HasSuffix(domain, ".gouv.fr") ||
		strings.HasSuffix(domain, ".gob.es") ||
		strings.HasSuffix(domain, ".gov.by") ||
		strings.HasSuffix(domain, ".gov.kz")
}

func isEduDomain(domain string) bool {
	return strings.HasSuffix(domain, ".edu") ||
		strings.HasSuffix(domain, ".edu.pl") ||
		strings.HasSuffix(domain, ".edu.ua") ||
		strings.HasSuffix(domain, ".ac.uk") ||
		strings.HasSuffix(domain, ".ac.be")
}

func toLower(s string) string {
	return strings.ToLower(s)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
