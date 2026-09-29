// Package jev classifies emails with Jev, TypeSafe's decision model.
//
// Jev does not generate text. It answers typed questions about a state — a
// choice among named options, or the probability of a yes ("noul") — with
// calibrated probabilities. That covers the classification half of the job
// and none of the summary half: a verdict from Jev carries a level, a category
// and a score, and never a summary.
//
// The API is called over plain HTTP, as with Gemini: TypeSafe ships SDKs for
// Python and JavaScript only, and the one endpoint used here is small.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/paperspell/email-assistant/internal/llm"
)

const (
	defaultBaseURL = "https://api.typesafe.ai/v1"
	// attemptTimeout bounds one HTTP attempt. Jev answers in well under a
	// second; the official SDKs allow 30 s for a whole call, retries included.
	attemptTimeout = 20 * time.Second
	// maxErrorBody bounds what is read from a failed response before it is put
	// into an error string.
	maxErrorBody = 2048

	// The retry policy follows the official SDKs: two retries after the first
	// attempt, backing off from 0.5 s and doubling up to 5 s. They also
	// subtract random jitter; one daemon polling a few mailboxes cannot cause
	// a thundering herd, so the delays here are deterministic.
	maxRetries  = 2
	baseBackoff = 500 * time.Millisecond
	maxBackoff  = 5 * time.Second
	// maxRetryWait is the longest server-requested delay worth waiting out.
	// A longer Retry-After means the service is down for a while, and the
	// scheduler's fallback to the rule verdict serves the owner better than a
	// poll cycle stalled on one message.
	maxRetryWait = 10 * time.Second

	// statusOverloaded is TypeSafe's answer while the service is temporarily
	// overloaded.
	statusOverloaded = 529
)

// Client calls TypeSafe's System One endpoint.
type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
	// sleep waits between retries; tests replace it to run without delays.
	sleep func(ctx context.Context, d time.Duration) error
}

// New creates a Client. model may be empty to use the default.
func New(apiKey, model string) *Client {
	return NewWithBaseURL(apiKey, model, defaultBaseURL)
}

// NewWithBaseURL creates a Client pointing at a custom base URL (used in tests).
func NewWithBaseURL(apiKey, model, baseURL string) *Client {
	if model == "" {
		model = llm.DefaultModel("jev")
	}
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: attemptTimeout},
		sleep:   sleepCtx,
	}
}

// Question types, as the API names them.
const (
	TypeNoul   = "noul"
	TypeChoice = "choice"
)

// Question is one typed question about the state. Build it with Noul or Choice.
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Option is one of the answers a Choice question can pick.
type Option struct {
	Key         string
	Description string
}

// options keeps a Choice's options in the order given. A Go map would be
// marshalled sorted by key, and the order is part of what the model reads —
// levels from most to least important, say.
type options []Option

// MarshalJSON writes the options as one JSON object, in order.
func (o options) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, opt := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(opt.Key)
		if err != nil {
			return nil, fmt.Errorf("marshal option key: %w", err)
		}
		desc, err := json.Marshal(opt.Description)
		if err != nil {
			return nil, fmt.Errorf("marshal option description: %w", err)
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(desc)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Noul asks for the probability that the answer is yes. yes and no describe
// the two outcomes; when both are empty the question stands on its own.
func Noul(instructions, yes, no string) Question {
	q := Question{Type: TypeNoul, Instructions: instructions}
	if yes != "" || no != "" {
		q.Criteria = map[string]string{"true": yes, "false": no}
	}
	return q
}

// Choice asks which of the options fits best. At most 255 are allowed.
func Choice(instructions string, opts ...Option) Question {
	return Question{Type: TypeChoice, Instructions: instructions, Criteria: options(opts)}
}

// Answer is the model's answer to one question. Noul is set for a noul
// question; Choice, Probabilities and Confidence for a choice.
type Answer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	// Confidence is how concentrated Probabilities is, from 0 to 1.
	Confidence float64 `json:"confidence,omitempty"`
}

// Usage is the token count TypeSafe bills for. Only input is charged.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Response is the answer to every question of one request, keyed by the ids
// the questions were given.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

type evaluateRequest struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}

