package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySums(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	old := PublicKey
	PublicKey = base64.StdEncoding.EncodeToString(pub)
	defer func() { PublicKey = old }()
	bin := []byte("openv6 binary")
	h := sha256.Sum256(bin)
	sums := []byte("# openv6 v1.2.3\n" + hex.EncodeToString(h[:]) + "  openv6-linux-amd64\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)))
	m, err := VerifySums(sums, sig, "v1.2.3")
	if err != nil || m["openv6-linux-amd64"] == "" {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if _, err := VerifySums(sums, sig, "v1.3.0"); err == nil {
		t.Fatal("old signed release accepted as another version (rollback)")
	}
	p := filepath.Join(t.TempDir(), "b")
	_ = os.WriteFile(p, bin, 0o644)
	if err := VerifyFile(p, m["openv6-linux-amd64"]); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(p, []byte("evil"), 0o644)
	if VerifyFile(p, m["openv6-linux-amd64"]) == nil {
		t.Fatal("tampered file accepted")
	}
	tampered := append([]byte{}, sums...)
	tampered[0] ^= 1
	if _, err := VerifySums(tampered, sig, "v1.2.3"); err == nil {
		t.Fatal("tampered sums accepted")
	}
}
