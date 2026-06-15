var scene, camera, renderer, controls, bridgeGroup, stressOverlayGroup;
var crackMarkers = [], sensorMarkers = [];
var geometryData = null, stressData = null;
var autoRotate = false, stressVisible = false, cracksVisible = false;
var originalPositions = null, deformed = false;

function initBridgeScene(containerId) {
    var container = document.getElementById(containerId);
    if (!container) return;

    scene = new THREE.Scene();
    scene.fog = new THREE.Fog(0x1a2530, 80, 200);

    var skyCanvas = document.createElement('canvas');
    skyCanvas.width = 512;
    skyCanvas.height = 512;
    var skyCtx = skyCanvas.getContext('2d');
    var skyGrad = skyCtx.createLinearGradient(0, 0, 0, 512);
    skyGrad.addColorStop(0, '#87CEEB');
    skyGrad.addColorStop(0.5, '#c9d8e8');
    skyGrad.addColorStop(1, '#e8dcc8');
    skyCtx.fillStyle = skyGrad;
    skyCtx.fillRect(0, 0, 512, 512);
    var skyTex = new THREE.CanvasTexture(skyCanvas);
    scene.background = skyTex;

    camera = new THREE.PerspectiveCamera(45, container.clientWidth / container.clientHeight, 0.1, 1000);
    camera.position.set(50, 30, 60);

    renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
    renderer.setSize(container.clientWidth, container.clientHeight);
    renderer.setPixelRatio(window.devicePixelRatio);
    renderer.shadowMap.enabled = true;
    renderer.shadowMap.type = THREE.PCFSoftShadowMap;
    container.appendChild(renderer.domElement);

    controls = new THREE.OrbitControls(camera, renderer.domElement);
    controls.enableDamping = true;
    controls.dampingFactor = 0.08;
    controls.target.set(18.5, 4, 0);
    controls.minDistance = 10;
    controls.maxDistance = 150;
    controls.maxPolarAngle = Math.PI / 2.1;

    var ambientLight = new THREE.AmbientLight(0xffffff, 0.55);
    scene.add(ambientLight);

    var hemiLight = new THREE.HemisphereLight(0xb4d0f0, 0x8b7355, 0.45);
    scene.add(hemiLight);

    var dirLight1 = new THREE.DirectionalLight(0xffffff, 0.85);
    dirLight1.position.set(40, 60, 30);
    dirLight1.castShadow = true;
    dirLight1.shadow.mapSize.width = 2048;
    dirLight1.shadow.mapSize.height = 2048;
    dirLight1.shadow.camera.near = 0.5;
    dirLight1.shadow.camera.far = 200;
    dirLight1.shadow.camera.left = -60;
    dirLight1.shadow.camera.right = 60;
    dirLight1.shadow.camera.top = 60;
    dirLight1.shadow.camera.bottom = -60;
    dirLight1.shadow.bias = -0.0005;
    scene.add(dirLight1);

    var dirLight2 = new THREE.DirectionalLight(0xffeedd, 0.35);
    dirLight2.position.set(-30, 40, -25);
    dirLight2.castShadow = true;
    dirLight2.shadow.mapSize.width = 1024;
    dirLight2.shadow.mapSize.height = 1024;
    dirLight2.shadow.camera.near = 0.5;
    dirLight2.shadow.camera.far = 150;
    dirLight2.shadow.camera.left = -50;
    dirLight2.shadow.camera.right = 50;
    dirLight2.shadow.camera.top = 50;
    dirLight2.shadow.camera.bottom = -50;
    scene.add(dirLight2);

    var groundGeo = new THREE.PlaneGeometry(400, 400);
    var groundCanvas = document.createElement('canvas');
    groundCanvas.width = 256;
    groundCanvas.height = 256;
    var gctx = groundCanvas.getContext('2d');
    gctx.fillStyle = '#d5cfc3';
    gctx.fillRect(0, 0, 256, 256);
    for (var i = 0; i < 2000; i++) {
        var px = Math.random() * 256;
        var py = Math.random() * 256;
        var shade = 180 + Math.floor(Math.random() * 50);
        gctx.fillStyle = 'rgb(' + shade + ',' + (shade - 10) + ',' + (shade - 30) + ')';
        gctx.fillRect(px, py, 2, 2);
    }
    var groundTex = new THREE.CanvasTexture(groundCanvas);
    groundTex.wrapS = THREE.RepeatWrapping;
    groundTex.wrapT = THREE.RepeatWrapping;
    groundTex.repeat.set(20, 20);
    var groundMat = new THREE.MeshStandardMaterial({
        map: groundTex,
        roughness: 0.95,
        metalness: 0.0,
        color: 0xcfc9bd
    });
    var ground = new THREE.Mesh(groundGeo, groundMat);
    ground.rotation.x = -Math.PI / 2;
    ground.position.y = -0.01;
    ground.receiveShadow = true;
    scene.add(ground);

    bridgeGroup = buildZhaozhouBridge();
    scene.add(bridgeGroup);

    stressOverlayGroup = new THREE.Group();
    scene.add(stressOverlayGroup);

    setupGroundAndEnvironment(bridgeGroup);

    window.addEventListener('resize', onWindowResize, false);

    animate();
}

