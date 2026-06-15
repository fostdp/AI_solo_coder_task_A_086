package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"

	"zhaozhou-bridge-monitor/database"
	"zhaozhou-bridge-monitor/handlers"
	"zhaozhou-bridge-monitor/services"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP server address")
	dbStr := flag.String("db", "postgres://bridge_admin:bridge2024@localhost:5432/zhaozhou_bridge?sslmode=disable", "DB connection string")
	mqttAddr := flag.String("mqtt", "localhost:1883", "MQTT broker host:port")
	flag.Parse()

	db, err := database.NewDB(*dbStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	fem := services.NewFEMService(nil, nil)
	predictor := services.NewDeformationPredictor(fem)

	alertSvc, err := services.NewAlertService(db, *mqttAddr, services.DefaultThresholds())
	if err != nil {
		log.Printf("Warning: Failed to initialize MQTT alert service: %v", err)
		alertSvc = nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	if alertSvc != nil {
		go alertSvc.Start(ctx)
	}

	h := handlers.NewHandler(db, fem, predictor, alertSvc)
	r := mux.NewRouter()
	h.SetupRoutes(r)

	srv := &http.Server{
		Addr:         *addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		log.Printf("Starting HTTP server on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-sigCh
	log.Println("Shutting down...")

	cancel()

	if alertSvc != nil {
		alertSvc.Close()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}

	log.Println("Server exited cleanly")
}
