package thumperrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/authzed/internal/thumper/internal/config"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/authzed-go/v1"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// Prepare transforms a loaded yaml script into one that can be efficiently executed.
func Prepare(inputs []*config.Script) (prepared []*ExecutableScript, err error) {
	for _, input := range inputs {
		steps := make([]executableStep, 0, len(input.Steps))

		for _, rawStep := range input.Steps {
			var step executableStep
			step, err = prepareStep(rawStep)
			if err != nil {
				return prepared, err
			}

			steps = append(steps, step)
		}

		prepared = append(prepared, &ExecutableScript{
			name:   input.Name,
			weight: input.Weight,
			steps:  steps,
		})
	}

	return prepared, err
}

func prepareStep(step config.ScriptStep) (executableStep, error) {
	consistencyForZedToken, consistencyDesc, err := prepareConsistency(step)
	if err != nil {
		return executableStep{}, fmt.Errorf("error preparing consistency: %w", err)
	}

	execStep := executableStep{
		op:          step.Op,
		consistency: consistencyDesc,
	}

	switch step.Op {
	case "CheckPermission":
		res, err := parseObject(step.Resource)
		if err != nil {
			return executableStep{}, fmt.Errorf("error parsing CheckPermission resource: %w", err)
		}

		sub, err := parseSubject(step.Subject)
		if err != nil {
			return executableStep{}, fmt.Errorf("error parsing CheckPermission subject: %w", err)
		}

		req := &v1.CheckPermissionRequest{
			Resource:   res,
			Subject:    sub,
			Permission: step.Permission,
			Context:    (*structpb.Struct)(step.Context),
		}
		expected := v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION
		if step.ExpectNoPermission {
			expected = v1.CheckPermissionResponse_PERMISSIONSHIP_NO_PERMISSION
		}

		switch step.ExpectPermissionship {
		case "HAS_PERMISSION":
			expected = v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION
		case "NO_PERMISSION":
			expected = v1.CheckPermissionResponse_PERMISSIONSHIP_NO_PERMISSION
		case "CONDITIONAL_PERMISSION":
			expected = v1.CheckPermissionResponse_PERMISSIONSHIP_CONDITIONAL_PERMISSION
		}

		execStep.body = func(ctx context.Context, client *authzed.Client, zt *v1.ZedToken) (*v1.ZedToken, error) {
			req.Consistency = consistencyForZedToken(zt)

			resp, err := client.CheckPermission(ctx, req)
			if err != nil {
				return nil, err
			}

			if resp.Permissionship != expected {
				return nil, fmt.Errorf(
					"CheckPermission returned wrong permissionship: %s#%s@%s => %s",
					step.Resource,
					step.Permission,
					step.Subject,
					resp.Permissionship,
				)
			}

			return resp.CheckedAt, nil
		}
	case "ReadRelationships":
		filter, err := parseRelationshipFilter(step.Resource, step.Permission, step.Subject)
		if err != nil {
			return executableStep{}, fmt.Errorf("error parsing ReadRealtionships filter: %w", err)
		}

		req := &v1.ReadRelationshipsRequest{
			RelationshipFilter: filter,
		}

		execStep.body = func(ctx context.Context, client *authzed.Client, zt *v1.ZedToken) (*v1.ZedToken, error) {
			req.Consistency = consistencyForZedToken(zt)
			resp, err := client.ReadRelationships(ctx, req)
			if err != nil {
				return nil, err
			}

			return zt, verifyExpectedStreamCount(resp, &v1.ReadRelationshipsResponse{}, step.NumExpected, "ReadRelationships error: %w")
		}
	case "DeleteRelationships":
		filter, err := parseRelationshipFilter(step.Resource, step.Permission, step.Subject)
		if err != nil {
			return executableStep{}, fmt.Errorf("error parsing DeleteRelationships filter: %w", err)
		}

		req := &v1.DeleteRelationshipsRequest{
			RelationshipFilter: filter,
		}

		execStep.body = func(ctx context.Context, client *authzed.Client, _ *v1.ZedToken) (*v1.ZedToken, error) {
			resp, err := client.DeleteRelationships(ctx, req)
			if err != nil {
				return nil, err
			}

			return resp.DeletedAt, nil
		}
	case "ExpandPermissionTree":
		res, err := parseObject(step.Resource)
		if err != nil {
			return executableStep{}, fmt.Errorf("error parsing ExpandPermissionTree resource: %w", err)
		}
		req := &v1.ExpandPermissionTreeRequest{
			Resource:   res,
			Permission: step.Permission,
		}

		execStep.body = func(ctx context.Context, client *authzed.Client, zt *v1.ZedToken) (*v1.ZedToken, error) {
			req.Consistency = consistencyForZedToken(zt)
			resp, err := client.ExpandPermissionTree(ctx, req)
			if err != nil {
				return nil, err
			}

			return resp.ExpandedAt, nil
		}
	case "LookupResources":
		sub, err := parseSubject(step.Subject)
		if err != nil {
			return executableStep{}, fmt.Errorf("error parsing LookupResources subject: %w", err)
		}

		req := &v1.LookupResourcesRequest{
			ResourceObjectType: step.Resource,
			Permission:         step.Permission,
			Subject:            sub,
			Context:            (*structpb.Struct)(step.Context),
		}

		execStep.body = func(ctx context.Context, client *authzed.Client, zt *v1.ZedToken) (*v1.ZedToken, error) {
			req.Consistency = consistencyForZedToken(zt)
			resp, err := client.LookupResources(ctx, req)
			if err != nil {
				return nil, err
			}

			return zt, verifyExpectedStreamCount(resp, &v1.LookupResourcesResponse{}, step.NumExpected, "LookupResources error: %w")
		}
	case "LookupSubjects":
		res, err := parseObject(step.Resource)
		if err != nil {
			return executableStep{}, fmt.Errorf("error parsing CheckPermission resource: %w", err)
		}

		req := &v1.LookupSubjectsRequest{
			SubjectObjectType: step.Subject,
			Resource:          res,
			Permission:        step.Permission,
			Context:           (*structpb.Struct)(step.Context),
		}

		execStep.body = func(ctx context.Context, client *authzed.Client, zt *v1.ZedToken) (*v1.ZedToken, error) {
			req.Consistency = consistencyForZedToken(zt)
			resp, err := client.LookupSubjects(ctx, req)
			if err != nil {
				return nil, err
			}

			return zt, verifyExpectedStreamCount(resp, &v1.LookupSubjectsResponse{}, step.NumExpected, "LookupResources error: %w")
		}
	case "WriteRelationships":
		updates, err := parseUpdates(step.Updates)
		if err != nil {
			return executableStep{}, fmt.Errorf("error parsing WriteRelationships updates: %w", err)
		}
		req := &v1.WriteRelationshipsRequest{
			Updates: updates,
		}

		execStep.body = func(ctx context.Context, client *authzed.Client, _ *v1.ZedToken) (*v1.ZedToken, error) {
			resp, err := client.WriteRelationships(ctx, req)
			if err != nil {
				return nil, err
			}
			return resp.WrittenAt, nil
		}
	case "WriteSchema":
		req := &v1.WriteSchemaRequest{
			Schema: step.Schema,
		}

		execStep.body = func(ctx context.Context, client *authzed.Client, zt *v1.ZedToken) (*v1.ZedToken, error) {
			_, err := client.WriteSchema(ctx, req)
			if err != nil {
				return nil, err
			}
			return zt, nil
		}
	case "CheckBulkPermissions":
		// Set up the check request
		items := make([]*v1.CheckBulkPermissionsRequestItem, 0, len(step.Checks))
		for _, check := range step.Checks {
			resource, err := parseObject(check.Resource)
			if err != nil {
				return executableStep{}, fmt.Errorf("error parsing CheckBulkPermissions resource: %w", err)
			}

			subject, err := parseSubject(check.Subject)
			if err != nil {
				return executableStep{}, fmt.Errorf("error parsing CheckBulkPermissions subject: %w", err)
			}
			items = append(items, &v1.CheckBulkPermissionsRequestItem{
				Resource:   resource,
				Permission: check.Permission,
				Subject:    subject,
				Context:    (*structpb.Struct)(check.Context),
			})
		}

		execStep.body = func(ctx context.Context, client *authzed.Client, zt *v1.ZedToken) (*v1.ZedToken, error) {
			req := &v1.CheckBulkPermissionsRequest{
				Consistency: consistencyForZedToken(zt),
				Items:       items,
			}
			resp, err := client.CheckBulkPermissions(ctx, req)
			if err != nil {
				return nil, err
			}

			// NOTE: this depends on the response ordering being the same as
			// the request ordering, which should be an assumption we can make.
			for index, pair := range resp.Pairs {
				check := step.Checks[index]

				expected := v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION
				if check.ExpectNoPermission {
					expected = v1.CheckPermissionResponse_PERMISSIONSHIP_NO_PERMISSION
				}

				switch check.ExpectPermissionship {
				case "HAS_PERMISSION":
					expected = v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION
				case "NO_PERMISSION":
					expected = v1.CheckPermissionResponse_PERMISSIONSHIP_NO_PERMISSION
				case "CONDITIONAL_PERMISSION":
					expected = v1.CheckPermissionResponse_PERMISSIONSHIP_CONDITIONAL_PERMISSION
				}

				if permissionship := pair.GetItem().Permissionship; permissionship != expected {
					return nil, fmt.Errorf(
						"CheckBulkPermissions returned wrong permissionship: %s#%s@%s => %s",
						check.Resource,
						check.Permission,
						check.Subject,
						permissionship,
					)
				}
			}

			return resp.CheckedAt, nil
		}
	default:
		return executableStep{}, fmt.Errorf("unknown script step operation: %s", step.Op)
	}

	return execStep, nil
}

