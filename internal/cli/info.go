package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/recon"
)

// InfoOptions holds options for the info command.
type InfoOptions struct {
	Target string
	Format string // "json" or "text"
}

// infoResult is the JSON-serializable output of the info command.
type infoResult struct {
	Path               string   `json:"path"`
	Size               int64    `json:"size"`
	SHA256             string   `json:"sha256"`
	Kind               string   `json:"kind"`
	Runtime            string   `json:"runtime,omitempty"`
	Compiler           string   `json:"compiler,omitempty"`
	Obfuscator         string   `json:"obfuscator,omitempty"`
	ObfuscatorFeatures []string `json:"obfuscator_features,omitempty"`
	Packed             bool     `json:"packed,omitempty"`
	EmbeddedSuspected  bool     `json:"embedded_suspected,omitempty"`
	EmbeddedSignals    []string `json:"embedded_signals,omitempty"`
	Recipe             string   `json:"recipe"`
}

// buildInfoResult runs recon.Classify and recipe.Match, returning a serializable result.
func buildInfoResult(ctx context.Context, path string) (infoResult, error) {
	r, err := recon.Classify(ctx, path)
	if err != nil {
		return infoResult{}, fmt.Errorf("classify: %w", err)
	}

	recipeName := ""
	if rec := recipe.Match(&r); rec != nil {
		recipeName = rec.Name()
	}

	return infoResult{
		Path:               r.Path,
		Size:               r.Size,
		SHA256:             r.SHA256,
		Kind:               r.Kind.String(),
		Runtime:            r.Runtime,
		Compiler:           r.Compiler,
		Obfuscator:         r.Obfuscator,
		ObfuscatorFeatures: r.ObfuscatorFeatures,
		Packed:             r.Packed,
		EmbeddedSuspected:  r.EmbeddedSuspected,
		EmbeddedSignals:    r.EmbeddedSignals,
		Recipe:             recipeName,
	}, nil
}

// Info runs recon on a single file and prints the result.
func Info(opts InfoOptions) error {
	if _, err := os.Stat(opts.Target); err != nil {
		return fmt.Errorf("file not found: %s", opts.Target)
	}

	result, err := buildInfoResult(context.Background(), opts.Target)
	if err != nil {
		return err
	}

	if opts.Format == "text" {
		return printInfoText(result)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func printInfoText(r infoResult) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Path:\t%s\n", r.Path)
	fmt.Fprintf(w, "Size:\t%d\n", r.Size)
	fmt.Fprintf(w, "SHA256:\t%s\n", r.SHA256)
	fmt.Fprintf(w, "Kind:\t%s\n", r.Kind)
	if r.Runtime != "" {
		fmt.Fprintf(w, "Runtime:\t%s\n", r.Runtime)
	}
	if r.Compiler != "" {
		fmt.Fprintf(w, "Compiler:\t%s\n", r.Compiler)
	}
	if r.Obfuscator != "" {
		fmt.Fprintf(w, "Obfuscator:\t%s\n", r.Obfuscator)
	}
	if r.Packed {
		fmt.Fprintf(w, "Packed:\tyes\n")
	}
	if r.EmbeddedSuspected {
		fmt.Fprintf(w, "Embedded:\tsuspected\n")
	}
	fmt.Fprintf(w, "Recipe:\t%s\n", r.Recipe)
	return w.Flush()
}
