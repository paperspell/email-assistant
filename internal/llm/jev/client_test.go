package jev

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/llm"
)

// reply is one canned response of the stub API.
type reply struct {
	status int
	body   string
	header map[string]string
}

// stub serves the replies in order, repeating the last one, and records every
// request body it receives.
type stub struct {
	mu      sync.Mutex
	replies []reply
	bodies  [][]byte
	headers []http.Header
}

func (s *stub) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

// newStub starts the stub and a client pointed at it whose sleeps are recorded
// instead of waited.
func newStub(t *testing.T, replies ...reply) (*Client, *stub, *[]time.Duration) {
	t.Helper()
	s := &stub{replies: replies}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		n := len(s.bodies)
		s.bodies = append(s.bodies, body)
		s.headers = append(s.headers, r.Header.Clone())
		rep := s.replies[min(n, len(s.replies)-1)]
		s.mu.Unlock()
		assert.Equal(t, "/v1/systemone", r.URL.Path)
		for k, v := range rep.header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(rep.status)
		_, _ = io.WriteString(w, rep.body)
	}))
	t.Cleanup(srv.Close)

	var slept []time.Duration
	c := NewWithBaseURL("test-key", "jev-latest", srv.URL+"/v1")
	c.sleep = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		return ctx.Err()
	}
	return c, s, &slept
}

var sampleQuestions = map[string]Question{
	"urgent": Noul("Does it convey urgency?", "", ""),
	"mood": Choice("How does the writer feel?",
		Option{"calm", "Calm"}, Option{"angry", "Angry"}),
}

const sampleAnswers = `{
  "model": "jev-1.13.0",
  "answers": {
    "urgent": {"type": "noul", "noul": 0.93},
    "mood": {"type": "choice", "choice": "angry",
             "probabilities": {"calm": 0.1, "angry": 0.9}, "confidence": 0.8}
  },
  "usage": {"input_tokens": 120, "output_tokens": 9}
}`

func TestEvaluate_SendsTheDocumentedRequest(t *testing.T) {
	c, s, _ := newStub(t, reply{status: http.StatusOK, body: sampleAnswers})

	_, err := c.Evaluate(context.Background(), map[string]string{"text": "hi"}, sampleQuestions)
	require.NoError(t, err)

	require.Equal(t, 1, s.calls())
	assert.Equal(t, "Bearer test-key", s.headers[0].Get("Authorization"))
	assert.Equal(t, "application/json", s.headers[0].Get("Content-Type"))

	var got map[string]any
	require.NoError(t, json.Unmarshal(s.bodies[0], &got))
	assert.Equal(t, "jev-latest", got["model"])
	assert.Equal(t, map[string]any{"text": "hi"}, got["state"])
	questions := got["questions"].(map[string]any)
	assert.Equal(t, map[string]any{"type": "noul", "instructions": "Does it convey urgency?"}, questions["urgent"],
		"a noul without outcomes sends no criteria")
	assert.Equal(t, "choice", questions["mood"].(map[string]any)["type"])
}

func TestChoice_KeepsOptionOrder(t *testing.T) {
	// A Go map would marshal sorted by key; the order is part of what the
	// model reads.
	q := Choice("?", Option{"zeta", "last letter"}, Option{"alpha", "first letter"})

	b, err := json.Marshal(q)

	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"choice","instructions":"?","criteria":{"zeta":"last letter","alpha":"first letter"}}`,
		string(b))
	assert.Less(t, strings.Index(string(b), "zeta"), strings.Index(string(b), "alpha"))
}

func TestNoul_WithOutcomesSendsCriteria(t *testing.T) {
	b, err := json.Marshal(Noul("?", "it is", "it is not"))

	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"noul","instructions":"?","criteria":{"true":"it is","false":"it is not"}}`, string(b))
}

func TestEvaluate_ParsesAnswers(t *testing.T) {
	c, _, _ := newStub(t, reply{status: http.StatusOK, body: sampleAnswers})

	got, err := c.Evaluate(context.Background(), "state", sampleQuestions)

	require.NoError(t, err)
	assert.Equal(t, "jev-1.13.0", got.Model)
	assert.InDelta(t, 0.93, *got.Answers["urgent"].Noul, 1e-9)
	assert.Equal(t, "angry", got.Answers["mood"].Choice)
	assert.InDelta(t, 0.9, got.Answers["mood"].Probabilities["angry"], 1e-9)
	assert.InDelta(t, 0.8, got.Answers["mood"].Confidence, 1e-9)
	assert.Equal(t, 120, got.Usage.InputTokens)
}

