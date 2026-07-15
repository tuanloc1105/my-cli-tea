package replacer

import (
	"errors"
	"reflect"
	"testing"
)

func TestSummaryRecord(t *testing.T) {
	wantSkipped := map[SkipReason]int64{SkipSymlink: 1}
	outcomes := []Outcome{
		{Path: "modified", Kind: OutcomeModified, Replacements: 2},
		{Path: "dry-run", Kind: OutcomeWouldModify, Replacements: 3},
		{Path: "no-match", Kind: OutcomeNoMatch},
		{Path: "symlink", Kind: OutcomeSkipped, Reason: SkipSymlink},
		{Path: "failed", Kind: OutcomeFailed, Err: errors.New("boom")},
	}

	var got Summary
	for _, outcome := range outcomes {
		if err := got.record(outcome); err != nil {
			t.Fatalf("record(%+v): %v", outcome, err)
		}
	}

	if got.Scanned != 5 || got.Modified != 1 || got.WouldModify != 1 || got.Replacements != 5 || got.NoMatch != 1 || got.Failed != 1 {
		t.Fatalf("unexpected summary: %+v", got)
	}
	if !reflect.DeepEqual(got.Skipped, wantSkipped) {
		t.Fatalf("Skipped = %#v, want %#v", got.Skipped, wantSkipped)
	}
}

func TestOutcomeValidateRejectsInvalidCombinations(t *testing.T) {
	tests := []Outcome{
		{},
		{Path: "file", Kind: OutcomeModified},
		{Path: "file", Kind: OutcomeNoMatch, Err: errors.New("boom")},
		{Path: "file", Kind: OutcomeSkipped},
		{Path: "file", Kind: OutcomeFailed},
	}

	for _, outcome := range tests {
		if err := outcome.validate(); err == nil {
			t.Fatalf("validate(%+v) succeeded, want error", outcome)
		}
	}
}

func TestPartialErrorUnwrapsPathFailures(t *testing.T) {
	errOne := errors.New("one")
	errTwo := errors.New("two")
	err := &PartialError{Total: 2, Failures: []PathError{{Path: "a", Err: errOne}, {Path: "b", Err: errTwo}}}

	if !errors.Is(err, errOne) || !errors.Is(err, errTwo) {
		t.Fatalf("PartialError does not unwrap all failures: %v", err)
	}
}

func TestSkipReasonsReturnsCopy(t *testing.T) {
	first := SkipReasons()
	first[0] = SkipSymlink
	second := SkipReasons()
	if second[0] != SkipBackupFile {
		t.Fatalf("SkipReasons returned shared storage: %#v", second)
	}
}
