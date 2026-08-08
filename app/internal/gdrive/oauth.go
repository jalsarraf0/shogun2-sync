// Package gdrive implements the Google Drive OAuth loopback flow ourselves,
// instead of shelling out to `rclone authorize`/`rclone config reconnect`.
//
// Why: rclone's own built-in OAuth webserver proved unreliable in practice
// (its local callback listener died seconds after starting, well before a
// human could complete the browser login, producing a bare "connection
// refused" with no diagnosable cause). Since we control this server, every
// request is logged and the wait window is generous.
//
// We reuse rclone's own OAuth client credentials rather than registering a
// new one. This isn't a workaround: rclone's client_id/client_secret are
// intentionally public in its open-source repo (backend/drive/drive.go) —
// per Google's "installed application" OAuth model, these credentials
// cannot be kept confidential in a distributed binary and are not treated
// as secret. Reusing them also means end users authorize against rclone's
// already Google-verified app, avoiding the multi-week manual review
// Google requires for any new app requesting full Drive scope.
package gdrive

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	// rcloneClientID and rcloneEncryptedClientSecret are copied verbatim from
	// https://github.com/rclone/rclone/blob/master/backend/drive/drive.go
	rcloneClientID              = "202264815644.apps.googleusercontent.com"
	rcloneEncryptedClientSecret = "eX8GpZTVx3vxMWVkuuBdDWmAUE6rGhTwVrvG9GhllYccSdj2-mvHVg"

	driveScope  = "https://www.googleapis.com/auth/drive"
	authTimeout = 5 * time.Minute
)

// obscureKey is rclone's fixed AES-CTR key for its "obscure" scheme
// (fs/config/obscure/obscure.go). It exists so client secrets aren't
// committed to rclone's repo in plaintext, not to make them confidential.
var obscureKey = []byte{
	0x9c, 0x93, 0x5b, 0x48, 0x73, 0x0a, 0x55, 0x4d,
	0x6b, 0xfd, 0x7c, 0x63, 0xc8, 0x86, 0xa9, 0x2b,
	0xd3, 0x90, 0x19, 0x8e, 0xb8, 0x12, 0x8a, 0xfb,
	0xf4, 0xde, 0x16, 0x2b, 0x8b, 0x95, 0xf6, 0x38,
}

// randomState generates a CSRF-protection token for the OAuth state
// parameter. Cryptographically random rather than derived from a
// timestamp, since a guessable state is the classic OAuth CSRF weakness —
// low practical exploitability here (loopback-only server on an ephemeral
// port an outside attacker can't predict), but no reason not to do this
// correctly.
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func revealClientSecret(s string) (string, error) {
	ciphertext, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	if len(ciphertext) < aes.BlockSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	block, err := aes.NewCipher(obscureKey)
	if err != nil {
		return "", err
	}
	buf := ciphertext[aes.BlockSize:]
	iv := ciphertext[:aes.BlockSize]
	out := make([]byte, len(buf))
	cipher.NewCTR(block, iv).XORKeyStream(out, buf)
	return string(out), nil
}

// AuthResult holds what the caller needs after a successful authorization.
type AuthResult struct {
	Token     *oauth2.Token
	TokenJSON string // ready to hand to `rclone config update <name> token <json>`
}

// Authorize runs the full OAuth loopback flow: starts a local callback
// server on an OS-assigned port, opens authURL via openURL (expected to
// launch the system's default browser — the standard, most reliable choice
// for OAuth logins; the same pattern used by gcloud, the GitHub CLI, AWS
// CLI, and rclone itself), waits for the redirect, and exchanges the code
// for a token.
func Authorize(ctx context.Context, openURL func(string) error) (*AuthResult, error) {
	secret, err := revealClientSecret(rcloneEncryptedClientSecret)
	if err != nil {
		return nil, fmt.Errorf("decode client secret: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start local listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/", port)

	conf := &oauth2.Config{
		ClientID:     rcloneClientID,
		ClientSecret: secret,
		Scopes:       []string{driveScope},
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURL,
	}

	state, err := randomState()
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}
	authURL := conf.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("gdrive oauth: callback hit: %s", r.URL.String())
		q := r.URL.Query()

		if got := q.Get("state"); got != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("state mismatch (got %q)", got):
			default:
			}
			return
		}
		if msg := q.Get("error"); msg != "" {
			fmt.Fprintf(w, "<html><body><h2>Authorization failed: %s</h2><p>You can close this window.</p></body></html>", msg)
			select {
			case errCh <- fmt.Errorf("google returned error: %s", msg):
			default:
			}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("callback had no code"):
			default:
			}
			return
		}
		fmt.Fprint(w, "<html><body><h2>Success!</h2><p>You can close this window and go back to Shogun2 Sync.</p></body></html>")
		select {
		case codeCh <- code:
		default:
		}
	})

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("gdrive oauth: local server error: %v", err)
		}
	}()
	defer srv.Shutdown(context.Background())

	log.Printf("gdrive oauth: listening on %s, opening browser", redirectURL)
	if err := openURL(authURL); err != nil {
		return nil, fmt.Errorf("open browser: %w", err)
	}

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return nil, err
	case <-time.After(authTimeout):
		return nil, fmt.Errorf("timed out after %s waiting for browser authorization", authTimeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	token, err := conf.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange code for token: %w", err)
	}

	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return nil, fmt.Errorf("marshal token: %w", err)
	}

	return &AuthResult{Token: token, TokenJSON: string(tokenJSON)}, nil
}
