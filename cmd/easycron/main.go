package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/djlord-it/easy-cron/internal/analytics"
	"github.com/djlord-it/easy-cron/internal/api"
	"github.com/djlord-it/easy-cron/internal/config"
	"github.com/djlord-it/easy-cron/internal/cron"
	"github.com/djlord-it/easy-cron/internal/dispatcher"
	"github.com/djlord-it/easy-cron/internal/metrics"
	"github.com/djlord-it/easy-cron/internal/reconciler"
	"github.com/djlord-it/easy-cron/internal/scheduler"
	"github.com/djlord-it/easy-cron/internal/store/postgres"
	"github.com/djlord-it/easy-cron/internal/transport/channel"

	_ "github.com/lib/pq"
)

// cronParserAdapter adapts internal/cron.Parser to scheduler.CronParser interface.
type cronParserAdapter struct {
	parser *cron.Parser
}

func (a *cronParserAdapter) Parse(expression string, timezone string) (scheduler.CronSchedule, error) {
	sched, err := a.parser.Parse(expression, timezone)
	if err != nil {
		return nil, err
	}
	return &cronScheduleAdapter{sched: sched}, nil
}

// cronScheduleAdapter adapts internal/cron.Schedule to scheduler.CronSchedule interface.
type cronScheduleAdapter struct {
	sched cron.Schedule
}

func (a *cronScheduleAdapter) Next(after time.Time) time.Time {
	return a.sched.Next(after)
}

// Build-time variables set via -ldflags
var (
	version = "dev"
	commit  = "unknown"
)

const (
	exitSuccess       = 0
	exitRuntimeError  = 1
	exitInvalidConfig = 2
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(exitRuntimeError)
	}

	cmd := os.Args[1]

	switch cmd {
	case "serve":
		os.Exit(runServe())
	case "validate":
		os.Exit(runValidate())
	case "config":
		os.Exit(runConfig())
	case "version":
		os.Exit(runVersion())
	case "--help", "-h", "help":
		printUsage()
		os.Exit(exitSuccess)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(exitRuntimeError)
	}
}

func printUsage() {
	fmt.Println(`easycron - distributed cron job scheduler

Usage:
  easycron <command>

Commands:
  serve      Start the scheduler and dispatcher
  validate   Validate configuration (no connections made)
  config     Print effective configuration as JSON (secrets masked)
  version    Print version information

Environment Variables:
  DATABASE_URL              PostgreSQL connection string (required)
  REDIS_ADDR                Redis address for analytics (optional)
  HTTP_ADDR                 HTTP server address (default: ":8080")
  TICK_INTERVAL             Scheduler tick interval (default: "30s")

  DB_OP_TIMEOUT             Database operation timeout (default: "5s")
  DB_MAX_OPEN_CONNS         Max open database connections (default: "25")
  DB_MAX_IDLE_CONNS         Max idle database connections (default: "5")
  DB_CONN_MAX_LIFETIME      Max connection lifetime (default: "30m")
  DB_CONN_MAX_IDLE_TIME     Max connection idle time (default: "5m")

  HTTP_SHUTDOWN_TIMEOUT     Graceful HTTP shutdown timeout (default: "10s")
  DISPATCHER_DRAIN_TIMEOUT  Dispatcher event drain timeout (default: "30s")

  METRICS_ENABLED           Enable Prometheus metrics (default: "false")
  METRICS_PATH              Metrics endpoint path (default: "/metrics")
  METRICS_PORT              Metrics server port (default: "9090")

  RECONCILE_ENABLED         Enable orphan execution reconciler (default: "false")
  RECONCILE_INTERVAL        How often to scan for orphans (default: "5m")
  RECONCILE_THRESHOLD       Age before execution is orphaned (default: "10m")
  RECONCILE_BATCH_SIZE      Max orphans per cycle (default: "100")`)
}

