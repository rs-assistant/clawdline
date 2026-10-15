import AppKit
import Foundation

/// The page offers this only after the daemon has staged a newer app, and
/// StagedAppWatcher calls it when nobody is using the app. The helper outlives
/// this process, waits for the daemon to replace the bundle, then opens that
/// exact installed path. It does not touch the service.
///
/// `behind` reopens without bringing the app forward (`open -g`), for a
/// restart the person did not ask for. `console` says the window was on
/// screen, so the new process puts it back (ReopenArgument).
func restartForStagedUpdate(_ version: String, behind: Bool = false, console: Bool = false) -> Bool {
    guard serviceMode != nil,
          version.range(of: #"^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$"#,
                        options: .regularExpression) != nil else { return false }
    let app = Bundle.main.bundleURL.path
    let pid = String(ProcessInfo.processInfo.processIdentifier)
    let expected = version.hasPrefix("v") ? String(version.dropFirst()) : version
    let script = """
    pid="$1"; app="$2"; expected="$3"; behind="$4"; console="$5"
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
    flags=""
    [ "$behind" = 1 ] && flags="-g"
    if [ -n "$console" ]; then
      /usr/bin/open $flags "$app" --args "$console"
    else
      /usr/bin/open $flags "$app"
    fi
    """
    let helper = Process()
    helper.executableURL = URL(fileURLWithPath: "/bin/sh")
    let consoleArgument = console ? (behind ? ReopenArgument.consoleBehind : ReopenArgument.console) : ""
    helper.arguments = ["-c", script, "clawdline-reopen", pid, app, expected, behind ? "1" : "0", consoleArgument]
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

/// What a restart for an update tells the process it opens about the window.
enum ReopenArgument {
    /// The window was on screen and the app in front: show it as usual.
    static let console = "--show-console"
    /// The window was on screen behind other apps: put it back there, without
    /// taking the front from whoever has it.
    static let consoleBehind = "--show-console-behind"

    static var requested: String? {
        CommandLine.arguments.first { $0 == console || $0 == consoleBehind }
    }
}
