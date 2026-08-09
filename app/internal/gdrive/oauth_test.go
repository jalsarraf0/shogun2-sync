package gdrive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRandomStateIsNonEmptyAndUnique(t *testing.T) {
	a, err := randomState()
	if err != nil {
		t.Fatal(err)
	}
	b, err := randomState()
	if err != nil {
		t.Fatal(err)
	}
	if a == "" || b == "" {
		t.Fatal("randomState returned an empty string")
	}
	if a == b {
		t.Fatal("randomState returned the same value twice in a row")
	}
}

func TestRevealClientSecretMatchesKnownValue(t *testing.T) {
	// Regression check: this is rclone's actual published client secret
	// (backend/drive/drive.go), decoded via its public "obscure" scheme.
	// If this ever breaks, Google Drive auth breaks silently for everyone.
	got, err := revealClientSecret(rcloneEncryptedClientSecret)
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("revealClientSecret returned an empty string")
	}
}

func TestCredentialsValidateRequiresCompletePair(t *testing.T) {
	tests := []struct {
		name    string
		creds   Credentials
		wantErr bool
	}{
		{name: "empty", creds: Credentials{}, wantErr: true},
		{name: "ID only", creds: Credentials{ClientID: "id"}, wantErr: true},
		{name: "secret only", creds: Credentials{ClientSecret: "secret"}, wantErr: true},
		{name: "blank secret", creds: Credentials{ClientID: "id", ClientSecret: "  "}, wantErr: true},
		{name: "complete", creds: Credentials{ClientID: "id", ClientSecret: "secret"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.creds.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCallbackStateMismatchReturnsBadRequestAndKeepsWaiting(t *testing.T) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	handler := callbackHandler("expected-state", codeCh, errCh)

	bad := httptest.NewRecorder()
	handler.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/?state=wrong&code=secret-code", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("mismatched state status = %d, want 400", bad.Code)
	}
	select {
	case code := <-codeCh:
		t.Fatalf("mismatched state delivered code %q", code)
	default:
	}
	select {
	case err := <-errCh:
		t.Fatalf("mismatched state terminated flow: %v", err)
	default:
	}

	good := httptest.NewRecorder()
	handler.ServeHTTP(good, httptest.NewRequest(http.MethodGet, "/?state=expected-state&code=real-code", nil))
	if good.Code != http.StatusOK {
		t.Fatalf("valid callback status = %d, want 200", good.Code)
	}
	select {
	case code := <-codeCh:
		if code != "real-code" {
			t.Fatalf("callback code = %q, want real-code", code)
		}
	default:
		t.Fatal("valid callback did not deliver its code")
	}
}

func TestCallbackEscapesGoogleError(t *testing.T) {
	errCh := make(chan error, 1)
	recorder := httptest.NewRecorder()
	callbackHandler("state", make(chan string, 1), errCh).ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/?state=state&error=%3Cscript%3Ebad%3C%2Fscript%3E", nil),
	)
	if strings.Contains(recorder.Body.String(), "<script>") {
		t.Fatalf("response rendered unescaped OAuth error: %s", recorder.Body.String())
	}
	select {
	case err := <-errCh:
		if !strings.Contains(err.Error(), "<script>bad</script>") {
			t.Fatalf("reported error = %v", err)
		}
	default:
		t.Fatal("OAuth error was not delivered")
	}
}

func TestCallbackServerHasTimeouts(t *testing.T) {
	srv := newCallbackServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Fatalf("callback server has missing timeout: %+v", srv)
	}
}

func TestAuthorizeDoesNotLogCallbackURLStateOrCode(t *testing.T) {
	var logs bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	oldPrefix := log.Prefix()
	log.SetOutput(&logs)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	})

	const rejectedCode = "do-not-log-this-code"
	var redirectURL, oauthState string
	_, err := Authorize(context.Background(), Credentials{ClientID: "id", ClientSecret: "secret"}, func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		redirectURL = u.Query().Get("redirect_uri")
		oauthState = u.Query().Get("state")

		resp, err := http.Get(redirectURL + "?state=wrong&code=" + rejectedCode)
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			return errors.New("mismatched callback did not return 400")
		}

		resp, err = http.Get(redirectURL + "?state=" + url.QueryEscape(oauthState) + "&error=access_denied")
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.Body.Close()
	})
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("Authorize error = %v, want access_denied", err)
	}

	for _, secret := range []string{redirectURL, oauthState, rejectedCode} {
		if secret != "" && strings.Contains(logs.String(), secret) {
			t.Fatalf("OAuth log contains callback material %q: %s", secret, logs.String())
		}
	}
}
