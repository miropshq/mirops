package crd

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// crdFS holds the operator's own CustomResourceDefinitions, embedded from the generated manifests.
// `make manifests` regenerates config/crd/bases and copies the files here; they are git-ignored and
// baked into the binary at build time (same pattern as the compatibility matrix). A given operator
// image therefore always ships the exact CRDs that match its types — no separate chart download and
// no version skew: choosing an image tag chooses its CRDs.
//
//go:embed *.yaml
var crdFS embed.FS

const fieldManager = "mirops-operator"

// Install applies every embedded CRD to the cluster via server-side apply, then waits until each is
// Established. The operator installs its own CRDs at startup — before the manager's informers begin
// watching the custom types — using a direct client built from cfg (the manager's cached client
// isn't ready pre-start). It is idempotent: it creates a missing CRD and updates a changed one, so
// upgrading the operator image upgrades the CRDs to match it.
func Install(ctx context.Context, cfg *rest.Config) error {
	s := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(s); err != nil {
		return fmt.Errorf("add apiextensions to scheme: %w", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: s})
	if err != nil {
		return fmt.Errorf("build client: %w", err)
	}

	files, err := fs.Glob(crdFS, "*.yaml")
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no embedded CRDs found — run `make manifests` before building")
	}

	// Apply every CRD (upgradeanalyses, remediationplans, ...) first, then wait for all of them.
	names := make([]string, 0, len(files))
	for _, f := range files {
		data, err := crdFS.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := yaml.Unmarshal(data, crd); err != nil {
			return fmt.Errorf("decode %s: %w", f, err)
		}
		// Server-side apply needs the GVK on the wire and no borrowed managedFields.
		crd.SetGroupVersionKind(apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"))
		crd.SetManagedFields(nil)
		if err := c.Patch(ctx, crd, client.Apply,
			client.FieldOwner(fieldManager), client.ForceOwnership); err != nil {
			return fmt.Errorf("apply CRD %s: %w", crd.Name, err)
		}
		names = append(names, crd.Name)
	}

	// Wait until every applied CRD is Established (the API server serves the type), so the manager's
	// informers don't hit "resource not found" retries on a fresh install.
	if err := waitEstablished(ctx, c, names); err != nil {
		return fmt.Errorf("waiting for CRDs to be established: %w", err)
	}
	return nil
}

// waitEstablished polls until every named CRD reports the Established condition, or the timeout hits.
func waitEstablished(ctx context.Context, c client.Client, names []string) error {
	return wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 30*time.Second, true,
		func(ctx context.Context) (bool, error) {
			for _, n := range names {
				crd := &apiextensionsv1.CustomResourceDefinition{}
				if err := c.Get(ctx, client.ObjectKey{Name: n}, crd); err != nil {
					return false, nil // not visible yet — keep polling
				}
				if !established(crd) {
					return false, nil
				}
			}
			return true, nil
		})
}

// established reports whether the CRD's Established condition is True (the API serves the type).
func established(crd *apiextensionsv1.CustomResourceDefinition) bool {
	for _, cond := range crd.Status.Conditions {
		if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
			return true
		}
	}
	return false
}
