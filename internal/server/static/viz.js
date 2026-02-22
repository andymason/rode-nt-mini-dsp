// RODE NT-USB Mini DSP Controller - Visualization JavaScript
// Placeholder implementation - real visualizations planned per rode-dsp-cli-go-plan.md

console.log('RODE DSP Visualization library loaded (placeholder)');

// Visualization state
const vizState = {
    gate: {
        threshold: -42,
        hysteresis: 50,
        currentLevel: -60,
        waveform: [],
        sampleRate: 48,
        windowSize: 100
    },
    compressor: {
        threshold: -20,
        ratio: 3.0,
        currentInput: -30,
        currentOutput: -30,
        kneeWidth: 3
    },
    eq: {
        aeHarmonics: 49,
        aeTune: 3516,
        bbDrive: 62,
        bbTune: 131,
        sampleRate: 48,
        frequencyRange: [20, 20000]
    }
};

// Gate visualization placeholder
class GateViz {
    constructor(canvasId) {
        this.canvas = document.getElementById(canvasId);
        if (!this.canvas) {
            console.warn(`Canvas not found: ${canvasId}`);
            return;
        }
        this.ctx = this.canvas.getContext('2d');
        this.width = this.canvas.width;
        this.height = this.canvas.height;

        console.log(`GateViz initialized for canvas: ${canvasId}`);
        console.log('Planned: Draw waveform bars (green=open, grey=closed)');
        console.log('Planned: Draw threshold + hysteresis lines');
        console.log('Planned: Real-time audio level visualization');
    }

    updateViz(threshold, hysteresis, currentLevel, waveform) {
        console.log(`GateViz.updateViz: threshold=${threshold}, hysteresis=${hysteresis}, currentLevel=${currentLevel}`);

        // Clear canvas
        this.ctx.fillStyle = '#111';
        this.ctx.fillRect(0, 0, this.width, this.height);

        // Draw placeholder message
        this.ctx.fillStyle = '#666';
        this.ctx.font = '14px Arial';
        this.ctx.textAlign = 'center';
        this.ctx.fillText('Gate Visualization (Planned)', this.width / 2, this.height / 2 - 20);

        this.ctx.fillStyle = '#888';
        this.ctx.font = '12px Arial';
        this.ctx.fillText(`Threshold: ${threshold} dB, Hysteresis: ${hysteresis}%`, this.width / 2, this.height / 2);
        this.ctx.fillText(`Current Level: ${currentLevel} dB`, this.width / 2, this.height / 2 + 20);

        // Draw a simple representation
        this.ctx.strokeStyle = '#aaff00';
        this.ctx.lineWidth = 2;
        this.ctx.beginPath();

        // Horizontal line for threshold
        const thresholdY = this.height * 0.3;
        this.ctx.moveTo(20, thresholdY);
        this.ctx.lineTo(this.width - 20, thresholdY);
        this.ctx.stroke();

        // Text for threshold
        this.ctx.fillStyle = '#aaff00';
        this.ctx.font = '10px Arial';
        this.ctx.fillText(`${threshold} dB`, 10, thresholdY - 5);

        // Current level indicator
        const levelY = thresholdY + (currentLevel - threshold) * 2;
        this.ctx.fillStyle = currentLevel > threshold ? '#4CAF50' : '#F44336';
        this.ctx.beginPath();
        this.ctx.arc(this.width / 2, Math.max(10, Math.min(this.height - 10, levelY)), 8, 0, Math.PI * 2);
        this.ctx.fill();
    }

    dbToY(db) {
        // Convert dB to canvas Y coordinate
        const minDb = -60;
        const maxDb = 0;
        const normalized = (db - minDb) / (maxDb - minDb);
        return this.height * (1 - normalized);
    }
}

// Compressor visualization placeholder
class CompViz {
    constructor(canvasId) {
        this.canvas = document.getElementById(canvasId);
        if (!this.canvas) {
            console.warn(`Canvas not found: ${canvasId}`);
            return;
        }
        this.ctx = this.canvas.getContext('2d');
        this.width = this.canvas.width;
        this.height = this.canvas.height;

        console.log(`CompViz initialized for canvas: ${canvasId}`);
        console.log('Planned: Draw transfer curve with current-level dot');
        console.log('Planned: Show compression ratio and threshold');
        console.log('Planned: Real-time input/output level tracking');
    }

