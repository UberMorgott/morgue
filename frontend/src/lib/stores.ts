import { writable } from 'svelte/store';
import { detectLang, type Lang } from './i18n';

export const currentLang = writable<Lang>(detectLang());

// True while startup auto-update checks are running. Blocks decompilation.
export const startupBusy = writable<boolean>(true);

// Run-trigger guard, kept at module scope so it survives HomePage unmount/
// remount. Leaving Home and returning must NOT re-launch the pipeline.
// lastRunPath = the input path the pipeline last started for.
export const lastRunPath = writable<string>('');

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
