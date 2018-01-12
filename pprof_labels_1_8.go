// +build !go1.9

package stackimpact

import (
	"context"
	"runtime/pprof"
)

func WithPprofLabel(key string, val string, ctx context.Context, fn func()) {
	fn()
}
