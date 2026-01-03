package mirror

import "context"

type DeploymentMirrorI interface {
	MirrorDeployment(ctx context.Context, name, sourceNS, targetNS string) error
	ListDeployments(ctx context.Context, namespace string) ([]string, error)
}
