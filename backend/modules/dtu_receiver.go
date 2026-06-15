package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"zhaozhou-bridge-monitor/config"
	"zhaozhou-bridge-monitor/database"
	"zhaozhou-bridge-monitor/models"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type DTUReceiver struct {
	Config      *config.AppConfig
	DB          *database.DB
	Bus         *MessageBus
	upgrader    websocket.Upgrader
	wsClients   map[*websocket.Conn]bool
	wsClientsMu sync.Mutex
	lastReading map[string]time.Time
}

func NewDTUReceiver(cfg *config.AppConfig, db *database.DB, bus *MessageBus) *DTUReceiver {
	return &DTUReceiver{
		Config:      cfg,
		DB:          db,
		Bus:         bus,
		wsClients:   make(map[*websocket.Conn]bool),
		lastReading: make(map[string]time.Time),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (d *DTUReceiver) RegisterRoutes(r *mux.Router) {
	api := r.PathPrefix("/api").Subrouter()

	api.HandleFunc("/sensors", d.ListSensors).Methods("GET")
	api.HandleFunc("/sensors/{id}/latest", d.GetLatestSensorData).Methods("GET")
	api.HandleFunc("/sensors/{id}/history", d.GetSensorHistory).Methods("GET")
	api.HandleFunc("/sensors/{id}/hourly", d.GetHourlyAggregates).Methods("GET")
	api.HandleFunc("/sensors/all/latest", d.GetAllLatestData).Methods("GET")
	api.HandleFunc("/sensors/data", d.IngestSensorData).Methods("POST")

	r.HandleFunc("/ws/realtime", d.RealTimeDataWS)
}

func (d *DTUReceiver) validateReading(r *models.SensorReading) error {
	if r.SensorID == "" {
		SensorIngestedTotal.WithLabelValues(r.SensorID, "false").Inc()
		return fmt.Errorf("sensor_id is required")
	}

	vr := d.Config.DTUReceiver.ValidateRange

	if r.StrainMicro < vr.StrainMin || r.StrainMicro > vr.StrainMax {
		SensorIngestedTotal.WithLabelValues(r.SensorID, "false").Inc()
		return fmt.Errorf("strain %.4f out of range [%.2f, %.2f]", r.StrainMicro, vr.StrainMin, vr.StrainMax)
	}
	if r.SettlementMM < vr.SettlMin || r.SettlementMM > vr.SettlMax {
		SensorIngestedTotal.WithLabelValues(r.SensorID, "false").Inc()
		return fmt.Errorf("settlement %.4f out of range [%.2f, %.2f]", r.SettlementMM, vr.SettlMin, vr.SettlMax)
	}
	if r.Temperature < vr.TempMin || r.Temperature > vr.TempMax {
		SensorIngestedTotal.WithLabelValues(r.SensorID, "false").Inc()
		return fmt.Errorf("temperature %.2f out of range [%.2f, %.2f]", r.Temperature, vr.TempMin, vr.TempMax)
	}
	if r.CrackWidthMM < vr.CrackMin || r.CrackWidthMM > vr.CrackMax {
		SensorIngestedTotal.WithLabelValues(r.SensorID, "false").Inc()
		return fmt.Errorf("crack_width %.4f out of range [%.2f, %.2f]", r.CrackWidthMM, vr.CrackMin, vr.CrackMax)
	}

	if d.Config.DTUReceiver.CooldownMs > 0 {
		cooldown := time.Duration(d.Config.DTUReceiver.CooldownMs) * time.Millisecond
		if last, ok := d.lastReading[r.SensorID]; ok {
			if time.Since(last) < cooldown {
				SensorIngestedTotal.WithLabelValues(r.SensorID, "false").Inc()
				return fmt.Errorf("sensor %s cooldown active", r.SensorID)
			}
		}
		d.lastReading[r.SensorID] = time.Now()
	}

	SensorIngestedTotal.WithLabelValues(r.SensorID, "true").Inc()
	return nil
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

func (d *DTUReceiver) IngestSensorData(w http.ResponseWriter, r *http.Request) {
	var reading models.SensorReading
	if err := json.NewDecoder(r.Body).Decode(&reading); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if reading.Time.IsZero() {
		reading.Time = time.Now()
	}

	if err := d.validateReading(&reading); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	ctx := r.Context()
	if err := d.DB.InsertSensorReading(ctx, &reading); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	select {
	case d.Bus.SensorReadingCh <- reading:
	default:
	}

	d.broadcastToWS(&reading)

	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (d *DTUReceiver) broadcastToWS(reading *models.SensorReading) {
	d.wsClientsMu.Lock()
	defer d.wsClientsMu.Unlock()

	data, err := json.Marshal(reading)
	if err != nil {
		return
	}

	for conn := range d.wsClients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			conn.Close()
			delete(d.wsClients, conn)
		}
	}
}

func (d *DTUReceiver) Start(ctx context.Context) {
	go d.broadcastLatest(ctx)
	go d.fanOutSensorReadings(ctx)
}

func (d *DTUReceiver) fanOutSensorReadings(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.Bus.ShutdownCh:
			return
		case reading, ok := <-d.Bus.SensorReadingCh:
			if !ok {
				return
			}
			msg := BusMessage{
				Type:      "sensor_reading",
				Source:    "dtu_receiver",
				Timestamp: reading.Time,
				Payload:   reading,
			}
			select {
			case d.Bus.BroadCastCh <- msg:
			default:
			}
		}
	}
}

