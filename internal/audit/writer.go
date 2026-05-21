package audit

// Writer combines a Log (disk) and a Broker (SSE fan-out) into a single
// call site. Handlers call writer.OK / writer.Err and both disk persistence
// and live streaming happen automatically.
type Writer struct {
	log    *Log
	broker *Broker
}

// NewWriter wraps log and broker. Either may be nil (the respective path is
// skipped). Passing nil for both is valid but produces a no-op writer.
func NewWriter(log *Log, broker *Broker) *Writer {
	return &Writer{log: log, broker: broker}
}

// OK records a successful operation.
func (w *Writer) OK(operation, target string, params map[string]any) {
	e := Entry{Operation: operation, Target: target, Params: params, Result: "ok"}
	if w.log != nil {
		w.log.Append(e)
	}
	if w.broker != nil {
		w.broker.Publish(e)
	}
}

// Err records a failed operation.
func (w *Writer) Err(operation, target string, params map[string]any, errMsg string) {
	e := Entry{Operation: operation, Target: target, Params: params, Result: "error", Error: errMsg}
	if w.log != nil {
		w.log.Append(e)
	}
	if w.broker != nil {
		w.broker.Publish(e)
	}
}
