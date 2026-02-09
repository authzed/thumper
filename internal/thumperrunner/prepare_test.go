package thumperrunner

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/authzed/internal/thumper/internal/config"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
)

// testClient implements the Client interface for testing.
type testClient struct {
	checkPermissionFn      func(ctx context.Context, in *v1.CheckPermissionRequest, opts ...grpc.CallOption) (*v1.CheckPermissionResponse, error)
	readRelationshipsFn    func(ctx context.Context, in *v1.ReadRelationshipsRequest, opts ...grpc.CallOption) (v1.PermissionsService_ReadRelationshipsClient, error)
	deleteRelationshipsFn  func(ctx context.Context, in *v1.DeleteRelationshipsRequest, opts ...grpc.CallOption) (*v1.DeleteRelationshipsResponse, error)
	expandPermissionTreeFn func(ctx context.Context, in *v1.ExpandPermissionTreeRequest, opts ...grpc.CallOption) (*v1.ExpandPermissionTreeResponse, error)
	lookupResourcesFn      func(ctx context.Context, in *v1.LookupResourcesRequest, opts ...grpc.CallOption) (v1.PermissionsService_LookupResourcesClient, error)
	lookupSubjectsFn       func(ctx context.Context, in *v1.LookupSubjectsRequest, opts ...grpc.CallOption) (v1.PermissionsService_LookupSubjectsClient, error)
	writeRelationshipsFn   func(ctx context.Context, in *v1.WriteRelationshipsRequest, opts ...grpc.CallOption) (*v1.WriteRelationshipsResponse, error)
	writeSchemaFn          func(ctx context.Context, in *v1.WriteSchemaRequest, opts ...grpc.CallOption) (*v1.WriteSchemaResponse, error)
	checkBulkPermissionsFn func(ctx context.Context, in *v1.CheckBulkPermissionsRequest, opts ...grpc.CallOption) (*v1.CheckBulkPermissionsResponse, error)
}

func (t *testClient) CheckPermission(ctx context.Context, in *v1.CheckPermissionRequest, opts ...grpc.CallOption) (*v1.CheckPermissionResponse, error) {
	return t.checkPermissionFn(ctx, in, opts...)
}

func (t *testClient) ReadRelationships(ctx context.Context, in *v1.ReadRelationshipsRequest, opts ...grpc.CallOption) (v1.PermissionsService_ReadRelationshipsClient, error) {
	return t.readRelationshipsFn(ctx, in, opts...)
}

func (t *testClient) DeleteRelationships(ctx context.Context, in *v1.DeleteRelationshipsRequest, opts ...grpc.CallOption) (*v1.DeleteRelationshipsResponse, error) {
	return t.deleteRelationshipsFn(ctx, in, opts...)
}

func (t *testClient) ExpandPermissionTree(ctx context.Context, in *v1.ExpandPermissionTreeRequest, opts ...grpc.CallOption) (*v1.ExpandPermissionTreeResponse, error) {
	return t.expandPermissionTreeFn(ctx, in, opts...)
}

func (t *testClient) LookupResources(ctx context.Context, in *v1.LookupResourcesRequest, opts ...grpc.CallOption) (v1.PermissionsService_LookupResourcesClient, error) {
	return t.lookupResourcesFn(ctx, in, opts...)
}

func (t *testClient) LookupSubjects(ctx context.Context, in *v1.LookupSubjectsRequest, opts ...grpc.CallOption) (v1.PermissionsService_LookupSubjectsClient, error) {
	return t.lookupSubjectsFn(ctx, in, opts...)
}

func (t *testClient) WriteRelationships(ctx context.Context, in *v1.WriteRelationshipsRequest, opts ...grpc.CallOption) (*v1.WriteRelationshipsResponse, error) {
	return t.writeRelationshipsFn(ctx, in, opts...)
}

func (t *testClient) WriteSchema(ctx context.Context, in *v1.WriteSchemaRequest, opts ...grpc.CallOption) (*v1.WriteSchemaResponse, error) {
	return t.writeSchemaFn(ctx, in, opts...)
}

