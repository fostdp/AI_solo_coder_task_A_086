package services

import (
	"context"
	"math"
	"time"

	"zhaozhou-bridge-monitor/models"
)

type FEMService struct {
	Nodes         []models.FEMNode
	Elements      []models.FEMElement
	Material      *models.MasonryMaterial
	Geometry      *models.BridgeGeometry
	LoadFactor    float64
	FreeDOFs      map[int]bool
	K             [][]float64
	Forces        []float64
	Displacements []float64
	thermalDeltaT float64
}

func NewFEMService(geom *models.BridgeGeometry, mat *models.MasonryMaterial) *FEMService {
	if geom == nil {
		geom = &models.BridgeGeometry{
			MainSpan:           37.02,
			MainRise:           7.23,
			Width:              9.6,
			SmallArchSpanLarge: 3.8,
			SmallArchSpanSmall: 2.8,
			SmallArchRiseLarge: 1.5,
			SmallArchRiseSmall: 1.0,
		}
	}
	if mat == nil {
		mat = &models.MasonryMaterial{
			ElasticModulus:        3e9,
			PoissonRatio:          0.15,
			Density:               2400,
			CompressiveStrength:   25e6,
			TensileStrength:       2e6,
			ThermalExpansionCoeff: 5e-6,
			CreepCoeff:            2.0,
		}
	}
	return &FEMService{
		Nodes:         make([]models.FEMNode, 0),
		Elements:      make([]models.FEMElement, 0),
		Material:      mat,
		Geometry:      geom,
		LoadFactor:    1.0,
		FreeDOFs:      make(map[int]bool),
		thermalDeltaT: 0.0,
	}
}

func (f *FEMService) addNode(x, y float64) int {
	id := len(f.Nodes)
	f.Nodes = append(f.Nodes, models.FEMNode{
		ID: id,
		X:  x,
		Y:  y,
	})
	return id
}

func parabolicY(x, span, rise, xOffset float64) float64 {
	xs := (x - xOffset) / span
	return rise - 4*rise*(xs-0.5)*(xs-0.5)
}

