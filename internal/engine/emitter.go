package engine

// emitter wraps the events channel with convenience methods.
type emitter struct {
	ch   chan<- PipelineEvent
	run  *Run
	opts Options
}

func (em emitter) emit(phase, target, msg string) {
	em.send(PipelineEvent{Phase: phase, Target: target, Message: msg})
}

func (em emitter) emitErr(phase, target string, err error) {
	em.send(PipelineEvent{Phase: phase, Target: target, Error: err})
}

// emitWarn emits a WARN-severity event. Unlike emitErr it does NOT set the Error
// field (so it is not counted or rendered as a failure); the human-readable text
// goes in Message and Severity marks it as a warning for renderers. Used for
// non-fatal noise — a missing OPTIONAL tool, or benign Windows cert-store output
// — that must be visible but must not read as a pipeline error.
func (em emitter) emitWarn(phase, target, msg string) {
	em.send(PipelineEvent{Phase: phase, Target: target, Message: msg, Severity: "warn"})
}

func (em emitter) send(ev PipelineEvent) {
	if ev.Error != nil {
		ev.ErrorMsg = ev.Error.Error()
	}
	if em.run != nil {
		em.run.consume(em.opts, ev)
		em.run.mu.Lock()
		persistErr := em.run.persistenceErr
		eventErr := em.run.eventErr
		em.run.mu.Unlock()
		if persistErr != nil && !ev.Done {
			return
		}
		if ev.Done && eventErr != nil {
			ev.Error = eventErr
			ev.ErrorMsg = eventErr.Error()
			ev.RunOutcome = errorOutcome(eventErr)
			ev.Phase = ev.RunOutcome
			if persistErr != nil {
				ev.RunOutcome = OutcomeError
				ev.Phase = OutcomeError
			}
		}
	}
	if em.ch != nil {
		em.ch <- ev
	}
}
