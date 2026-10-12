type Place = { id: string; projectId?: string }

// The pinned lifecycle renderer reads boardProjectId before the directory ID.
// Start Places carry the same canonical identity as projectId; expose it under
// that legacy name without changing the directory ID used by page addresses.
export function lifecyclePlace<T extends Place>(place: T): T & { boardProjectId?: string } {
  return place.projectId ? { ...place, boardProjectId: place.projectId } : place
}
