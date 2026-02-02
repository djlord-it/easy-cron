package metrics

import (
	"log"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// PrometheusSink implements Sink using Prometheus client library.
// All methods are non-blocking and fire-and-forget.
// Registration errors are logged but never propagated.
type PrometheusSink struct {
	// Scheduler metrics
	ticksTotal         prometheus.Counter
	tickErrorsTotal    prometheus.Counter
	jobsTriggeredTotal prometheus.Counter
	tickDuration       prometheus.Histogram
	tickDrift          prometheus.Histogram

	// Dispatcher metrics
	deliveryAttemptsTotal *prometheus.CounterVec
	deliveryOutcomesTotal *prometheus.CounterVec
	webhookDuration       prometheus.Histogram
	retryAttemptsTotal    *prometheus.CounterVec
	eventsInFlight        prometheus.Gauge

	// EventBus metrics
	bufferSize      prometheus.Gauge
	emitErrorsTotal prometheus.Counter
}

// NewPrometheusSink creates a new Prometheus metrics sink.
// If registration fails, it logs a warning and returns a functional sink.
// Metrics that fail to register will be replaced with no-op collectors.
func NewPrometheusSink(reg prometheus.Registerer) *PrometheusSink {
	s := &PrometheusSink{}
	s.initSchedulerMetrics(reg)
	s.initDispatcherMetrics(reg)
	s.initEventBusMetrics(reg)
	return s
}

func (s *PrometheusSink) initSchedulerMetrics(reg prometheus.Registerer) {
	s.ticksTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "easycron_scheduler_ticks_total",
		Help: "Total number of scheduler ticks processed.",
	})
	s.tickErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "easycron_scheduler_tick_errors_total",
		Help: "Total number of scheduler tick errors.",
	})
	s.jobsTriggeredTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "easycron_scheduler_jobs_triggered_total",
		Help: "Total number of jobs triggered (executions emitted).",
	})
	s.tickDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "easycron_scheduler_tick_duration_seconds",
		Help:    "Duration of each scheduler tick in seconds.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10},
	})
	s.tickDrift = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "easycron_scheduler_tick_drift_seconds",
		Help:    "Difference between actual tick time and expected interval in seconds.",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
	})

	s.register(reg, s.ticksTotal, "easycron_scheduler_ticks_total")
	s.register(reg, s.tickErrorsTotal, "easycron_scheduler_tick_errors_total")
	s.register(reg, s.jobsTriggeredTotal, "easycron_scheduler_jobs_triggered_total")
	s.register(reg, s.tickDuration, "easycron_scheduler_tick_duration_seconds")
	s.register(reg, s.tickDrift, "easycron_scheduler_tick_drift_seconds")
}

func (s *PrometheusSink) initDispatcherMetrics(reg prometheus.Registerer) {
	s.deliveryAttemptsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "easycron_dispatcher_delivery_attempts_total",
		Help: "Total number of webhook delivery attempts.",
	}, []string{"attempt", "status_class"})

	s.deliveryOutcomesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "easycron_dispatcher_delivery_outcomes_total",
		Help: "Total number of final delivery outcomes per execution.",
	}, []string{"outcome"})

	s.webhookDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "easycron_dispatcher_webhook_duration_seconds",
		Help:    "Webhook request latency in seconds (excludes backoff wait).",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	})

	s.retryAttemptsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "easycron_dispatcher_retry_attempts_total",
		Help: "Total number of retry attempts (excludes first attempt).",
	}, []string{"retryable"})

	s.eventsInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "easycron_dispatcher_events_in_flight",
		Help: "Number of events currently being processed.",
	})

	s.register(reg, s.deliveryAttemptsTotal, "easycron_dispatcher_delivery_attempts_total")
	s.register(reg, s.deliveryOutcomesTotal, "easycron_dispatcher_delivery_outcomes_total")
	s.register(reg, s.webhookDuration, "easycron_dispatcher_webhook_duration_seconds")
	s.register(reg, s.retryAttemptsTotal, "easycron_dispatcher_retry_attempts_total")
	s.register(reg, s.eventsInFlight, "easycron_dispatcher_events_in_flight")
}

func (s *PrometheusSink) initEventBusMetrics(reg prometheus.Registerer) {
	s.bufferSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "easycron_eventbus_buffer_size",
		Help: "Current number of events in the event bus buffer.",
	})
	s.emitErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "easycron_eventbus_emit_errors_total",
		Help: "Total number of emit errors (buffer full).",
	})

	s.register(reg, s.bufferSize, "easycron_eventbus_buffer_size")
	s.register(reg, s.emitErrorsTotal, "easycron_eventbus_emit_errors_total")
}

// register attempts to register a collector, logging any errors without propagating them.
func (s *PrometheusSink) register(reg prometheus.Registerer, c prometheus.Collector, name string) {
	if err := reg.Register(c); err != nil {
		log.Printf("metrics: failed to register %s: %v", name, err)
	}
}

// Scheduler metrics implementation

func (s *PrometheusSink) TickStarted() {
	s.ticksTotal.Inc()
}

func (s *PrometheusSink) TickCompleted(duration time.Duration, jobsTriggered int, err error) {
	s.tickDuration.Observe(duration.Seconds())
	s.jobsTriggeredTotal.Add(float64(jobsTriggered))
	if err != nil {
		s.tickErrorsTotal.Inc()
	}
}

func (s *PrometheusSink) TickDrift(drift time.Duration) {
	// Record absolute drift value
	d := drift.Seconds()
	if d < 0 {
		d = -d
	}
	s.tickDrift.Observe(d)
}

// Dispatcher metrics implementation

func (s *PrometheusSink) DeliveryAttemptCompleted(attempt int, statusClass string, duration time.Duration) {
	s.deliveryAttemptsTotal.WithLabelValues(strconv.Itoa(attempt), statusClass).Inc()
	s.webhookDuration.Observe(duration.Seconds())
}

func (s *PrometheusSink) DeliveryOutcome(outcome string) {
	s.deliveryOutcomesTotal.WithLabelValues(outcome).Inc()
}

func (s *PrometheusSink) RetryAttempt(retryable bool) {
	label := "false"
	if retryable {
		label = "true"
	}
	s.retryAttemptsTotal.WithLabelValues(label).Inc()
}

func (s *PrometheusSink) EventsInFlightIncr() {
	s.eventsInFlight.Inc()
}

func (s *PrometheusSink) EventsInFlightDecr() {
	s.eventsInFlight.Dec()
}

// EventBus metrics implementation

func (s *PrometheusSink) BufferSizeUpdate(size int) {
	s.bufferSize.Set(float64(size))
}

func (s *PrometheusSink) EmitError() {
	s.emitErrorsTotal.Inc()
}
