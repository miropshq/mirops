package mirror

import "context"

type ConfigMapMirrorI interface {
	MirrorConfigMap(ctx context.Context, name, sourceNS, targetNS string) error
	ListConfigMaps(ctx context.Context, namespace string) ([]string, error)
}