type consistencyFunc func(zt *v1.ZedToken) *v1.Consistency

var fullConsistency = &v1.Consistency{
	Requirement: &v1.Consistency_FullyConsistent{FullyConsistent: true},
}

var minimizeLatency = &v1.Consistency{
	Requirement: &v1.Consistency_MinimizeLatency{MinimizeLatency: true},
}

func prepareConsistency(step config.ScriptStep) (consistencyFunc, string, error) {
	switch step.Consistency {
	case "", "MinimizeLatency":
		return func(_ *v1.ZedToken) *v1.Consistency {
			return minimizeLatency
		}, "MinimizeLatency", nil
	case "AtLeastAsFresh":
		return func(zt *v1.ZedToken) *v1.Consistency {
			if zt != nil {
				return &v1.Consistency{
					Requirement: &v1.Consistency_AtLeastAsFresh{AtLeastAsFresh: zt},
				}
			}

			log.Warn().Msg("AtLeastAsFresh consistency requested, no zedtoken, using full consistency")
			return fullConsistency
		}, step.Consistency, nil
	case "AtExactSnapshot":
		return func(zt *v1.ZedToken) *v1.Consistency {
			if zt != nil {
				return &v1.Consistency{
					Requirement: &v1.Consistency_AtExactSnapshot{AtExactSnapshot: zt},
				}
			}

			log.Warn().Msg("AtExactSnapshot consistency requested, no zedtoken, using full consistency")
			return fullConsistency
		}, step.Consistency, nil
	case "FullyConsistent":
		return func(_ *v1.ZedToken) *v1.Consistency {
			return fullConsistency
		}, step.Consistency, nil

	default:
		return nil, "", fmt.Errorf("unknown consistency type requested: %s", step.Consistency)
	}
}

