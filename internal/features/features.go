package features

// EmailFeatures holds all signals extracted from an email message
// without fetching the email body.
type EmailFeatures struct {
	// Sender identity
	FromEmail  string
	FromDomain string

	// Thread signal
	IsReply bool // InReplyTo header is set

	// Newsletter / bulk signals
	HasListUnsubscribe bool
	IsBulkPrecedence   bool // Precedence: bulk or list

	// Subject keyword groups
	HasUrgentKeyword  bool
	HasMeetingKeyword bool
	HasInvoiceKeyword bool
	// HasMoneyKeyword marks mail about money actually moving. Unlike the other
	// keyword flags it matches whole words only: it drives a threshold bypass,
	// so "discharged" must not read as "charged".
	HasMoneyKeyword      bool
	HasSecurityKeyword   bool
	HasDeadlineKeyword   bool
	HasInterviewKeyword  bool
	HasGovernmentKeyword bool
	HasSchoolKeyword     bool

	// Detected language of the subject (ISO 639-1 or "und")
	Language string

	// Sender/domain history loaded from DB
	SenderScore     int
	DomainScore     int
	SenderSeenCount int
}
