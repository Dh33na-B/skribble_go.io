(() => {
  const normalizeBaseUrl = value => String(value || "").trim().replace(/\/+$/, "");
  const isLoopbackHost = host => ["localhost", "127.0.0.1", "::1"].includes(host);

  const getInitialApiBase = () => {
    const queryBase = normalizeBaseUrl(new URLSearchParams(window.location.search).get("api"));
    if (queryBase) {
      return queryBase;
    }

    try {
      const saved = normalizeBaseUrl(window.localStorage.getItem("scribble.apiBase"));
      if (saved) {
        return saved;
      }
    } catch {
      // Ignore storage errors.
    }

    const pageHost = window.location.hostname || "";
    if (!pageHost || window.location.protocol === "file:" || isLoopbackHost(pageHost)) {
      return "http://localhost:8080";
    }

    const protocol = window.location.protocol === "https:" ? "https:" : "http:";
    return `${protocol}//${pageHost}:8080`;
  };

  const S = {
    api: getInitialApiBase(),
    user: {
      token: "",
      ws: null
    },
    draw: {
      on: false,
      x: 0,
      y: 0
    },
    room: "",
    snap: null
  };

  const E = id => document.getElementById(id);
  const ctx = E("board").getContext("2d");
  const ui = {
    apiBase: E("apiBase"),
    applyBase: E("applyBase"),
    backendPill: E("backendPill"),
    userName: E("userName"),
    userEmail: E("userEmail"),
    userPass: E("userPass"),
    userToken: E("userToken"),
    connectWs: E("connectWs"),
    disconnectWs: E("disconnectWs"),
    roomCode: E("roomCode"),
    createRoom: E("createRoom"),
    joinRoom: E("joinRoom"),
    startGame: E("startGame"),
    registerUser: E("registerUser"),
    loginUser: E("loginUser"),
    snapshot: E("snapshot"),
    scores: E("scores"),
    board: E("board"),
    brushColor: E("brushColor"),
    brushSize: E("brushSize"),
    clearCanvas: E("clearCanvas"),
    chatInput: E("chatInput"),
    guessInput: E("guessInput"),
    sendChat: E("sendChat"),
    sendGuess: E("sendGuess"),
    log: E("log"),
    clearLog: E("clearLog")
  };

  ui.apiBase.value = S.api;

  const log = (message, kind = "") => {
    const d = document.createElement("div");
    d.className = `line ${kind}`.trim();
    d.textContent = `[${new Date().toLocaleTimeString()}] ${message}`;
    ui.log.prepend(d);
    if (ui.log.children.length > 180) {
      ui.log.lastChild.remove();
    }
  };

  const state = text => {
    ui.backendPill.textContent = text;
  };

  const short = value => {
    if (!value) {
      return "n/a";
    }
    return value.length > 10 ? value.slice(0, 8) : value;
  };

  const wsBase = () => {
    const url = new URL(S.api);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    return url.toString().replace(/\/+$/, "");
  };

  const persistApiBase = () => {
    try {
      window.localStorage.setItem("scribble.apiBase", S.api);
    } catch {
      // Ignore storage errors.
    }
  };

  const warnIfLocalhostFromLan = () => {
    const pageHost = window.location.hostname || "";
    if (!pageHost || isLoopbackHost(pageHost)) {
      return;
    }

    try {
      const apiHost = new URL(S.api).hostname;
      if (isLoopbackHost(apiHost)) {
        log("Using localhost API from another device will fail. Set apiBase to your server LAN IP.", "warn");
      }
    } catch {
      // Ignore invalid URL parsing errors.
    }
  };

  const resizeCanvas = () => {
    const dpr = window.devicePixelRatio || 1;
    const r = ui.board.getBoundingClientRect();
    ui.board.width = Math.max(320, Math.floor(r.width)) * dpr;
    ui.board.height = Math.max(260, Math.floor(r.height)) * dpr;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.lineCap = "round";
    ctx.lineJoin = "round";
  };

  const point = ev => {
    const r = ui.board.getBoundingClientRect();
    return {
      x: (ev.clientX - r.left) / r.width,
      y: (ev.clientY - r.top) / r.height
    };
  };

  const drawLine = payload => {
    const width = ui.board.width / (window.devicePixelRatio || 1);
    const height = ui.board.height / (window.devicePixelRatio || 1);
    ctx.strokeStyle = payload.color || "#000";
    ctx.lineWidth = payload.size || 3;
    ctx.beginPath();
    ctx.moveTo(payload.x0 * width, payload.y0 * height);
    ctx.lineTo(payload.x1 * width, payload.y1 * height);
    ctx.stroke();
  };

  const req = async (path, method, body, token) => {
    const headers = {};
    if (body) {
      headers["Content-Type"] = "application/json";
    }
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    }

    const res = await fetch(`${S.api}${path}`, {
      method,
      headers,
      body: body ? JSON.stringify(body) : undefined
    });

    const txt = await res.text();
    let data = {};
    try {
      data = txt ? JSON.parse(txt) : {};
    } catch {
      data = { message: txt };
    }

    if (!res.ok) {
      throw new Error(data.message || txt || `${res.status}`);
    }

    return data;
  };

  const tokenOf = () => {
    if (!S.user.token) {
      throw new Error("User not logged in");
    }

    return S.user.token;
  };

  const wsSend = (type, payload) => {
    if (!S.user.ws || S.user.ws.readyState !== WebSocket.OPEN) {
      throw new Error("Websocket not connected");
    }

    S.user.ws.send(JSON.stringify({ type, payload }));
  };

  const paintScores = scores => {
    ui.scores.innerHTML = "";
    const arr = Object.entries(scores || {}).sort((a, b) => b[1] - a[1]);
    if (!arr.length) {
      ui.scores.innerHTML = '<div class="muted">No scores yet.</div>';
      return;
    }

    arr.forEach(([userID, points]) => {
      const d = document.createElement("div");
      d.className = "scoreItem";
      d.innerHTML = `<span>${short(userID)}</span><strong>${points}</strong>`;
      ui.scores.appendChild(d);
    });
  };

  const onWsEvent = evt => {
    const type = evt.type;
    const payload = evt.payload || {};

    if (type === "state_snapshot") {
      S.snap = payload;
      ui.snapshot.textContent = JSON.stringify(payload);
      paintScores(payload.scores || {});
      log(`snapshot round ${payload.round_number || 0}`);
      return;
    }

    if (type === "draw_stroke") {
      drawLine(payload);
      return;
    }

    if (type === "score_update") {
      paintScores(payload.scores || {});
      log("scores updated", "ok");
      return;
    }

    if (type === "guess_result") {
      log(`guess ${short(payload.user_id)} -> ${payload.is_correct ? "correct" : "wrong"}`, payload.is_correct ? "ok" : "warn");
      return;
    }

    if (type === "player_joined") {
      log(`player joined ${short(evt.user_id)}`, "ok");
      return;
    }

    if (type === "player_left") {
      log(`player left ${short(evt.user_id)}`, "warn");
      return;
    }

    if (type === "chat_message") {
      log(`chat ${short(evt.user_id)}: ${payload.text || ""}`);
      return;
    }

    if (type === "guess_submit") {
      log(`guess ${short(evt.user_id)}: ${payload.text || ""}`);
      return;
    }

    if (type === "round_end") {
      log(`round ended word=${payload.word || ""}`, "ok");
      return;
    }

    if (type === "error") {
      log(`server: ${payload.message || "error"}`, "warn");
      return;
    }

    log(`event ${type}`);
  };

  const connectWs = () => {
    const token = tokenOf();
    const room = ui.roomCode.value.trim().toUpperCase();
    if (!room) {
      throw new Error("Room code required");
    }

    if (S.user.ws) {
      S.user.ws.close();
    }

    const ws = new WebSocket(`${wsBase()}/ws?room_id=${encodeURIComponent(room)}&token=${encodeURIComponent(token)}`);
    S.user.ws = ws;

    ws.onopen = () => log("WS connected", "ok");
    ws.onclose = () => {
      S.user.ws = null;
      log("WS closed");
    };
    ws.onerror = () => log("WS error", "warn");
    ws.onmessage = async e => {
      const raw = typeof e.data === "string" ? e.data : await e.data.text();
      raw
        .split("\n")
        .map(s => s.trim())
        .filter(Boolean)
        .forEach(chunk => {
          try {
            onWsEvent(JSON.parse(chunk));
          } catch {
            log(`raw ${chunk}`, "warn");
          }
        });
    };
  };

  const run = async fn => {
    try {
      state("working");
      await fn();
      state("ready");
    } catch (e) {
      log(e.message || String(e), "warn");
      state("error");
    }
  };

  window.addEventListener("resize", resizeCanvas);
  resizeCanvas();
  warnIfLocalhostFromLan();

  ui.applyBase.onclick = () => {
    const base = normalizeBaseUrl(ui.apiBase.value);
    if (!base) {
      log("base url required", "warn");
      return;
    }

    S.api = base;
    ui.apiBase.value = S.api;
    persistApiBase();
    log(`base url set ${S.api}`, "ok");
    warnIfLocalhostFromLan();
  };

  ui.registerUser.onclick = () =>
    run(async () => {
      await req("/register", "POST", {
        username: ui.userName.value.trim(),
        email: ui.userEmail.value.trim(),
        password: ui.userPass.value
      });
      log("user registered", "ok");
    });

  ui.loginUser.onclick = () =>
    run(async () => {
      const data = await req("/login", "POST", {
        email: ui.userEmail.value.trim(),
        password: ui.userPass.value
      });
      S.user.token = data.token || "";
      ui.userToken.value = S.user.token;
      log("user logged in", "ok");
    });

  ui.createRoom.onclick = () =>
    run(async () => {
      const data = await req("/create-room", "POST", null, tokenOf());
      S.room = data.room_id || "";
      ui.roomCode.value = S.room;
      log(`room created ${S.room}`, "ok");
    });

  ui.joinRoom.onclick = () =>
    run(async () => {
      const room = ui.roomCode.value.trim().toUpperCase();
      if (!room) {
        throw new Error("room code required");
      }

      const data = await req("/join-room", "POST", { room_id: room }, tokenOf());
      S.room = room;
      log(`joined ${room}, players=${data.players_count}`, "ok");
    });

  ui.startGame.onclick = () =>
    run(async () => {
      const room = ui.roomCode.value.trim().toUpperCase();
      if (!room) {
        throw new Error("room code required");
      }

      const data = await req("/start-game", "POST", { room_id: room }, tokenOf());
      log(`game started drawer=${short(data.drawer_id)}`, "ok");
    });

  ui.connectWs.onclick = () =>
    run(async () => {
      connectWs();
    });

  ui.disconnectWs.onclick = () => {
    if (S.user.ws) {
      S.user.ws.close();
    }
  };

  ui.sendChat.onclick = () =>
    run(async () => {
      const text = ui.chatInput.value.trim();
      if (!text) {
        throw new Error("chat empty");
      }

      wsSend("chat_message", { text });
      ui.chatInput.value = "";
    });

  ui.sendGuess.onclick = () =>
    run(async () => {
      const text = ui.guessInput.value.trim();
      if (!text) {
        throw new Error("guess empty");
      }

      wsSend("guess_submit", { text });
      ui.guessInput.value = "";
    });

  ui.clearCanvas.onclick = () => {
    const width = ui.board.width / (window.devicePixelRatio || 1);
    const height = ui.board.height / (window.devicePixelRatio || 1);
    ctx.clearRect(0, 0, width, height);
    log("canvas cleared");
  };

  ui.clearLog.onclick = () => {
    ui.log.innerHTML = "";
  };

  ui.board.onpointerdown = ev => {
    S.draw.on = true;
    const p = point(ev);
    S.draw.x = p.x;
    S.draw.y = p.y;
  };

  ui.board.onpointermove = ev => {
    if (!S.draw.on) {
      return;
    }

    const p = point(ev);
    const payload = {
      x0: S.draw.x,
      y0: S.draw.y,
      x1: p.x,
      y1: p.y,
      color: ui.brushColor.value,
      size: Number(ui.brushSize.value)
    };

    try {
      wsSend("draw_stroke", payload);
    } catch (e) {
      log(e.message, "warn");
      S.draw.on = false;
    }

    S.draw.x = p.x;
    S.draw.y = p.y;
  };

  ui.board.onpointerup = () => {
    S.draw.on = false;
  };

  ui.board.onpointerleave = () => {
    S.draw.on = false;
  };

  persistApiBase();
  log("frontend ready", "ok");
})();