function buildZhaozhouBridge() {
    var group = new THREE.Group();

    var mainSpan = 37.02;
    var mainRise = 7.23;
    var archThick = 1.0;
    var deckWidth = 9.6;
    var deckThick = 0.4;
    var abutmentHeight = 4.0;

    var parabolaCurve = new THREE.CurvePath();
    var parabolaPoints = [];
    var segments = 80;
    for (var i = 0; i <= segments; i++) {
        var t = i / segments;
        var x = t * mainSpan;
        var y = mainRise - 4 * mainRise * Math.pow(t - 0.5, 2);
        parabolaPoints.push(new THREE.Vector3(x, y + abutmentHeight, 0));
    }
    var parabolaLine = new THREE.CatmullRomCurve3(parabolaPoints);
    parabolaCurve.add(parabolaLine);

    var mainArchGeo = new THREE.TubeGeometry(parabolaLine, 80, archThick / 2, 8, false);
    var archMat = new THREE.MeshStandardMaterial({
        color: 0xc9a87c,
        roughness: 0.85,
        metalness: 0.05
    });

    var mainArch = new THREE.Mesh(mainArchGeo, archMat);
    mainArch.castShadow = true;
    mainArch.receiveShadow = true;
    group.add(mainArch);

    originalPositions = new Float32Array(mainArchGeo.attributes.position.array.length);
    originalPositions.set(mainArchGeo.attributes.position.array);

    var deckLength = mainSpan + 2;
    var deckGeo = new THREE.BoxGeometry(deckLength, deckThick, deckWidth);
    var deckMat = new THREE.MeshStandardMaterial({
        color: 0xb8a078,
        roughness: 0.9,
        metalness: 0.03
    });
    var deck = new THREE.Mesh(deckGeo, deckMat);
    deck.position.set(mainSpan / 2, abutmentHeight + mainRise + deckThick / 2, 0);
    deck.castShadow = true;
    deck.receiveShadow = true;
    group.add(deck);

    var parapetMat = new THREE.MeshStandardMaterial({
        color: 0x9a8260,
        roughness: 0.88,
        metalness: 0.02
    });
    var parapetHeight = 0.3;
    var parapetThick = 0.2;

    var parapetGeo = new THREE.BoxGeometry(deckLength, parapetHeight, parapetThick);
    var parapet1 = new THREE.Mesh(parapetGeo, parapetMat);
    parapet1.position.set(mainSpan / 2, abutmentHeight + mainRise + deckThick + parapetHeight / 2, deckWidth / 2 - parapetThick / 2);
    parapet1.castShadow = true;
    parapet1.receiveShadow = true;
    group.add(parapet1);

    var parapet2 = new THREE.Mesh(parapetGeo, parapetMat);
    parapet2.position.set(mainSpan / 2, abutmentHeight + mainRise + deckThick + parapetHeight / 2, -deckWidth / 2 + parapetThick / 2);
    parapet2.castShadow = true;
    parapet2.receiveShadow = true;
    group.add(parapet2);

    var postGeo = new THREE.CylinderGeometry(0.06, 0.06, parapetHeight, 8);
    var postSpacing = 2;
    for (var px = -1; px <= mainSpan + 1; px += postSpacing) {
        var post1 = new THREE.Mesh(postGeo, parapetMat);
        post1.position.set(px, abutmentHeight + mainRise + deckThick + parapetHeight / 2, deckWidth / 2 - parapetThick / 2);
        post1.castShadow = true;
        group.add(post1);

        var post2 = new THREE.Mesh(postGeo, parapetMat);
        post2.position.set(px, abutmentHeight + mainRise + deckThick + parapetHeight / 2, -deckWidth / 2 + parapetThick / 2);
        post2.castShadow = true;
        group.add(post2);
    }

    var spandrelMat = archMat.clone();
    var spandrelThickness = deckWidth - 0.5;

    function parabolaY(x) {
        var t = x / mainSpan;
        return mainRise - 4 * mainRise * Math.pow(t - 0.5, 2) + abutmentHeight;
    }

    var smallArches = [
        { xCenter: 5, span: 2.8, rise: 1.0, thick: 0.5 },
        { xCenter: 10, span: 3.8, rise: 1.5, thick: 0.6 },
        { xCenter: mainSpan - 10, span: 3.8, rise: 1.5, thick: 0.6 },
        { xCenter: mainSpan - 5, span: 2.8, rise: 1.0, thick: 0.5 }
    ];

    var wallSegments = [
        { xStart: 0, xEnd: smallArches[0].xCenter - smallArches[0].span / 2 },
        { xStart: smallArches[0].xCenter + smallArches[0].span / 2, xEnd: smallArches[1].xCenter - smallArches[1].span / 2 },
        { xStart: smallArches[1].xCenter + smallArches[1].span / 2, xEnd: smallArches[2].xCenter - smallArches[2].span / 2 },
        { xStart: smallArches[2].xCenter + smallArches[2].span / 2, xEnd: smallArches[3].xCenter - smallArches[3].span / 2 },
        { xStart: smallArches[3].xCenter + smallArches[3].span / 2, xEnd: mainSpan }
    ];

    wallSegments.forEach(function(seg) {
        var numSlices = Math.max(2, Math.ceil((seg.xEnd - seg.xStart) / 0.5));
        for (var s = 0; s < numSlices; s++) {
            var x0 = seg.xStart + (s / numSlices) * (seg.xEnd - seg.xStart);
            var x1 = seg.xStart + ((s + 1) / numSlices) * (seg.xEnd - seg.xStart);
            var segWidth = x1 - x0;
            var yArch0 = parabolaY(x0);
            var yArch1 = parabolaY(x1);
            var yArchAvg = (yArch0 + yArch1) / 2;
            var yDeck = abutmentHeight + mainRise + deckThick;
            var segHeight = yDeck - yArchAvg;

            if (segHeight > 0.05 && segWidth > 0.01) {
                var segGeo = new THREE.BoxGeometry(segWidth, segHeight, spandrelThickness);
                var segMesh = new THREE.Mesh(segGeo, spandrelMat);
                segMesh.position.set((x0 + x1) / 2, yArchAvg + segHeight / 2, 0);
                segMesh.castShadow = true;
                segMesh.receiveShadow = true;
                group.add(segMesh);
            }
        }
    });

    smallArches.forEach(function(sa, idx) {
        var saPoints = [];
        var saSegs = 30;
        for (var si = 0; si <= saSegs; si++) {
            var st = si / saSegs;
            var sax = sa.xCenter - sa.span / 2 + st * sa.span;
            var say = sa.rise - 4 * sa.rise * Math.pow(st - 0.5, 2);
            var baseY = parabolaY(sax);
            saPoints.push(new THREE.Vector3(sax, baseY + say, 0));
        }
        var saCurve = new THREE.CatmullRomCurve3(saPoints);
        var saGeo = new THREE.TubeGeometry(saCurve, 30, sa.thick / 2, 6, false);
        var saMesh = new THREE.Mesh(saGeo, archMat);
        saMesh.castShadow = true;
        saMesh.receiveShadow = true;
        group.add(saMesh);
    });

    var abutmentWidth = 6.0;
    var abutmentThickZ = deckWidth + 1;
    var abutmentGeo = new THREE.BoxGeometry(abutmentWidth, abutmentHeight, abutmentThickZ);
    var abutmentMat = new THREE.MeshStandardMaterial({
        color: 0xb5966e,
        roughness: 0.9,
        metalness: 0.02
    });

    var abutment1 = new THREE.Mesh(abutmentGeo, abutmentMat);
    abutment1.position.set(-1 - abutmentWidth / 2 + 0.5, abutmentHeight / 2, 0);
    abutment1.castShadow = true;
    abutment1.receiveShadow = true;
    group.add(abutment1);

    var abutment2 = new THREE.Mesh(abutmentGeo, abutmentMat);
    abutment2.position.set(mainSpan + 1 + abutmentWidth / 2 - 0.5, abutmentHeight / 2, 0);
    abutment2.castShadow = true;
    abutment2.receiveShadow = true;
    group.add(abutment2);

    function addSensorMarker(position, type, label) {
        var colors = {
            ARCH: 0x00aaaa,
            PIER: 0x0066ff,
            SARCH: 0x00cc44,
            CRACK: 0xffcc00
        };
        var sensorGeo = new THREE.SphereGeometry(0.15, 16, 16);
        var sensorMat = new THREE.MeshStandardMaterial({
            color: colors[type] || 0xffffff,
            emissive: colors[type] || 0xffffff,
            emissiveIntensity: 0.6,
            roughness: 0.3,
            metalness: 0.5
        });
        var sensor = new THREE.Mesh(sensorGeo, sensorMat);
        sensor.position.copy(position);
        sensor.castShadow = true;
        sensor.userData = { type: type, label: label };
        group.add(sensor);
        sensorMarkers.push(sensor);

        var haloCanvas = document.createElement('canvas');
        haloCanvas.width = 128;
        haloCanvas.height = 128;
        var hctx = haloCanvas.getContext('2d');
        var haloGrad = hctx.createRadialGradient(64, 64, 0, 64, 64, 64);
        var hexColor = '#' + (colors[type] || 0xffffff).toString(16).padStart(6, '0');
        haloGrad.addColorStop(0, hexColor + 'cc');
        haloGrad.addColorStop(0.5, hexColor + '55');
        haloGrad.addColorStop(1, hexColor + '00');
        hctx.fillStyle = haloGrad;
        hctx.fillRect(0, 0, 128, 128);
        var haloTex = new THREE.CanvasTexture(haloCanvas);
        var haloMat = new THREE.SpriteMaterial({
            map: haloTex,
            transparent: true,
            depthWrite: false,
            blending: THREE.AdditiveBlending
        });
        var halo = new THREE.Sprite(haloMat);
        halo.scale.set(1.2, 1.2, 1.2);
        halo.position.copy(position);
        group.add(halo);
        sensor.userData.halo = halo;
    }

    addSensorMarker(new THREE.Vector3(mainSpan / 2, parabolaY(mainSpan / 2) + 0.6, 0), 'ARCH', 'M1-Crown');
    addSensorMarker(new THREE.Vector3(mainSpan * 0.25, parabolaY(mainSpan * 0.25) + 0.6, deckWidth * 0.25), 'ARCH', 'M2-QuarterL');
    addSensorMarker(new THREE.Vector3(mainSpan * 0.75, parabolaY(mainSpan * 0.75) + 0.6, -deckWidth * 0.25), 'ARCH', 'M3-QuarterR');
    addSensorMarker(new THREE.Vector3(mainSpan * 0.1, parabolaY(mainSpan * 0.1) + 0.6, 0), 'ARCH', 'M4-Left');

    addSensorMarker(new THREE.Vector3(-1 - abutmentWidth / 2 + 0.5, abutmentHeight + 0.3, 0), 'PIER', 'P1-AbutmentL');
    addSensorMarker(new THREE.Vector3(mainSpan + 1 + abutmentWidth / 2 - 0.5, abutmentHeight + 0.3, 0), 'PIER', 'P2-AbutmentR');

    smallArches.forEach(function(sa, i) {
        var sax = sa.xCenter;
        var say = parabolaY(sax) + sa.rise + 0.4;
        addSensorMarker(new THREE.Vector3(sax, say, (i % 2 === 0 ? 1 : -1) * deckWidth * 0.2), 'SARCH', 'S' + (i + 1) + '-SmallArch');
    });

    addSensorMarker(new THREE.Vector3(mainSpan * 0.35, parabolaY(mainSpan * 0.35) + 1.5, deckWidth * 0.3), 'CRACK', 'C1-SpandrelL');
    addSensorMarker(new THREE.Vector3(mainSpan * 0.65, parabolaY(mainSpan * 0.65) + 1.5, -deckWidth * 0.3), 'CRACK', 'C2-SpandrelR');

    var crackPositions = [
        { x: mainSpan * 0.15, angle: -0.3, z: 0.1 },
        { x: mainSpan * 0.85, angle: 0.3, z: -0.1 },
        { x: mainSpan * 0.3, angle: -0.15, z: deckWidth * 0.2 },
        { x: mainSpan * 0.7, angle: 0.15, z: -deckWidth * 0.2 }
    ];

    crackPositions.forEach(function(cp) {
        var crackGeo = new THREE.PlaneGeometry(1.8, 0.08);
        var crackMat = new THREE.MeshBasicMaterial({
            color: 0xff2020,
            transparent: true,
            opacity: 0.9,
            side: THREE.DoubleSide,
            depthWrite: false
        });
        var crack = new THREE.Mesh(crackGeo, crackMat);
        var cy = parabolaY(cp.x);
        crack.position.set(cp.x, cy + 0.55, cp.z);
        crack.rotation.z = cp.angle;
        crack.rotation.y = cp.z !== 0 ? (cp.z > 0 ? -0.3 : 0.3) : 0;
        crack.visible = false;
        crack.scale.set(0.01, 0.01, 0.01);
        group.add(crack);
        crackMarkers.push(crack);
    });

    return group;
}

