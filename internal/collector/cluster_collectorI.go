package collector

import (
	"context"
)

type ClusterCollector interface {
	Collect(ctx context.Context) (*ClusterSnapshot, error)
}
