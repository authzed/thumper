package main

import (
	"os"

	"github.com/authzed/internal/thumper/internal/cmd"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/jzelinskie/cobrautil/v2"
	"github.com/jzelinskie/cobrautil/v2/cobraotel"
	"github.com/jzelinskie/cobrautil/v2/cobrazerolog"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var buckets = []float64{.006, .010, .018, .024, .032, .042, .056, .075, .100, .178, .316, .562, 1.000}

func main() {
	// GCP stackdriver compatible logs
	zerolog.LevelFieldName = "severity"
	grpc_prometheus.EnableClientHandlingTimeHistogram(grpc_prometheus.WithHistogramBuckets(buckets))

	rootCmd := &cobra.Command{
		Use:               "thumper",
		Short:             "SpiceDB Traffic Generator",
		Long:              "An artificial traffic generator and availability probe.",
		PersistentPreRunE: cmd.SyncFlagsCmdFunc,
		PreRunE:           cmd.DefaultPreRunE("thumper"),
		SilenceUsage:      true,
	}

	cobrazerolog.New().RegisterFlags(rootCmd.PersistentFlags())
	if err := cobrazerolog.New().RegisterFlagCompletion(rootCmd); err != nil {
		log.Logger.Fatal().Err(err).Msg("failed to register log flag completion")
	}
	cobraotel.New("thumper").RegisterFlags(rootCmd.PersistentFlags())

	rootCmd.PersistentFlags().String("permissions-system", "thumper", "permissions system to query")
	rootCmd.PersistentFlags().String("endpoint", "localhost:50051", "authzed gRPC API endpoint")
	rootCmd.PersistentFlags().String("token", "", "token used to authenticate to authzed")
	rootCmd.PersistentFlags().Bool("insecure", false, "connect over a plaintext connection")
	rootCmd.PersistentFlags().Bool("no-verify-ca", false, "do not attempt to verify the server's certificate chain and host name")
	rootCmd.PersistentFlags().String("ca-path", "", "override root certificate path")

	versionCmd := &cobra.Command{
		Use:     "version",
		Short:   "display thumper version information",
		RunE:    cobrautil.VersionRunFunc("thumper"),
		PreRunE: cmd.DefaultPreRunE("thumper"),
	}
	cobrautil.RegisterVersionFlags(versionCmd.Flags())
	rootCmd.AddCommand(versionCmd)

	cmd.RegisterRunFlags(cmd.RunCmd)
	rootCmd.AddCommand(cmd.RunCmd)

	rootCmd.AddCommand(cmd.MigrateCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
