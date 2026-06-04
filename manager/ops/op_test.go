package ops

import (
	"context"
	"errors"
	"testing"
)

func TestRunHappyPath(t *testing.T) {
	var seen []string
	err := Run(context.Background(), []Op{
		{Name: "a", Do: func(context.Context) error { seen = append(seen, "do-a"); return nil }, Undo: noUndo},
		{Name: "b", Do: func(context.Context) error { seen = append(seen, "do-b"); return nil }, Undo: noUndo},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := seen; len(got) != 2 || got[0] != "do-a" || got[1] != "do-b" {
		t.Fatalf("seen=%v", got)
	}
}

func TestRunFailureRollsBackInReverse(t *testing.T) {
	var seen []string
	boom := errors.New("boom")
	err := Run(context.Background(), []Op{
		{Name: "a", Do: func(context.Context) error { seen = append(seen, "do-a"); return nil },
			Undo: func(context.Context) error { seen = append(seen, "undo-a"); return nil }},
		{Name: "b", Do: func(context.Context) error { seen = append(seen, "do-b"); return nil },
			Undo: func(context.Context) error { seen = append(seen, "undo-b"); return nil }},
		{Name: "c", Do: func(context.Context) error { return boom }, Undo: noUndo},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	want := []string{"do-a", "do-b", "undo-b", "undo-a"}
	if len(seen) != len(want) {
		t.Fatalf("seen=%v want %v", seen, want)
	}
	for i, w := range want {
		if seen[i] != w {
			t.Fatalf("seen=%v want %v", seen, want)
		}
	}
}

func TestRunCancelledIsFailureNotSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, []Op{
		{Name: "a", Do: func(ctx context.Context) error { return ctx.Err() }, Undo: noUndo},
	})
	if err == nil {
		t.Fatal("Run must report failure on cancelled context")
	}
}

func TestRunUndoErrorsJoinedWithCause(t *testing.T) {
	cause := errors.New("step-c failed")
	undoErr := errors.New("undo-a failed")
	err := Run(context.Background(), []Op{
		{Name: "a", Do: func(context.Context) error { return nil }, Undo: func(context.Context) error { return undoErr }},
		{Name: "c", Do: func(context.Context) error { return cause }, Undo: noUndo},
	})
	if !errors.Is(err, cause) {
		t.Fatalf("err must wrap cause; got %v", err)
	}
	if !errors.Is(err, undoErr) {
		t.Fatalf("err must also wrap undo error; got %v", err)
	}
}

func TestRunUndoRunsEvenIfCtxCancelled(t *testing.T) {
	// Cancellation after the second Do should still undo the first step.
	ctx, cancel := context.WithCancel(context.Background())
	var seen []string
	err := Run(ctx, []Op{
		{Name: "a", Do: func(context.Context) error { seen = append(seen, "do-a"); return nil },
			Undo: func(context.Context) error { seen = append(seen, "undo-a"); return nil }},
		{Name: "b", Do: func(context.Context) error {
			seen = append(seen, "do-b")
			cancel()
			return nil
		}, Undo: func(context.Context) error { seen = append(seen, "undo-b"); return nil }},
		{Name: "c", Do: func(context.Context) error { seen = append(seen, "do-c"); return nil }, Undo: noUndo},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	// "do-c" must NOT appear; undo-b and undo-a MUST appear in reverse.
	for _, s := range seen {
		if s == "do-c" {
			t.Fatalf("step c must not have run after cancel; seen=%v", seen)
		}
	}
	if got := lastTwo(seen); got[0] != "undo-b" || got[1] != "undo-a" {
		t.Fatalf("undo order wrong; seen=%v", seen)
	}
}

func lastTwo(s []string) [2]string {
	n := len(s)
	return [2]string{s[n-2], s[n-1]}
}