func (f *FEMService) GenerateMesh() error {
	f.Nodes = f.Nodes[:0]
	f.Elements = f.Elements[:0]

	span := f.Geometry.MainSpan
	rise := f.Geometry.MainRise
	deckThickness := 1.0
	archThickness := 0.8

	archBottomNodes := make([]int, 0)
	archTopNodes := make([]int, 0)
	numArchNodes := 20
	for i := 0; i < numArchNodes; i++ {
		t := float64(i) / float64(numArchNodes-1)
		x := t * span
		yBottom := parabolicY(x, span, rise, 0)
		archBottomNodes = append(archBottomNodes, f.addNode(x, yBottom))

		dyDx := -8 * rise * (t - 0.5) / span
		lenNorm := math.Sqrt(1 + dyDx*dyDx)
		nx := -dyDx / lenNorm
		ny := 1.0 / lenNorm
		xTop := x + nx*archThickness
		yTop := yBottom + ny*archThickness
		archTopNodes = append(archTopNodes, f.addNode(xTop, yTop))
	}

	deckY := parabolicY(0, span, rise, 0) + rise + deckThickness
	deckNodes := make([]int, 0)
	numDeckNodes := 15
	for i := 0; i < numDeckNodes; i++ {
		t := float64(i) / float64(numDeckNodes-1)
		x := t * span
		deckNodes = append(deckNodes, f.addNode(x, deckY))
	}

	pierHeight := rise + 2.0
	pierBottomLeft := f.addNode(0, -pierHeight)
	pierTopLeft := archBottomNodes[0]
	pierBottomRight := f.addNode(span, -pierHeight)
	pierTopRight := archBottomNodes[len(archBottomNodes)-1]

	pierMidLeft := f.addNode(0, -pierHeight/2)
	pierMidRight := f.addNode(span, -pierHeight/2)

	smallArches := make([][]int, 0)
	smallArchConfigs := []struct {
		span, rise, xStart float64
	}{
		{f.Geometry.SmallArchSpanSmall, f.Geometry.SmallArchRiseSmall, 2.0},
		{f.Geometry.SmallArchSpanLarge, f.Geometry.SmallArchRiseLarge, 2.0 + f.Geometry.SmallArchSpanSmall + 1.0},
		{f.Geometry.SmallArchSpanLarge, f.Geometry.SmallArchRiseLarge, span - 2.0 - f.Geometry.SmallArchSpanSmall - 1.0 - f.Geometry.SmallArchSpanLarge},
		{f.Geometry.SmallArchSpanSmall, f.Geometry.SmallArchRiseSmall, span - 2.0 - f.Geometry.SmallArchSpanSmall},
	}

	for _, cfg := range smallArchConfigs {
		saNodes := make([]int, 0)
		numSAnodes := 8
		for i := 0; i < numSAnodes; i++ {
			t := float64(i) / float64(numSAnodes-1)
			x := cfg.xStart + t*cfg.span
			yBase := parabolicY(x, span, rise, 0)
			dyDx := -8 * rise * ((x / span) - 0.5) / span
			lenNorm := math.Sqrt(1 + dyDx*dyDx)
			nx := -dyDx / lenNorm
			ny := 1.0 / lenNorm
			yArch := yBase + archThickness*ny + parabolicY(x, cfg.span, cfg.rise, cfg.xStart)
			xArch := x + archThickness*nx
			saNodes = append(saNodes, f.addNode(xArch, yArch))
		}
		smallArches = append(smallArches, saNodes)
	}

	elemID := 0
	addTri := func(n1, n2, n3 int, thickness float64) {
		f.Elements = append(f.Elements, models.FEMElement{
			ID:        elemID,
			NodeIDs:   [3]int{n1, n2, n3},
			Thickness: thickness,
			Material:  f.Material,
		})
		elemID++
	}

	for i := 0; i < len(archBottomNodes)-1; i++ {
		addTri(archBottomNodes[i], archBottomNodes[i+1], archTopNodes[i], 0.8)
		addTri(archBottomNodes[i+1], archTopNodes[i+1], archTopNodes[i], 0.8)
	}

	for i := 0; i < len(deckNodes)-1; i++ {
		archIdx := int(math.Round(float64(i) * float64(len(archTopNodes)-1) / float64(len(deckNodes)-1)))
		archIdx2 := int(math.Round(float64(i+1) * float64(len(archTopNodes)-1) / float64(len(deckNodes)-1)))
		addTri(deckNodes[i], deckNodes[i+1], archTopNodes[archIdx], 0.5)
		addTri(deckNodes[i+1], archTopNodes[archIdx2], archTopNodes[archIdx], 0.5)
	}

	addTri(pierBottomLeft, pierMidLeft, pierTopLeft, 1.0)
	addTri(pierBottomLeft, f.addNode(1.5, -pierHeight), pierMidLeft, 1.0)
	addTri(pierMidLeft, f.addNode(1.5, -pierHeight/2), pierTopLeft, 1.0)
	addTri(pierBottomRight, pierMidRight, pierTopRight, 1.0)
	addTri(pierBottomRight, f.addNode(span-1.5, -pierHeight), pierMidRight, 1.0)
	addTri(pierMidRight, f.addNode(span-1.5, -pierHeight/2), pierTopRight, 1.0)

	for _, saNodes := range smallArches {
		for i := 0; i < len(saNodes)-1; i++ {
			tBase := float64(i) / float64(len(saNodes)-1)
			tBase2 := float64(i+1) / float64(len(saNodes)-1)
			archIdx := int(math.Round(tBase * float64(len(archTopNodes)-1)))
			archIdx2 := int(math.Round(tBase2 * float64(len(archTopNodes)-1)))
			addTri(saNodes[i], saNodes[i+1], archTopNodes[archIdx], 0.6)
			addTri(saNodes[i+1], archTopNodes[archIdx2], archTopNodes[archIdx], 0.6)
		}
	}

	f.FreeDOFs = make(map[int]bool)
	totalDOFs := 2 * len(f.Nodes)
	for i := 0; i < totalDOFs; i++ {
		f.FreeDOFs[i] = true
	}
	f.FreeDOFs[2*pierBottomLeft] = false
	f.FreeDOFs[2*pierBottomLeft+1] = false
	f.FreeDOFs[2*pierBottomRight] = false
	f.FreeDOFs[2*pierBottomRight+1] = false

	f.Forces = make([]float64, totalDOFs)
	f.Displacements = make([]float64, totalDOFs)
	f.K = make([][]float64, totalDOFs)
	for i := range f.K {
		f.K[i] = make([]float64, totalDOFs)
	}

	return nil
}

