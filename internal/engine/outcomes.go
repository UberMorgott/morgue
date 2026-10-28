package engine

import (
	"context"
	"errors"
	"fmt"
)

// These errors are returned only after the current run's summary was written.
// CLI callers can still deliver that summary for completed unsuccessful runs.
var (
	ErrTargetsFailed       = errors.New("pipeline has failed targets")
	ErrNoSuccessfulTargets = errors.New("nothing was decompiled")
)

func errorOutcome(err error) string {
	if err == nil {
		return OutcomeSuccess
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return OutcomeCancelled
	}
	return OutcomeError
}

func resultsError(results []TargetResult) error {
	var failures []error
	succeeded, skipped := 0, 0
	for _, result := range results {
		switch {
		case result.Error != nil:
			failures = append(failures, fmt.Errorf("%s: %w", result.Recon.Path, result.Error))
		case result.Skipped:
			skipped++
		default:
			succeeded++
		}
	}
	if len(failures) != 0 {
		return fmt.Errorf("%w (%d failed, %d succeeded): %w", ErrTargetsFailed, len(failures), succeeded, errors.Join(failures...))
	}
	if succeeded == 0 {
		return fmt.Errorf("%w (%d skipped)", ErrNoSuccessfulTargets, skipped)
	}
	return nil
}

func (em emitter) targetResult(result TargetResult) {
	outcome := errorOutcome(result.Error)
	message := "Complete"
	if result.Skipped {
		outcome, message = OutcomeSkipped, result.SkipReason
	}
	if result.Error != nil {
		message = result.Error.Error()
	}
	em.send(PipelineEvent{
		Phase: "target", Target: result.Recon.Path, TargetOutcome: outcome,
		Message: message, Error: result.Error, OutputPath: result.Output,
	})
}