func runServe() int {
	cfg := config.Load()

	if err := config.Validate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		return exitInvalidConfig
	}

	// Connect to PostgreSQL
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open database: %v\n", err)
		return exitRuntimeError
	}
	defer db.Close()

	// Configure connection pool
	db.SetMaxOpenConns(cfg.DBMaxOpenConns)
	db.SetMaxIdleConns(cfg.DBMaxIdleConns)
	db.SetConnMaxLifetime(cfg.DBConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.DBConnMaxIdleTime)

	log.Printf("easycron: db pool configured (max_open=%d, max_idle=%d, max_lifetime=%s, max_idle_time=%s)",
		cfg.DBMaxOpenConns, cfg.DBMaxIdleConns, cfg.DBConnMaxLifetime, cfg.DBConnMaxIdleTime)

	if err := db.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to database: %v\n", err)
		return exitRuntimeError
	}

	store := postgres.New(db, cfg.DBOpTimeout)
	cronParser := &cronParserAdapter{parser: cron.NewParser()}
	webhookSender := dispatcher.NewHTTPWebhookSender()

	// Initialize metrics sink (optional)
	var metricsSink *metrics.PrometheusSink
	var metricsServer *http.Server

	if cfg.MetricsEnabled {
		metricsSink = metrics.NewPrometheusSink(prometheus.DefaultRegisterer)
		log.Printf("easycron: metrics enabled (port=%s, path=%s)", cfg.MetricsPort, cfg.MetricsPath)

		// Start metrics HTTP server on separate port
		metricsMux := http.NewServeMux()
		metricsMux.Handle(cfg.MetricsPath, promhttp.Handler())
		metricsServer = &http.Server{
			Addr:    ":" + cfg.MetricsPort,
			Handler: metricsMux,
		}
		go func() {
			log.Printf("easycron: metrics server listening on :%s", cfg.MetricsPort)
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("easycron: metrics server error: %v", err)
			}
		}()
	} else {
		log.Println("easycron: METRICS_ENABLED not set; metrics disabled")
	}

	// Create event bus with optional metrics
	var busOpts []channel.Option
	if metricsSink != nil {
		busOpts = append(busOpts, channel.WithMetrics(metricsSink))
	}
	bus := channel.NewEventBus(100, busOpts...)

	sched := scheduler.New(
		scheduler.Config{TickInterval: cfg.TickInterval},
		store,
		cronParser,
		bus,
	)
	if metricsSink != nil {
		sched = sched.WithMetrics(metricsSink)
	}

	disp := dispatcher.New(store, webhookSender).
		WithDrainTimeout(cfg.DispatcherDrainTimeout)
	if metricsSink != nil {
		disp = disp.WithMetrics(metricsSink)
	}

	// Wire analytics if Redis is configured
	if cfg.RedisAddr != "" {
		redisClient := redis.NewClient(&redis.Options{
			Addr: cfg.RedisAddr,
		})
		sink := analytics.NewRedisSink(redisClient)
		disp = disp.WithAnalytics(sink)
		log.Printf("easycron: analytics enabled (redis=%s)", cfg.RedisAddr)
	} else {
		log.Println("easycron: REDIS_ADDR not set; analytics disabled")
	}

	// Create API handler with the same store instance
	// Using a fixed project ID for single-tenant mode
	projectID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	apiHandler := api.NewHandler(store, projectID).WithHealthChecker(db)

	// Start HTTP server with API handler
	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: apiHandler,
	}

	go func() {
		log.Printf("easycron: http server listening on %s", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("easycron: http server error: %v", err)
		}
	}()

	// Use separate contexts for scheduler, dispatcher, and reconciler to enable ordered shutdown.
	schedulerCtx, cancelScheduler := context.WithCancel(context.Background())
	dispatcherCtx, cancelDispatcher := context.WithCancel(context.Background())

	var schedulerWg sync.WaitGroup
	var dispatcherWg sync.WaitGroup
	var reconcilerWg sync.WaitGroup
	var cancelReconciler context.CancelFunc

	schedulerWg.Add(1)
	go func() {
		defer schedulerWg.Done()
		sched.Run(schedulerCtx)
	}()

	dispatcherWg.Add(1)
	go func() {
		defer dispatcherWg.Done()
		disp.Run(dispatcherCtx, bus.Channel())
	}()

	// Start reconciler if enabled
	if cfg.ReconcileEnabled {
		var reconcilerCtx context.Context
		reconcilerCtx, cancelReconciler = context.WithCancel(context.Background())
		recon := reconciler.New(
			reconciler.Config{
				Interval:  cfg.ReconcileInterval,
				Threshold: cfg.ReconcileThreshold,
				BatchSize: cfg.ReconcileBatchSize,
			},
			store,
			bus,
		)
		if metricsSink != nil {
			recon = recon.WithMetrics(metricsSink)
		}
		reconcilerWg.Add(1)
		go func() {
			defer reconcilerWg.Done()
			recon.Run(reconcilerCtx)
		}()
		log.Printf("easycron: reconciler enabled (interval=%s, threshold=%s, batch=%d)",
			cfg.ReconcileInterval, cfg.ReconcileThreshold, cfg.ReconcileBatchSize)
	} else {
		log.Println("easycron: RECONCILE_ENABLED not set; reconciler disabled")
	}

	log.Printf("easycron: started (tick=%s, http=%s)", cfg.TickInterval, cfg.HTTPAddr)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	received := <-sig

	log.Printf("easycron: received signal %v, shutting down", received)

	// Phase 1: Stop scheduler (no new events emitted)
	log.Println("easycron: stopping scheduler...")
	cancelScheduler()
	schedulerWg.Wait()
	log.Println("easycron: scheduler stopped")

	// Phase 2: Stop reconciler (no new re-emits)
	if cancelReconciler != nil {
		log.Println("easycron: stopping reconciler...")
		cancelReconciler()
		reconcilerWg.Wait()
		log.Println("easycron: reconciler stopped")
	}

	// Phase 3: Stop dispatcher (will drain buffered events before returning)
	log.Println("easycron: stopping dispatcher (draining events)...")
	cancelDispatcher()
	dispatcherWg.Wait()
	log.Println("easycron: dispatcher stopped")

	// Phase 4: Stop HTTP server with graceful shutdown
	log.Println("easycron: stopping http server...")
	httpShutdownCtx, httpShutdownCancel := context.WithTimeout(context.Background(), cfg.HTTPShutdownTimeout)
	defer httpShutdownCancel()
	if err := httpServer.Shutdown(httpShutdownCtx); err != nil {
		log.Printf("easycron: http server shutdown error: %v", err)
	}
	log.Println("easycron: http server stopped")

	// Phase 5: Stop metrics server if running (with same timeout)
	if metricsServer != nil {
		log.Println("easycron: stopping metrics server...")
		metricsShutdownCtx, metricsShutdownCancel := context.WithTimeout(context.Background(), cfg.HTTPShutdownTimeout)
		defer metricsShutdownCancel()
		if err := metricsServer.Shutdown(metricsShutdownCtx); err != nil {
			log.Printf("easycron: metrics server shutdown error: %v", err)
		}
		log.Println("easycron: metrics server stopped")
	}

	log.Println("easycron: stopped")
	return exitSuccess
}

func runValidate() int {
	cfg := config.Load()

	if err := config.Validate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return exitInvalidConfig
	}

	fmt.Println("configuration valid")
	return exitSuccess
}

func runConfig() int {
	cfg := config.Load()

	data, err := cfg.MaskedJSON()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal config: %v\n", err)
		return exitRuntimeError
	}

	fmt.Println(string(data))
	return exitSuccess
}

func runVersion() int {
	fmt.Printf("easycron version %s (commit: %s)\n", version, commit)
	return exitSuccess
}
