package controller

import (
	"context"
	"fmt"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	miropsv1 "github.com/miropshq/mirops/api/v1"
	"github.com/miropshq/mirops/internal/exporter"
)

// Report extensions. A report's name on the reports server is "<CR name><ext>", and so is its object
// name at a remote destination unless the user sets one. They also keep the two kinds apart: a mirror
// and an analysis that share a name never overwrite each other, and `*.mirops` / `*.mirror` filter
// reports from config files sharing a bucket, container or PVC. The content is JSON either way.
const (
	reportExt       = ".mirops" // UpgradeAnalysis
	mirrorReportExt = ".mirror" // ClusterMirror
)

// reportState values recorded on the status of an UpgradeAnalysis or a ClusterMirror.
const (
	reportStateWritten = "written"
	reportStateFailed  = "failed"
)

// isRemote reports whether a destination keeps the report outside the pod (no local replica: the
// reports server reads it back on demand).
func isRemote(src miropsv1.SourceConfig) bool {
	switch src.Type {
	case miropsv1.SourceTypeS3, miropsv1.SourceTypeBlob, miropsv1.SourceTypePVC:
		return true
	default:
		return false
	}
}

// localReportName is the report's file name in the operator's reports dir (source.type file, the
// default). The user may set source.path — only its basename is used, the directory is fixed — else it
// is "<name><ext>".
func localReportName(name string, src miropsv1.SourceConfig, ext string) string {
	if src.Type == miropsv1.SourceTypeFile && src.Path != "" {
		if base := filepath.Base(src.Path); base != "." && base != ".." && base != string(filepath.Separator) {
			return base
		}
	}
	return name + ext
}

// reportObjectName resolves a remote report's object/blob name. The user owns the name AND the
// extension: whatever they set in blobName/key is used verbatim. Only when it's unset does mirops fall
// back to "<name><ext>". It's used inside buildExporter, so a controller's write and the reports
// server's read-back always agree.
func reportObjectName(configured, name, ext string) string {
	if configured != "" {
		return configured
	}
	return name + ext
}

// buildExporter constructs the Exporter for a report's configured destination — an UpgradeAnalysis's or
// a ClusterMirror's spec.source. It is a package function (not a method) so the controllers and the
// reports HTTP server build the same exporter to write and to read back a report. When
// credentialsSecret is set, credentials come from that Secret in the operator's namespace.
func buildExporter(ctx context.Context, c client.Client, operatorNamespace, name string, src miropsv1.SourceConfig, ext string) (exporter.Exporter, error) {
	var secretData map[string][]byte
	if src.CredentialsSecret != "" {
		secret := &corev1.Secret{}
		if err := c.Get(ctx, types.NamespacedName{
			Name:      src.CredentialsSecret,
			Namespace: operatorNamespace,
		}, secret); err != nil {
			return nil, fmt.Errorf("reading credentials secret %q: %w", src.CredentialsSecret, err)
		}
		secretData = secret.Data
	}

	switch src.Type {
	case miropsv1.SourceTypeS3:
		return &exporter.S3Exporter{
			Bucket:          src.Bucket,
			Region:          src.Region,
			Key:             reportObjectName(src.Key, name, ext),
			AccessKeyID:     string(secretData["AWS_ACCESS_KEY_ID"]),
			SecretAccessKey: string(secretData["AWS_SECRET_ACCESS_KEY"]),
		}, nil
	case miropsv1.SourceTypeBlob:
		return &exporter.BlobExporter{
			AccountName:   src.AccountName,
			ContainerName: src.ContainerName,
			BlobName:      reportObjectName(src.BlobName, name, ext),
			ClientID:      string(secretData["AZURE_CLIENT_ID"]),
			ClientSecret:  string(secretData["AZURE_CLIENT_SECRET"]),
			TenantID:      string(secretData["AZURE_TENANT_ID"]),
		}, nil
	case miropsv1.SourceTypePVC:
		// A PVC destination is a filesystem write to a volume the Helm chart mounts. src.Path is the
		// mount directory; the report lands as <name><ext> so reports don't collide and stay distinct
		// from any config files sharing the volume.
		dir := src.Path
		if dir == "" {
			dir = "/mnt/mirops-reports"
		}
		return exporter.NewFileExporter(filepath.Join(dir, reportObjectName("", name, ext))), nil
	default:
		return exporter.NewFileExporter(src.Path), nil
	}
}
