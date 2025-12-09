(() => {
  const canvas = document.getElementById("canvas");
  const ctx = canvas.getContext("2d");
  const statusEl = document.getElementById("status");
  const playBtn = document.getElementById("playPause");
  const jumpBackBtn = document.getElementById("jumpBack");
  const jumpFwdBtn = document.getElementById("jumpFwd");
  const playbackRateSelect = document.getElementById("playbackRate");
  const scrub = document.getElementById("scrub");
  const timeLabel = document.getElementById("timeLabel");
  const legendEl = document.getElementById("legend");
  const lapsEl = document.getElementById("laps");
  const eventsEl = document.getElementById("events");
  const collapsibleHeaders = document.querySelectorAll(".collapsible");
  const deltaCanvas = document.getElementById("delta");
  const deltaCtx = deltaCanvas.getContext("2d");
  const deltaInfo = document.getElementById("deltaInfo");
  const deltaPlayer = document.getElementById("deltaPlayer");
  const steerCanvas = document.getElementById("steer");
  const steerCtx = steerCanvas.getContext("2d");
  const steerInfo = document.getElementById("steerInfo");
  const cornerStatsEl = document.getElementById("cornerStats");
  const segmentStatsEl = document.getElementById("segmentStats");
  const segmentGhostEl = document.getElementById("segmentGhost");
  const liveEl = document.getElementById("live");
  const livePanel = document.getElementById("livePanel");
  const liveShowControls = document.getElementById("liveShowControls");
  const advancedLatch = document.getElementById("advancedLatch");
  const settingsBtn = document.getElementById("settingsBtn");
  const settingsOverlay = document.getElementById("settingsOverlay");
  const settingsClose = document.getElementById("settingsClose");
  const showEventsToggle = document.getElementById("showEvents");
  const showEventsLatch = document.querySelector('label[for="showEvents"]');
  const showLabelsToggle = document.getElementById("showLabels");
  const showLabelsLatch = document.querySelector('label[for="showLabels"]');
  const settingsEventTypes = document.getElementById("settingsEventTypes");
  const eventFilterEl = document.getElementById("eventFilter");
  const eventPresetsEl = document.getElementById("eventPresets");
  const toastContainer = document.getElementById("toastContainer");
  const staticCanvas = document.createElement("canvas");
  const staticCtx = staticCanvas.getContext("2d");

  let data = null;
  let playing = false;
  let lastTs = 0;
  let currentTime = 0;
  let maxTime = 0;
  let playbackRate = 1;
  let unit = "mph";
  let boundsCache = null;
  let lastHudUpdate = 0;
  const hudInterval = 100; // ms
  let carCursors = [];
  let frameCapMs = 1000 / 45;
  let selectedCar = null; // null = show all
  let showControls = false;
  let showEvents = false;
  let showLabels = false;
  let eventTypes = new Set();
  let notifyTypes = new Set();
  let eventTypeOrder = [];
  let sortedEvents = [];
  let lastNotifiedTime = 0;
  let carColorMap = new Map();
  let cornerLapExpanded = true;
  let lastLapRendered = -1;
  let ghostMode = false;
  let ghostRestore = null;
  let boundsOverride = null;
  let freezePlayback = false;
  let ghostState = {
    active: false,
    carIdx: 0,
    lap: 0,
    playerStart: 0,
    playerEnd: 0,
    ghostStart: 0,
    ghostEnd: 0,
  };
  const majorEvents = new Set(["crash", "collision", "reset", "drift", "traction", "overtake", "position_gain", "position_loss", "pole_gain", "pole_loss", "early_brake", "late_brake"]);
  // Good → Bad → Misc ordering
  const preferredEventOrder = [
    // Good
    "position_gain", "pole_gain", "overtake",
    // Bad
    "crash", "collision", "early_brake", "late_brake", "position_loss", "pole_loss", "traction",
    // Misc
    "surface", "reset", "drift",
  ];
  let debugEl = null;
  let debugEnabled = true;
  const telemetryRanges = {
    susp: { min: Infinity, max: -Infinity },
    // temps stored in °C from the backend; midLow/midHigh adjusted per unit in computeTelemetryRanges
    temp: { min: Infinity, max: -Infinity, midLow: 88, midHigh: 99 },
    line: { min: Infinity, max: -Infinity },
  };
  let tempUnit = "c"; // c or f
  let selectedCorner = null;
  const isSoloSprint = () => (data?.raceType || "").toLowerCase() === "sprint" && (data?.cars?.length || 0) <= 1;

  function resize() {
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    canvas.width = rect.width;
    canvas.height = rect.height;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    const drect = deltaCanvas.getBoundingClientRect();
    deltaCanvas.width = drect.width * dpr;
    deltaCanvas.height = drect.height * dpr;
    deltaCtx.setTransform(dpr, 0, 0, dpr, 0, 0);
    if (steerCanvas && steerCtx) {
      const srect = steerCanvas.getBoundingClientRect();
      steerCanvas.width = srect.width * dpr;
      steerCanvas.height = srect.height * dpr;
      steerCtx.setTransform(dpr, 0, 0, dpr, 0, 0);
    }
    staticCanvas.width = rect.width;
    staticCanvas.height = rect.height;
    staticCtx.setTransform(1, 0, 0, 1, 0, 0);
    if (data) {
      renderStatic();
      draw();
    }
  }

  window.addEventListener("resize", resize);

  function fetchData() {
    fetch("data.json")
      .then((r) => r.json())
      .then((j) => {
        data = j;
        maxTime = computeMaxTime();
        scrub.max = maxTime || 1;
        statusEl.textContent = `Loaded master (${data.master.length} pts), ${data.cars?.length || 0} cars, ${data.events?.length || 0} events`;
        // Build dynamic event type list
        const types = new Set();
        (data.events || []).forEach((ev) => {
          const t = (ev.type || "").toLowerCase();
          if (t) types.add(t);
        });
        eventTypeOrder = sortEvents(Array.from(types));
        const defaultTypes = new Set(["position_gain","position_loss","pole_gain","pole_loss"]);
        eventTypes = new Set(eventTypeOrder.filter((t) => defaultTypes.has(t)));
        notifyTypes = new Set(eventTypes);
        sortedEvents = (data.events || []).slice().filter((e) => isFinite(e.time)).sort((a, b) => a.time - b.time);
        lastNotifiedTime = 0;
        carColorMap = new Map();
        (data.cars || []).forEach((car, idx) => {
          const col = palette[idx % palette.length];
          if (car && car.source) {
            carColorMap.set(car.source, col);
          }
        });
        computeTelemetryRanges();
        buildDeltaPlayer();
        buildLegend();
        buildSettingsEventTypes();
        buildLaps();
        buildEvents();
        buildCornerStats();
        buildSegmentStats();
        updateLive();
        initDrag();
        initSettings();
        initLiveToggles();
        initCollapsibles();
        initDebug();
        renderStatic();
        carCursors = new Array(data.cars?.length || 0).fill(0);
        resize();
      })
      .catch((err) => {
        statusEl.textContent = `Failed to load data.json: ${err}`;
        console.error(err);
      });
  }

  function initDrag() {
    if (!livePanel) return;
    let dragging = false;
    let startX = 0, startY = 0;
    let panelX = livePanel.offsetLeft;
    let panelY = livePanel.offsetTop;
    const header = document.getElementById("liveHeader");
    const target = header || livePanel;
    const onDown = (e) => {
      if (window.innerWidth <= 900) return;
      dragging = true;
      startX = e.clientX;
      startY = e.clientY;
      panelX = livePanel.offsetLeft;
      panelY = livePanel.offsetTop;
      livePanel.style.cursor = "grabbing";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
    };
    const onMove = (e) => {
      if (!dragging) return;
      const dx = e.clientX - startX;
      const dy = e.clientY - startY;
      livePanel.style.left = `${panelX + dx}px`;
      livePanel.style.top = `${panelY + dy}px`;
      livePanel.style.right = "auto";
      livePanel.style.position = "fixed";
    };
    const onUp = () => {
      dragging = false;
      livePanel.style.cursor = "grab";
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    target.addEventListener("pointerdown", onDown);
  }

  function initCollapsibles() {
    collapsibleHeaders.forEach((h) => {
      const targetId = h.dataset.target;
      const body = targetId ? document.getElementById(targetId) : null;
      const chev = h.querySelector(".chev");
      if (!body) return;
      body.style.display = "block";
      const setState = (open) => {
        body.style.display = open ? "block" : "none";
        if (chev) {
          chev.style.transform = open ? "rotate(90deg)" : "rotate(0deg)";
          chev.style.color = open ? "var(--accent)" : "var(--muted)";
        }
      };
      let open = true;
      setState(open);
      h.addEventListener("click", () => {
        open = !open;
        setState(open);
      });
    });
  }

  function initSettings() {
    if (!settingsBtn || !settingsOverlay) return;
    settingsBtn.addEventListener("click", () => {
      settingsOverlay.style.display = "flex";
    });
    if (settingsClose) {
      settingsClose.addEventListener("click", () => {
        settingsOverlay.style.display = "none";
      });
    }
    settingsOverlay.addEventListener("click", (e) => {
      if (e.target === settingsOverlay) {
        settingsOverlay.style.display = "none";
      }
    });
    const radios = settingsOverlay.querySelectorAll("input[name=unit]");
    radios.forEach((r) => {
      r.addEventListener("change", (e) => {
        unit = e.target.value;
        settingsOverlay.style.display = "none";
        updateLive();
        buildCornerStats();
      });
      if (r.checked) unit = r.value;
    });
    if (showEventsToggle) {
      showEventsToggle.checked = showEvents;
      if (showEventsLatch) showEventsLatch.classList.toggle("active", showEvents);
      showEventsToggle.addEventListener("change", (e) => {
        showEvents = e.target.checked;
        if (showEventsLatch) showEventsLatch.classList.toggle("active", showEvents);
        buildEventFilter();
        renderStatic();
        draw();
      });
    }
    if (showLabelsToggle) {
      showLabelsToggle.checked = showLabels;
      if (showLabelsLatch) showLabelsLatch.classList.toggle("active", showLabels);
      showLabelsToggle.addEventListener("change", (e) => {
        showLabels = e.target.checked;
        if (showLabelsLatch) showLabelsLatch.classList.toggle("active", showLabels);
        renderStatic();
        draw();
      });
    }
    const tempRadios = settingsOverlay.querySelectorAll("input[name=tempUnit]");
    tempRadios.forEach((r) => {
      r.addEventListener("change", (e) => {
        tempUnit = e.target.value;
        computeTelemetryRanges();
        updateLive();
      });
      if (r.checked) tempUnit = r.value;
    });
  }

  function initLiveToggles() {
    if (!liveShowControls) return;
    liveShowControls.checked = showControls;
    if (advancedLatch) {
      advancedLatch.classList.toggle("active", showControls);
    }
    liveShowControls.addEventListener("change", (e) => {
      showControls = e.target.checked;
      if (advancedLatch) {
        advancedLatch.classList.toggle("active", showControls);
      }
      updateLive();
    });
  }

  function initDebug() {
    if (!canvas) return;
    if (!debugEl) {
      debugEl = document.createElement("div");
      debugEl.style.position = "fixed";
      debugEl.style.background = "rgba(0,0,0,0.7)";
      debugEl.style.color = "#fff";
      debugEl.style.padding = "4px 8px";
      debugEl.style.borderRadius = "4px";
      debugEl.style.fontSize = "12px";
      debugEl.style.pointerEvents = "none";
      debugEl.style.zIndex = 1000;
      debugEl.style.display = "none";
      document.body.appendChild(debugEl);
    }
    const onMove = (e) => {
      if (!debugEnabled) return;
      if (!data || !data.heatmap || data.heatmap.length === 0) return;
      const rect = canvas.getBoundingClientRect();
      const mx = (e.clientX - rect.left) * (canvas.width / (rect.width || 1));
      const my = (e.clientY - rect.top) * (canvas.height / (rect.height || 1));
      const bounds = boundsCache || getBounds();
      let best = null;
      for (let i = 0; i < data.heatmap.length; i++) {
        const pt = data.heatmap[i];
        const { x, y } = project(pt, bounds, canvas.width, canvas.height);
        const dx = x - mx;
        const dy = y - my;
        const d2 = dx * dx + dy * dy;
        if (!best || d2 < best.d2) {
          best = { idx: i, pt, d2 };
        }
      }
      if (!best) return;
      debugEl.style.display = "block";
      debugEl.textContent = `Heat idx ${best.idx} accel ${Number(best.pt.avgAccel).toFixed(3)} x ${Number(best.pt.x).toFixed(1)} y ${Number(best.pt.y).toFixed(1)}`;
      debugEl.style.left = `${e.clientX + 12}px`;
      debugEl.style.top = `${e.clientY + 12}px`;
    };
    const onLeave = () => {
      if (debugEl) debugEl.style.display = "none";
    };
    canvas.addEventListener("pointermove", onMove);
    canvas.addEventListener("pointerleave", onLeave);
    // Fallback for browsers without pointer events.
    canvas.addEventListener("mousemove", onMove);
    canvas.addEventListener("mouseleave", onLeave);
  }

  function orient(pt) {
    // Preserve native axes: x+ right, y+ up. Y inversion handled in projection.
    const p = { x: pt.x, y: pt.y };
    if (boundsOverride && boundsOverride.shift) {
      return { x: p.x + boundsOverride.shift.x, y: p.y + boundsOverride.shift.y };
    }
    return p;
  }

  function getBounds() {
    if (boundsOverride && boundsOverride.bounds) {
      return boundsOverride.bounds;
    }
    const pts = data.master || [];
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    pts.forEach((p) => {
      const o = orient(p);
      minX = Math.min(minX, o.x);
      minY = Math.min(minY, o.y);
      maxX = Math.max(maxX, o.x);
      maxY = Math.max(maxY, o.y);
    });
    // pad a bit
    const pad = 100;
    return { minX: minX - pad, minY: minY - pad, maxX: maxX + pad, maxY: maxY + pad };
  }

  function project(pt, bounds, w, h) {
    const o = orient(pt);
    const sx = w / (bounds.maxX - bounds.minX || 1);
    const sy = h / (bounds.maxY - bounds.minY || 1);
    const scale = Math.min(sx, sy) * 0.95;
    const cx = (bounds.minX + bounds.maxX) / 2;
    const cy = (bounds.minY + bounds.maxY) / 2;
    const ox = w / 2;
    const oy = h / 2;
    return {
      x: ox + (o.x - cx) * scale,
      y: oy - (o.y - cy) * scale, // invert Y for screen
    };
  }

  const palette = [
    "#ffb100", "#6bc5ff", "#ff6b6b", "#7bd389", "#f78bff", "#ffd166",
    "#7af8ff", "#c084fc", "#90e0ef", "#ff9f1c",
  ];
  const ghostColor = "#00d8ff";

  function filteredCars() {
    if (selectedCar === null) return data.cars || [];
    const car = (data.cars || [])[selectedCar];
    return car ? [car] : [];
  }

  function draw() {
    const w = canvas.clientWidth;
    const h = canvas.clientHeight;
    ctx.clearRect(0, 0, w, h);
    if (staticCanvas.width && staticCanvas.height) {
      ctx.drawImage(staticCanvas, 0, 0);
    }
    const bounds = boundsCache || getBounds();

    // Cars (head only)
    filteredCars().forEach((car, idx) => {
      const globalIdx = selectedCar !== null ? selectedCar : idx;
      const color = palette[globalIdx % palette.length];
      const head = headAtTime(globalIdx, currentTime);
      if (head) {
        const { x, y } = project({ x: head.masterX, y: head.masterY }, bounds, w, h);
        ctx.fillStyle = color;
        ctx.beginPath();
        ctx.arc(x, y, 5, 0, Math.PI * 2);
        ctx.fill();
        if (showLabels) {
          // Label with car name; omit event on map labels.
          const label = car.source || `Car ${globalIdx + 1}`;
          const text = label;
          ctx.font = "12px 'Segoe UI', system-ui, sans-serif";
          ctx.textBaseline = "middle";
          ctx.fillStyle = "#fff";
          ctx.strokeStyle = "rgba(0,0,0,0.6)";
          ctx.lineWidth = 3;
          ctx.strokeText(text, x + 8, y);
          ctx.fillText(text, x + 8, y);
        }
      }
    });

    // Ghost head
    const ghostHead = ghostPosition();
    if (ghostHead) {
      const { x, y } = project({ x: ghostHead.masterX, y: ghostHead.masterY }, bounds, w, h);
      ctx.fillStyle = ghostColor;
      ctx.beginPath();
      ctx.arc(x, y, 5, 0, Math.PI * 2);
      ctx.fill();
    }

  }

  function renderStatic() {
    if (!data) return;
    boundsCache = getBounds();
    const w = canvas.width;
    const h = canvas.height;
    staticCtx.setTransform(1, 0, 0, 1, 0, 0);
    staticCanvas.width = w;
    staticCanvas.height = h;
    staticCtx.clearRect(0, 0, w, h);
    const bounds = boundsCache;
    // Start/finish marker (simple line)
    if (data.master && data.master.length > 1) {
      const start = data.master[0];
      const next = data.master[1];
      const { x: sx, y: sy } = project(start, bounds, w, h);
      const { x: ex, y: ey } = project(next, bounds, w, h);
      const dx = ex - sx;
      const dy = ey - sy;
      const len = Math.hypot(dx, dy) || 1;
      const ux = dx / len;
      const uy = dy / len;
      const extend = len * 12; // extend line to make it visible across wider tracks
      const sx2 = sx - ux * extend;
      const sy2 = sy - uy * extend;
      const ex2 = ex + ux * extend;
      const ey2 = ey + uy * extend;
      staticCtx.strokeStyle = "#aaa";
      staticCtx.lineWidth = 8;
      staticCtx.beginPath();
      staticCtx.moveTo(sx2, sy2);
      staticCtx.lineTo(ex2, ey2);
      staticCtx.stroke();
    }
    // Heatmap
    if (data.heatmap && data.heatmap.length > 1) {
      const accels = data.heatmap.map((p) => p.avgAccel).filter((v) => isFinite(v));
      const scale = heatScale(accels);
      staticCtx.lineWidth = 4;
      for (let i = 0; i < data.heatmap.length - 1; i++) {
        const a = data.heatmap[i];
        const b = data.heatmap[i + 1];
        const { x: ax, y: ay } = project(a, bounds, w, h);
        const { x: bx, y: by } = project(b, bounds, w, h);
        staticCtx.strokeStyle = heatColor((a.avgAccel + b.avgAccel) / 2, scale);
        staticCtx.beginPath();
        staticCtx.moveTo(ax, ay);
        staticCtx.lineTo(bx, by);
        staticCtx.stroke();
      }
    }
    // Master track
    staticCtx.strokeStyle = "#6e7791";
    staticCtx.lineWidth = 2;
    staticCtx.beginPath();
    data.master.forEach((p, i) => {
      const { x, y } = project(p, bounds, w, h);
      if (i === 0) staticCtx.moveTo(x, y);
      else staticCtx.lineTo(x, y);
    });
    staticCtx.stroke();

    // Highlight selected corner
    if (selectedCorner !== null && data.corners && data.corners[selectedCorner]) {
      const c = data.corners[selectedCorner];
      const pts = data.master.filter((p) => p.relS >= c.startS && p.relS <= c.endS);
      if (pts.length > 1) {
        staticCtx.strokeStyle = "#ffb100";
        staticCtx.lineWidth = 5;
        staticCtx.beginPath();
        pts.forEach((p, i) => {
          const { x, y } = project(p, bounds, w, h);
          if (i === 0) staticCtx.moveTo(x, y);
          else staticCtx.lineTo(x, y);
        });
        staticCtx.stroke();
        staticCtx.lineWidth = 2;
      }
    }

    // Events on map (optional)
    if (showEvents && data.events && data.events.length) {
      const curLap = currentLapAtTime();
      data.events.forEach((ev) => {
        const t = (ev.type || "").toLowerCase();
        if (!eventTypes.has(t)) return;
        if (curLap > 0 && ev.lap > 0 && ev.lap !== curLap) return;
        const pt = { x: ev.masterX ?? ev.x, y: ev.masterY ?? ev.y };
        if (!isFinite(pt.x) || !isFinite(pt.y)) return;
        const { x, y } = project(pt, bounds, w, h);
        staticCtx.fillStyle = eventColor(ev.type || "");
        staticCtx.beginPath();
        staticCtx.arc(x, y, 4, 0, Math.PI * 2);
        staticCtx.fill();
      });
    }
  }

  function heatScale(vals) {
    if (!vals || vals.length === 0) return 1;
    const absVals = vals.map((v) => Math.abs(v)).filter((v) => isFinite(v)).sort((a, b) => a - b);
    if (absVals.length === 0) return 1;
    const idx = Math.floor(absVals.length * 0.9);
    return Math.max(0.2, absVals[idx] || absVals[absVals.length - 1] || 1);
  }

  function heatColor(v, scale) {
    if (!isFinite(v)) return "#666";
    const maxAbs = Math.max(0.2, Math.abs(scale));
    const t = Math.max(-1, Math.min(1, v / maxAbs)); // -1..1
    const amber = { r: 255, g: 177, b: 0 };
    const green = { r: 93, g: 211, b: 158 };
    const red = { r: 255, g: 107, b: 107 };
    if (t >= 0) {
      // accel: amber -> green
      const r = Math.round(amber.r + (green.r - amber.r) * t);
      const g = Math.round(amber.g + (green.g - amber.g) * t);
      const b = Math.round(amber.b + (green.b - amber.b) * t);
      return `rgb(${r},${g},${b})`;
    }
    // decel: amber -> red
    const tt = Math.abs(t);
    const r = Math.round(amber.r + (red.r - amber.r) * tt);
    const g = Math.round(amber.g + (red.g - amber.g) * tt);
    const b = Math.round(amber.b + (red.b - amber.b) * tt);
    return `rgb(${r},${g},${b})`;
  }

  function rampColor(val, range) {
    if (!range || !isFinite(val) || !isFinite(range.min) || !isFinite(range.max) || range.max === range.min) {
      return "";
    }
    const midLow = isFinite(range.midLow) ? range.midLow : (range.min + range.max) / 2;
    const midHigh = isFinite(range.midHigh) ? range.midHigh : midLow;
    const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v));
    const lerpColor = (c1, c2, t) => {
      const r = Math.round(c1.r + (c2.r - c1.r) * t);
      const g = Math.round(c1.g + (c2.g - c1.g) * t);
      const b = Math.round(c1.b + (c2.b - c1.b) * t);
      return `rgb(${r},${g},${b})`;
    };
    const red = { r: 255, g: 107, b: 107 };
    const amber = { r: 255, g: 177, b: 0 };
    const green = { r: 93, g: 211, b: 158 };

    let t = 0;
    if (val < midLow) {
      const span = midLow - range.min;
      t = span > 0 ? -1 + clamp((val - range.min) / span, 0, 1) : -1;
    } else if (val > midHigh) {
      const span = range.max - midHigh;
      t = span > 0 ? clamp((val - midHigh) / span, 0, 1) : 1;
    } else {
      t = 0;
    }
    t = clamp(t, -1, 1);
    const u = Math.abs(t);
    if (u >= 0.5) {
      const blend = (u - 0.5) / 0.5; // 0.5->1 maps amber->red
      return lerpColor(amber, red, blend);
    }
    // 0->0.5 maps green->amber
    const blend = u / 0.5;
    return lerpColor(green, amber, blend);
  }

  function positionAtTime(points, t) {
    if (!points || points.length === 0) return null;
    if (t <= points[0].time) return { ...points[0] };
    if (t >= points[points.length - 1].time) return { ...points[points.length - 1] };
    // binary search
    let lo = 0, hi = points.length - 1;
    while (hi - lo > 1) {
      const mid = (hi + lo) >> 1;
      if (points[mid].time <= t) lo = mid; else hi = mid;
    }
    const p1 = points[lo], p2 = points[hi];
    const span = p2.time - p1.time || 1;
    const alpha = (t - p1.time) / span;
    return {
      masterX: p1.masterX + (p2.masterX - p1.masterX) * alpha,
      masterY: p1.masterY + (p2.masterY - p1.masterY) * alpha,
      relS: p1.relS + (p2.relS - p1.relS) * alpha,
      lap: p1.lap,
      speedMPH: p1.speedMPH + (p2.speedMPH - p1.speedMPH) * alpha,
      speedKMH: p1.speedKMH + (p2.speedKMH - p1.speedKMH) * alpha,
      gear: p1.gear,
      delta: p1.delta + (p2.delta - p1.delta) * alpha,
      longAcc: p1.longAcc + (p2.longAcc - p1.longAcc) * alpha,
      latAcc: p1.latAcc + (p2.latAcc - p1.latAcc) * alpha,
      yawRate: p1.yawRate + (p2.yawRate - p1.yawRate) * alpha,
      yawDegS: p1.yawDegS + (p2.yawDegS - p1.yawDegS) * alpha,
      throttle: p1.throttle + (p2.throttle - p1.throttle) * alpha,
      brake: p1.brake + (p2.brake - p1.brake) * alpha,
      steerDeg: p1.steerDeg + (p2.steerDeg - p1.steerDeg) * alpha,
      throttleInput: p1.throttleInput + (p2.throttleInput - p1.throttleInput) * alpha,
      brakeInput: p1.brakeInput + (p2.brakeInput - p1.brakeInput) * alpha,
      steerInput: p1.steerInput + (p2.steerInput - p1.steerInput) * alpha,
      suspFL: p1.suspFL + (p2.suspFL - p1.suspFL) * alpha,
      suspFR: p1.suspFR + (p2.suspFR - p1.suspFR) * alpha,
      suspRL: p1.suspRL + (p2.suspRL - p1.suspRL) * alpha,
      suspRR: p1.suspRR + (p2.suspRR - p1.suspRR) * alpha,
      tireTempFL: p1.tireTempFL + (p2.tireTempFL - p1.tireTempFL) * alpha,
      tireTempFR: p1.tireTempFR + (p2.tireTempFR - p1.tireTempFR) * alpha,
      tireTempRL: p1.tireTempRL + (p2.tireTempRL - p1.tireTempRL) * alpha,
      tireTempRR: p1.tireTempRR + (p2.tireTempRR - p1.tireTempRR) * alpha,
      distToLine: p1.distToLine + (p2.distToLine - p1.distToLine) * alpha,
    };
  }

  function buildLegend() {
    legendEl.innerHTML = "";
    (data.cars || []).forEach((car, idx) => {
      const color = palette[idx % palette.length];
      const el = document.createElement("span");
      el.className = "swatch" + (selectedCar === idx ? " selected" : "");
      el.innerHTML = `<span class="dot" style="background:${color}"></span>${car.source || "car " + (idx + 1)}`;
      el.style.cursor = "pointer";
      el.addEventListener("click", () => {
        selectedCar = selectedCar === idx ? null : idx;
        deltaPlayer.value = selectedCar === null ? 0 : idx;
        buildLegend();
        buildEvents();
        buildCornerStats();
        buildSegmentStats();
        updateLive();
        updateDelta(true);
        draw();
      });
      legendEl.appendChild(el);
    });
    buildEventFilter();
    buildEventPresets();
  }

  function buildEventFilter() {
    if (!eventFilterEl) return;
    eventFilterEl.innerHTML = "";
    const types = eventTypeOrder.length ? eventTypeOrder : sortEvents(Array.from(eventTypes));
    types.forEach((t) => {
      const id = `ev-${t}`;
      const label = document.createElement("label");
      label.htmlFor = id;
      const cb = document.createElement("input");
      cb.type = "checkbox";
      cb.id = id;
      cb.checked = eventTypes.has(t);
      cb.addEventListener("change", (e) => {
        if (e.target.checked) eventTypes.add(t); else eventTypes.delete(t);
        buildEvents();
        renderStatic();
        draw();
        buildSettingsEventTypes();
        rebuildNotificationTypes();
      });
      label.appendChild(cb);
      label.appendChild(document.createTextNode(` ${t}`));
      eventFilterEl.appendChild(label);
    });
    eventFilterEl.style.display = showEvents ? "flex" : "none";
  }

  function buildEventPresets() {
    if (!eventPresetsEl) return;
    eventPresetsEl.innerHTML = "";
    const presets = [
      { name: "All", types: null },
      { name: "Performance", types: ["position_gain","position_loss","pole_gain","pole_loss","overtake"] },
      { name: "Issues", types: ["crash","collision","early_brake","late_brake","traction","surface"] },
    ];
    presets.forEach((p) => {
      const btn = document.createElement("button");
      btn.className = "event-preset";
      btn.textContent = p.name;
      btn.addEventListener("click", () => {
        if (p.types === null) {
          eventTypes = new Set(eventTypeOrder);
          notifyTypes = new Set(eventTypes);
        } else {
          eventTypes = new Set(p.types.filter((t) => eventTypeOrder.includes(t)));
          notifyTypes = new Set(eventTypes);
        }
        buildEventFilter();
        buildSettingsEventTypes();
        buildEvents();
        renderStatic();
        // glow feedback
        btn.style.boxShadow = `0 0 0 2px var(--accent)`;
        setTimeout(() => {
          btn.style.boxShadow = "";
        }, 300);
      });
      eventPresetsEl.appendChild(btn);
    });
  }

  function buildSettingsEventTypes() {
    if (!settingsEventTypes) return;
    settingsEventTypes.innerHTML = "";
    const wrapTitle = document.createElement("h4");
    wrapTitle.textContent = "Event types";
    settingsEventTypes.appendChild(wrapTitle);
    const types = eventTypeOrder.length ? eventTypeOrder : sortEvents(Array.from(eventTypes));
    types.forEach((t) => {
      const col = eventColor(t);
      const tint = col || "#cdd7e1";
      const lbl = document.createElement("label");
      lbl.className = "latch-btn block";
      const cb = document.createElement("input");
      cb.type = "checkbox";
      cb.checked = eventTypes.has(t);
      lbl.classList.toggle("active", cb.checked);
      applyEventToggleStyle(lbl, cb.checked, tint);
      cb.addEventListener("change", (e) => {
        if (e.target.checked) eventTypes.add(t); else eventTypes.delete(t);
        lbl.classList.toggle("active", e.target.checked);
        applyEventToggleStyle(lbl, e.target.checked, tint);
        buildEvents();
        renderStatic();
        draw();
        buildEventFilter();
        rebuildNotificationTypes();
      });
      const dot = document.createElement("span");
      dot.className = "pill-dot";
      dot.style.background = tint;
      dot.style.borderColor = `${tint}aa`;
      const text = document.createElement("span");
      text.className = "pill-label";
      text.textContent = t;
      text.style.color = cb.checked ? tint : "";
      lbl.appendChild(cb);
      lbl.appendChild(dot);
      lbl.appendChild(text);
      settingsEventTypes.appendChild(lbl);
    });
  }

  function rebuildNotificationTypes() {
    notifyTypes = new Set(eventTypes);
  }

  function sortEvents(list) {
    const order = new Map();
    preferredEventOrder.forEach((t, idx) => order.set(t, idx));
    return list.sort((a, b) => {
      const ia = order.has(a) ? order.get(a) : preferredEventOrder.length + a.localeCompare(b);
      const ib = order.has(b) ? order.get(b) : preferredEventOrder.length + b.localeCompare(a);
      return ia - ib;
    });
  }

  function applyEventToggleStyle(lbl, active, tint) {
    if (active) {
      lbl.style.borderColor = `${tint}cc`;
      lbl.style.boxShadow = `0 0 0 2px ${tint}33`;
      lbl.style.background = `linear-gradient(135deg, ${tint}26, ${tint}0f)`;
    } else {
      lbl.style.borderColor = "";
      lbl.style.boxShadow = "";
      lbl.style.background = "";
    }
  }

  function currentLapAtTime() {
    const cars = data?.cars || [];
    const idx = selectedCar !== null ? selectedCar : 0;
    const car = cars[idx];
    if (!car || !car.points || !car.points.length) return 0;
    const head = positionAtTime(car.points, currentTime);
    if (head && isFinite(head.lap)) return head.lap;
    return 0;
  }

  function ghostPosition() {
    if (!ghostState.active) return null;
    const cars = data?.cars || [];
    const car = cars[ghostState.carIdx];
    if (!car || !car.points || !car.points.length) return null;
    const lapPts = car.points.filter((p) => p.lap === ghostState.lap);
    if (!lapPts.length) return null;
    const prog = clamp((currentTime - ghostState.playerStart) / (ghostState.playerEnd - ghostState.playerStart), 0, 1);
    const t = ghostState.ghostStart + prog*(ghostState.ghostEnd - ghostState.ghostStart);
    return positionAtTime(lapPts, t);
  }

  function stopGhost() {
    ghostMode = false;
    ghostState.active = false;
    freezePlayback = false;
    if (ghostRestore) {
      boundsOverride = ghostRestore.bounds || null;
      currentTime = ghostRestore.time;
      playing = ghostRestore.playing;
      ghostRestore = null;
      lastTs = performance.now();
      renderStatic();
      draw();
    }
    // no auto-toggle; leave advanced as user set it
  }

  function clamp(v, lo, hi) {
    if (v < lo) return lo;
    if (v > hi) return hi;
    return v;
  }

  function playSegmentGhost(carIdx, segIdx, lapNum) {
    const cars = data?.cars || [];
    const car = cars[carIdx];
    const seg = data?.segments?.[segIdx];
    if (!car || !seg || !car.points || !car.points.length) return;
    // focus on chosen car
    selectedCar = carIdx;
    if (deltaPlayer) deltaPlayer.value = carIdx;
    const playerWindow = segmentWindow(carIdx, lapNum, seg);
    if (!playerWindow) return;

    // Find best segment across all cars/laps by time
    let best = { carIdx, lap: lapNum, time: playerWindow.end - playerWindow.start, window: playerWindow };
    (cars || []).forEach((c, idx) => {
      (c.segmentsLap || []).forEach((s) => {
        if (s.segment !== segIdx || !isFinite(s.time) || s.time <= 0) return;
        if (s.time < best.time) {
          const win = segmentWindow(idx, s.lap, seg);
          if (win) {
            best = { carIdx: idx, lap: s.lap, time: s.time, window: win };
          }
        }
      });
    });

    ghostRestore = { time: currentTime, bounds: boundsOverride, playing: playing };
    boundsOverride = segmentBounds(seg);
    ghostState = {
      active: true,
      carIdx: best.carIdx,
      lap: best.lap,
      playerStart: playerWindow.start,
      playerEnd: playerWindow.end,
      ghostStart: best.window.start,
      ghostEnd: best.window.end,
    };
    ghostMode = true;
    freezePlayback = false;
    playing = true;
    currentTime = playerWindow.start;
    lastTs = performance.now();
    renderStatic();
    if (window.innerWidth <= 900) {
      window.scrollTo({ top: 0, behavior: "smooth" });
    }
    requestAnimationFrame(tick);
  }

  function segmentWindow(carIdx, lapNum, seg) {
    const cars = data?.cars || [];
    const car = cars[carIdx];
    if (!car || !seg || !car.points || !car.points.length) return null;
    let startTime = null, endTime = null;
    car.points.forEach((p) => {
      if (p.lap !== lapNum) return;
      if (startTime === null && p.relS >= seg.startS) {
        startTime = p.time;
      }
      if (p.relS <= seg.endS) {
        endTime = p.time;
      }
    });
    if (startTime === null || endTime === null || endTime <= startTime) return null;
    return { start: startTime, end: endTime };
  }

  function segmentBounds(seg) {
    if (!seg || !data || !data.master) return null;
    const pts = (data.master || []).filter((p) => p.relS >= seg.startS && p.relS <= seg.endS);
    if (!pts.length) return null;
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    pts.forEach((p) => {
      minX = Math.min(minX, p.x);
      minY = Math.min(minY, p.y);
      maxX = Math.max(maxX, p.x);
      maxY = Math.max(maxY, p.y);
    });
    const pad = 50;
    return {
      bounds: { minX: minX - pad, minY: minY - pad, maxX: maxX + pad, maxY: maxY + pad },
      shift: { x: 0, y: 0 },
    };
  }

  function ms(x) { return (x * 1000).toFixed(0); }
  function fmt(t) {
    if (!isFinite(t)) return "-";
    const s = Math.floor(t);
    const msPart = Math.floor((t - s) * 1000);
    const mm = Math.floor(s / 60);
    const ss = s % 60;
    return `${mm}:${ss.toString().padStart(2, "0")}.${msPart.toString().padStart(3, "0")}`;
  }

  function notifyEvent(ev) {
    if (ghostState.active) return; // suppress toasts during ghost playback
    const ttype = (ev.type || "").toLowerCase();
    if (!toastContainer || !ev || !majorEvents.has(ttype) || (notifyTypes.size && !notifyTypes.has(ttype))) return;
    const el = document.createElement("div");
    el.className = "toast";
    const title = (ev.source || ev.target || "").split("/").pop() || "Event";
    const carColor = colorForSource(ev.source || ev.target);
    const body = prettyToastBody(ttype, ev);
    const lap = ev.lap ?? "?";
    const t = isFinite(ev.time) ? ev.time.toFixed(2) : "?";
    el.innerHTML = `
      <div class="toast-type" style="${carColor ? `color:${carColor}` : ""}">${title}</div>
      <div class="toast-note">${body}</div>
      <div class="toast-meta">t=${t}s · lap ${lap}</div>
    `;
    if (carColor) {
      el.style.borderColor = `${carColor}88`;
      el.style.boxShadow = `0 0 0 2px ${carColor}22`;
    }
    el.addEventListener("click", () => el.remove());
    toastContainer.appendChild(el);
    setTimeout(() => el.remove(), 4200);
  }

  function checkNotifications(prev, now) {
    if (!sortedEvents || sortedEvents.length === 0) return;
    if (!isFinite(prev) || !isFinite(now) || now <= prev) return;
    for (let i = 0; i < sortedEvents.length; i++) {
      const ev = sortedEvents[i];
      if (!isFinite(ev.time)) continue;
      if (ev.time > prev && ev.time <= now && majorEvents.has((ev.type || "").toLowerCase())) {
        notifyEvent(ev);
      }
      if (ev.time > now) break;
    }
  }

  function latestMajorEventLabel(car, t) {
    if (!car || !car.source || !sortedEvents || !sortedEvents.length) return "";
    const src = car.source;
    let best = null;
    for (let i = sortedEvents.length - 1; i >= 0; i--) {
      const ev = sortedEvents[i];
      if (!isFinite(ev.time) || ev.time > t) continue;
      if (ev.source !== src) continue;
      const type = (ev.type || "").toLowerCase();
      if (!majorEvents.has(type)) continue;
      best = ev;
      break;
    }
    if (!best) return "";
    // Only show if fairly recent (e.g., within 5s)
    if (isFinite(best.time) && t - best.time > 5) return "";
    return best.type;
  }

  function colorForSource(src) {
    if (!src) return "";
    if (carColorMap.has(src)) return carColorMap.get(src);
    const cars = data?.cars || [];
    const idx = cars.findIndex((c) => c && c.source === src);
    if (idx >= 0) return palette[idx % palette.length];
    return "";
  }

  function prettyToastBody(ttype, ev) {
    switch (ttype) {
      case "collision": return "💥 Collision!";
      case "early_brake": return "🐢 Early Brake";
      case "late_brake": return "🚀 Late Brake";
      case "pole_gain": return "🏁⬆️ Took Pole Position";
      case "pole_loss": return "🏁⬇️ Lost Pole Position";
      case "position_gain": {
        const pos = isFinite(ev.racePosition) ? ` P${ev.racePosition}` : "";
        return `📈 Position +1${pos}`;
      }
      case "position_loss": {
        const pos = isFinite(ev.racePosition) ? ` P${ev.racePosition}` : "";
        return `📉 Position -1${pos}`;
      }
      case "surface": return "🌍 Surface Change";
      case "traction": return "🌀 Traction Loss";
      case "overtake": return "⚡ Overtake";
      default:
        return (ev.note || (ev.type || "event")).replace(/^./, (c) => c.toUpperCase());
    }
  }

  function buildLaps() {
    lapsEl.innerHTML = "";
    (data.cars || []).forEach((car, idx) => {
      const color = palette[idx % palette.length];
      const title = document.createElement("h2");
      title.textContent = car.source || `Car ${idx + 1}`;
      title.style.color = color;
      lapsEl.appendChild(title);
      if (!car.lapTimes || car.lapTimes.length === 0) {
        const p = document.createElement("div");
        p.textContent = "No lap data";
        p.style.color = "#888";
        lapsEl.appendChild(p);
        return;
      }
      const wrap = document.createElement("div");
      wrap.className = "table-wrap";
      const table = document.createElement("table");
      const head = document.createElement("tr");
      head.innerHTML = "<th>Lap</th><th>Lap Time</th><th>S1</th><th>Δ</th><th>S2</th><th>Δ</th><th>S3</th><th>Δ</th>";
      table.appendChild(head);
      car.lapTimes.forEach((lt) => {
        const row = document.createElement("tr");
        const deltas = lt.sectorDelta || [];
        const secs = lt.sectorTime || [];
        row.innerHTML = `
          <td>${lt.lap}</td>
          <td>${fmt(lt.lapTime)}</td>
          <td>${fmt(secs[0] ?? NaN)}</td>
          <td class="${deltaClass(deltas[0])}">${deltaText(deltas[0])}</td>
          <td>${fmt(secs[1] ?? NaN)}</td>
          <td class="${deltaClass(deltas[1])}">${deltaText(deltas[1])}</td>
          <td>${fmt(secs[2] ?? NaN)}</td>
          <td class="${deltaClass(deltas[2])}">${deltaText(deltas[2])}</td>
        `;
        table.appendChild(row);
      });
      wrap.appendChild(table);
      lapsEl.appendChild(wrap);
    });
  }

  function buildCornerStats() {
    if (!cornerStatsEl) return;
    cornerStatsEl.innerHTML = "";
    if (!data || !data.corners || !data.corners.length) {
      cornerStatsEl.textContent = "No corners detected";
      cornerStatsEl.style.color = "#888";
      return;
    }
    const car = selectedCar !== null ? (data.cars || [])[selectedCar] : (data.cars || [])[0];
    if (!car || !car.corners || !car.corners.length) {
      cornerStatsEl.textContent = "No corner stats for this car";
      cornerStatsEl.style.color = "#888";
      return;
    }
    cornerStatsEl.style.color = "";
    const wrap = document.createElement("div");
    wrap.className = "table-wrap";
    const table = document.createElement("table");
    const head = document.createElement("tr");
    head.innerHTML = "<th>#</th><th>Dir</th><th>Angle</th><th>Entry</th><th>Min</th><th>Exit</th>";
    table.appendChild(head);
    const statsByCorner = new Map();
    car.corners.forEach((c) => statsByCorner.set(c.corner, c));
    data.corners.forEach((c) => {
      const stat = statsByCorner.get(c.index);
      const dir = c.direction || "";
      const angle = isFinite(c.angleDeg) ? `${c.angleDeg.toFixed(1)}°` : "";
      const unitLabel = unit === "kmh" ? "km/h" : "mph";
      const entry = stat ? (unit === "kmh" ? stat.entryKMH : stat.entryMPH) : NaN;
      const min = stat ? (unit === "kmh" ? stat.minKMH : stat.minMPH) : NaN;
      const exit = stat ? (unit === "kmh" ? stat.exitKMH : stat.exitMPH) : NaN;
      const row = document.createElement("tr");
      row.style.cursor = "pointer";
      if (selectedCorner === c.index) {
        row.style.background = "rgba(255,177,0,0.12)";
      }
      row.innerHTML = `
        <td>${c.index + 1}</td>
        <td>${dir}</td>
        <td>${angle}</td>
        <td>${isFinite(entry) ? entry.toFixed(1) : "--"} ${unitLabel}</td>
        <td>${isFinite(min) ? min.toFixed(1) : "--"} ${unitLabel}</td>
        <td>${isFinite(exit) ? exit.toFixed(1) : "--"} ${unitLabel}</td>
      `;
      row.addEventListener("click", () => {
        selectedCorner = selectedCorner === c.index ? null : c.index;
        buildCornerStats();
        renderStatic();
        draw();
      });
      table.appendChild(row);
    });
    wrap.appendChild(table);
    cornerStatsEl.appendChild(wrap);

    // Per-lap table
    if (car.cornersLap && car.cornersLap.length) {
      const subhead = document.createElement("h4");
      subhead.className = "collapsible";
      subhead.dataset.target = "cornerLapSection";
      subhead.style.margin = "8px 0 4px";
      subhead.style.color = "var(--muted)";
      subhead.style.fontSize = "14px";
      subhead.style.display = "flex";
      subhead.style.alignItems = "center";
      subhead.style.justifyContent = "space-between";
      subhead.style.width = "100%";
      const labelSpan = document.createElement("span");
      labelSpan.textContent = "Per-lap (selected car)";
      labelSpan.style.color = "var(--muted)";
      labelSpan.style.flex = "1";
      const chevron = document.createElement("span");
      chevron.className = "chev";
      chevron.textContent = "›";
      subhead.appendChild(labelSpan);
      subhead.appendChild(chevron);
      const wrap2 = document.createElement("div");
      wrap2.id = "cornerLapSection";
      wrap2.className = "table-wrap";
      const table2 = document.createElement("table");
      table2.style.marginTop = "0";
      const head2 = document.createElement("tr");
      head2.innerHTML = "<th>Lap</th><th>Corner</th><th>Entry</th><th>Min</th><th>Exit</th>";
      table2.appendChild(head2);
      const unitLabel2 = unit === "kmh" ? "km/h" : "mph";
      // sort by lap then corner
      const sorted = [...car.cornersLap].sort((a, b) => a.lap - b.lap || a.corner - b.corner);
      sorted.forEach((s) => {
        const entry = unit === "kmh" ? s.entryKMH : s.entryMPH;
        const min = unit === "kmh" ? s.minKMH : s.minMPH;
        const exit = unit === "kmh" ? s.exitKMH : s.exitMPH;
        const row = document.createElement("tr");
        if (selectedCorner === s.corner) {
          row.style.background = "rgba(255,177,0,0.12)";
        }
        row.innerHTML = `
          <td>${s.lap}</td>
          <td>${s.corner + 1}</td>
          <td>${isFinite(entry) ? entry.toFixed(1) : "--"} ${unitLabel2}</td>
          <td>${isFinite(min) ? min.toFixed(1) : "--"} ${unitLabel2}</td>
          <td>${isFinite(exit) ? exit.toFixed(1) : "--"} ${unitLabel2}</td>
        `;
        row.addEventListener("click", (e) => {
          e.stopPropagation();
          selectedCorner = selectedCorner === s.corner ? null : s.corner;
          buildCornerStats();
          renderStatic();
          draw();
        });
        table2.appendChild(row);
      });
      wrap2.appendChild(table2);
      let expanded = cornerLapExpanded;
      wrap2.style.display = expanded ? "block" : "none";
      chevron.style.transform = expanded ? "rotate(90deg)" : "rotate(0deg)";
      chevron.style.color = expanded ? "var(--accent)" : "var(--muted)";
      subhead.addEventListener("click", () => {
        expanded = !expanded;
        cornerLapExpanded = expanded;
        wrap2.style.display = expanded ? "block" : "none";
        chevron.style.transform = expanded ? "rotate(90deg)" : "rotate(0deg)";
        chevron.style.color = expanded ? "var(--accent)" : "var(--muted)";
      });
      cornerStatsEl.appendChild(subhead);
      cornerStatsEl.appendChild(wrap2);
    }
  }

  function buildDeltaPlayer() {
    deltaPlayer.innerHTML = "";
    (data.cars || []).forEach((car, idx) => {
      const opt = document.createElement("option");
      opt.value = idx;
      opt.textContent = car.source || `Car ${idx + 1}`;
      deltaPlayer.appendChild(opt);
    });
    if (selectedCar !== null) {
      deltaPlayer.value = selectedCar;
    }
  deltaPlayer.addEventListener("change", () => {
    selectedCar = parseInt(deltaPlayer.value, 10);
    buildLegend();
    buildEvents();
    buildCornerStats();
    buildSegmentStats();
    updateLive();
    updateDelta();
    updateSteering();
    draw();
  });
  }

  function deltaClass(d) {
    if (!isFinite(d) || d === 0) return "";
    return d > 0 ? "delta-neg" : "delta-pos";
  }
  function deltaText(d) {
    if (!isFinite(d)) return "";
    if (d === 0) return "0";
    const sign = d > 0 ? "+" : "";
    return `${sign}${d.toFixed(3)}`;
  }

  function eventColor(t) {
    switch ((t || "").toLowerCase()) {
      case "crash": return "#ff6b6b";
      case "collision": return "#f3a712";
      case "reset": return "#5dd39e";
      case "surface": return "#6bc5ff";
      case "overtake": return "#f78bff";
      case "position_gain": return "#5dd39e";
      case "position_loss": return "#ff6b6b";
      case "pole_gain": return "#00c2ff";
      case "pole_loss": return "#ff6b6b";
      case "early_brake": return "#6bc5ff";
      case "late_brake": return "#ff9f1c";
      default: return "#cdd7e1";
    }
  }

  function buildEvents() {
    eventsEl.innerHTML = "";
    const curLap = currentLapAtTime();
    const list = (data.events || []).filter((ev) => {
      if (selectedCar !== null) {
        const car = (data.cars || [])[selectedCar];
        if (car && ev.source !== car.source && ev.target !== car.source) return false;
      }
      if (curLap > 0 && ev.lap > 0 && ev.lap !== curLap) return false;
      if (!eventTypes.has((ev.type || "").toLowerCase())) return false;
      return true;
    });
    list.forEach((ev) => {
      const row = document.createElement("div");
      row.className = "event-row";
      const color = eventColor(ev.type);
      row.innerHTML = `
        <span class="dot" style="background:${color}"></span>
        <span class="pill" style="background:${color}22;border:1px solid ${color}55">${ev.type}</span>
        <span>${(ev.source || "").split("/").pop()}</span>
        <span>t=${ev.time?.toFixed(2) ?? "?"}s</span>
        <span>lap ${ev.lap ?? "?"}</span>
      `;
      row.style.cursor = "pointer";
      row.addEventListener("click", () => {
        const t = ev.time ?? 0;
        playing = false;
        playBtn.textContent = "Play";
        updateTime(t);
      });
      eventsEl.appendChild(row);
    });
  }

  function buildSegmentStats() {
    if (!segmentStatsEl) return;
    const segmentSection = document.getElementById("segmentSection");
    if (isSoloSprint()) {
      if (segmentSection) segmentSection.style.display = "none";
      return;
    }
    if (segmentSection) segmentSection.style.display = "";
    segmentStatsEl.innerHTML = "";
    if (!data || !data.segments || !data.segments.length) {
      segmentStatsEl.textContent = "No segments detected";
      segmentStatsEl.style.color = "#888";
      return;
    }
    const car = selectedCar !== null ? (data.cars || [])[selectedCar] : (data.cars || [])[0];
    if (!car || !car.segments || !car.segments.length) {
      segmentStatsEl.textContent = "No segment stats for this car";
      segmentStatsEl.style.color = "#888";
      return;
    }
    segmentStatsEl.style.color = "";
    const wrap = document.createElement("div");
    wrap.className = "table-wrap";
    const table = document.createElement("table");
    const head = document.createElement("tr");
    head.innerHTML = "<th>#</th><th>Type</th><th>Entry</th><th>Min</th><th>Avg</th><th>Exit</th><th>Time</th>";
    table.appendChild(head);
    const statsBySeg = new Map();
    car.segments.forEach((s) => statsBySeg.set(s.segment, s));
    const unitLabel = unit === "kmh" ? "km/h" : "mph";
    const formatSpeed = (val) => {
      const n = Number(val);
      return Number.isFinite(n) ? n.toFixed(1) : "--";
    };
    data.segments.forEach((s, idx) => {
      const stat = statsBySeg.get(idx);
      const row = document.createElement("tr");
      const entryVal = stat ? (unit === "kmh" ? stat.entryKMH : stat.entryMPH) : NaN;
      const minVal = stat ? (unit === "kmh" ? stat.minKMH : stat.minMPH) : NaN;
      const avgVal = stat ? (unit === "kmh" ? stat.avgKMH : stat.avgMPH) : NaN;
      const exitVal = stat ? (unit === "kmh" ? stat.exitKMH : stat.exitMPH) : NaN;
      row.innerHTML = `
        <td>${idx + 1}</td>
        <td>${s.type}</td>
        <td>${formatSpeed(entryVal)} ${unitLabel}</td>
        <td>${formatSpeed(minVal)} ${unitLabel}</td>
        <td>${formatSpeed(avgVal)} ${unitLabel}</td>
        <td>${formatSpeed(exitVal)} ${unitLabel}</td>
        <td>${stat && Number.isFinite(Number(stat.time)) ? Number(stat.time).toFixed(2) : "--"} s</td>
      `;
      table.appendChild(row);
    });
    wrap.appendChild(table);
    segmentStatsEl.appendChild(wrap);
    buildSegmentGhostControls();
  }

  function buildSegmentGhostControls() {
    if (!segmentGhostEl) return;
    if (isSoloSprint()) {
      segmentGhostEl.style.display = "none";
      return;
    }
    segmentGhostEl.style.display = "";
    segmentGhostEl.innerHTML = "";
    const cars = data?.cars || [];
    if (!cars.length) return;
    const carSel = document.createElement("select");
    cars.forEach((c, idx) => {
      const opt = document.createElement("option");
      opt.value = idx;
      opt.textContent = c.source || `Car ${idx + 1}`;
      if (selectedCar === null && idx === 0) opt.selected = true;
      if (selectedCar === idx) opt.selected = true;
      carSel.appendChild(opt);
    });
    const segSel = document.createElement("select");
    (data.segments || []).forEach((s, idx) => {
      const opt = document.createElement("option");
      opt.value = idx;
      opt.textContent = `Segment ${idx + 1} (${s.type})`;
      segSel.appendChild(opt);
    });
    const lapSel = document.createElement("select");
    const fillLaps = () => {
      lapSel.innerHTML = "";
      const carIdx = parseInt(carSel.value, 10);
      const car = cars[carIdx];
      const laps = new Set();
      (car.points || []).forEach((p) => laps.add(p.lap));
      Array.from(laps).sort((a, b) => a - b).forEach((l) => {
        const opt = document.createElement("option");
        opt.value = l;
        opt.textContent = `Lap ${l}`;
        lapSel.appendChild(opt);
      });
    };
    fillLaps();
    carSel.addEventListener("change", fillLaps);
    const btn = document.createElement("button");
    btn.textContent = "Play segment ghost";
    btn.style.marginTop = "6px";
    btn.addEventListener("click", () => {
      const carIdx = parseInt(carSel.value, 10);
      const segIdx = parseInt(segSel.value, 10);
      const lapNum = parseInt(lapSel.value, 10);
      playSegmentGhost(carIdx, segIdx, lapNum);
    });
    segmentGhostEl.appendChild(carSel);
    segmentGhostEl.appendChild(segSel);
    segmentGhostEl.appendChild(lapSel);
    segmentGhostEl.appendChild(btn);
  }

  fetchData();

  function computeMaxTime() {
    let m = 0;
    (data.cars || []).forEach((car) => {
      const pts = car.points || [];
      if (pts.length > 0) {
        m = Math.max(m, pts[pts.length - 1].time);
      }
    });
    return m;
  }

  function updateTime(t) {
    const prev = currentTime;
    currentTime = Math.min(Math.max(0, t), maxTime || 0);
    scrub.value = currentTime;
    timeLabel.textContent = fmt(currentTime);
    if (currentTime > prev) {
      checkNotifications(lastNotifiedTime, currentTime);
      lastNotifiedTime = currentTime;
    } else {
      lastNotifiedTime = currentTime;
    }
    const curLap = currentLapAtTime();
    if (curLap !== lastLapRendered) {
      lastLapRendered = curLap;
      buildEvents();
      renderStatic();
    }
    if (ghostMode && ghostState.active && currentTime >= ghostState.playerEnd) {
      stopGhost();
    }
    updateHUD(true);
    draw();
  }

  playBtn.addEventListener("click", () => {
    playing = !playing;
    playBtn.textContent = playing ? "Pause" : "Play";
    lastTs = performance.now();
    if (playing) requestAnimationFrame(tick);
  });

  scrub.addEventListener("input", (e) => {
    playing = false;
    playBtn.textContent = "Play";
    updateTime(parseFloat(e.target.value));
  });

  if (jumpBackBtn) {
    jumpBackBtn.addEventListener("click", () => {
      updateTime(currentTime - 5);
    });
  }
  if (jumpFwdBtn) {
    jumpFwdBtn.addEventListener("click", () => {
      updateTime(currentTime + 5);
    });
  }

  if (playbackRateSelect) {
    const v = parseFloat(playbackRateSelect.value);
    playbackRate = isFinite(v) && v > 0 ? v : 1;
    playbackRateSelect.addEventListener("change", (e) => {
      const v = parseFloat(e.target.value);
      playbackRate = isFinite(v) && v > 0 ? v : 1;
    });
  }

  function tick(ts) {
    if (!playing && !ghostState.active) return;
    const elapsed = ts - lastTs;
    if (elapsed < frameCapMs) {
      requestAnimationFrame(tick);
      return;
    }
    lastTs = ts;
    const dt = (elapsed / 1000) * playbackRate;
    const nextTime = freezePlayback ? currentTime : currentTime + dt;
    updateTime(nextTime);
    if (currentTime >= maxTime && !freezePlayback) {
      playing = false;
      playBtn.textContent = "Play";
      return;
    }
    requestAnimationFrame(tick);
  }

  function updateHUD(force) {
    const now = performance.now();
    if (!force && now - lastHudUpdate < hudInterval) return;
    lastHudUpdate = now;
    updateDelta();
    updateSteering();
    updateLive();
  }

  function headAtTime(carIdx, t) {
    const car = (data.cars || [])[carIdx];
    if (!car || !car.points || car.points.length === 0) return null;
    const pts = car.points;
    let c = carCursors[carIdx] || 0;
    const last = pts.length - 1;
    if (t <= pts[0].time) {
      carCursors[carIdx] = 0;
      return { ...pts[0] };
    }
    if (t >= pts[last].time) {
      carCursors[carIdx] = last;
      return { ...pts[last] };
    }
    if (t < pts[c].time || t > pts[c+1]?.time) {
      // binary search
      let lo = 0, hi = last;
      while (hi - lo > 1) {
        const mid = (hi + lo) >> 1;
        if (pts[mid].time <= t) lo = mid; else hi = mid;
      }
      c = lo;
    } else {
      while (c + 1 < last && pts[c + 1].time < t) {
        c++;
      }
    }
    const p1 = pts[c];
    const p2 = pts[c + 1];
    const span = p2.time - p1.time || 1;
    const alpha = (t - p1.time) / span;
    carCursors[carIdx] = c;
    return {
      masterX: p1.masterX + (p2.masterX - p1.masterX) * alpha,
      masterY: p1.masterY + (p2.masterY - p1.masterY) * alpha,
      relS: p1.relS + (p2.relS - p1.relS) * alpha,
      lap: p1.lap,
      speedMPH: p1.speedMPH + (p2.speedMPH - p1.speedMPH) * alpha,
      speedKMH: p1.speedKMH + (p2.speedKMH - p1.speedKMH) * alpha,
      gear: p1.gear,
      delta: p1.delta + (p2.delta - p1.delta) * alpha,
      longAcc: p1.longAcc + (p2.longAcc - p1.longAcc) * alpha,
      latAcc: p1.latAcc + (p2.latAcc - p1.latAcc) * alpha,
      yawRate: p1.yawRate + (p2.yawRate - p1.yawRate) * alpha,
      yawDegS: p1.yawDegS + (p2.yawDegS - p1.yawDegS) * alpha,
      throttle: p1.throttle + (p2.throttle - p1.throttle) * alpha,
      brake: p1.brake + (p2.brake - p1.brake) * alpha,
      steerDeg: p1.steerDeg + (p2.steerDeg - p1.steerDeg) * alpha,
    };
  }

  function updateDelta() {
    const idx = parseInt(deltaPlayer.value || "0", 10) || 0;
    const car = (data.cars || [])[idx];
    if (!car || !car.points || car.points.length === 0) {
      deltaCtx.clearRect(0, 0, deltaCanvas.clientWidth, deltaCanvas.clientHeight);
      deltaInfo.textContent = "No data";
      return;
    }
    const head = positionAtTime(car.points, currentTime);
    if (!head) {
      deltaCtx.clearRect(0, 0, deltaCanvas.clientWidth, deltaCanvas.clientHeight);
      deltaInfo.textContent = "";
      return;
    }
    const lap = head.lap;
    const deltas = car.points.filter((p) => p.lap === lap && isFinite(p.delta));
    deltas.sort((a, b) => a.relS - b.relS);
    if (deltas.length === 0) {
      deltaCtx.clearRect(0, 0, deltaCanvas.clientWidth, deltaCanvas.clientHeight);
      deltaInfo.textContent = "";
      return;
    }
    const w = deltaCanvas.clientWidth;
    const h = deltaCanvas.clientHeight;
    deltaCtx.clearRect(0, 0, w, h);
    const xs = deltas.map((d) => d.relS);
    const ys = deltas.map((d) => d.delta || 0);
    const minX = Math.min(...xs), maxX = Math.max(...xs);
    let maxAbs = Math.max(Math.abs(Math.min(...ys)), Math.abs(Math.max(...ys)));
    if (maxAbs === 0) maxAbs = 0.1;
    const minY = -maxAbs;
    const maxY = maxAbs;
    const toX = (v) => ((v - minX) / (maxX - minX || 1)) * w;
    // Positive delta (slower) is below midline; negative (faster) above.
    const mid = h / 2;
    const toY = (v) => mid + (v / maxAbs) * (h / 2);

    // zero line centered
    deltaCtx.strokeStyle = "rgba(255,255,255,0.3)";
    deltaCtx.lineWidth = 1;
    deltaCtx.beginPath();
    deltaCtx.moveTo(0, h / 2);
    deltaCtx.lineTo(w, h / 2);
    deltaCtx.stroke();

    // Build filled path closed to midline
    const fillPath = new Path2D();
    deltas.forEach((p, i) => {
      const x = toX(p.relS);
      const y = toY(p.delta || 0);
      if (i === 0) {
        fillPath.moveTo(x, mid);
        fillPath.lineTo(x, y);
      } else {
        fillPath.lineTo(x, y);
      }
      if (i === deltas.length - 1) {
        fillPath.lineTo(x, mid);
        fillPath.closePath();
      }
    });

    // Shade above (ahead/negative) in green
    deltaCtx.save();
    deltaCtx.beginPath();
    deltaCtx.rect(0, 0, w, mid);
    deltaCtx.clip();
    deltaCtx.fillStyle = "rgba(93, 211, 158, 0.25)";
    deltaCtx.fill(fillPath);
    deltaCtx.restore();

    // Shade below (behind/positive) in red
    deltaCtx.save();
    deltaCtx.beginPath();
    deltaCtx.rect(0, mid, w, mid);
    deltaCtx.clip();
    deltaCtx.fillStyle = "rgba(255, 107, 107, 0.25)";
    deltaCtx.fill(fillPath);
    deltaCtx.restore();

    // delta line
    deltaCtx.strokeStyle = palette[0];
    deltaCtx.lineWidth = 1.5;
    deltaCtx.beginPath();
    deltas.forEach((p, i) => {
      const x = toX(p.relS);
      const y = toY(p.delta || 0);
      if (i === 0) deltaCtx.moveTo(x, y); else deltaCtx.lineTo(x, y);
    });
    deltaCtx.stroke();

    // zero line on top again
    deltaCtx.strokeStyle = "rgba(255,255,255,0.4)";
    deltaCtx.lineWidth = 1;
    deltaCtx.beginPath();
    deltaCtx.moveTo(0, h / 2);
    deltaCtx.lineTo(w, h / 2);
    deltaCtx.stroke();

    deltaInfo.textContent = `${car.source} lap ${lap} delta vs best sectors`;
  }

  function updateSteering() {
    if (!steerCanvas || !steerCtx) return;
    const idx = selectedCar !== null ? selectedCar : parseInt(deltaPlayer.value || "0", 10) || 0;
    const car = (data.cars || [])[idx];
    if (!car || !car.points || car.points.length === 0) {
      steerCtx.clearRect(0, 0, steerCanvas.clientWidth, steerCanvas.clientHeight);
      if (steerInfo) steerInfo.textContent = "";
      return;
    }
    const head = positionAtTime(car.points, currentTime);
    if (!head) return;
    const lap = head.lap;
    const points = car.points.filter((p) => p.lap === lap && isFinite(p.steerDeg));
    points.sort((a, b) => a.relS - b.relS);
    if (points.length === 0) {
      steerCtx.clearRect(0, 0, steerCanvas.clientWidth, steerCanvas.clientHeight);
      if (steerInfo) steerInfo.textContent = "";
      return;
    }
    const w = steerCanvas.clientWidth;
    const h = steerCanvas.clientHeight;
    steerCtx.clearRect(0, 0, w, h);
    const xs = points.map((p) => p.relS);
    const ys = points.map((p) => p.steerDeg); // deg
    const minX = Math.min(...xs), maxX = Math.max(...xs);
    let maxAbs = Math.max(Math.abs(Math.min(...ys)), Math.abs(Math.max(...ys)));
    if (maxAbs === 0) maxAbs = 0.1;
    const mid = h / 2;
    const toX = (v) => ((v - minX) / (maxX - minX || 1)) * w;
    const toY = (v) => mid + (v / maxAbs) * (h / 2);

    steerCtx.strokeStyle = "rgba(255,255,255,0.3)";
    steerCtx.lineWidth = 1;
    steerCtx.beginPath();
    steerCtx.moveTo(0, mid);
    steerCtx.lineTo(w, mid);
    steerCtx.stroke();

    steerCtx.strokeStyle = palette[idx % palette.length];
    steerCtx.lineWidth = 1.5;
    steerCtx.beginPath();
    points.forEach((p, i) => {
      const x = toX(p.relS);
      const y = toY(ys[i]);
      if (i === 0) steerCtx.moveTo(x, y); else steerCtx.lineTo(x, y);
    });
    steerCtx.stroke();

    if (steerInfo) {
      steerInfo.textContent = `${car.source} lap ${lap} steering (deg), ±${maxAbs.toFixed(1)} scale`;
    }
  }

  function updateLive() {
    if (!liveEl) return;
    liveEl.innerHTML = "";
    filteredCars().forEach((car, idx) => {
      const globalIdx = selectedCar !== null ? selectedCar : idx;
      const head = positionAtTime(car.points || [], currentTime);
      const speed = unit === "kmh" ? head?.speedKMH ?? null : head?.speedMPH ?? null;
      const gear = head?.gear ?? null;
      const throttle = head?.throttle ?? 0;
      const brake = head?.brake ?? 0;
      const latG = head && isFinite(head.latAcc) ? head.latAcc / 9.81 : null;
      const longG = head && isFinite(head.longAcc) ? head.longAcc / 9.81 : null;
      const yaw = head && isFinite(head.yawRate) ? head.yawRate * (180 / Math.PI) : null;
      const lineDev = head && isFinite(head.distToLine) ? head.distToLine : null;
      const susp = head ? [head.suspFL, head.suspFR, head.suspRL, head.suspRR] : [];
      const tempsRaw = head ? [head.tireTempFL, head.tireTempFR, head.tireTempRL, head.tireTempRR] : [];
      const temps = tempsRaw.map((v) => {
        if (!isFinite(v)) return v;
        return tempUnit === "f" ? v * 9/5 + 32 : v; // assume source is C; leave as-is for C
      });
      const row = document.createElement("div");
      row.className = "live-row";
      const name = document.createElement("span");
      name.textContent = car.source || `Car ${globalIdx + 1}`;
      const vals = document.createElement("span");
      vals.textContent = `${speed ? speed.toFixed(1) : "--"} ${unit === "kmh" ? "km/h" : "mph"} | Gear ${gear ?? "-"}`;
      row.appendChild(name);
      row.appendChild(vals);
      liveEl.appendChild(row);

      const bar = document.createElement("div");
      bar.className = "speed-bar";
      const fill = document.createElement("div");
      fill.className = "speed-fill";
      const capped = Math.max(0, Math.min(1, (speed || 0) / maxSpeedEstimate()));
      fill.style.width = `${capped * 100}%`;
      bar.appendChild(fill);
      liveEl.appendChild(bar);
      const label = document.createElement("span");
      label.className = "speed-label";
      label.textContent = `0 - ${maxSpeedEstimate()} ${unit === "kmh" ? "km/h" : "mph"} scale`;
      liveEl.appendChild(label);

      // Controls / dynamics meters
      const shouldShowAdvanced = showControls;
      if (shouldShowAdvanced) {
        liveEl.appendChild(makeMeter("Throttle", throttle, "#5dd39e"));
        liveEl.appendChild(makeMeter("Brake", brake, "#ff6b6b"));
      }

      if (shouldShowAdvanced) {
        const dynamics = document.createElement("div");
        dynamics.className = "live-row";
        const dynLabel = document.createElement("span");
        dynLabel.textContent = "Lat/Long/Yaw";
        const dynVals = document.createElement("span");
        dynVals.textContent = `${latG !== null ? latG.toFixed(2) : "--"}g / ${longG !== null ? longG.toFixed(2) : "--"}g / ${yaw !== null ? yaw.toFixed(1) : "--"}°/s`;
        dynamics.appendChild(dynLabel);
        dynamics.appendChild(dynVals);
        liveEl.appendChild(dynamics);
      }

      if (shouldShowAdvanced && lineDev !== null) {
        const lineRow = document.createElement("div");
        lineRow.className = "live-row";
        const l1 = document.createElement("span");
        l1.textContent = "Line deviation";
        const l2 = document.createElement("span");
        const maxAbs = telemetryRanges.line.maxAbs || 0;
        const pct = maxAbs > 0 ? Math.max(-100, Math.min(100, (lineDev / maxAbs) * 100)) : 0;
        l2.textContent = `${pct.toFixed(0)}%`;
        lineRow.appendChild(l1);
        lineRow.appendChild(l2);
        liveEl.appendChild(lineRow);

        // simple bar showing left/right offset (-100..100)
        const barWrap = document.createElement("div");
        barWrap.className = "meter";
        const bar = document.createElement("div");
        bar.className = "meter-bar";
        bar.style.position = "relative";
        const mid = document.createElement("div");
        mid.style.position = "absolute";
        mid.style.left = "50%";
        mid.style.top = "0";
        mid.style.bottom = "0";
        mid.style.width = "1px";
        mid.style.background = "rgba(255,255,255,0.4)";
        const fill = document.createElement("div");
        fill.className = "meter-fill";
        fill.style.background = pct >= 0 ? "#5dd39e" : "#ff6b6b";
        const pctAbs = Math.min(100, Math.abs(pct));
        if (pct >= 0) {
          fill.style.left = "50%";
          fill.style.width = `${pctAbs / 2}%`;
        } else {
          fill.style.left = `${50 - pctAbs / 2}%`;
          fill.style.width = `${pctAbs / 2}%`;
        }
        fill.style.position = "absolute";
        fill.style.top = "0";
        fill.style.bottom = "0";
        bar.appendChild(fill);
        bar.appendChild(mid);
        barWrap.appendChild(bar);
        liveEl.appendChild(barWrap);
      }

      // Suspension grid
      if (shouldShowAdvanced && susp.length === 4) {
        liveEl.appendChild(makeWheelGrid("Suspension (m)", susp, (v) => v !== undefined ? v.toFixed(3) : "--", telemetryRanges.susp));
      }
      // Tire temp grid
      if (shouldShowAdvanced && temps.length === 4) {
        const labelUnit = tempUnit === "f" ? "°F" : "°C";
        liveEl.appendChild(makeWheelGrid(`Tire Temp (${labelUnit})`, temps, (v) => v !== undefined ? v.toFixed(1) : "--", telemetryRanges.temp));
      }
    });
  }

  function makeMeter(label, value, color) {
    const wrap = document.createElement("div");
    wrap.className = "meter";
    const row = document.createElement("div");
    row.className = "live-row meter-row";
    const l = document.createElement("span");
    l.textContent = label;
    const v = document.createElement("span");
    v.textContent = `${(value * 100).toFixed(0)}%`;
    row.appendChild(l);
    row.appendChild(v);
    const bar = document.createElement("div");
    bar.className = "meter-bar";
    const fill = document.createElement("div");
    fill.className = "meter-fill";
    fill.style.background = color;
    fill.style.width = `${Math.max(0, Math.min(1, value)) * 100}%`;
    bar.appendChild(fill);
    wrap.appendChild(row);
    wrap.appendChild(bar);
    return wrap;
  }

  function maxSpeedEstimate() {
    const speeds = [];
    (data.cars || []).forEach((car) => {
      (car.points || []).forEach((p) => {
        const s = unit === "kmh" ? p.speedKMH : p.speedMPH;
        if (isFinite(s)) speeds.push(s);
      });
    });
    if (!speeds.length) return unit === "kmh" ? 300 : 200;
    speeds.sort((a, b) => a - b);
    const idx = Math.floor(speeds.length * 0.95);
    return Math.max(unit === "kmh" ? 100 : 60, Math.round(speeds[idx]));
  }

  function makeWheelGrid(title, vals, fmtFn, range) {
    const wrap = document.createElement("div");
    const heading = document.createElement("div");
    heading.className = "wheel-title";
    heading.textContent = title;
    wrap.appendChild(heading);
    const grid = document.createElement("div");
    grid.className = "wheel-grid";
    const labels = ["FL", "FR", "RL", "RR"];
    for (let i = 0; i < 4; i++) {
      const cell = document.createElement("div");
      cell.className = "wheel-cell";
      const val = vals[i];
      const txt = `${labels[i]} ${val !== undefined && isFinite(val) ? fmtFn(val) : "--"}`;
      cell.textContent = txt;
      if (val !== undefined && isFinite(val)) {
        const color = rampColor(val, range);
        if (color) {
          cell.style.background = color;
          cell.style.color = "#000";
        }
      }
      grid.appendChild(cell);
    }
    wrap.appendChild(grid);
    return wrap;
  }

  function computeTelemetryRanges() {
    telemetryRanges.susp.min = telemetryRanges.temp.min = telemetryRanges.line.min = Infinity;
    telemetryRanges.susp.max = telemetryRanges.temp.max = telemetryRanges.line.max = -Infinity;
    // Ideal slick temps in current unit.
    const idealLowC = 88, idealHighC = 99;
    const idealLow = tempUnit === "f" ? idealLowC * 9/5 + 32 : idealLowC;
    const idealHigh = tempUnit === "f" ? idealHighC * 9/5 + 32 : idealHighC;
    const pad = tempUnit === "f" ? 36 : 20; // ~20°C or 36°F padding outside ideal window
    telemetryRanges.temp.midLow = idealLow;
    telemetryRanges.temp.midHigh = idealHigh;
    (data.cars || []).forEach((car) => {
      (car.points || []).forEach((p) => {
        ["suspFL", "suspFR", "suspRL", "suspRR"].forEach((k) => {
          const v = p[k];
          if (isFinite(v)) {
            telemetryRanges.susp.min = Math.min(telemetryRanges.susp.min, v);
            telemetryRanges.susp.max = Math.max(telemetryRanges.susp.max, v);
          }
        });
        ["tireTempFL", "tireTempFR", "tireTempRL", "tireTempRR"].forEach((k) => {
          let v = p[k];
          if (isFinite(v) && tempUnit === "f") {
            v = v * 9/5 + 32;
          }
          if (isFinite(v)) {
            telemetryRanges.temp.min = Math.min(telemetryRanges.temp.min, v);
            telemetryRanges.temp.max = Math.max(telemetryRanges.temp.max, v);
          }
        });
        if (isFinite(p.distToLine)) {
          telemetryRanges.line.min = Math.min(telemetryRanges.line.min, p.distToLine);
          telemetryRanges.line.max = Math.max(telemetryRanges.line.max, p.distToLine);
        }
      });
    });
    if (!isFinite(telemetryRanges.susp.min) || !isFinite(telemetryRanges.susp.max)) {
      telemetryRanges.susp.min = 0; telemetryRanges.susp.max = 1;
    }
    if (!isFinite(telemetryRanges.temp.min) || !isFinite(telemetryRanges.temp.max)) {
      telemetryRanges.temp.min = idealLow - pad;
      telemetryRanges.temp.max = idealHigh + pad;
    }
    if (!isFinite(telemetryRanges.line.min) || !isFinite(telemetryRanges.line.max)) {
      telemetryRanges.line.min = 0; telemetryRanges.line.max = 0;
    }
    telemetryRanges.line.maxAbs = Math.max(Math.abs(telemetryRanges.line.min), Math.abs(telemetryRanges.line.max));
    // Ensure temp range covers ideal slick window.
    telemetryRanges.temp.min = Math.min(telemetryRanges.temp.min, telemetryRanges.temp.midLow - pad);
    telemetryRanges.temp.max = Math.max(telemetryRanges.temp.max, telemetryRanges.temp.midHigh + pad);
  }
})();
