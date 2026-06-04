// Package ops provides a small Operation/Saga primitive for compose-then-
// rollback sequences (firewall installs, exit-node rule sets, etc.). The
// contract is: each Op declares Do and Undo; Run executes Do in order; if
// any Do returns an error OR the context is cancelled, Undo runs on
// completed Ops in reverse and the first error propagates (with all undo
// errors joined).
package ops

import (
	"context"
	"errors"
	"fmt"
)

type Op struct {
	Name string
	Do   func(ctx context.Context) error
	Undo func(ctx context.Context) error
}

func noUndo(context.Context) error { return nil }

// Noop returns an Op with no-op Undo, used when a step is trivially
// reversible by the next step (e.g. setting a struct field).
func Noop(name string, do func(ctx context.Context) error) Op {
	return Op{Name: name, Do: do, Undo: noUndo}
}

func Run(ctx context.Context, steps []Op) error {
	done := make([]Op, 0, len(steps))
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return rollback(ctx, done, fmt.Errorf("context cancelled before %s: %w", step.Name, err))
		}
		if err := step.Do(ctx); err != nil {
			return rollback(ctx, done, fmt.Errorf("%s: %w", step.Name, err))
		}
		if err := ctx.Err(); err != nil {
			return rollback(ctx, append(done, step), fmt.Errorf("context cancelled after %s: %w", step.Name, err))
		}
		done = append(done, step)
	}
	return nil
}

func rollback(ctx context.Context, done []Op, cause error) error {
	rollbackCtx := context.WithoutCancel(ctx)
	var undoErrs []error
	for i := len(done) - 1; i >= 0; i-- {
		if err := done[i].Undo(rollbackCtx); err != nil {
			undoErrs = append(undoErrs, fmt.Errorf("undo %s: %w", done[i].Name, err))
		}
	}
	if len(undoErrs) > 0 {
		return errors.Join(append([]error{cause}, undoErrs...)...)
	}
	return cause
}