function setupGroundAndEnvironment(bridgeGroup) {
    var gridHelper = new THREE.GridHelper(200, 100, 0x555555, 0x333333);
    gridHelper.position.y = 0.001;
    gridHelper.material.opacity = 0.15;
    gridHelper.material.transparent = true;
    scene.add(gridHelper);

    scene.fog.color = new THREE.Color(0x1a2530);
}

function updateStressMap(femStressResults, femElements, femNodes) {
    while (stressOverlayGroup.children.length > 0) {
        var obj = stressOverlayGroup.children[0];
        stressOverlayGroup.remove(obj);
        if (obj.geometry) obj.geometry.dispose();
        if (obj.material) obj.material.dispose();
    }

    if (!femStressResults || !femElements || !femNodes || femElements.length === 0) return;

    var minStress = Infinity, maxStress = -Infinity;
    for (var i = 0; i < femStressResults.length; i++) {
        var s = femStressResults[i];
        if (s < minStress) minStress = s;
        if (s > maxStress) maxStress = s;
    }
    if (maxStress === minStress) maxStress = minStress + 1;

    stressData = { min: minStress, max: maxStress, values: femStressResults };

    var highStressElements = [];

    for (var ei = 0; ei < femElements.length; ei++) {
        var elem = femElements[ei];
        var stress = femStressResults[ei] !== undefined ? femStressResults[ei] : 0;
        var n0 = femNodes[elem[0]];
        var n1 = femNodes[elem[1]];
        var n2 = femNodes[elem[2]];
        if (!n0 || !n1 || !n2) continue;

        var shape = new THREE.Shape();
        var abutH = 4.0;
        var zOffset = 0.05;

        var sx0 = n0.x, sy0 = n0.y + abutH;
        var sx1 = n1.x, sy1 = n1.y + abutH;
        var sx2 = n2.x, sy2 = n2.y + abutH;

        shape.moveTo(sx0, sy0);
        shape.lineTo(sx1, sy1);
        shape.lineTo(sx2, sy2);
        shape.lineTo(sx0, sy0);

        var shapeGeo = new THREE.ShapeGeometry(shape);
        var color = window.stressColorMap(stress, minStress, maxStress);
        var meshMat = new THREE.MeshBasicMaterial({
            color: color,
            transparent: true,
            opacity: 0.5,
            side: THREE.DoubleSide,
            depthWrite: false,
            polygonOffset: true,
            polygonOffsetFactor: -1,
            polygonOffsetUnits: -1
        });

        var posAttr = shapeGeo.attributes.position;
        for (var vi = 0; vi < posAttr.count; vi++) {
            posAttr.setZ(vi, zOffset);
        }
        posAttr.needsUpdate = true;

        var mesh = new THREE.Mesh(shapeGeo, meshMat);
        mesh.rotation.x = -Math.PI / 2;
        stressOverlayGroup.add(mesh);

        highStressElements.push({ index: ei, stress: stress, centroid: window.computeElementCentroid(elem, femNodes) });
    }

    highStressElements.sort(function(a, b) { return b.stress - a.stress; });
    var topCount = Math.ceil(highStressElements.length * 0.2);
    for (var ti = 0; ti < topCount; ti++) {
        var hse = highStressElements[ti];
        var c = hse.centroid;
        var labelText = (hse.stress / 1e6).toFixed(2) + 'MPa';
        var sprite = window.createTextSprite(labelText, 0xffffff, 24);
        sprite.position.set(c.x, c.y + 4.0 + 0.5, 0.2);
        sprite.scale.set(2.0, 1.0, 1.0);
        stressOverlayGroup.add(sprite);
    }

    var legendParent = document.querySelector('.monitoring-panel, .dashboard-container, body');
    if (legendParent && !document.getElementById('stress-legend')) {
        window.createStressLegend(legendParent);
    }
    if (document.getElementById('stress-legend')) {
        window.updateStressLegend(minStress, maxStress);
    }

    stressOverlayGroup.visible = stressVisible;
}

