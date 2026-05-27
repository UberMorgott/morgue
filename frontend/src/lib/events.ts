// Wails v3 event helpers
import { Events } from '@wailsio/runtime';

type EventCallback = (data: any) => void;

// NOTE: Wails v3 Events.Off(name) removes ALL listeners for the given event name,
// not a specific callback. If multiple components listen to the same event,
// calling Off in one component's cleanup will remove the other's listener too.
// For now this is acceptable since each event name has at most one listener.
export function onEvent(name: string, callback: EventCallback): () => void {
  Events.On(name, callback);
  return () => Events.Off(name);
}
