/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	miropsv1 "github.com/miropshq/mirops/api/v1"
)

// EnsureDefaultClusterMirror creates the default ClusterMirror so the mirror runs out of the box,
// without anyone applying a CR by hand. It is bootstrap-only: if a ClusterMirror with that name
// already exists it is left untouched, so kubectl edits survive operator restarts and upgrades.
//
// specJSON is the ClusterMirrorSpec as JSON (the Helm chart renders mirror.default.spec with
// toJson); empty means the CRD defaults (scope all, refresh every 5m).
//
// It runs before the manager starts (the CRD was just installed and the manager cache isn't
// running yet), so c must be a direct, uncached client. With several replicas each one races to
// create it; the losers get AlreadyExists, which is not an error.
func EnsureDefaultClusterMirror(ctx context.Context, c client.Client, name, specJSON string) error {
	var spec miropsv1.ClusterMirrorSpec
	if specJSON != "" {
		if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
			return fmt.Errorf("parsing default ClusterMirror spec: %w", err)
		}
	}

	cm := &miropsv1.ClusterMirror{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       spec,
	}
	if err := c.Create(ctx, cm); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating default ClusterMirror %q: %w", name, err)
	}
	return nil
}
