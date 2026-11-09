package license

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidateVerdicts(t *testing.T) {
	var status int
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	old := APIBase
	APIBase = srv.URL
	defer func() { APIBase = old }()
	now := time.Now().Unix()
	st := State{Key: "OV6-X", ActivationID: "a", Status: "active", ValidatedAt: now - 3600, CheckedAt: now - 3600}

	// a captive portal / firewall answering 403 with HTML is not a verdict
	status, body = 403, "<html>blocked</html>"
	if got := Validate(context.Background(), st, "m1"); got.Status != "active" || !got.Pro(time.Now()) {
		t.Fatalf("captive portal revoked the key: %+v", got)
	}
	// Polar saying 404 revokes
	status, body = 404, `{"error":"ResourceNotFound","detail":"not found"}`
	if got := Validate(context.Background(), st, "m1"); got.Status != "invalid" {
		t.Fatalf("404 not revoked: %+v", got)
	}
	// activation made on another machine (copied data folder)
	status, body = 200, `{"benefit_id":"e2f37fe8-4ecd-430d-a02e-8b8265487fa8","status":"granted","activation":{"meta":{"machine":"m2"}}}`
	if got := Validate(context.Background(), st, "m1"); got.Status != "invalid" {
		t.Fatalf("other machine accepted: %+v", got)
	}
	if got := Validate(context.Background(), st, "m2"); got.Status != "active" {
		t.Fatalf("own machine rejected: %+v", got)
	}
	// clock moved back: a check "in the future" does not count
	future := st
	future.ValidatedAt = time.Now().Add(72 * time.Hour).Unix()
	if future.Pro(time.Now()) {
		t.Fatal("future validation accepted")
	}
}
