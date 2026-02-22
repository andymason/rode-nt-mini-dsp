// RODE NT-USB Mini DSP Controller - Web GUI Application JavaScript
// Real WebSocket implementation for real-time DSP control

class DSPController {
  constructor() {
    this.ws = null;
    this.isConnected = false;
    this.currentState = {};
    this.reconnectAttempts = 0;
    this.maxReconnectAttempts = 5;
    this.reconnectDelay = 2000;

    // Visualization state - synchronized with actual DSP parameters
    this.vizState = {
      compressor: {
        threshold: -20,
        ratio: 3.0,
        attack: 0.7,
        release: 21.0,
        gain: 2.0,
        enabled: true,
        currentInput: -30,
        currentOutput: -30,
      },
      gate: {
        threshold: -42,
        attack: 0.8,
        hold: 80.0,
        release: 210.0,
        range: -9.0,
        hysteresis: 50,
        enabled: true,
        currentLevel: -60,
        levelHistory: new Array(400).fill(-60),
      },
      auralExciter: {
        harmonics: 49.0,
        tune: 3516,
        enabled: true,
      },
      bigBottom: {
        drive: 62.0,
        tune: 131,
        enabled: true,
      },
    };

    // DOM Elements
    this.connectBtn = document.getElementById("connect-btn");
    this.statusDot = document.querySelector(".status-dot");
    this.statusText = document.querySelector(".status-text");

    // WebSocket URL (use current host and port)
    this.wsUrl = `ws://${window.location.host}/ws`;

    // Initialize
    this.bindEvents();
    this.initializeUI();
  }

  bindEvents() {
    // Connect button
    if (this.connectBtn) {
      this.connectBtn.addEventListener("click", () => this.toggleConnection());
    }

    // Window beforeunload - clean up WebSocket
    window.addEventListener("beforeunload", () => {
      if (this.ws && this.isConnected) {
        this.ws.close();
      }
    });
  }

  initializeUI() {
    // Initialize all sliders and toggles as disabled until connected
    this.disableAllControls();

    // Set up visualization canvases
    this.initializeVisualizations();

    // Set up audio monitoring placeholder
    this.initializeAudio();
  }

  toggleConnection() {
    if (this.isConnected) {
      this.disconnect();
    } else {
      this.connect();
    }
  }

  connect() {
    console.log("Connecting to WebSocket server...");

    // Update UI to connecting state
    this.updateConnectionStatus("connecting");

    try {
      this.ws = new WebSocket(this.wsUrl);

      this.ws.onopen = () => {
        console.log("WebSocket connection established");
        this.isConnected = true;
        this.reconnectAttempts = 0;
        this.updateConnectionStatus("connected");

        // Request current state from server
        this.sendMessage({ type: "get_state" });

        // Enable controls
        this.enableAllControls();
      };

      this.ws.onmessage = (event) => {
        this.handleMessage(event.data);
      };

      this.ws.onerror = (error) => {
        console.error("WebSocket error:", error);
        this.updateConnectionStatus("error");
      };

      this.ws.onclose = (event) => {
        console.log("WebSocket connection closed:", event.code, event.reason);
        this.isConnected = false;
        this.updateConnectionStatus("disconnected");
        this.disableAllControls();

        // Attempt reconnection if not intentionally disconnected
        if (
          event.code !== 1000 &&
          this.reconnectAttempts < this.maxReconnectAttempts
        ) {
          this.attemptReconnect();
        }
      };
    } catch (error) {
      console.error("Failed to create WebSocket:", error);
      this.updateConnectionStatus("error");
      setTimeout(() => this.updateConnectionStatus("disconnected"), 2000);
    }
  }

  disconnect() {
    if (this.ws) {
      this.ws.close(1000, "User requested disconnect");
    }
    this.isConnected = false;
    this.updateConnectionStatus("disconnected");
    this.disableAllControls();
  }

  attemptReconnect() {
    this.reconnectAttempts++;
    console.log(
      `Attempting to reconnect (${this.reconnectAttempts}/${this.maxReconnectAttempts})...`,
    );

    setTimeout(() => {
      if (!this.isConnected) {
        this.connect();
      }
    }, this.reconnectDelay * this.reconnectAttempts);
  }

  updateConnectionStatus(status) {
    const statusMap = {
      connecting: {
        text: "CONNECTING",
        dotClass: "loading",
        btnText: "Connecting...",
      },
      connected: {
        text: "CONNECTED",
        dotClass: "connected",
        btnText: "Disconnect",
      },
      disconnected: {
        text: "DISCONNECTED",
        dotClass: "disconnected",
        btnText: "Connect",
      },
      error: { text: "ERROR", dotClass: "disconnected", btnText: "Connect" },
    };

    const statusInfo = statusMap[status] || statusMap.disconnected;

    // Update status dot
    this.statusDot.className = "status-dot";
    this.statusDot.classList.add(statusInfo.dotClass);

    // Update status text
    this.statusText.textContent = statusInfo.text;

    // Update connect button
    if (this.connectBtn) {
      this.connectBtn.textContent = statusInfo.btnText;
      this.connectBtn.disabled = status === "connecting";
    }
  }

