/// <reference types="svelte" />
/// <reference types="vite/client" />

// AudioWorklet runs in its own global scope, separate from the DOM.
declare const sampleRate: number;
declare abstract class AudioWorkletProcessor {
  readonly port: MessagePort;
}
declare function registerProcessor(name: string, processor: typeof AudioWorkletProcessor): void;
