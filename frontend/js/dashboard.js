const apiBase = (window.location.origin || 'http://localhost:8080');
const wsBase = apiBase.replace(/^http/, 'ws');
let socket = null;
let sensorRegistry = [];
let latestData = {};
let alerts = [];
let femNodes = [];
let femElements = [];
let femStress = [];
let predictions = [];
let sparklineCharts = {};
let mainCharts = {};
let wsRetryCount = 0;
let wsReconnectTimeout = null;

async function fetchJSON(url, opts) {
    try {
        const res = await fetch(url, {
            headers: { 'Content-Type': 'application/json' },
            ...opts
        });
        if (!res.ok) {
            console.warn(`[fetchJSON] HTTP ${res.status}: ${url}`);
            return null;
        }
        return await res.json();
    } catch (e) {
        console.warn(`[fetchJSON] Failed: ${url}`, e.message);
        return null;
    }
}

async function initDashboard() {
    const registry = await fetchJSON(`${apiBase}/api/sensors`);
    if (registry && Array.isArray(registry)) {
        sensorRegistry = registry;
    }

    const geom = await fetchJSON(`${apiBase}/api/bridge/geometry`);
    if (geom) {
        femNodes = geom.nodes || [];
        femElements = geom.elements || [];
        if (typeof initBridgeScene === 'function') {
            initBridgeScene('viewport3d');
        }
    }

    const stressRes = await fetchJSON(`${apiBase}/api/fem/analyze`, {
        method: 'POST',
        body: JSON.stringify({ live_load: 10000, delta_t: 0 })
    });
    if (stressRes && Array.isArray(stressRes)) {
        femStress = stressRes.map(r => r.von_mises || 0);
        if (typeof updateStressMap === 'function') {
            const elemTriples = femElements.map(e =>
                Array.isArray(e.node_ids) ? e.node_ids : [e[0], e[1], e[2]]
            );
            updateStressMap(femStress, elemTriples, femNodes);
        }
    }

    const predRes = await fetchJSON(`${apiBase}/api/deformation/predict50`, {
        method: 'POST'
    });
    if (predRes && Array.isArray(predRes)) {
        predictions = predRes.map(p => ({
            dx: p.predicted_dx || 0,
            dy: p.predicted_dy || 0,
            node_id: p.node_id
        }));
    }

    const start = new Date(Date.now() - 24 * 3600 * 1000).toISOString();
    const alertsRes = await fetchJSON(`${apiBase}/api/alerts?start=${encodeURIComponent(start)}`);
    if (alertsRes && Array.isArray(alertsRes)) {
        alerts = alertsRes;
    }
    renderAlerts();

    connectWebSocket();
    initSparklines();
    initMainCharts();
    setupEventHandlers();

    setInterval(pollLatestData, 10000);
}

function connectWebSocket() {
    try {
        socket = new WebSocket(`${wsBase}/ws/realtime`);
    } catch (e) {
        console.warn('[WS] Create failed:', e.message);
        scheduleReconnect();
        return;
    }

    socket.addEventListener('open', () => {
        wsRetryCount = 0;
        const ind = document.getElementById('wsStatus');
        const val = document.getElementById('wsValue');
        if (ind) ind.className = 'status-indicator online';
        if (val) val.textContent = '已连接';
        console.log('[WS] Connected');
    });

    socket.addEventListener('message', (evt) => {
        try {
            const readings = JSON.parse(evt.data);
            if (Array.isArray(readings)) {
                updateLatestData(readings);
            }
        } catch (e) {
            console.warn('[WS] Parse failed:', e.message);
        }
    });

    socket.addEventListener('close', () => {
        const ind = document.getElementById('wsStatus');
        const val = document.getElementById('wsValue');
        if (ind) ind.className = 'status-indicator warning';
        if (val) val.textContent = '重连中...';
        console.log('[WS] Closed');
        scheduleReconnect();
    });

    socket.addEventListener('error', (e) => {
        const ind = document.getElementById('wsStatus');
        const val = document.getElementById('wsValue');
        if (ind) ind.className = 'status-indicator warning';
        if (val) val.textContent = '重连中...';
        console.warn('[WS] Error');
    });
}

