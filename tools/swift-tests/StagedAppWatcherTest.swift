import AppKit
import Foundation

let serviceMode: String? = "test"
enum LocalToken { static func read() -> String? { "test-token" } }
func shellLog(_ message: String) {}
func restartForStagedUpdate(_ version: String, behind: Bool = false, console: Bool = false) -> Bool { false }

@main struct StagedAppWatcherTest {
    static func main() {
        let home = URL(string: "http://127.0.0.1:7727/")!
        let empty = Data(#"{"apply":{"state":"idle"}}"#.utf8)
        let watcher = StagedAppWatcher(home: home)
        var paths: [String] = []
        watcher.load = { request, done in
            paths.append(request.url!.path)
            let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!
            done(empty, response, nil)
        }
        watcher.start()
        RunLoop.main.run(until: Date().addingTimeInterval(StagedAppWatcher.firstLookSeconds + 0.2))
        precondition(paths == ["/v1/update/fresh"], "launch must check the source: \(paths)")
        watcher.becameActive()
        precondition(paths == ["/v1/update/fresh", "/v1/update/fresh"], "foreground return must check again: \(paths)")

        let failing = StagedAppWatcher(home: home)
        var fallback: [String] = []
        failing.load = { request, done in
            fallback.append(request.url!.path)
            if fallback.count == 1 {
                done(nil, nil, URLError(.timedOut))
            } else {
                let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!
                done(empty, response, nil)
            }
        }
        failing.becameActive()
        precondition(fallback == ["/v1/update/fresh", "/v1/update"], "failed fresh check must read cache: \(fallback)")
    }
}
