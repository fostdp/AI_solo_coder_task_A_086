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
