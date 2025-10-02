package cmd

import (
	"fmt"

	thumperconf "github.com/authzed/internal/thumper/internal/config"
	"github.com/authzed/internal/thumper/internal/thumperrunner"

	"github.com/authzed/authzed-go/v1"
	"github.com/authzed/grpcutil"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/jzelinskie/cobrautil/v2"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var MigrateCmd = &cobra.Command{
	Use:   "migrate migration.yaml [migration2.yaml] [migration3.yaml]",
	Short: "run setup scripts",
	Example: `
	Run with a single script against a local SpiceDB:
		thumper migrate ./scripts/schema.yaml --token "testtesttesttest"

	Run against authzed.com:
		thumper migrate ./scripts/schema.yaml --token "tc_test_123123123" --endpoint grpc.authzed.com --insecure=false --permissions-system mypermissionssystem
	
	Run with environment variables:
		THUMPER_TOKEN=testtesttesttest thumper migrate ./scripts/schema.yaml
	`,
	Args:    cobra.MinimumNArgs(1),
	RunE:    migrateCmdFunc,
	PreRunE: DefaultPreRunE("thumper"),
}

func migrateCmdFunc(cmd *cobra.Command, args []string) error {
	client := clientFromFlags(cmd)
	psName := cobrautil.MustGetString(cmd, "permissions-system")

	// Load the migration scripts
	var preparedScripts []*thumperrunner.ExecutableScript
	for _, scriptFilename := range args {
		scriptVars := thumperconf.ScriptVariables{
			IsMigration: true,
		}
		if psName != "" {
			scriptVars.Prefix = fmt.Sprintf("%s/", psName)
		}

		fileScripts, _, err := thumperconf.Load(scriptFilename, scriptVars)
		if err != nil {
			return fmt.Errorf("unable to load script file: %w", err)
		}

		preparedFileScripts, err := thumperrunner.Prepare(fileScripts)
		if err != nil {
			return fmt.Errorf("error preparing scripts for execution: %w", err)
		}

		preparedScripts = append(preparedScripts, preparedFileScripts...)
	}

	// Run the scripts in order
	for _, script := range preparedScripts {
		if err := script.RunOnce(client); err != nil {
			return fmt.Errorf("error running migration scripts: %w", err)
		}
	}

	return nil
}

func clientFromFlags(cmd *cobra.Command) *authzed.Client {
	token := cobrautil.MustGetString(cmd, "token")
	endpoint := cobrautil.MustGetString(cmd, "endpoint")

	opts := []grpc.DialOption{
		grpc.WithUnaryInterceptor(grpc_prometheus.UnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpc_prometheus.StreamClientInterceptor),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`),
	}
	if cobrautil.MustGetBool(cmd, "insecure") {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		opts = append(opts, grpcutil.WithInsecureBearerToken(token))
	} else {
		opts = append(opts, grpcutil.WithBearerToken(token))
		rootCA := cobrautil.MustGetString(cmd, "ca-path")
		verificationOption := grpcutil.VerifyCA
		if cobrautil.MustGetBool(cmd, "no-verify-ca") {
			verificationOption = grpcutil.SkipVerifyCA
		}
		if rootCA != "" {
			opt, err := grpcutil.WithCustomCerts(verificationOption, rootCA)
			if err != nil {
				log.Fatal().Err(err).Msg("unable to initialize custom certs")
			}
			opts = append(opts, opt)
		} else {
			opt, err := grpcutil.WithSystemCerts(verificationOption)
			if err != nil {
				log.Fatal().Err(err).Msg("unable to initialize system certs")
			}
			opts = append(opts, opt)
		}
	}

	v1client, err := authzed.NewClient(endpoint, opts...)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to initialize v1 client")
	}

	return v1client
}
