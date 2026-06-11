import { writable } from 'svelte/store';
import { detectLang, type Lang } from './i18n';

export const currentLang = writable<Lang>(detectLang());

// True while startup auto-update checks are running. Blocks decompilation.
export const startupBusy = writable<boolean>(true);

// Incremented each time an API "run" command arrives. HomePage watches this
// store and starts the pipeline, bypassing the reactive inputPath guard.
export const apiRunSeq = writable<number>(0);

// Run-trigger guards, kept at module scope so they survive HomePage unmount/
// remount. Leaving Home and returning must NOT re-launch the pipeline.
// lastRunPath = the input path the pipeline last started for;
// lastApiRunSeq = the apiRunSeq value already acted on.
export const lastRunPath = writable<string>('');
export const lastApiRunSeq = writable<number>(0);

// --- App self-update progress ---
// Mirrors selfupdate.Progress emitted from Go via the `update:progress` event.
export type UpdatePhase = 'downloading' | 'installing' | 'done' | 'error' | '';
export interface UpdateProgress {
  active: boolean;
  phase: UpdatePhase;
  downloaded: number;
  total: number;
  percent: number;
  version: string;
  error: string;
}

const emptyUpdate: UpdateProgress = {
  active: false,
  phase: '',
  downloaded: 0,
  total: 0,
  percent: 0,
  version: '',
  error: '',
};

export const updateProgress = writable<UpdateProgress>({ ...emptyUpdate });

export function resetUpdateProgress() {
  updateProgress.set({ ...emptyUpdate });
}

// Persist language choice to localStorage
currentLang.subscribe((lang) => {
  try {
    localStorage.setItem('morgue-lang', lang);
  } catch {}
});
