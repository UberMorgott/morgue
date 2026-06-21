package recipe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFreePort(t *testing.T) {
	p, err := freePort()
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	if p <= 0 || p > 65535 {
		t.Fatalf("freePort returned %d", p)
	}
}

func TestWaitReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	cl := &AssetRipperClient{BaseURL: srv.URL, HTTP: srv.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := cl.waitReady(ctx); err != nil {
		t.Fatalf("waitReady: %v", err)
	}
}

func TestWaitReadyTimesOut(t *testing.T) {
	// Point at a free (unbound) port so connections fail until ctx expires.
	p, err := freePort()
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	cl := &AssetRipperClient{
		BaseURL: "http://127.0.0.1:" + itoaInt(p),
		HTTP:    &http.Client{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := cl.waitReady(ctx); err == nil {
		t.Fatal("expected waitReady to time out against an unbound port")
	}
}
