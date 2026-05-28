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

func (em emitter) send(ev PipelineEvent) {
	if em.ch == nil {
		return
	}
	em.ch <- ev
}
