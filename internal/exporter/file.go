package exporter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FileExporter writes the report to a local JSON file (the operator's reports dir, or a mounted PVC).
// mirops-cli reads it via --source flag.
type FileExporter struct {
	Path string
}

func NewFileExporter(path string) *FileExporter {
	if path == "" {
		path = "/tmp/mirops-report.mirops"
	}
	return &FileExporter{Path: path}
}

// Write goes through a temp file and a rename, so the reports server — which Headlamp polls and which
// serves this file directly — never hands out a half-written report.
func (e *FileExporter) Write(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(e.Path), 0o750); err != nil {
		return fmt.Errorf("creating report directory: %w", err)
	}
	tmp := e.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing report file: %w", err)
	}
	if err := os.Rename(tmp, e.Path); err != nil {
		return fmt.Errorf("publishing report file: %w", err)
	}
	return nil
}

func (e *FileExporter) Read() ([]byte, error) {
	data, err := os.ReadFile(e.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("report file %s not written yet: %w", e.Path, ErrNotFound)
		}
		return nil, fmt.Errorf("reading report file %s: %w", e.Path, err)
	}
	return data, nil
}

func (e *FileExporter) Location() string {
	return e.Path
}
