package models

import "time"

type SensorReading struct {
	Time          time.Time
	SensorID      string
	StrainMicro   float64
	SettlementMM  float64
	Temperature   float64
	CrackWidthMM  float64
}

type FEMStressResult struct {
	Time      time.Time
	ElementID int
	SigmaX    float64
	SigmaY    float64
	TauXY     float64
	VonMises  float64
	NodeIDs   []int
}

type DeformationPrediction struct {
	Time        time.Time
	NodeID      int
	PredictedDX float64
	PredictedDY float64
	TargetYear  int
}

type Alert struct {
	Time      time.Time
	AlertType string
	Severity  string
	Message   string
	SensorID  string
	Value     float64
	Threshold float64
}

type SensorInfo struct {
	SensorID      string
	SensorType    string
	LocationX     float64
	LocationY     float64
	LocationZ     float64
	InstalledDate time.Time
	Status        string
}

type FEMNode struct {
	ID int
	X  float64
	Y  float64
	Dx float64
	Dy float64
}

type FEMElement struct {
	ID        int
	NodeIDs   [3]int
	Thickness float64
	Material  *MasonryMaterial
}

type MasonryMaterial struct {
	ElasticModulus       float64
	PoissonRatio        float64
	Density              float64
	CompressiveStrength  float64
	TensileStrength      float64
	ThermalExpansionCoeff float64
	CreepCoeff           float64
}

type BridgeGeometry struct {
	MainSpan            float64
	MainRise            float64
	Width               float64
	SmallArchSpanLarge  float64
	SmallArchSpanSmall  float64
	SmallArchRiseLarge  float64
	SmallArchRiseSmall  float64
}

type HourlyAggregate struct {
	TimeBucket     time.Time
	AvgStrain      float64
	AvgSettlement  float64
	AvgTemperature float64
	AvgCrackWidth  float64
}
