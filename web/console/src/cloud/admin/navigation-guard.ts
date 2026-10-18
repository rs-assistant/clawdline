// An issued code is readable only until this page closes. Keep the decision
// beside the route handler so links and browser history ask the same question.
let pendingCode: (() => boolean) | null = null

export function guardCodeNavigation(confirmLeave: () => boolean): () => void {
  pendingCode = confirmLeave
  return () => {
    if (pendingCode === confirmLeave) pendingCode = null
  }
}

export function mayLeaveCode(): boolean {
  return pendingCode?.() ?? true
}
