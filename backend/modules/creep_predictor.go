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

type CreepPredictor struct {
	Config     *config.AppConfig
	DB         *database.DB
	Bus        *MessageBus
	Predictor  *services.DeformationPredictor
	FEMService *services.FEMService
}

func NewCreepPredictor(cfg *config.AppConfig, db *database.DB, bus *MessageBus, fem *services.FEMService) *CreepPredictor {
	predictor := services.NewDeformationPredictor(fem)

	predictor.CreepPhiInf = cfg.CreepModel.PhiInfNewStone
	predictor.CreepBeta = cfg.CreepModel.BetaShapeExponent
	predictor.HumidityPercent = cfg.CreepModel.HumidityPercent
	predictor.StoneEffectiveThickness = cfg.StoneMaterial.EffectiveThickness
	predictor.CreepLoadingAgeDays = cfg.CreepModel.ReferenceLoadingAgeDays
	predictor.AnnualTempCycleAmplitude = cfg.CreepModel.TemperatureAmplitudeC
	predictor.AnnualTempMean = cfg.CreepModel.TemperatureMeanC
	predictor.ConstructionYear = cfg.Bridge.ConstructionYearCE

	currentYear := time.Now().Year()
	if currentYear <= cfg.Bridge.ConstructionYearCE {
		currentYear = 2026
	}
	predictor.StoneAgeDays = float64(currentYear-cfg.Bridge.ConstructionYearCE) * 365.25
	predictor.AgeCorrectionFactor = predictor.GetAgeCorrectionReport()["age_correction_factor"]

	return &CreepPredictor{
		Config:     cfg,
		DB:         db,
		Bus:        bus,
		Predictor:  predictor,
		FEMService: fem,
	}
}

func (p *CreepPredictor) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.Bus.ShutdownCh:
			return
		case req, ok := <-p.Bus.PredictionReqCh:
			if !ok {
				return
			}
			p.processPrediction(ctx, req)
		}
	}
}

func (p *CreepPredictor) processPrediction(ctx context.Context, req PredictionRequestPayload) {
	start := time.Now()

	refTime := req.RefTime
	if refTime.IsZero() {
		refTime = time.Now()
	}

	predictions, err := p.Predictor.Predict50YearDeformation(refTime)
	DeformationPredictionTotal.Inc()
	computeMs := time.Since(start).Milliseconds()

	result := PredictionResultPayload{
		ComputeMs: computeMs,
		AgeReport: p.Predictor.GetAgeCorrectionReport(),
	}

	if err != nil {
		result.Error = err.Error()
		log.Printf("Prediction failed: %v", err)
	} else {
		result.Predictions = predictions

		dbCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		for i := range predictions {
			if err := p.DB.InsertPrediction(dbCtx, &predictions[i]); err != nil {
				log.Printf("Failed to insert prediction %d: %v", i, err)
			}
		}
		cancel()
	}

	select {
	case p.Bus.PredictionResCh <- result:
	case <-ctx.Done():
	case <-p.Bus.ShutdownCh:
	}
}

func (p *CreepPredictor) ScheduledDaily(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.Bus.ShutdownCh:
			return
		case <-ticker.C:
			req := PredictionRequestPayload{
				TargetYears: []int{1, 5, 10, 20, 30, 50},
				RefTime:     time.Now(),
			}
			select {
			case p.Bus.PredictionReqCh <- req:
			case <-ctx.Done():
			case <-p.Bus.ShutdownCh:
			}
		}
	}
}
