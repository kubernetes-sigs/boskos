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

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	v1 "k8s.io/api/core/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"sigs.k8s.io/boskos/cleaner"
	"sigs.k8s.io/boskos/common"
	"sigs.k8s.io/boskos/crds"
)

const controllerName = "boskos-libvirt-cleaner"

// Add creates a new cleaner controller
func Add(mgr manager.Manager, boskosClient cleaner.RecycleBoskosClient, namespace string) error {
	reconciler := &reconciler{
		ctx:          context.Background(),
		client:       mgr.GetClient(),
		boskosClient: boskosClient,
		namespace:    namespace,
	}

	c, err := controller.New(controllerName, mgr, controller.Options{
		Reconciler:              reconciler,
		MaxConcurrentReconciles: 4,
	})
	if err != nil {
		return fmt.Errorf("failed to create controller: %v", err)
	}

	if err := c.Watch(&source.Kind{Type: &crds.ResourceObject{}}, &handler.EnqueueRequestForObject{}); err != nil {
		return fmt.Errorf("failed to create watch: %v", err)
	}

	return nil
}

type reconciler struct {
	ctx          context.Context
	client       ctrlruntimeclient.Client
	boskosClient cleaner.RecycleBoskosClient
	recycleFunc  func(cleaner.RecycleBoskosClient, *common.Resource)
	namespace    string
}

func (r *reconciler) Reconcile(_ context.Context, request reconcile.Request) (reconcile.Result, error) {
	// TODO(alvaroaleman): figure out how to use the context
	log := logrus.WithField("resource-name", request.Name)
	err := r.reconcile(log, request)
	if err != nil {
		log.WithError(err).Error("Reconciliation error")
	}
	return reconcile.Result{}, err
}

func (r *reconciler) shouldDeactivate () (bool, error) {
	var err error = nil
	var nn_should_deactivate = types.NamespacedName{Namespace: "ci", Name: "deactivate"}
	var secret_should_deactivate v1.Secret

	if err = r.client.Get(r.ctx, nn_should_deactivate, &secret_should_deactivate); err != nil {
		if kerrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("shouldDeactivate: Get: %v", err)
	}

	bytes_should_deactivate, ok := secret_should_deactivate.Data["data"]
	if !ok {
		logrus.Debugf ("shouldDeactivate: bytes_should_deactivate = %v, ok = %v", bytes_should_deactivate, ok)
		return false, nil
	}

	string_should_deactivate := string(bytes_should_deactivate)
	bool_should_deactivate := string_should_deactivate == "yes"

	if !bool_should_deactivate {
		logrus.Debugf ("shouldDeactivate: string_should_deactivate = %s", string_should_deactivate)
	}

	return bool_should_deactivate, nil
}

func (r *reconciler) nameWeHandle (name string) (bool, error) {
	if should, err := r.shouldDeactivate (); err != nil {
		return false, err
	} else if should {
		logrus.Debugf ("nameWeHandle: shouldDeactivate!")
		return false, nil
	}
	if strings.HasPrefix(name, "libvirt-ppc64le-") {
		return true, nil
	} else if strings.HasPrefix(name, "libvirt-s390x-") {
		return true, nil
	} else {
		return false, nil
	}
}

func (r *reconciler) reconcile(log *logrus.Entry, request reconcile.Request) error {
	resourceObject := &crds.ResourceObject{}
	if err := r.client.Get(r.ctx, request.NamespacedName, resourceObject); err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get object %s: %v", request.NamespacedName.String(), err)
	}

	if resourceObject.Status.State == common.ToBeDeleted {
		if weHandle, err := r.nameWeHandle(resourceObject.Name); err != nil {
			return err
		} else if weHandle {
			if found, err:= Cleanup (resourceObject.Name); err != nil {
				return err
			} else if found {
				return fmt.Errorf("reconcile: libvirt cleanup still has uncleaned resources")
			} else {
				resourceObject.Status.State = common.Tombstone
				return nil
			}
		}
	}

	// We only care about unowned resources in ToBeDeleted state
	if resourceObject.Status.Owner != "" || resourceObject.Status.State != common.ToBeDeleted {
		return nil
	}

	isDynamic, err := r.isResourceDynamic(resourceObject)
	if err != nil {
		return fmt.Errorf("failed to check if resource is dynamic: %v", err)
	}
	if !isDynamic {
		return nil
	}

	commonResourceObject := resourceObject.ToResource()
	cleaner.RecycleOne(r.boskosClient, &commonResourceObject)

	resourceObject.Status.State = common.Tombstone
	if err := r.client.Update(r.ctx, resourceObject); err != nil {
		return fmt.Errorf("failed to update object after setting status to tombstone: %v", err)
	}
	log.WithField("new-state", common.Tombstone).Debug("Successfully updated objects state.")

	return nil
}

func (r *reconciler) isResourceDynamic(resourceObject *crds.ResourceObject) (bool, error) {
	drlcName := types.NamespacedName{Namespace: r.namespace, Name: resourceObject.Spec.Type}
	err := r.client.Get(r.ctx, drlcName, &crds.DRLCObject{})
	return !kerrors.IsNotFound(err), ctrlruntimeclient.IgnoreNotFound(err)
}
