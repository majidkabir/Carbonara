package calc

import "context"

type pathKey struct{}

// WithPath asks routers to also return each leg's path, for drawing it on
// a map. Meant for single calculations: paths cost router time and
// response size, so batches don't ask for them.
func WithPath(ctx context.Context) context.Context { return context.WithValue(ctx, pathKey{}, true) }

// WantPath says whether the calculation asked for paths.
func WantPath(ctx context.Context) bool { v, _ := ctx.Value(pathKey{}).(bool); return v }
