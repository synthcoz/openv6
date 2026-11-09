// Command sign writes SHA256SUMS.sig for a release: an ed25519 signature
// (base64) of SHA256SUMS using the seed in $OPENV6_SIGNING_KEY (base64).
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: sign <dir with SHA256SUMS>")
		os.Exit(2)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("OPENV6_SIGNING_KEY")))
	if err != nil || len(seed) != ed25519.SeedSize {
		fmt.Fprintln(os.Stderr, "OPENV6_SIGNING_KEY missing or invalid")
		os.Exit(1)
	}
	dir := os.Args[1]
	sums, err := os.ReadFile(dir + "/SHA256SUMS")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), sums)
	if err := os.WriteFile(dir+"/SHA256SUMS.sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("signed", dir+"/SHA256SUMS")
}
