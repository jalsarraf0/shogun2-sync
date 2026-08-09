// Package rcloneutil drives the rclone CLI as a subprocess for the pieces
// we don't reimplement ourselves: the actual Drive API calls and the
// bisync engine. We only replace the OAuth handoff (see internal/gdrive),
// which is what was actually unreliable.
package rcloneutil

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// PreferredGoogleDriveRemote is deliberately app-namespaced. If a user
	// already owns this name, ConfigureGoogleDriveRemote selects a stable
	// numeric suffix instead of modifying their remote.
	PreferredGoogleDriveRemote = "shogun2sync-gdrive"

	managedRemoteDescription = "Managed by Shogun2 Sync"
	rcStartupTimeout         = 10 * time.Second
	rcRequestTimeout         = 30 * time.Second
)

var (
	rcAddressPattern  = regexp.MustCompile(`Serving remote control on (http://[^[:space:]]+)`)
	jsonSecretPattern = regexp.MustCompile(
		`(?i)("(?:access_token|refresh_token|id_token|client_secret|token|password)"[[:space:]]*:[[:space:]]*)"(?:\\.|[^"\\])*"`,
	)
	keyValueSecretPattern = regexp.MustCompile(
		`(?im)((?:access_token|refresh_token|id_token|client_secret|token|password)[[:space:]]*[=:][[:space:]]*)[^[:space:],&]+`,
	)
)

// ErrNotInstalled is returned when the rclone binary can't be found.
var ErrNotInstalled = errors.New("rclone is not installed")

// Path returns the rclone executable this app should use. Linux releases
// carry a private copy so distro rclone versions cannot silently be too old
// for the sync engine. A sibling binary supports the raw portable archive;
// the fixed lib path supports installed packages. PATH remains a convenient
// development fallback and is the only lookup used on other operating
// systems.
func Path() (string, error) {
	executable, _ := os.Executable()
	return resolveRclonePath(runtime.GOOS, executable, "/usr/lib/shogun2sync/rclone", exec.LookPath)
}

func resolveRclonePath(goos, executable, privatePath string, lookPath func(string) (string, error)) (string, error) {
	return resolveRclonePathWithCheck(goos, executable, privatePath, lookPath, isExecutableFile)
}

func resolveRclonePathWithCheck(goos, executable, privatePath string, lookPath func(string) (string, error), executableFile func(string) bool) (string, error) {
	if goos == "linux" {
		if executable != "" {
			sibling := filepath.Join(filepath.Dir(executable), "rclone")
			if executableFile(sibling) {
				return sibling, nil
			}
		}
		if executableFile(privatePath) {
			return privatePath, nil
		}
	}
	path, err := lookPath("rclone")
	if err != nil {
		return "", ErrNotInstalled
	}
	return path, nil
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// Installed reports whether an app-bundled or PATH rclone is available.
func Installed() bool {
	_, err := Path()
	return err == nil
}

func run(ctx context.Context, args ...string) (string, error) {
	binary, err := Path()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		command := strings.Join(redactArgs(args), " ")
		detail := strings.TrimSpace(redactText(stderr.String(), secretArgValues(args)...))
		if detail == "" {
			return "", fmt.Errorf("rclone %s: %w", command, err)
		}
		return "", fmt.Errorf("rclone %s: %w: %s", command, err, detail)
	}
	return stdout.String(), nil
}

func isSensitiveKey(s string) bool {
	s = strings.TrimLeft(strings.ToLower(strings.TrimSpace(s)), "-")
	if before, _, ok := strings.Cut(s, "="); ok {
		s = before
	}
	switch s {
	case "token", "access_token", "refresh_token", "id_token", "client_secret", "password", "pass":
		return true
	default:
		return false
	}
}

func redactArgs(args []string) []string {
	redacted := append([]string(nil), args...)
	for i := range redacted {
		if key, _, ok := strings.Cut(redacted[i], "="); ok && isSensitiveKey(key) {
			redacted[i] = key + "=[REDACTED]"
			continue
		}
		if isSensitiveKey(redacted[i]) && i+1 < len(redacted) {
			redacted[i+1] = "[REDACTED]"
			i++
		}
	}
	return redacted
}

func secretArgValues(args []string) []string {
	var secrets []string
	for i := 0; i < len(args); i++ {
		if key, value, ok := strings.Cut(args[i], "="); ok && isSensitiveKey(key) {
			secrets = append(secrets, value)
			continue
		}
		if isSensitiveKey(args[i]) && i+1 < len(args) {
			secrets = append(secrets, args[i+1])
			i++
		}
	}
	return secrets
}

func redactText(s string, secrets ...string) string {
	// Replace longer values first in case one credential contains another.
	secrets = append([]string(nil), secrets...)
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
	}
	s = jsonSecretPattern.ReplaceAllString(s, `${1}"[REDACTED]"`)
	s = keyValueSecretPattern.ReplaceAllString(s, `${1}[REDACTED]`)
	return s
}

