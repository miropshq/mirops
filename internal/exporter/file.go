package exporter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/miropshq/mirops/internal/analysis"
)

// FileExporter writes the analysis report to a local JSON file.
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

func (e *FileExporter) Export(report *analysis.Report) error {
	if err := os.MkdirAll(filepath.Dir(e.Path), 0o750); err != nil {
		return fmt.Errorf("creating report directory: %w", err)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	if err := os.WriteFile(e.Path, data, 0o600); err != nil {
		return fmt.Errorf("writing report file: %w", err)
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
