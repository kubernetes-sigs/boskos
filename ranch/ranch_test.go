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
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-test/deep"
	"github.com/google/go-cmp/cmp"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"sigs.k8s.io/boskos/common"
	"sigs.k8s.io/boskos/crds"
)

// Make debugging a bit easier
func init() {
	logrus.SetLevel(logrus.DebugLevel)
}

var (
	startTime = fakeTime(time.Now().Truncate(time.Second))
	fakeNow   = fakeTime(startTime.Add(time.Second).Truncate(time.Second))
)

type nameGenerator struct {
	lock  sync.Mutex
	index int
}

func (g *nameGenerator) name() string {
	g.lock.Lock()
	defer g.lock.Unlock()
	g.index++
	return fmt.Sprintf("new-dynamic-res-%d", g.index)
}

// json does not serialized time with nanosecond precision
func fakeTime(t time.Time) metav1.Time {
	format := "2006-01-02 15:04:05.000"
	now, _ := time.Parse(format, t.Format(format))
	return metav1.Time{Time: now}
}

const testNS = "test"

func makeTestRanch(objects []runtime.Object) *Ranch {
	for _, obj := range objects {
		obj.(metav1.Object).SetNamespace(testNS)
	}
	client := &onceConflictingClient{Client: fakectrlruntimeclient.NewFakeClient(objects...)}
	s := NewStorage(context.Background(), client, testNS)
	s.now = func() metav1.Time {
		return fakeNow
	}
	nameGen := &nameGenerator{}
	s.generateName = nameGen.name
	r, _ := NewRanch("", s, testTTL)
	r.now = func() metav1.Time {
		return fakeNow
	}
	return r
}

