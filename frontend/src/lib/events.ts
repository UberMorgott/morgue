// Wails event helpers — wraps @wailsio/runtime Events
// In stub mode these are no-ops; with real Wails runtime they subscribe to Go events

type EventCallback = (data: any) => void;

const listeners: Map<string, EventCallback[]> = new Map();

export function onEvent(name: string, callback: EventCallback): () => void {
  try {
    // Try real Wails runtime
    const { Events } = require('@wailsio/runtime');
    Events.On(name, callback);
    return () => Events.Off(name, callback);
  } catch {
    // Fallback: local event bus for dev
    if (!listeners.has(name)) listeners.set(name, []);
    listeners.get(name)!.push(callback);
    return () => {
      const cbs = listeners.get(name);
      if (cbs) {
        const idx = cbs.indexOf(callback);
        if (idx >= 0) cbs.splice(idx, 1);
      }
    };
  }
}

export function emitLocal(name: string, data: any) {
  const cbs = listeners.get(name);
  if (cbs) cbs.forEach(cb => cb(data));
}
