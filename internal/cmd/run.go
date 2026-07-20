package cmd

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"sync"
	"time"

	thumperconf "github.com/authzed/internal/thumper/internal/config"
	"github.com/authzed/internal/thumper/internal/thumperrunner"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"github.com/go-logr/logr"
	"github.com/jzelinskie/cobrautil/v2"
	"github.com/jzelinskie/cobrautil/v2/cobrahttp"
	"github.com/jzelinskie/cobrautil/v2/cobraotel"
	"github.com/jzelinskie/cobrautil/v2/cobraproclimits"
	"github.com/jzelinskie/cobrautil/v2/cobrazerolog"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	SyncFlagsCmdFunc = cobrautil.SyncViperPreRunE("THUMPER")
	// TODO: this seems weird, but I'm not sure where to initialize it such that
	//  it's accessible when setting up flags and also when initializing the command
	MetricsServerBuilder = cobrahttp.New("metrics",
		cobrahttp.WithDefaultAddress(":9090"),
		cobrahttp.WithFlagPrefix("metrics"),
		cobrahttp.WithDefaultEnabled(true),
		cobrahttp.WithHandler(promhttp.Handler()))
	// PProfServerBuilder serves the net/http/pprof profiling endpoints. It is
	// disabled by default and enabled with --pprof-enabled.
	PProfServerBuilder = cobrahttp.New("pprof",
		cobrahttp.WithDefaultAddress(":6060"),
		cobrahttp.WithFlagPrefix("pprof"),
		cobrahttp.WithDefaultEnabled(false),
		cobrahttp.WithHandler(pprofHandler()))
)

// pprofHandler returns a mux serving the standard net/http/pprof endpoints
// under /debug/pprof/.
func pprofHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

func RegisterRunFlags(cmd *cobra.Command) {
	cmd.Flags().Int("qps", 1, "queries per second to generate")
	cmd.Flags().Duration("step-timeout", 500*time.Millisecond, "maximum time a single step is allowed to run")
	cmd.Flags().Duration("step-interval", time.Second, "time each worker waits between steps; lower it to drive more steps per worker (effective throughput is qps / step-interval)")
	cmd.Flags().Bool("randomize-starting-step", false, "randomize the starting script step for each worker")

	// Register http flags
	MetricsServerBuilder.RegisterFlags(cmd.Flags())
	PProfServerBuilder.RegisterFlags(cmd.Flags())
}

var RunCmd = &cobra.Command{
	Use:   "run script.yaml [script2.yaml] [script3.yaml]",
	Short: "run traffic generator",
	Example: `
	Run with a single script against a local SpiceDB:
		thumper run ./scripts/script.yaml --token "testtesttesttest"

	Run against authzed.com:
		thumper run ./scripts/script.yaml --token "tc_test_123123123" --endpoint grpc.authzed.com --insecure=false --permissions-system mypermissionssystem
	
	Run with environment variables:
		THUMPER_TOKEN=testtesttesttest thumper run ./scripts/script.yaml
	`,
	Args:    cobra.MinimumNArgs(1),
	RunE:    runCmdFunc,
	PreRunE: DefaultPreRunE("thumper"),
}

