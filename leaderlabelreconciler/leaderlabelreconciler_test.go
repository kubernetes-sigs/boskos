/*
Copyright 2020 The Kubernetes Authors.

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

package leaderlabelreconciler

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcile(t *testing.T) {
	const leaderLabelKey = "leader"
	testCases := []struct {
		name              string
		pod               *corev1.Pod
		expectedPodLabels map[string]string
	}{
		{
			name: "Label is added",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name: "leader",
			}},
			expectedPodLabels: map[string]string{leaderLabelKey: "true"},
		},
		{
			name: "Label is removed",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name:   "not-leader",
				Labels: map[string]string{leaderLabelKey: "true"}},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			client := fakectrlruntimeclient.NewFakeClient(tc.pod)

			r := &reconciler{client: client, leaderLabelKey: leaderLabelKey, leaderPodName: "leader"}
			if _, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: tc.pod.Name}}); err != nil {
				t.Fatalf("reconciliation failed: %v", err)
			}

			var pod corev1.Pod
			if err := client.Get(context.Background(), types.NamespacedName{Name: tc.pod.Name}, &pod); err != nil {
				t.Fatalf("failed to get pod: %v", err)
			}

			if diff := cmp.Diff(pod.Labels, tc.expectedPodLabels); diff != "" {
				t.Errorf("actual labels differ from expected labels: %s", diff)
			}
		})
	}
}
