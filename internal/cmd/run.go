package cmd

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	thumperconf "github.com/authzed/internal/thumper/internal/config"
	"github.com/authzed/internal/thumper/internal/thumperrunner"

	"github.com/jzelinskie/cobrautil"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	SyncFlagsCmdFunc = cobrautil.SyncViperPreRunE("THUMPER")
	LogCmdFunc       = cobrautil.ZeroLogRunE("log", zerolog.DebugLevel)
)

func init() {
	rand.Seed(time.Now().Unix())
}

func RegisterRunFlags(cmd *cobra.Command) {
	cmd.Flags().Int("qps", 1, "QPS with which to call authzed")
	cmd.Flags().Duration("step-timeout", 500*time.Millisecond, "maximum time a single step is allowed to run")
	cmd.Flags().Bool("randomize-starting-step", false, "randomize the starting script step for each worker")
	cobrautil.RegisterHTTPServerFlags(cmd.Flags(), "metrics", "metrics", ":9090", true)
}

var RunCmd = &cobra.Command{
	Use:   "run script.yaml [script2.yaml] [script3.yaml]",
	Short: "run thumper load generator",
	Example: `
	Run with a single script against a local SpiceDB:
		thumper run ./scripts/script.yaml --token "testtesttesttest"

	Run against authzed.com:
		thumper run ./scripts/script.yaml --token "tc_test_123123123" --endpoint grpc.authzed.com --insecure=false --permissions-system mypermissionssystem
	
	Run with environment variables:
		THUMPER_TOKEN=testtesttesttest thumper run ./scripts/script.yaml
	`,
	Args: cobra.MinimumNArgs(1),
	RunE: cobrautil.CommandStack(LogCmdFunc, runCmdFunc),
}

func runCmdFunc(cmd *cobra.Command, args []string) error {
	qps := cobrautil.MustGetInt(cmd, "qps")
	stepTimeout := cobrautil.MustGetDuration(cmd, "step-timeout")
	stepRandomization := cobrautil.MustGetBool(cmd, "randomize-starting-step")
	psName := cobrautil.MustGetString(cmd, "permissions-system")
	log.Info().Int("qps", qps).Str("permission-system", psName).Msg("starting run command")

	scriptVars := thumperconf.ScriptVariables{}
	if psName != "" {
		scriptVars.Prefix = fmt.Sprintf("%s/", psName)
	}

	// Keep track of the total stats for all workers
	var scriptsForStats []*thumperconf.Script

	scriptCache := make(map[string][]*thumperrunner.ExecutableScript, len(args))

	// Load the scripts and transform them, one copy per worker
	workerScripts := make([][]*thumperrunner.ExecutableScript, 0, qps)
	for i := 0; i < qps; i++ {
		var preparedScripts []*thumperrunner.ExecutableScript
		for _, scriptFilename := range args {
			if cached, ok := scriptCache[scriptFilename]; ok {
				preparedScripts = append(preparedScripts, cached...)

				// Skip actually loading it from disk
				continue
			}

			fileScripts, usedRandom, err := thumperconf.Load(scriptFilename, scriptVars)
			if err != nil {
				return fmt.Errorf("unable to load script file: %w", err)
			}

			if i == 0 {
				scriptsForStats = append(scriptsForStats, fileScripts...)
			}

			preparedFileScripts, err := thumperrunner.Prepare(fileScripts)
			if err != nil {
				return fmt.Errorf("error preparing scripts for execution: %w", err)
			}

			if !usedRandom {
				scriptCache[scriptFilename] = preparedFileScripts
			}

			preparedScripts = append(preparedScripts, preparedFileScripts...)
		}

		workerScripts = append(workerScripts, preparedScripts)
	}

	for op, probability := range thumperconf.Stats(scriptsForStats) {
		log.Info().Float32("probability", probability).Str("op", op).Msg("op probability")
	}

	//	Kick off the workers.
	//	TODO(jschorr): Add automatic disconnect if we start receiving
	//	too many errors.
	var wg sync.WaitGroup
	timeBetween := time.Duration(1) * time.Second / time.Duration(qps)
	for i := 0; i < qps; i++ {
		wg.Add(1)
		index := i
		go (func() {
			defer wg.Done()

			client := clientFromFlags(cmd)
			thumperrunner.RunWorker(thumperrunner.WorkerOptions{
				Index:             index,
				Client:            client,
				Scripts:           workerScripts[index],
				StepTimeout:       stepTimeout,
				StepRandomization: stepRandomization,
			})
		})()
		time.Sleep(timeBetween)
	}

	// Start the metrics endpoint.
	metricsSrv := cobrautil.HTTPServerFromFlags(cmd, "metrics")
	metricsSrv.Handler = promhttp.Handler()
	go func() {
		if err := cobrautil.HTTPListenFromFlags(cmd, "metrics", metricsSrv, zerolog.InfoLevel); err != nil {
			log.Fatal().Err(err).Msg("failed while serving metrics")
		}
	}()

	wg.Wait()
	log.Info().Msg("terminating")

	return nil
}
