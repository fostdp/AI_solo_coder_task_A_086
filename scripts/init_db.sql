CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE sensor_data (
    time TIMESTAMPTZ NOT NULL,
    sensor_id TEXT NOT NULL,
    strain_micro FLOAT,
    settlement_mm FLOAT,
    temperature FLOAT,
    crack_width_mm FLOAT
);

CREATE TABLE fem_stress_results (
    time TIMESTAMPTZ NOT NULL,
    element_id INT NOT NULL,
    sigma_x FLOAT,
    sigma_y FLOAT,
    tau_xy FLOAT,
    von_mises FLOAT,
    node_ids INT[]
);

CREATE TABLE deformation_predictions (
    time TIMESTAMPTZ NOT NULL,
    node_id INT NOT NULL,
    predicted_dx FLOAT,
    predicted_dy FLOAT,
    target_year INT
);

CREATE TABLE alerts (
    time TIMESTAMPTZ NOT NULL,
    alert_type TEXT,
    severity TEXT,
    message TEXT,
    sensor_id TEXT,
    value FLOAT,
    threshold FLOAT
);

CREATE TABLE sensor_registry (
    sensor_id TEXT PRIMARY KEY,
    sensor_type TEXT,
    location_x FLOAT,
    location_y FLOAT,
    location_z FLOAT,
    installed_date DATE,
    status TEXT
);

INSERT INTO sensor_registry (sensor_id, sensor_type, location_x, location_y, location_z, installed_date, status) VALUES
('ARCH-001', 'strain', 12.5, 6.8, 3.2, '2023-03-15', 'active'),
('ARCH-002', 'strain', 25.0, 6.8, 3.2, '2023-03-15', 'active'),
('ARCH-003', 'strain', 37.5, 6.8, 3.2, '2023-03-15', 'active'),
('ARCH-004', 'strain', 50.0, 6.8, 3.2, '2023-03-15', 'active'),
('PIER-001', 'settlement', 5.0, 0.0, 0.0, '2023-04-01', 'active'),
('PIER-002', 'settlement', 55.0, 0.0, 0.0, '2023-04-01', 'active'),
('SARCH-L001', 'strain', 15.0, 6.8, 5.5, '2023-04-10', 'active'),
('SARCH-L002', 'strain', 20.0, 6.8, 5.5, '2023-04-10', 'active'),
('SARCH-R001', 'strain', 40.0, 6.8, 5.5, '2023-04-10', 'active'),
('SARCH-R002', 'strain', 45.0, 6.8, 5.5, '2023-04-10', 'active'),
('CRACK-001', 'crack', 18.0, 6.5, 4.0, '2023-05-01', 'active'),
('CRACK-002', 'crack', 42.0, 6.5, 4.0, '2023-05-01', 'active');

SELECT create_hypertable('sensor_data', 'time', chunk_time_interval => INTERVAL '1 day');
SELECT create_hypertable('fem_stress_results', 'time', chunk_time_interval => INTERVAL '7 days');
SELECT create_hypertable('deformation_predictions', 'time', chunk_time_interval => INTERVAL '30 days');
SELECT create_hypertable('alerts', 'time', chunk_time_interval => INTERVAL '7 days');

CREATE INDEX idx_sensor_data_sensor_id ON sensor_data (sensor_id, time DESC);
CREATE INDEX idx_sensor_data_time ON sensor_data (time DESC);
CREATE INDEX idx_fem_stress_element_id ON fem_stress_results (element_id, time DESC);
CREATE INDEX idx_fem_stress_time ON fem_stress_results (time DESC);
CREATE INDEX idx_deformation_node_id ON deformation_predictions (node_id, time DESC);
CREATE INDEX idx_deformation_time ON deformation_predictions (time DESC);
CREATE INDEX idx_alerts_sensor_id ON alerts (sensor_id, time DESC);
CREATE INDEX idx_alerts_time ON alerts (time DESC);

CREATE MATERIALIZED VIEW sensor_data_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time) AS bucket,
    sensor_id,
    AVG(strain_micro) AS avg_strain_micro,
    AVG(settlement_mm) AS avg_settlement_mm,
    AVG(temperature) AS avg_temperature,
    AVG(crack_width_mm) AS avg_crack_width_mm,
    MIN(strain_micro) AS min_strain_micro,
    MAX(strain_micro) AS max_strain_micro,
    COUNT(*) AS sample_count
