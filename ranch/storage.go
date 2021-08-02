/*
Copyright 2017 The Kubernetes Authors.

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

package ranch

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/boskos/common"
	"sigs.k8s.io/boskos/crds"
)

// Storage is used to decouple ranch functionality with the resource persistence layer
type Storage struct {
	ctx           context.Context
	client        ctrlruntimeclient.Client
	namespace     string
	resourcesLock sync.RWMutex

	// For testing
	now          func() metav1.Time
	generateName func() string
}

// NewTestingStorage is used only for testing.
func NewTestingStorage(client ctrlruntimeclient.Client, namespace string, updateTime func() metav1.Time) *Storage {
	return &Storage{
		ctx:       context.Background(),
		client:    client,
		namespace: namespace,
		now:       updateTime,
	}
}

// NewStorage instantiates a new Storage with a PersistenceLayer implementation
func NewStorage(ctx context.Context, client ctrlruntimeclient.Client, namespace string) *Storage {
	return &Storage{
		ctx:          ctx,
		client:       client,
		namespace:    namespace,
		now:          metav1.Now,
		generateName: common.GenerateDynamicResourceName,
	}
}

// AddResource adds a new resource
func (s *Storage) AddResource(resource *crds.ResourceObject) error {
	resource.Namespace = s.namespace
	return s.client.Create(s.ctx, resource)
}

// DeleteResource deletes a resource if it exists, errors otherwise
func (s *Storage) DeleteResource(name string) error {
	o := &crds.ResourceObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: s.namespace,
		},
	}
	return s.client.Delete(s.ctx, o)
}

// UpdateResource updates a resource if it exists, errors otherwise
func (s *Storage) UpdateResource(resource *crds.ResourceObject) (*crds.ResourceObject, error) {
	resource.Namespace = s.namespace
	resource.Status.LastUpdate = s.now()

	if err := s.client.Update(s.ctx, resource); err != nil {
		return nil, fmt.Errorf("failed to update resources %s: %w", resource.Name, err)
	}

	return resource, nil
}

// GetResource gets an existing resource, errors otherwise
func (s *Storage) GetResource(name string) (*crds.ResourceObject, error) {
	o := &crds.ResourceObject{}
	nn := types.NamespacedName{Namespace: s.namespace, Name: name}
	if err := s.client.Get(s.ctx, nn, o); err != nil {
		return nil, fmt.Errorf("failed to get resource %s: %v", name, err)
	}
	if o.Status.UserData == nil {
		o.Status.UserData = map[string]string{}
	}

	return o, nil
}

// GetResources list all resources
func (s *Storage) GetResources() (*crds.ResourceObjectList, error) {
	resourceList := &crds.ResourceObjectList{}
	if err := s.client.List(s.ctx, resourceList, ctrlruntimeclient.InNamespace(s.namespace)); err != nil {
		return nil, fmt.Errorf("failed to list resources; %v", err)
	}

	sort.SliceStable(resourceList.Items, func(i, j int) bool {
		return resourceList.Items[i].Status.LastUpdate.Time.Before(resourceList.Items[j].Status.LastUpdate.Time)
	})

	return resourceList, nil
}

// AddDynamicResourceLifeCycle adds a new dynamic resource life cycle
func (s *Storage) AddDynamicResourceLifeCycle(resource *crds.DRLCObject) error {
	resource.Namespace = s.namespace
	return s.client.Create(s.ctx, resource)
}

// DeleteDynamicResourceLifeCycle deletes a dynamic resource life cycle if it exists, errors otherwise
func (s *Storage) DeleteDynamicResourceLifeCycle(name string) error {
	o := &crds.DRLCObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: s.namespace,
		},
	}
	if err := s.client.Delete(s.ctx, o); err != nil {
		return err
	}
	if err := wait.Poll(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		if err := s.client.Get(s.ctx, ctrlruntimeclient.ObjectKey{Namespace: s.namespace, Name: name}, o); err != nil {
			if kerrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("failed for deleted dynamic resource lifecycle %s/%s to vanish from cache: %w", s.namespace, name, err)
	}
	return nil
}

// UpdateDynamicResourceLifeCycle updates a dynamic resource life cycle. if it exists, errors otherwise
func (s *Storage) UpdateDynamicResourceLifeCycle(resource *crds.DRLCObject) (*crds.DRLCObject, error) {
	resource.Namespace = s.namespace
	if err := s.client.Update(s.ctx, resource); err != nil {
		return nil, fmt.Errorf("failed to update dlrc %s: %w", resource.Name, err)
	}

	// Make sure we have this change in our cache
	expectedSpec := resource.Spec
	name := ctrlruntimeclient.ObjectKey{Namespace: resource.Namespace, Name: resource.Name}
	if err := wait.Poll(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		drlc := &crds.DRLCObject{}
		if err := s.client.Get(s.ctx, name, drlc); err != nil {
			return false, err
		}
		return reflect.DeepEqual(expectedSpec, drlc.Spec), nil
	}); err != nil {
		return nil, fmt.Errorf("failed waiting for object update of %s to appear in cache: %w", name, err)
	}

	return resource, nil
}

// GetDynamicResourceLifeCycle gets an existing dynamic resource life cycle, errors otherwise
func (s *Storage) GetDynamicResourceLifeCycle(name string) (*crds.DRLCObject, error) {
	drlc := &crds.DRLCObject{}
	nn := types.NamespacedName{Namespace: s.namespace, Name: name}
	if err := s.client.Get(s.ctx, nn, drlc); err != nil {
		return nil, fmt.Errorf("failed to get dlrc %s: %q", name, err)
	}

	return drlc, nil
}

// GetDynamicResourceLifeCycles list all dynamic resource life cycle
func (s *Storage) GetDynamicResourceLifeCycles() (*crds.DRLCObjectList, error) {
	drlcList := &crds.DRLCObjectList{}
	if err := s.client.List(s.ctx, drlcList, ctrlruntimeclient.InNamespace(s.namespace)); err != nil {
		return nil, fmt.Errorf("failed to list drlcs: %v", err)
	}

	return drlcList, nil
}

// SyncResources will update static and dynamic resources.
// It will add new resources to storage and try to remove newly deleted resources
// from storage.
// If the newly deleted resource is currently held by a user, the deletion will
// yield to next update cycle.
func (s *Storage) SyncResources(config *common.BoskosConfig) error {
	if config == nil {
		return nil
	}

	var staticResourcesFromConfigByName map[string]crds.ResourceObject
	if err := retryOnConflict(retry.DefaultBackoff, func() error {
		staticResourcesFromConfigByName = map[string]crds.ResourceObject{}
		existingSRByName := map[string]crds.ResourceObject{}
		newDRLCByType := map[string]crds.DRLCObject{}
		existingDRLCByType := map[string]crds.DRLCObject{}

		for _, entry := range config.Resources {
			if entry.IsDRLC() {
				newDRLCByType[entry.Type] = *crds.FromDynamicResourceLifecycle(common.NewDynamicResourceLifeCycleFromConfig(entry))
			} else {
				for _, res := range common.NewResourcesFromConfig(entry) {
					staticResourcesFromConfigByName[res.Name] = *crds.FromResource(res)
				}
			}
		}

		if err := func() error {
			s.resourcesLock.Lock()
			defer s.resourcesLock.Unlock()

			resources, err := s.GetResources()
			if err != nil {
				return err
			}
			existingDRLC, err := s.GetDynamicResourceLifeCycles()
			if err != nil {
				return err
			}
			for _, dRLC := range existingDRLC.Items {
				existingDRLCByType[dRLC.Name] = dRLC
			}

			// Split resources between static and dynamic resources
			lifeCycleTypes := sets.String{}

			for _, lc := range existingDRLC.Items {
				lifeCycleTypes.Insert(lc.Name)
			}
			// Considering the migration case from mason resources to dynamic resources.
			// Dynamic resources already exist but they don't have an associated DRLC
			for _, lc := range newDRLCByType {
				lifeCycleTypes.Insert(lc.Name)
			}

			for _, res := range resources.Items {
				if _, inStaticConfig := staticResourcesFromConfigByName[res.Name]; inStaticConfig || !lifeCycleTypes.Has(res.Spec.Type) {
					existingSRByName[res.Name] = res
				}
			}

			var errs []error
			if err := s.syncStaticResources(staticResourcesFromConfigByName, existingSRByName); err != nil {
				errs = append(errs, fmt.Errorf("failed to sync static resources: %w", err))
			}
			if err := s.syncDynamicResourceLifeCycles(newDRLCByType, existingDRLCByType); err != nil {
				errs = append(errs, fmt.Errorf("failed to sync dynamic resources: %w", err))
			}
			return utilerrors.NewAggregate(errs)
		}(); err != nil {
			return err
		}
		return nil
	}); err != nil {
		logrus.WithError(err).Error("Encountered error syncing resources from configfile.")
		return err
	}

	if err := s.UpdateAllDynamicResources(staticResourcesFromConfigByName); err != nil {
		logrus.WithError(err).Error("Encountered error updating dynamic resources.")
		return err
	}

	return nil
}

// updateDynamicResources updates dynamic resource based on an existing dynamic resource life cycle.
// It will make sure than MinCount of resource exists, and attempt to delete expired and resources over MaxCount.
// If resources are held by another user than Boskos, they will be deleted in a following cycle.
func (s *Storage) updateDynamicResources(lifecycle *crds.DRLCObject, resources []crds.ResourceObject) (toAdd, toDelete []crds.ResourceObject) {
	var notInUseRes []crds.ResourceObject
	tombStoned := 0
	toBeDeleted := 0
	for _, r := range resources {
		if r.Status.Owner != "" {
			// We can only delete resources not in use.
			continue
		}
		if r.Status.State == common.Tombstone {
			// Ready to be fully deleted.
			toDelete = append(toDelete, r)
			tombStoned++
		} else if r.Status.State == common.ToBeDeleted {
			// Already in the process of cleaning up.
			// Don't create new resources yet, but also don't delete additional
			// resources.
			toBeDeleted++
		} else {
			if r.Status.ExpirationDate != nil && s.now().After(r.Status.ExpirationDate.Time) {
				// Expired. Don't decrement the active count until it's tombstoned,
				// however, as it might be depending on other resources that need
				// to be released first.
				toDelete = append(toDelete, r)
				toBeDeleted++
			} else {
				notInUseRes = append(notInUseRes, r)
			}
		}
	}

	// Tombstoned resources are ready to be fully deleted, so replace them if necessary.
	activeCount := len(resources) - tombStoned
	for i := activeCount; i < lifecycle.Spec.MinCount; i++ {
		res := newResourceFromNewDynamicResourceLifeCycle(s.generateName(), lifecycle, s.now())
		toAdd = append(toAdd, *res)
		activeCount++
	}

	// ToBeDeleted resources may take some time to be fully cleaned up.
	// We can temporarily exceed MaxCount while these are being cleaned up,
	// particularly if MaxCount was recently lowered.
	numberOfResToDelete := activeCount - toBeDeleted - lifecycle.Spec.MaxCount
	// Sorting to get consistent deletion mechanism (ease testing)
	sort.SliceStable(notInUseRes, func(i, j int) bool {
		return notInUseRes[i].Name > notInUseRes[j].Name
	})
	for i := 0; i < len(notInUseRes); i++ {
		res := notInUseRes[i]
		if i < numberOfResToDelete {
			toDelete = append(toDelete, res)
		}
	}

	return
}

// UpdateAllDynamicResources queries for all existing DynamicResourceLifeCycles
// and dynamic resources and calls updateDynamicResources for each type.
// This ensures that the MinCount and MaxCount parameters are honored, that
// any expired resources are deleted, and that any Tombstoned resources are
// completely removed.
// nolint:gocognit
func (s *Storage) UpdateAllDynamicResources(staticResources map[string]crds.ResourceObject) error {
	s.resourcesLock.Lock()
	defer s.resourcesLock.Unlock()

	return retryOnConflict(retry.DefaultBackoff, func() error {
		var resToAdd, resToDelete []crds.ResourceObject
		var dRLCToDelete []crds.DRLCObject
		existingDRLCByType := map[string]crds.DRLCObject{}
		existingDRsByType := map[string][]crds.ResourceObject{}

		resources, err := s.GetResources()
		if err != nil {
			return err
		}
		existingDRLC, err := s.GetDynamicResourceLifeCycles()
		if err != nil {
			return err
		}
		for _, dRLC := range existingDRLC.Items {
			existingDRLCByType[dRLC.Name] = dRLC
		}

		// Filter to only look at dynamic resources
		for _, res := range resources.Items {
			// Do not delete objects that have a static config
			if _, hasStaticConfig := staticResources[res.Name]; hasStaticConfig {
				continue
			}
			if _, ok := existingDRLCByType[res.Spec.Type]; ok {
				existingDRsByType[res.Spec.Type] = append(existingDRsByType[res.Spec.Type], res)
			}
		}

		for resType, dRLC := range existingDRLCByType {
			existingDRs := existingDRsByType[resType]
			toAdd, toDelete := s.updateDynamicResources(&dRLC, existingDRs)
			resToAdd = append(resToAdd, toAdd...)
			resToDelete = append(resToDelete, toDelete...)

			if dRLC.Spec.MinCount == 0 && dRLC.Spec.MaxCount == 0 {
				currentCount := len(existingDRs)
				addCount := len(resToAdd)
				delCount := len(resToDelete)
				if addCount == 0 && (currentCount == 0 || currentCount == delCount) {
					dRLCToDelete = append(dRLCToDelete, dRLC)
				}
			}
		}

		if err := s.persistResources(resToAdd, resToDelete, true); err != nil {
			return err
		}

		if len(dRLCToDelete) > 0 {
			if err := s.deleteDynamicResourceLifecycles(dRLCToDelete, staticResources); err != nil {
				return err
			}
		}

		return nil
	})
}

// syncDynamicResourceLifeCycles compares the new DRLC configuration against
// the current configuration. If a DRLC has been deleted from the new
// configuration, it is updated to indicate that its dynamic resources should
// be removed.
// No dynamic resources are created, deleted, or modified by this function.
func (s *Storage) syncDynamicResourceLifeCycles(newDRLCByType, existingDRLCByType map[string]crds.DRLCObject) error {
	var dRLCToUpdate, dRLCToAdd []crds.DRLCObject

	var errs []error
	for _, existingDRLC := range existingDRLCByType {
		newDRLC, existsInNew := newDRLCByType[existingDRLC.Name]
		if existsInNew {
			// Copy the ObjectMeta so we can compare, the update doesn't fail due to unset ResourceVersion
			// and to keep any additional metadata that was added.
			newDRLC.ObjectMeta = *existingDRLC.ObjectMeta.DeepCopy()
			if !reflect.DeepEqual(existingDRLC, newDRLC) {
				dRLCToUpdate = append(dRLCToUpdate, newDRLC)
			}
		} else if existingDRLC.Spec.MinCount != 0 || existingDRLC.Spec.MaxCount != 0 {
			// Mark for deletion of all associated dynamic resources.
			existingDRLC.Spec.MinCount = 0
			existingDRLC.Spec.MaxCount = 0
			dRLCToUpdate = append(dRLCToUpdate, existingDRLC)
		}
	}

	for _, newDRLC := range newDRLCByType {
		_, exists := existingDRLCByType[newDRLC.Name]
		if !exists {
			dRLCToAdd = append(dRLCToAdd, newDRLC)
		}
	}

	errs = append(errs, s.persistDynamicResourceLifeCycles(dRLCToUpdate, dRLCToAdd))
	return utilerrors.NewAggregate(errs)
}

func (s *Storage) persistResources(resToAdd, resToDelete []crds.ResourceObject, dynamic bool) error {
	var errs []error
	for _, r := range resToDelete {
		// If currently busy, yield deletion to later cycles.
		if r.Status.Owner != "" {
			continue
		}
		l := logrus.WithField("name", r.Name)
		if dynamic {
			// Only delete resource in tombsone state and mark the other as to deleted
			// This is necessary for dynamic resources that depends on other resources
			// as they need to be released to prevent leak.
			if r.Status.State == common.Tombstone {
				l.Info("Deleting resource")
				if err := s.DeleteResource(r.Name); err != nil {
					errs = append(errs, err)
				}
			} else if r.Status.State != common.ToBeDeleted {
				r.Status.State = common.ToBeDeleted
				l.Info("Marking resource to be deleted")
				if _, err := s.UpdateResource(&r); err != nil {
					errs = append(errs, err)
				}
			}
		} else {
			// Static resources can be deleted right away.
			l.Info("Deleting resource")
			if err := s.DeleteResource(r.Name); err != nil {
				errs = append(errs, err)
			}
		}
	}

	for _, r := range resToAdd {
		logrus.WithField("name", r.Name).Info("Adding resource")
		r.Status.LastUpdate = s.now()
		if err := s.AddResource(&r); err != nil {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (s *Storage) persistDynamicResourceLifeCycles(dRLCToUpdate, dRLCToAdd []crds.DRLCObject) error {
	var errs []error
	for idx, DRLC := range dRLCToAdd {
		logrus.Infof("Adding resource type life cycle %s", DRLC.Name)
		if err := s.AddDynamicResourceLifeCycle(&dRLCToAdd[idx]); err != nil {
			errs = append(errs, err)
		}
	}

	for idx, dRLC := range dRLCToUpdate {
		logrus.Infof("Updating resource type life cycle %s", dRLC.Name)
		if _, err := s.UpdateDynamicResourceLifeCycle(&dRLCToUpdate[idx]); err != nil {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (s *Storage) deleteDynamicResourceLifecycles(dRLCToDelete []crds.DRLCObject, staticResourcesByName map[string]crds.ResourceObject) error {
	remainingTypes := map[string]bool{}
	resources, err := s.GetResources()
	if err != nil {
		return err
	}
	for _, res := range resources.Items {
		if _, hasStaticConfig := staticResourcesByName[res.Name]; hasStaticConfig {
			continue
		}
		remainingTypes[res.Spec.Type] = true
	}

	var errs []error
	for idx, dRLC := range dRLCToDelete {
		// Only delete a dynamic resource if all resources are gone
		if !remainingTypes[dRLC.Name] {
			logrus.Infof("Deleting resource type life cycle %s", dRLC.Name)
			if err := s.DeleteDynamicResourceLifeCycle(dRLC.Name); err != nil {
				errs = append(errs, err)
			}
		} else {
			// Mark this DRLC as pending deletion by setting min and max count to zero.
			dRLC.Spec.MinCount = 0
			dRLC.Spec.MaxCount = 0
			if _, err := s.UpdateDynamicResourceLifeCycle(&dRLCToDelete[idx]); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (s *Storage) syncStaticResources(newResourcesByName, existingResourcesByName map[string]crds.ResourceObject) error {
	var resToAdd, resToDelete []crds.ResourceObject

	// Delete resources
	for _, res := range existingResourcesByName {
		_, exists := newResourcesByName[res.Name]
		if !exists {
			resToDelete = append(resToDelete, res)
		}
	}

	// Add new resources
	for _, res := range newResourcesByName {
		_, exists := existingResourcesByName[res.Name]
		if !exists {
			resToAdd = append(resToAdd, res)
		}
	}
	return s.persistResources(resToAdd, resToDelete, false)
}