func verifyExpectedStreamCount(stream grpc.ClientStream, msg proto.Message, numExpected uint, errMsg string) error {
	var received uint

	for err := stream.RecvMsg(msg); !errors.Is(err, io.EOF); err = stream.RecvMsg(msg) {
		if err != nil {
			return fmt.Errorf(errMsg, err)
		}
		received++
	}

	if received != numExpected {
		return fmt.Errorf(
			errMsg,
			fmt.Errorf("wrong number of stream objects received %d != %d", numExpected, received),
		)
	}

	return nil
}

func parseComponents(obj string) (objType string, objID string, relation string) {
	rootAndRelation := strings.SplitN(obj, "#", 2)
	if len(rootAndRelation) > 1 {
		relation = rootAndRelation[1]
	}
	typeAndID := strings.SplitN(rootAndRelation[0], ":", 2)
	objType = typeAndID[0]
	if len(typeAndID) > 1 {
		objID = typeAndID[1]
	}
	return objType, objID, relation
}

func parseObject(obj string) (*v1.ObjectReference, error) {
	objType, objID, rel := parseComponents(obj)
	if rel != "" {
		panic(fmt.Sprintf("invalid object %s: unexpected relation: %s", obj, rel))
	}
	return &v1.ObjectReference{
		ObjectType: objType,
		ObjectId:   objID,
	}, nil
}

