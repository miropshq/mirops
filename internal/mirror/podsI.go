package mirror

import "context"

type PodMirrorI interface {
	MirrorPod(ctx context.Context, name, sourceNS, targetNS string) error
	ListPods(ctx context.Context, namespace string) ([]string, error)
}
