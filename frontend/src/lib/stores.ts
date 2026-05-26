import { writable } from 'svelte/store';
import { detectLang, type Lang } from './i18n';

export const currentLang = writable<Lang>(detectLang());

// Persist language choice to localStorage
currentLang.subscribe((lang) => {
  try {
    localStorage.setItem('morgue-lang', lang);
  } catch {}
});
