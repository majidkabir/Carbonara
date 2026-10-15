package calc

import (
	"context"
	"errors"
	"time"
)

// ErrTemporary marks a failure that is not the input's fault and may not
// happen again: a routing service that didn't answer, was overloaded or
// restarting. Calculating the shipment again later can succeed. Providers
// wrap their errors with it (errors.Is).
var ErrTemporary = errors.New("temporary failure, try again")

// Temporary says whether err is worth trying again.
func Temporary(err error) bool { return errors.Is(err, ErrTemporary) }

// DefaultRetryWaits are the pauses before CalculateRetrying tries a
// shipment again.
var DefaultRetryWaits = []time.Duration{time.Second, 3 * time.Second}

// CalculateRetrying is Calculate, trying again after a pause when it fails
// for a temporary reason (Calculator.RetryWaits, default
// DefaultRetryWaits). Other failures return at once: the same input gives
// the same error.
func (c Calculator) CalculateRetrying(ctx context.Context, s Shipment) (Result, error) {
	waits := c.RetryWaits
	if waits == nil {
		waits = DefaultRetryWaits
	}
	r, err := c.Calculate(ctx, s)
	for _, w := range waits {
		if !Temporary(err) {
			break
		}
		t := time.NewTimer(w)
		select {
		case <-ctx.Done():
			t.Stop()
			return r, err
		case <-t.C:
		}
		r, err = c.Calculate(ctx, s)
	}
	return r, err
}
