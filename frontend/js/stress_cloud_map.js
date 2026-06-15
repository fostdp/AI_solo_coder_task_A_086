window.stressColorMap = function(value, minVal, maxVal) {
    var range = maxVal - minVal;
    var normalized = range > 0 ? (value - minVal) / range : 0.5;
    normalized = Math.max(0, Math.min(1, normalized));

    var c1, c2, t;

    if (normalized < 0.2) {
        c1 = new THREE.Color(0x0000ff);
        c2 = new THREE.Color(0x00ffff);
        t = normalized / 0.2;
    } else if (normalized < 0.4) {
        c1 = new THREE.Color(0x00ffff);
        c2 = new THREE.Color(0x00ff00);
        t = (normalized - 0.2) / 0.2;
    } else if (normalized < 0.6) {
        c1 = new THREE.Color(0x00ff00);
        c2 = new THREE.Color(0xffff00);
        t = (normalized - 0.4) / 0.2;
    } else if (normalized < 0.8) {
        c1 = new THREE.Color(0xffff00);
        c2 = new THREE.Color(0xff8800);
        t = (normalized - 0.6) / 0.2;
    } else {
        c1 = new THREE.Color(0xff8800);
        c2 = new THREE.Color(0xff0000);
        t = normalized < 1 ? (normalized - 0.8) / 0.2 : 1;
    }

    var result = new THREE.Color();
    result.r = c1.r + (c2.r - c1.r) * t;
    result.g = c1.g + (c2.g - c1.g) * t;
    result.b = c1.b + (c2.b - c1.b) * t;
    return result;
};

window.createStressLegend = function(parentDiv) {
    if (!parentDiv) parentDiv = document.body;

    var legend = document.createElement('div');
    legend.id = 'stress-legend';
    legend.style.cssText = 'position:absolute;right:20px;bottom:20px;z-index:10;background:rgba(15,15,20,0.85);padding:14px 18px;border-radius:8px;border:1px solid rgba(255,255,255,0.15);box-shadow:0 4px 16px rgba(0,0,0,0.4);font-family:Arial,sans-serif;color:#fff;user-select:none;';

    var title = document.createElement('div');
    title.style.cssText = 'font-size:12px;font-weight:600;margin-bottom:10px;text-align:center;letter-spacing:0.5px;';
    title.textContent = 'von Mises (Pa)';
    legend.appendChild(title);

    var barContainer = document.createElement('div');
    barContainer.style.cssText = 'display:flex;align-items:flex-start;gap:10px;';

    var labelsCol = document.createElement('div');
    labelsCol.style.cssText = 'display:flex;flex-direction:column;justify-content:space-between;height:200px;font-size:11px;color:#ddd;width:48px;text-align:right;';

    var maxLabel = document.createElement('div');
    maxLabel.id = 'stress-legend-max';
    maxLabel.textContent = 'Max';
    maxLabel.style.cssText = 'color:#ff6666;font-weight:600;';

    var highLabel = document.createElement('div');
    highLabel.textContent = 'High';
    highLabel.style.cssText = 'color:#ffcc66;';

    var medLabel = document.createElement('div');
    medLabel.textContent = 'Medium';
    medLabel.style.cssText = 'color:#99ff66;';

    var lowLabel = document.createElement('div');
    lowLabel.textContent = 'Low';
    lowLabel.style.cssText = 'color:#66ccff;';

    var minLabel = document.createElement('div');
    minLabel.id = 'stress-legend-min';
    minLabel.textContent = 'Min';
    minLabel.style.cssText = 'color:#6688ff;font-weight:600;';

    labelsCol.appendChild(maxLabel);
    labelsCol.appendChild(highLabel);
    labelsCol.appendChild(medLabel);
    labelsCol.appendChild(lowLabel);
    labelsCol.appendChild(minLabel);

    var barCol = document.createElement('div');
    barCol.style.cssText = 'position:relative;';

    var bar = document.createElement('div');
    bar.style.cssText = 'width:30px;height:200px;border-radius:4px;border:1px solid rgba(255,255,255,0.25);background:linear-gradient(to bottom, #ff0000 0%, #ff8800 25%, #ffff00 50%, #00ff00 75%, #00ffff 90%, #0000ff 100%);box-shadow:inset 0 0 6px rgba(0,0,0,0.3);';
    barCol.appendChild(bar);

    var valCol = document.createElement('div');
    valCol.style.cssText = 'display:flex;flex-direction:column;justify-content:space-between;height:200px;font-size:10px;color:#bbb;width:60px;';

    var maxVal = document.createElement('div');
    maxVal.id = 'stress-legend-max-val';
    maxVal.textContent = '---';

    var highVal = document.createElement('div');
    highVal.id = 'stress-legend-high-val';
    highVal.textContent = '---';

    var medVal = document.createElement('div');
    medVal.id = 'stress-legend-med-val';
    medVal.textContent = '---';

    var lowVal = document.createElement('div');
    lowVal.id = 'stress-legend-low-val';
    lowVal.textContent = '---';

    var minVal = document.createElement('div');
    minVal.id = 'stress-legend-min-val';
    minVal.textContent = '---';

    valCol.appendChild(maxVal);
    valCol.appendChild(highVal);
    valCol.appendChild(medVal);
    valCol.appendChild(lowVal);
    valCol.appendChild(minVal);

    barContainer.appendChild(labelsCol);
    barContainer.appendChild(barCol);
    barContainer.appendChild(valCol);
    legend.appendChild(barContainer);

    parentDiv.appendChild(legend);
    return legend;
};