func listRemoteNames(ctx context.Context) ([]string, error) {
	out, err := run(ctx, "listremotes")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSuffix(strings.TrimSpace(line), ":")
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// RemoteExists reports whether a remote with this name is already configured.
func RemoteExists(ctx context.Context, name string) (bool, error) {
	if !Installed() {
		return false, ErrNotInstalled
	}
	names, err := listRemoteNames(ctx)
	if err != nil {
		return false, err
	}
	for _, remoteName := range names {
		if remoteName == name {
			return true, nil
		}
	}
	return false, nil
}

type remoteMetadata struct {
	Type        string
	Description string
}

func readRemoteMetadata(ctx context.Context, name string) (remoteMetadata, error) {
	// `config redacted` is intentionally used instead of config dump/show so
	// an inspection failure can never place another remote's credentials in
	// our captured stdout or returned errors.
	out, err := run(ctx, "config", "redacted", name)
	if err != nil {
		return remoteMetadata{}, err
	}
	var metadata remoteMetadata
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "type":
			metadata.Type = strings.TrimSpace(value)
		case "description":
			metadata.Description = strings.TrimSpace(value)
		}
	}
	return metadata, nil
}

func remoteSuffixRank(name, preferred string) (int, bool) {
	if name == preferred {
		return 1, true
	}
	prefix := preferred + "-"
	if !strings.HasPrefix(name, prefix) {
		return 0, false
	}
	suffix := strings.TrimPrefix(name, prefix)
	n, err := strconv.Atoi(suffix)
	if err != nil || n < 2 || strconv.Itoa(n) != suffix {
		return 0, false
	}
	return n, true
}

type metadataReader func(context.Context, string) (remoteMetadata, error)

func selectManagedRemote(ctx context.Context, preferred string, names []string, readMetadata metadataReader) (name string, exists bool, err error) {
	occupied := make(map[string]bool, len(names))
	type candidate struct {
		name string
		rank int
	}
	var candidates []candidate
	for _, name := range names {
		occupied[name] = true
		if rank, ok := remoteSuffixRank(name, preferred); ok {
			candidates = append(candidates, candidate{name: name, rank: rank})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].rank < candidates[j].rank })
	for _, candidate := range candidates {
		metadata, err := readMetadata(ctx, candidate.name)
		if err != nil {
			return "", false, err
		}
		if metadata.Type == "drive" && metadata.Description == managedRemoteDescription {
			return candidate.name, true, nil
		}
	}

	for rank := 1; ; rank++ {
		candidateName := preferred
		if rank > 1 {
			candidateName = preferred + "-" + strconv.Itoa(rank)
		}
		if !occupied[candidateName] {
			return candidateName, false, nil
		}
	}
}

func validateRemoteName(name string) error {
	if name == "" || name != strings.TrimSpace(name) || strings.ContainsAny(name, ":\r\n") {
		return fmt.Errorf("invalid rclone remote name %q", name)
	}
	return nil
}

// ConfigureGoogleDriveRemote creates or updates an app-owned Google Drive
// remote and returns the actual remote name. The preferred name is never
// overwritten unless it carries this app's ownership marker; an unrelated
// remote causes a stable numeric suffix to be selected instead.
//
// rootFolderID may be empty. It is deliberately written even when empty so
// reauthorizing a previously scoped remote correctly restores My Drive as
// the root.
//
// OAuth material is sent to a short-lived rclone remote-control process in
// an HTTP request body. It is never placed in subprocess arguments, where it
// would be visible to process-list tools.
func ConfigureGoogleDriveRemote(ctx context.Context, preferredName, rootFolderID, tokenJSON, clientID, clientSecret string) (string, error) {
	if !Installed() {
		return "", ErrNotInstalled
	}
	if err := validateRemoteName(preferredName); err != nil {
		return "", err
	}
	if strings.TrimSpace(tokenJSON) == "" {
		return "", errors.New("Google OAuth token is empty")
	}
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientSecret) == "" {
		return "", errors.New("Google client ID and client secret must be provided together")
	}

	names, err := listRemoteNames(ctx)
	if err != nil {
		return "", err
	}
	name, exists, err := selectManagedRemote(ctx, preferredName, names, readRemoteMetadata)
	if err != nil {
		return "", err
	}

	parameters := map[string]string{
		"scope":                "drive",
		"token":                tokenJSON,
		"client_id":            clientID,
		"client_secret":        clientSecret,
		"description":          managedRemoteDescription,
		"root_folder_id":       rootFolderID,
		"config_refresh_token": "false",
	}
	request := map[string]any{
		"name":       name,
		"parameters": parameters,
		"opt": map[string]bool{
			"nonInteractive": true,
			"noOutput":       true,
		},
	}
	endpoint := "config/update"
	if !exists {
		endpoint = "config/create"
		request["type"] = "drive"
		parameters["config_is_local"] = "false"
	}
	if err := invokeRcloneRC(ctx, endpoint, request, tokenJSON, clientSecret); err != nil {
		return "", err
	}
	return name, nil
}

