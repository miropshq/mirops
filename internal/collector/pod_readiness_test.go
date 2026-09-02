package collector

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// A Succeeded pod (e.g. a finished Job) completed cleanly, so it must not be counted as
// not-ready — that would wrongly drag Health down. A Failed pod is a real problem and must
// count, like any other not-ready pod.
func TestCollectExcludesSucceededPodsFromNotReady(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme, appsv1.AddToScheme, batchv1.AddToScheme,
		policyv1.AddToScheme, networkingv1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}

	pod := func(name string, phase corev1.PodPhase, ready bool) *corev1.Pod {
		st := corev1.ConditionFalse
		if ready {
			st = corev1.ConditionTrue
		}
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name},
			Status: corev1.PodStatus{
				Phase:      phase,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: st}},
			},
		}
	}
	objects := []runtime.Object{
		pod("running-ready", corev1.PodRunning, true), // healthy, counts in Total, not not-ready
		pod("succeeded", corev1.PodSucceeded, false),  // completed OK -> must be skipped entirely
		pod("failed", corev1.PodFailed, false),        // failed -> must count as not-ready
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()
	disc := fakeclientset.NewSimpleClientset().Discovery().(*fakediscovery.FakeDiscovery)
	disc.FakedServerVersion = &version.Info{GitVersion: "v1.34.0"}

	c := &DefaultClusterCollector{Client: cl, DiscoveryClient: disc}
	snap, err := c.Collect(context.Background(), Scope{})
	if err != nil {
		t.Fatal(err)
	}

	if snap.TotalPods != 2 {
		t.Errorf("TotalPods = %d, want 2 (succeeded pod excluded)", snap.TotalPods)
	}
	if snap.NotReadyPods != 1 {
		t.Errorf("NotReadyPods = %d, want 1 (only the failed pod)", snap.NotReadyPods)
	}
	if len(snap.PodIssues) != 1 || snap.PodIssues[0].Name != "failed" {
		t.Errorf("PodIssues = %+v, want only the 'failed' pod", snap.PodIssues)
	}
}
