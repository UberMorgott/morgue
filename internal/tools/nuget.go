package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// fetchNuGetLatestVersion returns the latest version of a NuGet package.
func fetchNuGetLatestVersion(ctx context.Context, packageID string) (string, error) {
	url := fmt.Sprintf("https://api.nuget.org/v3-flatcontainer/%s/index.json", strings.ToLower(packageID))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpDo(ctx, client, http.MethodGet, url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nuget API returned %d for %s", resp.StatusCode, packageID)
	}

	var result struct {
		Versions []string `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	latest := latestStableVersion(result.Versions)
	if latest == "" {
		return "", fmt.Errorf("no stable versions found for %s", packageID)
	}
	return latest, nil
}

// latestStableVersion returns the highest non-prerelease version (no SemVer
// "-" suffix) by numeric component compare, ignoring list order. "" if none.
func latestStableVersion(versions []string) string {
	best := ""
	for _, v := range versions {
		core, _, _ := strings.Cut(v, "+")
		if core == "" || strings.Contains(core, "-") {
			continue
		}
		if best == "" || compareNumericVersion(core, best) > 0 {
			best = v
		}
	}
	return best
}

// compareNumericVersion compares dotted versions component-wise (numeric when
// both parse, else lexical); missing components count as 0.
func compareNumericVersion(a, b string) int {
	a, _, _ = strings.Cut(a, "+")
	b, _, _ = strings.Cut(b, "+")
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		x, y := "0", "0"
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		switch {
		case ex == nil && ey == nil:
			if nx != ny {
				return cmp.Compare(nx, ny)
			}
		case x != y:
			return strings.Compare(x, y)
		}
	}
	return 0
}
