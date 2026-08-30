package main

import (
	"bufio"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTelegramSetup is a scripted telegramSetup for testing detectTelegramChatID
// without a live bot. LatestChatID walks chatIDs one call at a time.
type fakeTelegramSetup struct {
	username       string
	chatIDs        []int64
	idx            int
	latestErr      error
	verifyErr      error
	verifiedChatID int64
}

func (f *fakeTelegramSetup) Username() string { return f.username }

func (f *fakeTelegramSetup) LatestChatID() (int64, error) {
	if f.latestErr != nil {
		return 0, f.latestErr
	}
	if f.idx >= len(f.chatIDs) {
		return 0, nil
	}
	id := f.chatIDs[f.idx]
	f.idx++
	return id, nil
}

func (f *fakeTelegramSetup) SendVerification(chatID int64) error {
	f.verifiedChatID = chatID
	return f.verifyErr
}

// withSeams overrides the package-level detectTelegramChatID seams for one test
// and restores them afterwards. The poll delay is zeroed so tests never sleep.
func withSeams(t *testing.T, client telegramSetup, factoryErr error) {
	t.Helper()
	origNew, origOpen := newTelegramSetup, openTelegramLink
	origDelay, origAttempts := chatIDPollDelay, chatIDPollAttempts
	t.Cleanup(func() {
		newTelegramSetup, openTelegramLink = origNew, origOpen
		chatIDPollDelay, chatIDPollAttempts = origDelay, origAttempts
	})

	newTelegramSetup = func(string) (telegramSetup, error) { return client, factoryErr }
	openTelegramLink = func(string) error { return nil }
	chatIDPollDelay = 0
	chatIDPollAttempts = 5
}

func scanner(input string) *bufio.Scanner { return bufio.NewScanner(strings.NewReader(input)) }

func TestDetectTelegramChatID_DetectsAfterPolling(t *testing.T) {
	fake := &fakeTelegramSetup{username: "test_bot", chatIDs: []int64{0, 0, 42}}
	withSeams(t, fake, nil)

	got := detectTelegramChatID("123:abc", scanner(""), "")
	assert.Equal(t, "42", got)
	assert.Equal(t, int64(42), fake.verifiedChatID, "the detected chat id should be verified")
}

func TestDetectTelegramChatID_VerificationFailureStillReturnsID(t *testing.T) {
	fake := &fakeTelegramSetup{username: "test_bot", chatIDs: []int64{7}, verifyErr: errors.New("chat not found")}
	withSeams(t, fake, nil)

	got := detectTelegramChatID("123:abc", scanner(""), "")
	assert.Equal(t, "7", got, "a failed test message must not lose the detected id")
}

func TestDetectTelegramChatID_InvalidTokenFallsBackToManual(t *testing.T) {
	withSeams(t, nil, errors.New("failed to check bot token"))

	// No message is ever detected because the client never builds; the manual
	// chat id typed at the prompt is used instead.
	got := detectTelegramChatID("bad", scanner("999\n"), "")
	assert.Equal(t, "999", got)
}

func TestDetectTelegramChatID_NoMessageFallsBackToManual(t *testing.T) {
	fake := &fakeTelegramSetup{username: "test_bot"} // LatestChatID always 0
	withSeams(t, fake, nil)

	got := detectTelegramChatID("123:abc", scanner("555\n"), "")
	assert.Equal(t, "555", got)
	assert.Zero(t, fake.verifiedChatID, "no verification is sent when nothing was detected")
}

func TestDetectTelegramChatID_ManualFallbackKeepsCurrentOnEmptyInput(t *testing.T) {
	fake := &fakeTelegramSetup{username: "test_bot"}
	withSeams(t, fake, nil)

	// Empty input at the manual prompt keeps the existing chat id.
	got := detectTelegramChatID("123:abc", scanner("\n"), "314")
	assert.Equal(t, "314", got)
}

// Guard against accidentally reintroducing a real sleep in the poll loop.
func TestDetectTelegramChatID_DoesNotSleepInTests(t *testing.T) {
	fake := &fakeTelegramSetup{username: "test_bot"}
	withSeams(t, fake, nil)

	start := time.Now()
	_ = detectTelegramChatID("123:abc", scanner("1\n"), "")
	require.Less(t, time.Since(start), 500*time.Millisecond)
}

func TestPromptModel_RePromptsOnBadNumber(t *testing.T) {
	// Опечатка в номере не должна ронять мастер: спрашиваем снова.
	sc := bufio.NewScanner(strings.NewReader("9\n2\n"))

	got, err := promptModel(sc, "anthropic", "")

	require.NoError(t, err)
	assert.Equal(t, "claude-opus-5", got)
}

func TestPromptModel_EmptyKeepsCurrent(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("\n"))

	got, err := promptModel(sc, "anthropic", "claude-haiku-4-5")

	require.NoError(t, err)
	assert.Equal(t, "claude-haiku-4-5", got)
}

func TestPromptModel_AcceptsUnlistedID(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("claude-future-9\n"))

	got, err := promptModel(sc, "anthropic", "")

	require.NoError(t, err)
	assert.Equal(t, "claude-future-9", got)
}

func TestPromptLanguage_NumberPicksLocale(t *testing.T) {
	// "11" is ru in the shipped order (en, be, de, es, fr, he, it, kk, pl, pt, ru, uk).
	sc := bufio.NewScanner(strings.NewReader("11\n"))
	assert.Equal(t, "ru", promptLanguage(sc, ""))
}

func TestPromptLanguage_AcceptsLocaleCode(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("pl\n"))
	assert.Equal(t, "pl", promptLanguage(sc, ""))
}

func TestPromptLanguage_MigratesLegacyName(t *testing.T) {
	// Existing installs hold a free-text name; it must survive as a locale code.
	sc := bufio.NewScanner(strings.NewReader("\n"))
	assert.Equal(t, "ru", promptLanguage(sc, "Russian"))
}

func TestPromptLanguage_UnknownFallsBackToEnglish(t *testing.T) {
	// Storing an unrenderable value would leave the bot with no catalog to use.
	sc := bufio.NewScanner(strings.NewReader("klingon\n"))
	assert.Equal(t, "en", promptLanguage(sc, ""))
}
