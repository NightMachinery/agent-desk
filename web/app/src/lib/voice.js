import worklet from './voice-worklet.js?worker&url';
import { post, uploadVoice } from './api.js';

const voicePattern = /\[voice (v-[a-z0-9]{12}) \d+:[0-5]\d, (?:recording|transcribing|failed)\]/g;

export function voiceTokens(text) {
  return [...text.matchAll(voicePattern)].map((match) => {
    const [token, id] = match;
    const start = match.index;
    return { id, token, start, end: start + token.length };
  });
}

export function voiceToken(rec) {
  const secs = Math.floor(rec.bytes / 32000);
  return `[voice ${rec.id} ${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, '0')}, ${rec.status}]`;
}

export function transcriptBlock(text) {
  const trimmed = text.trim();
  let fenceLength = 3;
  for (const [run] of trimmed.matchAll(/`+/g)) fenceLength = Math.max(fenceLength, run.length + 1);
  const fence = '`'.repeat(fenceLength);
  return `${fence}speech-to-text\n${trimmed}\n${fence}`;
}

export function placeBlock(text, start, end, block) {
  const left = text.slice(0, start);
  const right = text.slice(end);
  return (left && !left.endsWith('\n\n') ? (left.endsWith('\n') ? '\n' : '\n\n') : '') + block
    + (right && !right.startsWith('\n\n') ? (right.startsWith('\n') ? '\n' : '\n\n') : '');
}

// The microphone and the browser's uploader have separate lifetimes. Once
// stopped, queued chunks drain before finishing the recording on the server.
export async function captureVoice(node, onUpdate, onError) {
  if (!navigator.mediaDevices?.getUserMedia || !window.AudioContext) {
    throw new Error('Voice recording requires microphone access on HTTPS or localhost.');
  }
  const context = new AudioContext();
  /** @type {MediaStream | undefined} */
  let mic;
  let rec;
  try {
    const resumed = context.resume();
    void resumed.catch(() => {});
    const stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1 }, video: false });
    mic = stream;
    await resumed;
    if (!context.audioWorklet) throw new Error('This browser does not support microphone streaming.');
    await context.audioWorklet.addModule(worklet);
    const input = context.createMediaStreamSource(stream);
    const processor = new AudioWorkletNode(context, 'desk-pcm', { channelCount: 1 });
    const mute = context.createGain();
    mute.gain.value = 0;
    rec = await post('recordings', { node });
    let offset = 0;
    let pending = 0;
    let stopped = false;
    let uploadFailed = false;
    let overflowed = false;
    let upload = Promise.resolve();
    let finish;
    const ended = new Promise((resolve) => { finish = resolve; });
    const name = `recordings/${rec.id}`;

    function stop() {
      if (!stopped) {
        stopped = true;
        processor.port.postMessage('stop');
      }
      return ended;
    }
    processor.port.onmessage = ({ data }) => {
      if (data === null) {
        stream.getTracks().forEach((t) => t.stop());
        void context.close();
        void upload.then(async () => {
          try {
            onUpdate(await post(name, { action: 'finish' }));
          } catch (err) {
            onError(err);
          } finally {
            finish();
          }
        });
        return;
      }
      if (uploadFailed || overflowed) return;
      if (pending >= 128) {
        overflowed = true;
        onError(new Error('Audio upload fell behind. The desk kept the received audio.'));
        stop();
        return;
      }
      pending++;
      upload = upload.then(async () => {
        if (uploadFailed) return;
        const next = await uploadVoice(name, offset, data);
        offset = next.bytes;
        onUpdate(next);
      }).catch((err) => {
        uploadFailed = true;
        onError(err);
        stop();
      }).finally(() => { pending--; });
    };
    stream.getAudioTracks().forEach((track) => track.addEventListener('ended', stop, { once: true }));
    input.connect(processor).connect(mute).connect(context.destination);
    return { rec, stop, ended };
  } catch (err) {
    mic?.getTracks().forEach((t) => t.stop());
    void context.close();
    if (rec) void post(`recordings/${rec.id}`, { action: 'finish' }).catch(() => {});
    throw err;
  }
}
