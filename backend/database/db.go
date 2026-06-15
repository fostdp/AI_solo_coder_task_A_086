package database

import (
	"context"
	"database/sql"
	"time"

	"zhaozhou-bridge-monitor/models"

	"github.com/lib/pq"
	_ "github.com/lib/pq"
)

type DB struct {
	db *sql.DB
}

func NewDB(connStr string) (*DB, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db: db}, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) InsertSensorReading(ctx context.Context, r *models.SensorReading) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO sensor_data (time, sensor_id, strain_micro, settlement_mm, temperature, crack_width_mm)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		r.Time, r.SensorID, r.StrainMicro, r.SettlementMM, r.Temperature, r.CrackWidthMM,
	)
	return err
}

func (d *DB) QuerySensorData(ctx context.Context, sensorID string, start, end time.Time) ([]models.SensorReading, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT time, sensor_id, strain_micro, settlement_mm, temperature, crack_width_mm
		 FROM sensor_data
		 WHERE sensor_id = $1 AND time >= $2 AND time <= $3
		 ORDER BY time ASC`,
		sensorID, start, end,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.SensorReading
	for rows.Next() {
		var r models.SensorReading
		if err := rows.Scan(&r.Time, &r.SensorID, &r.StrainMicro, &r.SettlementMM, &r.Temperature, &r.CrackWidthMM); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (d *DB) QueryLatestSensorData(ctx context.Context, sensorID string) (*models.SensorReading, error) {
	var r models.SensorReading
	err := d.db.QueryRowContext(ctx,
		`SELECT time, sensor_id, strain_micro, settlement_mm, temperature, crack_width_mm
		 FROM sensor_data
		 WHERE sensor_id = $1
		 ORDER BY time DESC
		 LIMIT 1`,
		sensorID,
	).Scan(&r.Time, &r.SensorID, &r.StrainMicro, &r.SettlementMM, &r.Temperature, &r.CrackWidthMM)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (d *DB) QueryAllLatestSensorData(ctx context.Context) ([]models.SensorReading, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT DISTINCT ON (sensor_id)
		 time, sensor_id, strain_micro, settlement_mm, temperature, crack_width_mm
		 FROM sensor_data
		 ORDER BY sensor_id, time DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.SensorReading
	for rows.Next() {
		var r models.SensorReading
		if err := rows.Scan(&r.Time, &r.SensorID, &r.StrainMicro, &r.SettlementMM, &r.Temperature, &r.CrackWidthMM); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (d *DB) InsertFEMResult(ctx context.Context, r *models.FEMStressResult) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO fem_stress_results (time, element_id, sigma_x, sigma_y, tau_xy, von_mises, node_ids)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		r.Time, r.ElementID, r.SigmaX, r.SigmaY, r.TauXY, r.VonMises, pq.Array(r.NodeIDs),
	)
	return err
}

func (d *DB) QueryFEMResults(ctx context.Context, start, end time.Time) ([]models.FEMStressResult, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT time, element_id, sigma_x, sigma_y, tau_xy, von_mises, node_ids
		 FROM fem_stress_results
		 WHERE time >= $1 AND time <= $2
		 ORDER BY time ASC`,
		start, end,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.FEMStressResult
	for rows.Next() {
		var r models.FEMStressResult
		var nodeIDs pq.Int64Array
		if err := rows.Scan(&r.Time, &r.ElementID, &r.SigmaX, &r.SigmaY, &r.TauXY, &r.VonMises, &nodeIDs); err != nil {
			return nil, err
		}
		r.NodeIDs = make([]int, len(nodeIDs))
		for i, v := range nodeIDs {
			r.NodeIDs[i] = int(v)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (d *DB) InsertPrediction(ctx context.Context, p *models.DeformationPrediction) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO deformation_predictions (time, node_id, predicted_dx, predicted_dy, target_year)
		 VALUES ($1, $2, $3, $4, $5)`,
		p.Time, p.NodeID, p.PredictedDX, p.PredictedDY, p.TargetYear,
	)
	return err
}

func (d *DB) QueryPredictions(ctx context.Context, targetYear int) ([]models.DeformationPrediction, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT time, node_id, predicted_dx, predicted_dy, target_year
		 FROM deformation_predictions
		 WHERE target_year = $1
		 ORDER BY node_id ASC`,
		targetYear,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.DeformationPrediction
	for rows.Next() {
		var p models.DeformationPrediction
		if err := rows.Scan(&p.Time, &p.NodeID, &p.PredictedDX, &p.PredictedDY, &p.TargetYear); err != nil {
			return nil, err
		}
		results = append(results, p)
	}
	return results, rows.Err()
}

func (d *DB) InsertAlert(ctx context.Context, a *models.Alert) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO alerts (time, alert_type, severity, message, sensor_id, value, threshold)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		a.Time, a.AlertType, a.Severity, a.Message, a.SensorID, a.Value, a.Threshold,
	)
	return err
}

func (d *DB) QueryAlerts(ctx context.Context, start, end time.Time, severity string) ([]models.Alert, error) {
	var rows *sql.Rows
	var err error

	if severity != "" {
		rows, err = d.db.QueryContext(ctx,
			`SELECT time, alert_type, severity, message, sensor_id, value, threshold
			 FROM alerts
			 WHERE time >= $1 AND time <= $2 AND severity = $3
			 ORDER BY time DESC`,
			start, end, severity,
		)
	} else {
		rows, err = d.db.QueryContext(ctx,
			`SELECT time, alert_type, severity, message, sensor_id, value, threshold
			 FROM alerts
			 WHERE time >= $1 AND time <= $2
			 ORDER BY time DESC`,
			start, end,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.Alert
	for rows.Next() {
		var a models.Alert
		if err := rows.Scan(&a.Time, &a.AlertType, &a.Severity, &a.Message, &a.SensorID, &a.Value, &a.Threshold); err != nil {
			return nil, err
		}
		results = append(results, a)
	}
	return results, rows.Err()
}

func (d *DB) GetSensorRegistry(ctx context.Context) ([]models.SensorInfo, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT sensor_id, sensor_type, location_x, location_y, location_z, installed_date, status
		 FROM sensor_registry
		 ORDER BY sensor_id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.SensorInfo
	for rows.Next() {
		var s models.SensorInfo
		if err := rows.Scan(&s.SensorID, &s.SensorType, &s.LocationX, &s.LocationY, &s.LocationZ, &s.InstalledDate, &s.Status); err != nil {
			return nil, err
		}
		results = append(results, s)
	}
	return results, rows.Err()
}

func (d *DB) QueryHourlyAggregates(ctx context.Context, sensorID string, start, end time.Time) ([]models.HourlyAggregate, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT time_bucket('1 hour', time) AS time_bucket,
		        AVG(strain_micro) AS avg_strain,
		        AVG(settlement_mm) AS avg_settlement,
		        AVG(temperature) AS avg_temperature,
		        AVG(crack_width_mm) AS avg_crack_width
		 FROM sensor_data
		 WHERE sensor_id = $1 AND time >= $2 AND time <= $3
		 GROUP BY time_bucket
		 ORDER BY time_bucket ASC`,
		sensorID, start, end,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.HourlyAggregate
	for rows.Next() {
		var a models.HourlyAggregate
		if err := rows.Scan(&a.TimeBucket, &a.AvgStrain, &a.AvgSettlement, &a.AvgTemperature, &a.AvgCrackWidth); err != nil {
			return nil, err
		}
		results = append(results, a)
	}
	return results, rows.Err()
}
