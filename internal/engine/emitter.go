package engine

// emitter wraps the events channel with convenience methods.
type emitter struct {
	ch chan<- PipelineEvent
}

func (em emitter) emit(phase, target, msg string) {
	if em.ch == nil {
		return
	}
	em.ch <- PipelineEvent{Phase: phase, Target: target, Message: msg}
}

func (em emitter) emitErr(phase, target string, err error) {
	if em.ch == nil {
		return
	}
	em.ch <- PipelineEvent{Phase: phase, Target: target, Error: err}
}

// emitWarn emits a WARN-severity event. Unlike emitErr it does NOT set the Error
// field (so it is not counted or rendered as a failure); the human-readable text
// goes in Message and Severity marks it as a warning for renderers. Used for
// non-fatal noise — a missing OPTIONAL tool, or benign Windows cert-store output
// — that must be visible but must not read as a pipeline error.
func (em emitter) emitWarn(phase, target, msg string) {
	if em.ch == nil {
		return
	}
	em.ch <- PipelineEvent{Phase: phase, Target: target, Message: msg, Severity: "warn"}
}

func (em emitter) send(ev PipelineEvent) {
	if em.ch == nil {
		return
	}
	em.ch <- ev
}
