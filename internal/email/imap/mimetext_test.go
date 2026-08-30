package imap

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// multipartMessage is the shape most real mail arrives in: an alternative
// container holding the same letter as plain text and as HTML, both
// quoted-printable encoded.
const multipartMessage = "From: Billing <billing@example.com>\r\n" +
	"Subject: Payment receipt\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/alternative; boundary=\"b1_boundary\"\r\n" +
	"\r\n" +
	"--b1_boundary\r\n" +
	"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n" +
	"\r\n" +
	"Your card was charged =E2=82=AC12.00.\r\n" +
	"\r\n" +
	"Thanks for your business.\r\n" +
	"--b1_boundary\r\n" +
	"Content-Type: text/html; charset=\"UTF-8\"\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n" +
	"\r\n" +
	"<html><body><p>Your card was charged =E2=82=AC12.00.</p></body></html>\r\n" +
	"--b1_boundary--\r\n"

func TestExtractText_MultipartPrefersPlainText(t *testing.T) {
	out := extractText([]byte(multipartMessage))

	assert.Contains(t, out, "Your card was charged €12.00.")
	assert.Contains(t, out, "Thanks for your business.")

	// None of the MIME plumbing may reach the reader.
	assert.NotContains(t, out, "b1_boundary")
	assert.NotContains(t, out, "Content-Type")
	assert.NotContains(t, out, "Content-Transfer-Encoding")
	assert.NotContains(t, out, "quoted-printable")
	assert.NotContains(t, out, "=E2=82=AC")
	assert.NotContains(t, out, "<p>")
	assert.NotContains(t, out, "<html>")
}

func TestExtractText_HTMLOnlyIsStripped(t *testing.T) {
	msg := "Subject: s\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=\"UTF-8\"\r\n" +
		"\r\n" +
		"<html><head><style>p{color:red}</style></head>" +
		"<body><p>Invoice <b>INV-42</b> is due.</p></body></html>\r\n"

	out := extractText([]byte(msg))

	assert.Contains(t, out, "Invoice")
	assert.Contains(t, out, "INV-42")
	assert.NotContains(t, out, "<b>")
	assert.NotContains(t, out, "color:red")
}

func TestExtractText_Base64Part(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("Счёт на оплату за август."))
	// Wrap at 20 chars, the way real senders wrap base64 payloads.
	var wrapped strings.Builder
	for i := 0; i < len(payload); i += 20 {
		end := min(i+20, len(payload))
		wrapped.WriteString(payload[i:end] + "\r\n")
	}
	msg := "Subject: s\r\n" +
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" + wrapped.String()

	out := extractText([]byte(msg))

	assert.Equal(t, "Счёт на оплату за август.", out)
}

func TestExtractText_SkipsAttachments(t *testing.T) {
	msg := "Subject: s\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"m1\"\r\n" +
		"\r\n" +
		"--m1\r\n" +
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
		"\r\n" +
		"See the attached invoice.\r\n" +
		"--m1\r\n" +
		"Content-Type: text/csv; charset=\"UTF-8\"\r\n" +
		"Content-Disposition: attachment; filename=\"invoice.csv\"\r\n" +
		"\r\n" +
		"id,amount\r\n1,12.00\r\n" +
		"--m1--\r\n"

	out := extractText([]byte(msg))

	assert.Contains(t, out, "See the attached invoice.")
	assert.NotContains(t, out, "id,amount")
}

func TestExtractText_KeepsParagraphBreaks(t *testing.T) {
	msg := "Subject: s\r\n" +
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
		"\r\n" +
		"First paragraph.\r\n\r\n\r\n\r\nSecond    paragraph.\r\n"

	out := extractText([]byte(msg))

	// Runs of blank lines collapse to one, but the paragraph break survives: a
	// letter flattened onto a single line is unreadable.
	assert.Equal(t, "First paragraph.\n\nSecond paragraph.", out)
}

func TestExtractText_NonMIMEFallsBackToStripping(t *testing.T) {
	// Not a parseable message at all — the old behaviour is the floor.
	out := extractText([]byte("<p>bare fragment</p>"))
	assert.Contains(t, out, "bare fragment")
	assert.NotContains(t, out, "<p>")
}

func TestExtractText_Latin1Charset(t *testing.T) {
	msg := "Subject: s\r\n" +
		"Content-Type: text/plain; charset=\"iso-8859-1\"\r\n" +
		"\r\n" +
		"Facture pay\xe9e\r\n"

	out := extractText([]byte(msg))

	require.NotEmpty(t, out)
	assert.Equal(t, "Facture payée", out)
}
