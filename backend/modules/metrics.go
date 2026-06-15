package modules

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	HttpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "zhaozhou_http_requests_total",
		Help: "Total number of HTTP requests processed",
	}, []string{"method", "path", "status_code"})

	HttpResponseTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "zhaozhou_http_response_time_seconds",
		Help:    "HTTP request response time distribution",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	HttpInFlightRequests = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "zhaozhou_http_in_flight_requests",
		Help: "Current number of in-flight HTTP requests",
	})

	SensorIngestedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "zhaozhou_sensor_ingested_total",
		Help: "Total sensor readings ingested",
	}, []string{"sensor_id", "valid"})

	FEMComputeTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "zhaozhou_fem_compute_total",
		Help: "Total FEM computations performed",
	})

	FEMComputeDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "zhaozhou_fem_compute_duration_seconds",
		Help:    "FEM computation duration distribution",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 8),
	})

	DeformationPredictionTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "zhaozhou_deformation_prediction_total",
		Help: "Total deformation predictions performed",
	})

	AlertsGeneratedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "zhaozhou_alerts_generated_total",
		Help: "Total alerts generated",
	}, []string{"severity", "alert_type"})

	AlertsMqttPublished = promauto.NewCounter(prometheus.CounterOpts{
		Name: "zhaozhou_alerts_mqtt_published_total",
		Help: "Total alerts published to MQTT",
	})

	WsConnectedClients = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "zhaozhou_ws_connected_clients",
		Help: "Current number of connected WebSocket clients",
	})

	GoGoroutines = promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "zhaozhou_go_goroutines",
		Help: "Current number of goroutines",
	}, func() float64 {
		return float64(numGoroutineFunc())
	})
)

var numGoroutineFunc func() int

func SetGoroutineFunc(f func() int) {
	numGoroutineFunc = f
}

func PrometheusHandler() http.Handler {
	return promhttp.Handler()
}

func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/metrics") ||
			strings.HasPrefix(r.URL.Path, "/debug/pprof") {
			next.ServeHTTP(w, r)
			return
		}

		HttpInFlightRequests.Inc()
		defer HttpInFlightRequests.Dec()

		start := time.Now()
		mw := &metricsResponseWriter{w, http.StatusOK}
		next.ServeHTTP(mw, r)

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(mw.statusCode)

		HttpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, status).Inc()
		HttpResponseTime.WithLabelValues(r.Method, r.URL.Path).Observe(duration)
	})
}

type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (m *metricsResponseWriter) WriteHeader(code int) {
	m.statusCode = code
	m.ResponseWriter.WriteHeader(code)
}
