package scanner

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// SizeMode selects the metric accumulated for each item.
type SizeMode string

const (
	SizeModeLogical   SizeMode = "logical"
	SizeModeAllocated SizeMode = "allocated"
)

func (mode SizeMode) Valid() bool {
	return mode == SizeModeLogical || mode == SizeModeAllocated
}

func ParseSizeMode(value string) (SizeMode, error) {
	mode := SizeMode(value)
	if !mode.Valid() {
		return "", &ScanError{
			Kind: ErrorKindInvalidOptions,
			Err:  fmt.Errorf("invalid size mode %q", value),
		}
	}
	return mode, nil
}

// ItemType describes the filesystem entry represented by an item.
type ItemType string

const (
	ItemTypeFile      ItemType = "file"
	ItemTypeDirectory ItemType = "directory"
	ItemTypeSymlink   ItemType = "symlink"
	ItemTypeOther     ItemType = "other"
)

type ScanOptions struct {
	ShowProgress   bool
	ExcludeList    []string
	Ctx            context.Context
	MaxDepth       int // 0 = unlimited
	ProgressWriter io.Writer
	SizeMode       SizeMode
}

type ItemInfo struct {
	Name string   `json:"name"`
	Size int64    `json:"size"`
	Type ItemType `json:"type"`
}

type ScanStatus string

const (
	ScanStatusComplete ScanStatus = "complete"
	ScanStatusPartial  ScanStatus = "partial"
	ScanStatusFailed   ScanStatus = "failed"
)

type ScanResult struct {
	Items          []ItemInfo
	WarningCount   int64
	WarningSummary string
	Status         ScanStatus
}

func newScanResult() ScanResult {
	return ScanResult{Items: []ItemInfo{}, Status: ScanStatusComplete}
}

type ErrorKind string

const (
	ErrorKindInvalidOptions ErrorKind = "invalid_options"
	ErrorKindUnsupported    ErrorKind = "unsupported"
	ErrorKindRootOpen       ErrorKind = "root_open"
	ErrorKindCancelled      ErrorKind = "cancelled"
)

// ScanError classifies scanner failures without discarding their cause.
type ScanError struct {
	Kind ErrorKind
	Path string
	Err  error
}

func (err *ScanError) Error() string {
	if err.Path != "" {
		return fmt.Sprintf("%s %s: %v", err.Kind, err.Path, err.Err)
	}
	return fmt.Sprintf("%s: %v", err.Kind, err.Err)
}

func (err *ScanError) Unwrap() error {
	return err.Err
}

func ErrorKindOf(err error) (ErrorKind, bool) {
	var scanErr *ScanError
	if !errors.As(err, &scanErr) {
		return "", false
	}
	return scanErr.Kind, true
}