function setStressMapVisible(visible) {
    stressVisible = visible;
    if (stressOverlayGroup) {
        stressOverlayGroup.visible = visible;
    }
}

function setCrackMarkersVisible(visible) {
    cracksVisible = visible;
    var duration = 600;
    var startTime = performance.now();

    function animateCracks(now) {
        var elapsed = now - startTime;
        var t = Math.min(elapsed / duration, 1);
        var easeT = visible ? (1 - Math.pow(1 - t, 3)) : (1 - Math.pow(t, 3));
        var scale = visible ? easeT : (1 - easeT);

        crackMarkers.forEach(function(crack) {
            crack.visible = visible || scale > 0.01;
            crack.scale.set(visible ? Math.max(0.01, scale) : scale, visible ? Math.max(0.01, scale) : scale, Math.max(0.01, scale));
        });

        if (t < 1) {
            requestAnimationFrame(animateCracks);
        } else {
            crackMarkers.forEach(function(crack) {
                crack.visible = visible;
                if (visible) crack.scale.set(1, 1, 1);
            });
        }
    }
    requestAnimationFrame(animateCracks);
}

function animateDeformation50Years(deformationPredictions, femNodes) {
    if (!bridgeGroup || !originalPositions || !femNodes) return;

    var duration = 6000;
    var startTime = performance.now();
    var amplify = 500;
    var abutH = 4.0;

    var infoDiv = document.getElementById('deformation-info');
    if (!infoDiv) {
        infoDiv = document.createElement('div');
        infoDiv.id = 'deformation-info';
        infoDiv.style.cssText = 'position:absolute;top:10px;left:10px;background:rgba(0,0,0,0.75);color:#fff;padding:12px 18px;border-radius:6px;font-family:Arial;font-size:13px;z-index:20;pointer-events:none;';
        var container = document.getElementById('bridge-3d-container') || document.body;
        container.appendChild(infoDiv);
    }

    var mainArch = null;
    for (var bi = 0; bi < bridgeGroup.children.length; bi++) {
        var child = bridgeGroup.children[bi];
        if (child.geometry && child.geometry.type === 'TubeGeometry') {
            if (child.geometry.parameters && child.geometry.parameters.tubularSegments === 80) {
                mainArch = child;
                break;
            }
        }
    }
    if (!mainArch) {
        mainArch = bridgeGroup.children[0];
    }

    function animStep(now) {
        var elapsed = now - startTime;
        var progress = Math.min(elapsed / duration, 1);
        var easeProgress = 1 - Math.pow(1 - progress, 3);

        var geo = mainArch.geometry;
        var posAttr = geo.attributes.position;
        var positions = posAttr.array;

        for (var vi = 0; vi < posAttr.count; vi++) {
            var ox = originalPositions[vi * 3];
            var oy = originalPositions[vi * 3 + 1];
            var oz = originalPositions[vi * 3 + 2];

            var nodeY = oy - abutH;
            var interp = window.interpolateNodeDisplacements(ox, nodeY, femNodes, deformationPredictions);
            var dx = interp.dx * amplify * easeProgress;
            var dy = interp.dy * amplify * easeProgress;

            positions[vi * 3] = ox + dx;
            positions[vi * 3 + 1] = oy + dy;
            positions[vi * 3 + 2] = oz;
        }
        posAttr.needsUpdate = true;
        geo.computeVertexNormals();

        var maxDisp = 0;
        if (deformationPredictions && deformationPredictions.length > 0) {
            for (var di = 0; di < deformationPredictions.length; di++) {
                var pred = deformationPredictions[di];
                var d = Math.sqrt(pred.dx * pred.dx + pred.dy * pred.dy);
                if (d > maxDisp) maxDisp = d;
            }
        }
        var year = (progress * 50).toFixed(1);
        var dispMm = (maxDisp * 1000 * easeProgress).toFixed(3);
        infoDiv.innerHTML = '<strong>50-Year Deformation Simulation</strong><br>Year: ' + year + ' / 50<br>Max displacement: ' + dispMm + ' mm<br>Visual exaggeration: &times;' + amplify + '<br>Progress: ' + (progress * 100).toFixed(0) + '%';

        if (progress < 1) {
            requestAnimationFrame(animStep);
        } else {
            deformed = true;
            infoDiv.style.pointerEvents = 'auto';
            var resetBtn = document.createElement('button');
            resetBtn.textContent = 'Reset Deformation';
            resetBtn.style.cssText = 'display:block;margin-top:8px;padding:6px 12px;background:#2196F3;color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:12px;';
            resetBtn.onclick = function() {
                resetView();
                infoDiv.innerHTML = '<strong>50-Year Deformation Simulation</strong><br>Completed - View reset.';
                setTimeout(function() { if (infoDiv.parentNode) infoDiv.parentNode.removeChild(infoDiv); }, 2500);
            };
            var existingBtn = infoDiv.querySelector('button');
            if (!existingBtn) infoDiv.appendChild(resetBtn);
        }
    }
    requestAnimationFrame(animStep);
}

