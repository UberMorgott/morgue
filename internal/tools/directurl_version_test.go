package tools

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

// TestDirectURLRecordsAndComparesDate: an unversioned direct download (strings)
// records the asset's Last-Modified date as its version, and the bulk check
// flags an update once upstream publishes a newer asset.
func TestDirectURLRecordsAndComparesDate(t *testing.T) {
	lastModified := "Tue, 04 Mar 2025 10:00:00 GMT"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", lastModified)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte("MZ"))
	}))
	defer srv.Close()
	onlyTestTool(t, ToolDef{Name: "test-dated", Method: MethodDirectURL, URL: srv.URL + "/dated.exe", Binary: "dated.exe"})

	m := NewManager(t.TempDir(), config.Config{})
	ver, err := m.Install(t.Context(), "test-dated", nil)
	if err != nil || ver != "2025.03.04" {
		t.Fatalf("Install = (%q, %v), want 2025.03.04", ver, err)
	}

	st := m.CheckAllWithUpdates()[0]
	if st.Version != "2025.03.04" || st.LatestVersion != "2025.03.04" || st.UpdateAvailable {
		t.Fatalf("same asset: %+v", st)
	}

	lastModified = "Wed, 01 Oct 2025 10:00:00 GMT"
	st = m.CheckAllWithUpdates()[0]
	if st.LatestVersion != "2025.10.01" || !st.UpdateAvailable {
		t.Fatalf("newer asset not offered: %+v", st)
	}
	if latest, upd := m.CheckLatestVersionSingle("test-dated"); latest != "2025.10.01" || !upd {
		t.Fatalf("single check = (%q, %v)", latest, upd)
	}
}
