// AudioWorklet processor that converts the mic Float32 stream to Int16 PCM
// chunks at whatever sample rate the parent AudioContext is configured for
// (we use 16 kHz for Gemini Live). Runs off the main thread.
class PcmCaptureProcessor extends AudioWorkletProcessor {
  process(inputs) {
    const input = inputs[0];
    if (!input || input.length === 0) return true;
    const channel = input[0];
    if (!channel || channel.length === 0) return true;

    const i16 = new Int16Array(channel.length);
    for (let i = 0; i < channel.length; i++) {
      let s = channel[i];
      if (s > 1) s = 1;
      else if (s < -1) s = -1;
      i16[i] = s < 0 ? s * 0x8000 : s * 0x7fff;
    }
    // Transfer ownership of the underlying buffer for zero-copy messaging.
    this.port.postMessage(i16, [i16.buffer]);
    return true;
  }
}

registerProcessor('pcm-capture', PcmCaptureProcessor);