function resetView() {
    if (camera) {
        camera.position.set(50, 30, 60);
    }
    if (controls) {
        controls.target.set(18.5, 4, 0);
        controls.update();
    }

    if (deformed && originalPositions && bridgeGroup) {
        var mainArch = null;
        for (var bi = 0; bi < bridgeGroup.children.length; bi++) {
            var child = bridgeGroup.children[bi];
            if (child.geometry && child.geometry.type === 'TubeGeometry') {
                if (child.geometry.parameters && child.geometry.parameters.tubularSegments === 80) {
                    mainArch = child;
                    break;
                }
            }
        }
        if (!mainArch) mainArch = bridgeGroup.children[0];

        if (mainArch) {
            var geo = mainArch.geometry;
            var posAttr = geo.attributes.position;
            var positions = posAttr.array;
            for (var i = 0; i < originalPositions.length; i++) {
                positions[i] = originalPositions[i];
            }
            posAttr.needsUpdate = true;
            geo.computeVertexNormals();
        }
    }

    deformed = false;

    var infoDiv = document.getElementById('deformation-info');
    if (infoDiv && infoDiv.parentNode) {
        infoDiv.parentNode.removeChild(infoDiv);
    }
}

function setAutoRotate(enabled) {
    autoRotate = enabled;
    if (controls) {
        controls.autoRotate = enabled;
        controls.autoRotateSpeed = 0.8;
    }
}

function animate() {
    requestAnimationFrame(animate);
    if (controls) controls.update();

    var time = Date.now() * 0.001;
    sensorMarkers.forEach(function(sensor, idx) {
        if (sensor.userData && sensor.userData.halo) {
            var pulse = 1 + 0.3 * Math.sin(time * 2 + idx * 0.7);
            sensor.userData.halo.scale.set(1.2 * pulse, 1.2 * pulse, 1.2 * pulse);
        }
    });

    if (renderer && scene && camera) {
        renderer.render(scene, camera);
    }
}

function onWindowResize() {
    var container = document.getElementById('bridge-3d-container');
    if (!container || !camera || !renderer) return;
    camera.aspect = container.clientWidth / container.clientHeight;
    camera.updateProjectionMatrix();
    renderer.setSize(container.clientWidth, container.clientHeight);
}
