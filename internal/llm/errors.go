package llm

import "errors"

// ErrOutOfCredits is returned, wrapped, when the provider refuses a request
// because the account's balance is exhausted — Gemini's prepaid credits
// answering HTTP 402, for example. It is a condition the owner must act on
// rather than a transient failure, so the scheduler reports it to Telegram
// instead of only logging it.
var ErrOutOfCredits = errors.New("llm provider: out of credits")