func runCmdFunc(cmd *cobra.Command, args []string) error {
	qps := cobrautil.MustGetInt(cmd, "qps")
	stepTimeout := cobrautil.MustGetDuration(cmd, "step-timeout")
	stepInterval := cobrautil.MustGetDuration(cmd, "step-interval")
	stepRandomization := cobrautil.MustGetBool(cmd, "randomize-starting-step")
	psName := cobrautil.MustGetString(cmd, "permissions-system")
	log.Info().Int("qps", qps).Str("permission-system", psName).Msg("starting run command")

	scriptVars := thumperconf.ScriptVariables{}
	if psName != "" {
		scriptVars.Prefix = fmt.Sprintf("%s/", psName)
	}

	// Keep track of the total stats for all workers
	var scriptsForStats []*thumperconf.Script

	// Parse each unique script file exactly once. Scripts that use
	// randomObjectID keep their placeholders here; each worker resolves them to
	// fresh values at runtime (see PrepareRenderable), so the YAML is never
	// re-parsed no matter how long the run lasts. Static scripts are identical
	// across workers, so they are prepared once here and shared by pointer.
	type loadedFile struct {
		scripts    []*thumperconf.Script
		usedRandom bool
		prepared   []*thumperrunner.ExecutableScript // set only for static scripts
	}
	files := make(map[string]loadedFile, len(args))
	for _, scriptFilename := range args {
		if _, ok := files[scriptFilename]; ok {
			continue
		}

		scripts, usedRandom, err := thumperconf.Load(scriptFilename, scriptVars)
		if err != nil {
			return fmt.Errorf("unable to load script file: %w", err)
		}

		loaded := loadedFile{scripts: scripts, usedRandom: usedRandom}
		if !usedRandom {
			if loaded.prepared, err = thumperrunner.Prepare(scripts); err != nil {
				return fmt.Errorf("error preparing scripts for execution: %w", err)
			}
		}
		files[scriptFilename] = loaded
		scriptsForStats = append(scriptsForStats, scripts...)
	}

	// Build one set of executable scripts per worker. Scripts with random IDs
	// get their own renderable copy per worker (so a randomized starting step
	// can't make workers emit identical IDs during their first partial cycle);
	// static scripts reuse the single shared Prepare result from above.
	workerScripts := make([][]*thumperrunner.ExecutableScript, 0, qps)
	for i := 0; i < qps; i++ {
		var preparedScripts []*thumperrunner.ExecutableScript
		for _, scriptFilename := range args {
			loaded := files[scriptFilename]

			if !loaded.usedRandom {
				preparedScripts = append(preparedScripts, loaded.prepared...)
				continue
			}

			renderable, err := thumperrunner.PrepareRenderable(loaded.scripts)
			if err != nil {
				return fmt.Errorf("error preparing scripts for execution: %w", err)
			}
			preparedScripts = append(preparedScripts, renderable...)
		}

		workerScripts = append(workerScripts, preparedScripts)
	}

	for op, probability := range thumperconf.Stats(scriptsForStats) {
		log.Info().Float32("probability", probability).Str("op", op).Msg("op probability")
	}

	//	Kick off the workers.
	//	TODO(jschorr): Add automatic disconnect if we start receiving too many errors.
	var wg sync.WaitGroup
	// Stagger worker starts evenly across one step interval so their ticks are
	// spread out rather than firing all at once.
	timeBetween := stepInterval / time.Duration(qps)
	for i := 0; i < qps; i++ {
		wg.Add(1)
		index := i
		go func() {
			defer wg.Done()

			client := clientFromFlags(cmd)
			thumperrunner.RunWorker(thumperrunner.WorkerOptions{
				Index:             index,
				Client:            client,
				Scripts:           workerScripts[index],
				StepTimeout:       stepTimeout,
				StepInterval:      stepInterval,
				StepRandomization: stepRandomization,
			})
		}()
		time.Sleep(timeBetween)
	}

	// Start the metrics endpoint.
	metricsSrv := MetricsServerBuilder.ServerFromFlags(cmd)
	go func() {
		if err := MetricsServerBuilder.ListenFromFlags(cmd, metricsSrv); err != nil {
			log.Fatal().Err(err).Msg("failed while serving metrics")
		}
	}()

	// Start the pprof endpoint. Disabled by default; ListenFromFlags is a no-op
	// unless --pprof-enabled is set.
	pprofSrv := PProfServerBuilder.ServerFromFlags(cmd)
	go func() {
		if err := PProfServerBuilder.ListenFromFlags(cmd, pprofSrv); err != nil {
			log.Fatal().Err(err).Msg("failed while serving pprof")
		}
	}()

	wg.Wait()
	log.Info().Msg("terminating")

	return nil
}

// DefaultPreRunE sets up viper, zerolog, and OpenTelemetry flag handling for a command.
func DefaultPreRunE(programName string) cobrautil.CobraRunFunc {
	return cobrautil.CommandStack(
		cobrazerolog.New(
			cobrazerolog.WithPreRunLevel(zerolog.DebugLevel),
			cobrazerolog.WithTarget(func(logger zerolog.Logger) {
				log.Logger = logger
				zerolog.DefaultContextLogger = &logger
				zerolog.SetGlobalLevel(logger.GetLevel())
			}),
		).RunE(),
		// TODO we don't wire logger for now because cobrazerolog is not executed by the time we reference the logger
		//  in the method call here
		cobrautil.SyncViperDotEnvPreRunE(programName, "thumper.env", logr.Discard()),
		cobraproclimits.SetMemLimitRunE(memlimit.WithRatio(1.0)),
		cobraproclimits.SetProcLimitRunE(),
		cobraotel.New("thumper").RunE(),
	)
}
