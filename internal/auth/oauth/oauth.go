// Package oauth implements the Google OAuth flow used to authenticate Gmail
// accounts over IMAP with XOAUTH2: a loopback consent flow to obtain tokens, and
// a refreshing, self-persisting token source for the daemon.
package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"
)

// gmailScope grants full IMAP/SMTP access, required for IMAP login.
const gmailScope = "https://mail.google.com/"

// googleEndpoint is Google's OAuth 2.0 endpoint, declared inline to avoid the
// heavy cloud.google.com dependency pulled in by golang.org/x/oauth2/google.
var googleEndpoint = oauth2.Endpoint{
	AuthURL:   "https://accounts.google.com/o/oauth2/auth",
	TokenURL:  "https://oauth2.googleapis.com/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

// consentTimeout bounds how long Consent waits for the browser round-trip.
const consentTimeout = 5 * time.Minute

// Tokens is the durable OAuth state stored per account.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// GoogleConfig builds the oauth2 config for Gmail IMAP access.
func GoogleConfig(clientID, clientSecret string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     googleEndpoint,
		Scopes:       []string{gmailScope},
	}
}

// Consent runs the loopback authorization-code flow: it starts a localhost
// listener, opens the browser to Google's consent screen, captures the returned
// code, and exchanges it for tokens. access_type=offline + prompt=consent ensure
// a refresh token is always returned.
func Consent(ctx context.Context, cfg *oauth2.Config) (Tokens, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Tokens{}, fmt.Errorf("oauth: start callback listener: %w", err)
	}
	defer ln.Close() //nolint:errcheck

	// Desktop-app clients may use any loopback redirect URI without console setup.
	local := *cfg
	local.RedirectURL = fmt.Sprintf("http://%s/", ln.Addr().String())

	state, err := randomState()
	if err != nil {
		return Tokens{}, err
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{ //nolint:gosec // local loopback, short-lived
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("state") != state {
				http.Error(w, "state mismatch", http.StatusBadRequest)
				errCh <- fmt.Errorf("oauth: state mismatch on callback")
				return
			}
			if e := q.Get("error"); e != "" {
				http.Error(w, "authorization denied", http.StatusBadRequest)
				errCh <- fmt.Errorf("oauth: authorization denied: %s", e)
				return
			}
			fmt.Fprintln(w, "Authorization complete. You can close this tab and return to the terminal.") //nolint:errcheck
			codeCh <- q.Get("code")
		}),
	}
	go srv.Serve(ln)  //nolint:errcheck
	defer srv.Close() //nolint:errcheck

	authURL := local.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
	fmt.Println("Opening your browser to authorize access to Gmail...")
	fmt.Printf("If it does not open, visit this URL:\n\n  %s\n\n", authURL)
	if err := openBrowser(authURL); err != nil {
		fmt.Println("(could not open the browser automatically — open the URL above manually)")
	}

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return Tokens{}, err
	case <-ctx.Done():
		return Tokens{}, fmt.Errorf("oauth: consent cancelled: %w", ctx.Err())
	case <-time.After(consentTimeout):
		return Tokens{}, fmt.Errorf("oauth: timed out waiting for authorization")
	}

	tok, err := local.Exchange(ctx, code)
	if err != nil {
		return Tokens{}, fmt.Errorf("oauth: exchange authorization code: %w", err)
	}
	if tok.RefreshToken == "" {
		return Tokens{}, fmt.Errorf("oauth: no refresh token returned; revoke the app's access at " +
			"https://myaccount.google.com/permissions and try again")
	}
	return Tokens{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
	}, nil
}

// TokenSource returns a token source seeded from stored tokens that refreshes the
// access token as needed and calls persist whenever the token changes, so a
// refreshed (or rotated) token is written back to storage.
func TokenSource(ctx context.Context, cfg *oauth2.Config, t Tokens, persist func(Tokens) error) oauth2.TokenSource {
	base := cfg.TokenSource(ctx, &oauth2.Token{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		Expiry:       t.Expiry,
	})
	return &persistingSource{base: base, last: t, persist: persist}
}

type persistingSource struct {
	base    oauth2.TokenSource
	last    Tokens
	persist func(Tokens) error
}

func (p *persistingSource) Token() (*oauth2.Token, error) {
	tok, err := p.base.Token()
	if err != nil {
		return nil, fmt.Errorf("oauth: refresh token: %w", err)
	}
	cur := Tokens{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, Expiry: tok.Expiry}
	// A refresh response may omit the refresh token; keep the existing one.
	if cur.RefreshToken == "" {
		cur.RefreshToken = p.last.RefreshToken
	}
	if cur.AccessToken != p.last.AccessToken || cur.RefreshToken != p.last.RefreshToken {
		// Best-effort: only advance last when persistence succeeds so a failed
		// write is retried on the next refresh.
		if perr := p.persist(cur); perr == nil {
			p.last = cur
		}
	}
	return tok, nil
}

// IsReauthRequired reports whether err indicates the refresh token is no longer
// valid (revoked or expired), meaning the user must re-authorize. This is the
// signal to stop retrying and prompt for re-consent rather than loop.
func IsReauthRequired(err error) bool {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		return re.ErrorCode == "invalid_grant"
	}
	return false
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oauth: generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	if err := exec.Command(cmd, args...).Start(); err != nil { //nolint:gosec // cmd is from a fixed set
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}
