package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"zhaozhou-bridge-monitor/database"
	"zhaozhou-bridge-monitor/models"
	"zhaozhou-bridge-monitor/services"
)

type Handler struct {
	DB           *database.DB
	FEMService   *services.FEMService
	Predictor    *services.DeformationPredictor
	AlertSvc     *services.AlertService
	upgrader     websocket.Upgrader
}

func NewHandler(db *database.DB, fem *services.FEMService, pred *services.DeformationPredictor, alert *services.AlertService) *Handler {
	return &Handler{
		DB:         db,
		FEMService: fem,
		Predictor:  pred,
		AlertSvc:   alert,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (h *Handler) SetupRoutes(r *mux.Router) {
	r.Use(h.EnableCORS)

	api := r.PathPrefix("/api").Subrouter()

	api.HandleFunc("/sensors", h.ListSensors).Methods("GET")
	api.HandleFunc("/sensors/{id}/latest", h.GetLatestSensorData).Methods("GET")
	api.HandleFunc("/sensors/{id}/history", h.GetSensorHistory).Methods("GET")
	api.HandleFunc("/sensors/{id}/hourly", h.GetHourlyAggregates).Methods("GET")
	api.HandleFunc("/sensors/all/latest", h.GetAllLatestData).Methods("GET")
	api.HandleFunc("/sensors/data", h.IngestSensorData).Methods("POST")

	api.HandleFunc("/fem/stress", h.GetFEMStressResults).Methods("GET")
	api.HandleFunc("/fem/analyze", h.RunFEMAnalysis).Methods("POST")

	api.HandleFunc("/deformation/predict", h.GetDeformationPredictions).Methods("GET")
	api.HandleFunc("/deformation/predict50", h.Predict50Years).Methods("POST")

	api.HandleFunc("/alerts", h.GetAlerts).Methods("GET")

	api.HandleFunc("/bridge/geometry", h.GetBridgeGeometry).Methods("GET")

	r.HandleFunc("/ws/realtime", h.RealTimeDataWS)

	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./frontend")))
}

func (h *Handler) EnableCORS(next http.Handler) http.Handler {
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

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (h *Handler) ListSensors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sensors, err := h.DB.GetSensorRegistry(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sensors)
}

func (h *Handler) GetLatestSensorData(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	ctx := r.Context()
	data, err := h.DB.QueryLatestSensorData(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (h *Handler) GetSensorHistory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	q := r.URL.Query()
	startStr := q.Get("start")
	endStr := q.Get("end")

	var start, end time.Time
	var err error

	if startStr != "" {
		start, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	} else {
		start = time.Now().Add(-24 * time.Hour)
	}

	if endStr != "" {
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	} else {
		end = time.Now()
	}

	ctx := r.Context()
	data, err := h.DB.QuerySensorData(ctx, id, start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (h *Handler) GetHourlyAggregates(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	q := r.URL.Query()
	startStr := q.Get("start")
	endStr := q.Get("end")

	var start, end time.Time
	var err error

	if startStr != "" {
		start, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	} else {
		start = time.Now().Add(-24 * time.Hour)
	}

	if endStr != "" {
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	} else {
		end = time.Now()
	}

	ctx := r.Context()
	data, err := h.DB.QueryHourlyAggregates(ctx, id, start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (h *Handler) GetAllLatestData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data, err := h.DB.QueryAllLatestSensorData(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (h *Handler) IngestSensorData(w http.ResponseWriter, r *http.Request) {
	var reading models.SensorReading
	if err := json.NewDecoder(r.Body).Decode(&reading); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if reading.Time.IsZero() {
		reading.Time = time.Now()
	}

	ctx := r.Context()
	if err := h.DB.InsertSensorReading(ctx, &reading); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *Handler) GetFEMStressResults(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	startStr := q.Get("start")
	endStr := q.Get("end")
	ctx := r.Context()

	if startStr != "" && endStr != "" {
		start, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		end, err := time.Parse(time.RFC3339, endStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		results, err := h.DB.QueryFEMResults(ctx, start, end)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, results)
		return
	}

	results, err := h.FEMService.RunFullAnalysis(10000, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	for i := range results {
		if err := h.DB.InsertFEMResult(ctx, &results[i]); err != nil {
			log.Printf("Failed to insert FEM result: %v", err)
		}
	}

	if h.AlertSvc != nil {
		h.AlertSvc.CheckFEMStresses(ctx, results)
	}

	writeJSON(w, http.StatusOK, results)
}

func (h *Handler) RunFEMAnalysis(w http.ResponseWriter, r *http.Request) {
	var params struct {
		LiveLoad float64 `json:"live_load"`
		DeltaT   float64 `json:"delta_t"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	results, err := h.FEMService.RunFullAnalysis(params.LiveLoad, params.DeltaT)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	ctx := r.Context()
	for i := range results {
		if err := h.DB.InsertFEMResult(ctx, &results[i]); err != nil {
			log.Printf("Failed to insert FEM result: %v", err)
		}
	}

	if h.AlertSvc != nil {
		h.AlertSvc.CheckFEMStresses(ctx, results)
	}

	writeJSON(w, http.StatusOK, results)
}

func (h *Handler) GetDeformationPredictions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	targetYearStr := q.Get("target_year")
	targetYear := 50
	if targetYearStr != "" {
		ty, err := strconv.Atoi(targetYearStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		targetYear = ty
	}

	ctx := r.Context()
	existing, err := h.DB.QueryPredictions(ctx, targetYear)
	if err == nil && len(existing) > 0 {
		writeJSON(w, http.StatusOK, existing)
		return
	}

	allPredictions, err := h.Predictor.Predict50YearDeformation(time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	for i := range allPredictions {
		if err := h.DB.InsertPrediction(ctx, &allPredictions[i]); err != nil {
			log.Printf("Failed to insert prediction: %v", err)
		}
	}

	writeJSON(w, http.StatusOK, allPredictions)
}

func (h *Handler) Predict50Years(w http.ResponseWriter, r *http.Request) {
	allPredictions, err := h.Predictor.Predict50YearDeformation(time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	ctx := r.Context()
	for i := range allPredictions {
		if err := h.DB.InsertPrediction(ctx, &allPredictions[i]); err != nil {
			log.Printf("Failed to insert prediction: %v", err)
		}
	}

	writeJSON(w, http.StatusOK, allPredictions)
}

func (h *Handler) GetAlerts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	startStr := q.Get("start")
	endStr := q.Get("end")
	severity := q.Get("severity")

	var start, end time.Time
	var err error

	if startStr != "" {
		start, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	} else {
		start = time.Now().Add(-7 * 24 * time.Hour)
	}

	if endStr != "" {
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	} else {
		end = time.Now()
	}

	ctx := r.Context()
	alerts, err := h.DB.QueryAlerts(ctx, start, end, severity)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, alerts)
}

func (h *Handler) GetBridgeGeometry(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"nodes":    h.FEMService.Nodes,
		"elements": h.FEMService.Elements,
		"geometry": h.FEMService.Geometry,
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) RealTimeDataWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		data, err := h.DB.QueryAllLatestSensorData(ctx)
		cancel()
		if err != nil {
			log.Printf("WS query failed: %v", err)
			continue
		}

		if err := conn.WriteJSON(data); err != nil {
			log.Printf("WS write failed: %v", err)
			return
		}
	}
}
