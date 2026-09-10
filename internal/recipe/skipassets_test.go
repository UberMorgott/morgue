package recipe

import (
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

func TestContextSkipAssets(t *testing.T) {
	on := &config.Config{UnityExtractAssets: true}
	off := &config.Config{UnityExtractAssets: false}

	tests := []struct {
		name string
		ctx  Context
		want bool
	}{
		{"default config extracts", Context{Config: on}, false},
		{"config toggle off", Context{Config: off}, true},
		{"code-only flag", Context{Config: on, CodeOnly: true}, true},
		{"both off", Context{Config: off, CodeOnly: true}, true},
		{"nil config extracts", Context{}, false},
		{"nil config + code-only", Context{CodeOnly: true}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ctx.SkipAssets(); got != tt.want {
				t.Errorf("SkipAssets() = %v, want %v", got, tt.want)
			}
		})
	}
}