func (t *testClient) CheckBulkPermissions(ctx context.Context, in *v1.CheckBulkPermissionsRequest, opts ...grpc.CallOption) (*v1.CheckBulkPermissionsResponse, error) {
	return t.checkBulkPermissionsFn(ctx, in, opts...)
}

// fakeStream implements grpc.ClientStream for testing streaming RPCs.
type fakeStream struct {
	grpc.ClientStream
	msgs []interface{}
	idx  int
}

func (f *fakeStream) RecvMsg(_ interface{}) error {
	if f.idx >= len(f.msgs) {
		return io.EOF
	}
	f.idx++
	return nil
}

func (f *fakeStream) Header() (metadata.MD, error) { return nil, nil }
func (f *fakeStream) Trailer() metadata.MD         { return nil }
func (f *fakeStream) CloseSend() error             { return nil }
func (f *fakeStream) Context() context.Context     { return context.Background() }
func (f *fakeStream) SendMsg(interface{}) error    { return nil }

// fakeReadRelStream wraps fakeStream to satisfy the ReadRelationshipsClient interface.
type fakeReadRelStream struct {
	*fakeStream
}

func (f *fakeReadRelStream) Recv() (*v1.ReadRelationshipsResponse, error) {
	return nil, io.EOF
}

// fakeLookupResourcesStream wraps fakeStream to satisfy the LookupResourcesClient interface.
type fakeLookupResourcesStream struct {
	*fakeStream
}

func (f *fakeLookupResourcesStream) Recv() (*v1.LookupResourcesResponse, error) {
	return nil, io.EOF
}

// fakeLookupSubjectsStream wraps fakeStream to satisfy the LookupSubjectsClient interface.
type fakeLookupSubjectsStream struct {
	*fakeStream
}

func (f *fakeLookupSubjectsStream) Recv() (*v1.LookupSubjectsResponse, error) {
	return nil, io.EOF
}

func TestParseComponents(t *testing.T) {
	tests := []struct {
		input                           string
		expectType, expectID, expectRel string
	}{
		{"document:doc1", "document", "doc1", ""},
		{"document:doc1#viewer", "document", "doc1", "viewer"},
		{"user:user1", "user", "user1", ""},
		{"document", "document", "", ""},
		{"group:eng#member", "group", "eng", "member"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			objType, objID, rel := parseComponents(tt.input)
			require.Equal(t, tt.expectType, objType)
			require.Equal(t, tt.expectID, objID)
			require.Equal(t, tt.expectRel, rel)
		})
	}
}

func TestParseObject(t *testing.T) {
	t.Run("valid object", func(t *testing.T) {
		obj, err := parseObject("document:doc1")
		require.NoError(t, err)
		require.Equal(t, "document", obj.ObjectType)
		require.Equal(t, "doc1", obj.ObjectId)
	})

	// Documents current panic behavior on invalid input
	t.Run("panics on object with relation", func(t *testing.T) {
		require.Panics(t, func() {
			_, _ = parseObject("document:doc1#viewer")
		})
	})
}

func TestParseSubject(t *testing.T) {
	t.Run("valid subject without relation", func(t *testing.T) {
		sub, err := parseSubject("user:user1")
		require.NoError(t, err)
		require.Equal(t, "user", sub.Object.ObjectType)
		require.Equal(t, "user1", sub.Object.ObjectId)
		require.Empty(t, sub.OptionalRelation)
	})

	t.Run("valid subject with relation", func(t *testing.T) {
		sub, err := parseSubject("group:eng#member")
		require.NoError(t, err)
		require.Equal(t, "group", sub.Object.ObjectType)
		require.Equal(t, "eng", sub.Object.ObjectId)
		require.Equal(t, "member", sub.OptionalRelation)
	})

	// Documents current panic behavior on invalid input
	t.Run("panics on missing type", func(t *testing.T) {
		require.Panics(t, func() {
			_, _ = parseSubject("")
		})
	})

	// Documents current panic behavior on invalid input
	t.Run("panics on missing object ID", func(t *testing.T) {
		require.Panics(t, func() {
			_, _ = parseSubject("user")
		})
	})
}

