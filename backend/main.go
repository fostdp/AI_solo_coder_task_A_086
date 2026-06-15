package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	_ "net/http/pprof"

	"github.com/gorilla/mux"

	"zhaozhou-bridge-monitor/config"
	"zhaozhou-bridge-monitor/database"
	"zhaozhou-bridge-monitor/modules"
)

func mainHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"name":   "Zhaozhou Bridge Monitor",
	})
}

func EnableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func writeJSONResp(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

func writeErrorResp(w http.ResponseWriter, status int, err error) {
	writeJSONResp(w, status, map[string]string{"error": err.Error()})
}

func makeFEMGetHandler(cfg *config.AppConfig, bus *modules.MessageBus, db *database.DB, fea *modules.FEASimulator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		startStr := q.Get("start")
		endStr := q.Get("end")
		ctx := r.Context()

		if startStr != "" && endStr != "" {
			start, err := time.Parse(time.RFC3339, startStr)
			if err != nil {
				writeErrorResp(w, http.StatusBadRequest, err)
				return
			}
			end, err := time.Parse(time.RFC3339, endStr)
			if err != nil {
				writeErrorResp(w, http.StatusBadRequest, err)
				return
			}

			results, err := db.QueryFEMResults(ctx, start, end)
			if err != nil {
				writeErrorResp(w, http.StatusInternalServerError, err)
				return
			}
			writeJSONResp(w, http.StatusOK, results)
			return
		}

		requestID := fmt.Sprintf("fem-req-%d", time.Now().UnixNano())
		req := modules.FEMRequestPayload{
			LiveLoadPa: cfg.FEMOptions.DefaultLiveLoadPa,
			DeltaTC:    cfg.FEMOptions.DefaultDeltaTC,
			RequestID:  requestID,
		}

		select {
		case bus.FEMRequestCh <- req:
		case <-time.After(5 * time.Second):
			writeErrorResp(w, http.StatusServiceUnavailable, fmt.Errorf("FEM service busy"))
			return
		}

		select {
		case result := <-bus.FEMResultCh:
			if result.Error != "" {
				writeErrorResp(w, http.StatusInternalServerError, fmt.Errorf("%s", result.Error))
				return
			}
			writeJSONResp(w, http.StatusOK, result.Stresses)
		case <-time.After(120 * time.Second):
			writeErrorResp(w, http.StatusGatewayTimeout, fmt.Errorf("FEM analysis timed out"))
		}
	}
}

func makeFEMAnalyzeHandler(cfg *config.AppConfig, bus *modules.MessageBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var params struct {
			LiveLoad float64 `json:"live_load"`
			DeltaT   float64 `json:"delta_t"`
		}
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
			writeErrorResp(w, http.StatusBadRequest, err)
			return
		}

		requestID := fmt.Sprintf("fem-req-%d", time.Now().UnixNano())
		req := modules.FEMRequestPayload{
			LiveLoadPa: params.LiveLoad,
			DeltaTC:    params.DeltaT,
			RequestID:  requestID,
		}

		select {
		case bus.FEMRequestCh <- req:
		case <-time.After(5 * time.Second):
			writeErrorResp(w, http.StatusServiceUnavailable, fmt.Errorf("FEM service busy"))
			return
		}

		select {
		case result := <-bus.FEMResultCh:
			if result.Error != "" {
				writeErrorResp(w, http.StatusInternalServerError, fmt.Errorf("%s", result.Error))
				return
			}
			writeJSONResp(w, http.StatusOK, result.Stresses)
		case <-time.After(120 * time.Second):
			writeErrorResp(w, http.StatusGatewayTimeout, fmt.Errorf("FEM analysis timed out"))
		}
	}
}

func makePredictHandler(db *database.DB, bus *modules.MessageBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		targetYearStr := q.Get("target_year")
		ctx := r.Context()

		if targetYearStr != "" {
			ty, err := strconv.Atoi(targetYearStr)
			if err != nil {
				writeErrorResp(w, http.StatusBadRequest, err)
				return
			}
			existing, err := db.QueryPredictions(ctx, ty)
			if err == nil && len(existing) > 0 {
				writeJSONResp(w, http.StatusOK, existing)
				return
			}
		}

		req := modules.PredictionRequestPayload{
			TargetYears: []int{1, 5, 10, 20, 30, 50},
			RefTime:     time.Now(),
		}

		select {
		case bus.PredictionReqCh <- req:
		case <-time.After(5 * time.Second):
			writeErrorResp(w, http.StatusServiceUnavailable, fmt.Errorf("Prediction service busy"))
			return
		}

		select {
		case result := <-bus.PredictionResCh:
			if result.Error != "" {
				writeErrorResp(w, http.StatusInternalServerError, fmt.Errorf("%s", result.Error))
				return
			}
			writeJSONResp(w, http.StatusOK, result.Predictions)
		case <-time.After(300 * time.Second):
			writeErrorResp(w, http.StatusGatewayTimeout, fmt.Errorf("Prediction timed out"))
		}
	}
}

