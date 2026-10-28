package tools

import "testing"

func TestLatestStableVersion(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{[]string{"9.1.0.7988", "11.0.0.9375", "10.0.0.8330", "12.0.0.1-preview1"}, "11.0.0.9375"},
		{[]string{"1.10.0", "1.9.0", "1.2.0"}, "1.10.0"},
		{[]string{"2.0.0-rc.1", "1.0.0+build5", "1.0.0"}, "1.0.0+build5"},
		{[]string{"1.0", "1.0.1"}, "1.0.1"},
		{[]string{"1.0.0-beta"}, ""},
		{nil, ""},
	} {
		if got := latestStableVersion(tc.in); got != tc.want {
			t.Errorf("latestStableVersion(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
