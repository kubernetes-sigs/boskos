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
	"fmt"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func AddToManager(mgr manager.Manager, podLabelSelector labels.Selector, leaderLabelKey string, leaderPodName string) error {
	return builder.ControllerManagedBy(mgr).
		For(
			&corev1.Pod{},
			builder.WithPredicates(predicate.NewPredicateFuncs(func(o ctrlruntimeclient.Object) bool {
				return podLabelSelector.Matches(labels.Set(o.GetLabels()))
			}))).
		WithOptions(controller.Options{MaxConcurrentReconciles: 10}).
		Complete(&reconciler{
			client:         mgr.GetClient(),
			leaderLabelKey: leaderLabelKey,
			leaderPodName:  leaderPodName,
		})
}

type reconciler struct {
	client         ctrlruntimeclient.Client
	leaderLabelKey string
	leaderPodName  string
}

const leaderLabelValue = "true"

func (r *reconciler) Reconcile(ctx context.Context, req reconcile.Request) (_ reconcile.Result, e error) {
	log := logrus.WithField("request", req.String())
	var pod corev1.Pod
	if err := r.client.Get(ctx, req.NamespacedName, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("failed to get pod %s: %w", req, err)
	}

	if pod.Name == r.leaderPodName && pod.DeletionTimestamp == nil {
		if pod.Labels[r.leaderLabelKey] == leaderLabelValue {
			return reconcile.Result{}, nil
		}
		if pod.Labels == nil {
			pod.Labels = map[string]string{}
		}
		pod.Labels[r.leaderLabelKey] = leaderLabelValue
		if err := r.client.Update(ctx, &pod); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update leader pod %s: %w", pod.Name, err)
		}

		log.Info("Set leader label on pod")
		return reconcile.Result{}, nil
	}

	if pod.Labels[r.leaderLabelKey] != leaderLabelValue {
		return reconcile.Result{}, nil
	}

	delete(pod.Labels, r.leaderLabelKey)
	if err := r.client.Update(ctx, &pod); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to update non-leader pod %s: %w", pod.Name, err)
	}
	log.Info("Removed leader label on pod")

	return reconcile.Result{}, nil
}
