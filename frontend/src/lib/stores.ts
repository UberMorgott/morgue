import { writable } from 'svelte/store';
import { detectLang, type Lang } from './i18n';

export const currentLang = writable<Lang>(detectLang());

// True while startup auto-update checks are running. Blocks decompilation.
export const startupBusy = writable<boolean>(true);

// Persist language choice to localStorage
currentLang.subscribe((lang) => {
  try {
    localStorage.setItem('morgue-lang', lang);
  } catch {}
});
