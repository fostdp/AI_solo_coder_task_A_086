package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"zhaozhou-bridge-monitor/database"
	"zhaozhou-bridge-monitor/models"
)

type ThresholdConfig struct {
	StrainLimitMicro     float64
	SettlementLimitMM    float64
	SettlementAbsoluteMM float64
	CrackWidthMM         float64
	CrackGrowthRate      float64
	StressLimitPa        float64
}

type AlertService struct {
	DB               *database.DB
	MQTTClient       mqtt.Client
	Thresholds       *ThresholdConfig
	AlertTopic       string
	HistoricalData   map[string][]models.SensorReading
	AlertCooldown    map[string]time.Time
	CooldownDuration time.Duration
}

func DefaultThresholds() *ThresholdConfig {
	return &ThresholdConfig{
		StrainLimitMicro:     500.0,
		SettlementLimitMM:    2.0,
		SettlementAbsoluteMM: 30.0,
		CrackWidthMM:         0.3,
		CrackGrowthRate:      0.1,
		StressLimitPa:        15e6,
	}
}

func NewAlertService(db *database.DB, mqttBroker string, thresholds *ThresholdConfig) (*AlertService, error) {
	if thresholds == nil {
		thresholds = DefaultThresholds()
	}

	clientID := fmt.Sprintf("zhaozhou-alert-service-%d", time.Now().UnixNano())
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s", mqttBroker))
	opts.SetClientID(clientID)
	opts.SetAutoReconnect(true)
	opts.SetCleanSession(true)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	return &AlertService{
		DB:               db,
		MQTTClient:       client,
		Thresholds:       thresholds,
		AlertTopic:       "zhaozhou/bridge/alerts",
		HistoricalData:   make(map[string][]models.SensorReading),
		AlertCooldown:    make(map[string]time.Time),
		CooldownDuration: time.Hour,
	}, nil
}

func (a *AlertService) Start(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.checkAllSensors(ctx)
		}
	}
}

func (a *AlertService) checkAllSensors(ctx context.Context) {
	readings, err := a.DB.QueryAllLatestSensorData(ctx)
	if err != nil {
		return
	}

	registry, err := a.DB.GetSensorRegistry(ctx)
	if err != nil {
		return
	}

	sensorTypes := make(map[string]string)
	for _, info := range registry {
		sensorTypes[info.SensorID] = info.SensorType
	}

	end := time.Now()
	start := end.Add(-7 * 24 * time.Hour)

	for _, reading := range readings {
		history, err := a.DB.QuerySensorData(ctx, reading.SensorID, start, end)
		if err == nil && len(history) > 0 {
			a.HistoricalData[reading.SensorID] = history
		} else {
			if existing, ok := a.HistoricalData[reading.SensorID]; ok {
				existing = append(existing, reading)
				if len(existing) > 100 {
					existing = existing[len(existing)-100:]
				}
				a.HistoricalData[reading.SensorID] = existing
			} else {
				a.HistoricalData[reading.SensorID] = []models.SensorReading{reading}
			}
		}

		sensorType := sensorTypes[reading.SensorID]
		switch sensorType {
		case "ARCH", "SARCH":
			a.checkStrainThresholds(ctx, reading)
		case "PIER":
			a.checkSettlementThresholds(ctx, reading)
		case "CRACK":
			a.checkCrackThresholds(ctx, reading)
		}
	}
}

func (a *AlertService) checkStrainThresholds(ctx context.Context, reading models.SensorReading) {
	warnThreshold := a.Thresholds.StrainLimitMicro * 0.8
	critThreshold := a.Thresholds.StrainLimitMicro

	if reading.StrainMicro > critThreshold {
		msg := fmt.Sprintf("Strain critical: %.2f microstrain exceeds limit %.2f", reading.StrainMicro, critThreshold)
		a.TriggerAlert(ctx, "strain_exceedance", "critical", msg, reading.SensorID, reading.StrainMicro, critThreshold)
	} else if reading.StrainMicro > warnThreshold {
		msg := fmt.Sprintf("Strain warning: %.2f microstrain approaching limit %.2f", reading.StrainMicro, critThreshold)
		a.TriggerAlert(ctx, "strain_exceedance", "warning", msg, reading.SensorID, reading.StrainMicro, warnThreshold)
	}
}

func (a *AlertService) checkSettlementThresholds(ctx context.Context, reading models.SensorReading) {
	absThreshold := a.Thresholds.SettlementAbsoluteMM
	absValue := math.Abs(reading.SettlementMM)

	if absValue > absThreshold {
		msg := fmt.Sprintf("Settlement absolute critical: %.2f mm exceeds limit %.2f mm", absValue, absThreshold)
		a.TriggerAlert(ctx, "settlement_absolute", "critical", msg, reading.SensorID, absValue, absThreshold)
	}

	data := a.HistoricalData[reading.SensorID]
	if len(data) < 3 {
		return
	}

	slope, _, r2 := a.linearRegression(data, func(r models.SensorReading) float64 {
		return r.SettlementMM
	})

	if r2 < 0.3 {
		return
	}

	ratePerMonth := slope * 30.44
	warnThreshold := a.Thresholds.SettlementLimitMM
	critThreshold := a.Thresholds.SettlementLimitMM * 2.5
	absRate := math.Abs(ratePerMonth)

	if absRate > critThreshold {
		msg := fmt.Sprintf("Settlement rate critical: %.4f mm/month exceeds limit %.2f mm/month", absRate, critThreshold)
		a.TriggerAlert(ctx, "settlement_rate", "critical", msg, reading.SensorID, absRate, critThreshold)
	} else if absRate > warnThreshold {
		msg := fmt.Sprintf("Settlement rate warning: %.4f mm/month exceeds limit %.2f mm/month", absRate, warnThreshold)
		a.TriggerAlert(ctx, "settlement_rate", "warning", msg, reading.SensorID, absRate, warnThreshold)
	}
}

