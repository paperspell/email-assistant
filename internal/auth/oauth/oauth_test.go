package oauth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestXOAUTH2_InitialResponseFormat(t *testing.T) {
	mech, ir, err := NewXOAUTH2("user@example.com", "tok123").Start()
	require.NoError(t, err)
	assert.Equal(t, "XOAUTH2", mech)
	assert.Equal(t, "user=user@example.com\x01auth=Bearer tok123\x01\x01", string(ir))
}

func TestXOAUTH2_ChallengeAborts(t *testing.T) {
	_, err := NewXOAUTH2("u", "t").Next([]byte("eyJzdGF0dXMi"))
	require.Error(t, err)
}

// staticSource returns a fixed token, simulating an oauth2 base source.
type staticSource struct{ tok *oauth2.Token }

func (s staticSource) Token() (*oauth2.Token, error) { return s.tok, nil }

func TestTokenSource_PersistsOnChange(t *testing.T) {
	var persisted []Tokens
	persist := func(tk Tokens) error {
		persisted = append(persisted, tk)
		return nil
	}

	// The base source returns a different (refreshed) access token than the seed.
	refreshed := &oauth2.Token{
		AccessToken:  "new-access",
		RefreshToken: "refresh-1",
		Expiry:       time.Now().Add(time.Hour),
	}
	ps := &persistingSource{
		base:    staticSource{tok: refreshed},
		last:    Tokens{AccessToken: "old-access", RefreshToken: "refresh-1"},
		persist: persist,
	}

	got, err := ps.Token()
	require.NoError(t, err)
	assert.Equal(t, "new-access", got.AccessToken)
	require.Len(t, persisted, 1, "a changed access token must be persisted")
	assert.Equal(t, "new-access", persisted[0].AccessToken)
	assert.Equal(t, "refresh-1", persisted[0].RefreshToken)
}

func TestTokenSource_NoPersistWhenUnchanged(t *testing.T) {
	var calls int
	persist := func(Tokens) error { calls++; return nil }

	same := &oauth2.Token{AccessToken: "same-access", RefreshToken: "refresh-1"}
	ps := &persistingSource{
		base:    staticSource{tok: same},
		last:    Tokens{AccessToken: "same-access", RefreshToken: "refresh-1"},
		persist: persist,
	}

	_, err := ps.Token()
	require.NoError(t, err)
	assert.Zero(t, calls, "an unchanged token must not be persisted")
}

func TestTokenSource_KeepsRefreshTokenWhenRefreshOmitsIt(t *testing.T) {
	var persisted []Tokens
	ps := &persistingSource{
		base:    staticSource{tok: &oauth2.Token{AccessToken: "new-access"}}, // no refresh token
		last:    Tokens{AccessToken: "old-access", RefreshToken: "keep-me"},
		persist: func(tk Tokens) error { persisted = append(persisted, tk); return nil },
	}
	_, err := ps.Token()
	require.NoError(t, err)
	require.Len(t, persisted, 1)
	assert.Equal(t, "keep-me", persisted[0].RefreshToken, "refresh token is carried forward")
}

func TestGoogleConfig(t *testing.T) {
	cfg := GoogleConfig("id", "secret")
	assert.Equal(t, "id", cfg.ClientID)
	assert.Equal(t, "secret", cfg.ClientSecret)
	assert.Equal(t, []string{gmailScope}, cfg.Scopes)
	assert.Equal(t, "https://oauth2.googleapis.com/token", cfg.Endpoint.TokenURL)
}

func TestIsReauthRequired(t *testing.T) {
	assert.True(t, IsReauthRequired(&oauth2.RetrieveError{ErrorCode: "invalid_grant"}))
	assert.False(t, IsReauthRequired(&oauth2.RetrieveError{ErrorCode: "temporarily_unavailable"}))
	assert.False(t, IsReauthRequired(assert.AnError))
	assert.False(t, IsReauthRequired(nil))
}

// Ensure persistingSource satisfies oauth2.TokenSource at compile time.
var _ oauth2.TokenSource = (*persistingSource)(nil)
