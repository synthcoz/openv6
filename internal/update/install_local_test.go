package update

import (
	"io"
	"os"
	"testing"
)

func TestInstallRealBinary(t *testing.T) {
	src := os.Getenv("OPENV6_TEST_BIN")
	if src == "" {
		t.Skip("set OPENV6_TEST_BIN")
	}
	dir := t.TempDir()
	cp := func() string {
		f, _ := os.CreateTemp(dir, "tmp")
		s, _ := os.Open(src)
		io.Copy(f, s)
		s.Close()
		f.Close()
		return f.Name()
	}
	dst := dir + "/openv6"
	os.WriteFile(dst, []byte("old"), 0o755)
	if err := Install(cp(), dst, "v9.9.9"); err == nil {
		t.Fatal("wrong version accepted")
	}
	if b, _ := os.ReadFile(dst); string(b) != "old" {
		t.Fatal("dst replaced on failure")
	}
	if err := Install(cp(), dst, "v0.2.0"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) == "old" {
		t.Fatal("not replaced")
	}
	bad := dir + "/bad"
	os.WriteFile(bad, []byte("<html>"), 0o644)
	if err := Install(bad, dst, ""); err == nil {
		t.Fatal("non-ELF accepted")
	}
}
