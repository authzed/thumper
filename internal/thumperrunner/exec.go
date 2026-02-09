package thumperrunner

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
)

// Client is an interface that abstracts the SpiceDB API calls used by thumper.
// *authzed.Client satisfies this interface.
type Client interface {
	CheckPermission(ctx context.Context, in *v1.CheckPermissionRequest, opts ...grpc.CallOption) (*v1.CheckPermissionResponse, error)
	ReadRelationships(ctx context.Context, in *v1.ReadRelationshipsRequest, opts ...grpc.CallOption) (v1.PermissionsService_ReadRelationshipsClient, error)
	DeleteRelationships(ctx context.Context, in *v1.DeleteRelationshipsRequest, opts ...grpc.CallOption) (*v1.DeleteRelationshipsResponse, error)
	ExpandPermissionTree(ctx context.Context, in *v1.ExpandPermissionTreeRequest, opts ...grpc.CallOption) (*v1.ExpandPermissionTreeResponse, error)
	LookupResources(ctx context.Context, in *v1.LookupResourcesRequest, opts ...grpc.CallOption) (v1.PermissionsService_LookupResourcesClient, error)
	LookupSubjects(ctx context.Context, in *v1.LookupSubjectsRequest, opts ...grpc.CallOption) (v1.PermissionsService_LookupSubjectsClient, error)
	WriteRelationships(ctx context.Context, in *v1.WriteRelationshipsRequest, opts ...grpc.CallOption) (*v1.WriteRelationshipsResponse, error)
	WriteSchema(ctx context.Context, in *v1.WriteSchemaRequest, opts ...grpc.CallOption) (*v1.WriteSchemaResponse, error)
	CheckBulkPermissions(ctx context.Context, in *v1.CheckBulkPermissionsRequest, opts ...grpc.CallOption) (*v1.CheckBulkPermissionsResponse, error)
}

type executableStep struct {
	op          string
	consistency string
	body        func(context.Context, Client, *v1.ZedToken) (*v1.ZedToken, error)
}

// ExecutableScript is a thumper yaml script that has been post-processed for
// execution efficiency.
type ExecutableScript struct {
	name   string
	weight uint
	steps  []executableStep
}

type ExecutableContext struct {
	script *ExecutableScript
	client Client

	numExecuted int
	zedToken    *v1.ZedToken
}

// StepForward advances the script one step and then stops.
func (s *ExecutableContext) StepForward(workerIndex int, stepTimeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), stepTimeout)
	defer cancel()

	stepNum := s.numExecuted % len(s.script.steps)
	step := s.script.steps[stepNum]

	log.Debug().
		Str("script", s.script.name).
		Int("step", stepNum).
		Int("worker", workerIndex).
		Str("op", step.op).
		Str("consistency", step.consistency).
		Msg("executing script step")

	newToken, err := step.body(ctx, s.client, s.zedToken)
	if err != nil {
		log.Warn().
			Str("script", s.script.name).
			Int("step", stepNum).
			Int("worker", workerIndex).
			Str("op", step.op).
			Str("consistency", step.consistency).
			Err(err).
			Msg("error calling script step")
	}

	s.numExecuted++
	s.zedToken = newToken
}

// RunOnce runs all steps in a script and then stops.
func (s *ExecutableScript) RunOnce(client Client) error {
	log.Info().Str("script", s.name).Msg("running migration script")

	ctx, cancel := context.WithTimeout(context.Background(), 3600*time.Second)
	defer cancel()

	for stepNum, step := range s.steps {
		log.Debug().Int("step", stepNum).Int("total", len(s.steps)).Msg("executing migration step")
		_, err := step.body(ctx, client, nil)
		if err != nil {
			return fmt.Errorf("error running script %s: %w", s.name, err)
		}
	}

	return nil
}