func (a *AlertService) checkCrackThresholds(ctx context.Context, reading models.SensorReading) {
	warnWidth := a.Thresholds.CrackWidthMM
	critWidth := a.Thresholds.CrackWidthMM * 2.0

	if reading.CrackWidthMM > critWidth {
		msg := fmt.Sprintf("Crack width critical: %.4f mm exceeds limit %.4f mm", reading.CrackWidthMM, critWidth)
		a.TriggerAlert(ctx, "crack_width", "critical", msg, reading.SensorID, reading.CrackWidthMM, critWidth)
	} else if reading.CrackWidthMM > warnWidth {
		msg := fmt.Sprintf("Crack width warning: %.4f mm exceeds limit %.4f mm", reading.CrackWidthMM, warnWidth)
		a.TriggerAlert(ctx, "crack_width", "warning", msg, reading.SensorID, reading.CrackWidthMM, warnWidth)
	}

	data := a.HistoricalData[reading.SensorID]
	if len(data) < 3 {
		return
	}

	slope, _, r2 := a.linearRegression(data, func(r models.SensorReading) float64 {
		return r.CrackWidthMM
	})

	if r2 < 0.3 {
		return
	}

	growthRatePerMonth := slope * 30.44
	warnRate := a.Thresholds.CrackGrowthRate
	critRate := a.Thresholds.CrackGrowthRate * 2.0

	if growthRatePerMonth > critRate {
		msg := fmt.Sprintf("Crack growth acceleration critical: %.4f mm/month exceeds %.4f mm/month", growthRatePerMonth, critRate)
		a.TriggerAlert(ctx, "crack_growth_acceleration", "critical", msg, reading.SensorID, growthRatePerMonth, critRate)
	} else if growthRatePerMonth > warnRate {
		msg := fmt.Sprintf("Crack growth acceleration warning: %.4f mm/month exceeds %.4f mm/month", growthRatePerMonth, warnRate)
		a.TriggerAlert(ctx, "crack_growth_acceleration", "warning", msg, reading.SensorID, growthRatePerMonth, warnRate)
	}
}

func (a *AlertService) TriggerAlert(ctx context.Context, alertType, severity, message, sensorID string, value, threshold float64) (bool, error) {
	cooldownKey := fmt.Sprintf("%s|%s", sensorID, alertType)
	if lastTime, ok := a.AlertCooldown[cooldownKey]; ok {
		if time.Since(lastTime) < a.CooldownDuration {
			return false, nil
		}
	}

	alert := &models.Alert{
		Time:      time.Now(),
		AlertType: alertType,
		Severity:  severity,
		Message:   message,
		SensorID:  sensorID,
		Value:     value,
		Threshold: threshold,
	}

	if err := a.DB.InsertAlert(ctx, alert); err != nil {
		return false, err
	}

	payload := map[string]interface{}{
		"time":       alert.Time.Format(time.RFC3339),
		"alert_type": alert.AlertType,
		"severity":   alert.Severity,
		"message":    alert.Message,
		"sensor_id":  alert.SensorID,
		"value":      alert.Value,
		"threshold":  alert.Threshold,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}

	qos := byte(1)
	retained := severity == "critical"

	if token := a.MQTTClient.Publish(a.AlertTopic, qos, retained, jsonData); token.Wait() && token.Error() != nil {
		return false, token.Error()
	}

	a.AlertCooldown[cooldownKey] = time.Now()
	return true, nil
}

func (a *AlertService) linearRegression(data []models.SensorReading, extract func(models.SensorReading) float64) (slope, intercept float64, r2 float64) {
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

func (a *AlertService) CheckFEMStresses(ctx context.Context, results []models.FEMStressResult) ([]models.Alert, error) {
	limit := a.Thresholds.StressLimitPa
	warnLimit := limit * 0.8
	generated := make([]models.Alert, 0)

	for _, r := range results {
		if r.VonMises > limit {
			sensorID := fmt.Sprintf("FEM-ELEMENT-%d", r.ElementID)
			msg := fmt.Sprintf("FEM stress critical at element %d: %.2f Pa exceeds limit %.2f Pa", r.ElementID, r.VonMises, limit)
			published, _ := a.TriggerAlert(ctx, "stress_exceedance", "critical", msg, sensorID, r.VonMises, limit)
			if published {
				generated = append(generated, models.Alert{
					Time:      time.Now(),
					AlertType: "stress_exceedance",
					Severity:  "critical",
					SensorID:  sensorID,
					Value:     r.VonMises,
					Threshold: limit,
				})
			}
		} else if r.VonMises > warnLimit {
			sensorID := fmt.Sprintf("FEM-ELEMENT-%d", r.ElementID)
			msg := fmt.Sprintf("FEM stress warning at element %d: %.2f Pa approaching limit %.2f Pa", r.ElementID, r.VonMises, limit)
			published, _ := a.TriggerAlert(ctx, "stress_exceedance", "warning", msg, sensorID, r.VonMises, warnLimit)
			if published {
				generated = append(generated, models.Alert{
					Time:      time.Now(),
					AlertType: "stress_exceedance",
					Severity:  "warning",
					SensorID:  sensorID,
					Value:     r.VonMises,
					Threshold: warnLimit,
				})
			}
		}
	}

	return generated, nil
}

func (a *AlertService) Close() error {
	a.MQTTClient.Disconnect(100)
	return nil
}
