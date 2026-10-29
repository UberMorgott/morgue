package quality

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

// Container measures installer/archive unpacking (nsis). The unpacked tree is
// a container product; code found inside is a child target with its own
// report, so code metrics here are not-applicable rather than 0/0.
type Container struct{}

func init() { Register("container", Container{}) }

// containerBookkeeping are files Morgue writes into every target output; they
// do not count as unpacked payload.
var containerBookkeeping = map[string]bool{
	"recon.json": true, QualityFile: true, ProvenanceFile: true, "index.md": true,
}

func (Container) Collect(ctx context.Context, in Input) (runmodel.QualityReport, error) {
	basis := "container/installer extraction: unpacked payload has no code inventory of its own; nested code targets carry their own quality reports"
	r := NotApplicableReport("container", basis)
	r.Checks = r.Checks[:0]
	var files int64
	err := filepath.WalkDir(in.Output, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			if d.Name() == ".morgue" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(in.Output, path)
		if !strings.Contains(filepath.ToSlash(rel), "/") && containerBookkeeping[strings.ToLower(d.Name())] {
			return nil
		}
		files++
		return nil
	})
	switch {
	case err != nil:
		r.Status = "unknown"
		r.Warnings = append(r.Warnings, "unpacked tree not readable: "+err.Error())
		r.Checks = append(r.Checks, runmodel.QualityCheck{ID: "output.present", Status: "unknown", Message: err.Error(), Evidence: []runmodel.Evidence{}})
	case files == 0:
		r.Status = "degraded"
		r.Checks = append(r.Checks, runmodel.QualityCheck{ID: "output.present", Status: "fail", Message: "container produced no unpacked files", Evidence: []runmodel.Evidence{}})
	default:
		r.Checks = append(r.Checks, runmodel.QualityCheck{ID: "output.present", Status: "pass",
			Message: fmt.Sprintf("%d unpacked files (container product, kind container/asset; not code coverage)", files), Evidence: []runmodel.Evidence{}})
	}
	r.Checks = append(r.Checks, runmodel.QualityCheck{ID: "artifact.fidelity", Status: "not-applicable", Message: basis, Evidence: []runmodel.Evidence{}})
	return r, nil
}