window.updateStressLegend = function(minVal, maxVal) {
    var minEl = document.getElementById('stress-legend-min-val');
    var maxEl = document.getElementById('stress-legend-max-val');
    var lowEl = document.getElementById('stress-legend-low-val');
    var medEl = document.getElementById('stress-legend-med-val');
    var highEl = document.getElementById('stress-legend-high-val');

    function formatVal(v) {
        if (v === undefined || v === null || isNaN(v)) return '---';
        var abs = Math.abs(v);
        if (abs >= 1e9) return (v / 1e9).toFixed(2) + ' GPa';
        if (abs >= 1e6) return (v / 1e6).toFixed(2) + ' MPa';
        if (abs >= 1e3) return (v / 1e3).toFixed(2) + ' kPa';
        return v.toFixed(1) + ' Pa';
    }

    if (minEl) minEl.textContent = formatVal(minVal);
    if (maxEl) maxEl.textContent = formatVal(maxVal);

    if (lowEl && minVal !== undefined && maxVal !== undefined) {
        lowEl.textContent = formatVal(minVal + (maxVal - minVal) * 0.25);
    }
    if (medEl && minVal !== undefined && maxVal !== undefined) {
        medEl.textContent = formatVal(minVal + (maxVal - minVal) * 0.5);
    }
    if (highEl && minVal !== undefined && maxVal !== undefined) {
        highEl.textContent = formatVal(minVal + (maxVal - minVal) * 0.75);
    }
};

