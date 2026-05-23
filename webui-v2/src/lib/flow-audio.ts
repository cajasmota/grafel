/* ============================================================
   flow-audio.ts — optional WebAudio "blip" on step arrival.

   Behavior (per #1922):
     • OFF BY DEFAULT (callers must explicitly toggle).
     • Persisted in localStorage["archigraph:flows:audio"] as "on"/"off".
     • One shared AudioContext lazily constructed on first blip.
     • Sine wave, ~440Hz, 60ms total, peak ~-20dB, fast attack + decay.

   No external deps; pure browser WebAudio. Safe under SSR (guards on
   typeof window / AudioContext).
   ============================================================ */

export const FLOW_AUDIO_KEY = "archigraph:flows:audio";

export function readFlowAudio(): boolean {
  if (typeof localStorage === "undefined") return false;
  try {
    return localStorage.getItem(FLOW_AUDIO_KEY) === "on";
  } catch {
    return false;
  }
}

export function writeFlowAudio(on: boolean): void {
  if (typeof localStorage === "undefined") return;
  try {
    localStorage.setItem(FLOW_AUDIO_KEY, on ? "on" : "off");
  } catch {
    /* storage unavailable */
  }
}

let ctx: AudioContext | null = null;

function getCtx(): AudioContext | null {
  if (typeof window === "undefined") return null;
  // Some test envs lack AudioContext; bail silently.
  const Ctor: typeof AudioContext | undefined =
    (window as unknown as { AudioContext?: typeof AudioContext }).AudioContext ??
    (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!Ctor) return null;
  if (!ctx) {
    try {
      ctx = new Ctor();
    } catch {
      ctx = null;
    }
  }
  return ctx;
}

/**
 * Suspend the shared AudioContext so any residual oscillator nodes stop
 * producing output immediately. Called on route unmount (#1954) to silence
 * audio that may be mid-play when the user navigates away from the graph view.
 * The context is resumed automatically by playStepBlip() on the next blip.
 */
export function suspendAudioCtx(): void {
  if (!ctx || ctx.state === "suspended" || ctx.state === "closed") return;
  try {
    void ctx.suspend();
  } catch {
    /* best-effort */
  }
}

/**
 * Play a short sine-wave blip. No-op on failure (unsupported, suspended,
 * etc.) — audio is strictly optional polish.
 */
export function playStepBlip(): void {
  const c = getCtx();
  if (!c) return;
  try {
    // Resume the context if it was suspended (autoplay rules).
    if (c.state === "suspended") void c.resume();

    const now = c.currentTime;
    const osc = c.createOscillator();
    const gain = c.createGain();
    osc.type = "sine";
    osc.frequency.setValueAtTime(440, now);

    // -20 dB ~= linear 0.1 amplitude; quick attack + decay over ~60ms.
    gain.gain.setValueAtTime(0.0001, now);
    gain.gain.exponentialRampToValueAtTime(0.1, now + 0.006);
    gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.06);

    osc.connect(gain).connect(c.destination);
    osc.start(now);
    osc.stop(now + 0.07);
  } catch {
    /* swallow — best-effort */
  }
}
