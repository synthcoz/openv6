package agent

import (
	"testing"
	"time"

	"openv6/internal/proto"
)

// The heartbeat must stay cheap: an unchanged desired state is not
// re-applied until resyncEvery, a change or a re-apply request is.
func TestApplyOnlyOnChange(t *testing.T) {
	a := New(Config{DryRun: true, DataDir: t.TempDir()})
	defer a.proxies.Close()
	r := &proto.SyncResponse{NodeID: 1, Interval: 5}
	a.maybeApply(r)
	first := a.lastApply
	if first.IsZero() {
		t.Fatal("first sync not applied")
	}
	time.Sleep(10 * time.Millisecond)
	a.maybeApply(&proto.SyncResponse{NodeID: 1, Interval: 5, Commands: []proto.Command{{ID: "x", Type: "reapply"}}})
	if a.lastApply != first {
		t.Fatal("unchanged state was re-applied")
	}
	time.Sleep(50 * time.Millisecond) // the reapply command sets the flag
	a.maybeApply(r)
	if a.lastApply == first {
		t.Fatal("reapply request ignored")
	}
	second := a.lastApply
	a.maybeApply(&proto.SyncResponse{NodeID: 1, Interval: 5, Bypass: true})
	if a.lastApply == second {
		t.Fatal("changed state not applied")
	}
}
