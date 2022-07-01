/*
Copyright 2022 The Kubernetes Authors.

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
	"reflect"
	"testing"
	"time"
)

type recordedAction struct {
	resource    string
	sourceState string
	exp         time.Duration
	targetState string
}

type fakeBoskosRecorder struct {
	recordedActions []recordedAction
}

func (f *fakeBoskosRecorder) Reset(resource, sourceState string, exp time.Duration, targetState string) (map[string]string, error) {
	f.recordedActions = append(f.recordedActions, recordedAction{resource, sourceState, exp, targetState})
	return nil, nil
}

func TestSync(t *testing.T) {
	for _, tc := range []struct {
		name            string
		reap            *reaper
		res             string
		expectedActions []recordedAction
	}{
		{
			name: "Defaults",
			reap: &reaper{
				extraSourceStates: nil,
				expiryDuration:    30 * time.Minute,
				targetState:       "dirty",
			},
			res:             "foo",
			expectedActions: []recordedAction{{"foo", "busy", 30 * time.Minute, "dirty"}, {"foo", "cleaning", 30 * time.Minute, "dirty"}, {"foo", "leased", 30 * time.Minute, "dirty"}},
		},
		{
			name: "Cleaning up extra state after long time",
			reap: &reaper{
				extraSourceStates: []string{"manual", "foobar"},
				expiryDuration:    14 * 24 * time.Hour,
				targetState:       "dirty",
			},
			res:             "bar",
			expectedActions: []recordedAction{{"bar", "busy", 14 * 24 * time.Hour, "dirty"}, {"bar", "cleaning", 14 * 24 * time.Hour, "dirty"}, {"bar", "leased", 14 * 24 * time.Hour, "dirty"}, {"bar", "manual", 14 * 24 * time.Hour, "dirty"}, {"bar", "foobar", 14 * 24 * time.Hour, "dirty"}},
		},
	} {
		fakeBoskos := &fakeBoskosRecorder{}
		tc.reap.sync(fakeBoskos, tc.res)
		if !reflect.DeepEqual(tc.expectedActions, fakeBoskos.recordedActions) {
			t.Errorf("[%s]: tc.expectedActions and fakeBoskos.recordedActions are not equal, was: tc.expectedActions %v, fakeBoskos.recordedActions %v", tc.name, tc.expectedActions, fakeBoskos.recordedActions)
		}
	}
}
