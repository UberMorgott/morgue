// Wails v3 event helpers
import { Events } from '@wailsio/runtime';

type EventCallback = (data: any) => void;

// Events.On returns an unsubscribe function that removes only this listener,
// so multiple components can safely listen to the same event name.
export function onEvent(name: string, callback: EventCallback): () => void {
  return Events.On(name, callback);
}
