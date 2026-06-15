#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
赵州桥传感器数据模拟器 (4G DTU 上报模拟)
Zhaozhou Bridge Sensor Simulator - 4G DTU Reporting Simulation
"""

import urllib.request
import urllib.error
import json
import time
import datetime
import random
import math
import argparse
import threading
import signal
import sys
import os


SENSOR_DEFS = [
    {"sensor_id": "ARCH-001", "type": "strain", "x": 18.5, "y": 7.23},
    {"sensor_id": "ARCH-002", "type": "strain", "x": 9.0, "y": 5.4},
    {"sensor_id": "ARCH-003", "type": "strain", "x": 28.0, "y": 5.4},
    {"sensor_id": "ARCH-004", "type": "strain", "x": 0.5, "y": 0.5},
    {"sensor_id": "PIER-001", "type": "settlement", "x": 0.0, "y": 0.0},
    {"sensor_id": "PIER-002", "type": "settlement", "x": 37.02, "y": 0.0},
    {"sensor_id": "SARCH-L001", "type": "strain", "x": 8.0, "y": 4.5},
    {"sensor_id": "SARCH-L002", "type": "strain", "x": 14.0, "y": 5.2},
    {"sensor_id": "SARCH-R001", "type": "strain", "x": 23.0, "y": 5.2},
    {"sensor_id": "SARCH-R002", "type": "strain", "x": 29.0, "y": 4.5},
    {"sensor_id": "CRACK-001", "type": "crack", "x": 20.0, "y": 6.5},
    {"sensor_id": "CRACK-002", "type": "crack", "x": 12.0, "y": 4.0},
]

BRIDGE_SPAN = 37.02
ALPHA_MICRO = 5.0
g_running = True
g_lock = threading.Lock()


def gaussian(mean, stddev):
    return random.gauss(mean, stddev)


def compute_temperature(ts, sensor_offset=0.0):
    doy = ts.timetuple().tm_yday
    hour = ts.hour + ts.minute / 60.0 + ts.second / 3600.0
    annual = 12.0 + 25.0 * math.sin(2.0 * math.pi * (doy - 80.0) / 365.0)
    diurnal = 5.0 * math.sin(2.0 * math.pi * (hour - 6.0) / 24.0)
    noise = (random.random() - 0.5) * 1.0
    return annual + diurnal + noise + sensor_offset


def generate_reading(sensor_id, current_time, accumulated_drift, injections=None, injection_state=None):
    if injections is None:
        injections = {}
    if injection_state is None:
        injection_state = {}

    info = None
    for s in SENSOR_DEFS:
        if s["sensor_id"] == sensor_id:
            info = s
            break
    if info is None:
        info = {"sensor_id": sensor_id, "type": "unknown", "x": 0, "y": 0}

    sensor_offset_key = sensor_id + "_temp_offset"
    if sensor_offset_key not in accumulated_drift:
        accumulated_drift[sensor_offset_key] = (random.random() - 0.5) * 0.4
    temp_offset = accumulated_drift[sensor_offset_key]
    temperature = compute_temperature(current_time, temp_offset)

    inject_temp = injections.get("inject_temp", 0.0)
    temperature += inject_temp

    base_temp_15 = 12.0 + 25.0 * math.sin(2.0 * math.pi * (96 - 80.0) / 365.0)
    delta_T = temperature - base_temp_15

    doy = current_time.timetuple().tm_yday
    year_frac = doy / 365.0
    hour = current_time.hour
    days_since_start = accumulated_drift.get("_elapsed_days", 0.0)

    strain_micro = None
    settlement_mm = None
    crack_width_mm = None

    inject_load = injections.get("inject_load", 1.0)
    inject_strain_spike = injections.get("inject_strain_spike")
    inject_settlement = injections.get("inject_settlement", 0.0)
    inject_crack_growth = injections.get("inject_crack_growth", 0.0)

    if info["type"] == "strain":
        x_pos = info["x"]
        base_strain = 80.0 + 120.0 * abs(math.sin(math.pi * x_pos / BRIDGE_SPAN))
        thermal_strain = ALPHA_MICRO * delta_T
        daytime_factor = 1.0 if 8 <= hour <= 20 else 0.35
        live_load_component = random.random() * 60.0 * daytime_factor
        live_load_component *= inject_load
        drift_key = sensor_id + "_creep"
        if drift_key not in accumulated_drift:
            accumulated_drift[drift_key] = 0.0
        accumulated_drift[drift_key] += 0.05 * (1.0 / 144.0)
        creep_drift = accumulated_drift[drift_key]
        noise = gaussian(0, 3.0)
        strain_micro = base_strain + thermal_strain + live_load_component + creep_drift + noise

        if inject_strain_spike is not None:
            spike_sensor, spike_value = inject_strain_spike
            if sensor_id == spike_sensor and not injection_state.get("strain_spike_applied", False):
                strain_micro += spike_value
                injection_state["strain_spike_applied"] = True

    elif info["type"] == "settlement":
        seasonal = 0.3 * math.sin(2.0 * math.pi * year_frac)
        drift_key = sensor_id + "_settle"
        if drift_key not in accumulated_drift:
            accumulated_drift[drift_key] = 0.0
        rate_per_10min = 0.015 / 144.0
        if days_since_start > 200:
            rate_per_10min += 0.05 / 144.0
        accumulated_drift[drift_key] += rate_per_10min
        cumulative = accumulated_drift[drift_key]
        noise = gaussian(0, 0.05)
        settlement_mm = seasonal + cumulative + noise
        settlement_mm += inject_settlement

    elif info["type"] == "crack":
        seasonal = 0.02 * math.sin(2.0 * math.pi * year_frac)
        base = 0.08
        drift_key = sensor_id + "_growth"
        if drift_key not in accumulated_drift:
            accumulated_drift[drift_key] = 0.0
        growth_per_10min = 0.001 / 144.0
        if days_since_start > 400:
            growth_per_10min += 0.004 / 144.0
        accumulated_drift[drift_key] += growth_per_10min
        growth = accumulated_drift[drift_key]
        noise = gaussian(0, 0.005)
        crack_width_mm = base + seasonal + growth + noise
        crack_width_mm *= (1 + inject_crack_growth * days_since_start)

    reading = {
        "time": current_time.strftime("%Y-%m-%dT%H:%M:%S+08:00"),
        "sensor_id": sensor_id,
        "strain_micro": strain_micro,
        "settlement_mm": settlement_mm,
        "temperature": temperature,
        "crack_width_mm": crack_width_mm,
    }
    return reading


def ingest_reading(api, reading):
    url = api.rstrip("/") + "/api/sensors/data"
    body = json.dumps(reading).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            status = resp.getcode()
            if 200 <= status < 300:
                return True, status
            return False, status
    except urllib.error.HTTPError as e:
        return False, e.code
    except urllib.error.URLError as e:
        return False, str(e.reason)
    except Exception as e:
        return False, str(e)


def log_ts():
    return datetime.datetime.now().strftime("%Y-%m-%d %H:%M:%S")


def backfill_historical(api, days, interval, injections=None, injection_state=None):
    print(f"\n[回填历史数据] 开始生成 {days} 天历史数据, 间隔 {interval}s ...")
    now = datetime.datetime.now()
    start_time = now - datetime.timedelta(days=days)
    total_seconds = days * 86400
    total_steps = max(1, int(total_seconds / interval))
    steps_done = 0
    next_pct = 10
    accumulated_drift = {}
    drift_day_key = "_elapsed_days"
    elapsed_days = 0.0
    day_increment = interval / 86400.0
    ok_count = 0
    err_count = 0

    current = start_time
    while current <= now and g_running:
        accumulated_drift[drift_day_key] = elapsed_days
        for s in SENSOR_DEFS:
            if not g_running:
                break
            reading = generate_reading(s["sensor_id"], current, accumulated_drift, injections, injection_state)
            ok, status = ingest_reading(api, reading)
            if ok:
                ok_count += 1
            else:
                err_count += 1
        steps_done += 1
        elapsed_days += day_increment
        pct = int((steps_done / total_steps) * 100)
        if pct >= next_pct:
            dots = "." * (pct // 10)
            print(f"  [{dots:10s}] {pct:3d}%  已上报 {ok_count + err_count} 条 (成功 {ok_count}, 失败 {err_count})")
            while next_pct <= pct:
                next_pct += 10
        current += datetime.timedelta(seconds=interval)

    print(f"[回填历史数据] 完成! 共上报 {ok_count + err_count} 条 (成功 {ok_count}, 失败 {err_count})")
    return elapsed_days


def realtime_loop(api, interval_sec, injections=None, injection_state=None, duration_hours=0.0):
    global g_running
    print(f"\n[实时模拟中] 每隔 {interval_sec:.1f}s 上报一次, Ctrl+C 退出...")
    accumulated_drift = {}
    start_time = time.time()
    duration_seconds = duration_hours * 3600.0

    def signal_handler(sig, frame):
        global g_running
        with g_lock:
            if not g_running:
                return
            print(f"\n[{log_ts()}] 收到退出信号, 正在关闭...")
            g_running = False

    signal.signal(signal.SIGINT, signal_handler)
    if hasattr(signal, "SIGTERM"):
        signal.signal(signal.SIGTERM, signal_handler)

    elapsed_days = 0.0
    drift_day_key = "_elapsed_days"
    ok_count = 0
    err_count = 0
    last_summary = 0

    while g_running:
        if duration_hours > 0 and (time.time() - start_time) >= duration_seconds:
            print(f"\n[{log_ts()}] 已达到运行时长 {duration_hours} 小时, 正在停止...")
            g_running = False
            break

        cycle_ok = 0
        cycle_err = 0
        values_summary = []

        current = datetime.datetime.now()
        accumulated_drift[drift_day_key] = elapsed_days

        for s in SENSOR_DEFS:
            if not g_running:
                break
            reading = generate_reading(s["sensor_id"], current, accumulated_drift, injections, injection_state)
            sid = reading["sensor_id"]
            ok, status = ingest_reading(api, reading)
            if ok:
                ok_count += 1
                cycle_ok += 1
                prefix = f"  [{log_ts()}] [OK] {sid}"
                parts = []
                if reading["strain_micro"] is not None:
                    parts.append(f"应变={reading['strain_micro']:.2f}με")
                if reading["settlement_mm"] is not None:
                    parts.append(f"沉降={reading['settlement_mm']:.3f}mm")
                if reading["temperature"] is not None:
                    parts.append(f"温度={reading['temperature']:.2f}℃")
                if reading["crack_width_mm"] is not None:
                    parts.append(f"裂缝={reading['crack_width_mm']:.4f}mm")
                print(prefix + " " + " ".join(parts))
                if reading["temperature"] is not None:
                    values_summary.append(reading["temperature"])
            else:
                err_count += 1
                cycle_err += 1
                print(f"  [{log_ts()}] [ERROR] {sid} HTTP {status}")

        elapsed_days += interval_sec / 86400.0

        if values_summary:
            avg_temp = sum(values_summary) / len(values_summary)
            if time.time() - last_summary > 600:
                print(f"\n[{log_ts()}] === 运行摘要 ===  累计成功 {ok_count}, 失败 {err_count}, 平均温度 {avg_temp:.2f}℃, 模拟已过 {elapsed_days:.4f} 天\n")
                last_summary = time.time()

        jitter = interval_sec * 0.05 * (random.random() - 0.5)
        sleep_time = max(0.5, interval_sec + jitter)
        end_sleep = time.time() + sleep_time
        while time.time() < end_sleep and g_running:
            time.sleep(min(0.2, end_sleep - time.time()))

    print(f"\n[{log_ts()}] 模拟已停止. 共上报 {ok_count + err_count} 条 (成功 {ok_count}, 失败 {err_count})")


def print_banner(api, interval, days, injections=None, duration_hours=0.0):
    art = r"""
    ╔══════════════════════════════════════════════════════════╗
    ║                                                          ║
    ║     赵州桥  ·  传感器数据模拟器 4G DTU                  ║
    ║     Zhaozhou Bridge SHM - Sensor Simulator              ║
    ║                                                          ║
    ║           .───.          .───.          .───.           ║
    ║          /     \        /     \        /     \          ║
    ║         |   ╭───╯      ╰───╮  |      ╰───╮   |         ║
    ║         |  /             \  |  |  /        \  |         ║
    ║     ════╰─╯───────────────╰──╯──╯───────────╰─╯════    ║
    ║                                                          ║
    ╚══════════════════════════════════════════════════════════╝
    """
    print(art)
    print("  配置信息:")
    print(f"    API 地址       : {api}")
    print(f"    上报间隔       : {interval:.1f} 秒")
    print(f"    传感器数量     : {len(SENSOR_DEFS)}")
    for s in SENSOR_DEFS:
        print(f"      - {s['sensor_id']:12s}  type={s['type']:10s}  (x={s['x']:.2f}, y={s['y']:.2f})")
    print(f"    历史回填天数   : {days} 天")
    if duration_hours > 0:
        print(f"    运行时长       : {duration_hours} 小时后自动停止")
    print()

    if injections is None:
        injections = {}

    has_injections = False
    print("  数据注入配置:")

    inject_temp = injections.get("inject_temp", 0.0)
    if inject_temp != 0.0:
        has_injections = True
        print(f"    温度偏移       : +{inject_temp:.1f}℃ (温度注入)")

    inject_load = injections.get("inject_load", 1.0)
    if inject_load != 1.0:
        has_injections = True
        if inject_load == 0.0:
            print(f"    活荷载系数     : {inject_load:.1f} (无荷载注入)")
        elif inject_load > 1.0:
            print(f"    活荷载系数     : {inject_load:.1f} (过载 {inject_load:.1f}x 注入)")
        else:
            print(f"    活荷载系数     : {inject_load:.1f} (轻载注入)")

    inject_strain_spike = injections.get("inject_strain_spike")
    if inject_strain_spike is not None:
        has_injections = True
        spike_sensor, spike_value = inject_strain_spike
        print(f"    应变尖峰       : 传感器 {spike_sensor} +{spike_value} με (一次性注入)")

    inject_settlement = injections.get("inject_settlement", 0.0)
    if inject_settlement != 0.0:
        has_injections = True
        print(f"    沉降偏移       : +{inject_settlement:.2f}mm (所有 PIER 传感器永久注入)")

    inject_crack_growth = injections.get("inject_crack_growth", 0.0)
    if inject_crack_growth != 0.0:
        has_injections = True
        print(f"    裂缝增长加速   : x{inject_crack_growth:.2f} 倍率 (时间相关注入)")

    if not has_injections:
        print("    (无数据注入, 使用正常模拟数据)")
    print()


def parse_strain_spike(value):
    if "=" not in value:
        raise argparse.ArgumentTypeError(f"格式错误, 应为 sensor_id=value, 例如: ARCH-001=2000")
    parts = value.split("=", 1)
    sensor_id = parts[0].strip()
    try:
        spike_value = float(parts[1].strip())
    except ValueError:
        raise argparse.ArgumentTypeError(f"应变值必须为数字, 收到: {parts[1]}")
    return (sensor_id, spike_value)


def str_to_bool(value):
    if isinstance(value, bool):
        return value
    if value is None:
        return False
    s = str(value).strip().lower()
    if s in ("true", "t", "yes", "y", "1"):
        return True
    if s in ("false", "f", "no", "n", "0"):
        return False
    raise ValueError(f"无法将 '{value}' 解析为布尔值")


def main():
    parser = argparse.ArgumentParser(
        description="赵州桥传感器数据模拟器 - 4G DTU 上报模拟",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--api",
        default="http://localhost:8080",
        help="API 基础地址 (默认: http://localhost:8080)",
    )
    parser.add_argument(
        "--interval",
        type=float,
        default=600.0,
        help="上报间隔秒数 (默认: 600 = 10分钟)",
    )
    parser.add_argument(
        "--fast-mode",
        action="store_true",
        help="快速模式: 使用 1 秒间隔 (测试用)",
    )
    parser.add_argument(
        "--days",
        type=float,
        default=0.0,
        help="先回填 N 天历史数据 (默认: 0, 不回填)",
    )
    parser.add_argument(
        "--inject-temp",
        type=float,
        default=0.0,
        help="注入固定温度偏移量 (℃), 例如 --inject-temp 40 表示 +40℃ 热浪模拟",
    )
    parser.add_argument(
        "--inject-load",
        type=float,
        default=1.0,
        help="注入活荷载倍率 (1.0=正常, 3.0=3倍过载, 0.0=无荷载)",
    )
    parser.add_argument(
        "--inject-strain-spike",
        type=parse_strain_spike,
        default=None,
        help="注入一次性应变尖峰, 格式: sensor_id=value, 例如 ARCH-001=2000",
    )
    parser.add_argument(
        "--inject-settlement",
        type=float,
        default=0.0,
        help="注入永久沉降偏移量 (mm), 应用于所有 PIER 传感器",
    )
    parser.add_argument(
        "--inject-crack-growth",
        type=float,
        default=0.0,
        help="注入裂缝增长加速倍率",
    )
    parser.add_argument(
        "--duration-hours",
        type=float,
        default=0.0,
        help="运行 N 小时后自动停止 (0=无限运行, 默认 0)",
    )
    parser.add_argument(
        "--env-config",
        action="store_true",
        help="从环境变量加载配置 (Docker 专用)",
    )
    args = parser.parse_args()

    if args.env_config:
        api = os.environ.get("API_BASE", args.api)
        sim_interval = float(os.environ.get("SIM_INTERVAL", args.interval))
        sim_fast_mode = str_to_bool(os.environ.get("SIM_FAST_MODE", args.fast_mode))
        sim_backfill_days = float(os.environ.get("SIM_BACKFILL_DAYS", args.days))
        inject_temp = float(os.environ.get("INJECT_TEMP", args.inject_temp))
        inject_load = float(os.environ.get("INJECT_LOAD", args.inject_load))
        inject_strain_spike_str = os.environ.get("INJECT_STRAIN_SPIKE")
        if inject_strain_spike_str:
            inject_strain_spike = parse_strain_spike(inject_strain_spike_str)
        else:
            inject_strain_spike = args.inject_strain_spike
        inject_settlement = float(os.environ.get("INJECT_SETTLEMENT", args.inject_settlement))
        inject_crack_growth = float(os.environ.get("INJECT_CRACK_GROWTH", args.inject_crack_growth))
        duration_hours = float(os.environ.get("DURATION_HOURS", args.duration_hours))
    else:
        api = args.api
        sim_interval = args.interval
        sim_fast_mode = args.fast_mode
        sim_backfill_days = args.days
        inject_temp = args.inject_temp
        inject_load = args.inject_load
        inject_strain_spike = args.inject_strain_spike
        inject_settlement = args.inject_settlement
        inject_crack_growth = args.inject_crack_growth
        duration_hours = args.duration_hours

    interval = 1.0 if sim_fast_mode else sim_interval
    days = sim_backfill_days

    injections = {
        "inject_temp": inject_temp,
        "inject_load": inject_load,
        "inject_strain_spike": inject_strain_spike,
        "inject_settlement": inject_settlement,
        "inject_crack_growth": inject_crack_growth,
    }

    injection_state = {
        "strain_spike_applied": False,
    }

    print_banner(api, interval, days, injections, duration_hours)

    print(f"[{log_ts()}] 模拟启动")

    backfill_interval = 3600.0
    if sim_fast_mode:
        backfill_interval = 600.0

    elapsed_from_backfill = 0.0
    if days > 0:
        elapsed_from_backfill = backfill_historical(api, days, backfill_interval, injections, injection_state)

    if elapsed_from_backfill > 0:
        print(f"[{log_ts()}] 历史回填模拟耗时约 {elapsed_from_backfill:.2f} 天, 已附加到实时模拟累计漂移.")

    if g_running:
        realtime_loop(api, interval, injections, injection_state, duration_hours)

    print(f"[{log_ts()}] 程序正常退出. 再见!\n")
    sys.exit(0)


if __name__ == "__main__":
    main()
