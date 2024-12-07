package main

import (
	"os"

	"github.com/authzed/internal/thumper/internal/cmd"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/jzelinskie/cobrautil/v2"
	"github.com/jzelinskie/cobrautil/v2/cobrazerolog"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var buckets = []float64{.006, .010, .018, .024, .032, .042, .056, .075, .100, .178, .316, .562, 1.000}

func main() {
	// GCP stackdriver compatible logs
	zl := cobrazerolog.New(cobrazerolog.WithPreRunLevel(zerolog.DebugLevel))
	zerolog.LevelFieldName = "severity"
	grpc_prometheus.EnableClientHandlingTimeHistogram(grpc_prometheus.WithHistogramBuckets(buckets))

	rootCmd := &cobra.Command{
		Use:               "thumper",
		Short:             "The Authzed Load Generator",
		Long:              "An artificial load generator for managing health and performance of Authzed.",
		PersistentPreRunE: cmd.SyncFlagsCmdFunc,
	}

	zl.RegisterFlags(rootCmd.PersistentFlags())

	rootCmd.PersistentFlags().String("permissions-system", "thumper", "permissions system to query")
	rootCmd.PersistentFlags().String("endpoint", "localhost:50051", "authzed gRPC API endpoint")
	rootCmd.PersistentFlags().String("token", "", "token used to authenticate to authzed")
	rootCmd.PersistentFlags().Bool("insecure", true, "connect over a plaintext connection")
	rootCmd.PersistentFlags().Bool("no-verify-ca", false, "do not attempt to verify the server's certificate chain and host name")
	rootCmd.PersistentFlags().String("ca-path", "", "override root certificate path")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "display thumper version information",
		RunE:  cobrautil.VersionRunFunc("thumper"),
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