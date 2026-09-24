package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/state"
)

func TestLoopbackAndReadOnly(t *testing.T) {
	dir := t.TempDir()
	st := state.Open(dir)
	rep := &domain.RunReport{ID: "run_abc", CreatedAt: time.Now().UTC(), DataAsOf: time.Now().UTC(), Outcome: "succeeded", SchemaVersion: "evolvectl.run/v1"}
	if err := st.SaveReport(rep); err != nil {
		t.Fatal(err)
	}
	h := (&Server{Store: st}).Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil)
	req.Host = "evil.example"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("host status %d", rr.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/runs", nil)
	req.Host = "127.0.0.1:9"
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post status %d", rr.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil)
	req.Host = "127.0.0.1:9"
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get status %d %s", rr.Code, rr.Body.String())
	}
}

func TestRejectsPublicBind(t *testing.T) {
	if _, _, err := Listen("0.0.0.0", "0"); err == nil {
		t.Fatal("bound public address")
	}
}
