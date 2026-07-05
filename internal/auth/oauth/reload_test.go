package oauth

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// fakeSource returns scripted results; one is built per rebuild.
type fakeSource struct {
	tok   *oauth2.Token
	err   error
	calls int
}

func (f *fakeSource) Token() (*oauth2.Token, error) {
	f.calls++
	return f.tok, f.err
}

func invalidGrant() error {
	return fmt.Errorf("oauth: refresh token: %w", &oauth2.RetrieveError{ErrorCode: "invalid_grant"})
}

// scripted builds fake sources from a queue so successive rebuilds get distinct
// scripted behaviour. It records how many times build was invoked.
func scripted(sources ...*fakeSource) (func(Tokens) oauth2.TokenSource, *int) {
	i := 0
	built := 0
	build := func(Tokens) oauth2.TokenSource {
		built++
		s := sources[i]
		i++
		return s
	}
	return build, &built
}

func TestReloading_SucceedsWithoutReload(t *testing.T) {
	want := &oauth2.Token{AccessToken: "at"}
	build, built := scripted(&fakeSource{tok: want})
	reloaded := 0
	rs := newReloadingSource(Tokens{RefreshToken: "r1"}, build, func() (Tokens, error) {
		reloaded++
		return Tokens{}, nil
	})

	got, err := rs.Token()
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, 1, *built, "no rebuild on success")
	assert.Equal(t, 0, reloaded, "reload not consulted on success")
}

func TestReloading_RebuildsWhenStoredTokenChanged(t *testing.T) {
	fresh := &oauth2.Token{AccessToken: "new"}
	build, built := scripted(
		&fakeSource{err: invalidGrant()}, // initial: cached token rejected
		&fakeSource{tok: fresh},          // rebuilt with the re-authorized token
	)
	rs := newReloadingSource(Tokens{RefreshToken: "old"}, build, func() (Tokens, error) {
		return Tokens{RefreshToken: "new-refresh"}, nil
	})

	got, err := rs.Token()
	require.NoError(t, err)
	assert.Equal(t, fresh, got)
	assert.Equal(t, 2, *built, "rebuilt once with the new token")
	assert.Equal(t, Tokens{RefreshToken: "new-refresh"}, rs.current)
}

func TestReloading_NoRebuildWhenStoredTokenUnchanged(t *testing.T) {
	build, built := scripted(&fakeSource{err: invalidGrant()})
	rs := newReloadingSource(Tokens{RefreshToken: "same"}, build, func() (Tokens, error) {
		return Tokens{RefreshToken: "same"}, nil // no re-auth happened yet
	})

	_, err := rs.Token()
	assert.True(t, IsReauthRequired(err), "original re-auth error surfaces")
	assert.Equal(t, 1, *built, "no rebuild when stored token is unchanged")
}

func TestReloading_ReloadErrorSurfacesOriginal(t *testing.T) {
	build, built := scripted(&fakeSource{err: invalidGrant()})
	rs := newReloadingSource(Tokens{RefreshToken: "old"}, build, func() (Tokens, error) {
		return Tokens{}, errors.New("db locked")
	})

	_, err := rs.Token()
	assert.True(t, IsReauthRequired(err), "original re-auth error surfaces, not the reload error")
	assert.Equal(t, 1, *built)
}

func TestReloading_RetriesOnlyOnce(t *testing.T) {
	build, built := scripted(
		&fakeSource{err: invalidGrant()}, // initial rejected
		&fakeSource{err: invalidGrant()}, // rebuilt, still rejected
	)
	rs := newReloadingSource(Tokens{RefreshToken: "old"}, build, func() (Tokens, error) {
		return Tokens{RefreshToken: "new"}, nil
	})

	_, err := rs.Token()
	assert.True(t, IsReauthRequired(err))
	assert.Equal(t, 2, *built, "rebuilt once, not looping")

	// current advanced to the stored token, so a subsequent call with the same
	// stored token does not rebuild again.
	build2, built2 := scripted(&fakeSource{err: invalidGrant()})
	rs.build = build2
	rs.inner = build2(rs.current)
	_, err = rs.Token()
	assert.True(t, IsReauthRequired(err))
	assert.Equal(t, 1, *built2, "no further rebuild when stored token matches current")
}

func TestReloading_NonReauthErrorNotRecovered(t *testing.T) {
	boom := errors.New("network unreachable")
	build, built := scripted(&fakeSource{err: boom})
	reloaded := 0
	rs := newReloadingSource(Tokens{RefreshToken: "r1"}, build, func() (Tokens, error) {
		reloaded++
		return Tokens{}, nil
	})

	_, err := rs.Token()
	assert.ErrorIs(t, err, boom)
	assert.Equal(t, 1, *built)
	assert.Equal(t, 0, reloaded, "reload not consulted for non-reauth errors")
}
