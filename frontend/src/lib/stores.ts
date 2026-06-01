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

// Persist language choice to localStorage
currentLang.subscribe((lang) => {
  try {
    localStorage.setItem('morgue-lang', lang);
  } catch {}
});
