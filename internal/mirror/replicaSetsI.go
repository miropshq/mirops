package mirror

import "context"

type ReplicaSetMirrorI interface {
	MirrorReplicaSet(ctx context.Context, name, sourceNS, targetNS string) error
	ListReplicaSets(ctx context.Context, namespace string) ([]string, error)
}