func (f *FEMService) BuildStiffnessMatrix() error {
	numDOFs := 2 * len(f.Nodes)
	for i := range f.K {
		for j := range f.K[i] {
			f.K[i][j] = 0
		}
	}

	E := f.Material.ElasticModulus
	nu := f.Material.PoissonRatio
	D := f.buildConstitutiveMatrix(E, nu)

	for _, elem := range f.Elements {
		n1 := f.Nodes[elem.NodeIDs[0]]
		n2 := f.Nodes[elem.NodeIDs[1]]
		n3 := f.Nodes[elem.NodeIDs[2]]

		B, area := f.buildStrainDisplacementMatrix(n1, n2, n3)

		t := elem.Thickness
		Ke := f.computeElementStiffness(B, D, t, area)

		dofMap := make([]int, 6)
		for i := 0; i < 3; i++ {
			dofMap[2*i] = 2 * elem.NodeIDs[i]
			dofMap[2*i+1] = 2*elem.NodeIDs[i] + 1
		}

		for i := 0; i < 6; i++ {
			for j := 0; j < 6; j++ {
				gi := dofMap[i]
				gj := dofMap[j]
				if gi < numDOFs && gj < numDOFs {
					f.K[gi][gj] += Ke[i][j]
				}
			}
		}
	}

	return nil
}

func (f *FEMService) buildConstitutiveMatrix(E, nu float64) [3][3]float64 {
	var D [3][3]float64
	factor := E / (1 - nu*nu)
	D[0][0] = factor
	D[0][1] = factor * nu
	D[0][2] = 0
	D[1][0] = factor * nu
	D[1][1] = factor
	D[1][2] = 0
	D[2][0] = 0
	D[2][1] = 0
	D[2][2] = factor * (1 - nu) / 2
	return D
}

func (f *FEMService) buildStrainDisplacementMatrix(n1, n2, n3 models.FEMNode) ([3][6]float64, float64) {
	var B [3][6]float64
	x1, y1 := n1.X, n1.Y
	x2, y2 := n2.X, n2.Y
	x3, y3 := n3.X, n3.Y

	area := 0.5 * ((x2-x1)*(y3-y1) - (x3-x1)*(y2-y1))
	if area < 0 {
		area = -area
	}

	b1 := y2 - y3
	b2 := y3 - y1
	b3 := y1 - y2

	c1 := x3 - x2
	c2 := x1 - x3
	c3 := x2 - x1

	twoA := 2 * area
	if twoA == 0 {
		twoA = 1e-10
	}

	B[0][0] = b1 / twoA
	B[0][2] = b2 / twoA
	B[0][4] = b3 / twoA

	B[1][1] = c1 / twoA
	B[1][3] = c2 / twoA
	B[1][5] = c3 / twoA

	B[2][0] = c1 / twoA
	B[2][1] = b1 / twoA
	B[2][2] = c2 / twoA
	B[2][3] = b2 / twoA
	B[2][4] = c3 / twoA
	B[2][5] = b3 / twoA

	return B, area
}

func (f *FEMService) computeElementStiffness(B [3][6]float64, D [3][3]float64, t, A float64) [6][6]float64 {
	var Ke [6][6]float64
	var DB [3][6]float64

	for i := 0; i < 3; i++ {
		for j := 0; j < 6; j++ {
			DB[i][j] = 0
			for k := 0; k < 3; k++ {
				DB[i][j] += D[i][k] * B[k][j]
			}
		}
	}

	for i := 0; i < 6; i++ {
		for j := 0; j < 6; j++ {
			Ke[i][j] = 0
			for k := 0; k < 3; k++ {
				Ke[i][j] += B[k][i] * DB[k][j]
			}
			Ke[i][j] *= t * A
		}
	}

	return Ke
}

func (f *FEMService) ApplyGravityLoad() error {
	g := 9.81
	rho := f.Material.Density

	for _, elem := range f.Elements {
		n1 := f.Nodes[elem.NodeIDs[0]]
		n2 := f.Nodes[elem.NodeIDs[1]]
		n3 := f.Nodes[elem.NodeIDs[2]]

		_, area := f.buildStrainDisplacementMatrix(n1, n2, n3)
		weight := rho * g * area * elem.Thickness
		perNode := weight / 3.0

		for i := 0; i < 3; i++ {
			dofY := 2*elem.NodeIDs[i] + 1
			if dofY < len(f.Forces) {
				f.Forces[dofY] -= perNode * f.LoadFactor
			}
		}
	}

	return nil
}