func AreErrorsEqual(got error, expect error) bool {
	if got == nil && expect == nil {
		return true
	}

	if got == nil || expect == nil {
		return false
	}

	switch got.(type) {
	case *OwnerNotMatch:
		if o, ok := expect.(*OwnerNotMatch); ok {
			if o.request == got.(*OwnerNotMatch).request && o.owner == got.(*OwnerNotMatch).owner {
				return true
			}
		}
		return false
	case *ResourceNotFound:
		if o, ok := expect.(*ResourceNotFound); ok {
			if o.name == got.(*ResourceNotFound).name {
				return true
			}
		}
		return false
	case *ResourceTypeNotFound:
		if o, ok := expect.(*ResourceTypeNotFound); ok {
			if o.rType == got.(*ResourceTypeNotFound).rType {
				return true
			}
		}
		return false
	case *StateNotMatch:
		if o, ok := expect.(*StateNotMatch); ok {
			if o.expect == got.(*StateNotMatch).expect && o.current == got.(*StateNotMatch).current {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func TestAcquire(t *testing.T) {
	var testcases = []struct {
		name      string
		resources []runtime.Object
		owner     string
		rtype     string
		state     string
		dest      string
		expectErr error
	}{
		{
			name:      "ranch has no resource",
			owner:     "user",
			rtype:     "t",
			state:     "s",
			dest:      "d",
			expectErr: &ResourceTypeNotFound{"t"},
		},
		{
			name: "no match type",
			resources: []runtime.Object{
				newResource("res", "wrong", "s", "", startTime),
			},
			owner:     "user",
			rtype:     "t",
			state:     "s",
			dest:      "d",
			expectErr: &ResourceTypeNotFound{"t"},
		},
		{
			name: "no match state",
			resources: []runtime.Object{
				newResource("res", "t", "wrong", "", startTime),
			},
			owner:     "user",
			rtype:     "t",
			state:     "s",
			dest:      "d",
			expectErr: &ResourceNotFound{"t"},
		},
		{
			name: common.Busy,
			resources: []runtime.Object{
				newResource("res", "t", "s", "foo", startTime),
			},
			owner:     "user",
			rtype:     "t",
			state:     "s",
			dest:      "d",
			expectErr: &ResourceNotFound{"t"},
		},
		{
			name: "ok",
			resources: []runtime.Object{
				newResource("res", "t", "s", "", startTime),
			},
			owner:     "user",
			rtype:     "t",
			state:     "s",
			dest:      "d",
			expectErr: nil,
		},
	}

	for _, tc := range testcases {
		c := makeTestRanch(tc.resources)
		now := metav1.Now()
		c.now = func() metav1.Time {
			return now
		}
		res, createdTime, err := c.Acquire(tc.rtype, tc.state, tc.dest, tc.owner, "")
		if !AreErrorsEqual(err, tc.expectErr) {
			t.Errorf("%s - Got error %v, expected error %v", tc.name, err, tc.expectErr)
			continue
		}

		resources, err2 := c.Storage.GetResources()
		if err2 != nil {
			t.Errorf("failed to get resources")
			continue
		}
		if !now.Equal(&createdTime) {
			t.Errorf("expected createdAt %s, got %s", now, createdTime)
		}
		if err == nil {
			if res.Status.State != tc.dest {
				t.Errorf("%s - Wrong final state. Got %v, expected %v", tc.name, res.Status.State, tc.dest)
			}
			if diff := cmp.Diff(resources.Items[0], *res); diff != "" {
				t.Errorf("resources differ: %s", diff)
			} else if !res.Status.LastUpdate.After(startTime.Time) {
				t.Errorf("%s - LastUpdate did not update.", tc.name)
			}
		} else {
			for _, res := range resources.Items {
				if !res.Status.LastUpdate.Equal(&startTime) {
					t.Errorf("%s - LastUpdate should not update. Got %v, expected %v", tc.name, resources.Items[0].Status.LastUpdate, startTime)
				}
			}
		}
	}
}

func TestAcquirePriority(t *testing.T) {
	now := metav1.Now()
	expiredFuture := metav1.Time{Time: now.Add(2 * testTTL)}
	owner := "tester"
	res := crds.NewResource("res", "type", common.Free, "", now)
	r := makeTestRanch(nil)
	r.requestMgr.now = func() metav1.Time { return now }

	// Setting Priority, this request will fail
	if _, _, err := r.Acquire(res.Spec.Type, res.Status.State, common.Dirty, owner, "request_id_1"); err == nil {
		t.Errorf("should fail as there are not resource available")
	}
	if err := r.Storage.AddResource(res); err != nil {
		t.Fatalf("failed to add resource: %v", err)
	}
	// Attempting to acquire this resource without priority
	if _, _, err := r.Acquire(res.Spec.Type, res.Status.State, common.Dirty, owner, ""); err == nil {
		t.Errorf("should fail as there is only resource, and it is prioritizes to request_id_1")
	}
	// Attempting to acquire this resource with priority, which will set a place in the queue
	if _, _, err := r.Acquire(res.Spec.Type, res.Status.State, common.Dirty, owner, "request_id_2"); err == nil {
		t.Errorf("should fail as there is only resource, and it is prioritizes to request_id_1")
	}
	// Attempting with the first request
	_, createdTime, err := r.Acquire(res.Spec.Type, res.Status.State, common.Dirty, owner, "request_id_1")
	if err != nil {
		t.Fatalf("should succeed since the request priority should match its rank in the queue. got %v", err)
	}
	if !now.Equal(&createdTime) {
		t.Errorf("expected createdAt %s, got %s", now, createdTime)
	}
	r.Release(res.Name, common.Free, "tester")
	// Attempting with the first request
	if _, _, err := r.Acquire(res.Spec.Type, res.Status.State, common.Dirty, owner, "request_id_1"); err == nil {
		t.Errorf("should not succeed since this request has already been fulfilled")
	}
	// Attempting to acquire this resource without priority
	if _, _, err := r.Acquire(res.Spec.Type, res.Status.State, common.Dirty, owner, ""); err == nil {
		t.Errorf("should fail as request_id_2 has rank 1 now")
	}
	r.requestMgr.cleanup(expiredFuture)
	now2 := metav1.Now()
	r.now = func() metav1.Time { return now2 }
	// Attempting to acquire this resource without priority
	_, createdTime, err = r.Acquire(res.Spec.Type, res.Status.State, common.Dirty, owner, "")
	if err != nil {
		t.Errorf("request_id_2 expired, this should work now, got %v", err)
	}
	if !now2.Equal(&createdTime) {
		t.Errorf("expected createdAt %s, got %s", now, createdTime)
	}
}

func TestAcquireRoundRobin(t *testing.T) {
	var resources []runtime.Object
	for i := 1; i < 5; i++ {
		resources = append(resources, newResource(fmt.Sprintf("res-%d", i), "t", "s", "", startTime))
	}

	results := map[string]int{}

	c := makeTestRanch(resources)
	for i := 0; i < 4; i++ {
		res, _, err := c.Acquire("t", "s", "d", "foo", "")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		_, found := results[res.Name]
		if found {
			t.Errorf("resource %s was used more than once", res.Name)
		}
		c.Release(res.Name, "s", "foo")
	}
}

func TestAcquireOnDemand(t *testing.T) {
	owner := "tester"
	rType := "dr"
	requestID1 := "req1234"
	requestID2 := "req12345"
	requestID3 := "req123456"
	now := metav1.Now()
	dRLCs := []runtime.Object{
		&crds.DRLCObject{
			ObjectMeta: metav1.ObjectMeta{
				Name: rType,
			},
			Spec: crds.DRLCSpec{
				MinCount:     0,
				MaxCount:     2,
				InitialState: common.Dirty,
			},
		},
	}
	// Not adding any resources to start with
	c := makeTestRanch(dRLCs)
	c.now = func() metav1.Time { return now }
	// First acquire should trigger a creation
	if _, _, err := c.Acquire(rType, common.Free, common.Busy, owner, requestID1); err == nil {
		t.Errorf("should fail since there is not resource yet")
	}
	if resources, err := c.Storage.GetResources(); err != nil {
		t.Fatal(err)
	} else if len(resources.Items) != 1 {
		t.Fatal("A resource should have been created")
	}
	// Attempting to create another resource
	if _, _, err := c.Acquire(rType, common.Free, common.Busy, owner, requestID1); err == nil {
		t.Errorf("should succeed since the created is dirty")
	}
	if resources, err := c.Storage.GetResources(); err != nil {
		t.Error(err)
	} else if len(resources.Items) != 1 {
		t.Errorf("No new resource should have been created")
	}
	// Creating another
	if _, _, err := c.Acquire(rType, common.Free, common.Busy, owner, requestID2); err == nil {
		t.Errorf("should succeed since the created is dirty")
	}
	if resources, err := c.Storage.GetResources(); err != nil {
		t.Error(err)
	} else if len(resources.Items) != 2 {
		t.Errorf("Another resource should have been created")
	}
	// Attempting to create another
	if _, _, err := c.Acquire(rType, common.Free, common.Busy, owner, requestID3); err == nil {
		t.Errorf("should fail since there is not resource yet")
	}
	resources, err := c.Storage.GetResources()
	if err != nil {
		t.Error(err)
	} else if len(resources.Items) != 2 {
		t.Errorf("No other resource should have been created")
	}
	for _, res := range resources.Items {
		c.Storage.DeleteResource(res.Name)
	}
	if _, _, err := c.Acquire(rType, common.Free, common.Busy, owner, ""); err == nil {
		t.Errorf("should fail since there is not resource yet")
	}
	if resources, err := c.Storage.GetResources(); err != nil {
		t.Error(err)
	} else if len(resources.Items) != 0 {
		t.Errorf("No new resource should have been created")
	}
}

func TestRelease(t *testing.T) {
	var lifespan = time.Minute
	updatedRes := crds.NewResource("res", "t", "d", "", fakeNow)
	expirationDate := fakeTime(fakeNow.Add(lifespan))
	updatedRes.Status.ExpirationDate = &expirationDate
	var testcases = []struct {
		name        string
		resource    *crds.ResourceObject
		dResource   *crds.DRLCObject
		resName     string
		owner       string
		dest        string
		expectErr   error
		expectedRes *crds.ResourceObject
	}{
		{
			name:      "ranch has no resource",
			resName:   "res",
			owner:     "user",
			dest:      "d",
			expectErr: &ResourceNotFound{"res"},
		},
		{
			name:        "wrong owner",
			resource:    newResource("res", "t", "s", "merlin", startTime),
			resName:     "res",
			owner:       "user",
			dest:        "d",
			expectErr:   &OwnerNotMatch{"user", "merlin"},
			expectedRes: crds.NewResource("res", "t", "s", "merlin", startTime),
		},
		{
			name:      "no match name",
			resource:  newResource("foo", "t", "s", "merlin", startTime),
			resName:   "res",
			owner:     "user",
			dest:      "d",
			expectErr: &ResourceNotFound{"res"},
		},
		{
			name:        "ok",
			resource:    newResource("res", "t", "s", "merlin", startTime),
			resName:     "res",
			owner:       "merlin",
			dest:        "d",
			expectErr:   nil,
			expectedRes: crds.NewResource("res", "t", "d", "", fakeNow),
		},
		{
			name:     "ok - has dynamic resource lf no lifespan",
			resource: newResource("res", "t", "s", "merlin", startTime),
			dResource: &crds.DRLCObject{ObjectMeta: metav1.ObjectMeta{
				Name: "t",
			}},
			resName:     "res",
			owner:       "merlin",
			dest:        "d",
			expectErr:   nil,
			expectedRes: crds.NewResource("res", "t", "d", "", fakeNow),
		},
		{
			name:     "ok - has dynamic resource lf with lifespan",
			resource: crds.NewResource("res", "t", "s", "merlin", startTime),
			dResource: &crds.DRLCObject{
				ObjectMeta: metav1.ObjectMeta{Name: "t"},
				Spec:       crds.DRLCSpec{LifeSpan: &lifespan},
			},
			resName:     "res",
			owner:       "merlin",
			dest:        "d",
			expectErr:   nil,
			expectedRes: updatedRes,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []runtime.Object
			if tc.resource != nil {
				objs = append(objs, tc.resource)
			}
			if tc.dResource != nil {
				objs = append(objs, tc.dResource)
			}
			if tc.expectedRes != nil {
				tc.expectedRes.Namespace = testNS
			}
			c := makeTestRanch(objs)
			releaseErr := c.Release(tc.resName, tc.dest, tc.owner)
			if !AreErrorsEqual(releaseErr, tc.expectErr) {
				t.Fatalf("Got error %v, expected error %v", releaseErr, tc.expectErr)
			}
			res, _ := c.Storage.GetResource(tc.resName)
			if diff := diffResourceObjects(res, tc.expectedRes); diff != nil {
				t.Errorf("result didn't match expected, diff: %v", diff)
			}
		})
	}
}

func diffResourceObjects(a, b *crds.ResourceObject) []string {
	if a != nil {
		a.TypeMeta = metav1.TypeMeta{}
		a.ResourceVersion = "0"
		a.Status.LastUpdate.Time = a.Status.LastUpdate.UTC()
	}
	if b != nil {
		b.TypeMeta = metav1.TypeMeta{}
		b.ResourceVersion = "0"
		b.Status.LastUpdate.Time = b.Status.LastUpdate.UTC()
	}
	return deep.Equal(a, b)
}

func TestReset(t *testing.T) {
	var testcases = []struct {
		name       string
		resources  []runtime.Object
		rtype      string
		state      string
		dest       string
		expire     time.Duration
		hasContent bool
	}{

		{
			name: "empty - has no owner",
			resources: []runtime.Object{
				newResource("res", "t", "s", "", metav1.Time{Time: startTime.Add(-time.Minute * 20)}),
			},
			rtype:  "t",
			state:  "s",
			expire: time.Minute * 10,
			dest:   "d",
		},
		{
			name: "empty - not expire",
			resources: []runtime.Object{
				newResource("res", "t", "s", "", startTime),
			},
			rtype:  "t",
			state:  "s",
			expire: time.Minute * 10,
			dest:   "d",
		},
		{
			name: "empty - no match type",
			resources: []runtime.Object{
				newResource("res", "wrong", "s", "", metav1.Time{Time: startTime.Add(-time.Minute * 20)}),
			},
			rtype:  "t",
			state:  "s",
			expire: time.Minute * 10,
			dest:   "d",
		},
		{
			name: "empty - no match state",
			resources: []runtime.Object{
				newResource("res", "t", "wrong", "", metav1.Time{Time: startTime.Add(-time.Minute * 20)}),
			},
			rtype:  "t",
			state:  "s",
			expire: time.Minute * 10,
			dest:   "d",
		},
		{
			name: "ok",
			resources: []runtime.Object{
				newResource("res", "t", "s", "user", metav1.Time{Time: startTime.Add(-time.Minute * 20)}),
			},
			rtype:      "t",
			state:      "s",
			expire:     time.Minute * 10,
			dest:       "d",
			hasContent: true,
		},
	}

	for _, tc := range testcases {
		c := makeTestRanch(tc.resources)
		rmap, err := c.Reset(tc.rtype, tc.state, tc.expire, tc.dest)
		if err != nil {
			t.Errorf("failed to reset %v", err)
		}

		if !tc.hasContent {
			if len(rmap) != 0 {
				t.Errorf("%s - Expect empty map. Got %v", tc.name, rmap)
			}
		} else {
			if owner, ok := rmap["res"]; !ok || owner != "user" {
				t.Errorf("%s - Expect res - user. Got %v", tc.name, rmap)
			}
			resources, err := c.Storage.GetResources()
			if err != nil {
				t.Errorf("failed to get resources")
				continue
			}
			if !resources.Items[0].Status.LastUpdate.After(startTime.Time) {
				t.Errorf("%s - LastUpdate did not update.", tc.name)
			}
		}
	}
}

func TestUpdate(t *testing.T) {
	var testcases = []struct {
		name      string
		resources []runtime.Object
		resName   string
		owner     string
		state     string
		expectErr error
	}{
		{
			name:      "ranch has no resource",
			resName:   "res",
			owner:     "user",
			state:     "s",
			expectErr: &ResourceNotFound{"res"},
		},
		{
			name: "wrong owner",
			resources: []runtime.Object{
				newResource("res", "t", "s", "merlin", startTime),
			},
			resName:   "res",
			owner:     "user",
			state:     "s",
			expectErr: &OwnerNotMatch{"user", "merlin"},
		},
		{
			name: "wrong state",
			resources: []runtime.Object{
				newResource("res", "t", "s", "merlin", startTime),
			},
			resName:   "res",
			owner:     "merlin",
			state:     "foo",
			expectErr: &StateNotMatch{"s", "foo"},
		},
		{
			name: "no matched resource",
			resources: []runtime.Object{
				newResource("foo", "t", "s", "merlin", startTime),
			},
			resName:   "res",
			owner:     "merlin",
			state:     "s",
			expectErr: &ResourceNotFound{"res"},
		},
		{
			name: "ok",
			resources: []runtime.Object{
				newResource("res", "t", "s", "merlin", startTime),
			},
			resName: "res",
			owner:   "merlin",
			state:   "s",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			c := makeTestRanch(tc.resources)
			err := c.Update(tc.resName, tc.owner, tc.state, nil)
			if !AreErrorsEqual(err, tc.expectErr) {
				t.Fatalf("Got error %v, expected error %v", err, tc.expectErr)
			}

			resources, err2 := c.Storage.GetResources()
			if err2 != nil {
				t.Fatalf("failed to get resources")
			}

			if err == nil {
				if resources.Items[0].Status.Owner != tc.owner {
					t.Errorf("%s - Wrong owner after release. Got %v, expected %v", tc.name, resources.Items[0].Status.Owner, tc.owner)
				} else if resources.Items[0].Status.State != tc.state {
					t.Errorf("%s - Wrong state after release. Got %v, expected %v", tc.name, resources.Items[0].Status.State, tc.state)
				} else if !resources.Items[0].Status.LastUpdate.After(startTime.Time) {
					t.Errorf("%s - LastUpdate did not update.", tc.name)
				}
			} else {
				for _, res := range resources.Items {
					if !res.Status.LastUpdate.Equal(&startTime) {
						t.Errorf("%s - LastUpdate should not update. Got %v, expected %v", tc.name, resources.Items[0].Status.LastUpdate, startTime)
					}
				}
			}
		})
	}
}

func TestMetric(t *testing.T) {
	var testcases = []struct {
		name         string
		resources    []runtime.Object
		metricType   string
		expectErr    error
		expectMetric common.Metric
	}{
		{
			name:       "ranch has no resource",
			metricType: "t",
			expectErr:  &ResourceNotFound{"t"},
		},
		{
			name: "no matching resource",
			resources: []runtime.Object{
				newResource("res", "t", "s", "merlin", metav1.Now()),
			},
			metricType: "foo",
			expectErr:  &ResourceNotFound{"foo"},
		},
		{
			name: "one resource",
			resources: []runtime.Object{
				newResource("res", "t", "s", "merlin", metav1.Now()),
			},
			metricType: "t",
			expectMetric: common.Metric{
				Type: "t",
				Current: map[string]int{
					"s": 1,
				},
				Owners: map[string]int{
					"merlin": 1,
				},
			},
		},
		{
			name: "multiple resources",
			resources: []runtime.Object{
				newResource("res-1", "t", "s", "merlin", metav1.Now()),
				newResource("res-2", "t", "p", "pony", metav1.Now()),
				newResource("res-3", "t", "s", "pony", metav1.Now()),
				newResource("res-4", "foo", "s", "pony", metav1.Now()),
				newResource("res-5", "t", "d", "merlin", metav1.Now()),
			},
			metricType: "t",
			expectMetric: common.Metric{
				Type: "t",
				Current: map[string]int{
					"s": 2,
					"d": 1,
					"p": 1,
				},
				Owners: map[string]int{
					"merlin": 2,
					"pony":   2,
				},
			},
		},
	}

	for _, tc := range testcases {
		c := makeTestRanch(tc.resources)
		metric, err := c.Metric(tc.metricType)
		if !AreErrorsEqual(err, tc.expectErr) {
			t.Errorf("%s - Got error %v, expected error %v", tc.name, err, tc.expectErr)
			continue
		}

		if err == nil {
			if !reflect.DeepEqual(metric, tc.expectMetric) {
				t.Errorf("%s - wrong metric, got %v, want %v", tc.name, metric, tc.expectMetric)
			}
		}
	}
}

func TestAllMetrics(t *testing.T) {
	var testcases = []struct {
		name          string
		resources     []runtime.Object
		expectMetrics []common.Metric
	}{
		{
			name:          "ranch has no resource",
			expectMetrics: []common.Metric{},
		},
		{
			name: "one resource",
			resources: []runtime.Object{
				newResource("res", "t", "s", "merlin", metav1.Now()),
			},
			expectMetrics: []common.Metric{
				{
					Type: "t",
					Current: map[string]int{
						"s": 1,
					},
					Owners: map[string]int{
						"merlin": 1,
					},
				},
			},
		},
		{
			name: "multiple resources",
			resources: []runtime.Object{
				newResource("res-1", "t", "s", "merlin", metav1.Now()),
				newResource("res-2", "t", "p", "pony", metav1.Now()),
				newResource("res-3", "t", "s", "pony", metav1.Now()),
				newResource("res-4", "foo", "s", "pony", metav1.Now()),
				newResource("res-5", "t", "d", "merlin", metav1.Now()),
				newResource("res-6", "foo", "x", "mars", metav1.Now()),
				newResource("res-7", "bar", "d", "merlin", metav1.Now()),
			},
			expectMetrics: []common.Metric{
				{
					Type: "bar",
					Current: map[string]int{
						"d": 1,
					},
					Owners: map[string]int{
						"merlin": 1,
					},
				},
				{
					Type: "foo",
					Current: map[string]int{
						"s": 1,
						"x": 1,
					},
					Owners: map[string]int{
						"pony": 1,
						"mars": 1,
					},
				},
				{
					Type: "t",
					Current: map[string]int{
						"s": 2,
						"d": 1,
						"p": 1,
					},
					Owners: map[string]int{
						"merlin": 2,
						"pony":   2,
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		c := makeTestRanch(tc.resources)
		metrics, err := c.AllMetrics()
		if err != nil {
			t.Errorf("%s - Got error %v", tc.name, err)
			continue
		}
		if !reflect.DeepEqual(metrics, tc.expectMetrics) {
			t.Errorf("%s - wrong metrics, got %v, want %v", tc.name, metrics, tc.expectMetrics)
		}
	}
}

func setExpiration(res *crds.ResourceObject, exp metav1.Time) *crds.ResourceObject {
	res.Status.ExpirationDate = &exp
	return res
}

func TestSyncResources(t *testing.T) {
	var testcases = []struct {
		name        string
		currentRes  []runtime.Object
		expectedRes *crds.ResourceObjectList
		expectedLCs *crds.DRLCObjectList
		config      *common.BoskosConfig
	}{
		{
			name: "migration from mason resource to dynamic resource does not delete resource",
			currentRes: []runtime.Object{
				newResource("res-1", "t", "", "", startTime),
				newResource("dt_1", "mason", "", "", startTime),
				newResource("dt_2", "mason", "", "", startTime),
			},
			config: &common.BoskosConfig{
				Resources: []common.ResourceEntry{
					{
						Type:  "t",
						Names: []string{"res-1"},
					},
					{
						Type:     "mason",
						MinCount: 2,
						MaxCount: 4,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("res-1", "t", common.Free, "", startTime),
				*newResource("dt_1", "mason", common.Free, "", startTime),
				*newResource("dt_2", "mason", common.Free, "", startTime),
			},
			},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "mason"},
					Spec: crds.DRLCSpec{
						MinCount: 2,
						MaxCount: 4,
					}},
			}},
		},
		{
			name: "empty",
		},
		{
			name: "append",
			currentRes: []runtime.Object{
				newResource("res-1", "t", "", "", startTime),
			},
			config: &common.BoskosConfig{
				Resources: []common.ResourceEntry{
					{
						Type:  "t",
						Names: []string{"res-1", "res-2"},
					},
					{
						Type:     "dt",
						MinCount: 1,
						MaxCount: 2,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("res-1", "t", common.Free, "", startTime),
				*newResource("res-2", "t", common.Free, "", fakeNow),
				*newResource("new-dynamic-res-1", "dt", common.Free, "", fakeNow),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					},
				},
			}},
		},
		{
			name: "should not change anything",
			currentRes: []runtime.Object{
				newResource("res-1", "t", "", "", startTime),
				newResource("dt_1", "dt", "", "", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					},
				},
			},
			config: &common.BoskosConfig{
				Resources: []common.ResourceEntry{
					{
						Type:  "t",
						Names: []string{"res-1"},
					},
					{
						Type:     "dt",
						MinCount: 1,
						MaxCount: 2,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("res-1", "t", "", "", startTime),
				*newResource("dt_1", "dt", "", "", startTime),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					},
				},
			}},
		},
		{
			name: "delete, lifecycle should not delete dynamic res until all associated resources are gone",
			currentRes: []runtime.Object{
				newResource("res", "t", "", "", startTime),
				newResource("dt_1", "dt", "", "", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					},
				},
			},
			config: &common.BoskosConfig{},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("dt_1", "dt", common.ToBeDeleted, "", fakeNow),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 0,
						MaxCount: 0,
					},
				},
			}},
		},
		{
			name: "delete, life cycle should be deleted as all resources are deleted",
			currentRes: []runtime.Object{
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					},
				},
			},
			config: &common.BoskosConfig{},
		},
		{
			name: "delete busy",
			currentRes: []runtime.Object{
				newResource("res", "t", common.Busy, "o", startTime),
				newResource("dt_1", "dt", common.Busy, "o", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					},
				},
			},
			config: &common.BoskosConfig{},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("res", "t", common.Busy, "o", startTime),
				*newResource("dt_1", "dt", common.Busy, "o", startTime),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 0,
						MaxCount: 0,
					},
				},
			}},
		},
		{
			name: "append and delete",
			currentRes: []runtime.Object{
				newResource("res-1", "t", common.Tombstone, "", startTime),
				newResource("dt_1", "dt", common.ToBeDeleted, "", startTime),
				newResource("dt_2", "dt", "", "", startTime),
				newResource("dt_3", "dt", "", "", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 3,
					},
				},
			},
			config: &common.BoskosConfig{
				Resources: []common.ResourceEntry{
					{
						Type:  "t",
						Names: []string{"res-2"},
					},
					{
						Type:     "dt",
						MinCount: 1,
						MaxCount: 2,
					},
					{
						Type:     "dt2",
						MinCount: 1,
						MaxCount: 2,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("res-2", "t", common.Free, "", fakeNow),
				*newResource("dt_1", "dt", common.ToBeDeleted, "", startTime),
				*newResource("dt_2", "dt", common.Free, "", startTime),
				*newResource("dt_3", "dt", common.Free, "", startTime),
				*newResource("new-dynamic-res-1", "dt2", common.Free, "", fakeNow),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt2"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					},
				},
			}},
		},
		{
			name: "append and delete busy",
			currentRes: []runtime.Object{
				newResource("res-1", "t", common.Busy, "o", startTime),
				newResource("dt_1", "dt", "", "", startTime),
				newResource("dt_2", "dt", common.Tombstone, "", startTime),
				newResource("dt_3", "dt", common.Busy, "o", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 3,
					},
				},
			},
			config: &common.BoskosConfig{
				Resources: []common.ResourceEntry{
					{
						Type:  "t",
						Names: []string{"res-2"},
					},
					{
						Type:     "dt",
						MinCount: 1,
						MaxCount: 2,
					},
					{
						Type:     "dt2",
						MinCount: 1,
						MaxCount: 2,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("res-1", "t", common.Busy, "o", startTime),
				*newResource("res-2", "t", common.Free, "", fakeNow),
				*newResource("dt_1", "dt", common.Free, "", startTime),
				*newResource("dt_3", "dt", common.Busy, "o", startTime),
				*newResource("new-dynamic-res-1", "dt2", common.Free, "", fakeNow),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt2"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					},
				},
			}},
		},
		{
			name: "append/delete mixed type",
			currentRes: []runtime.Object{
				newResource("res-1", "t", common.Tombstone, "", startTime),
			},
			config: &common.BoskosConfig{
				Resources: []common.ResourceEntry{
					{
						Type:  "t",
						Names: []string{"res-2"},
					},
					{
						Type:  "t2",
						Names: []string{"res-3"},
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("res-2", "t", "free", "", fakeNow),
				*newResource("res-3", "t2", "free", "", fakeNow),
			}},
		},
		{
			name: "delete expired resource",
			currentRes: []runtime.Object{
				setExpiration(
					newResource("dt_1", "dt", "", "", startTime),
					startTime),
				newResource("dt_2", "dt", "", "", startTime),
				setExpiration(
					newResource("dt_3", "dt", common.Tombstone, "", startTime),
					startTime),
				newResource("dt_4", "dt", "", "", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 2,
						MaxCount: 4,
					},
				},
			},
			config: &common.BoskosConfig{
				Resources: []common.ResourceEntry{
					{
						Type:     "dt",
						MinCount: 2,
						MaxCount: 4,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*setExpiration(
					newResource("dt_1", "dt", common.ToBeDeleted, "", fakeNow),
					startTime),
				*newResource("dt_2", "dt", common.Free, "", startTime),
				*newResource("dt_4", "dt", common.Free, "", startTime),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 2,
						MaxCount: 4,
					},
				},
			}},
		},
		{
			name: "delete expired resource / do not delete busy",
			currentRes: []runtime.Object{
				setExpiration(
					newResource("dt_1", "dt", common.Tombstone, "", startTime),
					startTime),
				newResource("dt_2", "dt", "", "", startTime),
				setExpiration(
					newResource("dt_3", "dt", common.Busy, "o", startTime),
					startTime),
				newResource("dt_4", "dt", common.Busy, "o", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 4,
					},
				},
			},
			config: &common.BoskosConfig{
				Resources: []common.ResourceEntry{
					{
						Type:     "dt",
						MinCount: 1,
						MaxCount: 3,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("dt_2", "dt", common.Free, "", startTime),
				*setExpiration(
					newResource("dt_3", "dt", common.Busy, "o", startTime),
					startTime),
				*newResource("dt_4", "dt", common.Busy, "o", startTime),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 3,
					},
				},
			}},
		},
		{
			name: "delete expired resource, recreate up to Min",
			currentRes: []runtime.Object{
				setExpiration(
					newResource("dt_1", "dt", "", "", startTime),
					startTime),
				newResource("dt_2", "dt", "", "", startTime),
				setExpiration(
					newResource("dt_3", "dt", common.Tombstone, "", startTime),
					startTime),
				newResource("dt_4", "dt", "", "", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 4,
						MaxCount: 6,
					},
				},
			},
			config: &common.BoskosConfig{
				Resources: []common.ResourceEntry{
					{
						Type:     "dt",
						MinCount: 4,
						MaxCount: 6,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*setExpiration(
					newResource("dt_1", "dt", common.ToBeDeleted, "", fakeNow),
					startTime),
				*newResource("new-dynamic-res-1", "dt", common.Free, "", fakeNow),
				*newResource("dt_2", "dt", common.Free, "", startTime),
				*newResource("dt_4", "dt", common.Free, "", startTime),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 4,
						MaxCount: 6,
					},
				},
			}},
		},
		{
			name: "decrease max count with resources being deleted",
			currentRes: []runtime.Object{
				newResource("dt_1", "dt", common.Free, "", startTime),
				newResource("dt_2", "dt", common.Free, "", startTime),
				newResource("dt_3", "dt", common.Free, "", startTime),
				newResource("dt_4", "dt", common.ToBeDeleted, "", startTime),
				newResource("dt_5", "dt", common.Tombstone, "", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 6,
					},
				},
			},
			config: &common.BoskosConfig{
				Resources: []common.ResourceEntry{
					{
						Type:     "dt",
						MinCount: 1,
						MaxCount: 1,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("dt_1", "dt", common.Free, "", startTime),
				*newResource("dt_2", "dt", common.ToBeDeleted, "", fakeNow),
				*newResource("dt_3", "dt", common.ToBeDeleted, "", fakeNow),
				*newResource("dt_4", "dt", common.ToBeDeleted, "", startTime),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 1,
					},
				},
			}},
		},
		{
			name: "increase min count with resources being deleted",
			currentRes: []runtime.Object{
				newResource("dt_1", "dt", common.Free, "", startTime),
				newResource("dt_2", "dt", common.ToBeDeleted, "", startTime),
				newResource("dt_3", "dt", common.ToBeDeleted, "", startTime),
				newResource("dt_4", "dt", common.Tombstone, "", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 6,
					},
				},
			},
			config: &common.BoskosConfig{
				Resources: []common.ResourceEntry{
					{
						Type:     "dt",
						MinCount: 4,
						MaxCount: 6,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("dt_1", "dt", common.Free, "", startTime),
				*newResource("dt_2", "dt", common.ToBeDeleted, "", startTime),
				*newResource("dt_3", "dt", common.ToBeDeleted, "", startTime),
				*newResource("new-dynamic-res-1", "dt", common.Free, "", fakeNow),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 4,
						MaxCount: 6,
					},
				},
			}},
		},
		{
			name: "Dynamic boskos config adopts pre-existing static resources",
			config: &common.BoskosConfig{Resources: []common.ResourceEntry{{
				Type:     "test-resource",
				State:    common.Free,
				MinCount: 10,
				MaxCount: 10,
			}}},
			currentRes: []runtime.Object{
				newResource("test-resource-0", "test-resource", common.Free, "", startTime),
				newResource("test-resource-1", "test-resource", common.Free, "", startTime),
				newResource("test-resource-2", "test-resource", common.Free, "", startTime),
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("test-resource-0", "test-resource", common.Free, "", startTime),
				*newResource("test-resource-1", "test-resource", common.Free, "", startTime),
				*newResource("test-resource-2", "test-resource", common.Free, "", startTime),
				*newResource("new-dynamic-res-1", "test-resource", common.Free, "", fakeNow),
				*newResource("new-dynamic-res-2", "test-resource", common.Free, "", fakeNow),
				*newResource("new-dynamic-res-3", "test-resource", common.Free, "", fakeNow),
				*newResource("new-dynamic-res-4", "test-resource", common.Free, "", fakeNow),
				*newResource("new-dynamic-res-5", "test-resource", common.Free, "", fakeNow),
				*newResource("new-dynamic-res-6", "test-resource", common.Free, "", fakeNow),
				*newResource("new-dynamic-res-7", "test-resource", common.Free, "", fakeNow),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{{
				ObjectMeta: metav1.ObjectMeta{Name: "test-resource"},
				Spec: crds.DRLCSpec{
					InitialState: common.Free,
					MinCount:     10,
					MaxCount:     10,
				},
			}}},
		},
		{
			name: "Dynamic config that adopted static resources gets changed back to static",
			config: &common.BoskosConfig{Resources: []common.ResourceEntry{{
				Type:  "test-resource",
				State: common.Free,
				Names: []string{
					"test-resource-0",
					"test-resource-1",
					"test-resource-2",
				},
			}}},
			currentRes: []runtime.Object{
				newResource("test-resource-0", "test-resource", common.Free, "", startTime),
				newResource("test-resource-1", "test-resource", common.Free, "", startTime),
				newResource("test-resource-2", "test-resource", common.Free, "", startTime),
				newResource("fd957edc-4148-49e8-af83-53d38bcd4e54", "test-resource", common.Free, "", startTime),
				newResource("fd957edc-4148-49e8-af83-53d38bcd4e55", "test-resource", common.Free, "", startTime),
				newResource("fd957edc-4148-49e8-af83-53d38bcd4e56", "test-resource", common.Free, "", startTime),
				newResource("fd957edc-4148-49e8-af83-53d38bcd4e57", "test-resource", common.Free, "", startTime),
				newResource("fd957edc-4148-49e8-af83-53d38bcd4e58", "test-resource", common.Free, "", startTime),
				newResource("fd957edc-4148-49e8-af83-53d38bcd4e59", "test-resource", common.Free, "", startTime),
				newResource("fd957edc-4148-49e8-af83-53d38bcd4e60", "test-resource", common.Free, "", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "test-resource"},
					Spec: crds.DRLCSpec{
						MinCount: 10,
						MaxCount: 10,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("test-resource-0", "test-resource", common.Free, "", startTime),
				*newResource("test-resource-1", "test-resource", common.Free, "", startTime),
				*newResource("test-resource-2", "test-resource", common.Free, "", startTime),
				*newResource("fd957edc-4148-49e8-af83-53d38bcd4e54", "test-resource", common.ToBeDeleted, "", fakeNow),
				*newResource("fd957edc-4148-49e8-af83-53d38bcd4e55", "test-resource", common.ToBeDeleted, "", fakeNow),
				*newResource("fd957edc-4148-49e8-af83-53d38bcd4e56", "test-resource", common.ToBeDeleted, "", fakeNow),
				*newResource("fd957edc-4148-49e8-af83-53d38bcd4e57", "test-resource", common.ToBeDeleted, "", fakeNow),
				*newResource("fd957edc-4148-49e8-af83-53d38bcd4e58", "test-resource", common.ToBeDeleted, "", fakeNow),
				*newResource("fd957edc-4148-49e8-af83-53d38bcd4e59", "test-resource", common.ToBeDeleted, "", fakeNow),
				*newResource("fd957edc-4148-49e8-af83-53d38bcd4e60", "test-resource", common.ToBeDeleted, "", fakeNow),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{{
				ObjectMeta: metav1.ObjectMeta{Name: "test-resource"},
				Spec: crds.DRLCSpec{
					MinCount: 0,
					MaxCount: 0,
				},
			}}},
		},
		{
			name: "move names over types",
			config: &common.BoskosConfig{Resources: []common.ResourceEntry{
				{
					Type:  "type-0",
					State: common.Free,
					Names: []string{
						"res-0-type-0",
					},
				},
				{
					Type:  "type-1",
					State: common.Free,
					Names: []string{
						"res-1-type-0",
						"res-0-type-1",
					},
				},
			}},
			currentRes: []runtime.Object{
				newResource("res-0-type-0", "type-0", common.Free, "", startTime),
				newResource("res-1-type-0", "type-0", common.Free, "", startTime),
				newResource("res-0-type-1", "type-1", common.Free, "", startTime),
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("res-0-type-0", "type-0", common.Free, "", startTime),
				*newResource("res-1-type-0", "type-1", common.Free, "", fakeNow),
				*newResource("res-0-type-1", "type-1", common.Free, "", startTime),
			}},
		},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := makeTestRanch(tc.currentRes)
			if err := c.Storage.SyncResources(tc.config); err != nil {
				t.Fatalf("syncResources failed: %v, type: %T", err, err)
			}
			resources, err := c.Storage.GetResources()
			if err != nil {
				t.Fatalf("failed to get resources: %v", err)
			}
			if tc.expectedRes == nil {
				tc.expectedRes = &crds.ResourceObjectList{}
			}
			sortResourcesLists(tc.expectedRes, resources)
			for idx := range tc.expectedRes.Items {
				tc.expectedRes.Items[idx].Namespace = testNS
				if tc.expectedRes.Items[idx].Status.UserData == nil {
					tc.expectedRes.Items[idx].Status.UserData = map[string]string{}
				}
			}
			if diff := compareResourceObjectsLists(resources, tc.expectedRes); diff != "" {
				t.Errorf("received resource differs from expected, diff: %v", diff)
			}
			lfs, err := c.Storage.GetDynamicResourceLifeCycles()
			if err != nil {
				t.Fatalf("failed to get dynamic resources life cycles: %v", err)
			}
			if tc.expectedLCs == nil {
				tc.expectedLCs = &crds.DRLCObjectList{}
			}
			for idx := range tc.expectedLCs.Items {
				tc.expectedLCs.Items[idx].Namespace = testNS
			}
			if diff := compareDRLCLists(lfs, tc.expectedLCs); diff != "" {
				t.Errorf("received drlc do not match expected, diff: %s", diff)
			}
		})
	}
}

