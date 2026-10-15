import { followsRelay } from "../client.js"

/** Only the installed Mac shell can reopen itself after the staged app swaps. */
export function canRestartStagedApp(): boolean {
  return !followsRelay() && Boolean(window.webkit?.messageHandlers?.shellRestartApp)
}

export function restartStagedApp(version: string): boolean {
  if (!canRestartStagedApp() || !version) return false
  try {
    window.webkit!.messageHandlers!.shellRestartApp!.postMessage({ version })
    return true
  } catch {
    return false
  }
}
