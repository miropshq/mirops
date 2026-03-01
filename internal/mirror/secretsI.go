package mirror

import "context"

type SecretMirrorI interface {
	MirrorSecret(ctx context.Context, name, sourceNS, targetNS string) error
	ListSecrets(ctx context.Context, namespace string) ([]string, error)
}
