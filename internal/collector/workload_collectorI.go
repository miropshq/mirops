package collector

import (
	"context"
)

type WorkloadCollector interface {
	Collect(ctx context.Context) ([]ResourceRef, error)
}
