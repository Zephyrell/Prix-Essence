/* Prix-Essence — frontend */
(function () {
  "use strict";

  // ---- Configuration ----
  const FUELS = [
    { key: "Gazole",  label: "⛽ Diesel", color: "#3b82f6", defaultRadius: 20 },
    { key: "SP95",    label: "SP95",      color: "#22c55e", defaultRadius: 20 },
    { key: "E10",     label: "E10",       color: "#10b981", defaultRadius: 20 },
    { key: "SP98",    label: "SP98",      color: "#eab308", defaultRadius: 20 },
    { key: "E85",     label: "🌽 Éthanol", color: "#a855f7", defaultRadius: 50 },
    { key: "GPLc",    label: "GPLc",      color: "#f97316", defaultRadius: 50 },
  ];
  const FAV_KEY = "prixessence.favs";
  const DEFAULT_VIEW = [46.6, 2.35]; // Paris

  // ---- État global ----
  const state = {
    fuel: "Gazole",
    radius: 20,
    center: null,        // point recherché (ou géoloc), null = vue complète
    stations: [],        // dernière liste chargée
    markers: [],         // L.layerGroup
    favorites: loadFavs(),
  };

  const map = L.map("map", { zoomControl: true }).setView(DEFAULT_VIEW, 6);
  L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
    maxZoom: 19,
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
  }).addTo(map);

  const markerLayer = L.layerGroup().addTo(map);

  // ---- Références DOM ----
  const $ = (id) => document.getElementById(id);
  const fuelPillsEl = $("fuel-pills");
  const statusLine = $("status-line");
  const legendMin = $("legend-min");
  const legendMax = $("legend-max");
  const stationList = $("station-list");
  const panelEmpty = $("panel-empty");
  const listingTitle = $("listing-title");
  const radiusInput = $("radius");
  const radiusLabel = $("radius-label");

  // ---- Helpers ----
  function fmtPrix(v) {
    if (v == null) return "—";
    return v.toFixed(3).replace(".", ",") + " €";
  }
  function priceColor(p, min, max) {
    // t = 0..1 sur [min,max], interpolé vert → jaune → rouge
    const t = max === min ? 0.5 : Math.min(1, Math.max(0, (p - min) / (max - min)));
    let r, g;
    if (t < 0.5) { r = Math.round(255 * (2 * t)); g = 255; }
    else { r = 255; g = Math.round(255 * (1 - 2 * (t - 0.5))); }
    return `rgb(${r},${g},0)`;
  }
  function esc(s) {
    const d = document.createElement("div");
    d.textContent = s == null ? "" : String(s);
    return d.innerHTML;
  }

  // ---- Favoris (localStorage) ----
  function loadFavs() {
    try { return JSON.parse(localStorage.getItem(FAV_KEY)) || []; } catch { return []; }
  }
  function saveFavs() { localStorage.setItem(FAV_KEY, JSON.stringify(state.favorites)); }
  function renderFavs() {
    const el = $("favorites");
    el.innerHTML = "";
    state.favorites.forEach((f, i) => {
      const chip = document.createElement("button");
      chip.className = "fav-chip";
      chip.innerHTML = `📍 ${esc(f.label)} <span class="x">✕</span>`;
      chip.onclick = (e) => {
        if (e.target.closest(".x")) {
          state.favorites.splice(i, 1);
          saveFavs(); renderFavs();
          return;
        }
        setCenter(f.label, +f.lat, +f.lon, true);
      };
      el.appendChild(chip);
    });
  }
  function addFavorite(label, lat, lon) {
    const saved = state.favorites.some((f) => f.lat === lat && f.lon === lon);
    if (saved) return;
    state.favorites.push({ label, lat, lon });
    saveFavs(); renderFavs();
    flash(`📍 « ${label} » ajouté aux favoris`, 1800);
  }
  function flash(msg, ms) {
    const s = statusLine;
    s.innerHTML = esc(msg);
    clearTimeout(s._t);
    s._t = setTimeout(refreshStatus, ms || 4000);
  }

  // ---- Sélecteur de carburant ----
  function renderFuelPills() {
    fuelPillsEl.innerHTML = "";
    FUELS.forEach((f) => {
      const b = document.createElement("button");
      b.className = "fuel-pill" + (f.key === state.fuel ? " active" : "");
      b.textContent = f.label;
      b.onclick = () => { state.fuel = f.key; renderFuelPills(); loadStations(); };
      fuelPillsEl.appendChild(b);
    });
  }

  // ---- Statut ----
  async function refreshStatus() {
    try {
      const r = await fetch("/api/status");
      const s = await r.json();
      const dateStr = s.data_date ? "(données du " + new Date(s.data_date).toLocaleString("fr-FR") + ")" : "";
      statusLine.innerHTML = `${s.nb_stations.toLocaleString("fr-FR")} stations ${dateStr} ·
        ${s.nb_stations_avec_carburant ? s.nb_stations_avec_carburant.toLocaleString("fr-FR") + " · " : ""}
        <a href="#" class="popup-link" onclick="return false" title="Rafraîchir">🔄</a>`;
      statusLine.querySelector("a").onclick = (e) => { e.preventDefault(); triggerRefresh(); };
    } catch (err) {
      statusLine.textContent = "Impossible de récupérer le statut.";
    }
  }

  // ---- Données ----
  async function loadStations() {
    const params = new URLSearchParams({ fuel: state.fuel });
    if (state.center) {
      params.set("lat", state.center.lat);
      params.set("lon", state.center.lon);
      params.set("radius", state.radius * 1000);
    }
    params.set("limit", "500");
    listingTitle.textContent = state.center ? `Autour de l'emplacement` : "Toutes les stations";
    try {
      const r = await fetch("/api/stations?" + params.toString());
      if (!r.ok) throw new Error("HTTP " + r.status);
      const data = await r.json();
      state.stations = data.stations || [];
      renderMap();
      renderList();
    } catch (err) {
      statusLine.textContent = "Erreur lors du chargement des stations.";
      console.error(err);
    }
  }

  // ---- Carte ----
  function renderMap() {
    markerLayer.clearLayers();
    if (!state.stations.length) {
      legendMin.textContent = "—"; legendMax.textContent = "—";
      return;
    }
    const prices = state.stations
      .map((s) => s.prix)
      .filter((p) => p != null);
    const min = Math.min(...prices), max = Math.max(...prices);
    legendMin.textContent = fmtPrix(min);
    legendMax.textContent = fmtPrix(max);

    state.stations.forEach((s) => {
      if (s.prix == null) return;
      const color = priceColor(s.prix, min, max);
      const price = s.prix.toFixed(3).replace(".", ",");
      // Point coloré (petit, lisible à tous zooms) + étiquette permanente du
      // prix, affichée dans une petite bulle sombre au-dessus du point.
      const m = L.circleMarker([s.lat, s.lon], {
        radius: 7,
        color: "#0f172a",
        weight: 1.5,
        fillColor: color,
        fillOpacity: 0.95,
      });
      m.bindPopup(popupHTML(s));
      m.bindTooltip(`<b>${price}</b> €`, {
        direction: "top",
        offset: [0, -10],
        opacity: 1,
        permanent: true,
        className: "price-tip",
      });
      m.on("click", () => selectStation(s));
      markerLayer.addLayer(m);
    });

    if (!state.center && state.stations.length) {
      // Zoom global sur les stations affichées
      try { map.fitBounds(markerLayer.getBounds(), { padding: [20, 20] }); } catch {}
    }
  }

  function popupHTML(s) {
    const fuel = state.fuel;
    return `
      <div>
        <div class="popup-price">${fmtPrix(s.prix)}</div>
        <div class="popup-title">${esc(s.adresse || "Station")}</div>
        <div class="popup-meta">${esc(s.ville || "")} ${esc(s.cp || "")}${s.distance_km != null ? " · " + s.distance_km.toFixed(1).replace(".", ",") + " km" : ""}</div>
        <div class="popup-meta">${fuel} · maj. ${s.maj ? new Date(s.maj).toLocaleString("fr-FR") : "—"}</div>
        <div class="spark-label">Évolution ${fuel} · 14 jours</div>
        <canvas class="spark" data-station="${esc(s.id)}" data-price="${s.prix}"></canvas>
        <a href="#" class="popup-link" data-idx="fav">★ Mémoriser ce lieu</a>
      </div>`;
  }

  function addPopupEvents(id, station) {
    const el = document.getElementById(id);
    if (!el) return;
    el.querySelector('[data-idx="fav"]').onclick = (e) => {
      e.preventDefault();
      addFavorite(station.ville || station.adresse, station.lat, station.lon);
    };
    const draft = el.querySelector("canvas[data-station]");
    if (draft) {
      const sid = draft.dataset.station;
      const canvas = document.createElement("canvas");
      draft.replaceWith(canvas);
      loadSpark(canvas, sid, state.fuel);
    }
  }

  // Liaison des événements popup après ouverture
  map.on("popupopen", (ev) => {
    const lyr = ev.popup._source;
    const st = state.stations.find((s) => s.id === lyr.options._sid);
    // Le marker porte _sid ; si absent on le retrouve par lat/lon
    const st2 = st || state.stations.find((s) => s.lat === lyr.getLatLng().lat && s.lon === lyr.getLatLng().lng);
    if (st2) addPopupEvents(st2.id, st2);
  });

  // ---- Liste latérale ----
  function renderList() {
    stationList.innerHTML = "";
    const sorted = state.stations.slice();
    sorted.sort((a, b) => (a.distance_km ?? 1e9) - (b.distance_km ?? 1e9));
    const prices = sorted.map((s) => s.prix).filter((p) => p != null);
    const min = prices.length ? Math.min(...prices) : 0;
    const max = prices.length ? Math.max(...prices) : 0;
    panelEmpty.hidden = sorted.length !== 0;

    sorted.forEach((s) => {
      const li = document.createElement("li");
      li.className = "station-item";
      const hasPrice = s.prix != null;
      const color = hasPrice ? priceColor(s.prix, min, max) : "#6366f1";
      li.innerHTML = `
        <div class="s-row">
          <span class="s-name">${esc(s.ville || "Ville")}</span>
          <span class="s-price" style="color:${hasPrice ? color : "var(--muted)"}">${fmtPrix(s.prix)}</span>
        </div>
        <div class="s-meta">${esc(s.adresse || "")}${s.distance_km != null ? " · " + s.distance_km.toFixed(1).replace(".", ",") + " km" : ""}</div>
        <div class="s-bar"><i style="width:${hasPrice ? Math.round((1 - (s.prix - min) / (max - min || 1)) * 100) : 0}%;background:${color}"></i></div>`;
      li.onclick = () => {
        selectStation(s);
        map.setView([s.lat, s.lon], 13);
      };
      stationList.appendChild(li);
    });
  }

  function selectStation(s) {
    document.querySelectorAll(".station-item.selected").forEach((n) => n.classList.remove("selected"));
    const idx = state.stations.indexOf(s);
    const items = document.querySelectorAll(".station-item");
    if (items[idx]) items[idx].classList.add("selected");
  }

  // ---- Historique (sparkline) ----
  async function loadSpark(canvas, stationId, fuel) {
    try {
      const r = await fetch(`/api/history?station=${encodeURIComponent(stationId)}&fuel=${encodeURIComponent(fuel)}`);
      if (!r.ok) return;
      const data = await r.json();
      const pts = (data.history || []).map((h) => h.prix).filter((p) => p != null);
      if (pts.length < 2) { drawNoData(canvas); return; }
      drawSpark(canvas, pts);
    } catch { drawNoData(canvas); }
  }

  function drawSpark(canvas, pts) {
    const w = canvas.width = 220;
    const h = canvas.height = 44;
    const ctx = canvas.getContext("2d");
    const min = Math.min(...pts), max = Math.max(...pts);
    const pad = 4;
    const step = (w - pad * 2) / (pts.length - 1 || 1);
    ctx.clearRect(0, 0, w, h);
    ctx.beginPath();
    pts.forEach((p, i) => {
      const x = pad + i * step;
      const y = h - pad - ((p - min) / (max - min || 1)) * (h - pad * 2);
      i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
    });
    ctx.strokeStyle = "#22c55e";
    ctx.lineWidth = 2;
    ctx.stroke();
    const last = pts[pts.length - 1], first = pts[0];
    ctx.fillStyle = "rgba(226,232,240,.7)";
    ctx.font = "9px sans-serif";
    ctx.fillText(first.toFixed(3), pad, h - 2);
    ctx.textAlign = "right";
    ctx.fillText(last.toFixed(3), w - pad, h - 2);
    ctx.textAlign = "left";
    // Variation sur la fenêtre, au-dessus de la courbe.
    const delta = last - first;
    const sign = delta >= 0 ? "+" : "";
    ctx.fillStyle = delta > 0 ? "#ef4444" : "#22c55e";
    ctx.font = "bold 9px sans-serif";
    ctx.fillText(sign + delta.toFixed(3), w - pad, 8);
  }
  function drawNoData(canvas) {
    canvas.width = 220; canvas.height = 44;
    const ctx = canvas.getContext("2d");
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = "rgba(226,232,240,.5)";
    ctx.font = "10px sans-serif";
    ctx.fillText("Aucun historique sur 14 jours", 6, 22);
  }

  // ---- Localisation ----
  function setCenter(label, lat, lon, keepView) {
    state.center = { lat, lon };
    radiusLabel.textContent = state.radius + " km";
    loadStations();
    if (!keepView) map.setView([lat, lon], 11);
  }

  $("btn-geo").onclick = () => {
    if (!navigator.geolocation) { flash("Géolocalisation non supportée."); return; }
    navigator.geolocation.getCurrentPosition(
      (pos) => setCenter("Ma position", pos.coords.latitude, pos.coords.longitude, false),
      () => flash("Impossible d'obtenir la position.", 3000),
      { enableHighAccuracy: true, timeout: 10000 }
    );
  };

  radiusInput.oninput = () => { radiusLabel.textContent = radiusInput.value + " km"; };
  radiusInput.onchange = () => {
    state.radius = +radiusInput.value;
    radiusLabel.textContent = state.radius + " km";
    if (state.center) loadStations();
  };

  // ---- Autocomplete villes ----
  const cityInput = $("city-input");
  const suggestions = $("city-suggestions");
  let suggestTimer = null;
  cityInput.addEventListener("input", () => {
    clearTimeout(suggestTimer);
    const q = cityInput.value.trim();
    if (q.length < 2) { suggestions.hidden = true; return; }
    suggestTimer = setTimeout(async () => {
      const r = await fetch("/api/cities?q=" + encodeURIComponent(q));
      const data = await r.json();
      const cities = data.cities || [];
      suggestions.innerHTML = "";
      if (!cities.length) { suggestions.hidden = true; return; }
      cities.forEach((c) => {
        const li = document.createElement("li");
        li.innerHTML = `${esc(c.ville)} <span class="cp">${esc(c.cp || "")}</span>`;
        li.onclick = () => {
          cityInput.value = `${c.ville} ${c.cp || ""}`;
          suggestions.hidden = true;
          setCenter(c.ville, c.lat, c.lon, false);
        };
        suggestions.appendChild(li);
      });
      suggestions.hidden = false;
    }, 250);
  });
  document.addEventListener("click", (e) => {
    if (!e.target.closest(".city-search")) suggestions.hidden = true;
  });

  // ---- Manuel refresh ----
  async function triggerRefresh() {
    statusLine.textContent = "Rafraîchissement…";
    try {
      const r = await fetch("/api/refresh", { method: "POST" });
      const j = await r.json();
      flash("Données mises à jour : " + j.message);
      loadStations();
    } catch {
      statusLine.textContent = "Échec du rafraîchissement.";
    }
  }

  // ---- Init ----
  renderFuelPills();
  renderFavs();
  refreshStatus();
  loadStations();
})();
