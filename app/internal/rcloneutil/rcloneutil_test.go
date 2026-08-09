package rcloneutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactArgsAndErrorText(t *testing.T) {
	const (
		token  = `{"access_token":"access-value","refresh_token":"refresh-value"}`
		secret = "client-secret-value"
	)
	args := []string{
		"config", "update", "remote",
		"token", token,
		"client_secret=" + secret,
		"scope", "drive",
	}
	safeCommand := strings.Join(redactArgs(args), " ")
	if strings.Contains(safeCommand, token) || strings.Contains(safeCommand, secret) {
		t.Fatalf("redacted command leaked a credential: %s", safeCommand)
	}
	if strings.Count(safeCommand, "[REDACTED]") != 2 {
		t.Fatalf("redacted command = %s", safeCommand)
	}

	stderr := `failed input={"token":"{\"access_token\":\"access-value\",\"refresh_token\":\"refresh-value\"}","client_secret":"client-secret-value"} refresh_token=refresh-value`
	safeError := redactText(stderr, token, secret)
	for _, value := range []string{token, secret, "access-value", "refresh-value"} {
		if strings.Contains(safeError, value) {
			t.Fatalf("redacted error leaked %q: %s", value, safeError)
		}
	}
	if !strings.Contains(safeError, "[REDACTED]") {
		t.Fatalf("redacted error has no redaction marker: %s", safeError)
	}
}

func TestSelectManagedRemoteReusesOwnedRemoteDeterministically(t *testing.T) {
	names := []string{
		PreferredGoogleDriveRemote,
		PreferredGoogleDriveRemote + "-2",
		PreferredGoogleDriveRemote + "-3",
		"personal-drive",
	}
	metadata := map[string]remoteMetadata{
		PreferredGoogleDriveRemote:        {Type: "drive", Description: "user remote"},
		PreferredGoogleDriveRemote + "-2": {Type: "drive", Description: "another user remote"},
		PreferredGoogleDriveRemote + "-3": {Type: "drive", Description: managedRemoteDescription},
	}
	read := func(_ context.Context, name string) (remoteMetadata, error) {
		value, ok := metadata[name]
		if !ok {
			return remoteMetadata{}, fmt.Errorf("unexpected metadata read for %q", name)
		}
		return value, nil
	}

	got, exists, err := selectManagedRemote(context.Background(), PreferredGoogleDriveRemote, names, read)
	if err != nil {
		t.Fatal(err)
	}
	if got != PreferredGoogleDriveRemote+"-3" || !exists {
		t.Fatalf("selectManagedRemote = (%q, %v), want owned -3", got, exists)
	}
}

func TestSelectManagedRemoteDoesNotHijackOccupiedNames(t *testing.T) {
	names := []string{PreferredGoogleDriveRemote, PreferredGoogleDriveRemote + "-2"}
	read := func(_ context.Context, name string) (remoteMetadata, error) {
		return remoteMetadata{Type: "drive", Description: "belongs to the user"}, nil
	}
	got, exists, err := selectManagedRemote(context.Background(), PreferredGoogleDriveRemote, names, read)
	if err != nil {
		t.Fatal(err)
	}
	if got != PreferredGoogleDriveRemote+"-3" || exists {
		t.Fatalf("selectManagedRemote = (%q, %v), want unused -3", got, exists)
	}
}

func TestSelectManagedRemoteOnlyTrustsDriveOwnershipMarker(t *testing.T) {
	names := []string{PreferredGoogleDriveRemote}
	read := func(_ context.Context, name string) (remoteMetadata, error) {
		return remoteMetadata{Type: "s3", Description: managedRemoteDescription}, nil
	}
	got, exists, err := selectManagedRemote(context.Background(), PreferredGoogleDriveRemote, names, read)
	if err != nil {
		t.Fatal(err)
	}
	if got != PreferredGoogleDriveRemote+"-2" || exists {
		t.Fatalf("selectManagedRemote = (%q, %v), want unused -2", got, exists)
	}
}

