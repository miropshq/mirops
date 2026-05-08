package exporter

import "github.com/miropshq/mirops/internal/analysis"

// Exporter writes an analysis report to a destination.
type Exporter interface {
	Export(report *analysis.Report) error
	// Location returns the destination path or URL for the status field.
	Location() string
}
