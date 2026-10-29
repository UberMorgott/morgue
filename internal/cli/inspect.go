package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/workspace"
)

// DefaultInspectLimit bounds target rows printed per call.
const DefaultInspectLimit = 50

// InspectOptions configures `morgue inspect OUTPUT`.
type InspectOptions struct {
	Root    string
	JSON    bool
	Rebuild bool // --rebuild-manifest: exclusive lock, prior snapshot preserved
	Offset  int
	Limit   int
	Out     io.Writer

	// Read and RebuildManifest default to the workspace package readers.
	Read            func(root string) (workspace.Recovery, error)
	RebuildManifest func(root string) (workspace.Recovery, error)
}

// InspectReport is the --json shape: status + paged target list.
type InspectReport struct {
	Root         string                  `json:"root"`
	Status       string                  `json:"status"` // running|finished|interrupted|corrupt
	RunID        string                  `json:"runId"`
	Outcome      *string                 `json:"outcome"`
	LastSeq      uint64                  `json:"lastSeq"`
	TornTail     bool                    `json:"tornTail"`
	Rebuilt      bool                    `json:"rebuilt"`
	Error        string                  `json:"error,omitempty"`
	Diagnostics  []string                `json:"diagnostics"`
	Targets      runmodel.Counts         `json:"targets"`
	LeafTargets  runmodel.Counts         `json:"leafTargets"`
	Warnings     []string                `json:"warnings"`
	TargetsTotal int                     `json:"targetsTotal"`
	Offset       int                     `json:"offset"`
	Limit        int                     `json:"limit"`
	HasMore      bool                    `json:"hasMore"`
	TargetPage   []runmodel.TargetRecord `json:"targetPage"`
	// Quality summarizes each TargetPage row (same order): status, key
	// metrics and the code fidelity of its recorded artifacts.
	Quality   []TargetQuality     `json:"quality"`
	Workspace *runmodel.Workspace `json:"-"`
}

// TargetQuality is the compact per-target quality view. Null metrics were not
// measured; Status "not-collected" means the target has no quality report.
type TargetQuality struct {
	ID               string         `json:"id"`
	Status           string         `json:"status"`
	Family           string         `json:"family"`
	FunctionsRatio   *float64       `json:"functionsRatio"`
	TypesRatio       *float64       `json:"typesRatio"`
	DecompilerErrors *int64         `json:"decompilerErrors"`
	NamesAutoRatio   *float64       `json:"namesAutoRatio"`
	Fidelity         map[string]int `json:"fidelity"`
}

// codeFidelity lists the artifact kinds that describe recovered code, in
// display order (most to least faithful).
var codeFidelity = []string{runmodel.FidelitySource, runmodel.FidelityDecompiled, runmodel.FidelityPseudocode, runmodel.FidelityStub}

func targetQuality(t runmodel.TargetRecord, arts []runmodel.Artifact) TargetQuality {
	tq := TargetQuality{ID: t.ID, Status: "not-collected", Fidelity: map[string]int{}}
	for _, a := range arts {
		if a.TargetID != nil && *a.TargetID == t.ID && slices.Contains(codeFidelity, a.Kind) {
			tq.Fidelity[a.Kind]++
		}
	}
	q := t.Quality
	if q == nil {
		return tq
	}
	tq.Status, tq.Family = q.Status, q.Family
	tq.FunctionsRatio, tq.TypesRatio, tq.NamesAutoRatio = q.Functions.Ratio, q.Types.Ratio, q.Names.AutoRatio
	if q.DecompilerErrors.Status == "measured" || q.DecompilerErrors.Status == "estimated" {
		tq.DecompilerErrors = q.DecompilerErrors.Value
	}
	return tq
}