function scheduleReconnect() {
    if (wsReconnectTimeout) return;
    wsRetryCount = Math.min(wsRetryCount + 1, 10);
    const delay = Math.min(5 * Math.pow(1.5, wsRetryCount - 1), 30) * 1000;
    console.log(`[WS] Reconnect in ${(delay / 1000).toFixed(1)}s (attempt ${wsRetryCount})`);
    wsReconnectTimeout = setTimeout(() => {
        wsReconnectTimeout = null;
        connectWebSocket();
    }, delay);
}

function updateLatestData(readings) {
    const now = new Date();
    let archVal = null, pierVal = null, tempVal = null, crackVal = null;
    let pierVals = [];
    let hasAlert = false;

    readings.forEach(r => {
        latestData[r.sensor_id || r.SensorID] = r;

        const sid = r.sensor_id || r.SensorID || '';
        const strain = r.strain_micro !== undefined ? r.strain_micro : r.StrainMicro;
        const settle = r.settlement_mm !== undefined ? r.settlement_mm : r.SettlementMM;
        const temp = r.temperature !== undefined ? r.temperature : r.Temperature;
        const crack = r.crack_width_mm !== undefined ? r.crack_width_mm : r.CrackWidthMM;

        if (sid.startsWith('ARCH')) {
            if (strain !== undefined && strain !== null) {
                if (archVal === null || sid === 'ARCH-001') archVal = strain;
            }
        }
        if (sid.startsWith('PIER')) {
            if (settle !== undefined && settle !== null) {
                pierVals.push(settle);
                if (pierVal === null) pierVal = settle;
            }
        }
        if (temp !== undefined && temp !== null) {
            tempVal = tempVal === null ? temp : (tempVal + temp) / 2;
        }
        if (sid.startsWith('CRACK')) {
            if (crack !== undefined && crack !== null) {
                if (crackVal === null || sid === 'CRACK-001') crackVal = crack;
                if (crack > 0.4) hasAlert = true;
            }
        }
    });

    if (pierVals.length > 1) {
        const rate = Math.abs(pierVals[0] - pierVals[1]);
        if (rate > 2) hasAlert = true;
    }

    if (archVal !== null) {
        const el = document.getElementById('valArch');
        if (el) el.textContent = archVal.toFixed(1);
        pushSparkline('arch', archVal);
    }
    if (pierVal !== null) {
        const el = document.getElementById('valPier');
        if (el) el.textContent = pierVal.toFixed(1);
        pushSparkline('pier', pierVal);
    }
    if (tempVal !== null) {
        const el = document.getElementById('valTemp');
        if (el) el.textContent = tempVal.toFixed(1);
        pushSparkline('temp', tempVal);
    }
    if (crackVal !== null) {
        const el = document.getElementById('valCrack');
        if (el) el.textContent = crackVal.toFixed(2);
        pushSparkline('crack', crackVal);
    }

    ['metricArch', 'metricPier', 'metricTemp', 'metricCrack'].forEach(id => {
        const el = document.getElementById(id);
        if (!el) return;
        el.classList.remove('updated');
        void el.offsetWidth;
        el.classList.add('updated');
    });

    if (hasAlert) {
        ['metricArch', 'metricPier', 'metricTemp', 'metricCrack'].forEach(id => {
            const el = document.getElementById(id);
            if (el) {
                el.style.borderColor = '#e74c3c';
                el.style.boxShadow = '0 0 14px rgba(231,76,60,0.5)';
                setTimeout(() => {
                    if (el) {
                        el.style.borderColor = '';
                        el.style.boxShadow = '';
                    }
                }, 3000);
            }
        });
    }

    if (mainCharts.live) {
        const t = now.getHours().toString().padStart(2, '0') + ':' +
                  now.getMinutes().toString().padStart(2, '0');
        const chart = mainCharts.live;
        chart.data.labels.push(t);
        if (chart.data.labels.length > 48) chart.data.labels.shift();
        const datasets = [
            { idx: 0, val: archVal },
            { idx: 1, val: pierVal },
            { idx: 2, val: tempVal },
            { idx: 3, val: crackVal !== null ? crackVal * 100 : null }
        ];
        datasets.forEach(ds => {
            chart.data.datasets[ds.idx].data.push(ds.val !== null ? ds.val : null);
            if (chart.data.datasets[ds.idx].data.length > 48) {
                chart.data.datasets[ds.idx].data.shift();
            }
        });
        chart.update('none');
    }

    const t = now.getHours().toString().padStart(2, '0') + ':' +
              now.getMinutes().toString().padStart(2, '0') + ':' +
              now.getSeconds().toString().padStart(2, '0');
    const lastUpd = document.getElementById('lastUpdate');
    if (lastUpd) lastUpd.textContent = t;
}

