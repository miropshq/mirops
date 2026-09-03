package collector

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCollectJobsIncludesActiveAndTerminalFailures(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	jobs := []batchv1.Job{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "active"}, Status: batchv1.JobStatus{Active: 1}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "exhausted"}, Status: batchv1.JobStatus{
			Failed: 3,
			Conditions: []batchv1.JobCondition{{
				Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded",
			}},
		}},
		// A failed attempt is not terminal while the Job controller may still retry it.
		{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "retrying"}, Status: batchv1.JobStatus{Failed: 1}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ignored", Name: "excluded"}, Status: batchv1.JobStatus{Active: 1}},
	}
	objects := make([]runtime.Object, 0, len(jobs))
	for i := range jobs {
		objects = append(objects, &jobs[i])
	}
	c := &DefaultClusterCollector{Client: fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()}
	snapshot := &ClusterSnapshot{}

	if err := c.collectJobs(context.Background(), map[string]bool{"ignored": true}, snapshot, nil); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.JobIssues) != 2 {
		t.Fatalf("job issue count = %d, want 2", len(snapshot.JobIssues))
	}

	issues := make(map[string]JobIssue, len(snapshot.JobIssues))
	for _, issue := range snapshot.JobIssues {
		issues[issue.Name] = issue
	}
	if got := issues["active"].Status; got != JobStatusActive {
		t.Errorf("active job status = %q, want Active", got)
	}
	failed := issues["exhausted"]
	if failed.Status != JobStatusFailed || failed.Reason != "BackoffLimitExceeded" {
		t.Errorf("failed job = status %q reason %q", failed.Status, failed.Reason)
	}
}

func TestJobStatusDoesNotTreatRetryAsTerminalFailure(t *testing.T) {
	retrying := &batchv1.Job{Status: batchv1.JobStatus{Failed: 1}}
	if got := jobStatus(retrying); got != JobStatusPending {
		t.Errorf("retrying job status = %q, want Pending", got)
	}
}
