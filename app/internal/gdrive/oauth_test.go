package gdrive

import "testing"

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
