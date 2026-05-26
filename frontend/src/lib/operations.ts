import { writable, derived } from 'svelte/store';

export type OperationStatus = 'pending' | 'running' | 'success' | 'failed';
export type OperationType = 'download' | 'update' | 'delete' | 'pipeline';

export interface Operation {
  id: string;
  type: OperationType;
  label: string;
  status: OperationStatus;
  progress: number;
  error?: string;
  startedAt?: number;
}

export const operations = writable<Operation[]>([]);

export const activeOperation = derived(operations, ($ops) =>
  $ops.find((op) => op.status === 'running') ?? null
);

export function addOperation(op: Omit<Operation, 'startedAt'>) {
  operations.update((list) => {
    const existing = list.findIndex((o) => o.id === op.id);
    const entry: Operation = { ...op, startedAt: Date.now() };
    if (existing >= 0) {
      list[existing] = entry;
      return [...list];
    }
    return [...list, entry];
  });
}

export function updateOperation(id: string, partial: Partial<Operation>) {
  operations.update((list) =>
    list.map((op) => (op.id === id ? { ...op, ...partial } : op))
  );
}

export function removeOperation(id: string) {
  operations.update((list) => list.filter((op) => op.id !== id));
}

export function clearCompleted() {
  operations.update((list) =>
    list.filter((op) => op.status !== 'success' && op.status !== 'failed')
  );
}
