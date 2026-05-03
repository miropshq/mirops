package collector

import (
	"context"
)

// systemNamespaces are excluded when scope mode is "application"
var systemNamespaces = map[string]bool{
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
}

// Scope controls which namespaces are included in the analysis.
type Scope struct {
	// Mode is "all" or "application"
	Mode string
	// ExcludeNamespaces is an optional list of namespaces to skip
	ExcludeNamespaces []string
}

type ClusterCollector interface {
	Collect(ctx context.Context, scope Scope) (*ClusterSnapshot, error)
}