window.createTextSprite = function(text, color, fontSize) {
    if (!fontSize) fontSize = 32;
    if (!color) color = 0xffffff;

    var canvas = document.createElement('canvas');
    var ctx = canvas.getContext('2d');
    var font = 'Arial';

    ctx.font = 'bold ' + fontSize + 'px ' + font;
    var metrics = ctx.measureText(text);
    var textWidth = metrics.width;
    var textHeight = fontSize * 1.4;

    var padding = 12;
    canvas.width = textWidth + padding * 2;
    canvas.height = textHeight + padding * 2;

    ctx = canvas.getContext('2d');
    ctx.font = 'bold ' + fontSize + 'px ' + font;
    ctx.textBaseline = 'middle';

    ctx.fillStyle = 'rgba(0,0,0,0.7)';
    var radius = 6;
    var w = canvas.width;
    var h = canvas.height;
    ctx.beginPath();
    ctx.moveTo(radius, 0);
    ctx.lineTo(w - radius, 0);
    ctx.quadraticCurveTo(w, 0, w, radius);
    ctx.lineTo(w, h - radius);
    ctx.quadraticCurveTo(w, h, w - radius, h);
    ctx.lineTo(radius, h);
    ctx.quadraticCurveTo(0, h, 0, h - radius);
    ctx.lineTo(0, radius);
    ctx.quadraticCurveTo(0, 0, radius, 0);
    ctx.closePath();
    ctx.fill();

    ctx.strokeStyle = 'rgba(255,255,255,0.2)';
    ctx.lineWidth = 1;
    ctx.stroke();

    var r = ((color >> 16) & 255) / 255;
    var g = ((color >> 8) & 255) / 255;
    var b = (color & 255) / 255;
    ctx.fillStyle = 'rgb(' + Math.round(r * 255) + ',' + Math.round(g * 255) + ',' + Math.round(b * 255) + ')';
    ctx.fillText(text, padding, canvas.height / 2);

    var texture = new THREE.CanvasTexture(canvas);
    texture.minFilter = THREE.LinearFilter;
    texture.magFilter = THREE.LinearFilter;
    texture.needsUpdate = true;

    var material = new THREE.SpriteMaterial({
        map: texture,
        transparent: true,
        depthWrite: false
    });

    var sprite = new THREE.Sprite(material);
    var aspect = canvas.width / canvas.height;
    var baseSize = 3;
    sprite.scale.set(baseSize * aspect, baseSize, 1);

    return sprite;
};

window.computeElementCentroid = function(element, nodes) {
    if (!element || !nodes) return { x: 0, y: 0, z: 0 };

    var n0 = nodes[element[0]] || { x: 0, y: 0, z: 0 };
    var n1 = nodes[element[1]] || { x: 0, y: 0, z: 0 };
    var n2 = nodes[element[2]] || { x: 0, y: 0, z: 0 };

    return {
        x: (n0.x + n1.x + n2.x) / 3,
        y: (n0.y + n1.y + n2.y) / 3,
        z: ((n0.z || 0) + (n1.z || 0) + (n2.z || 0)) / 3
    };
};

window.findNearestFEMNode = function(x, y, femNodes) {
    if (!femNodes || femNodes.length === 0) return -1;

    var bestIdx = 0;
    var bestDist = Infinity;

    for (var i = 0; i < femNodes.length; i++) {
        var node = femNodes[i];
        if (!node) continue;

        var dx = node.x - x;
        var dy = node.y - y;
        var distSq = dx * dx + dy * dy;

        if (distSq < bestDist) {
            bestDist = distSq;
            bestIdx = i;
        }
    }

    return bestIdx;
};

window.interpolateNodeDisplacements = function(x, y, femNodes, predictions) {
    if (!femNodes || !predictions || femNodes.length === 0) {
        return { dx: 0, dy: 0 };
    }

    var k = Math.min(4, femNodes.length);
    var candidates = [];

    for (var i = 0; i < femNodes.length; i++) {
        var node = femNodes[i];
        if (!node) continue;

        var dx = node.x - x;
        var dy = node.y - y;
        var distSq = dx * dx + dy * dy;

        candidates.push({
            index: i,
            distSq: distSq,
            dist: Math.sqrt(distSq)
        });
    }

    candidates.sort(function(a, b) { return a.distSq - b.distSq; });
    var nearest = candidates.slice(0, k);

    var sumWeights = 0;
    var sumDx = 0;
    var sumDy = 0;
    var power = 2;
    var epsilon = 1e-8;

    for (var j = 0; j < nearest.length; j++) {
        var c = nearest[j];
        var pred = predictions[c.index];

        if (!pred) continue;

        if (c.dist < epsilon) {
            return { dx: pred.dx || 0, dy: pred.dy || 0 };
        }

        var weight = 1.0 / Math.pow(c.dist, power);
        sumWeights += weight;
        sumDx += weight * (pred.dx || 0);
        sumDy += weight * (pred.dy || 0);
    }

    if (sumWeights < epsilon) {
        var firstPred = predictions[nearest[0].index];
        if (firstPred) {
            return { dx: firstPred.dx || 0, dy: firstPred.dy || 0 };
        }
        return { dx: 0, dy: 0 };
    }

    return {
        dx: sumDx / sumWeights,
        dy: sumDy / sumWeights
    };
};
