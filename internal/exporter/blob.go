package exporter

import (
	"context"
	"encoding/json"
	"fmt"

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

func (e *BlobExporter) Export(report *analysis.Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	ctx := context.Background()
	url := fmt.Sprintf("https://%s.blob.core.windows.net/", e.AccountName)

	var client *azblob.Client
	if e.ClientID != "" && e.ClientSecret != "" && e.TenantID != "" {
		cred, err := azidentity.NewClientSecretCredential(e.TenantID, e.ClientID, e.ClientSecret, nil)
		if err != nil {
			return fmt.Errorf("creating Azure SP credential: %w", err)
		}
		client, err = azblob.NewClient(url, cred, nil)
		if err != nil {
			return fmt.Errorf("creating Azure Blob client: %w", err)
		}
	} else {
		// Workload Identity / Managed Identity
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return fmt.Errorf("creating Azure default credential: %w", err)
		}
		client, err = azblob.NewClient(url, cred, nil)
		if err != nil {
			return fmt.Errorf("creating Azure Blob client: %w", err)
		}
	}

	_, err = client.UploadBuffer(ctx, e.ContainerName, e.BlobName, data, &azblob.UploadBufferOptions{
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: toPtr("application/json"),
		},
	})
	if err != nil {
		return fmt.Errorf("uploading report to blob %s/%s/%s: %w", e.AccountName, e.ContainerName, e.BlobName, err)
	}

	return nil
}

func (e *BlobExporter) Location() string {
	return fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s", e.AccountName, e.ContainerName, e.BlobName)
}

func toPtr[T any](v T) *T { return &v }