func TestParseRelationshipFilter(t *testing.T) {
	t.Run("resource only", func(t *testing.T) {
		filter, err := parseRelationshipFilter("document", "", "")
		require.NoError(t, err)
		require.Equal(t, "document", filter.ResourceType)
		require.Empty(t, filter.OptionalResourceId)
		require.Empty(t, filter.OptionalRelation)
		require.Nil(t, filter.OptionalSubjectFilter)
	})

	t.Run("resource with ID and relation and subject", func(t *testing.T) {
		filter, err := parseRelationshipFilter("document:doc1", "viewer", "user:user1")
		require.NoError(t, err)
		require.Equal(t, "document", filter.ResourceType)
		require.Equal(t, "doc1", filter.OptionalResourceId)
		require.Equal(t, "viewer", filter.OptionalRelation)
		require.NotNil(t, filter.OptionalSubjectFilter)
		require.Equal(t, "user", filter.OptionalSubjectFilter.SubjectType)
		require.Equal(t, "user1", filter.OptionalSubjectFilter.OptionalSubjectId)
	})

	t.Run("subject with relation", func(t *testing.T) {
		filter, err := parseRelationshipFilter("document", "", "group:eng#member")
		require.NoError(t, err)
		require.NotNil(t, filter.OptionalSubjectFilter)
		require.Equal(t, "group", filter.OptionalSubjectFilter.SubjectType)
		require.Equal(t, "eng", filter.OptionalSubjectFilter.OptionalSubjectId)
		require.Equal(t, "member", filter.OptionalSubjectFilter.OptionalRelation.Relation)
	})
}

