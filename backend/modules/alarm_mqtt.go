package modules

import (
	"context"
	"log"
	"math"
	"time"

	"zhaozhou-bridge-monitor/config"
	"zhaozhou-bridge-monitor/database"
	"zhaozhou-bridge-monitor/models"
	"zhaozhou-bridge-monitor/services"
)

type AlarmMQTT struct {
	Config     *config.AppConfig
	DB         *database.DB
	Bus        *MessageBus
	AlertSvc   *services.AlertService
	Thresholds *services.ThresholdConfig
}

func NewAlarmMQTT(cfg *config.AppConfig, db *database.DB, bus *MessageBus) *AlarmMQTT {
	thresholds := cfg.ToThresholdConfig()

	broker := cfg.AlarmMQTT.BrokerHostport

	alertSvc, err := services.NewAlertService(db, broker, thresholds)
	if err != nil {
		log.Printf("Warning: Failed to initialize MQTT alert service: %v", err)
		return &AlarmMQTT{
			Config:     cfg,
			DB:         db,
			Bus:        bus,
			AlertSvc:   nil,
			Thresholds: thresholds,
		}
	}

	alertSvc.AlertTopic = cfg.AlarmMQTT.TopicAlerts
	alertSvc.CooldownDuration = time.Duration(cfg.AlarmMQTT.CooldownMinutes) * time.Minute

	return &AlarmMQTT{
		Config:     cfg,
		DB:         db,
		Bus:        bus,
		AlertSvc:   alertSvc,
		Thresholds: thresholds,
	}
}

func (a *AlarmMQTT) Run(ctx context.Context) {
	if a.AlertSvc != nil {
		go a.AlertSvc.Start(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.Bus.ShutdownCh:
			return
		case payload, ok := <-a.Bus.AlertEvalCh:
			if !ok {
				return
			}
			a.processAlertEval(ctx, payload)
		}
	}
}

func (a *AlarmMQTT) processAlertEval(ctx context.Context, payload AlertEvaluatePayload) {
	result := AlertResultPayload{}

	if a.AlertSvc == nil {
		select {
		case a.Bus.AlertResultCh <- result:
		default:
		}
		return
	}

	generated := make([]models.Alert, 0)

	if len(payload.FEMStresses) > 0 {
		femAlerts, err := a.AlertSvc.CheckFEMStresses(ctx, payload.FEMStresses)
		if err != nil {
			log.Printf("FEM stress alert check failed: %v", err)
		}
		for _, alert := range femAlerts {
			AlertsGeneratedTotal.WithLabelValues(alert.Severity, alert.AlertType).Inc()
			AlertsMqttPublished.Inc()
		}
	}

	if len(payload.AllLatestReadings) > 0 {
		registry, err := a.DB.GetSensorRegistry(ctx)
		sensorTypes := make(map[string]string)
		if err == nil {
			for _, info := range registry {
				sensorTypes[info.SensorID] = info.SensorType
			}
		}

		for _, reading := range payload.AllLatestReadings {
			sensorType := sensorTypes[reading.SensorID]
			switch sensorType {
			case "ARCH", "SARCH":
				a.checkStrainManual(ctx, reading, &generated)
			case "PIER":
				a.checkSettlementManual(ctx, reading, payload.HistoryBySensor[reading.SensorID], &generated)
			case "CRACK":
				a.checkCrackManual(ctx, reading, payload.HistoryBySensor[reading.SensorID], &generated)
			}
		}
	}

	result.Generated = generated
	result.MQTTPublished = len(generated)

	select {
	case a.Bus.AlertResultCh <- result:
	default:
	}
}

func (a *AlarmMQTT) checkStrainManual(ctx context.Context, reading models.SensorReading, generated *[]models.Alert) {
	warnThreshold := a.Config.Thresholds.StrainWarn
	critThreshold := a.Config.Thresholds.StrainCrit

	if reading.StrainMicro > critThreshold {
		alert := models.Alert{
			Time:      time.Now(),
			AlertType: "strain_exceedance",
			Severity:  "critical",
			SensorID:  reading.SensorID,
			Value:     reading.StrainMicro,
			Threshold: critThreshold,
		}
		*generated = append(*generated, alert)
		AlertsGeneratedTotal.WithLabelValues(alert.Severity, alert.AlertType).Inc()
	} else if reading.StrainMicro > warnThreshold {
		alert := models.Alert{
			Time:      time.Now(),
			AlertType: "strain_exceedance",
			Severity:  "warning",
			SensorID:  reading.SensorID,
			Value:     reading.StrainMicro,
			Threshold: warnThreshold,
		}
		*generated = append(*generated, alert)
		AlertsGeneratedTotal.WithLabelValues(alert.Severity, alert.AlertType).Inc()
	}
}