FROM sensor_data
GROUP BY bucket, sensor_id
WITH NO DATA;

CREATE MATERIALIZED VIEW sensor_data_daily
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', time) AS bucket,
    sensor_id,
    AVG(strain_micro) AS avg_strain_micro,
    AVG(settlement_mm) AS avg_settlement_mm,
    AVG(temperature) AS avg_temperature,
    AVG(crack_width_mm) AS avg_crack_width_mm,
    MIN(strain_micro) AS min_strain_micro,
    MAX(strain_micro) AS max_strain_micro,
    STDDEV(strain_micro) AS stddev_strain_micro,
    COUNT(*) AS sample_count
FROM sensor_data
GROUP BY bucket, sensor_id
WITH NO DATA;

SELECT add_continuous_aggregate_policy('sensor_data_hourly',
    start_offset => INTERVAL '3 hours',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour');

SELECT add_continuous_aggregate_policy('sensor_data_daily',
    start_offset => INTERVAL '3 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '1 day');

SELECT add_retention_policy('sensor_data', INTERVAL '2 years');
SELECT add_retention_policy('fem_stress_results', INTERVAL '2 years');
SELECT add_retention_policy('deformation_predictions', INTERVAL '2 years');
SELECT add_retention_policy('alerts', INTERVAL '2 years');

-- ============================================
-- PRODUCTION ENHANCEMENTS
-- ============================================

-- 8. Add telemetry extension
CREATE EXTENSION IF NOT EXISTS timescaledb_toolkit;

-- ============================================
-- 1. Materialized Views
-- ============================================

-- 15-minute continuous aggregate
CREATE MATERIALIZED VIEW sensor_data_15min
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('15 minutes', time) AS bucket,
    sensor_id,
    AVG(strain_micro) AS avg_strain_micro,
    MIN(strain_micro) AS min_strain_micro,
    MAX(strain_micro) AS max_strain_micro,
    STDDEV(strain_micro) AS stddev_strain_micro,
    AVG(settlement_mm) AS avg_settlement_mm,
    MIN(settlement_mm) AS min_settlement_mm,
    MAX(settlement_mm) AS max_settlement_mm,
    STDDEV(settlement_mm) AS stddev_settlement_mm,
    AVG(temperature) AS avg_temperature,
    MIN(temperature) AS min_temperature,
    MAX(temperature) AS max_temperature,
    STDDEV(temperature) AS stddev_temperature,
    AVG(crack_width_mm) AS avg_crack_width_mm,
    MIN(crack_width_mm) AS min_crack_width_mm,
    MAX(crack_width_mm) AS max_crack_width_mm,
    STDDEV(crack_width_mm) AS stddev_crack_width_mm,
    COUNT(*) AS sample_count
FROM sensor_data
GROUP BY bucket, sensor_id
WITH NO DATA;

-- 1-hour continuous aggregate (enhanced with percentile_cont)
CREATE MATERIALIZED VIEW sensor_data_1hour
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time) AS bucket,
    sensor_id,
    AVG(strain_micro) AS avg_strain_micro,
    MIN(strain_micro) AS min_strain_micro,
    MAX(strain_micro) AS max_strain_micro,
    STDDEV(strain_micro) AS stddev_strain_micro,
    PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY strain_micro) AS p95_strain_micro,
    AVG(settlement_mm) AS avg_settlement_mm,
    MIN(settlement_mm) AS min_settlement_mm,
    MAX(settlement_mm) AS max_settlement_mm,
    STDDEV(settlement_mm) AS stddev_settlement_mm,
    PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY settlement_mm) AS p95_settlement_mm,
    AVG(temperature) AS avg_temperature,
    MIN(temperature) AS min_temperature,
    MAX(temperature) AS max_temperature,
    STDDEV(temperature) AS stddev_temperature,
    PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY temperature) AS p95_temperature,
    AVG(crack_width_mm) AS avg_crack_width_mm,
    MIN(crack_width_mm) AS min_crack_width_mm,
    MAX(crack_width_mm) AS max_crack_width_mm,
    STDDEV(crack_width_mm) AS stddev_crack_width_mm,
    PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY crack_width_mm) AS p95_crack_width_mm,
    COUNT(*) AS sample_count