func TestRemoteSuffixRank(t *testing.T) {
	tests := []struct {
		name string
		rank int
		ok   bool
	}{
		{name: PreferredGoogleDriveRemote, rank: 1, ok: true},
		{name: PreferredGoogleDriveRemote + "-2", rank: 2, ok: true},
		{name: PreferredGoogleDriveRemote + "-19", rank: 19, ok: true},
		{name: PreferredGoogleDriveRemote + "-1"},
		{name: PreferredGoogleDriveRemote + "-02"},
		{name: PreferredGoogleDriveRemote + "-notes"},
		{name: "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rank, ok := remoteSuffixRank(tt.name, PreferredGoogleDriveRemote)
			if rank != tt.rank || ok != tt.ok {
				t.Fatalf("remoteSuffixRank(%q) = (%d, %v), want (%d, %v)", tt.name, rank, ok, tt.rank, tt.ok)
			}
		})
	}
}

func TestRCClientSendsCredentialsInBodyAndRedactsFailure(t *testing.T) {
	const (
		username = "rc-user"
		password = "rc-password"
		token    = `{"access_token":"access-value","refresh_token":"refresh-value"}`
		secret   = "client-secret-value"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/config/update" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		gotUser, gotPassword, ok := r.BasicAuth()
		if !ok || gotUser != username || gotPassword != password {
			t.Errorf("BasicAuth = (%q, %q, %v)", gotUser, gotPassword, ok)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if !bytesContainAll(body, token, secret) {
			t.Errorf("request body did not contain configuration credentials")
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "rejected " + secret,
			"input": map[string]any{"token": token, "client_secret": secret},
		})
	}))
	defer server.Close()

	client := rcClient{
		baseURL: server.URL, username: username, password: password, client: server.Client(),
	}
	err := client.call(context.Background(), "config/update", map[string]any{
		"parameters": map[string]string{"token": token, "client_secret": secret},
	}, token, secret)
	if err == nil {
		t.Fatal("call unexpectedly succeeded")
	}
	for _, value := range []string{token, secret, "access-value", "refresh-value"} {
		if strings.Contains(err.Error(), value) {
			t.Fatalf("RC error leaked %q: %v", value, err)
		}
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("RC error did not report a redaction: %v", err)
	}
}

func TestRCDProcessArgumentsContainNoCredentials(t *testing.T) {
	args := strings.Join(rcloneRCDArgs(), " ")
	for _, forbidden := range []string{"token", "client_secret", "access-value", "refresh-value", "--rc-user", "--rc-pass"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("rclone rcd arguments contain %q: %s", forbidden, args)
		}
	}
}

func TestSetProcessEnvReplacesExistingValue(t *testing.T) {
	got := setProcessEnv([]string{"PATH=/bin", "RCLONE_RC_PASS=old", "rclone_rc_pass=duplicate"}, "RCLONE_RC_PASS", "new")
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "old") || strings.Contains(joined, "duplicate") {
		t.Fatalf("setProcessEnv retained an old credential: %q", got)
	}
	if strings.Count(joined, "RCLONE_RC_PASS=new") != 1 {
		t.Fatalf("setProcessEnv result = %q", got)
	}
}

func bytesContainAll(body []byte, values ...string) bool {
	for _, value := range values {
		encoded, _ := json.Marshal(value)
		if !strings.Contains(string(body), string(encoded)) {
			return false
		}
	}
	return true
}

