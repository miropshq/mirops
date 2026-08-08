package exporter

import "github.com/miropshq/mirops/internal/analysis"

// Exporter writes an analysis report to a destination and reads it back.
type Exporter interface {
	Export(report *analysis.Report) error
	// Read fetches the previously exported report bytes from the destination. The reports HTTP
	// server uses it to serve remote reports (s3/blob/pvc) on demand, so the operator never keeps
	// a local replica in the pod and the read stays inside the operator's credentials.
	Read() ([]byte, error)
	// Location returns the destination path or URL for the status field.
	Location() string
}