FROM sensor_data
GROUP BY bucket, sensor_id
WITH NO DATA;

-- 1-day continuous aggregate (enhanced with daily delta and growth rate)
CREATE MATERIALIZED VIEW sensor_data_1day
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', time) AS bucket,
    sensor_id,
    AVG(strain_micro) AS avg_strain_micro,
    MIN(strain_micro) AS min_strain_micro,
    MAX(strain_micro) AS max_strain_micro,
    STDDEV(strain_micro) AS stddev_strain_micro,
    AVG(settlement_mm) AS avg_settlement_mm,
    MIN(settlement_mm) AS min_settlement_mm,
    MAX(settlement_mm) AS max_settlement_mm,
    STDDEV(settlement_mm) AS stddev_settlement_mm,
    FIRST(settlement_mm, time) AS first_settlement_mm,
    LAST(settlement_mm, time) AS last_settlement_mm,
    (LAST(settlement_mm, time) - FIRST(settlement_mm, time)) AS daily_settlement_delta_mm,
    AVG(temperature) AS avg_temperature,
    MIN(temperature) AS min_temperature,
    MAX(temperature) AS max_temperature,
    STDDEV(temperature) AS stddev_temperature,
    AVG(crack_width_mm) AS avg_crack_width_mm,
    MIN(crack_width_mm) AS min_crack_width_mm,
    MAX(crack_width_mm) AS max_crack_width_mm,
    STDDEV(crack_width_mm) AS stddev_crack_width_mm,
    FIRST(crack_width_mm, time) AS first_crack_width_mm,
    LAST(crack_width_mm, time) AS last_crack_width_mm,
    (MAX(crack_width_mm) - MIN(crack_width_mm)) AS daily_max_crack_growth_mm,
    COUNT(*) AS sample_count
FROM sensor_data
GROUP BY bucket, sensor_id
WITH NO DATA;

-- Latest readings materialized view
CREATE MATERIALIZED VIEW sensor_latest AS
SELECT DISTINCT ON (sensor_id)
    time,
    sensor_id,
    strain_micro,
    settlement_mm,
    temperature,
    crack_width_mm
FROM sensor_data
ORDER BY sensor_id, time DESC
WITH NO DATA;

-- ============================================
-- 4. Continuous Aggregate Policies
-- ============================================

SELECT add_continuous_aggregate_policy('sensor_data_15min',
    start_offset => INTERVAL '1 hour',
    end_offset => INTERVAL '15 minutes',
    schedule_interval => INTERVAL '30 minutes',
    if_not_exists => TRUE);

SELECT add_continuous_aggregate_policy('sensor_data_1hour',
    start_offset => INTERVAL '3 hours',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour',
    if_not_exists => TRUE);

SELECT add_continuous_aggregate_policy('sensor_data_1day',
    start_offset => INTERVAL '3 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '1 day',
    if_not_exists => TRUE);

-- Refresh policy for sensor_latest
SELECT add_refresh_policy(
    relation => 'sensor_latest',
    schedule_interval => INTERVAL '1 minute',
    if_not_exists => TRUE
);

-- ============================================
-- 2. Compression Policies
-- ============================================

ALTER TABLE sensor_data SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'sensor_id',
    timescaledb.compress_orderby = 'time DESC'
);

ALTER TABLE fem_stress_results SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'element_id',
    timescaledb.compress_orderby = 'time DESC'
);

ALTER TABLE alerts SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'severity',
    timescaledb.compress_orderby = 'time DESC'
);

SELECT add_compression_policy('sensor_data',
    compress_after => INTERVAL '7 days',
    if_not_exists => TRUE);

SELECT add_compression_policy('fem_stress_results',
    compress_after => INTERVAL '30 days',
    if_not_exists => TRUE);

SELECT add_compression_policy('alerts',
    compress_after => INTERVAL '1 year',
    if_not_exists => TRUE);

-- ============================================
-- 3. Enhanced Retention Policies
-- ============================================

-- Remove existing retention policies with incorrect intervals
SELECT remove_retention_policy('sensor_data', if_not_exists => TRUE);
SELECT remove_retention_policy('fem_stress_results', if_not_exists => TRUE);
SELECT remove_retention_policy('deformation_predictions', if_not_exists => TRUE);
SELECT remove_retention_policy('alerts', if_not_exists => TRUE);