    updateViz(threshold, ratio, currentInput, currentOutput) {
        console.log(`CompViz.updateViz: threshold=${threshold}, ratio=${ratio}, input=${currentInput}, output=${currentOutput}`);

        // Clear canvas
        this.ctx.fillStyle = '#111';
        this.ctx.fillRect(0, 0, this.width, this.height);

        // Draw placeholder message
        this.ctx.fillStyle = '#666';
        this.ctx.font = '14px Arial';
        this.ctx.textAlign = 'center';
        this.ctx.fillText('Compressor Transfer Curve (Planned)', this.width / 2, this.height / 2 - 20);

        this.ctx.fillStyle = '#888';
        this.ctx.font = '12px Arial';
        this.ctx.fillText(`Threshold: ${threshold} dB, Ratio: ${ratio}:1`, this.width / 2, this.height / 2);
        this.ctx.fillText(`Input: ${currentInput} dB → Output: ${currentOutput} dB`, this.width / 2, this.height / 2 + 20);

        // Draw a simple transfer curve representation
        this.ctx.strokeStyle = '#2196F3';
        this.ctx.lineWidth = 2;
        this.ctx.beginPath();

        // Diagonal line for 1:1 compression (below threshold)
        const x1 = 20;
        const y1 = this.height - 20;
        const x2 = this.mapPoint(threshold, -60, 0, 20, this.width - 20);
        const y2 = this.mapPoint(threshold, -60, 0, this.height - 20, 20);

        this.ctx.moveTo(x1, y1);
        this.ctx.lineTo(x2, y2);

        // Compression line (above threshold)
        const compressionSlope = 1 / ratio;
        const x3 = this.width - 20;
        const y3 = y2 + (x3 - x2) * compressionSlope * (20 / (this.width - 40)) * (this.height - 40);

        this.ctx.lineTo(x3, y3);
        this.ctx.stroke();

        // Current input/output point
        const inputX = this.mapPoint(currentInput, -60, 0, 20, this.width - 20);
        const outputY = this.mapPoint(currentOutput, -60, 0, this.height - 20, 20);

        this.ctx.fillStyle = '#FF9800';
        this.ctx.beginPath();
        this.ctx.arc(inputX, outputY, 6, 0, Math.PI * 2);
        this.ctx.fill();

        // Labels
        this.ctx.fillStyle = '#888';
        this.ctx.font = '10px Arial';
        this.ctx.fillText('Input (dB)', this.width / 2, this.height - 5);
        this.ctx.save();
        this.ctx.translate(5, this.height / 2);
        this.ctx.rotate(-Math.PI / 2);
        this.ctx.fillText('Output (dB)', 0, 0);
        this.ctx.restore();
    }

    mapPt(x, y, xMin, xMax, yMin, yMax) {
        // Map point from data coordinates to canvas coordinates
        const canvasX = this.mapPoint(x, xMin, xMax, 20, this.width - 20);
        const canvasY = this.mapPoint(y, yMin, yMax, this.height - 20, 20);
        return { x: canvasX, y: canvasY };
    }

    mapPoint(value, inMin, inMax, outMin, outMax) {
        return ((value - inMin) / (inMax - inMin)) * (outMax - outMin) + outMin;
    }
}

// EQ visualization placeholder
class EQViz {
    constructor(canvasId) {
        this.canvas = document.getElementById(canvasId);
        if (!this.canvas) {
            console.warn(`Canvas not found: ${canvasId}`);
            return;
        }
        this.ctx = this.canvas.getContext('2d');
        this.width = this.canvas.width;
        this.height = this.canvas.height;

        console.log(`EQViz initialized for canvas: ${canvasId}`);
        console.log('Planned: Draw Gaussian frequency response combining AE + BB');
        console.log('Planned: Show harmonics and tune frequency effects');
        console.log('Planned: Frequency range: 20Hz - 20kHz (log scale)');
    }