func TestUpdateAllDynamicResources(t *testing.T) {
	var testcases = []struct {
		name        string
		currentRes  []runtime.Object
		expectedRes *crds.ResourceObjectList
		expectedLCs *crds.DRLCObjectList
	}{
		{
			name: "empty",
		},
		{
			name: "do nothing",
			currentRes: []runtime.Object{
				newResource("dt_1", "dt", common.Free, "", startTime),
				newResource("t_1", "t", common.Free, "", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 4,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("dt_1", "dt", common.Free, "", startTime),
				*newResource("t_1", "t", common.Free, "", startTime),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 4,
					}},
			}},
		},
		{
			name: "delete expired free resources",
			currentRes: []runtime.Object{
				setExpiration(
					newResource("dt_1", "dt", common.Free, "", startTime),
					metav1.Time{Time: fakeNow.Add(time.Hour)}),
				setExpiration(
					newResource("dt_2", "dt", common.Free, "", startTime),
					startTime),
				setExpiration(
					newResource("dt_3", "dt", common.Busy, "owner", startTime),
					startTime),
				setExpiration(
					newResource("dt_4", "dt", common.ToBeDeleted, "", startTime),
					startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 4,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				// Unchanged because expiration is in the future
				*setExpiration(
					newResource("dt_1", "dt", common.Free, "", startTime),
					metav1.Time{Time: fakeNow.Add(time.Hour)}),
				// Newly deleted
				*setExpiration(
					newResource("dt_2", "dt", common.ToBeDeleted, "", fakeNow),
					startTime),
				// Unchanged because owned
				*setExpiration(
					newResource("dt_3", "dt", common.Busy, "owner", startTime),
					startTime),
				// Unchanged because already being deleted
				*setExpiration(
					newResource("dt_4", "dt", common.ToBeDeleted, "", startTime),
					startTime),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 4,
					}},
			}},
		},
		{
			name: "no dynamic resources, nothing to make",
			currentRes: []runtime.Object{
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 0,
						MaxCount: 4,
					},
				},
			},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 0,
						MaxCount: 4,
					}},
			}},
		},
		{
			name: "no dynamic resources, make some",
			currentRes: []runtime.Object{
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 2,
						MaxCount: 4,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("new-dynamic-res-1", "dt", common.Free, "", fakeNow),
				*newResource("new-dynamic-res-2", "dt", common.Free, "", fakeNow),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 2,
						MaxCount: 4,
					}},
			}},
		},
		{
			name: "scale down",
			currentRes: []runtime.Object{
				newResource("dt_1", "dt", common.Free, "", startTime),
				newResource("dt_2", "dt", common.Free, "", startTime),
				newResource("dt_4", "dt", common.Busy, "owner", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("dt_1", "dt", common.Free, "", startTime),
				*newResource("dt_2", "dt", common.ToBeDeleted, "", fakeNow),
				*newResource("dt_4", "dt", common.Busy, "owner", startTime),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					}},
			}},
		},
		{
			name: "replace some resources",
			currentRes: []runtime.Object{
				newResource("dt_1", "dt", common.Free, "", startTime),
				newResource("dt_2", "dt", common.Busy, "owner", startTime),
				newResource("dt_3", "dt", common.ToBeDeleted, "", startTime),
				newResource("dt_4", "dt", common.Tombstone, "", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 4,
						MaxCount: 8,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("dt_1", "dt", common.Free, "", startTime),
				*newResource("dt_2", "dt", common.Busy, "owner", startTime),
				*newResource("dt_3", "dt", common.ToBeDeleted, "", startTime),
				*newResource("new-dynamic-res-1", "dt", common.Free, "", fakeNow),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 4,
						MaxCount: 8,
					}},
			}},
		},
		{
			name: "scale down, busy > maxcount",
			currentRes: []runtime.Object{
				newResource("dt_1", "dt", common.Free, "", startTime),
				newResource("dt_2", "dt", common.Busy, "owner", startTime),
				newResource("dt_3", "dt", common.Busy, "owner", startTime),
				newResource("dt_4", "dt", common.Busy, "owner", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("dt_1", "dt", common.ToBeDeleted, "", fakeNow),
				*newResource("dt_2", "dt", common.Busy, "owner", startTime),
				*newResource("dt_3", "dt", common.Busy, "owner", startTime),
				*newResource("dt_4", "dt", common.Busy, "owner", startTime),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 1,
						MaxCount: 2,
					}},
			}},
		},
		{
			name: "delete all free when DRLC is being removed",
			currentRes: []runtime.Object{
				newResource("dt_1", "dt", common.Free, "", startTime),
				newResource("dt_2", "dt", common.Free, "", startTime),
				newResource("dt_3", "dt", common.Tombstone, "", startTime),
				newResource("dt_4", "dt", common.Busy, "owner", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 0,
						MaxCount: 0,
					},
				},
			},
			expectedRes: &crds.ResourceObjectList{Items: []crds.ResourceObject{
				*newResource("dt_1", "dt", common.ToBeDeleted, "", fakeNow),
				*newResource("dt_2", "dt", common.ToBeDeleted, "", fakeNow),
				*newResource("dt_4", "dt", common.Busy, "owner", startTime),
			}},
			expectedLCs: &crds.DRLCObjectList{Items: []crds.DRLCObject{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 0,
						MaxCount: 0,
					}},
			}},
		},
		{
			name: "delete DRLC when no resources remain",
			currentRes: []runtime.Object{
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 0,
						MaxCount: 0,
					},
				},
			},
		},
		{
			name: "delete DRLC when all resources tombstoned",
			currentRes: []runtime.Object{
				newResource("dt_1", "dt", common.Tombstone, "", startTime),
				newResource("dt_3", "dt", common.Tombstone, "", startTime),
				&crds.DRLCObject{
					ObjectMeta: metav1.ObjectMeta{Name: "dt"},
					Spec: crds.DRLCSpec{
						MinCount: 0,
						MaxCount: 0,
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := makeTestRanch(tc.currentRes)
			err := c.Storage.UpdateAllDynamicResources(nil)
			if err != nil {
				t.Fatalf("error updating dynamic resources: %v", err)
			}
			if tc.expectedRes == nil {
				tc.expectedRes = &crds.ResourceObjectList{}
			}
			if tc.expectedLCs == nil {
				tc.expectedLCs = &crds.DRLCObjectList{}
			}
			for idx := range tc.expectedRes.Items {
				tc.expectedRes.Items[idx].Namespace = testNS
			}
			for idx := range tc.expectedLCs.Items {
				tc.expectedLCs.Items[idx].Namespace = testNS
			}
			resources, err := c.Storage.GetResources()
			if err != nil {
				t.Fatalf("failed to get resources: %v", err)
			}
			for idx := range tc.expectedRes.Items {
				// needed to prevent test failures due to nil != empty
				if tc.expectedRes.Items[idx].Status.UserData == nil {
					tc.expectedRes.Items[idx].Status.UserData = map[string]string{}
				}
			}

			if diff := compareResourceObjectsLists(resources, tc.expectedRes); diff != "" {
				t.Errorf("diff:\n%v", diff)
			}
			lfs, err := c.Storage.GetDynamicResourceLifeCycles()
			if err != nil {
				t.Fatalf("failed to get dynamic resource life cycles: %v", err)
			}

			if diff := compareDRLCLists(lfs, tc.expectedLCs); diff != "" {
				t.Errorf("diff: %s", diff)
			}
		})
	}
}