func (f *FEMService) ApplyLiveLoad(laneLoad float64) error {
	span := f.Geometry.MainSpan
	width := f.Geometry.Width
	deckNodes := make([]int, 0)

	maxY := f.Nodes[0].Y
	for _, n := range f.Nodes {
		if n.Y > maxY {
			maxY = n.Y
		}
	}
	deckY := maxY
	tol := 0.5
	for _, n := range f.Nodes {
		if math.Abs(n.Y-deckY) < tol {
			deckNodes = append(deckNodes, n.ID)
		}
	}

	sortInts(deckNodes, func(i, j int) bool {
		return f.Nodes[deckNodes[i]].X < f.Nodes[deckNodes[j]].X
	})

	for i := 0; i < len(deckNodes)-1; i++ {
		n1 := f.Nodes[deckNodes[i]]
		n2 := f.Nodes[deckNodes[i+1]]
		dx := n2.X - n1.X
		forceSegment := laneLoad * dx * width
		perNode := forceSegment / 2.0

		dof1 := 2*deckNodes[i] + 1
		dof2 := 2*deckNodes[i+1] + 1
		if dof1 < len(f.Forces) {
			f.Forces[dof1] -= perNode * f.LoadFactor
		}
		if dof2 < len(f.Forces) {
			f.Forces[dof2] -= perNode * f.LoadFactor
		}
	}

	return nil
}

func sortInts(arr []int, less func(i, j int) bool) {
	n := len(arr)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if less(j+1, j) {
				arr[j], arr[j+1] = arr[j+1], arr[j]
			}
		}
	}
}

func (f *FEMService) ApplyThermalLoad(deltaT float64) error {
	f.thermalDeltaT = deltaT
	E := f.Material.ElasticModulus
	nu := f.Material.PoissonRatio
	alpha := f.Material.ThermalExpansionCoeff
	D := f.buildConstitutiveMatrix(E, nu)

	eps0 := [3]float64{alpha * deltaT, alpha * deltaT, 0}

	var D_eps0 [3]float64
	for i := 0; i < 3; i++ {
		D_eps0[i] = 0
		for j := 0; j < 3; j++ {
			D_eps0[i] += D[i][j] * eps0[j]
		}
	}

	for _, elem := range f.Elements {
		n1 := f.Nodes[elem.NodeIDs[0]]
		n2 := f.Nodes[elem.NodeIDs[1]]
		n3 := f.Nodes[elem.NodeIDs[2]]

		B, area := f.buildStrainDisplacementMatrix(n1, n2, n3)
		t := elem.Thickness

		var nodalForces [6]float64
		for i := 0; i < 6; i++ {
			nodalForces[i] = 0
			for k := 0; k < 3; k++ {
				nodalForces[i] += B[k][i] * D_eps0[k]
			}
			nodalForces[i] *= -t * area
		}

		for i := 0; i < 3; i++ {
			dofX := 2 * elem.NodeIDs[i]
			dofY := 2*elem.NodeIDs[i] + 1
			if dofX < len(f.Forces) {
				f.Forces[dofX] += nodalForces[2*i]
			}
			if dofY < len(f.Forces) {
				f.Forces[dofY] += nodalForces[2*i+1]
			}
		}
	}

	return nil
}

func (f *FEMService) Solve() error {
	freeList := make([]int, 0)
	numDOFs := 2 * len(f.Nodes)
	for i := 0; i < numDOFs; i++ {
		if f.FreeDOFs[i] {
			freeList = append(freeList, i)
		}
	}
	nFree := len(freeList)

	Kfree := make([][]float64, nFree)
	for i := range Kfree {
		Kfree[i] = make([]float64, nFree)
	}
	Ffree := make([]float64, nFree)

	for i := 0; i < nFree; i++ {
		Ffree[i] = f.Forces[freeList[i]]
		for j := 0; j < nFree; j++ {
			Kfree[i][j] = f.K[freeList[i]][freeList[j]]
		}
	}

	Ufree, err := gaussianElimination(Kfree, Ffree)
	if err != nil {
		return err
	}

	for i := 0; i < numDOFs; i++ {
		f.Displacements[i] = 0
	}
	for i := 0; i < nFree; i++ {
		f.Displacements[freeList[i]] = Ufree[i]
	}

	for i := range f.Nodes {
		f.Nodes[i].Dx = f.Displacements[2*i]
		f.Nodes[i].Dy = f.Displacements[2*i+1]
	}

	return nil
}

