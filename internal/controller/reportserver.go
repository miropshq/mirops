/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	miropsv1 "github.com/miropshq/mirops/api/v1"
)

// ReportServer serves report JSON over HTTP. A file (default) destination is served from the local
// reports dir on disk. A remote destination (s3/blob/pvc) keeps no local replica in the pod, so the
// report is read back from its destination on demand using the operator's own credentials — the
// connection never leaves the operator (e.g. the pod's IRSA role for S3). When the destination
// can't be reached, the handler returns 502 with a JSON error the UI can display.
type ReportServer struct {
	Client            client.Client
	ReportsDir        string
	OperatorNamespace string
}

// ServeHTTP is mounted behind http.StripPrefix("/reports/", …), so r.URL.Path is the bare report
// file name (e.g. "mirops-test.json").
func (s *ReportServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.URL.Path)
	if name == "." || name == "/" || name == "" {
		http.Error(w, "report name required", http.StatusBadRequest)
		return
	}

	// File destination: serve the local copy from disk if it's there.
	if data, err := os.ReadFile(filepath.Join(s.ReportsDir, name)); err == nil {
		writeJSONBytes(w, http.StatusOK, data)
		return
	}

	// Remote destination: look up the (cluster-scoped) UpgradeAnalysis and read the report back
	// from s3/blob/pvc. The report file name is "<ua.Name>.json" for remote destinations.
	ctx := r.Context()
	log := logf.FromContext(ctx)
	uaName := strings.TrimSuffix(name, ".json")

	ua := &miropsv1.UpgradeAnalysis{}
	if err := s.Client.Get(ctx, client.ObjectKey{Name: uaName}, ua); err != nil {
		http.Error(w, "report not found", http.StatusNotFound)
		return
	}

	exp, err := buildExporter(ctx, s.Client, s.OperatorNamespace, ua)
	if err != nil {
		s.writeReadError(w, log, uaName, "", err)
		return
	}
	data, err := exp.Read()
	if err != nil {
		s.writeReadError(w, log, uaName, exp.Location(), err)
		return
	}
	writeJSONBytes(w, http.StatusOK, data)
}

func (s *ReportServer) writeReadError(w http.ResponseWriter, log logr.Logger, name, location string, err error) {
	log.Error(err, "failed to read report from remote storage", "report", name, "location", location)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":    "cannot read report from remote storage",
		"location": location,
		"detail":   err.Error(),
	})
}

func writeJSONBytes(w http.ResponseWriter, status int, data []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
