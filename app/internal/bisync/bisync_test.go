package bisync

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestShellQuoteSurvivesApostrophes(t *testing.T) {
	cases := map[string]string{
		"/home/ken/Drive":     `'/home/ken/Drive'`,
		"/home/o'brien/Drive": `'/home/o'\''brien/Drive'`,
		"/home/ken/100%/sync": `'/home/ken/100%/sync'`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSystemdQuoteProtectsSpecialCharacters(t *testing.T) {
	want := `"/home/ken/My $$aves/100%%/run\\\"now.sh"`
	if got := systemdQuote(`/home/ken/My $aves/100%/run\"now.sh`); got != want {
		t.Errorf("systemdQuote = %q, want %q", got, want)
	}
}

// The guard has to be spelled exactly as systemd spells it. ConditionPathIsExecutable
// is not a real directive, and a quoted value is rejected as "not absolute" — either
// mistake makes systemd drop the line and run the timer unconditionally, which is the
// failure this guard exists to prevent.
func TestServiceSkipsAfterBundledRcloneIsUninstalled(t *testing.T) {
	unit := serviceUnit(
		`/opt/shogun2sync/rclone`,
		`/bin/sh`,
		`/home/ken/.config/shogun2sync/gdrive-bisync.sh`,
	)
	if !strings.Contains(unit, "\nConditionFileIsExecutable=/opt/shogun2sync/rclone\n") {
		t.Fatalf("service does not guard its bundled rclone path:\n%s", unit)
	}
	if strings.Contains(unit, "ConditionPathIsExecutable") {
		t.Fatalf("service uses a directive systemd does not know:\n%s", unit)
	}
}

func TestServiceConditionEscapesSpecifiersAndDropsUnusablePaths(t *testing.T) {
	unit := serviceUnit(`/opt/100%pure/rclone`, `/bin/sh`, `/tmp/s.sh`)
	if !strings.Contains(unit, "\nConditionFileIsExecutable=/opt/100%%pure/rclone\n") {
		t.Fatalf("percent was not escaped for the condition:\n%s", unit)
	}

	// A relative path in a Condition* value makes systemd log an error and skip
	// the line, so omit the guard rather than ship a unit that logs on every run.
	// A newline would let a crafted path inject its own directives.
	for _, unusable := range []string{"rclone", "", "/opt/bad\nExecStart=/bin/false"} {
		guarded := serviceUnit(unusable, `/bin/sh`, `/tmp/s.sh`)
		if strings.Contains(guarded, "ConditionFileIsExecutable") {
			t.Fatalf("emitted a condition for unusable path %q:\n%s", unusable, guarded)
		}
	}
}

// The generated script is what actually runs every two minutes, so its
// shape matters more than most code in this app.
func TestSyncScriptIsValidShellAndQuotesPaths(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the generated script only ever runs on Linux")
	}
	awkward := "/home/o'brien/Google Drive 100%"
	script := syncScript("/usr/bin/rclone", awkward, "gdrive:Shogun2SaveSync", "/tmp/x.log")

	// `sh -n` parses without executing: catches any quoting mistake that
	// would otherwise only show up as a broken timer on someone's machine.
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated script is not valid shell: %v\n%s\n---\n%s", err, out, script)
	}

	if !strings.Contains(script, `'/home/o'\''brien/Google Drive 100%'`) {
		t.Errorf("path not safely quoted in script:\n%s", script)
	}

	// A bare `--resync` on any failure would let a transient network error
	// overwrite the other player's newer saves.
	for _, want := range []string{
		`if [ "$status" -eq 7 ] && [ -z "$(ls -A "$LOCAL" 2>/dev/null)" ]; then`,
		"--resync-mode path2",
		"--resilient",
		"--recover",
		"--max-lock 2m",
		"--conflict-resolve newer",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script is missing %q:\n%s", want, script)
		}
	}
}

// Guarding the resync only matters if it's genuinely unreachable when the
// folder has saves in it. Run the real generated script against a stub
// rclone that always reports the critical-abort exit code.
func TestScriptDoesNotResyncWhenLocalFolderHasSaves(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the generated script only ever runs on Linux")
	}
	dir := t.TempDir()
	stub := dir + "/rclone"
	// Records every invocation, and always fails the way rclone does when
	// it needs a --resync to recover.
	if err := writeFile(stub, "#!/bin/sh\necho \"$@\" >> "+dir+"/calls\nexit 7\n"); err != nil {
		t.Fatal(err)
	}

	local := dir + "/local"
	if err := mkdirAll(local); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(local+"/Otomo Spring 1545.save", "x"); err != nil {
		t.Fatal(err)
	}

	script := dir + "/sync.sh"
	if err := writeFile(script, syncScript(stub, local, "gdrive:Shogun2SaveSync", dir+"/log")); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("sh", script).CombinedOutput()
	if err == nil {
		t.Fatalf("expected the script to report the failure, got success: %s", out)
	}

	calls, _ := readFile(dir + "/calls")
	if strings.Contains(calls, "--resync") {
		t.Fatalf("script ran --resync with real saves present, which can overwrite the other player:\n%s", calls)
	}

	// And the opposite case: an empty folder is the one place it's safe.
	if err := removeAll(dir + "/calls"); err != nil {
		t.Fatal(err)
	}
	if err := removeAll(local + "/Otomo Spring 1545.save"); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.Command("sh", script).CombinedOutput(); err == nil {
		t.Fatal("stub always exits 7, so the script should still fail")
	}
	calls, _ = readFile(dir + "/calls")
	if !strings.Contains(calls, "--resync") {
		t.Fatalf("empty folder should have triggered the safe --resync:\n%s", calls)
	}
	if !strings.Contains(calls, "--resync-mode path2") {
		t.Fatalf("empty-folder recovery must make the remote side authoritative:\n%s", calls)
	}
}

func TestMinimumRcloneVersionCoversGeneratedFlags(t *testing.T) {
	if MinRcloneMajor != 1 || MinRcloneMinor < 71 {
		t.Fatalf("minimum rclone is %d.%d; generated bisync flags require 1.71+", MinRcloneMajor, MinRcloneMinor)
	}
}
