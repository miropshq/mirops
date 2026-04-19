package collector

import (
	"context"
)

type ClusterCollectorI interface {
	Collect(ctx context.Context) (*ClusterSnapshot, error)
}