func TestParseUpdates(t *testing.T) {
	t.Run("touch operation", func(t *testing.T) {
		updates, err := parseUpdates([]config.Update{
			{Op: "TOUCH", Resource: "document:doc1", Subject: "user:user1", Relation: "viewer"},
		})
		require.NoError(t, err)
		require.Len(t, updates, 1)
		require.Equal(t, v1.RelationshipUpdate_OPERATION_TOUCH, updates[0].Operation)
		require.Equal(t, "document", updates[0].Relationship.Resource.ObjectType)
		require.Equal(t, "viewer", updates[0].Relationship.Relation)
	})

	t.Run("create operation", func(t *testing.T) {
		updates, err := parseUpdates([]config.Update{
			{Op: "CREATE", Resource: "document:doc1", Subject: "user:user1", Relation: "viewer"},
		})
		require.NoError(t, err)
		require.Equal(t, v1.RelationshipUpdate_OPERATION_CREATE, updates[0].Operation)
	})

	t.Run("delete operation", func(t *testing.T) {
		updates, err := parseUpdates([]config.Update{
			{Op: "DELETE", Resource: "document:doc1", Subject: "user:user1", Relation: "viewer"},
		})
		require.NoError(t, err)
		require.Equal(t, v1.RelationshipUpdate_OPERATION_DELETE, updates[0].Operation)
	})

	// Documents current panic behavior on invalid input
	t.Run("panics on unknown operation", func(t *testing.T) {
		require.Panics(t, func() {
			_, _ = parseUpdates([]config.Update{
				{Op: "INVALID", Resource: "document:doc1", Subject: "user:user1", Relation: "viewer"},
			})
		})
	})

	t.Run("with caveat", func(t *testing.T) {
		updates, err := parseUpdates([]config.Update{
			{
				Op:       "TOUCH",
				Resource: "document:doc1",
				Subject:  "user:user1",
				Relation: "viewer",
				Caveat: &config.CaveatContext{
					Name: "test_caveat",
				},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, updates[0].Relationship.OptionalCaveat)
		require.Equal(t, "test_caveat", updates[0].Relationship.OptionalCaveat.CaveatName)
	})

	t.Run("empty updates", func(t *testing.T) {
		updates, err := parseUpdates(nil)
		require.NoError(t, err)
		require.Empty(t, updates)
	})
}

func TestPrepareConsistency(t *testing.T) {
	zt := &v1.ZedToken{Token: "test-token"}

	t.Run("default is MinimizeLatency", func(t *testing.T) {
		fn, desc, err := prepareConsistency(config.ScriptStep{})
		require.NoError(t, err)
		require.Equal(t, "MinimizeLatency", desc)
		c := fn(zt)
		require.True(t, c.GetMinimizeLatency())
	})

	t.Run("explicit MinimizeLatency", func(t *testing.T) {
		fn, desc, err := prepareConsistency(config.ScriptStep{Consistency: "MinimizeLatency"})
		require.NoError(t, err)
		require.Equal(t, "MinimizeLatency", desc)
		c := fn(nil)
		require.True(t, c.GetMinimizeLatency())
	})

	t.Run("AtLeastAsFresh with token", func(t *testing.T) {
		fn, desc, err := prepareConsistency(config.ScriptStep{Consistency: "AtLeastAsFresh"})
		require.NoError(t, err)
		require.Equal(t, "AtLeastAsFresh", desc)
		c := fn(zt)
		require.Equal(t, zt, c.GetAtLeastAsFresh())
	})

	t.Run("AtLeastAsFresh without token falls back to full", func(t *testing.T) {
		fn, _, err := prepareConsistency(config.ScriptStep{Consistency: "AtLeastAsFresh"})
		require.NoError(t, err)
		c := fn(nil)
		require.True(t, c.GetFullyConsistent())
	})

	t.Run("AtExactSnapshot with token", func(t *testing.T) {
		fn, desc, err := prepareConsistency(config.ScriptStep{Consistency: "AtExactSnapshot"})
		require.NoError(t, err)
		require.Equal(t, "AtExactSnapshot", desc)
		c := fn(zt)
		require.Equal(t, zt, c.GetAtExactSnapshot())
	})

	t.Run("AtExactSnapshot without token falls back to full", func(t *testing.T) {
		fn, _, err := prepareConsistency(config.ScriptStep{Consistency: "AtExactSnapshot"})
		require.NoError(t, err)
		c := fn(nil)
		require.True(t, c.GetFullyConsistent())
	})

	t.Run("FullyConsistent", func(t *testing.T) {
		fn, desc, err := prepareConsistency(config.ScriptStep{Consistency: "FullyConsistent"})
		require.NoError(t, err)
		require.Equal(t, "FullyConsistent", desc)
		c := fn(zt)
		require.True(t, c.GetFullyConsistent())
	})

	t.Run("unknown consistency errors", func(t *testing.T) {
		_, _, err := prepareConsistency(config.ScriptStep{Consistency: "InvalidConsistency"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown consistency type")
	})
}

func TestPrepare(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		result, err := Prepare(nil)
		require.NoError(t, err)
		require.Nil(t, result)
	})

	t.Run("unknown operation", func(t *testing.T) {
		_, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "UnknownOp"},
				},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown script step operation")
	})

	t.Run("invalid consistency", func(t *testing.T) {
		_, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{
						Op:          "CheckPermission",
						Resource:    "document:doc1",
						Subject:     "user:user1",
						Permission:  "view",
						Consistency: "BadConsistency",
					},
				},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "error preparing consistency")
	})

	t.Run("single CheckPermission step", func(t *testing.T) {
		result, err := Prepare([]*config.Script{
			{
				Name:   "check-test",
				Weight: 5,
				Steps: []config.ScriptStep{
					{
						Op:         "CheckPermission",
						Resource:   "document:doc1",
						Subject:    "user:user1",
						Permission: "view",
					},
				},
			},
		})
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Equal(t, "check-test", result[0].name)
		require.Equal(t, uint(5), result[0].weight)
		require.Len(t, result[0].steps, 1)
		require.Equal(t, "CheckPermission", result[0].steps[0].op)
		require.Equal(t, "MinimizeLatency", result[0].steps[0].consistency)
		require.NotNil(t, result[0].steps[0].body)
	})

	t.Run("multiple scripts with multiple steps", func(t *testing.T) {
		result, err := Prepare([]*config.Script{
			{
				Name:   "script-1",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "CheckPermission", Resource: "document:doc1", Subject: "user:user1", Permission: "view"},
					{Op: "WriteSchema", Schema: "definition doc {}"},
				},
			},
			{
				Name:   "script-2",
				Weight: 2,
				Steps: []config.ScriptStep{
					{Op: "LookupResources", Resource: "document", Subject: "user:user1", Permission: "view"},
				},
			},
		})
		require.NoError(t, err)
		require.Len(t, result, 2)
		require.Len(t, result[0].steps, 2)
		require.Len(t, result[1].steps, 1)
	})

	t.Run("all supported operations prepare without error", func(t *testing.T) {
		_, err := Prepare([]*config.Script{
			{
				Name:   "all-ops",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "CheckPermission", Resource: "document:doc1", Subject: "user:user1", Permission: "view"},
					{Op: "ReadRelationships", Resource: "document:doc1"},
					{Op: "DeleteRelationships", Resource: "document:doc1"},
					{Op: "ExpandPermissionTree", Resource: "document:doc1", Permission: "view"},
					{Op: "LookupResources", Resource: "document", Subject: "user:user1", Permission: "view"},
					{Op: "LookupSubjects", Resource: "document:doc1", Subject: "user", Permission: "view"},
					{
						Op: "WriteRelationships",
						Updates: []config.Update{
							{Op: "TOUCH", Resource: "document:doc1", Subject: "user:user1", Relation: "viewer"},
						},
					},
					{Op: "WriteSchema", Schema: "definition doc {}"},
					{
						Op: "CheckBulkPermissions",
						Checks: []config.Check{
							{Resource: "document:doc1", Subject: "user:user1", Permission: "view"},
						},
					},
				},
			},
		})
		require.NoError(t, err)
	})

	t.Run("consistency is preserved on steps", func(t *testing.T) {
		result, err := Prepare([]*config.Script{
			{
				Name:   "consistency-test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "CheckPermission", Resource: "document:doc1", Subject: "user:user1", Permission: "view", Consistency: "FullyConsistent"},
					{Op: "CheckPermission", Resource: "document:doc1", Subject: "user:user1", Permission: "view", Consistency: "AtLeastAsFresh"},
				},
			},
		})
		require.NoError(t, err)
		require.Equal(t, "FullyConsistent", result[0].steps[0].consistency)
		require.Equal(t, "AtLeastAsFresh", result[0].steps[1].consistency)
	})
}

