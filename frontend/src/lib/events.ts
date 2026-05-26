// Wails v3 event helpers
import { Events } from '@wailsio/runtime';

type EventCallback = (data: any) => void;

export function onEvent(name: string, callback: EventCallback): () => void {
  Events.On(name, callback);
  return () => Events.Off(name);
}
