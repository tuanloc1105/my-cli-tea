package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type reportTempFile interface {
	io.Writer
	Name() string
	Chmod(os.FileMode) error
	Sync() error
	Close() error
}

type reportFileDependencies struct {
	lstat      func(string) (os.FileInfo, error)
	createTemp func(string, string) (reportTempFile, error)
	rename     func(string, string) error
	remove     func(string) error
}

func defaultReportFileDependencies() reportFileDependencies {
	return reportFileDependencies{
		lstat: os.Lstat,
		createTemp: func(dir, pattern string) (reportTempFile, error) {
			return os.CreateTemp(dir, pattern)
		},
		rename: replaceReportFile,
		remove: os.Remove,
	}
}

func writeReportFile(path string, data []byte) error {
	return writeReportFileWithDependencies(path, data, defaultReportFileDependencies())
}

func writeReportFileWithDependencies(path string, data []byte, deps reportFileDependencies) (resultErr error) {
	mode := os.FileMode(0o644)
	info, err := deps.lstat(path)
	if err == nil && info.Mode().IsRegular() {
		mode = info.Mode().Perm()
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect report destination: %w", err)
	}

	directory := filepath.Dir(path)
	temp, err := deps.createTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary report: %w", err)
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			if err := deps.remove(tempPath); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("remove temporary report: %w", err))
			}
		}
	}()

	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set report permissions: %w", err)
	}
	if err := writeAll(temp, data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary report: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync temporary report: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary report: %w", err)
	}
	if err := deps.rename(tempPath, path); err != nil {
		return fmt.Errorf("replace report destination: %w", err)
	}
	cleanup = false
	return nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(data) {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