// Evaluate asks every question about state in one request. The response is
// checked against the questions — each answered, with its own type, a choice
// naming one of its options — so callers can index Answers without guarding.
func (c *Client) Evaluate(ctx context.Context, state any, questions map[string]Question) (Response, error) {
	body, err := json.Marshal(evaluateRequest{Model: c.model, State: state, Questions: questions})
	if err != nil {
		return Response{}, fmt.Errorf("jev evaluate: marshal request: %w", err)
	}

	for attempt := 0; ; attempt++ {
		resp, err := c.post(ctx, body)
		if err != nil {
			// The attempt got no answer at all. Retry unless the caller has
			// given up, which the context says.
			if ctx.Err() != nil || attempt == maxRetries {
				return Response{}, fmt.Errorf("jev evaluate: %w", err)
			}
			if err := c.sleep(ctx, backoff(attempt)); err != nil {
				return Response{}, fmt.Errorf("jev evaluate: %w", err)
			}
			continue
		}
		if resp.StatusCode == http.StatusOK {
			out, err := decode(resp.Body, questions)
			resp.Body.Close() //nolint:errcheck
			return out, err
		}

		failure := statusError(resp)
		wait, retry := retryDelay(resp, attempt)
		resp.Body.Close() //nolint:errcheck
		if !retry {
			return Response{}, failure
		}
		if err := c.sleep(ctx, wait); err != nil {
			return Response{}, fmt.Errorf("%w; stopped waiting to retry: %w", failure, err)
		}
	}
}

func (c *Client) post(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/systemone", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post: %w", err)
	}
	return resp, nil
}

// statusError describes a failed response. 402 is wrapped as ErrOutOfCredits
// so the scheduler tells the owner: TypeSafe does not document the status for
// an exhausted balance, but 402 is what Gemini uses and what the scheduler's
// alert is built around, and mapping it costs nothing if it never comes.
func statusError(resp *http.Response) error {
	snippet, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil {
		// The explanation is lost but not the status code, which is the part
		// the operator cannot do without.
		snippet = nil
	}
	msg := fmt.Sprintf("jev evaluate: http %d", resp.StatusCode)
	if s := strings.TrimSpace(string(snippet)); s != "" {
		msg += ": " + s
	}
	if resp.StatusCode == http.StatusPaymentRequired {
		return fmt.Errorf("%s: %w", msg, llm.ErrOutOfCredits)
	}
	return errors.New(msg)
}

// retryDelay decides whether a failed response is worth another attempt, and
// after how long. Retried: request timeout, rate limit, overload and server
// errors — the statuses the official SDKs retry. A delay the server asks for
// is honoured when it is short enough to wait out.
func retryDelay(resp *http.Response, attempt int) (time.Duration, bool) {
	if attempt >= maxRetries {
		return 0, false
	}
	code := resp.StatusCode
	if code != http.StatusRequestTimeout && code != http.StatusTooManyRequests &&
		code != statusOverloaded && code < http.StatusInternalServerError {
		return 0, false
	}
	if wait, ok := serverDelay(resp.Header); ok {
		if wait > maxRetryWait {
			return 0, false
		}
		return wait, true
	}
	return backoff(attempt), true
}

// serverDelay reads the delay a response asks for: retry-after-ms first, as
// the more precise, then Retry-After in seconds or as an HTTP date.
func serverDelay(h http.Header) (time.Duration, bool) {
	if v := h.Get("retry-after-ms"); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil && ms >= 0 {
			return time.Duration(ms * float64(time.Millisecond)), true
		}
	}
	v := h.Get("Retry-After")
	if v == "" {
		return 0, false
	}
	if s, err := strconv.Atoi(v); err == nil && s >= 0 {
		return time.Duration(s) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0), true
	}
	return 0, false
}

func backoff(attempt int) time.Duration {
	return min(baseBackoff<<attempt, maxBackoff)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // the caller wraps it with context
	case <-t.C:
		return nil
	}
}

func decode(r io.Reader, questions map[string]Question) (Response, error) {
	var out Response
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		return Response{}, fmt.Errorf("jev evaluate: decode response: %w", err)
	}
	for id, q := range questions {
		a, ok := out.Answers[id]
		if !ok {
			return Response{}, fmt.Errorf("jev evaluate: no answer to %q", id)
		}
		if a.Type != q.Type {
			return Response{}, fmt.Errorf("jev evaluate: %q answered as %q, asked as %q", id, a.Type, q.Type)
		}
		switch q.Type {
		case TypeNoul:
			if a.Noul == nil {
				return Response{}, fmt.Errorf("jev evaluate: %q has no probability", id)
			}
		case TypeChoice:
			if !hasOption(q, a.Choice) {
				return Response{}, fmt.Errorf("jev evaluate: %q chose %q, not one of its options", id, a.Choice)
			}
		}
	}
	return out, nil
}

func hasOption(q Question, key string) bool {
	opts, ok := q.Criteria.(options)
	if !ok {
		return false
	}
	for _, o := range opts {
		if o.Key == key {
			return true
		}
	}
	return false
}