func compareResourceObjectsLists(a, b *crds.ResourceObjectList) string {
	sortResourcesLists(a, b)
	a.TypeMeta = metav1.TypeMeta{}
	a.ResourceVersion = ""
	b.ResourceVersion = ""
	b.TypeMeta = metav1.TypeMeta{}
	for idx := range a.Items {
		a.Items[idx].TypeMeta = metav1.TypeMeta{}
		a.Items[idx].ResourceVersion = ""
		if a.Items[idx].Status.UserData == nil {
			a.Items[idx].Status.UserData = map[string]string{}
		}
	}
	for idx := range b.Items {
		b.Items[idx].TypeMeta = metav1.TypeMeta{}
		b.Items[idx].ResourceVersion = ""
		if b.Items[idx].Status.UserData == nil {
			b.Items[idx].Status.UserData = map[string]string{}
		}
	}
	return cmp.Diff(a, b, timeComparer, cmp.AllowUnexported(sync.Map{}, sync.Mutex{}, atomic.Value{}))
}

var timeComparer = cmp.Comparer(func(a, b time.Time) (r bool) {
	return a.Equal(b)
})

func compareDRLCLists(a, b *crds.DRLCObjectList) string {
	sortDRLCList(a, b)
	a.TypeMeta = metav1.TypeMeta{}
	a.ResourceVersion = ""
	b.ResourceVersion = ""
	b.TypeMeta = metav1.TypeMeta{}
	for idx := range a.Items {
		a.Items[idx].TypeMeta = metav1.TypeMeta{}
		a.Items[idx].ResourceVersion = ""
	}
	for idx := range b.Items {
		b.Items[idx].TypeMeta = metav1.TypeMeta{}
		b.Items[idx].ResourceVersion = ""
	}
	return cmp.Diff(a, b, timeComparer)
}