type rcClient struct {
	baseURL  string
	username string
	password string
	client   *http.Client
}

func (c rcClient) call(ctx context.Context, endpoint string, payload any, secrets ...string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode rclone configuration request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+"/"+endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create rclone configuration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("call rclone %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return fmt.Errorf("read rclone %s response: %w", endpoint, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(redactText(string(responseBody), secrets...))
		if detail == "" {
			detail = resp.Status
		}
		return fmt.Errorf("rclone %s failed: %s", endpoint, detail)
	}
	var result struct {
		Error string `json:"Error"`
	}
	if len(responseBody) != 0 && json.Unmarshal(responseBody, &result) == nil && result.Error != "" {
		return fmt.Errorf("rclone %s failed: %s", endpoint, redactText(result.Error, secrets...))
	}
	return nil
}

type synchronizedLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *synchronizedLog) append(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.b.Len() < 32<<10 {
		l.b.WriteString(line)
		l.b.WriteByte('\n')
	}
}

func (l *synchronizedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func randomRCPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func rcloneRCDArgs() []string {
	return []string{
		"rcd",
		"--rc-addr", "127.0.0.1:0",
		"--ask-password=false",
		"--log-level", "NOTICE",
		"--log-format", "",
	}
}

func setProcessEnv(environ []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environ)+1)
	for _, entry := range environ {
		entryKey, _, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(entryKey, key) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, prefix+value)
}

func invokeRcloneRC(ctx context.Context, endpoint string, payload any, secrets ...string) error {
	binary, err := Path()
	if err != nil {
		return err
	}
	password, err := randomRCPassword()
	if err != nil {
		return fmt.Errorf("secure rclone control channel: %w", err)
	}
	const username = "shogun2sync"
	startupSecrets := append(append([]string(nil), secrets...), password)

	daemonCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(daemonCtx, binary, rcloneRCDArgs()...)
	// Keep even the short-lived control credential out of the process list.
	// Unlike OAuth material it only protects this one loopback daemon, but
	// putting it in the environment also prevents another local account from
	// racing a privileged RC call during the configuration request.
	cmd.Env = setProcessEnv(os.Environ(), "RCLONE_RC_USER", username)
	cmd.Env = setProcessEnv(cmd.Env, "RCLONE_RC_PASS", password)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("capture rclone control output: %w", err)
	}
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start rclone control process: %w", err)
	}

	addressCh := make(chan string, 1)
	var logs synchronizedLog
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			logs.append(line)
			if match := rcAddressPattern.FindStringSubmatch(line); len(match) == 2 {
				select {
				case addressCh <- match[1]:
				default:
				}
			}
		}
	}()
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	stop := func() {
		cancel()
		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-waitCh
		}
	}

	timer := time.NewTimer(rcStartupTimeout)
	defer timer.Stop()
	var address string
	select {
	case address = <-addressCh:
	case err := <-waitCh:
		cancel()
		detail := strings.TrimSpace(redactText(logs.String(), startupSecrets...))
		if detail != "" {
			return fmt.Errorf("rclone control process exited during startup: %w: %s", err, detail)
		}
		return fmt.Errorf("rclone control process exited during startup: %w", err)
	case <-timer.C:
		stop()
		return fmt.Errorf("timed out starting rclone control process: %s", strings.TrimSpace(redactText(logs.String(), startupSecrets...)))
	case <-ctx.Done():
		stop()
		return ctx.Err()
	}
	defer stop()

	client := rcClient{
		baseURL:  address,
		username: username,
		password: password,
		client: &http.Client{
			Timeout: rcRequestTimeout,
			Transport: &http.Transport{
				Proxy:       nil,
				DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			},
		},
	}
	return client.call(ctx, endpoint, payload, secrets...)
}

// EnsureSubfolder creates subfolder at the root of remote if it doesn't
// already exist. rclone mkdir is idempotent.
func EnsureSubfolder(ctx context.Context, remoteName, subfolder string) error {
	if !Installed() {
		return ErrNotInstalled
	}
	_, err := run(ctx, "mkdir", remoteName+":"+subfolder)
	return err
}

// VerifyAccess does a cheap round-trip against the Drive API (listing the
// configured root) to confirm the token and root_folder_id actually work,
// so setup fails loudly here instead of silently later during sync.
func VerifyAccess(ctx context.Context, remoteName string) error {
	if !Installed() {
		return ErrNotInstalled
	}
	_, err := run(ctx, "lsd", remoteName+":", "--max-depth", "1")
	return err
}

// ShareableLink returns a Drive share link for remoteName:subfolder, for
// the "I own this Drive" flow — the host needs something to actually send
// their friend after creating the sync folder.
func ShareableLink(ctx context.Context, remoteName, subfolder string) (string, error) {
	if !Installed() {
		return "", ErrNotInstalled
	}
	out, err := run(ctx, "link", remoteName+":"+subfolder)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
