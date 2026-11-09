package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// PublicKey verifies SHA256SUMS of every release (ed25519, base64). The
// matching private key lives only in the release workflow's secrets, so a
// compromised download location cannot ship a binary openv6 will install.
var PublicKey = "Fcgc0SP+wMcB/FgW0aIGbMHk633JWtjUs8U78PXKJ2Y="

// ErrUnsigned: the release has no valid signature; nothing is installed.
var ErrUnsigned = errors.New("release is not signed by openv6; update refused (download the installer manually)")

// Checksums downloads and verifies SHA256SUMS(.sig) of a release and
// returns asset name -> sha256 hex.
func (r *Release) Checksums(ctx context.Context) (map[string]string, error) {
	su, sigu := r.Assets["SHA256SUMS"], r.Assets["SHA256SUMS.sig"]
	if su == "" || sigu == "" {
		return nil, ErrUnsigned
	}
	dir, err := os.MkdirTemp("", "openv6-sums-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	read := func(u string) ([]byte, error) {
		p, err := Download(ctx, u, filepath.Join(dir, "x"), false)
		if err != nil {
			return nil, err
		}
		return os.ReadFile(p)
	}
	sums, err := read(su)
	if err != nil {
		return nil, err
	}
	sig, err := read(sigu)
	if err != nil {
		return nil, err
	}
	return VerifySums(sums, sig)
}

// VerifySums checks the signature and parses "hex  name" lines.
func VerifySums(sums, sig []byte) (map[string]string, error) {
	pub, err := base64.StdEncoding.DecodeString(PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, ErrUnsigned
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), sums, raw) {
		return nil, ErrUnsigned
	}
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && len(f[0]) == 64 {
			out[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return out, nil
}

// VerifyFile compares a downloaded file with its signed checksum.
func VerifyFile(path, want string) error {
	if want == "" {
		return fmt.Errorf("%s is not listed in the signed checksums", filepath.Base(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch for %s: the download was altered or corrupted", filepath.Base(path))
	}
	return nil
}