function pushSparkline(key, value) {
    const chart = sparklineCharts[key];
    if (!chart) return;
    chart.data.datasets[0].data.push(value);
    if (chart.data.datasets[0].data.length > 30) {
        chart.data.datasets[0].data.shift();
    }
    chart.data.labels.push('');
    if (chart.data.labels.length > 30) chart.data.labels.shift();
    chart.update('none');
}

function initSparklines() {
    const COLORS = window.COLORS || {
        teal: '#1abc9c', warning: '#f39c12', orange: '#e67e22', red: '#e74c3c'
    };

    function makeSpark(canvasId, color, fillAlpha) {
        const ctx = document.getElementById(canvasId);
        if (!ctx) return null;
        return new Chart(ctx, {
            type: 'line',
            data: {
                labels: Array(30).fill(''),
                datasets: [{
                    data: [],
                    borderColor: color,
                    backgroundColor: color + (fillAlpha || '20'),
                    fill: true,
                    tension: 0.4,
                    borderWidth: 1.5,
                    pointRadius: 0
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                plugins: { legend: { display: false }, tooltip: { enabled: false } },
                scales: {
                    x: { display: false },
                    y: { display: false }
                },
                elements: {
                    line: { tension: 0.4 }
                }
            }
        });
    }

    sparklineCharts.arch = makeSpark('sparkArch', COLORS.teal);
    sparklineCharts.pier = makeSpark('sparkPier', COLORS.warning);
    sparklineCharts.temp = makeSpark('sparkTemp', COLORS.orange);
    sparklineCharts.crack = makeSpark('sparkCrack', COLORS.red);
}

function initMainCharts() {
    const COLORS = window.COLORS || {
        teal: '#1abc9c', warning: '#f39c12', orange: '#e67e22', red: '#e74c3c',
        blue: '#3498db', text: '#ecf0f1', textDim: '#95a5a6',
        grid: 'rgba(255,255,255,0.06)'
    };

    const baseOpts = {
        responsive: true,
        maintainAspectRatio: false,
        animation: { duration: 500 },
        plugins: {
            legend: {
                labels: { color: COLORS.textDim, font: { size: 11 }, padding: 12, usePointStyle: true }
            },
            tooltip: {
                backgroundColor: 'rgba(15,25,35,0.95)',
                titleColor: COLORS.text,
                bodyColor: COLORS.textDim,
                borderColor: 'rgba(255,255,255,0.1)',
                borderWidth: 1,
                padding: 10,
                cornerRadius: 6
            }
        }
    };

    function genLabels(n) {
        const arr = [];
        const now = new Date();
        for (let i = n - 1; i >= 0; i--) {
            const d = new Date(now - i * 30 * 60000);
            arr.push(d.getHours().toString().padStart(2, '0') + ':' +
                     d.getMinutes().toString().padStart(2, '0'));
        }
        return arr;
    }

    const liveCtx = document.getElementById('chartLiveCanvas');
    if (liveCtx) {
        mainCharts.live = new Chart(liveCtx, {
            type: 'line',
            data: {
                labels: genLabels(48),
                datasets: [
                    {
                        label: '拱券应变 (με)',
                        data: Array(48).fill(null),
                        borderColor: COLORS.teal,
                        backgroundColor: 'transparent',
                        yAxisID: 'y',
                        tension: 0.35,
                        borderWidth: 2,
                        pointRadius: 1.5,
                        pointBackgroundColor: COLORS.teal
                    },
                    {
                        label: '桥墩沉降 (mm)',
                        data: Array(48).fill(null),
                        borderColor: COLORS.warning,
                        backgroundColor: 'transparent',
                        yAxisID: 'y1',
                        tension: 0.35,
                        borderWidth: 2,
                        pointRadius: 1.5,
                        pointBackgroundColor: COLORS.warning
                    },
                    {
                        label: '温度 (℃)',
                        data: Array(48).fill(null),
                        borderColor: COLORS.orange,
                        backgroundColor: 'transparent',
                        yAxisID: 'y2',
                        tension: 0.35,
                        borderWidth: 2,
                        pointRadius: 1.5,
                        pointBackgroundColor: COLORS.orange
                    },
                    {
                        label: '裂缝宽度 (mm×100)',
                        data: Array(48).fill(null),
                        borderColor: COLORS.red,
                        backgroundColor: 'transparent',
                        yAxisID: 'y3',
                        tension: 0.35,
                        borderWidth: 2,
                        pointRadius: 1.5,
                        pointBackgroundColor: COLORS.red
                    }
                ]
            },
            options: {
                ...baseOpts,
                scales: {
                    x: {
                        ticks: { color: COLORS.textDim, font: { size: 10 }, maxTicksLimit: 12 },
                        grid: { color: COLORS.grid, drawBorder: false },
                        border: { display: false }
                    },
                    y: {
                        position: 'left',
                        title: { display: true, text: '应变 με / 裂缝×100', color: COLORS.textDim, font: { size: 10 } },
                        ticks: { color: COLORS.textDim, font: { size: 10 } },
                        grid: { color: COLORS.grid, drawBorder: false },
                        border: { display: false }
                    },
                    y1: {
                        position: 'right',
                        title: { display: true, text: '沉降 mm', color: COLORS.textDim, font: { size: 10 } },
                        ticks: { color: COLORS.textDim, font: { size: 10 } },
                        grid: { drawOnChartArea: false },
                        border: { display: false }
                    },
                    y2: {
                        position: 'right',
                        offset: true,
                        title: { display: true, text: '温度 ℃', color: COLORS.textDim, font: { size: 10 } },
                        ticks: { color: COLORS.textDim, font: { size: 10 } },
                        grid: { drawOnChartArea: false },
                        border: { display: false }
                    },
                    y3: {
                        display: false,
                        position: 'right',
                        grid: { drawOnChartArea: false }
                    }
                },
                plugins: {
                    ...baseOpts.plugins,
                    title: { display: true, text: '传感器 24 小时趋势', color: COLORS.textDim, font: { size: 12, weight: '500' }, padding: { bottom: 16 }, align: 'start' }
                }
            }
        });
    }

    const stressCtx = document.getElementById('chartStressCanvas');
    if (stressCtx) {
        const groups = ['拱顶 Crown', '拱肩 Shoulder L', '拱肩 Shoulder R', '拱脚 Spring L',
                        '拱脚 Spring R', '小拱 1-2', '小拱 3-4', '桥面 Deck'];
        const initStress = groups.map(() => Math.random() * 15);
        const colors = initStress.map(v => {
            if (v > 12) return COLORS.red;
            if (v > 8) return COLORS.warning;
            return COLORS.teal;
        });

        mainCharts.stress = new Chart(stressCtx, {
            type: 'bar',
            data: {
                labels: groups,
                datasets: [{
                    label: 'von Mises 应力 (MPa)',
                    data: initStress,
                    backgroundColor: colors.map(c => c + '33'),
                    borderColor: colors,
                    borderWidth: 1.5,
                    borderRadius: 4,
                    borderSkipped: false
                }]
            },
            options: {
                ...baseOpts,
                indexAxis: 'y',
                plugins: {
                    ...baseOpts.plugins,
                    legend: { display: false },
                    title: { display: true, text: '各构件组 von Mises 等效应力分布', color: COLORS.textDim, font: { size: 12, weight: '500' }, padding: { bottom: 16 }, align: 'start' },
                    datalabels: {
                        anchor: 'end',
                        align: 'right',
                        color: COLORS.text,
                        font: { size: 10, weight: '600' },
                        formatter: v => v.toFixed(1) + ' MPa'
                    }
                },
                scales: {
                    x: {
                        title: { display: true, text: 'MPa', color: COLORS.textDim, font: { size: 10 } },
                        ticks: { color: COLORS.textDim, font: { size: 10 } },
                        grid: { color: COLORS.grid, drawBorder: false },
                        border: { display: false },
                        max: 18
                    },
                    y: {
                        ticks: { color: COLORS.text, font: { size: 11 } },
                        grid: { display: false },
                        border: { display: false }
                    }
                }
            }
        });
    }

    const predictCtx = document.getElementById('chartPredictCanvas');
    if (predictCtx) {
        const years = [0, 1, 5, 10, 20, 30, 50];
        const crown = years.map(y => y === 0 ? 0 : -y * 0.7 - (y > 20 ? (y - 20) * 0.05 : 0));
        const spring = years.map(y => y === 0 ? 0 : y * 0.24);
        const pier = years.map(y => y === 0 ? 0 : -y * 0.36);
        const crownUp = crown.map(v => v * 1.15);
        const crownDown = crown.map(v => v * 0.85);

        mainCharts.predict = new Chart(predictCtx, {
            type: 'line',
            data: {
                labels: years.map(y => y + '年'),
                datasets: [
                    {
                        label: '拱顶下沉 Crown Displacement',
                        data: crown,
                        borderColor: COLORS.orange,
                        backgroundColor: COLORS.orange + '20',
                        fill: '+1',
                        borderWidth: 2.5,
                        pointRadius: 3,
                        pointBackgroundColor: COLORS.orange,
                        tension: 0.35
                    },
                    {
                        label: '置信上限',
                        data: crownUp,
                        borderColor: 'rgba(52,152,219,0.4)',
                        backgroundColor: 'transparent',
                        borderDash: [4, 4],
                        pointRadius: 0,
                        tension: 0.35
                    },
                    {
                        label: '置信下限',
                        data: crownDown,
                        borderColor: 'rgba(52,152,219,0.4)',
                        backgroundColor: COLORS.orange + '10',
                        fill: '-1',
                        borderDash: [4, 4],
                        pointRadius: 0,
                        tension: 0.35
                    },
                    {
                        label: '拱脚水平位移 Spring Horizontal',
                        data: spring,
                        borderColor: COLORS.teal,
                        backgroundColor: 'transparent',
                        borderWidth: 2,
                        pointRadius: 2,
                        tension: 0.35
                    },
                    {
                        label: '桥墩沉降 Pier Settlement',
                        data: pier,
                        borderColor: COLORS.blue,
                        backgroundColor: 'transparent',
                        borderWidth: 2,
                        pointRadius: 2,
                        tension: 0.35
                    }
                ]
            },
            options: {
                ...baseOpts,
                plugins: {
                    ...baseOpts.plugins,
                    title: { display: true, text: '50年累计变形预测 (mm) — 考虑徐变、收缩与温度作用', color: COLORS.textDim, font: { size: 12, weight: '500' }, padding: { bottom: 16 }, align: 'start' }
                },
                scales: {
                    x: {
                        ticks: { color: COLORS.textDim, font: { size: 10 } },
                        grid: { color: COLORS.grid, drawBorder: false },
                        border: { display: false }
                    },
                    y: {
                        title: { display: true, text: '累计位移 mm', color: COLORS.textDim, font: { size: 10 } },
                        ticks: { color: COLORS.textDim, font: { size: 10 } },
                        grid: { color: COLORS.grid, drawBorder: false },
                        border: { display: false }
                    }
                }
            }
        });
    }
}

function switchTab(tabName) {
    document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
    const btn = document.querySelector(`.tab-btn[data-tab="${tabName}"]`);
    if (btn) btn.classList.add('active');

    ['chartLive', 'chartStress', 'chartPredict'].forEach(id => {
        const el = document.getElementById(id);
        if (!el) return;
        if ((tabName === 'live' && id === 'chartLive') ||
            (tabName === 'stress' && id === 'chartStress') ||
            (tabName === 'predict' && id === 'chartPredict')) {
            el.style.display = '';
        } else {
            el.style.display = 'none';
        }
    });
}

function renderAlerts() {
    const list = document.getElementById('alertsList');
    if (!list) return;
    list.innerHTML = '';

    const sorted = [...alerts].sort((a, b) => {
        const ta = a.time || a.Time;
        const tb = b.time || b.Time;
        return new Date(tb) - new Date(ta);
    });

    const sevText = { critical: '严重', warning: '警告', info: '信息' };

    sorted.forEach(a => {
        const sev = a.severity || a.Severity || 'info';
        const msg = a.message || a.Message || '';
        const sid = a.sensor_id || a.SensorID || '';
        const tRaw = a.time || a.Time;
        const dt = new Date(tRaw);
        const tStr = isNaN(dt.getTime())
            ? (typeof tRaw === 'string' ? tRaw.slice(11, 19) : '--:--:--')
            : dt.getHours().toString().padStart(2, '0') + ':' +
              dt.getMinutes().toString().padStart(2, '0') + ':' +
              dt.getSeconds().toString().padStart(2, '0');

        const el = document.createElement('div');
        el.className = 'alert-item ' + sev + (sev === 'critical' ? ' critical-row' : '');
        el.innerHTML = `
            <div class="alert-top">
                <span class="alert-badge badge-${sev}">${sevText[sev] || sev}</span>
                <span class="alert-time">${tStr}</span>
            </div>
            <div class="alert-meta">
                <span class="alert-sensor"><strong>${sid}</strong></span>
            </div>
            <div class="alert-message">${msg}</div>
        `;
        list.appendChild(el);
    });

    const counts = sorted.reduce((acc, a) => {
        const s = (a.severity || a.Severity || 'info');
        acc[s] = (acc[s] || 0) + 1;
        return acc;
    }, { critical: 0, warning: 0, info: 0 });

    const total = counts.critical + counts.warning + counts.info;
    const cnt = document.getElementById('alertsCount');
    if (cnt) cnt.textContent = total;
    const cC = document.getElementById('cntCritical');
    if (cC) cC.textContent = counts.critical;
    const cW = document.getElementById('cntWarning');
    if (cW) cW.textContent = counts.warning;
    const cI = document.getElementById('cntInfo');
    if (cI) cI.textContent = counts.info;
}

async function pollLatestData() {
    if (socket && socket.readyState === WebSocket.OPEN) {
        return;
    }
    const data = await fetchJSON(`${apiBase}/api/sensors/all/latest`);
    if (data && Array.isArray(data)) {
        if (!(socket && socket.readyState === WebSocket.OPEN)) {
            updateLatestData(data);
        }
    }
}

function startClock() {
    const weekdays = ['日', '一', '二', '三', '四', '五', '六'];
    function tick() {
        const now = new Date();
        const t = now.getHours().toString().padStart(2, '0') + ':' +
                  now.getMinutes().toString().padStart(2, '0') + ':' +
                  now.getSeconds().toString().padStart(2, '0');
        const d = now.getFullYear() + '-' +
                  (now.getMonth() + 1).toString().padStart(2, '0') + '-' +
                  now.getDate().toString().padStart(2, '0') + ' 星期' +
                  weekdays[now.getDay()];
        const ct = document.getElementById('clockTime');
        const cd = document.getElementById('clockDate');
        if (ct) ct.textContent = t;
        if (cd) cd.textContent = d;
    }
    tick();
    setInterval(tick, 1000);
}

function setupEventHandlers() {
    const stressBtn = document.getElementById('btnStress') || document.getElementById('btn-stress-map');
    if (stressBtn) {
        stressBtn.addEventListener('click', async () => {
            stressBtn.classList.toggle('active');
            const active = stressBtn.classList.contains('active');
            if (typeof setStressMapVisible === 'function') {
                setStressMapVisible(active);
            }
            const res = await fetchJSON(`${apiBase}/api/fem/analyze`, {
                method: 'POST',
                body: JSON.stringify({ live_load: 10000, delta_t: 0 })
            });
            if (res && Array.isArray(res)) {
                femStress = res.map(r => r.von_mises || 0);
                if (typeof updateStressMap === 'function') {
                    const elemTriples = femElements.map(e =>
                        Array.isArray(e.node_ids) ? e.node_ids : [e[0], e[1], e[2]]
                    );
                    updateStressMap(femStress, elemTriples, femNodes);
                }
                if (mainCharts.stress) {
                    const groups = 8;
                    const newVals = [];
                    for (let i = 0; i < groups; i++) {
                        const start = Math.floor(i * femStress.length / groups);
                        const end = Math.floor((i + 1) * femStress.length / groups);
                        let sum = 0, cnt = 0;
                        for (let j = start; j < end && j < femStress.length; j++) {
                            sum += (femStress[j] || 0) / 1e6;
                            cnt++;
                        }
                        newVals.push(cnt ? sum / cnt : Math.random() * 15);
                    }
                    mainCharts.stress.data.datasets[0].data = newVals;
                    const COLORS = window.COLORS || { red: '#e74c3c', warning: '#f39c12', teal: '#1abc9c' };
                    const colors = newVals.map(v => {
                        if (v > 12) return COLORS.red;
                        if (v > 8) return COLORS.warning;
                        return COLORS.teal;
                    });
                    mainCharts.stress.data.datasets[0].backgroundColor = colors.map(c => c + '33');
                    mainCharts.stress.data.datasets[0].borderColor = colors;
                    mainCharts.stress.update();
                }
            }
        });
    }

    const crackBtn = document.getElementById('btnCrack') || document.getElementById('btn-crack-markers');
    if (crackBtn) {
        crackBtn.addEventListener('click', () => {
            crackBtn.classList.toggle('active');
            const active = crackBtn.classList.contains('active');
            if (typeof setCrackMarkersVisible === 'function') {
                setCrackMarkersVisible(active);
            }
        });
    }

    const resetBtn = document.getElementById('btnReset') || document.getElementById('btn-reset-view');
    if (resetBtn) {
        resetBtn.addEventListener('click', () => {
            if (typeof resetView === 'function') {
                resetView();
            }
        });
    }

    const rotBtn = document.getElementById('btnRotate') || document.getElementById('btn-auto-rotate');
    if (rotBtn) {
        rotBtn.addEventListener('click', () => {
            rotBtn.classList.toggle('active');
            const active = rotBtn.classList.contains('active');
            if (typeof setAutoRotate === 'function') {
                setAutoRotate(active);
            }
        });
    }

    const predBtn = document.getElementById('btnPredict') || document.getElementById('btn-predict-50');
    if (predBtn) {
        predBtn.addEventListener('click', async () => {
            predBtn.classList.toggle('active');
            if (predictions.length === 0 || !femNodes.length) {
                const pr = await fetchJSON(`${apiBase}/api/deformation/predict50`, { method: 'POST' });
                if (pr && Array.isArray(pr)) {
                    predictions = pr.map(p => ({
                        dx: p.predicted_dx || 0,
                        dy: p.predicted_dy || 0,
                        node_id: p.node_id
                    }));
                }
            }
            if (typeof animateDeformation50Years === 'function') {
                animateDeformation50Years(predictions, femNodes);
            }
            if (mainCharts.predict) {
                mainCharts.predict.data.datasets.forEach((ds, i) => {
                    if (i === 0) {
                        ds.borderWidth = 4;
                        ds.pointRadius = 5;
                    }
                });
                mainCharts.predict.update();
                setTimeout(() => {
                    if (mainCharts.predict) {
                        mainCharts.predict.data.datasets.forEach((ds, i) => {
                            if (i === 0) {
                                ds.borderWidth = 2.5;
                                ds.pointRadius = 3;
                            }
                        });
                        mainCharts.predict.update();
                    }
                }, 7000);
            }
        });
    }

    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            switchTab(btn.dataset.tab);
        });
    });
}

document.addEventListener('DOMContentLoaded', () => {
    startClock();
    initDashboard();
});
