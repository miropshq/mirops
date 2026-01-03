package mirror

import "context"

type ServiceMirrorI interface {
	MirrorService(ctx context.Context, name, sourceNS, targetNS string) error
	ListServices(ctx context.Context, namespace string) ([]string, error)
}
