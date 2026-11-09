package netcfg

import (
	"os"
	"testing"
)

// Runs only in a private network namespace: scripts/netns-test.sh
func TestBypassInNetns(t *testing.T) {
	if os.Getenv("OPENV6_NETNS_TEST") != "1" {
		t.Skip("run via scripts/netns-test.sh")
	}
	steps, err := SelfTestBypass(t.TempDir())
	for _, s := range steps {
		t.Log(s)
	}
	if err != nil {
		t.Fatal(err)
	}
}