  sendMessage(message) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      console.warn("WebSocket not connected, cannot send message:", message);
      return;
    }

    try {
      const jsonMessage = JSON.stringify(message);
      this.ws.send(jsonMessage);
      console.log("Sent message:", message);
    } catch (error) {
      console.error("Failed to send WebSocket message:", error);
    }
  }

  handleMessage(data) {
    // Server may batch multiple JSON messages separated by newlines
    const lines = data.split('\n').filter(l => l.trim());
    for (const line of lines) {
      try {
        const message = JSON.parse(line);
        console.log("Received message:", message);

        switch (message.type) {
          case "state":
            this.handleStateMessage(message);
            break;

          case "param_update":
            this.handleParamUpdate(message);
            break;

          case "connection_status":
            this.handleConnectionStatus(message);
            break;

          case "error":
            this.handleErrorMessage(message);
            break;

          default:
            console.warn("Unknown message type:", message.type);
        }
      } catch (error) {
        console.error("Failed to parse WebSocket message:", error, line);
      }
    }
  }

  handleStateMessage(message) {
    // Handle full state update (with value field)
    if (message.value) {
      try {
        // message.value is already a parsed object (JSON.parse was done in handleMessage)
        const state = typeof message.value === "string"
          ? JSON.parse(message.value)
          : message.value;
        this.currentState = state;
        this.updateUIFromState(state);
        console.log("[VALIDATE] Full state received from server:", JSON.stringify(state));
        this._validateFullState(state);
      } catch (error) {
        console.error("Failed to parse state:", error);
      }
    }
    // Handle partial enable update (from set_enable responses)
    else if (message.effect !== undefined && message.enabled !== undefined) {
      // Update current state
      if (!this.currentState.enabled) this.currentState.enabled = {};
      this.currentState.enabled[message.effect] = message.enabled;

      // Validate against what we sent
      if (this._pendingEnableSet) {
        const p = this._pendingEnableSet;
        if (p.effect === message.effect && p.enabled === message.enabled) {
          console.log(`[VALIDATE] ✓ effect ${message.effect} enabled=${message.enabled} confirmed`);
        } else {
          console.warn(`[VALIDATE] ✗ MISMATCH: sent effect=${p.effect} enabled=${p.enabled}, got effect=${message.effect} enabled=${message.enabled}`);
        }
        this._pendingEnableSet = null;
      }

      // Update UI for this specific effect
      this.updateEffectEnabled(message.effect, message.enabled);
      console.log(
        `Updated effect ${message.effect} enabled: ${message.enabled}`,
      );
    }
  }

  _validateFullState(state) {
    const effectNames = ["Compressor", "Noise Gate", "Aural Exciter", "Big Bottom"];
    if (state.enabled) {
      for (const [k, v] of Object.entries(state.enabled)) {
        console.log(`[VALIDATE]   effect ${k} (${effectNames[k] || k}): enabled=${v}`);
      }
    }
    if (state.params) {
      for (const [key, value] of Object.entries(state.params)) {
        const [eff, param] = key.split("_");
        console.log(`[VALIDATE]   param ${key} (eff=${eff} param=${param}): ${value}`);
      }
    }
  }

  handleParamUpdate(message) {
    // Update individual parameter
    const { effect, param, value } = message;

    if (effect !== undefined && param !== undefined && value !== undefined) {
      // Validate against what we sent
      if (this._pendingParamSet) {
        const p = this._pendingParamSet;
        if (p.effect === effect && p.param === param) {
          const delta = Math.abs(p.value - value);
          if (delta < 0.01) {
            console.log(`[VALIDATE] ✓ param eff=${effect} param=${param} value=${value} confirmed`);
          } else {
            console.warn(`[VALIDATE] ✗ MISMATCH: sent value=${p.value}, server echo=${value} (delta=${delta.toFixed(4)}, likely snapped to resolution)`);
          }
        }
        this._pendingParamSet = null;
      }

      // Update current state
      if (!this.currentState.params) this.currentState.params = {};
      if (!this.currentState.params[effect])
        this.currentState.params[effect] = {};
      this.currentState.params[effect][param] = value;

      // Update UI for this specific parameter
      this.updateParameterUI(effect, param, value);
    }
  }

  handleConnectionStatus(message) {
    if (message.enabled !== undefined) {
      this.updateConnectionStatus(
        message.enabled ? "connected" : "disconnected",
      );
    }
  }

  handleErrorMessage(message) {
    console.error("Server error:", message.error);
    this.showNotification(`Error: ${message.error}`, "error");
  }

  updateUIFromState(state) {
    // Update enabled states
    if (state.enabled) {
      for (const [effectId, enabled] of Object.entries(state.enabled)) {
        this.updateEffectEnabled(parseInt(effectId), enabled);
      }
    }

    // Update parameter values (flat "effectId_paramId" keys from Go server)
    if (state.params) {
      for (const [key, value] of Object.entries(state.params)) {
        const parts = key.split("_");
        if (parts.length === 2) {
          this.updateParameterUI(parseInt(parts[0]), parseInt(parts[1]), value);
        }
      }
    }
  }

  updateEffectEnabled(effectId, enabled) {
    // Find the toggle for this effect
    const effectPanels = document.querySelectorAll(".effect-panel");
    if (effectId < effectPanels.length) {
      const toggle = effectPanels[effectId].querySelector(".toggle input");
      if (toggle) {
        toggle.checked = enabled;
      }

      // Update visualization state
      this.updateVizEnable(effectId, enabled);
    }
  }

  updateVizEnable(effectId, enabled) {
    // Update visualization state enabled flag
    const effectNames = ["compressor", "gate", "auralExciter", "bigBottom"];
    if (effectId >= 0 && effectId < effectNames.length) {
      const effectName = effectNames[effectId];
      if (this.vizState[effectName]) {
        this.vizState[effectName].enabled = enabled;
        console.log(`Updated vizState.${effectName}.enabled = ${enabled}`);

        // Special handling for compressor enabled state
        if (effectName === "compressor") {
          if (!enabled) {
            // When compressor is disabled, output equals input (no compression)
            this.vizState.compressor.currentOutput =
              this.vizState.compressor.currentInput;
            console.log(
              `Compressor disabled: output = input (${this.vizState.compressor.currentOutput}dB)`,
            );
          } else {
            // When compressor is enabled, recalculate output with current parameters
            const input = this.vizState.compressor.currentInput;
            const threshold = this.vizState.compressor.threshold;
            const ratio = this.vizState.compressor.ratio;
            const gain = this.vizState.compressor.gain;
            this.vizState.compressor.currentOutput =
              this.calculateCompressorOutput(input, threshold, ratio, gain);
            console.log(
              `Compressor enabled: ${input}dB -> ${this.vizState.compressor.currentOutput}dB (gain: ${gain}dB)`,
            );
          }
        }

        this.updateVisualizations();
      }
    }
  }

  updateParameterUI(effectId, paramId, value) {
    // Find the parameter element for this effect and parameter
    const effectPanels = document.querySelectorAll(".effect-panel");
    if (effectId < effectPanels.length) {
      const params = effectPanels[effectId].querySelectorAll(".param");
      if (paramId <= params.length) {
        const param = params[paramId - 1]; // paramId is 1-based in UI

        // Update value display
        const valueSpan = param.querySelector(".value");
        if (valueSpan) {
          // Format value based on parameter type
          valueSpan.textContent = this.formatValue(effectId, paramId, value);
        }

        // Update slider (log-scale sliders need inverse conversion)
        const slider = param.querySelector('input[type="range"]');
        if (slider) {
          slider.value = this.paramToSlider(slider, value);
        }

        // Update hex display (calculate from value)
        const hexDiv = param.querySelector(".hex");
        if (hexDiv) {
          const hexValue = this.calculateHexValue(effectId, paramId, value);
          hexDiv.textContent = `Hex: ${hexValue}`;
        }

        // Update visualization state
        this.updateVizParam(effectId, paramId, value);
      }
    }
  }

  updateVizParam(effectId, paramId, value) {
    // Update visualization state based on parameter changes
    const effectNames = ["compressor", "gate", "auralExciter", "bigBottom"];
    const paramNames = [
      ["threshold", "ratio", "attack", "release", "gain"],
      ["threshold", "attack", "hold", "release", "range", "hysteresis"],
      ["harmonics", "tune"],
      ["drive", "tune"],
    ];

    if (effectId >= 0 && effectId < effectNames.length) {
      const effectName = effectNames[effectId];
      if (paramId >= 1 && paramId <= paramNames[effectId].length) {
        const paramName = paramNames[effectId][paramId - 1];

        // Update the visualization state
        if (this.vizState[effectName]) {
          this.vizState[effectName][paramName] = value;
          console.log(`Updated vizState.${effectName}.${paramName} = ${value}`);

          // Special handling for specific parameter changes
          if (effectName === "compressor") {
            if (
              paramName === "threshold" ||
              paramName === "ratio" ||
              paramName === "gain"
            ) {
              // Recalculate compressor output when threshold, ratio, or gain changes
              const input = this.vizState.compressor.currentInput;
              const threshold = this.vizState.compressor.threshold;
              const ratio = this.vizState.compressor.ratio;
              const gain = this.vizState.compressor.gain;
              this.vizState.compressor.currentOutput =
                this.calculateCompressorOutput(input, threshold, ratio, gain);
              console.log(
                `Recalculated compressor output: ${input}dB -> ${this.vizState.compressor.currentOutput}dB (gain: ${gain}dB)`,
              );
            }
          } else if (effectName === "gate") {
            if (paramName === "threshold") {
              // Simulate some audio level variation when threshold changes
              // Just add a small random variation to make visualization more interesting
              const variation = (Math.random() - 0.5) * 5;
              this.vizState.gate.currentLevel = value + variation;
            }
          }
        }

        // Trigger visualization updates
        this.updateVisualizations();
      }
    }
  }

  updateVisualizations() {
    // Visualization draw functions read from this.vizState on every animation frame
    // Changes to vizState will be automatically reflected on the next frame render
    // No explicit redraw needed due to continuous requestAnimationFrame loops
  }

  formatValue(effectId, paramId, value) {
    // Format values based on parameter type
    const formats = {
      // Compressor (effectId: 0)
      "0_1": (v) => `${v.toFixed(1)} dB`, // Threshold
      "0_2": (v) => `${v.toFixed(1)}:1`, // Ratio
      "0_3": (v) => `${v.toFixed(2)} ms`, // Attack
      "0_4": (v) => `${v.toFixed(1)} ms`, // Release
      "0_5": (v) => `${v.toFixed(1)} dB`, // Gain

      // Noise Gate (effectId: 1)
      "1_1": (v) => `${v.toFixed(1)} dB`, // Threshold
      "1_2": (v) => `${v.toFixed(2)} ms`, // Attack
      "1_3": (v) => `${v.toFixed(1)} ms`, // Hold
      "1_4": (v) => `${v.toFixed(1)} ms`, // Release
      "1_5": (v) => `${v.toFixed(1)} dB`, // Range
      "1_6": (v) => `${v.toFixed(0)}%`, // Hysteresis

      // Aural Exciter (effectId: 2)
      "2_1": (v) => `${v.toFixed(1)}%`, // Harmonics
      "2_2": (v) => `${v.toFixed(0)} Hz`, // Tune

      // Big Bottom (effectId: 3)
      "3_1": (v) => `${v.toFixed(1)}%`, // Drive
      "3_2": (v) => `${v.toFixed(0)} Hz`, // Tune
    };

    const key = `${effectId}_${paramId}`;
    return formats[key] ? formats[key](value) : value.toString();
  }

  calculateHexValue(effectId, paramId, value) {
    // Simplified hex calculation - in real implementation this would
    // match the Go encoding functions
    const effectNames = [
      "Compressor",
      "Noise Gate",
      "Aural Exciter",
      "Big Bottom",
    ];
    const paramNames = [
      ["Threshold", "Ratio", "Attack", "Release", "Gain"],
      ["Threshold", "Attack", "Hold", "Release", "Range", "Hysteresis"],
      ["Harmonics", "Tune"],
      ["Drive", "Tune"],
    ];

    if (
      effectId < effectNames.length &&
      paramId <= paramNames[effectId].length
    ) {
      return `0x${Math.round(value * 1000)
        .toString(16)
        .toUpperCase()
        .padStart(8, "0")}`;
    }

    return "0x00000000";
  }

  calculateCompressorOutput(inputDb, threshold, ratio, gain = 0) {
    // Calculate compressor output based on input, threshold, ratio, and gain
    // Basic compressor algorithm: output = input when input <= threshold
    // output = threshold + (input - threshold) / ratio when input > threshold
    // Then apply makeup gain

    let outputDb;
    if (inputDb <= threshold) {
      // Below threshold: no compression (1:1 ratio)
      outputDb = inputDb;
    } else {
      // Above threshold: apply compression
      const aboveThreshold = inputDb - threshold;
      const compressed = aboveThreshold / ratio;
      outputDb = threshold + compressed;
    }

    // Apply makeup gain
    return outputDb + gain;
  }

  disableAllControls() {
    // Disable all sliders
    document.querySelectorAll('input[type="range"]').forEach((slider) => {
      slider.disabled = true;
    });

    // Disable all toggles
    document.querySelectorAll(".toggle input").forEach((toggle) => {
      toggle.disabled = true;
    });

    // Disable all reset buttons
    document.querySelectorAll(".btn.reset").forEach((btn) => {
      btn.disabled = true;
    });
  }

  enableAllControls() {
    // Enable all sliders and add event listeners
    document
      .querySelectorAll('input[type="range"]')
      .forEach((slider, index) => {
        slider.disabled = false;

        // Remove existing listeners and add new ones
        slider.oninput = null;
        slider.onchange = null;

        slider.addEventListener("input", (e) => {
          this.handleSliderChange(e.target);
        });

        slider.addEventListener("change", (e) => {
          this.handleSliderCommit(e.target);
        });
      });

    // Enable all toggles and add event listeners
    const effectPanelsForToggle = Array.from(document.querySelectorAll(".effect-panel"));
    document.querySelectorAll(".toggle input").forEach((toggle) => {
      toggle.disabled = false;

      toggle.onchange = null;
      toggle.addEventListener("change", (e) => {
        const panel = e.target.closest(".effect-panel");
        const effectIndex = effectPanelsForToggle.indexOf(panel);
        if (effectIndex === -1) {
          console.warn("Toggle outside effect-panel — ignoring", e.target);
          return;
        }
        this.handleToggleChange(e.target, effectIndex);
      });
    });

    // Enable all reset buttons and add event listeners
    document.querySelectorAll(".btn.reset").forEach((btn, index) => {
      btn.disabled = false;

      btn.onclick = null;
      btn.addEventListener("click", (e) => {
        e.preventDefault();
        this.handleResetDefaults(index);
      });
    });
  }

  // Convert slider position → actual param value (handles log-scale sliders)
  sliderToParam(slider) {
    const raw = parseFloat(slider.value);
    const sMin = parseFloat(slider.min);
    const sMax = parseFloat(slider.max);
    const t = (raw - sMin) / (sMax - sMin);

    if (slider.dataset.scale === "log") {
      const pMin = parseFloat(slider.dataset.paramMin);
      const pMax = parseFloat(slider.dataset.paramMax);
      return pMin * Math.pow(pMax / pMin, t);
    }

    if (slider.dataset.scale === "neglog") {
      // Logarithmic scale for negative-dB params (e.g. Range -100 to 0).
      // t=0 → most negative (pParamMin), t=1 → closest to 0 (-pNegmax).
      // Formula: absVal = pMaxAbs * (pMinAbs / pMaxAbs)^t
      const pMaxAbs = Math.abs(parseFloat(slider.dataset.paramMin)); // e.g. 100
      const pMinAbs = parseFloat(slider.dataset.paramNegmax);        // e.g. 0.5
      const absVal = pMaxAbs * Math.pow(pMinAbs / pMaxAbs, t);
      return -absVal;
    }

    return raw;
  }

  // Convert actual param value → slider position (inverse of above)
  paramToSlider(slider, paramValue) {
    const sMin = parseFloat(slider.min);
    const sMax = parseFloat(slider.max);

    if (slider.dataset.scale === "log") {
      const pMin = parseFloat(slider.dataset.paramMin);
      const pMax = parseFloat(slider.dataset.paramMax);
      const t = Math.log(paramValue / pMin) / Math.log(pMax / pMin);
      return sMin + t * (sMax - sMin);
    }

    if (slider.dataset.scale === "neglog") {
      const pMaxAbs = Math.abs(parseFloat(slider.dataset.paramMin));
      const pMinAbs = parseFloat(slider.dataset.paramNegmax);
      const absVal = Math.max(pMinAbs, Math.min(pMaxAbs, -paramValue));
      const t = Math.log(absVal / pMaxAbs) / Math.log(pMinAbs / pMaxAbs);
      return sMin + t * (sMax - sMin);
    }

    return paramValue;
  }

  handleSliderChange(slider) {
    // Update value display in real-time during drag
    const param = slider.closest(".param");
    if (param) {
      const valueSpan = param.querySelector(".value");
      if (valueSpan) {
        // Parse effect and param from DOM structure
        const effectPanel = slider.closest(".effect-panel");
        const effectIndex = Array.from(
          document.querySelectorAll(".effect-panel"),
        ).indexOf(effectPanel);
        const paramIndex = Array.from(
          effectPanel.querySelectorAll(".param"),
        ).indexOf(param);

        const value = this.sliderToParam(slider);
        valueSpan.textContent = this.formatValue(
          effectIndex,
          paramIndex + 1,
          value,
        );

        // Update hex display
        const hexDiv = param.querySelector(".hex");
        if (hexDiv) {
          const hexValue = this.calculateHexValue(
            effectIndex,
            paramIndex + 1,
            value,
          );
          hexDiv.textContent = `Hex: ${hexValue}`;
        }

        // Update visualization state in real-time
        this.updateVizParam(effectIndex, paramIndex + 1, value);
      }
    }
  }

  handleSliderCommit(slider) {
    // Send update to server when slider is released
    const param = slider.closest(".param");
    if (param) {
      const effectPanel = slider.closest(".effect-panel");
      const effectIndex = Array.from(
        document.querySelectorAll(".effect-panel"),
      ).indexOf(effectPanel);
      const paramIndex = Array.from(
        effectPanel.querySelectorAll(".param"),
      ).indexOf(param);

      const value = this.sliderToParam(slider);

      // Track pending param for validation
      this._pendingParamSet = { effect: effectIndex, param: paramIndex + 1, value };

      this.sendMessage({
        type: "set_param",
        effect: effectIndex,
        param: paramIndex + 1, // Convert to 1-based
        value: value,
      });

      console.log(
        `[VALIDATE] set_param sent: effect=${effectIndex}, param=${paramIndex + 1}, value=${value}`,
      );
    }
  }

  handleToggleChange(toggle, effectIndex) {
    const enabled = toggle.checked;

    // Update visualization immediately
    this.updateVizEnable(effectIndex, enabled);

    // Track what we expect the server to confirm
    this._pendingEnableSet = { effect: effectIndex, enabled };

    this.sendMessage({
      type: "set_enable",
      effect: effectIndex,
      enabled: enabled,
    });

    console.log(
      `[VALIDATE] set_enable sent: effect=${effectIndex}, enabled=${enabled}`,
    );
  }

  handleResetDefaults(effectIndex) {
    this.sendMessage({
      type: "reset_defaults",
      effect: effectIndex,
    });

    console.log(`Reset defaults sent: effect=${effectIndex}`);
    this.showNotification(
      `Reset ${this.getEffectName(effectIndex)} to defaults`,
      "info",
    );
  }

  getEffectName(effectIndex) {
    const names = ["Compressor", "Noise Gate", "Aural Exciter", "Big Bottom"];
    return names[effectIndex] || `Effect ${effectIndex}`;
  }

  showNotification(message, type = "info") {
    // Create notification element
    const notification = document.createElement("div");
    notification.className = `notification ${type}`;
    notification.textContent = message;
    notification.style.cssText = `
            position: fixed;
            top: 20px;
            right: 20px;
            padding: 12px 20px;
            background: ${type === "error" ? "#F44336" : type === "success" ? "#4CAF50" : "#2196F3"};
            color: white;
            border-radius: 6px;
            box-shadow: 0 4px 12px rgba(0,0,0,0.3);
            z-index: 1000;
            animation: slideIn 0.3s ease;
        `;

    document.body.appendChild(notification);

    // Remove after 3 seconds
    setTimeout(() => {
      notification.style.animation = "slideOut 0.3s ease";
      setTimeout(() => {
        if (notification.parentNode) {
          notification.parentNode.removeChild(notification);
        }
      }, 300);
    }, 3000);

    // Add CSS animations if not already present
    if (!document.getElementById("notification-styles")) {
      const style = document.createElement("style");
      style.id = "notification-styles";
      style.textContent = `
                @keyframes slideIn {
                    from { transform: translateX(100%); opacity: 0; }
                    to { transform: translateX(0); opacity: 1; }
                }
                @keyframes slideOut {
                    from { transform: translateX(0); opacity: 1; }
                    to { transform: translateX(100%); opacity: 0; }
                }
            `;
      document.head.appendChild(style);
    }
  }

  resizeCanvas(canvas) {
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    const cssW = Math.round(rect.width);
    const cssH = Math.round(rect.height);
    const w = cssW * dpr;
    const h = cssH * dpr;
    if (canvas.width !== w || canvas.height !== h) {
      canvas.width = w;
      canvas.height = h;
      canvas._cssWidth = cssW;
      canvas._cssHeight = cssH;
      canvas._dpr = dpr;
      const ctx = canvas.getContext("2d");
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    }
  }

  resizeAllCanvases() {
    ["comp-viz", "gate-viz", "eq-viz"].forEach(id => {
      const c = document.getElementById(id);
      if (c) this.resizeCanvas(c);
    });
  }

  initializeVisualizations() {
    this.resizeAllCanvases();

    // Initialize gate visualization
    this.initGateVisualization();

    // Initialize compressor visualization
    this.initCompressorVisualization();

    // Initialize EQ visualization
    this.initEQVisualization();

    // Resize canvases on window resize (debounced)
    let resizeTimer;
    window.addEventListener("resize", () => {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(() => this.resizeAllCanvases(), 100);
    });
  }

  initGateVisualization() {
    const canvas = document.getElementById("gate-viz");
    if (!canvas) return;
    const ctx = canvas.getContext("2d");

    const ML = 36, MR = 8, MT = 8, MB = 22; // margins

    const draw = () => {
      const W = canvas._cssWidth || canvas.width, H = canvas._cssHeight || canvas.height;
      const PW = W - ML - MR, PH = H - MT - MB;
      const g = this.vizState.gate;
      const minDb = -60, maxDb = 0;

      const dbToY = (db) => MT + PH * (1 - (db - minDb) / (maxDb - minDb));

      // Background
      ctx.fillStyle = "#0d0d0d";
      ctx.fillRect(0, 0, W, H);

      // Grid
      ctx.lineWidth = 1;
      [-12, -24, -36, -48].forEach((db) => {
        const y = dbToY(db);
        ctx.strokeStyle = "#222";
        ctx.beginPath(); ctx.moveTo(ML, y); ctx.lineTo(ML + PW, y); ctx.stroke();
        ctx.fillStyle = "#444";
        ctx.font = "9px monospace";
        ctx.textAlign = "right";
        ctx.fillText(db, ML - 4, y + 3);
      });
      // 0 dB label
      ctx.fillStyle = "#444"; ctx.textAlign = "right";
      ctx.fillText("0", ML - 4, dbToY(0) + 3);

      // Threshold (open) line
      const tY = dbToY(g.threshold);
      ctx.strokeStyle = g.enabled ? "#aaff00" : "#555";
      ctx.lineWidth = 1;
      ctx.setLineDash([4, 4]);
      ctx.beginPath(); ctx.moveTo(ML, tY); ctx.lineTo(ML + PW, tY); ctx.stroke();
      ctx.setLineDash([]);
      ctx.fillStyle = g.enabled ? "#aaff00" : "#555";
      ctx.font = "9px monospace"; ctx.textAlign = "left";
      ctx.fillText(`open ${g.threshold.toFixed(1)} dB`, ML + PW - 80, tY - 3);

      // Hysteresis: close threshold line below the open threshold
      const hystDb = (g.hysteresis / 100) * 6;
      const closeThreshold = g.threshold - hystDb;
      const hY = dbToY(closeThreshold);
      ctx.strokeStyle = g.enabled ? "#557722" : "#333";
      ctx.lineWidth = 1;
      ctx.setLineDash([2, 4]);
      ctx.beginPath(); ctx.moveTo(ML, hY); ctx.lineTo(ML + PW, hY); ctx.stroke();
      ctx.setLineDash([]);
      ctx.fillStyle = g.enabled ? "#557722" : "#333";
      ctx.fillText(`close ${closeThreshold.toFixed(1)} dB`, ML + PW - 92, hY + 10);

      // Scrolling level history — draw each segment with correct colour applied before stroke
      const hist = g.levelHistory;
      const step = PW / hist.length;
      ctx.lineWidth = 1.5;
      let segStart = 0;
      for (let i = 1; i <= hist.length; i++) {
        const prevOpen = g.enabled && hist[i - 1] > g.threshold;
        const currOpen = i < hist.length ? (g.enabled && hist[i] > g.threshold) : !prevOpen;
        if (currOpen !== prevOpen) {
          ctx.beginPath();
          ctx.strokeStyle = prevOpen ? "#aaff00" : "#444";
          for (let j = segStart; j <= i - 1; j++) {
            const x = ML + j * step;
            const y = dbToY(hist[j]);
            j === segStart ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
          }
          ctx.stroke();
          segStart = i - 1;
        }
      }

      // Current level dot at right edge
      const curY = dbToY(g.currentLevel);
      const gateOpen = g.enabled && g.currentLevel > g.threshold;
      ctx.fillStyle = gateOpen ? "#aaff00" : "#e74c3c";
      ctx.beginPath();
      ctx.arc(ML + PW - 4, curY, 4, 0, Math.PI * 2);
      ctx.fill();

      // Axes
      ctx.strokeStyle = "#333";
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(ML, MT); ctx.lineTo(ML, MT + PH);
      ctx.lineTo(ML + PW, MT + PH);
      ctx.stroke();

      // Info text (top-left, compact)
      ctx.font = "10px monospace";
      ctx.textAlign = "left";
      ctx.fillStyle = g.enabled ? "#aaff00" : "#555";
      ctx.fillText(`GATE ${g.enabled ? "ON" : "OFF"}`, ML + 4, MT + 12);
      ctx.fillStyle = "#666";
      ctx.fillText(`${g.currentLevel.toFixed(1)} dBFS`, ML + 4, MT + 24);

      requestAnimationFrame(draw.bind(this));
    };
    draw.call(this);
  }

  initCompressorVisualization() {
    const canvas = document.getElementById("comp-viz");
    if (!canvas) return;
    const ctx = canvas.getContext("2d");

    const ML = 36, MR = 8, MT = 8, MB = 22;

    const draw = () => {
      const W = canvas._cssWidth || canvas.width, H = canvas._cssHeight || canvas.height;
      const PW = W - ML - MR, PH = H - MT - MB;
      const c = this.vizState.compressor;
      const minDb = -60, maxDb = 0;

      const dbToX = (db) => ML + ((db - minDb) / (maxDb - minDb)) * PW;
      const dbToY = (db) => MT + PH - ((db - minDb) / (maxDb - minDb)) * PH;

      // Background
      ctx.fillStyle = "#0d0d0d";
      ctx.fillRect(0, 0, W, H);

      // Grid
      ctx.lineWidth = 1;
      [-12, -24, -36, -48].forEach((db) => {
        const x = dbToX(db), y = dbToY(db);
        ctx.strokeStyle = "#1e1e1e";
        // Vertical
        ctx.beginPath(); ctx.moveTo(x, MT); ctx.lineTo(x, MT + PH); ctx.stroke();
        // Horizontal
        ctx.beginPath(); ctx.moveTo(ML, y); ctx.lineTo(ML + PW, y); ctx.stroke();
        // X axis labels
        ctx.fillStyle = "#444"; ctx.font = "9px monospace"; ctx.textAlign = "center";
        ctx.fillText(db, x, MT + PH + 14);
        // Y axis labels
        ctx.textAlign = "right";
        ctx.fillText(db, ML - 4, y + 3);
      });
      ctx.fillStyle = "#444"; ctx.textAlign = "right";
      ctx.fillText("0", ML - 4, dbToY(0) + 3);
      ctx.textAlign = "center";
      ctx.fillText("0", dbToX(0), MT + PH + 14);

      // 1:1 reference line
      ctx.strokeStyle = "#2a2a2a";
      ctx.lineWidth = 1;
      ctx.setLineDash([3, 3]);
      ctx.beginPath();
      ctx.moveTo(dbToX(minDb), dbToY(minDb));
      ctx.lineTo(dbToX(maxDb), dbToY(maxDb));
      ctx.stroke();
      ctx.setLineDash([]);

      // Threshold vertical marker
      const tX = dbToX(c.threshold);
      ctx.strokeStyle = "#c0392b";
      ctx.lineWidth = 1;
      ctx.setLineDash([3, 3]);
      ctx.beginPath(); ctx.moveTo(tX, MT); ctx.lineTo(tX, MT + PH); ctx.stroke();
      ctx.setLineDash([]);

      // Compressor transfer curve
      ctx.strokeStyle = c.enabled ? "#aaff00" : "#3a3a3a";
      ctx.lineWidth = 2;
      ctx.beginPath();
      for (let i = 0; i <= PW; i++) {
        const inputDb = minDb + (i / PW) * (maxDb - minDb);
        const outputDb = this.calculateCompressorOutput(inputDb, c.threshold, c.ratio, c.gain);
        const x = ML + i;
        const y = dbToY(Math.max(minDb, Math.min(maxDb, outputDb)));
        i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
      }
      ctx.stroke();

      // Current level dot
      const dotX = dbToX(Math.max(minDb, Math.min(maxDb, c.currentInput)));
      const dotY = dbToY(Math.max(minDb, Math.min(maxDb, c.currentOutput)));
      ctx.fillStyle = c.enabled ? "#aaff00" : "#555";
      ctx.beginPath(); ctx.arc(dotX, dotY, 5, 0, Math.PI * 2); ctx.fill();

      // Axes
      ctx.strokeStyle = "#333"; ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(ML, MT); ctx.lineTo(ML, MT + PH); ctx.lineTo(ML + PW, MT + PH);
      ctx.stroke();

      // Axis labels
      ctx.fillStyle = "#555"; ctx.font = "9px monospace";
      ctx.textAlign = "center";
      ctx.fillText("Input (dBFS)", ML + PW / 2, H - 2);
      ctx.save(); ctx.translate(10, MT + PH / 2); ctx.rotate(-Math.PI / 2);
      ctx.fillText("Output (dBFS)", 0, 0); ctx.restore();

      // Param readout (top-left)
      ctx.font = "10px monospace"; ctx.textAlign = "left";
      ctx.fillStyle = c.enabled ? "#aaff00" : "#555";
      ctx.fillText(`COMP ${c.enabled ? "ON" : "OFF"}`, ML + 4, MT + 12);
      ctx.fillStyle = "#666";
      ctx.fillText(`in  ${c.currentInput.toFixed(1)} dBFS`, ML + 4, MT + 24);
      ctx.fillText(`out ${c.currentOutput.toFixed(1)} dBFS`, ML + 4, MT + 36);

      // Param readout (top-right)
      ctx.textAlign = "right";
      ctx.fillStyle = "#666";
      ctx.fillText(`thr ${c.threshold.toFixed(1)} dB`, ML + PW - 2, MT + 12);
      ctx.fillText(`${c.ratio.toFixed(1)}:1`, ML + PW - 2, MT + 24);
      ctx.fillText(`+${c.gain.toFixed(1)} dB gain`, ML + PW - 2, MT + 36);

      requestAnimationFrame(draw.bind(this));
    };
    draw.call(this);
  }

  initEQVisualization() {
    const canvas = document.getElementById("eq-viz");
    if (!canvas) return;
    const ctx = canvas.getContext("2d");

    const draw = () => {
      const W = canvas._cssWidth || canvas.width, H = canvas._cssHeight || canvas.height;
      const ML = 36, MR = 8, MT = 8, MB = 22;
      const PW = W - ML - MR, PH = H - MT - MB;

      const ae = this.vizState.auralExciter;
      const bb = this.vizState.bigBottom;
      const aeEnabled = ae.enabled, bbEnabled = bb.enabled;

      // dB range for spectrum
      const minDb = -90, maxDb = 0;

      const freqToX = (f) => ML + (Math.log(f / 20) / Math.log(1000)) * PW;
      const dbToY = (db) => MT + PH - ((db - minDb) / (maxDb - minDb)) * PH;

      ctx.fillStyle = "#0d0d0d";
      ctx.fillRect(0, 0, W, H);

      // Grid - freq verticals
      const gridFreqs = [50, 100, 200, 500, 1000, 2000, 5000, 10000, 20000];
      ctx.lineWidth = 1; ctx.strokeStyle = "#1e1e1e";
      gridFreqs.forEach((f) => {
        const x = freqToX(f);
        ctx.beginPath(); ctx.moveTo(x, MT); ctx.lineTo(x, MT + PH); ctx.stroke();
      });

      // Grid - dB horizontals
      [-18, -36, -54, -72].forEach((db) => {
        const y = dbToY(db);
        ctx.beginPath(); ctx.moveTo(ML, y); ctx.lineTo(ML + PW, y); ctx.stroke();
        ctx.fillStyle = "#444"; ctx.font = "9px monospace"; ctx.textAlign = "right";
        ctx.fillText(db, ML - 4, y + 3);
      });
      ctx.fillStyle = "#444"; ctx.textAlign = "right";
      ctx.fillText("0", ML - 4, dbToY(0) + 3);

      // Freq axis labels
      ctx.fillStyle = "#444"; ctx.font = "9px monospace"; ctx.textAlign = "center";
      [20, 100, 500, "1k", "5k", "20k"].forEach((label, i) => {
        const f = [20, 100, 500, 1000, 5000, 20000][i];
        ctx.fillText(label, freqToX(f), MT + PH + 14);
      });

      // Live spectrum
      if (this.freqData && this.audioContext) {
        const binCount = this.freqData.length;
        const sampleRate = this.audioContext.sampleRate;
        ctx.beginPath();
        ctx.strokeStyle = "rgba(41, 182, 246, 0.75)";
        ctx.lineWidth = 1.5;
        let started = false;
        for (let i = 1; i < binCount; i++) {
          const freq = (i / binCount) * (sampleRate / 2);
          if (freq < 20 || freq > 20000) continue;
          const x = freqToX(freq);
          const y = dbToY(Math.max(minDb, this.freqData[i]));
          if (!started) { ctx.moveTo(x, y); started = true; }
          else ctx.lineTo(x, y);
        }
        ctx.stroke();
      }

      // Effect curves use a relative boost scale (0–12 dB), anchored at the bottom
      // of the plot area so they don't overlap with the spectrum.
      // boostToY: 0 dB boost = bottom of plot, 12 dB boost = top of plot
      const maxBoost = 12;
      const boostToY = (boostDb) => MT + PH - (boostDb / maxBoost) * PH;

      // AE effect curve
      if (aeEnabled) {
        ctx.beginPath(); ctx.strokeStyle = "#e74c3c"; ctx.lineWidth = 1.5;
        for (let i = 0; i <= PW; i++) {
          const freq = 20 * Math.pow(1000, i / PW);
          const boostDb = (ae.harmonics / 100) *
            Math.exp(-Math.pow(Math.log2(freq / ae.tune), 2) * 1.5) * maxBoost;
          const x = ML + i, y = boostToY(boostDb);
          i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
        }
        ctx.stroke();
      }

      // BB effect curve
      if (bbEnabled) {
        ctx.beginPath(); ctx.strokeStyle = "#aaff00"; ctx.lineWidth = 1.5;
        for (let i = 0; i <= PW; i++) {
          const freq = 20 * Math.pow(1000, i / PW);
          const boostDb = (bb.drive / 100) *
            Math.exp(-Math.pow(Math.log2(freq / bb.tune), 2) * 2.0) * maxBoost;
          const x = ML + i, y = boostToY(boostDb);
          i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
        }
        ctx.stroke();
      }

      // Axes border
      ctx.strokeStyle = "#333"; ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(ML, MT); ctx.lineTo(ML, MT + PH); ctx.lineTo(ML + PW, MT + PH);
      ctx.stroke();

      // Legend (top-right)
      ctx.font = "10px monospace"; ctx.textAlign = "right";
      let legendY = MT + 12;
      if (this.freqData && this.audioContext) {
        ctx.fillStyle = "rgba(41,182,246,0.9)";
        ctx.fillText("spectrum", ML + PW - 2, legendY); legendY += 13;
      }
      if (aeEnabled) {
        ctx.fillStyle = "#e74c3c";
        ctx.fillText(`AE ${ae.harmonics.toFixed(0)}% @${ae.tune}Hz`, ML + PW - 2, legendY); legendY += 13;
      }
      if (bbEnabled) {
        ctx.fillStyle = "#aaff00";
        ctx.fillText(`BB ${bb.drive.toFixed(0)}% @${bb.tune}Hz`, ML + PW - 2, legendY);
      }
      if (!aeEnabled && !bbEnabled && !(this.freqData && this.audioContext)) {
        ctx.fillStyle = "#333"; ctx.textAlign = "center";
        ctx.fillText("Start mic or enable AE/BB", ML + PW / 2, MT + PH / 2);
      }

      // Animation frame
      requestAnimationFrame(draw.bind(this));
    };

    draw.call(this);
  }

  initializeAudio() {
    const startBtn = document.querySelector(".audio-controls .btn:first-of-type");
    const stopBtn = document.querySelector(".audio-controls .btn:first-of-type + .btn");
    const vuBar = document.querySelector(".vu-bar");
    const vuLabel = document.querySelector(".vu-label");

    if (!startBtn || !stopBtn || !vuBar || !vuLabel) return;

    startBtn.disabled = false;
    stopBtn.disabled = true;

    startBtn.addEventListener("click", async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({ audio: true, video: false });
        this.audioContext = new AudioContext();
        const source = this.audioContext.createMediaStreamSource(stream);

        this.analyser = this.audioContext.createAnalyser();
        this.analyser.fftSize = 2048;
        this.analyser.smoothingTimeConstant = 0.8;
        source.connect(this.analyser);

        // Store for EQ viz
        this.freqData = new Float32Array(this.analyser.frequencyBinCount);
        this.timeData = new Float32Array(this.analyser.fftSize);
        this.audioStream = stream;

        startBtn.disabled = true;
        stopBtn.disabled = false;
        console.log("Audio monitoring started (live mic)");

        const updateAudio = () => {
          if (!this.analyser) return;

          this.analyser.getFloatTimeDomainData(this.timeData);
          this.analyser.getFloatFrequencyData(this.freqData);

          // Calculate RMS level in dB
          let sum = 0;
          for (let i = 0; i < this.timeData.length; i++) {
            sum += this.timeData[i] * this.timeData[i];
          }
          const rms = Math.sqrt(sum / this.timeData.length);
          const db = rms > 0 ? 20 * Math.log10(rms) : -100;
          const clampedDb = Math.max(-60, Math.min(0, db));

          // Feed real level into visualizations
          this.vizState.compressor.currentInput = clampedDb;
          this.vizState.compressor.currentOutput = this.vizState.compressor.enabled
            ? this.calculateCompressorOutput(
                clampedDb,
                this.vizState.compressor.threshold,
                this.vizState.compressor.ratio,
                this.vizState.compressor.gain,
              )
            : clampedDb;
          this.vizState.gate.currentLevel = clampedDb;
          this.vizState.gate.levelHistory.push(clampedDb);
          if (this.vizState.gate.levelHistory.length > 400) {
            this.vizState.gate.levelHistory.shift();
          }

          // Update VU meter
          const percentage = ((clampedDb + 60) / 60) * 100;
          vuBar.style.width = `${percentage}%`;
          vuLabel.textContent = `${clampedDb.toFixed(1)} dB`;
          if (clampedDb > -6) {
            vuBar.style.background = "linear-gradient(90deg, #F44336, #FF9800)";
          } else if (clampedDb > -18) {
            vuBar.style.background = "linear-gradient(90deg, #FF9800, #4CAF50)";
          } else {
            vuBar.style.background = "linear-gradient(90deg, #2196F3, #4CAF50)";
          }

          this.audioFrameId = requestAnimationFrame(updateAudio);
        };

        updateAudio();
      } catch (err) {
        console.error("Microphone access denied:", err);
        this.showNotification("Microphone access denied", "error");
      }
    });

    stopBtn.addEventListener("click", () => {
      if (this.audioStream) {
        this.audioStream.getTracks().forEach(t => t.stop());
        this.audioStream = null;
      }
      if (this.audioContext) {
        this.audioContext.close();
        this.audioContext = null;
      }
      if (this.audioFrameId) {
        cancelAnimationFrame(this.audioFrameId);
        this.audioFrameId = null;
      }
      this.analyser = null;
      this.freqData = null;
      this.timeData = null;

      // Reset to idle levels
      this.vizState.compressor.currentInput = -60;
      this.vizState.compressor.currentOutput = -60;
      this.vizState.gate.currentLevel = -60;

      vuBar.style.width = "0%";
      vuLabel.textContent = "-∞ dB";
      startBtn.disabled = false;
      stopBtn.disabled = true;
      console.log("Audio monitoring stopped");
    });
  }
}

// Initialize controller when DOM is loaded
document.addEventListener("DOMContentLoaded", () => {
  console.log("RODE DSP Web GUI initialized");

  // Create global controller instance
  window.dspController = new DSPController();

  // Auto-connect if URL has ?auto-connect parameter
  if (window.location.search.includes("auto-connect")) {
    setTimeout(() => {
      window.dspController.connect();
    }, 1000);
  }
});

// Export for module usage
if (typeof module !== "undefined" && module.exports) {
  module.exports = DSPController;
}