    updateViz(aeHarmonics, aeTune, bbDrive, bbTune) {
        console.log(`EQViz.updateViz: AE=${aeHarmonics}% @ ${aeTune}Hz, BB=${bbDrive}% @ ${bbTune}Hz`);

        // Clear canvas
        this.ctx.fillStyle = '#111';
        this.ctx.fillRect(0, 0, this.width, this.height);

        // Draw placeholder message
        this.ctx.fillStyle = '#666';
        this.ctx.font = '14px Arial';
        this.ctx.textAlign = 'center';
        this.ctx.fillText('EQ Response Visualization (Planned)', this.width / 2, this.height / 2 - 30);

        this.ctx.fillStyle = '#888';
        this.ctx.font = '12px Arial';
        this.ctx.fillText(`Aural Exciter: ${aeHarmonics}% @ ${aeTune}Hz`, this.width / 2, this.height / 2);
        this.ctx.fillText(`Big Bottom: ${bbDrive}% @ ${bbTune}Hz`, this.width / 2, this.height / 2 + 20);

        // Draw simple frequency response representation
        this.ctx.strokeStyle = '#9C27B0';
        this.ctx.lineWidth = 2;
        this.ctx.beginPath();

        // AE response (higher frequency bump)
        const aeX = this.freqToX(aeTune);
        const aeHeight = (aeHarmonics / 100) * 30;

        // BB response (lower frequency bump)
        const bbX = this.freqToX(bbTune);
        const bbHeight = (bbDrive / 100) * 40;

        // Draw baseline
        this.ctx.moveTo(20, this.height / 2);

        // Draw BB bump
        if (bbHeight > 0) {
            const bbStartX = bbX - 30;
            const bbPeakX = bbX;
            const bbEndX = bbX + 30;

            this.ctx.lineTo(bbStartX, this.height / 2);
            this.ctx.quadraticCurveTo(bbPeakX, this.height / 2 - bbHeight, bbEndX, this.height / 2);
        }

        // Draw AE bump
        if (aeHeight > 0) {
            const aeStartX = aeX - 20;
            const aePeakX = aeX;
            const aeEndX = aeX + 20;

            this.ctx.lineTo(aeStartX, this.height / 2);
            this.ctx.quadraticCurveTo(aePeakX, this.height / 2 - aeHeight, aeEndX, this.height / 2);
        }

        this.ctx.lineTo(this.width - 20, this.height / 2);
        this.ctx.stroke();

        // Frequency labels
        this.ctx.fillStyle = '#888';
        this.ctx.font = '10px Arial';
        this.ctx.fillText('20Hz', 20, this.height - 5);
        this.ctx.fillText('1kHz', this.width / 2, this.height - 5);
        this.ctx.fillText('20kHz', this.width - 25, this.height - 5);

        // Gain label
        this.ctx.save();
        this.ctx.translate(5, this.height / 2);
        this.ctx.rotate(-Math.PI / 2);
        this.ctx.fillText('Gain (dB)', 0, 0);
        this.ctx.restore();
    }

    freqToX(freq) {
        // Convert frequency to X coordinate (log scale)
        const minFreq = 20;
        const maxFreq = 20000;
        const logMin = Math.log10(minFreq);
        const logMax = Math.log10(maxFreq);
        const logFreq = Math.log10(freq);

        const normalized = (logFreq - logMin) / (logMax - logMin);
        return 20 + normalized * (this.width - 40);
    }
}

// Initialize all visualizations
function initializeVisualizations() {
    console.log('Initializing RODE DSP visualizations');

    const gateViz = new GateViz('gate-viz');
    const compViz = new CompViz('comp-viz');
    const eqViz = new EQViz('eq-viz');

    // Update with current state
    if (gateViz.canvas) {
        gateViz.updateViz(
            vizState.gate.threshold,
            vizState.gate.hysteresis,
            vizState.gate.currentLevel,
            vizState.gate.waveform
        );
    }

    if (compViz.canvas) {
        compViz.updateViz(
            vizState.compressor.threshold,
            vizState.compressor.ratio,
            vizState.compressor.currentInput,
            vizState.compressor.currentOutput
        );
    }

    if (eqViz.canvas) {
        eqViz.updateViz(
            vizState.eq.aeHarmonics,
            vizState.eq.aeTune,
            vizState.eq.bbDrive,
            vizState.eq.bbTune
        );
    }

    return {
        gateViz,
        compViz,
        eqViz,
        vizState
    };
}

// Update visualization state
function updateVizState(newState) {
    if (newState.gate) Object.assign(vizState.gate, newState.gate);
    if (newState.compressor) Object.assign(vizState.compressor, newState.compressor);
    if (newState.eq) Object.assign(vizState.eq, newState.eq);

    console.log('Visualization state updated:', vizState);
}

// Public API
window.RODEViz = {
    GateViz,
    CompViz,
    EQViz,
    initializeVisualizations,
    updateVizState,
    vizState
};

// Auto-initialize when DOM is loaded
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initializeVisualizations);
} else {
    initializeVisualizations();
}

// Export for module usage
if (typeof module !== 'undefined' && module.exports) {
    module.exports = window.RODEViz;
}
