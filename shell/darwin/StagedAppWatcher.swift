import AppKit
import Foundation

/// A newer app the daemon has staged is put in place without the person
/// having to know it is there.
///
/// A healthy release update replaces the daemon at once but leaves the app
/// bundle staged while the app runs, and the daemon swaps it in only after the
/// app quits (docs/updates.md). Left to the person, that never happened: on
/// 2026-10-15 the installed app stayed at 1.0.1 through four releases, because
/// closing the window does not quit a menu bar app and a quick quit-and-reopen
/// started the old bundle again. So the shell asks the daemon itself, and
/// restarts into the staged bundle when that costs the person nothing:
///
/// - within a minute of launching, when the person has only just opened it;
/// - otherwise, when the app is not in front and the input bar is not open.
///
/// The restart reopens behind other apps when the app was not in front, and
/// puts the window back when it was on screen (ReopenArgument). A version is
/// tried at most once every half hour, so a swap that keeps failing does not
/// become an app that keeps restarting.
final class StagedAppWatcher {
    static let pollSeconds: TimeInterval = 30
    static let firstLookSeconds: TimeInterval = 3
    static let launchGraceSeconds: TimeInterval = 60
    static let retrySeconds: TimeInterval = 30 * 60

    /// Whether the person is using the app now.
    var inUse: () -> Bool = { true }
    /// Whether the console window is on screen.
    var consoleOnScreen: () -> Bool = { false }
    /// The request seam lets the shell test launch and foreground behavior.
    var load: (URLRequest, @escaping (Data?, URLResponse?, Error?) -> Void) -> Void = { request, done in
        URLSession.shared.dataTask(with: request, completionHandler: done).resume()
    }

    private let home: URL
    private let launchedAt = Date()
    private var timer: Timer?
    private var restarting = false

    init(home: URL) { self.home = home }

    func start() {
        DispatchQueue.main.asyncAfter(deadline: .now() + Self.firstLookSeconds) { [weak self] in self?.look(fresh: true) }
        guard serviceMode != nil else { return }
        timer = Timer.scheduledTimer(withTimeInterval: Self.pollSeconds, repeats: true) { [weak self] _ in
            self?.look(fresh: false)
        }
    }

    func becameActive() { look(fresh: true) }

    private func look(fresh: Bool) {
        guard !restarting, let token = LocalToken.read() else { return }
        var request = URLRequest(url: home.appendingPathComponent(fresh ? "v1/update/fresh" : "v1/update"))
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.timeoutInterval = fresh ? 12 : 10
        load(request) { [weak self] data, response, _ in
            if (response as? HTTPURLResponse)?.statusCode == 200, let data {
                if let version = StagedApp.version(in: data) {
                    DispatchQueue.main.async { self?.consider(version) }
                }
            } else if fresh {
                self?.look(fresh: false)
            }
        }
    }

    private func consider(_ version: String) {
        let own = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? ""
        let bare = version.hasPrefix("v") ? String(version.dropFirst()) : version
        guard !restarting, bare != own else { return }
        let justLaunched = Date().timeIntervalSince(launchedAt) < Self.launchGraceSeconds
        let front = inUse()
        guard justLaunched || !front else { return }
        let key = "stagedAppRestartTried.\(bare)"
        if let last = UserDefaults.standard.object(forKey: key) as? Date,
           Date().timeIntervalSince(last) < Self.retrySeconds { return }
        UserDefaults.standard.set(Date(), forKey: key)
        shellLog("update: restarting into staged app \(version) from \(own)"
                 + " (\(justLaunched ? "just launched" : "not in use"))")
        restarting = restartForStagedUpdate(version, behind: !front, console: consoleOnScreen())
    }
}

enum StagedApp {
    /// The release a healthy update left an app bundle staged for, or nil.
    static func version(in data: Data) -> String? {
        guard let body = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let apply = body["apply"] as? [String: Any],
              apply["state"] as? String == "healthy",
              let staged = apply["staged_app"] as? String, !staged.isEmpty,
              let to = apply["to"] as? String, !to.isEmpty else { return nil }
        return to
    }
}