-- Raw sensor data: 2 years
SELECT add_retention_policy('sensor_data',
    drop_after => INTERVAL '2 years',
    if_not_exists => TRUE);

-- 15min aggregate: 5 years
SELECT add_retention_policy('sensor_data_15min',
    drop_after => INTERVAL '5 years',
    if_not_exists => TRUE);

-- 1hour aggregate: 10 years
SELECT add_retention_policy('sensor_data_1hour',
    drop_after => INTERVAL '10 years',
    if_not_exists => TRUE);

-- 1day aggregate: permanent (no retention)

-- FEM results: 10 years
SELECT add_retention_policy('fem_stress_results',
    drop_after => INTERVAL '10 years',
    if_not_exists => TRUE);

-- Deformation predictions: permanent (no retention)

-- Alerts: 5 years
SELECT add_retention_policy('alerts',
    drop_after => INTERVAL '5 years',
    if_not_exists => TRUE);

-- ============================================
-- 5. Additional Indexes
-- ============================================

CREATE INDEX IF NOT EXISTS idx_sensor_data_time_sensor ON sensor_data (time DESC, sensor_id);
CREATE INDEX IF NOT EXISTS idx_fem_results_time_element ON fem_stress_results (time DESC, element_id);
CREATE INDEX IF NOT EXISTS idx_alerts_time_severity ON alerts (time DESC, severity);
CREATE INDEX IF NOT EXISTS idx_sensor_registry_type ON sensor_registry (sensor_type);

-- ============================================
-- 6. Informational Views
-- ============================================

CREATE OR REPLACE VIEW bridge_health_summary AS
SELECT
    sr.sensor_id,
    sr.sensor_type,
    sr.location_x,
    sr.location_y,
    sr.location_z,
    sr.installed_date,
    sl.time AS last_reading_time,
    sl.strain_micro,
    sl.settlement_mm,
    sl.temperature,
    sl.crack_width_mm,
    CASE
        WHEN sr.sensor_type = 'strain' THEN
            CASE
                WHEN sl.strain_micro > 200 THEN 'critical'
                WHEN sl.strain_micro > 150 THEN 'warning'
                ELSE 'normal'
            END
        WHEN sr.sensor_type = 'settlement' THEN
            CASE
                WHEN ABS(sl.settlement_mm) > 50 THEN 'critical'
                WHEN ABS(sl.settlement_mm) > 30 THEN 'warning'
                ELSE 'normal'
            END
        WHEN sr.sensor_type = 'crack' THEN
            CASE
                WHEN sl.crack_width_mm > 2.0 THEN 'critical'
                WHEN sl.crack_width_mm > 1.0 THEN 'warning'
                ELSE 'normal'
            END
        WHEN sl.temperature > 60 OR sl.temperature < -20 THEN 'warning'
        ELSE 'normal'
    END AS status,
    CASE sr.sensor_type
        WHEN 'strain' THEN 'threshold: >150 warning, >200 critical'
        WHEN 'settlement' THEN 'threshold: >30mm warning, >50mm critical'
        WHEN 'crack' THEN 'threshold: >1.0mm warning, >2.0mm critical'
        ELSE 'temperature: >60 or < -20 warning'
    END AS threshold_info
FROM sensor_registry sr
LEFT JOIN sensor_latest sl ON sr.sensor_id = sl.sensor_id
WHERE sr.status = 'active';

CREATE OR REPLACE VIEW daily_alert_summary AS
SELECT
    time_bucket('1 day', time) AS alert_day,
    severity,
    alert_type,
    COUNT(*) AS alert_count,
    STRING_AGG(DISTINCT sensor_id, ', ') AS affected_sensors
FROM alerts
GROUP BY alert_day, severity, alert_type
ORDER BY alert_day DESC, severity, alert_type;

-- ============================================
-- 7. Read-only Monitoring Role
-- ============================================

DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'monitoring_reader') THEN
    CREATE ROLE monitoring_reader NOLOGIN;
  END IF;
END$$;

GRANT SELECT ON ALL TABLES IN SCHEMA public TO monitoring_reader;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO monitoring_reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO monitoring_reader;
