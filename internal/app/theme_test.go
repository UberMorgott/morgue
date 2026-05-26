package app

import (
	"testing"
)

func TestCyberpunkThemeColors(t *testing.T) {
	th := CyberpunkTheme()

	tests := []struct {
		name  string
		color string
	}{
		{"BG", th.BG},
		{"FG", th.FG},
		{"Accent", th.Accent},
		{"Accent2", th.Accent2},
		{"Err", th.Err},
		{"Warn", th.Warn},
		{"Dim", th.Dim},
		{"Running", th.Running},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.color == "" {
				t.Errorf("Theme.%s is empty", tt.name)
			}
		})
	}
}

func TestStyleMethodsNoPanic(t *testing.T) {
	th := CyberpunkTheme()

	// These should not panic and should return non-empty rendered strings
	funcs := []struct {
		name string
		fn   func() string
	}{
		{"Title", func() string { return th.Title().Render("test") }},
		{"Subtitle", func() string { return th.Subtitle().Render("test") }},
		{"StatusBar", func() string { return th.StatusBar().Render("test") }},
		{"ProgressDone", func() string { return th.ProgressDone().Render("test") }},
		{"ProgressTodo", func() string { return th.ProgressTodo().Render("test") }},
		{"Dimmed", func() string { return th.Dimmed().Render("test") }},
		{"ErrorText", func() string { return th.ErrorText().Render("test") }},
		{"WarningText", func() string { return th.WarningText().Render("test") }},
		{"SuccessText", func() string { return th.SuccessText().Render("test") }},
	}

	for _, tt := range funcs {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn()
			if result == "" {
				t.Errorf("%s() returned empty string", tt.name)
			}
		})
	}
}
