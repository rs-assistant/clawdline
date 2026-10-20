package connectionhealth

import "context"

// carried is the one thing a request's context hands from its handler to the
// answer it publishes: the tracker and the logical correlation. Nothing else
// rides along, so a payload, channel or sender cannot reach a phase this way.
type carried struct {
	t    *Tracker
	corr string
}

type carriedKey struct{}

// WithCorrelation is ctx carrying the correlation of the handler that is
// answering, so the answer's sealing and spooling continue the same logical
// operation instead of minting their own. A nil tracker (monitoring off) or an
// invalid correlation answers ctx itself: nothing is allocated and nothing
// downstream records a phase.
func WithCorrelation(ctx context.Context, t *Tracker, correlation string) context.Context {
	if t == nil || !ValidID(correlation) {
		return ctx
	}
	return context.WithValue(ctx, carriedKey{}, &carried{t: t, corr: correlation})
}

// CarriedCorrelation is the correlation WithCorrelation put on ctx, or "".
func CarriedCorrelation(ctx context.Context) string {
	if c, ok := ctx.Value(carriedKey{}).(*carried); ok {
		return c.corr
	}
	return ""
}

// StartCarried begins a finite transport-class daemon phase under the
// correlation ctx carries. A ctx carrying none gives the zero Handle, which
// records nothing: an untraced publisher keeps its exact behaviour.
func StartCarried(ctx context.Context, stage Stage, location string) Handle {
	c, ok := ctx.Value(carriedKey{}).(*carried)
	if !ok {
		return Handle{}
	}
	return c.t.Start(DaemonSpec(stage, ClassTransport, location, c.corr))
}
