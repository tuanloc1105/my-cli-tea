package replacer

import (
	"errors"
	"fmt"
)

const DefaultMaxInputSize int64 = 512 * 1024 * 1024

var (
	ErrConcurrentModification = errors.New("source changed during replacement")
	ErrPostCommitDurability   = errors.New("replacement committed but directory sync failed")
)

// Options defines one replacement run. Search must not be empty, limits are in
// bytes, and a zero MaxOutputSize means unlimited.
type Options struct {
	Target        string
	Search        []byte
	Replacement   []byte
	Backup        bool
	DryRun        bool
	MaxInputSize  int64
	MaxOutputSize int64
	Workers       int
}

type OutcomeKind string

const (
	OutcomeModified    OutcomeKind = "modified"
	OutcomeWouldModify OutcomeKind = "would-modify"
	OutcomeNoMatch     OutcomeKind = "no-match"
	OutcomeSkipped     OutcomeKind = "skipped"
	OutcomeFailed      OutcomeKind = "failed"
)

type SkipReason string

const (
	SkipBackupFile     SkipReason = "backup-file"
	SkipBinaryNUL      SkipReason = "binary-nul"
	SkipHardlink       SkipReason = "hardlink"
	SkipInputTooLarge  SkipReason = "input-too-large"
	SkipInvalidUTF8    SkipReason = "invalid-utf8"
	SkipNonRegular     SkipReason = "non-regular"
	SkipOutputTooLarge SkipReason = "output-too-large"
	SkipSymlink        SkipReason = "symlink"
)

var skipReasonOrder = []SkipReason{
	SkipBackupFile,
	SkipBinaryNUL,
	SkipHardlink,
	SkipInputTooLarge,
	SkipInvalidUTF8,
	SkipNonRegular,
	SkipOutputTooLarge,
	SkipSymlink,
}

// SkipReasons returns the stable display order for policy skip counters.
func SkipReasons() []SkipReason {
	return append([]SkipReason(nil), skipReasonOrder...)
}

// Outcome is the single result produced for one encountered filesystem entry.
// Detail is intended for a human-facing no-match or skip explanation.
type Outcome struct {
	Path              string
	Kind              OutcomeKind
	Reason            SkipReason
	Detail            string
	Replacements      int64
	BackupPath        string
	TargetIsDirectory bool
	Err               error
}

func (o Outcome) validate() error {
	if o.Path == "" {
		return errors.New("outcome path is empty")
	}

	switch o.Kind {
	case OutcomeModified, OutcomeWouldModify:
		if o.Replacements <= 0 {
			return fmt.Errorf("%s outcome has no replacements", o.Kind)
		}
		if o.Reason != "" || o.Err != nil {
			return fmt.Errorf("%s outcome has a skip reason or error", o.Kind)
		}
	case OutcomeNoMatch:
		if o.Replacements != 0 || o.Reason != "" || o.Err != nil {
			return errors.New("no-match outcome has replacements, a skip reason, or an error")
		}
	case OutcomeSkipped:
		if o.Reason == "" {
			return errors.New("skipped outcome has no reason")
		}
		if o.Replacements != 0 || o.Err != nil {
			return errors.New("skipped outcome has replacements or an error")
		}
	case OutcomeFailed:
		if o.Err == nil {
			return errors.New("failed outcome has no error")
		}
		if o.Reason != "" || o.Replacements != 0 {
			return errors.New("failed outcome has a skip reason or replacements")
		}
	default:
		return fmt.Errorf("unknown outcome kind %q", o.Kind)
	}

	return nil
}

// Reporter is called serially by Run's coordinator.
type Reporter interface {
	Report(Outcome)
}

type ReporterFunc func(Outcome)

func (f ReporterFunc) Report(outcome Outcome) {
	if f != nil {
		f(outcome)
	}
}

type Summary struct {
	Scanned           int64
	Modified          int64
	WouldModify       int64
	Replacements      int64
	NoMatch           int64
	Skipped           map[SkipReason]int64
	Failed            int64
	TargetIsDirectory bool
}

func (s *Summary) record(outcome Outcome) error {
	if err := outcome.validate(); err != nil {
		return err
	}

	s.Scanned++
	s.Replacements += outcome.Replacements
	switch outcome.Kind {
	case OutcomeModified:
		s.Modified++
	case OutcomeWouldModify:
		s.WouldModify++
	case OutcomeNoMatch:
		s.NoMatch++
	case OutcomeSkipped:
		if s.Skipped == nil {
			s.Skipped = make(map[SkipReason]int64)
		}
		s.Skipped[outcome.Reason]++
	case OutcomeFailed:
		s.Failed++
	}
	return nil
}

type PathError struct {
	Path string
	Err  error
}

func (e PathError) Error() string {
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

func (e PathError) Unwrap() error {
	return e.Err
}

// PartialError reports operational failures after all independent entries have
// been processed.
type PartialError struct {
	Total    int64
	Failures []PathError
}

func (e *PartialError) Error() string {
	if e == nil || e.Total == 0 {
		return ""
	}
	if e.Total == 1 && len(e.Failures) == 1 {
		return fmt.Sprintf("1 path failed: %v", e.Failures[0])
	}
	if len(e.Failures) == 0 {
		return fmt.Sprintf("%d paths failed", e.Total)
	}
	return fmt.Sprintf("%d paths failed; first: %v", e.Total, e.Failures[0])
}

func (e *PartialError) Unwrap() []error {
	if e == nil {
		return nil
	}
	errs := make([]error, 0, len(e.Failures))
	for i := range e.Failures {
		errs = append(errs, e.Failures[i])
	}
	return errs
}