func makePredict50Handler(bus *modules.MessageBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req := modules.PredictionRequestPayload{
			TargetYears: []int{1, 5, 10, 20, 30, 50},
			RefTime:     time.Now(),
		}

		select {
		case bus.PredictionReqCh <- req:
		case <-time.After(5 * time.Second):
			writeErrorResp(w, http.StatusServiceUnavailable, fmt.Errorf("Prediction service busy"))
			return
		}

		select {
		case result := <-bus.PredictionResCh:
			if result.Error != "" {
				writeErrorResp(w, http.StatusInternalServerError, fmt.Errorf("%s", result.Error))
				return
			}
			writeJSONResp(w, http.StatusOK, result.Predictions)
		case <-time.After(300 * time.Second):
			writeErrorResp(w, http.StatusGatewayTimeout, fmt.Errorf("Prediction timed out"))
		}
	}
}

func makeGetAlertsHandler(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		startStr := q.Get("start")
		endStr := q.Get("end")
		severity := q.Get("severity")

		var start, end time.Time
		var err error

		if startStr != "" {
			start, err = time.Parse(time.RFC3339, startStr)
			if err != nil {
				writeErrorResp(w, http.StatusBadRequest, err)
				return
			}
		} else {
			start = time.Now().Add(-7 * 24 * time.Hour)
		}

		if endStr != "" {
			end, err = time.Parse(time.RFC3339, endStr)
			if err != nil {
				writeErrorResp(w, http.StatusBadRequest, err)
				return
			}
		} else {
			end = time.Now()
		}

		ctx := r.Context()
		alerts, err := db.QueryAlerts(ctx, start, end, severity)
		if err != nil {
			writeErrorResp(w, http.StatusInternalServerError, err)
			return
		}
		writeJSONResp(w, http.StatusOK, alerts)
	}
}

func makeBridgeGeometryHandler(cfg *config.AppConfig, fea *modules.FEASimulator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodes, elements := fea.GetNodesElements()
		response := map[string]interface{}{
			"nodes":    nodes,
			"elements": elements,
			"geometry": cfg.ToBridgeGeometry(),
		}
		writeJSONResp(w, http.StatusOK, response)
	}
}

func main() {
	configPath := flag.String("config", "../config/bridge_config.json", "Path to config file")
	dbStr := flag.String("db", "postgres://bridge_admin:bridge2024@localhost:5432/zhaozhou_bridge?sslmode=disable", "DB connection string")
	mqttAddr := flag.String("mqtt", "", "MQTT broker host:port (overrides config)")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *mqttAddr != "" {
		cfg.AlarmMQTT.BrokerHostport = *mqttAddr
	}

	db, err := database.NewDB(*dbStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	bus := modules.NewMessageBus()
	defer bus.Close()

	dtuReceiver := modules.NewDTUReceiver(cfg, db, bus)
	feaSimulator := modules.NewFEASimulator(cfg, db, bus)
	creepPredictor := modules.NewCreepPredictor(cfg, db, bus, feaSimulator.FEMService)
	alarmMQTT := modules.NewAlarmMQTT(cfg, db, bus)

	modules.SetGoroutineFunc(func() int { return runtime.NumGoroutine() })

	r := mux.NewRouter()

	r.Use(EnableCORS)
	r.Use(modules.MetricsMiddleware)
	r.Use(modules.GzipMiddleware)
	r.Use(modules.CacheControlMiddleware)

	r.HandleFunc("/health", modules.HealthCheckHandler).Methods("GET")
	r.Handle("/metrics", modules.PrometheusHandler()).Methods("GET")
	r.PathPrefix("/debug/pprof/").Handler(http.DefaultServeMux)

	dtuReceiver.RegisterRoutes(r)

	api := r.PathPrefix("/api").Subrouter()
	api.HandleFunc("/health", mainHandler).Methods("GET")
	api.HandleFunc("/fem/stress", makeFEMGetHandler(cfg, bus, db, feaSimulator)).Methods("GET")
	api.HandleFunc("/fem/analyze", makeFEMAnalyzeHandler(cfg, bus)).Methods("POST")
	api.HandleFunc("/deformation/predict", makePredictHandler(db, bus)).Methods("GET")
	api.HandleFunc("/deformation/predict50", makePredict50Handler(bus)).Methods("POST")
	api.HandleFunc("/alerts", makeGetAlertsHandler(db)).Methods("GET")
	api.HandleFunc("/bridge/geometry", makeBridgeGeometryHandler(cfg, feaSimulator)).Methods("GET")

	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./frontend")))

	ctx, cancel := context.WithCancel(context.Background())

	go dtuReceiver.Start(ctx)
	go feaSimulator.Run(ctx)
	go creepPredictor.Run(ctx)
	go creepPredictor.ScheduledDaily(ctx)
	go alarmMQTT.Run(ctx)

	listenAddr := cfg.DTUReceiver.ListenAddr
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	srv := &http.Server{
		Addr:         listenAddr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 300 * time.Second,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		log.Printf("Starting HTTP server on %s", listenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-sigCh
	log.Println("Shutting down...")

	cancel()

	alarmMQTT.Close()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}

	log.Println("Server exited cleanly")
}
