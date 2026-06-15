package modules

import (
	"context"
	"log"
	"time"

	"zhaozhou-bridge-monitor/config"
	"zhaozhou-bridge-monitor/database"
	"zhaozhou-bridge-monitor/models"
	"zhaozhou-bridge-monitor/services"
)

type FEASimulator struct {
	Config     *config.AppConfig
	DB         *database.DB
	Bus        *MessageBus
	FEMService *services.FEMService
	hasRunOnce bool
}

func NewFEASimulator(cfg *config.AppConfig, db *database.DB, bus *MessageBus) *FEASimulator {
	geom := cfg.ToBridgeGeometry()
	mat := cfg.ToMasonryMaterial()
	fem := services.NewFEMService(geom, mat)
	fem.UseSubmodeling = cfg.FEMOptions.UseSubmodeling
	return &FEASimulator{
		Config:     cfg,
		DB:         db,
		Bus:        bus,
		FEMService: fem,
		hasRunOnce: false,
	}
}

func (f *FEASimulator) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-f.Bus.ShutdownCh:
			return
		case req, ok := <-f.Bus.FEMRequestCh:
			if !ok {
				return
			}
			f.processRequest(ctx, req)
		}
	}
}

func (f *FEASimulator) processRequest(ctx context.Context, req FEMRequestPayload) {
	FEMComputeTotal.Inc()
	start := time.Now()

	stresses, err := f.FEMService.RunFullAnalysis(req.LiveLoadPa, req.DeltaTC)
	f.hasRunOnce = true

	computeMs := time.Since(start).Milliseconds()
	FEMComputeDuration.Observe(time.Since(start).Seconds())

	result := FEMResultPayload{
		RequestID: req.RequestID,
		ComputeMs: computeMs,
	}

	if err != nil {
		result.Error = err.Error()
		log.Printf("FEM analysis failed: %v", err)
	} else {
		result.Stresses = stresses
		result.Nodes = make([]models.FEMNode, len(f.FEMService.Nodes))
		copy(result.Nodes, f.FEMService.Nodes)
		result.Elements = make([]models.FEMElement, len(f.FEMService.Elements))
		copy(result.Elements, f.FEMService.Elements)

		dbCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		for i := range stresses {
			if err := f.DB.InsertFEMResult(dbCtx, &stresses[i]); err != nil {
				log.Printf("Failed to insert FEM result %d: %v", i, err)
			}
		}
		cancel()

		alertPayload := AlertEvaluatePayload{
			FEMStresses: stresses,
		}
		select {
		case f.Bus.AlertEvalCh <- alertPayload:
		default:
		}
	}

	select {
	case f.Bus.FEMResultCh <- result:
	case <-ctx.Done():
	case <-f.Bus.ShutdownCh:
	}
}

func (f *FEASimulator) GetNodesElements() ([]models.FEMNode, []models.FEMElement) {
	if !f.hasRunOnce {
		_, err := f.FEMService.RunFullAnalysis(
			f.Config.FEMOptions.DefaultLiveLoadPa,
			f.Config.FEMOptions.DefaultDeltaTC,
		)
		if err != nil {
			log.Printf("FEM initial run failed: %v", err)
		}
		f.hasRunOnce = true
	}
	nodes := make([]models.FEMNode, len(f.FEMService.Nodes))
	copy(nodes, f.FEMService.Nodes)
	elements := make([]models.FEMElement, len(f.FEMService.Elements))
	copy(elements, f.FEMService.Elements)
	return nodes, elements
}
