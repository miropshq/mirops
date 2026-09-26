package exporter

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrNotFound is returned (wrapped) by an Exporter's Read when the report object doesn't exist at the
// destination yet — typically a poll that arrives before the export finishes. The reports server maps
// it to a transient 404 (keep polling) rather than a 502, so it isn't logged as a failure.
var ErrNotFound = errors.New("report not found at destination")

// Exporter writes a report to a destination and reads it back. It is report-agnostic: the
// UpgradeAnalysis report and the ClusterMirror report go through the same destinations.
type Exporter interface {
	// Write stores the report's bytes (JSON) at the destination, replacing any previous version.
	Write(data []byte) error
	// Read fetches the previously exported report bytes from the destination. The reports HTTP
	// server uses it to serve remote reports (s3/blob/pvc) on demand, so the operator never keeps
	// a local replica in the pod and the read stays inside the operator's credentials.
	Read() ([]byte, error)
	// Location returns the destination path or URL for the status field.
	Location() string
}

// Export marshals a report as indented JSON and writes it through e.
func Export(e Exporter, report any) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}
	return e.Write(data)
}