func TestPrepareStepBodies(t *testing.T) {
	checkedAt := &v1.ZedToken{Token: "checked-at"}

	t.Run("CheckPermission body calls client and validates permissionship", func(t *testing.T) {
		tc := &testClient{
			checkPermissionFn: func(_ context.Context, in *v1.CheckPermissionRequest, _ ...grpc.CallOption) (*v1.CheckPermissionResponse, error) {
				require.Equal(t, "document", in.Resource.ObjectType)
				require.Equal(t, "doc1", in.Resource.ObjectId)
				require.Equal(t, "user", in.Subject.Object.ObjectType)
				require.Equal(t, "user1", in.Subject.Object.ObjectId)
				require.Equal(t, "view", in.Permission)
				return &v1.CheckPermissionResponse{
					Permissionship: v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION,
					CheckedAt:      checkedAt,
				}, nil
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "CheckPermission", Resource: "document:doc1", Subject: "user:user1", Permission: "view"},
				},
			},
		})
		require.NoError(t, err)

		zt, err := result[0].steps[0].body(context.Background(), tc, nil)
		require.NoError(t, err)
		require.Equal(t, checkedAt, zt)
	})

	t.Run("CheckPermission body returns error on wrong permissionship", func(t *testing.T) {
		tc := &testClient{
			checkPermissionFn: func(_ context.Context, _ *v1.CheckPermissionRequest, _ ...grpc.CallOption) (*v1.CheckPermissionResponse, error) {
				return &v1.CheckPermissionResponse{
					Permissionship: v1.CheckPermissionResponse_PERMISSIONSHIP_NO_PERMISSION,
					CheckedAt:      checkedAt,
				}, nil
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "CheckPermission", Resource: "document:doc1", Subject: "user:user1", Permission: "view"},
				},
			},
		})
		require.NoError(t, err)

		_, err = result[0].steps[0].body(context.Background(), tc, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "wrong permissionship")
	})

	t.Run("CheckPermission body with expectNoPermission", func(t *testing.T) {
		tc := &testClient{
			checkPermissionFn: func(_ context.Context, _ *v1.CheckPermissionRequest, _ ...grpc.CallOption) (*v1.CheckPermissionResponse, error) {
				return &v1.CheckPermissionResponse{
					Permissionship: v1.CheckPermissionResponse_PERMISSIONSHIP_NO_PERMISSION,
					CheckedAt:      checkedAt,
				}, nil
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "CheckPermission", Resource: "document:doc1", Subject: "user:user1", Permission: "view", ExpectNoPermission: true},
				},
			},
		})
		require.NoError(t, err)

		zt, err := result[0].steps[0].body(context.Background(), tc, nil)
		require.NoError(t, err)
		require.Equal(t, checkedAt, zt)
	})

	t.Run("CheckPermission body with expectPermissionship override", func(t *testing.T) {
		tc := &testClient{
			checkPermissionFn: func(_ context.Context, _ *v1.CheckPermissionRequest, _ ...grpc.CallOption) (*v1.CheckPermissionResponse, error) {
				return &v1.CheckPermissionResponse{
					Permissionship: v1.CheckPermissionResponse_PERMISSIONSHIP_CONDITIONAL_PERMISSION,
					CheckedAt:      checkedAt,
				}, nil
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "CheckPermission", Resource: "document:doc1", Subject: "user:user1", Permission: "view", ExpectPermissionship: "CONDITIONAL_PERMISSION"},
				},
			},
		})
		require.NoError(t, err)

		zt, err := result[0].steps[0].body(context.Background(), tc, nil)
		require.NoError(t, err)
		require.Equal(t, checkedAt, zt)
	})

	t.Run("CheckPermission body with context", func(t *testing.T) {
		tc := &testClient{
			checkPermissionFn: func(_ context.Context, in *v1.CheckPermissionRequest, _ ...grpc.CallOption) (*v1.CheckPermissionResponse, error) {
				require.NotNil(t, in.Context)
				require.Equal(t, "bar", in.Context.Fields["foo"].GetStringValue())
				return &v1.CheckPermissionResponse{
					Permissionship: v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION,
					CheckedAt:      checkedAt,
				}, nil
			},
		}

		caveatCtx := &config.ProtoStruct{
			Fields: map[string]*structpb.Value{
				"foo": {Kind: &structpb.Value_StringValue{StringValue: "bar"}},
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "CheckPermission", Resource: "document:doc1", Subject: "user:user1", Permission: "view", Context: caveatCtx},
				},
			},
		})
		require.NoError(t, err)

		_, err = result[0].steps[0].body(context.Background(), tc, nil)
		require.NoError(t, err)
	})

	t.Run("CheckPermission body propagates client error", func(t *testing.T) {
		tc := &testClient{
			checkPermissionFn: func(_ context.Context, _ *v1.CheckPermissionRequest, _ ...grpc.CallOption) (*v1.CheckPermissionResponse, error) {
				return nil, fmt.Errorf("connection refused")
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "CheckPermission", Resource: "document:doc1", Subject: "user:user1", Permission: "view"},
				},
			},
		})
		require.NoError(t, err)

		_, err = result[0].steps[0].body(context.Background(), tc, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "connection refused")
	})

	t.Run("WriteRelationships body calls client and returns token", func(t *testing.T) {
		writtenAt := &v1.ZedToken{Token: "written-at"}
		tc := &testClient{
			writeRelationshipsFn: func(_ context.Context, in *v1.WriteRelationshipsRequest, _ ...grpc.CallOption) (*v1.WriteRelationshipsResponse, error) {
				require.Len(t, in.Updates, 1)
				require.Equal(t, v1.RelationshipUpdate_OPERATION_TOUCH, in.Updates[0].Operation)
				return &v1.WriteRelationshipsResponse{WrittenAt: writtenAt}, nil
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{
						Op: "WriteRelationships",
						Updates: []config.Update{
							{Op: "TOUCH", Resource: "document:doc1", Subject: "user:user1", Relation: "viewer"},
						},
					},
				},
			},
		})
		require.NoError(t, err)

		zt, err := result[0].steps[0].body(context.Background(), tc, nil)
		require.NoError(t, err)
		require.Equal(t, writtenAt, zt)
	})

	t.Run("WriteSchema body calls client", func(t *testing.T) {
		tc := &testClient{
			writeSchemaFn: func(_ context.Context, in *v1.WriteSchemaRequest, _ ...grpc.CallOption) (*v1.WriteSchemaResponse, error) {
				require.Equal(t, "definition doc {}", in.Schema)
				return &v1.WriteSchemaResponse{}, nil
			},
		}

		inputZt := &v1.ZedToken{Token: "input-token"}
		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "WriteSchema", Schema: "definition doc {}"},
				},
			},
		})
		require.NoError(t, err)

		zt, err := result[0].steps[0].body(context.Background(), tc, inputZt)
		require.NoError(t, err)
		require.Equal(t, inputZt, zt)
	})

	t.Run("DeleteRelationships body calls client and returns token", func(t *testing.T) {
		deletedAt := &v1.ZedToken{Token: "deleted-at"}
		tc := &testClient{
			deleteRelationshipsFn: func(_ context.Context, in *v1.DeleteRelationshipsRequest, _ ...grpc.CallOption) (*v1.DeleteRelationshipsResponse, error) {
				require.Equal(t, "document", in.RelationshipFilter.ResourceType)
				return &v1.DeleteRelationshipsResponse{DeletedAt: deletedAt}, nil
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "DeleteRelationships", Resource: "document:doc1"},
				},
			},
		})
		require.NoError(t, err)

		zt, err := result[0].steps[0].body(context.Background(), tc, nil)
		require.NoError(t, err)
		require.Equal(t, deletedAt, zt)
	})

	t.Run("ExpandPermissionTree body calls client and returns token", func(t *testing.T) {
		expandedAt := &v1.ZedToken{Token: "expanded-at"}
		tc := &testClient{
			expandPermissionTreeFn: func(_ context.Context, in *v1.ExpandPermissionTreeRequest, _ ...grpc.CallOption) (*v1.ExpandPermissionTreeResponse, error) {
				require.Equal(t, "document", in.Resource.ObjectType)
				require.Equal(t, "view", in.Permission)
				return &v1.ExpandPermissionTreeResponse{ExpandedAt: expandedAt}, nil
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "ExpandPermissionTree", Resource: "document:doc1", Permission: "view"},
				},
			},
		})
		require.NoError(t, err)

		zt, err := result[0].steps[0].body(context.Background(), tc, nil)
		require.NoError(t, err)
		require.Equal(t, expandedAt, zt)
	})

	t.Run("ReadRelationships body calls client", func(t *testing.T) {
		inputZt := &v1.ZedToken{Token: "input-token"}
		tc := &testClient{
			readRelationshipsFn: func(_ context.Context, in *v1.ReadRelationshipsRequest, _ ...grpc.CallOption) (v1.PermissionsService_ReadRelationshipsClient, error) {
				require.Equal(t, "document", in.RelationshipFilter.ResourceType)
				return &fakeReadRelStream{&fakeStream{}}, nil
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "ReadRelationships", Resource: "document:doc1"},
				},
			},
		})
		require.NoError(t, err)

		zt, err := result[0].steps[0].body(context.Background(), tc, inputZt)
		require.NoError(t, err)
		require.Equal(t, inputZt, zt)
	})

	t.Run("LookupResources body calls client", func(t *testing.T) {
		inputZt := &v1.ZedToken{Token: "input-token"}
		tc := &testClient{
			lookupResourcesFn: func(_ context.Context, in *v1.LookupResourcesRequest, _ ...grpc.CallOption) (v1.PermissionsService_LookupResourcesClient, error) {
				require.Equal(t, "document", in.ResourceObjectType)
				require.Equal(t, "view", in.Permission)
				return &fakeLookupResourcesStream{&fakeStream{}}, nil
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "LookupResources", Resource: "document", Subject: "user:user1", Permission: "view"},
				},
			},
		})
		require.NoError(t, err)

		zt, err := result[0].steps[0].body(context.Background(), tc, inputZt)
		require.NoError(t, err)
		require.Equal(t, inputZt, zt)
	})

	t.Run("LookupSubjects body calls client", func(t *testing.T) {
		inputZt := &v1.ZedToken{Token: "input-token"}
		tc := &testClient{
			lookupSubjectsFn: func(_ context.Context, in *v1.LookupSubjectsRequest, _ ...grpc.CallOption) (v1.PermissionsService_LookupSubjectsClient, error) {
				require.Equal(t, "document", in.Resource.ObjectType)
				require.Equal(t, "user", in.SubjectObjectType)
				return &fakeLookupSubjectsStream{&fakeStream{}}, nil
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{Op: "LookupSubjects", Resource: "document:doc1", Subject: "user", Permission: "view"},
				},
			},
		})
		require.NoError(t, err)

		zt, err := result[0].steps[0].body(context.Background(), tc, inputZt)
		require.NoError(t, err)
		require.Equal(t, inputZt, zt)
	})

	t.Run("CheckBulkPermissions body calls client and validates", func(t *testing.T) {
		bulkCheckedAt := &v1.ZedToken{Token: "bulk-checked-at"}
		tc := &testClient{
			checkBulkPermissionsFn: func(_ context.Context, in *v1.CheckBulkPermissionsRequest, _ ...grpc.CallOption) (*v1.CheckBulkPermissionsResponse, error) {
				require.Len(t, in.Items, 2)
				return &v1.CheckBulkPermissionsResponse{
					CheckedAt: bulkCheckedAt,
					Pairs: []*v1.CheckBulkPermissionsPair{
						{Response: &v1.CheckBulkPermissionsPair_Item{Item: &v1.CheckBulkPermissionsResponseItem{
							Permissionship: v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION,
						}}},
						{Response: &v1.CheckBulkPermissionsPair_Item{Item: &v1.CheckBulkPermissionsResponseItem{
							Permissionship: v1.CheckPermissionResponse_PERMISSIONSHIP_NO_PERMISSION,
						}}},
					},
				}, nil
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{
						Op: "CheckBulkPermissions",
						Checks: []config.Check{
							{Resource: "document:doc1", Subject: "user:user1", Permission: "view"},
							{Resource: "document:doc2", Subject: "user:user2", Permission: "view", ExpectNoPermission: true},
						},
					},
				},
			},
		})
		require.NoError(t, err)

		zt, err := result[0].steps[0].body(context.Background(), tc, nil)
		require.NoError(t, err)
		require.Equal(t, bulkCheckedAt, zt)
	})

	t.Run("CheckBulkPermissions body returns error on wrong permissionship", func(t *testing.T) {
		tc := &testClient{
			checkBulkPermissionsFn: func(_ context.Context, _ *v1.CheckBulkPermissionsRequest, _ ...grpc.CallOption) (*v1.CheckBulkPermissionsResponse, error) {
				return &v1.CheckBulkPermissionsResponse{
					CheckedAt: checkedAt,
					Pairs: []*v1.CheckBulkPermissionsPair{
						{Response: &v1.CheckBulkPermissionsPair_Item{Item: &v1.CheckBulkPermissionsResponseItem{
							Permissionship: v1.CheckPermissionResponse_PERMISSIONSHIP_NO_PERMISSION,
						}}},
					},
				}, nil
			},
		}

		result, err := Prepare([]*config.Script{
			{
				Name:   "test",
				Weight: 1,
				Steps: []config.ScriptStep{
					{
						Op: "CheckBulkPermissions",
						Checks: []config.Check{
							{Resource: "document:doc1", Subject: "user:user1", Permission: "view"},
						},
					},
				},
			},
		})
		require.NoError(t, err)

		_, err = result[0].steps[0].body(context.Background(), tc, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "wrong permissionship")
	})
}
