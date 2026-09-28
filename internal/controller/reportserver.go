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
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	miropsv1 "github.com/miropshq/mirops/api/v1"
	"github.com/miropshq/mirops/internal/exporter"
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
// file name (e.g. "prod.mirops"). Every report is "<name>.mirops", so ?kind= says whose it is:
// "ClusterMirror" for a mirror's; absent (or "UpgradeAnalysis") for an analysis's.
func (s *ReportServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.URL.Path)
	if name == "." || name == "/" || name == "" {
		http.Error(w, "report name required", http.StatusBadRequest)
		return
	}
	crName := strings.TrimSuffix(strings.TrimSuffix(name, reportExt), ".json")
	mirror := strings.EqualFold(r.URL.Query().Get("kind"), "ClusterMirror")

	// Find the (cluster-scoped) resource the report belongs to — a ClusterMirror or an UpgradeAnalysis,
	// as the request says — so a mirror and an analysis never answer for each other. A ".json" suffix is
	// tolerated so a client that hasn't been updated still resolves to the right analysis.
	ctx := r.Context()
	log := logf.FromContext(ctx)

	var src miropsv1.SourceConfig
	if mirror {
		cm := &miropsv1.ClusterMirror{}
		if err := s.Client.Get(ctx, client.ObjectKey{Name: crName}, cm); err != nil {
			http.Error(w, "report not found", http.StatusNotFound)
			return
		}
		src = cm.Spec.Source
	} else {
		ua := &miropsv1.UpgradeAnalysis{}
		if err := s.Client.Get(ctx, client.ObjectKey{Name: crName}, ua); err != nil {
			http.Error(w, "report not found", http.StatusNotFound)
			return
		}
		src = ua.Spec.Source
	}

	// File destination: the local copy on disk. Missing means it isn't written yet (e.g. the first
	// rebuild after a restart); a plain 404 tells the poller to retry.
	if !isRemote(src) {
		data, err := os.ReadFile(filepath.Join(s.ReportsDir, localReportName(crName, src)))
		if err != nil {
			http.Error(w, "report not available yet", http.StatusNotFound)
			return
		}
		writeJSONBytes(w, http.StatusOK, data)
		return
	}

	// Remote destination (s3/blob/pvc): read the report back from its spec.source.
	exp, err := buildExporter(ctx, s.Client, s.OperatorNamespace, crName, src)
	if err != nil {
		s.writeReadError(w, log, crName, "", err)
		return
	}
	data, err := exp.Read()
	if err != nil {
		// The report isn't at the destination yet — a poll that beat the export. This is expected
		// while an analysis is in flight, so return a plain 404 (the UI keeps polling) and don't log
		// it as a failure. Real connection/auth errors still surface as 502 + ERROR below.
		if errors.Is(err, exporter.ErrNotFound) {
			log.V(1).Info("report not written yet", "report", crName, "location", exp.Location())
			http.Error(w, "report not available yet", http.StatusNotFound)
			return
		}
		s.writeReadError(w, log, crName, exp.Location(), err)
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
