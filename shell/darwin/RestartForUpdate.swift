import AppKit
import Foundation

/// The page offers this only after the daemon has staged a newer app. The
/// helper outlives this process, waits for the daemon to replace the bundle,
/// then opens that exact installed path. It does not touch the service.
func restartForStagedUpdate(_ version: String) -> Bool {
    guard serviceMode != nil,
          version.range(of: #"^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$"#,
                        options: .regularExpression) != nil else { return false }
    let app = Bundle.main.bundleURL.path
    let pid = String(ProcessInfo.processInfo.processIdentifier)
    let expected = version.hasPrefix("v") ? String(version.dropFirst()) : version
    let script = """
    pid="$1"; app="$2"; expected="$3"
    n=0
    while /bin/kill -0 "$pid" 2>/dev/null && [ "$n" -lt 300 ]; do
      /bin/sleep 0.1; n=$((n+1))
    done
    if /bin/kill -0 "$pid" 2>/dev/null; then
      /usr/bin/logger -t clawdline 'app update: old shell did not quit; relaunch cancelled'
      exit 1
    fi
    n=0
    while [ "$n" -lt 900 ]; do
      actual=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$app/Contents/Info.plist" 2>/dev/null)
      [ "$actual" = "$expected" ] && break
      /bin/sleep 0.1; n=$((n+1))
    done
    if [ "$actual" != "$expected" ]; then
      /usr/bin/logger -t clawdline 'app update: staged bundle did not replace the installed app before relaunch'
    fi
    /usr/bin/open "$app"
    """
    let helper = Process()
    helper.executableURL = URL(fileURLWithPath: "/bin/sh")
    helper.arguments = ["-c", script, "clawdline-reopen", pid, app, expected]
    helper.standardOutput = FileHandle.nullDevice
    helper.standardError = FileHandle.nullDevice
    do {
        try helper.run()
        shellLog("update: app reopening after staged bundle replaces this one")
        DispatchQueue.main.async { NSApp.terminate(nil) }
        return true
    } catch {
        shellLog("update: could not start app reopen helper: \(error)")
        return false
    }
}
