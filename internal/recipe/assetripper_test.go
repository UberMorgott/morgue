package recipe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestAssetRipperDriverFlow(t *testing.T) {
	var mu sync.Mutex
	var hits []string
	loadPath, exportPath := "", ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.Path)
		mu.Unlock()
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/LoadFolder":
			loadPath = r.FormValue("Path")
		case "/Export/PrimaryContent":
			exportPath = r.FormValue("Path")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cl := &AssetRipperClient{BaseURL: srv.URL, HTTP: srv.Client()}
	err := cl.ExportPrimaryContent(context.Background(), "D:/game/Game_Data", "D:/out/data")
	if err != nil {
		t.Fatalf("ExportPrimaryContent: %v", err)
	}
	if loadPath != "D:/game/Game_Data" {
		t.Fatalf("LoadFolder Path = %q", loadPath)
	}
	if exportPath != "D:/out/data" {
		t.Fatalf("Export Path = %q", exportPath)
	}
	want := []string{"/LoadFolder", "/Export/PrimaryContent", "/Reset"}
	if len(hits) != 3 || hits[0] != want[0] || hits[1] != want[1] || hits[2] != want[2] {
		t.Fatalf("hits = %v, want %v", hits, want)
	}
}

func TestAssetRipperUnityProjectFlow(t *testing.T) {
	var mu sync.Mutex
	var hits []string
	loadPath, exportPath := "", ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.Path)
		mu.Unlock()
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/LoadFolder":
			loadPath = r.FormValue("Path")
		case "/Export/UnityProject":
			exportPath = r.FormValue("Path")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cl := &AssetRipperClient{BaseURL: srv.URL, HTTP: srv.Client()}
	err := cl.ExportUnityProject(context.Background(), "D:/game/Game_Data", "D:/out/project")
	if err != nil {
		t.Fatalf("ExportUnityProject: %v", err)
	}
	if loadPath != "D:/game/Game_Data" {
		t.Fatalf("LoadFolder Path = %q", loadPath)
	}
	if exportPath != "D:/out/project" {
		t.Fatalf("Export Path = %q", exportPath)
	}
	want := []string{"/LoadFolder", "/Export/UnityProject", "/Reset"}
	if len(hits) != 3 || hits[0] != want[0] || hits[1] != want[1] || hits[2] != want[2] {
		t.Fatalf("hits = %v, want %v", hits, want)
	}
}

func TestAssetRipperNon200CapturesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	cl := &AssetRipperClient{BaseURL: srv.URL, HTTP: srv.Client()}
	err := cl.ExportPrimaryContent(context.Background(), "g", "o")
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !containsStr(err.Error(), "boom") {
		t.Fatalf("error should include body: %v", err)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