func qualityLine(q TargetQuality) string {
	parts := []string{q.Status}
	if q.Family != "" {
		parts[0] += " (" + q.Family + ")"
	}
	pct := func(label string, v *float64) {
		if v != nil {
			parts = append(parts, fmt.Sprintf("%s %.1f%%", label, *v*100))
		}
	}
	pct("functions", q.FunctionsRatio)
	pct("types", q.TypesRatio)
	if q.DecompilerErrors != nil {
		parts = append(parts, fmt.Sprintf("decompiler errors %d", *q.DecompilerErrors))
	}
	pct("auto names", q.NamesAutoRatio)
	var fid []string
	for _, k := range codeFidelity {
		if n := q.Fidelity[k]; n > 0 {
			fid = append(fid, fmt.Sprintf("%s x%d", k, n))
		}
	}
	if len(fid) == 0 {
		fid = []string{"none"}
	}
	parts = append(parts, "fidelity "+strings.Join(fid, ", "))
	return strings.Join(parts, " | ")
}

// ErrWorkspaceCorrupt marks a journal that could only be read up to a prefix.
var ErrWorkspaceCorrupt = errors.New("workspace journal is corrupt")

// Inspect reads a local workspace without modifying it (unless Rebuild) and
// reports interruption/corruption. Corruption returns an error after the
// readable prefix has been printed.
func Inspect(opts InspectOptions) error {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Limit <= 0 {
		opts.Limit = DefaultInspectLimit
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	if opts.Root == "" {
		return errors.New("inspect: OUTPUT path required")
	}
	if fi, err := os.Stat(opts.Root); err != nil || !fi.IsDir() {
		return fmt.Errorf("inspect: %s is not a directory", opts.Root)
	}
	if opts.Read == nil {
		opts.Read = workspace.Read
	}
	if opts.RebuildManifest == nil {
		opts.RebuildManifest = workspace.RebuildManifest
	}
	read := opts.Read
	if opts.Rebuild {
		read = opts.RebuildManifest
	}
	if read == nil {
		return errors.New("inspect: workspace reader unavailable")
	}
	rec, rerr := read(opts.Root)
	rep := buildInspectReport(opts, rec, rerr)
	if opts.JSON {
		enc := json.NewEncoder(opts.Out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	} else {
		printInspectText(opts.Out, rep)
	}
	if rerr != nil {
		return fmt.Errorf("%w: %v", ErrWorkspaceCorrupt, rerr)
	}
	return nil
}

func buildInspectReport(opts InspectOptions, rec workspace.Recovery, rerr error) InspectReport {
	ws := rec.Workspace
	rep := InspectReport{
		Root: opts.Root, RunID: ws.Run.ID, Outcome: ws.Run.Outcome, LastSeq: rec.LastSeq,
		TornTail: rec.TornTail, Rebuilt: opts.Rebuild && rerr == nil,
		Diagnostics: nonNil(rec.Diagnostics), Warnings: nonNil(ws.Warnings),
		Targets: ws.Run.Targets, LeafTargets: ws.Run.LeafTargets,
		TargetsTotal: len(ws.Targets), Offset: opts.Offset, Limit: opts.Limit,
		TargetPage: []runmodel.TargetRecord{}, Quality: []TargetQuality{}, Workspace: &ws,
	}
	switch {
	case rerr != nil:
		rep.Status, rep.Error = "corrupt", rerr.Error()
	case rec.Active:
		rep.Status = "running"
	case rec.Interrupted:
		rep.Status = "interrupted"
	default:
		rep.Status = "finished"
	}
	if opts.Offset < len(ws.Targets) {
		end := min(opts.Offset+opts.Limit, len(ws.Targets))
		rep.TargetPage = ws.Targets[opts.Offset:end]
		rep.HasMore = end < len(ws.Targets)
		for _, t := range rep.TargetPage {
			rep.Quality = append(rep.Quality, targetQuality(t, ws.Artifacts))
		}
	}
	return rep
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func printInspectText(w io.Writer, r InspectReport) {
	outcome := "none"
	if r.Outcome != nil {
		outcome = *r.Outcome
	}
	fmt.Fprintf(w, "workspace: %s\n", r.Root)
	fmt.Fprintf(w, "status: %s\n", r.Status)
	fmt.Fprintf(w, "run: %s outcome: %s\n", nonEmptyStr(r.RunID, "unknown"), outcome)
	fmt.Fprintf(w, "journal: committed through seq %d", r.LastSeq)
	if r.TornTail {
		fmt.Fprint(w, " (torn final line ignored)")
	}
	fmt.Fprintln(w)
	if r.Rebuilt {
		fmt.Fprintln(w, "manifest: rebuilt from journal (prior snapshot preserved)")
	}
	switch r.Status {
	case "interrupted":
		fmt.Fprintln(w, "note: run has no terminal event and no live lock; outputs may be partial. Start a fresh run to redo it.")
	case "running":
		fmt.Fprintln(w, "note: a Morgue process holds this workspace; data shown is the committed prefix so far.")
	case "corrupt":
		fmt.Fprintf(w, "error: %s\n", r.Error)
	}
	fmt.Fprintf(w, "targets: %s\n", countsLine(r.Targets))
	fmt.Fprintf(w, "leaf targets: %s\n", countsLine(r.LeafTargets))
	for _, d := range r.Diagnostics {
		fmt.Fprintf(w, "diagnostic: %s\n", oneLine(d))
	}
	for _, x := range r.Warnings {
		fmt.Fprintf(w, "warning: %s\n", oneLine(x))
	}
	if r.TargetsTotal == 0 {
		fmt.Fprintln(w, "no targets recorded")
		return
	}
	end := r.Offset + len(r.TargetPage)
	fmt.Fprintf(w, "targets %d-%d of %d (id | outcome | recipe | input -> index):\n", min(r.Offset+1, end), end, r.TargetsTotal)
	for i, t := range r.TargetPage {
		state := t.State
		if t.Outcome != nil {
			state = *t.Outcome
		}
		idx := "-"
		if t.Output != "" {
			idx = path.Join(t.Output, workspace.IndexPath)
		}
		fmt.Fprintf(w, "  %s | %s | %s | %s -> %s\n", t.ID, state, nonEmptyStr(t.Recipe, "-"), oneLine(t.RelativeInput), idx)
		if t.Error != "" {
			fmt.Fprintf(w, "    error: %s\n", oneLine(t.Error))
		} else if t.SkipReason != "" {
			fmt.Fprintf(w, "    skipped: %s\n", oneLine(t.SkipReason))
		}
		if i < len(r.Quality) {
			fmt.Fprintf(w, "    quality: %s\n", oneLine(qualityLine(r.Quality[i])))
		}
	}
	if r.HasMore {
		fmt.Fprintf(w, "more: rerun with --offset %d\n", end)
	}
	fmt.Fprintf(w, "read next: %s, %s\n", workspace.AgentsPath, workspace.IndexPath)
}

func countsLine(c runmodel.Counts) string {
	return fmt.Sprintf("%d total, %d success, %d error, %d cancelled, %d skipped, %d running, %d pending",
		c.Total, c.Success, c.Error, c.Cancelled, c.Skipped, c.Running, c.Pending)
}

// oneLine bounds untrusted text to one terminal line of at most 300 runes.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if rs := []rune(s); len(rs) > 300 {
		s = string(rs[:300]) + "…"
	}
	return s
}

// NewInspectCmd builds `morgue inspect OUTPUT [--json] [--rebuild-manifest]
// [--offset N] [--limit N]` using the given readers (nil = workspace package).
func NewInspectCmd(read, rebuild func(string) (workspace.Recovery, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect OUTPUT",
		Short: "Read a run workspace (status, interruption, corruption, targets) without modifying it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := InspectOptions{Root: args[0], Out: cmd.OutOrStdout(), Read: read, RebuildManifest: rebuild}
			opts.JSON, _ = cmd.Flags().GetBool("json")
			opts.Rebuild, _ = cmd.Flags().GetBool("rebuild-manifest")
			opts.Offset, _ = cmd.Flags().GetInt("offset")
			opts.Limit, _ = cmd.Flags().GetInt("limit")
			return Inspect(opts)
		},
	}
	cmd.Flags().Bool("json", false, "print a machine-readable report")
	cmd.Flags().Bool("rebuild-manifest", false, "rebuild morgue.json from the journal (needs exclusive lock; prior snapshot kept)")
	cmd.Flags().Int("offset", 0, "first target row to print")
	cmd.Flags().Int("limit", DefaultInspectLimit, "maximum target rows to print")
	return cmd
}

func nonEmptyStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