func newResource(name, rtype, state, owner string, t metav1.Time) *crds.ResourceObject {
	if state == "" {
		state = common.Free
	}

	return &crds.ResourceObject{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: crds.ResourceSpec{
			Type: rtype,
		},
		Status: crds.ResourceStatus{
			State:      state,
			Owner:      owner,
			LastUpdate: t,
			UserData:   map[string]string{},
		},
	}
}

func sortResourcesLists(rls ...*crds.ResourceObjectList) {
	for _, rl := range rls {
		sort.Slice(rl.Items, func(i, j int) bool {
			return rl.Items[i].Name < rl.Items[j].Name
		})
		if len(rl.Items) == 0 {
			rl.Items = nil
		}
	}
}

func sortDRLCList(drlcs ...*crds.DRLCObjectList) {
	for _, drlc := range drlcs {
		sort.Slice(drlc.Items, func(i, j int) bool {
			return drlc.Items[i].Name < drlc.Items[j].Name
		})
		if len(drlc.Items) == 0 {
			drlc.Items = nil
		}
	}
}

// onceConflictingClient returns an IsConflict error on the first Update request it receives. It
// is used to verify that there is retrying for conflicts in place.
type onceConflictingClient struct {
	didConflict bool
	ctrlruntimeclient.Client
}

