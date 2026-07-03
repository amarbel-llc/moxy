// Package spoolctx threads an async job's clown output-spool path (RFC-0010)
// from the dispatch layer down to the native exec layer through the call
// context. It exists as its own package so neither side has to import the
// other: internal/asyncjob sets the path after resolving it via
// `ringmaster spool-path`, and internal/native reads it in runMoxinProcess to
// tee the child's output into the spool. An empty/absent value means "no
// spool" (clown disabled or absent), and the tee is skipped.
package spoolctx

import "context"

type pathKey struct{}

// WithPath returns ctx carrying the spool file path. An empty path is a no-op
// (PathFromContext will report no spool).
func WithPath(ctx context.Context, path string) context.Context {
	if path == "" {
		return ctx
	}
	return context.WithValue(ctx, pathKey{}, path)
}

// PathFromContext returns the spool path set by WithPath, or "" if none.
func PathFromContext(ctx context.Context) string {
	p, _ := ctx.Value(pathKey{}).(string)
	return p
}