func (d *DTUReceiver) broadcastLatest(ctx context.Context) {
	interval := 10 * time.Second
	if d.Config != nil && d.Config.DTUReceiver.HistoryWindowDays > 0 {
		ms := 10000
		if d.Config.DTUReceiver.CooldownMs > 0 {
			ms = d.Config.DTUReceiver.CooldownMs * 10
		}
		if ms < 1000 {
			ms = 1000
		}
		interval = time.Duration(ms) * time.Millisecond
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.Bus.ShutdownCh:
			return
		case <-ticker.C:
			qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			data, err := d.DB.QueryAllLatestSensorData(qctx)
			cancel()
			if err != nil {
				log.Printf("WS query failed: %v", err)
				continue
			}

			d.wsClientsMu.Lock()
			for conn := range d.wsClients {
				if err := conn.WriteJSON(data); err != nil {
					conn.Close()
					delete(d.wsClients, conn)
				}
			}
			d.wsClientsMu.Unlock()
		}
	}
}

func (d *DTUReceiver) ListSensors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sensors, err := d.DB.GetSensorRegistry(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sensors)
}

func (d *DTUReceiver) GetLatestSensorData(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	ctx := r.Context()
	data, err := d.DB.QueryLatestSensorData(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (d *DTUReceiver) GetSensorHistory(w http.ResponseWriter, r *http.Request) {
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
	data, err := d.DB.QuerySensorData(ctx, id, start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (d *DTUReceiver) GetHourlyAggregates(w http.ResponseWriter, r *http.Request) {
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
	data, err := d.DB.QueryHourlyAggregates(ctx, id, start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (d *DTUReceiver) GetAllLatestData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data, err := d.DB.QueryAllLatestSensorData(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (d *DTUReceiver) RealTimeDataWS(w http.ResponseWriter, r *http.Request) {
	conn, err := d.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	d.wsClientsMu.Lock()
	d.wsClients[conn] = true
	d.wsClientsMu.Unlock()

	WsConnectedClients.Inc()

	defer func() {
		d.wsClientsMu.Lock()
		delete(d.wsClients, conn)
		d.wsClientsMu.Unlock()
		conn.Close()
		WsConnectedClients.Dec()
	}()

	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-r.Context().Done():
	case <-d.Bus.ShutdownCh:
	}
}
