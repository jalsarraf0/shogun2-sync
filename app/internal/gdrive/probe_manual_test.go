//go:build manual

package gdrive

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Manual probe: is rclone's shared OAuth client still alive? A disabled or
// unknown client answers invalid_client; a live one answers invalid_grant
// for a bogus authorization code. Run explicitly with -run TestProbe.
func TestProbeSharedClient(t *testing.T) {
	if testing.Short() {
		t.Skip("network probe")
	}
	secret, err := revealClientSecret(rcloneEncryptedClientSecret)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{
		"client_id":     {rcloneClientID},
		"client_secret": {secret},
		"code":          {"bogus-code-for-probe"},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {"http://127.0.0.1:1/"},
	}
	resp, err := http.Post("https://oauth2.googleapis.com/token",
		"application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	t.Logf("status=%s body=%s", resp.Status, b)
}