func gaussianElimination(A [][]float64, b []float64) ([]float64, error) {
	n := len(b)
	Aug := make([][]float64, n)
	for i := 0; i < n; i++ {
		Aug[i] = make([]float64, n+1)
		copy(Aug[i][:n], A[i])
		Aug[i][n] = b[i]
	}

	for col := 0; col < n; col++ {
		maxRow := col
		maxVal := math.Abs(Aug[col][col])
		for row := col + 1; row < n; row++ {
			if math.Abs(Aug[row][col]) > maxVal {
				maxVal = math.Abs(Aug[row][col])
				maxRow = row
			}
		}
		if maxVal < 1e-20 {
			for i := 0; i < n; i++ {
				if math.Abs(Aug[i][col]) > 1e-20 {
					maxRow = i
					maxVal = math.Abs(Aug[i][col])
					break
				}
			}
			if maxVal < 1e-20 {
				continue
			}
		}
		Aug[col], Aug[maxRow] = Aug[maxRow], Aug[col]

		pivot := Aug[col][col]
		if math.Abs(pivot) < 1e-15 {
			continue
		}
		for row := col + 1; row < n; row++ {
			factor := Aug[row][col] / pivot
			for j := col; j <= n; j++ {
				Aug[row][j] -= factor * Aug[col][j]
			}
		}
	}

	x := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		sum := Aug[i][n]
		for j := i + 1; j < n; j++ {
			sum -= Aug[i][j] * x[j]
		}
		if math.Abs(Aug[i][i]) > 1e-15 {
			x[i] = sum / Aug[i][i]
		} else {
			x[i] = 0
		}
	}

	return x, nil
}

func (f *FEMService) ComputeElementStresses() []models.FEMStressResult {
	results := make([]models.FEMStressResult, 0, len(f.Elements))
	now := time.Now()

	E := f.Material.ElasticModulus
	nu := f.Material.PoissonRatio
	alpha := f.Material.ThermalExpansionCoeff
	D := f.buildConstitutiveMatrix(E, nu)
	deltaT := f.thermalDeltaT

	for _, elem := range f.Elements {
		n1 := f.Nodes[elem.NodeIDs[0]]
		n2 := f.Nodes[elem.NodeIDs[1]]
		n3 := f.Nodes[elem.NodeIDs[2]]

		B, _ := f.buildStrainDisplacementMatrix(n1, n2, n3)

		var u [6]float64
		u[0] = n1.Dx
		u[1] = n1.Dy
		u[2] = n2.Dx
		u[3] = n2.Dy
		u[4] = n3.Dx
		u[5] = n3.Dy

		var eps [3]float64
		for i := 0; i < 3; i++ {
			eps[i] = 0
			for j := 0; j < 6; j++ {
				eps[i] += B[i][j] * u[j]
			}
		}

		eps[0] -= alpha * deltaT
		eps[1] -= alpha * deltaT

		var sig [3]float64
		for i := 0; i < 3; i++ {
			sig[i] = 0
			for j := 0; j < 3; j++ {
				sig[i] += D[i][j] * eps[j]
			}
		}

		sigx := sig[0]
		sigy := sig[1]
		tauxy := sig[2]
		vonMises := math.Sqrt(sigx*sigx - sigx*sigy + sigy*sigy + 3*tauxy*tauxy)

		nodeIDs := make([]int, 3)
		copy(nodeIDs, elem.NodeIDs[:])

		results = append(results, models.FEMStressResult{
			Time:      now,
			ElementID: elem.ID,
			SigmaX:    sigx,
			SigmaY:    sigy,
			TauXY:     tauxy,
			VonMises:  vonMises,
			NodeIDs:   nodeIDs,
		})
	}

	return results
}

func (f *FEMService) RunFullAnalysis(liveLoad, deltaT float64) ([]models.FEMStressResult, error) {
	ctx := context.Background()
	_ = ctx

	for i := range f.Forces {
		f.Forces[i] = 0
	}
	f.thermalDeltaT = 0.0

	if err := f.GenerateMesh(); err != nil {
		return nil, err
	}

	if err := f.BuildStiffnessMatrix(); err != nil {
		return nil, err
	}

	if err := f.ApplyGravityLoad(); err != nil {
		return nil, err
	}

	if liveLoad > 0 {
		if err := f.ApplyLiveLoad(liveLoad); err != nil {
			return nil, err
		}
	}

	if math.Abs(deltaT) > 1e-10 {
		if err := f.ApplyThermalLoad(deltaT); err != nil {
			return nil, err
		}
	}

	if err := f.Solve(); err != nil {
		return nil, err
	}

	return f.ComputeElementStresses(), nil
}
