/* ============================================================
   graph-audio.ts — graph-view audio toggle (#1932).

   Thin wrapper around the WebAudio "blip" primitive from flow-audio.ts.
   The two views (flows + graph) share the same playStepBlip but have
   independent localStorage toggles, so a user can enable audio on the
   graph without re-enabling it on flows or vice-versa.

   Key per spec: localStorage["archigraph:graph:audio"] = "on" | "off".
   Default OFF.
   ============================================================ */

export { playStepBlip, suspendAudioCtx as suspendGraphAudio } from "./flow-audio";

export const GRAPH_AUDIO_KEY = "archigraph:graph:audio";

export function readGraphAudio(): boolean {
  if (typeof localStorage === "undefined") return false;
  try {
    return localStorage.getItem(GRAPH_AUDIO_KEY) === "on";
  } catch {
    return false;
  }
}

export function writeGraphAudio(on: boolean): void {
  if (typeof localStorage === "undefined") return;
  try {
    localStorage.setItem(GRAPH_AUDIO_KEY, on ? "on" : "off");
  } catch {
    /* storage unavailable */
  }
}
