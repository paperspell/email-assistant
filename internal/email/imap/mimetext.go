package imap

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"

	"golang.org/x/net/html/charset"
)

// maxMIMEDepth bounds recursion into nested multipart containers. Real mail
// nests two or three levels (mixed > alternative > related); anything deeper is
// malformed or hostile, and the parts beyond it are not worth a stack frame.
const maxMIMEDepth = 8

// extractText renders the readable text of a raw RFC 5322 message.
//
// The message body arrives as raw MIME: part boundaries, per-part headers and
// base64/quoted-printable payloads. Handing that to the user (or to the model)
// shows boundary markers and Content-Type lines as if they were the letter, so
// the structure is parsed here and only the text parts are kept. text/plain is
// preferred over text/html in a multipart/alternative, since the plain part is
// what the sender wrote for reading.
//
// Anything that cannot be parsed falls back to stripping tags from the raw
// input: a rough body beats an empty one.
func extractText(raw []byte) string {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return normalizeText(stripHTML(string(raw)))
	}
	text, err := partText(mailHeader(msg.Header), msg.Body, 0)
	if err != nil || strings.TrimSpace(text) == "" {
		body, rerr := io.ReadAll(msg.Body)
		if rerr != nil || len(bytes.TrimSpace(body)) == 0 {
			return normalizeText(text)
		}
		return normalizeText(stripHTML(string(body)))
	}
	return normalizeText(text)
}

// header is the subset of the MIME headers the walk needs, so top-level
// mail.Header and per-part textproto.MIMEHeader can share one code path.
type header interface{ Get(string) string }

type mailHeader mail.Header

func (h mailHeader) Get(key string) string { return mail.Header(h).Get(key) }

// partText walks one MIME part, recursing into multipart containers.
func partText(h header, body io.Reader, depth int) (string, error) {
	if depth > maxMIMEDepth {
		return "", nil
	}

	mediaType, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil || mediaType == "" {
		// A missing or malformed Content-Type means text/plain per RFC 2045.
		mediaType, params = "text/plain", map[string]string{}
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", nil
		}
		return multipartText(mediaType, boundary, body, depth)
	}

	// Attachments are not the letter, even when they are text.
	if disp, _, derr := mime.ParseMediaType(h.Get("Content-Disposition")); derr == nil && disp == "attachment" {
		return "", nil
	}
	if !strings.HasPrefix(mediaType, "text/") {
		return "", nil
	}

	decoded, err := decodeBody(body, h.Get("Content-Transfer-Encoding"), params["charset"])
	if err != nil {
		return "", err
	}
	if mediaType == "text/html" {
		return stripHTML(decoded), nil
	}
	return decoded, nil
}

// multipartText collects the text of a multipart container. For
// multipart/alternative the parts are the same content in different formats, so
// only the best one is kept; for mixed/related they are distinct pieces and all
// text is joined.
func multipartText(mediaType, boundary string, body io.Reader, depth int) (string, error) {
	mr := multipart.NewReader(body, boundary)
	alternative := mediaType == "multipart/alternative"

	var plain, html string
	var joined []string
	for {
		part, err := mr.NextPart()
		if err != nil {
			break // io.EOF, or a truncated body: keep what was parsed
		}
		text, perr := partText(part.Header, part, depth+1)
		_ = part.Close() //nolint:errcheck // the reader is drained by partText
		if perr != nil || strings.TrimSpace(text) == "" {
			continue
		}
		if !alternative {
			joined = append(joined, text)
			continue
		}
		// An unparseable part header is treated as the plain alternative, which
		// is the safer of the two to show.
		partType, _, ptErr := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if ptErr == nil && partType == "text/html" {
			html = text
		} else {
			plain = text
		}
	}

	if !alternative {
		return strings.Join(joined, "\n\n"), nil
	}
	if strings.TrimSpace(plain) != "" {
		return plain, nil
	}
	return html, nil
}

// decodeBody undoes the transfer encoding and converts the payload to UTF-8.
func decodeBody(body io.Reader, transferEncoding, charsetLabel string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(transferEncoding)) {
	case "base64":
		body = base64.NewDecoder(base64.StdEncoding, newlineStripper{body})
	case "quoted-printable":
		body = quotedprintable.NewReader(body)
	}

	// charset.NewReaderLabel falls back to the label in the payload (HTML meta)
	// and then to UTF-8, so an unknown or absent charset still yields text.
	r, err := charset.NewReaderLabel(labelOrUTF8(charsetLabel), body)
	if err != nil {
		r = body
	}

	decoded, err := io.ReadAll(r)
	if err != nil {
		// Truncated or undecodable payloads still carry usable text up to the
		// failure; a partial body beats none.
		return string(decoded), nil
	}
	return string(decoded), nil
}

func labelOrUTF8(label string) string {
	if strings.TrimSpace(label) == "" {
		return "utf-8"
	}
	return label
}

// newlineStripper drops the line breaks base64 payloads are wrapped at, which
// the strict decoder would otherwise reject.
type newlineStripper struct{ r io.Reader }

// unchanged, or callers stop seeing io.EOF.
//
//nolint:wrapcheck // an io.Reader adapter must pass the underlying error through
func (s newlineStripper) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		filtered := p[:0]
		for _, b := range p[:n] {
			if b != '\r' && b != '\n' {
				filtered = append(filtered, b)
			}
		}
		n = len(filtered)
	}
	return n, err
}

var (
	horizontalWS = regexp.MustCompile(`[ \t\x{00a0}]+`)
	blankLines   = regexp.MustCompile(`\n{3,}`)
)

// normalizeText tidies extracted text for display: it collapses runs of spaces
// and trims each line, but keeps paragraph breaks — a letter flattened onto one
// line is technically clean and practically unreadable.
func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = horizontalWS.ReplaceAllString(s, " ")

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	s = strings.Join(lines, "\n")
	s = blankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
