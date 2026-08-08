package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/miropshq/mirops/internal/analysis"
)

// BlobExporter uploads the analysis report to an Azure Blob Storage container.
// Credentials are resolved in order:
//  1. Service Principal from clientID/clientSecret/tenantID (fallback secret)
//  2. Workload Identity / Managed Identity (automatic when empty)
type BlobExporter struct {
	AccountName   string
	ContainerName string
	BlobName      string
	ClientID      string
	ClientSecret  string
	TenantID      string
}

// client builds an Azure Blob client: a Service Principal when clientID/secret/tenant are provided,
// otherwise the default chain (Workload Identity / Managed Identity).
func (e *BlobExporter) client() (*azblob.Client, error) {
	url := fmt.Sprintf("https://%s.blob.core.windows.net/", e.AccountName)
	if e.ClientID != "" && e.ClientSecret != "" && e.TenantID != "" {
		cred, err := azidentity.NewClientSecretCredential(e.TenantID, e.ClientID, e.ClientSecret, nil)
		if err != nil {
			return nil, fmt.Errorf("creating Azure SP credential: %w", err)
		}
		return azblob.NewClient(url, cred, nil)
	}
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure default credential: %w", err)
	}
	return azblob.NewClient(url, cred, nil)
}

func (e *BlobExporter) Export(report *analysis.Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	client, err := e.client()
	if err != nil {
		return err
	}

	_, err = client.UploadBuffer(context.Background(), e.ContainerName, e.BlobName, data, &azblob.UploadBufferOptions{
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: toPtr("application/json"),
		},
	})
	if err != nil {
		return fmt.Errorf("uploading report to blob %s/%s/%s: %w", e.AccountName, e.ContainerName, e.BlobName, err)
	}

	return nil
}

func (e *BlobExporter) Read() ([]byte, error) {
	client, err := e.client()
	if err != nil {
		return nil, err
	}

	resp, err := client.DownloadStream(context.Background(), e.ContainerName, e.BlobName, nil)
	if err != nil {
		return nil, fmt.Errorf("reading report from blob %s/%s/%s: %w", e.AccountName, e.ContainerName, e.BlobName, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

func (e *BlobExporter) Location() string {
	return fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s", e.AccountName, e.ContainerName, e.BlobName)
}

func toPtr[T any](v T) *T { return &v }