func (a *AlarmMQTT) checkSettlementManual(ctx context.Context, reading models.SensorReading, history []models.SensorReading, generated *[]models.Alert) {
	absThreshold := a.Config.Thresholds.SettlAbsCrit
	absValue := math.Abs(reading.SettlementMM)

	if absValue > absThreshold {
		alert := models.Alert{
			Time:      time.Now(),
			AlertType: "settlement_absolute",
			Severity:  "critical",
			SensorID:  reading.SensorID,
			Value:     absValue,
			Threshold: absThreshold,
		}
		*generated = append(*generated, alert)
		AlertsGeneratedTotal.WithLabelValues(alert.Severity, alert.AlertType).Inc()
	}

	if len(history) < 3 {
		return
	}

	minPoints := a.Config.AlarmMQTT.MinTrendPoints
	if len(history) < minPoints {
		return
	}

	slope, _, r2 := linearRegression(history, func(r models.SensorReading) float64 {
		return r.SettlementMM
	})

	minR2 := a.Config.AlarmMQTT.MinR2ForTrend
	if r2 < minR2 {
		return
	}

	ratePerMonth := slope * 30.44
	warnThreshold := a.Config.Thresholds.SettlRateWarn
	critThreshold := a.Config.Thresholds.SettlRateCrit
	absRate := math.Abs(ratePerMonth)

	if absRate > critThreshold {
		alert := models.Alert{
			Time:      time.Now(),
			AlertType: "settlement_rate",
			Severity:  "critical",
			SensorID:  reading.SensorID,
			Value:     absRate,
			Threshold: critThreshold,
		}
		*generated = append(*generated, alert)
		AlertsGeneratedTotal.WithLabelValues(alert.Severity, alert.AlertType).Inc()
	} else if absRate > warnThreshold {
		alert := models.Alert{
			Time:      time.Now(),
			AlertType: "settlement_rate",
			Severity:  "warning",
			SensorID:  reading.SensorID,
			Value:     absRate,
			Threshold: warnThreshold,
		}
		*generated = append(*generated, alert)
		AlertsGeneratedTotal.WithLabelValues(alert.Severity, alert.AlertType).Inc()
	}
}

func (a *AlarmMQTT) checkCrackManual(ctx context.Context, reading models.SensorReading, history []models.SensorReading, generated *[]models.Alert) {
	warnWidth := a.Config.Thresholds.CrackWidthWarn
	critWidth := a.Config.Thresholds.CrackWidthCrit

	if reading.CrackWidthMM > critWidth {
		alert := models.Alert{
			Time:      time.Now(),
			AlertType: "crack_width",
			Severity:  "critical",
			SensorID:  reading.SensorID,
			Value:     reading.CrackWidthMM,
			Threshold: critWidth,
		}
		*generated = append(*generated, alert)
		AlertsGeneratedTotal.WithLabelValues(alert.Severity, alert.AlertType).Inc()
	} else if reading.CrackWidthMM > warnWidth {
		alert := models.Alert{
			Time:      time.Now(),
			AlertType: "crack_width",
			Severity:  "warning",
			SensorID:  reading.SensorID,
			Value:     reading.CrackWidthMM,
			Threshold: warnWidth,
		}
		*generated = append(*generated, alert)
		AlertsGeneratedTotal.WithLabelValues(alert.Severity, alert.AlertType).Inc()
	}

	if len(history) < 3 {
		return
	}

	minPoints := a.Config.AlarmMQTT.MinTrendPoints
	if len(history) < minPoints {
		return
	}

	slope, _, r2 := linearRegression(history, func(r models.SensorReading) float64 {
		return r.CrackWidthMM
	})

	minR2 := a.Config.AlarmMQTT.MinR2ForTrend
	if r2 < minR2 {
		return
	}

	growthRatePerMonth := slope * 30.44
	warnRate := a.Config.Thresholds.CrackGrowthWarn
	critRate := a.Config.Thresholds.CrackGrowthCrit

	if growthRatePerMonth > critRate {
		alert := models.Alert{
			Time:      time.Now(),
			AlertType: "crack_growth_acceleration",
			Severity:  "critical",
			SensorID:  reading.SensorID,
			Value:     growthRatePerMonth,
			Threshold: critRate,
		}
		*generated = append(*generated, alert)
		AlertsGeneratedTotal.WithLabelValues(alert.Severity, alert.AlertType).Inc()
	} else if growthRatePerMonth > warnRate {
		alert := models.Alert{
			Time:      time.Now(),
			AlertType: "crack_growth_acceleration",
			Severity:  "warning",
			SensorID:  reading.SensorID,
			Value:     growthRatePerMonth,
			Threshold: warnRate,
		}
		*generated = append(*generated, alert)
		AlertsGeneratedTotal.WithLabelValues(alert.Severity, alert.AlertType).Inc()
	}
}

func linearRegression(data []models.SensorReading, extract func(models.SensorReading) float64) (slope, intercept float64, r2 float64) {
	n := len(data)
	if n < 2 {
		return 0, 0, 0
	}

	xVals := make([]float64, n)
	yVals := make([]float64, n)
	baseTime := data[0].Time

	for i, r := range data {
		xVals[i] = r.Time.Sub(baseTime).Hours() / 24.0
		yVals[i] = extract(r)
	}

	var sumX, sumY, sumXY, sumXX float64
	for i := 0; i < n; i++ {
		sumX += xVals[i]
		sumY += yVals[i]
		sumXY += xVals[i] * yVals[i]
		sumXX += xVals[i] * xVals[i]
	}

	meanX := sumX / float64(n)
	meanY := sumY / float64(n)

	denominator := sumXX - float64(n)*meanX*meanX
	if math.Abs(denominator) < 1e-20 {
		return 0, meanY, 0
	}

	slope = (sumXY - float64(n)*meanX*meanY) / denominator
	intercept = meanY - slope*meanX

	var ssRes, ssTot float64
	for i := 0; i < n; i++ {
		predicted := slope*xVals[i] + intercept
		ssRes += (yVals[i] - predicted) * (yVals[i] - predicted)
		ssTot += (yVals[i] - meanY) * (yVals[i] - meanY)
	}

	if ssTot < 1e-20 {
		r2 = 1.0
	} else {
		r2 = 1.0 - ssRes/ssTot
	}

	return slope, intercept, r2
}

func (a *AlarmMQTT) Close() {
	if a.AlertSvc != nil {
		a.AlertSvc.Close()
	}
}