func TestEvaluate_RejectsAnAnswerThatDoesNotFitItsQuestion(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"missing answer", `{"answers":{"urgent":{"type":"noul","noul":0.5}}}`, `no answer to "mood"`},
		{"wrong type", `{"answers":{"urgent":{"type":"choice","choice":"calm"},
			"mood":{"type":"choice","choice":"calm"}}}`, `answered as "choice"`},
		{"noul without probability", `{"answers":{"urgent":{"type":"noul"},
			"mood":{"type":"choice","choice":"calm"}}}`, "has no probability"},
		{"unknown option", `{"answers":{"urgent":{"type":"noul","noul":0.5},
			"mood":{"type":"choice","choice":"sad"}}}`, `chose "sad"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, _ := newStub(t, reply{status: http.StatusOK, body: tt.body})

			_, err := c.Evaluate(context.Background(), "state", sampleQuestions)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestEvaluate_PaymentRequiredIsOutOfCredits(t *testing.T) {
	c, s, _ := newStub(t, reply{status: http.StatusPaymentRequired, body: `{"detail":"balance exhausted"}`})

	_, err := c.Evaluate(context.Background(), "state", sampleQuestions)

	require.ErrorIs(t, err, llm.ErrOutOfCredits)
	assert.Contains(t, err.Error(), "balance exhausted")
	assert.Equal(t, 1, s.calls(), "an empty balance does not refill on retry")
}

func TestEvaluate_ClientErrorsAreNotRetried(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity} {
		c, s, slept := newStub(t, reply{status: status, body: `{"detail":"nope"}`})

		_, err := c.Evaluate(context.Background(), "state", sampleQuestions)

		require.Error(t, err)
		assert.NotErrorIs(t, err, llm.ErrOutOfCredits)
		assert.Contains(t, err.Error(), "nope")
		assert.Equal(t, 1, s.calls(), "status %d", status)
		assert.Empty(t, *slept)
	}
}

func TestEvaluate_RetriesOverloadWithBackoff(t *testing.T) {
	c, s, slept := newStub(t,
		reply{status: statusOverloaded},
		reply{status: http.StatusInternalServerError},
		reply{status: http.StatusOK, body: sampleAnswers},
	)

	_, err := c.Evaluate(context.Background(), "state", sampleQuestions)

	require.NoError(t, err)
	assert.Equal(t, 3, s.calls())
	assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second}, *slept)
}

func TestEvaluate_GivesUpAfterTwoRetries(t *testing.T) {
	c, s, _ := newStub(t, reply{status: http.StatusTooManyRequests, body: "slow down"})

	_, err := c.Evaluate(context.Background(), "state", sampleQuestions)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "http 429: slow down")
	assert.Equal(t, 1+maxRetries, s.calls())
}

func TestEvaluate_HonoursTheDelayTheServerAsksFor(t *testing.T) {
	c, _, slept := newStub(t,
		reply{status: http.StatusTooManyRequests, header: map[string]string{"retry-after-ms": "1500"}},
		reply{status: http.StatusTooManyRequests, header: map[string]string{"Retry-After": "2"}},
		reply{status: http.StatusOK, body: sampleAnswers},
	)

	_, err := c.Evaluate(context.Background(), "state", sampleQuestions)

	require.NoError(t, err)
	assert.Equal(t, []time.Duration{1500 * time.Millisecond, 2 * time.Second}, *slept)
}

func TestEvaluate_DoesNotWaitOutALongOutage(t *testing.T) {
	// The scheduler's fallback to the rule verdict serves the owner better
	// than a poll cycle stalled on one message.
	c, s, slept := newStub(t,
		reply{status: statusOverloaded, header: map[string]string{"Retry-After": "120"}},
		reply{status: http.StatusOK, body: sampleAnswers},
	)

	_, err := c.Evaluate(context.Background(), "state", sampleQuestions)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "http 529")
	assert.Equal(t, 1, s.calls())
	assert.Empty(t, *slept)
}

func TestEvaluate_StopsWhenTheCallerGivesUp(t *testing.T) {
	c, s, _ := newStub(t, reply{status: statusOverloaded})
	ctx, cancel := context.WithCancel(context.Background())
	c.sleep = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}

	_, err := c.Evaluate(ctx, "state", sampleQuestions)

	require.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "http 529", "the status that caused the wait is kept")
	assert.Equal(t, 1, s.calls())
}

func TestEvaluate_RetriesWhenNoResponseArrives(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			// Drop the connection without answering.
			hj, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, err := hj.Hijack()
			require.NoError(t, err)
			_ = conn.Close()
			return
		}
		_, _ = io.WriteString(w, sampleAnswers)
	}))
	t.Cleanup(srv.Close)
	c := NewWithBaseURL("k", "", srv.URL)
	c.sleep = func(context.Context, time.Duration) error { return nil }

	_, err := c.Evaluate(context.Background(), "state", sampleQuestions)

	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestNew_DefaultsToTheCurrentModel(t *testing.T) {
	assert.Equal(t, "jev-latest", New("k", "").model)
	assert.Equal(t, "jev-pinned", New("k", "jev-pinned").model)
}

func TestServerDelay_HTTPDate(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat))

	d, ok := serverDelay(h)

	assert.True(t, ok)
	assert.Zero(t, d, "a date in the past means retry now")
}
