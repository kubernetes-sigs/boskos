/*
Copyright 2026 The Kubernetes Authors.

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

package resources

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3files"
	s3filestypes "github.com/aws/aws-sdk-go-v2/service/s3files/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/google/go-cmp/cmp"
)

type mockS3FilesPage struct {
	fileSystems []s3filestypes.ListFileSystemsDescription
	nextToken   *string
	err         error
}

type mockS3FilesClient struct {
	pages     map[string]mockS3FilesPage
	tags      map[string][]s3filestypes.Tag
	tagErrors map[string]error
	// mountTargets holds the successive ListMountTargets responses for each
	// file system. The last response repeats once the list is exhausted.
	mountTargets        map[string][][]s3filestypes.ListMountTargetsDescription
	mountTargetCalls    map[string]int
	accessPoints        map[string][]s3filestypes.ListAccessPointsDescription
	accessPointErrors   map[string]error
	deleteFSErrors      map[string]error
	deletedFileSystems  []*s3files.DeleteFileSystemInput
	deletedMountTarget  []string
	deletedAccessPoints []string
	listTokens          []string
	events              []string
}

func (client *mockS3FilesClient) ListFileSystems(_ context.Context, input *s3files.ListFileSystemsInput, _ ...func(*s3files.Options)) (*s3files.ListFileSystemsOutput, error) {
	token := aws.ToString(input.NextToken)
	client.listTokens = append(client.listTokens, token)
	client.events = append(client.events, "list:"+token)

	page, ok := client.pages[token]
	if !ok {
		return nil, fmt.Errorf("unexpected pagination token %q", token)
	}
	if page.err != nil {
		return nil, page.err
	}
	return &s3files.ListFileSystemsOutput{
		FileSystems: page.fileSystems,
		NextToken:   page.nextToken,
	}, nil
}

func (client *mockS3FilesClient) ListTagsForResource(_ context.Context, input *s3files.ListTagsForResourceInput, _ ...func(*s3files.Options)) (*s3files.ListTagsForResourceOutput, error) {
	id := aws.ToString(input.ResourceId)
	if err := client.tagErrors[id]; err != nil {
		return nil, err
	}
	return &s3files.ListTagsForResourceOutput{Tags: client.tags[id]}, nil
}

func (client *mockS3FilesClient) ListMountTargets(_ context.Context, input *s3files.ListMountTargetsInput, _ ...func(*s3files.Options)) (*s3files.ListMountTargetsOutput, error) {
	id := aws.ToString(input.FileSystemId)
	if client.mountTargetCalls == nil {
		client.mountTargetCalls = map[string]int{}
	}
	responses := client.mountTargets[id]
	call := client.mountTargetCalls[id]
	client.mountTargetCalls[id] = call + 1
	if len(responses) == 0 {
		return &s3files.ListMountTargetsOutput{}, nil
	}
	if call >= len(responses) {
		call = len(responses) - 1
	}
	return &s3files.ListMountTargetsOutput{MountTargets: responses[call]}, nil
}

func (client *mockS3FilesClient) DeleteMountTarget(_ context.Context, input *s3files.DeleteMountTargetInput, _ ...func(*s3files.Options)) (*s3files.DeleteMountTargetOutput, error) {
	id := aws.ToString(input.MountTargetId)
	client.deletedMountTarget = append(client.deletedMountTarget, id)
	client.events = append(client.events, "delete-mt:"+id)
	return &s3files.DeleteMountTargetOutput{}, nil
}

func (client *mockS3FilesClient) ListAccessPoints(_ context.Context, input *s3files.ListAccessPointsInput, _ ...func(*s3files.Options)) (*s3files.ListAccessPointsOutput, error) {
	id := aws.ToString(input.FileSystemId)
	if err := client.accessPointErrors[id]; err != nil {
		return nil, err
	}
	return &s3files.ListAccessPointsOutput{AccessPoints: client.accessPoints[id]}, nil
}

func (client *mockS3FilesClient) DeleteAccessPoint(_ context.Context, input *s3files.DeleteAccessPointInput, _ ...func(*s3files.Options)) (*s3files.DeleteAccessPointOutput, error) {
	id := aws.ToString(input.AccessPointId)
	client.deletedAccessPoints = append(client.deletedAccessPoints, id)
	client.events = append(client.events, "delete-ap:"+id)
	return &s3files.DeleteAccessPointOutput{}, nil
}

func (client *mockS3FilesClient) DeleteFileSystem(_ context.Context, input *s3files.DeleteFileSystemInput, _ ...func(*s3files.Options)) (*s3files.DeleteFileSystemOutput, error) {
	copied := *input
	client.deletedFileSystems = append(client.deletedFileSystems, &copied)
	id := aws.ToString(input.FileSystemId)
	client.events = append(client.events, "delete:"+id)
	if err := client.deleteFSErrors[id]; err != nil {
		return nil, err
	}
	return &s3files.DeleteFileSystemOutput{}, nil
}

var noS3FilesWait = s3FilesMountTargetWait{attempts: 3, interval: 0}

func TestMarkAndSweepS3FilesFileSystemsDeletesAccessPointsBeforeMountTargets(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	client := &mockS3FilesClient{
		pages: map[string]mockS3FilesPage{
			"": {fileSystems: []s3filestypes.ListFileSystemsDescription{
				testS3FilesFileSystem("fs-old", s3filestypes.LifeCycleStateAvailable, old),
			}},
		},
		accessPoints: map[string][]s3filestypes.ListAccessPointsDescription{
			"fs-old": {
				testS3FilesAccessPoint("fsap-1", s3filestypes.LifeCycleStateAvailable),
				testS3FilesAccessPoint("fsap-2", s3filestypes.LifeCycleStateDeleting),
				testS3FilesAccessPoint("fsap-3", s3filestypes.LifeCycleStateDeleted),
			},
		},
		mountTargets: map[string][][]s3filestypes.ListMountTargetsDescription{
			"fs-old": {
				{testS3FilesMountTarget("fsmt-1", s3filestypes.LifeCycleStateAvailable)},
				{},
			},
		},
	}

	if err := markAndSweepS3FilesFileSystems(client, Options{}, NewSet(0), noS3FilesWait); err != nil {
		t.Fatalf("markAndSweepS3FilesFileSystems() error = %v", err)
	}
	wantEvents := []string{"list:", "delete-ap:fsap-1", "delete-mt:fsmt-1", "delete:fs-old"}
	if diff := cmp.Diff(wantEvents, client.events); diff != "" {
		t.Errorf("API call order mismatch (-want +got):\n%s", diff)
	}
}

func TestMarkAndSweepS3FilesFileSystemsContinuesWhenAccessPointsCannotBeListed(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	client := &mockS3FilesClient{
		pages: map[string]mockS3FilesPage{
			"": {fileSystems: []s3filestypes.ListFileSystemsDescription{
				testS3FilesFileSystem("fs-old", s3filestypes.LifeCycleStateAvailable, old),
			}},
		},
		accessPointErrors: map[string]error{"fs-old": errors.New("throttled")},
	}

	if err := markAndSweepS3FilesFileSystems(client, Options{}, NewSet(0), noS3FilesWait); err != nil {
		t.Fatalf("markAndSweepS3FilesFileSystems() error = %v", err)
	}
	if diff := cmp.Diff([]string{"fs-old"}, deletedS3FilesIDs(client)); diff != "" {
		t.Errorf("deleted file systems mismatch (-want +got):\n%s", diff)
	}
}

func TestMarkAndSweepS3FilesFileSystemsIgnoresDeletedMountTargets(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	client := &mockS3FilesClient{
		pages: map[string]mockS3FilesPage{
			"": {fileSystems: []s3filestypes.ListFileSystemsDescription{
				testS3FilesFileSystem("fs-old", s3filestypes.LifeCycleStateAvailable, old),
			}},
		},
		mountTargets: map[string][][]s3filestypes.ListMountTargetsDescription{
			"fs-old": {{testS3FilesMountTarget("fsmt-gone", s3filestypes.LifeCycleStateDeleted)}},
		},
	}

	if err := markAndSweepS3FilesFileSystems(client, Options{}, NewSet(0), noS3FilesWait); err != nil {
		t.Fatalf("markAndSweepS3FilesFileSystems() error = %v", err)
	}
	if len(client.deletedMountTarget) != 0 {
		t.Errorf("deleted mount targets %v, want none for an already deleted mount target", client.deletedMountTarget)
	}
	if diff := cmp.Diff([]string{"fs-old"}, deletedS3FilesIDs(client)); diff != "" {
		t.Errorf("deleted file systems mismatch (-want +got):\n%s", diff)
	}
}

func TestS3FilesFileSystemsDisabledByDefault(t *testing.T) {
	// Both calls would panic on the nil Options.Config if the type did not
	// return before building a client.
	if err := (S3FilesFileSystems{}).MarkAndSweep(Options{}, NewSet(0)); err != nil {
		t.Errorf("MarkAndSweep() error = %v, want nil", err)
	}
	set, err := (S3FilesFileSystems{}).ListAll(Options{})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if arns := set.GetARNs(); len(arns) != 0 {
		t.Errorf("ListAll() = %v, want an empty set while cleanup is disabled", arns)
	}
}

func TestMarkAndSweepS3FilesFileSystemsSelectsManagedResources(t *testing.T) {
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-time.Hour)
	ttlOverrideOld := now.Add(-2 * time.Hour)

	client := &mockS3FilesClient{
		pages: map[string]mockS3FilesPage{
			"": {
				fileSystems: []s3filestypes.ListFileSystemsDescription{
					testS3FilesFileSystem("fs-old", s3filestypes.LifeCycleStateAvailable, old),
					testS3FilesFileSystem("fs-recent", s3filestypes.LifeCycleStateAvailable, recent),
					testS3FilesFileSystem("fs-untagged", s3filestypes.LifeCycleStateAvailable, old),
					testS3FilesFileSystem("fs-excluded", s3filestypes.LifeCycleStateAvailable, old),
					testS3FilesFileSystem("fs-deleting", s3filestypes.LifeCycleStateDeleting, old),
					testS3FilesFileSystem("fs-ttl", s3filestypes.LifeCycleStateAvailable, ttlOverrideOld),
					testS3FilesFileSystem("fs-error", s3filestypes.LifeCycleStateError, old),
				},
			},
		},
		tags: map[string][]s3filestypes.Tag{
			"fs-old":      testS3FilesTags(map[string]string{"owned": "true"}),
			"fs-recent":   testS3FilesTags(map[string]string{"owned": "true"}),
			"fs-excluded": testS3FilesTags(map[string]string{"owned": "true", "preserve": "true"}),
			"fs-deleting": testS3FilesTags(map[string]string{"owned": "true"}),
			"fs-ttl":      testS3FilesTags(map[string]string{"owned": "true", "janitor-ttl": "1h"}),
			"fs-error":    testS3FilesTags(map[string]string{"owned": "true"}),
		},
	}

	opts := Options{
		Account:     "123456789012",
		Region:      "us-west-2",
		IncludeTags: mustTagMatcher(t, "owned=true"),
		ExcludeTags: mustTagMatcher(t, "preserve=true"),
		TTLTagKey:   "janitor-ttl",
	}
	if err := markAndSweepS3FilesFileSystems(client, opts, NewSet(24*time.Hour), noS3FilesWait); err != nil {
		t.Fatalf("markAndSweepS3FilesFileSystems() error = %v", err)
	}

	if diff := cmp.Diff([]string{"fs-old", "fs-ttl", "fs-error"}, deletedS3FilesIDs(client)); diff != "" {
		t.Errorf("deleted file systems mismatch (-want +got):\n%s", diff)
	}
	for _, input := range client.deletedFileSystems {
		if !aws.ToBool(input.ForceDelete) {
			t.Errorf("DeleteFileSystem(%q) did not set ForceDelete", aws.ToString(input.FileSystemId))
		}
	}
}

func TestMarkAndSweepS3FilesFileSystemsDryRun(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	client := &mockS3FilesClient{
		pages: map[string]mockS3FilesPage{
			"": {fileSystems: []s3filestypes.ListFileSystemsDescription{
				testS3FilesFileSystem("fs-old", s3filestypes.LifeCycleStateAvailable, old),
			}},
		},
		mountTargets: map[string][][]s3filestypes.ListMountTargetsDescription{
			"fs-old": {{testS3FilesMountTarget("fsmt-1", s3filestypes.LifeCycleStateAvailable)}},
		},
	}

	if err := markAndSweepS3FilesFileSystems(client, Options{DryRun: true}, NewSet(0), noS3FilesWait); err != nil {
		t.Fatalf("markAndSweepS3FilesFileSystems() error = %v", err)
	}
	if len(client.deletedFileSystems) != 0 || len(client.deletedMountTarget) != 0 {
		t.Errorf("dry run issued deletes: file systems %v, mount targets %v", deletedS3FilesIDs(client), client.deletedMountTarget)
	}
}

func TestMarkAndSweepS3FilesFileSystemsDeletesMountTargetsFirst(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	client := &mockS3FilesClient{
		pages: map[string]mockS3FilesPage{
			"": {fileSystems: []s3filestypes.ListFileSystemsDescription{
				testS3FilesFileSystem("fs-old", s3filestypes.LifeCycleStateAvailable, old),
			}},
		},
		mountTargets: map[string][][]s3filestypes.ListMountTargetsDescription{
			"fs-old": {
				{
					testS3FilesMountTarget("fsmt-1", s3filestypes.LifeCycleStateAvailable),
					testS3FilesMountTarget("fsmt-2", s3filestypes.LifeCycleStateDeleting),
				},
				{testS3FilesMountTarget("fsmt-2", s3filestypes.LifeCycleStateDeleting)},
				{},
			},
		},
	}

	if err := markAndSweepS3FilesFileSystems(client, Options{}, NewSet(0), noS3FilesWait); err != nil {
		t.Fatalf("markAndSweepS3FilesFileSystems() error = %v", err)
	}
	wantEvents := []string{"list:", "delete-mt:fsmt-1", "delete:fs-old"}
	if diff := cmp.Diff(wantEvents, client.events); diff != "" {
		t.Errorf("API call order mismatch (-want +got):\n%s", diff)
	}
}

func TestMarkAndSweepS3FilesFileSystemsLeavesFileSystemWhileMountTargetsRemain(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	client := &mockS3FilesClient{
		pages: map[string]mockS3FilesPage{
			"": {fileSystems: []s3filestypes.ListFileSystemsDescription{
				testS3FilesFileSystem("fs-old", s3filestypes.LifeCycleStateAvailable, old),
			}},
		},
		mountTargets: map[string][][]s3filestypes.ListMountTargetsDescription{
			"fs-old": {{testS3FilesMountTarget("fsmt-1", s3filestypes.LifeCycleStateDeleting)}},
		},
	}

	if err := markAndSweepS3FilesFileSystems(client, Options{}, NewSet(0), noS3FilesWait); err != nil {
		t.Fatalf("markAndSweepS3FilesFileSystems() error = %v", err)
	}
	if len(client.deletedFileSystems) != 0 {
		t.Errorf("deleted file systems %v while a mount target remained, want none", deletedS3FilesIDs(client))
	}
}

func TestMarkAndSweepS3FilesFileSystemsPaginatesBeforeDeleting(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	client := &mockS3FilesClient{
		pages: map[string]mockS3FilesPage{
			"": {
				fileSystems: []s3filestypes.ListFileSystemsDescription{
					testS3FilesFileSystem("fs-first", s3filestypes.LifeCycleStateAvailable, old),
				},
				nextToken: aws.String("second"),
			},
			"second": {fileSystems: []s3filestypes.ListFileSystemsDescription{
				testS3FilesFileSystem("fs-second", s3filestypes.LifeCycleStateAvailable, old),
			}},
		},
	}

	if err := markAndSweepS3FilesFileSystems(client, Options{}, NewSet(0), noS3FilesWait); err != nil {
		t.Fatalf("markAndSweepS3FilesFileSystems() error = %v", err)
	}
	wantEvents := []string{"list:", "list:second", "delete:fs-first", "delete:fs-second"}
	if diff := cmp.Diff(wantEvents, client.events); diff != "" {
		t.Errorf("API call order mismatch (-want +got):\n%s", diff)
	}
}

func TestMarkAndSweepS3FilesFileSystemsDoesNotDeleteAfterListFailure(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	listErr := errors.New("list failed")
	client := &mockS3FilesClient{
		pages: map[string]mockS3FilesPage{
			"": {
				fileSystems: []s3filestypes.ListFileSystemsDescription{
					testS3FilesFileSystem("fs-first", s3filestypes.LifeCycleStateAvailable, old),
				},
				nextToken: aws.String("second"),
			},
			"second": {err: listErr},
		},
	}

	err := markAndSweepS3FilesFileSystems(client, Options{}, NewSet(0), noS3FilesWait)
	if !errors.Is(err, listErr) {
		t.Fatalf("markAndSweepS3FilesFileSystems() error = %v, want wrapped list error", err)
	}
	if len(client.deletedFileSystems) != 0 {
		t.Errorf("issued %d DeleteFileSystem calls after pagination failed, want 0", len(client.deletedFileSystems))
	}
}

func TestMarkAndSweepS3FilesFileSystemsSkipsFileSystemWithUnreadableTags(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	client := &mockS3FilesClient{
		pages: map[string]mockS3FilesPage{
			"": {fileSystems: []s3filestypes.ListFileSystemsDescription{
				testS3FilesFileSystem("fs-no-tags", s3filestypes.LifeCycleStateAvailable, old),
				testS3FilesFileSystem("fs-ok", s3filestypes.LifeCycleStateAvailable, old),
			}},
		},
		tagErrors: map[string]error{"fs-no-tags": errors.New("throttled")},
	}

	if err := markAndSweepS3FilesFileSystems(client, Options{}, NewSet(0), noS3FilesWait); err != nil {
		t.Fatalf("markAndSweepS3FilesFileSystems() error = %v", err)
	}
	if diff := cmp.Diff([]string{"fs-ok"}, deletedS3FilesIDs(client)); diff != "" {
		t.Errorf("deleted file systems mismatch (-want +got):\n%s", diff)
	}
}

func TestMarkAndSweepS3FilesFileSystemsContinuesAfterDeleteFailure(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	client := &mockS3FilesClient{
		pages: map[string]mockS3FilesPage{
			"": {fileSystems: []s3filestypes.ListFileSystemsDescription{
				testS3FilesFileSystem("fs-first", s3filestypes.LifeCycleStateAvailable, old),
				testS3FilesFileSystem("fs-second", s3filestypes.LifeCycleStateAvailable, old),
			}},
		},
		deleteFSErrors: map[string]error{"fs-first": errors.New("delete failed")},
	}

	if err := markAndSweepS3FilesFileSystems(client, Options{}, NewSet(0), noS3FilesWait); err != nil {
		t.Fatalf("markAndSweepS3FilesFileSystems() error = %v", err)
	}
	if diff := cmp.Diff([]string{"fs-first", "fs-second"}, deletedS3FilesIDs(client)); diff != "" {
		t.Errorf("delete attempts mismatch (-want +got):\n%s", diff)
	}
}

func TestMarkAndSweepS3FilesFileSystemsSkipsUnavailableService(t *testing.T) {
	testCases := []struct {
		name string
		err  error
	}{
		{
			name: "endpoint not resolvable",
			err:  fmt.Errorf("operation error S3Files: ListFileSystems: %w", &net.DNSError{Err: "no such host", Name: "s3files.example.amazonaws.com", IsNotFound: true}),
		},
		{
			name: "access denied",
			err:  testS3FilesResponseError(http.StatusForbidden),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &mockS3FilesClient{pages: map[string]mockS3FilesPage{"": {err: testCase.err}}}

			if err := markAndSweepS3FilesFileSystems(client, Options{}, NewSet(0), noS3FilesWait); err != nil {
				t.Errorf("markAndSweepS3FilesFileSystems() error = %v, want nil", err)
			}
			if _, err := listAllS3FilesFileSystems(client, Options{}); err != nil {
				t.Errorf("listAllS3FilesFileSystems() error = %v, want nil", err)
			}
		})
	}
}

func TestMarkAndSweepS3FilesFileSystemsReturnsOtherListErrors(t *testing.T) {
	client := &mockS3FilesClient{pages: map[string]mockS3FilesPage{"": {err: testS3FilesResponseError(http.StatusInternalServerError)}}}

	if err := markAndSweepS3FilesFileSystems(client, Options{}, NewSet(0), noS3FilesWait); err == nil {
		t.Error("markAndSweepS3FilesFileSystems() error = nil, want the list error")
	}
}

func TestMarkAndSweepS3FilesFileSystemsRejectsIncompleteMetadata(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	testCases := []struct {
		name   string
		mutate func(*s3filestypes.ListFileSystemsDescription)
	}{
		{
			name: "missing ID",
			mutate: func(fileSystem *s3filestypes.ListFileSystemsDescription) {
				fileSystem.FileSystemId = nil
			},
		},
		{
			name: "missing ARN",
			mutate: func(fileSystem *s3filestypes.ListFileSystemsDescription) {
				fileSystem.FileSystemArn = nil
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			valid := testS3FilesFileSystem("fs-valid", s3filestypes.LifeCycleStateAvailable, old)
			invalid := testS3FilesFileSystem("fs-invalid", s3filestypes.LifeCycleStateAvailable, old)
			testCase.mutate(&invalid)
			client := &mockS3FilesClient{
				pages: map[string]mockS3FilesPage{
					"": {fileSystems: []s3filestypes.ListFileSystemsDescription{valid, invalid}},
				},
			}

			if err := markAndSweepS3FilesFileSystems(client, Options{}, NewSet(0), noS3FilesWait); err == nil {
				t.Fatal("markAndSweepS3FilesFileSystems() error = nil, want metadata validation error")
			}
			if len(client.deletedFileSystems) != 0 {
				t.Errorf("issued %d DeleteFileSystem calls with incomplete metadata, want 0", len(client.deletedFileSystems))
			}
		})
	}
}

func TestListAllS3FilesFileSystems(t *testing.T) {
	now := time.Now()
	client := &mockS3FilesClient{
		pages: map[string]mockS3FilesPage{
			"": {
				fileSystems: []s3filestypes.ListFileSystemsDescription{
					testS3FilesFileSystem("fs-a", s3filestypes.LifeCycleStateAvailable, now),
				},
				nextToken: aws.String("second"),
			},
			"second": {fileSystems: []s3filestypes.ListFileSystemsDescription{
				testS3FilesFileSystem("fs-b", s3filestypes.LifeCycleStateCreating, now),
			}},
		},
	}

	set, err := listAllS3FilesFileSystems(client, Options{Account: "123456789012", Region: "us-west-2"})
	if err != nil {
		t.Fatalf("listAllS3FilesFileSystems() error = %v", err)
	}
	if diff := cmp.Diff([]string{testS3FilesARN("fs-a"), testS3FilesARN("fs-b")}, set.GetARNs()); diff != "" {
		t.Errorf("listed file systems mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"", "second"}, client.listTokens); diff != "" {
		t.Errorf("pagination tokens mismatch (-want +got):\n%s", diff)
	}
}

func TestS3FilesFileSystemsRegisteredBeforeNetworkResources(t *testing.T) {
	s3FilesIndex := -1
	networkInterfacesIndex := -1
	for i, resourceType := range RegionalTypeList {
		switch resourceType.(type) {
		case S3FilesFileSystems:
			s3FilesIndex = i
		case NetworkInterfaces:
			networkInterfacesIndex = i
		}
	}

	if s3FilesIndex == -1 {
		t.Fatal("S3FilesFileSystems is not registered in RegionalTypeList")
	}
	if networkInterfacesIndex == -1 {
		t.Fatal("NetworkInterfaces is not registered in RegionalTypeList")
	}
	if s3FilesIndex >= networkInterfacesIndex {
		t.Errorf("S3FilesFileSystems index = %d, want before NetworkInterfaces index %d", s3FilesIndex, networkInterfacesIndex)
	}
}

func testS3FilesFileSystem(id string, status s3filestypes.LifeCycleState, creationTime time.Time) s3filestypes.ListFileSystemsDescription {
	return s3filestypes.ListFileSystemsDescription{
		CreationTime:  &creationTime,
		FileSystemArn: aws.String(testS3FilesARN(id)),
		FileSystemId:  aws.String(id),
		Status:        status,
	}
}

func testS3FilesMountTarget(id string, status s3filestypes.LifeCycleState) s3filestypes.ListMountTargetsDescription {
	return s3filestypes.ListMountTargetsDescription{
		MountTargetId: aws.String(id),
		Status:        status,
	}
}

func testS3FilesAccessPoint(id string, status s3filestypes.LifeCycleState) s3filestypes.ListAccessPointsDescription {
	return s3filestypes.ListAccessPointsDescription{
		AccessPointId: aws.String(id),
		Status:        status,
	}
}

func testS3FilesTags(values map[string]string) []s3filestypes.Tag {
	tags := make([]s3filestypes.Tag, 0, len(values))
	for key, value := range values {
		tags = append(tags, s3filestypes.Tag{Key: aws.String(key), Value: aws.String(value)})
	}
	return tags
}

func testS3FilesARN(id string) string {
	return "arn:aws:s3files:us-west-2:123456789012:file-system/" + id
}

func testS3FilesResponseError(statusCode int) error {
	return &awshttp.ResponseError{
		ResponseError: &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: statusCode}},
			Err:      errors.New("request failed"),
		},
	}
}

func deletedS3FilesIDs(client *mockS3FilesClient) []string {
	ids := make([]string, 0, len(client.deletedFileSystems))
	for _, input := range client.deletedFileSystems {
		ids = append(ids, aws.ToString(input.FileSystemId))
	}
	return ids
}