func TestConfigureGoogleDriveRemoteUsesNamespaceAndClearsRoot(t *testing.T) {
	if !Installed() {
		t.Skip("rclone is not installed")
	}
	configPath := filepath.Join(t.TempDir(), "rclone.conf")
	t.Setenv("RCLONE_CONFIG", configPath)
	initial := fmt.Sprintf("[%s]\ntype = drive\ndescription = Personal remote\n", PreferredGoogleDriveRemote)
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	const tokenOne = `{"access_token":"first-access","refresh_token":"first-refresh","token_type":"Bearer"}`
	name, err := ConfigureGoogleDriveRemote(
		context.Background(), PreferredGoogleDriveRemote, "old-root", tokenOne, "client-id", "client-secret",
	)
	if err != nil {
		t.Fatal(err)
	}
	if name != PreferredGoogleDriveRemote+"-2" {
		t.Fatalf("created remote = %q, want app suffix -2", name)
	}

	const tokenTwo = `{"access_token":"second-access","refresh_token":"second-refresh","token_type":"Bearer"}`
	name, err = ConfigureGoogleDriveRemote(
		context.Background(), PreferredGoogleDriveRemote, "", tokenTwo, "client-id", "client-secret",
	)
	if err != nil {
		t.Fatal(err)
	}
	if name != PreferredGoogleDriveRemote+"-2" {
		t.Fatalf("reused remote = %q, want app suffix -2", name)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	sections := parseINISections(string(data))
	userRemote := sections[PreferredGoogleDriveRemote]
	if userRemote["description"] != "Personal remote" || userRemote["token"] != "" {
		t.Fatal("pre-existing user remote was modified")
	}
	managed := sections[PreferredGoogleDriveRemote+"-2"]
	if managed["description"] != managedRemoteDescription {
		t.Fatalf("managed remote description = %q", managed["description"])
	}
	if managed["root_folder_id"] != "" {
		t.Fatalf("root_folder_id was not cleared: %q", managed["root_folder_id"])
	}
	if strings.Contains(managed["token"], "first-access") || !strings.Contains(managed["token"], "second-access") {
		t.Fatal("managed remote token was not updated")
	}
}

func parseINISections(data string) map[string]map[string]string {
	sections := make(map[string]map[string]string)
	var current map[string]string
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			current = make(map[string]string)
			sections[name] = current
			continue
		}
		if current == nil {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			current[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return sections
}

func TestValidateRemoteName(t *testing.T) {
	if err := validateRemoteName(PreferredGoogleDriveRemote); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", " remote", "remote ", "remote:name", "remote\nname"} {
		if err := validateRemoteName(name); err == nil {
			t.Errorf("validateRemoteName(%q) unexpectedly succeeded", name)
		}
	}
}

func TestResolveRclonePathPriority(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "shogun2sync")
	sibling := filepath.Join(dir, "rclone")
	private := filepath.Join(dir, "private-rclone")
	executableFiles := map[string]bool{sibling: true, private: true}
	isExecutable := func(path string) bool {
		return executableFiles[path]
	}
	lookPath := func(string) (string, error) { return "/from/PATH/rclone", nil }

	got, err := resolveRclonePathWithCheck("linux", executable, private, lookPath, isExecutable)
	if err != nil || got != sibling {
		t.Fatalf("Linux sibling resolution = (%q, %v), want %q", got, err, sibling)
	}
	delete(executableFiles, sibling)
	got, err = resolveRclonePathWithCheck("linux", executable, private, lookPath, isExecutable)
	if err != nil || got != private {
		t.Fatalf("Linux private resolution = (%q, %v), want %q", got, err, private)
	}
	got, err = resolveRclonePathWithCheck("windows", executable, private, lookPath, isExecutable)
	if err != nil || got != "/from/PATH/rclone" {
		t.Fatalf("non-Linux resolution = (%q, %v), want PATH", got, err)
	}
}

func TestResolveRclonePathReturnsSentinel(t *testing.T) {
	_, err := resolveRclonePath("windows", "", "", func(string) (string, error) {
		return "", exec.ErrNotFound
	})
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("resolveRclonePath error = %v, want ErrNotInstalled", err)
	}
}

func TestSelectManagedRemotePropagatesMetadataError(t *testing.T) {
	want := errors.New("metadata unavailable")
	_, _, err := selectManagedRemote(
		context.Background(), PreferredGoogleDriveRemote, []string{PreferredGoogleDriveRemote},
		func(context.Context, string) (remoteMetadata, error) { return remoteMetadata{}, want },
	)
	if !errors.Is(err, want) {
		t.Fatalf("selectManagedRemote error = %v, want %v", err, want)
	}
}