func parseSubject(sub string) (*v1.SubjectReference, error) {
	objType, objID, rel := parseComponents(sub)
	if objType == "" {
		panic(fmt.Sprintf("invalid subject %s: missing subject type", sub))
	}
	if objID == "" {
		panic(fmt.Sprintf("invalid subject %s: missing object ID", sub))
	}
	return &v1.SubjectReference{
		Object: &v1.ObjectReference{
			ObjectType: objType,
			ObjectId:   objID,
		},
		OptionalRelation: rel,
	}, nil
}

func parseRelationshipFilter(resource, relation, sub string) (*v1.RelationshipFilter, error) {
	filter := &v1.RelationshipFilter{}
	resType, resID, resRel := parseComponents(resource)
	if resRel != "" {
		panic(fmt.Sprintf("invalid resource %s: unexpected relation: %s", resource, resRel))
	}
	filter.ResourceType = resType
	filter.OptionalResourceId = resID
	filter.OptionalRelation = relation

	if sub != "" {
		subType, subID, subRel := parseComponents(sub)
		filter.OptionalSubjectFilter = &v1.SubjectFilter{
			SubjectType:       subType,
			OptionalSubjectId: subID,
		}

		if subRel != "" {
			filter.OptionalSubjectFilter.OptionalRelation = &v1.SubjectFilter_RelationFilter{
				Relation: subRel,
			}
		}
	}

	return filter, nil
}

func parseUpdates(stepUpdates []config.Update) ([]*v1.RelationshipUpdate, error) {
	updates := make([]*v1.RelationshipUpdate, 0, len(stepUpdates))
	for _, su := range stepUpdates {
		var op v1.RelationshipUpdate_Operation
		switch su.Op {
		case "TOUCH":
			op = v1.RelationshipUpdate_OPERATION_TOUCH
		case "CREATE":
			op = v1.RelationshipUpdate_OPERATION_CREATE
		case "DELETE":
			op = v1.RelationshipUpdate_OPERATION_DELETE
		default:
			panic(fmt.Sprintf("unknown WriteRelationships update operation: %s", su.Op))
		}

		res, err := parseObject(su.Resource)
		if err != nil {
			return nil, fmt.Errorf("error parsing update resource: %w", err)
		}

		sub, err := parseSubject(su.Subject)
		if err != nil {
			return nil, fmt.Errorf("error parsing update subject: %w", err)
		}

		var caveat *v1.ContextualizedCaveat
		if su.Caveat != nil {
			caveat = &v1.ContextualizedCaveat{
				CaveatName: su.Caveat.Name,
				Context:    (*structpb.Struct)(su.Caveat.Context),
			}
		}

		updates = append(updates, &v1.RelationshipUpdate{
			Operation: op,
			Relationship: &v1.Relationship{
				Resource:       res,
				Relation:       su.Relation,
				Subject:        sub,
				OptionalCaveat: caveat,
			},
		})
	}

	return updates, nil
}
