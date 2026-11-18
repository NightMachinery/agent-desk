// Convert capture to mono 16 kHz signed PCM. A weighted box filter averages
// samples across render boundaries, including at non-integer input rates.
class DeskPCM extends AudioWorkletProcessor {
  step = sampleRate / 16000;
  remaining = this.step;
  integral = 0;
  count = 0;
  buffer = new ArrayBuffer(8000);
  samples = new DataView(this.buffer);
  stopped = false;

  constructor() {
    super();
    this.port.onmessage = () => {
      this.stopped = true;
      this.flush();
      this.port.postMessage(null);
    };
  }

  flush() {
    if (!this.count) return;
    const chunk = this.count * 2 === this.buffer.byteLength ? this.buffer : this.buffer.slice(0, this.count * 2);
    this.port.postMessage(chunk, [chunk]);
    this.count = 0;
    this.buffer = new ArrayBuffer(8000);
    this.samples = new DataView(this.buffer);
  }

  process(inputs) {
    if (this.stopped) return false;
    for (const sample of inputs[0]?.[0] ?? []) {
      let width = 1;
      while (width > 1e-8) {
        const span = Math.min(this.remaining, width);
        this.integral += span * sample;
        this.remaining -= span;
        width -= span;
        if (this.remaining <= 1e-8) {
          const mono = Math.max(-1, Math.min(1, this.integral / this.step));
          this.samples.setInt16(2 * this.count++, Math.round(mono * 32767), true);
          this.integral = 0;
          this.remaining = this.step;
          if (this.count * 2 === this.buffer.byteLength) this.flush();
        }
      }
    }
    return true;
  }
}

registerProcessor('desk-pcm', DeskPCM);