func (occ *onceConflictingClient) Update(ctx context.Context, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.UpdateOption) error {
	if !occ.didConflict {
		occ.didConflict = true
		return kerrors.NewConflict(schema.GroupResource{}, "obj", errors.New("conflicting as requested"))
	}
	return occ.Client.Update(ctx, obj, opts...)
}

func TestIsConflict(t *testing.T) {
	gr := schema.GroupResource{}
	testCases := []struct {
		name        string
		err         error
		shouldMatch bool
	}{
		{
			name:        "direct match",
			err:         kerrors.NewConflict(gr, "test", errors.New("invalid")),
			shouldMatch: true,
		},
		{
			name: "no match",
			err:  errors.New("something else"),
		},
		{
			name:        "nested match",
			err:         fmt.Errorf("we found an error: %w", fmt.Errorf("here: %w", kerrors.NewConflict(gr, "test", errors.New("invalid")))),
			shouldMatch: true,
		},
		{
			name: "nested, no match",
			err:  fmt.Errorf("We also found this: %w", fmt.Errorf("there: %w", errors.New("nope"))),
		},
		{
			name:        "aggregate, match",
			err:         utilerrors.NewAggregate([]error{errors.New("some err"), kerrors.NewConflict(gr, "test", errors.New("invalid"))}),
			shouldMatch: true,
		},
		{
			name: "aggregate, no match",
			err:  utilerrors.NewAggregate([]error{errors.New("some err"), errors.New("other err")}),
		},
		{
			name:        "wrapped aggregate, match",
			err:         fmt.Errorf("err: %w", fmt.Errorf("didn't work: %w", utilerrors.NewAggregate([]error{errors.New("some err"), kerrors.NewConflict(gr, "test", errors.New("invalid"))}))),
			shouldMatch: true,
		},
		{
			name: "wrapped aggregate, no match",
			err:  fmt.Errorf("err: %w", fmt.Errorf("didn't work: %w", utilerrors.NewAggregate([]error{errors.New("some err"), errors.New("nope")}))),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if result := isConflict(tc.err); result != tc.shouldMatch {
				t.Errorf("expected match: %t, got match: %t", tc.shouldMatch, result)
			}
		})
	}
}
