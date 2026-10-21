// Explicit Cloud pairing handovers through the authenticated local daemon.
import AppKit
import Foundation
import WebKit

// MARK: - Pairing another machine through this Mac's assistant

/// What the daemon answers `POST /v1/cloud/pairing/agent` with, or refuses it
/// with.
private struct DaemonPairAgent: Decodable {
    let mode: String?
    let taskID: String?
    let trust: String?
    let expiresAt: Double?
    let error: Refusal?

    struct Refusal: Decodable {
        let code: String?
        let message: String?
    }

    enum CodingKeys: String, CodingKey {
        case mode, error, trust
        case taskID = "task_id"
        case expiresAt = "expires_at"
    }
}

/// What `GET /v1/cloud/pairing/agent/<task_id>` answers: a word for where the
/// hand-off is and, once it ended, the task's own sentence. Never the offer.
private struct DaemonPairAgentProgress: Decodable, Equatable {
    let state: String
    let summary: String?
    let failedReason: String?

    enum CodingKeys: String, CodingKey {
        case state, summary
        case failedReason = "failed_reason"
    }

    var ended: Bool { state == "done" || state == "failed" }
}

/// The Cloud tab's way of asking this Mac to carry a pairing offer to another
/// machine.
///
/// The Cloud console, shown a machine this browser is not paired with, offers a
/// command to paste on that machine. This Mac often reaches that machine
/// already — an ssh alias, a cloud provider's session manager — so the page
/// can post the offer here instead (`clawdlinePairAgent`), and an assistant on
/// this Mac runs the same command over that access. The offer travels over the
/// person's own channel either way, never through Cloud.
///
/// Three things keep this from being a way for a page to start work on this
/// Mac:
///
/// - It is registered on the Cloud view's content controller only, and it
///   answers only the main frame of Cloud's own origin.
/// - The person is asked, in a native sheet naming the machine and showing the
///   command, before anything runs.
/// - The page supplies an offer, an id and a name, and no text: the daemon
///   checks all three and writes the instructions from its own template.
///
/// The offer is never logged, and the sheet shows only its first characters.
@MainActor
final class CloudPairAgent: NSObject, WKScriptMessageHandlerWithReply {
    static let name = "clawdlinePairAgent"

    /// One question at a time: a second press while the sheet is up is not a
    /// second request.
    private var asking = false

    func userContentController(_ controller: WKUserContentController,
                               didReceive message: WKScriptMessage,
                               replyHandler: @escaping (Any?, String?) -> Void) {
        let refuse = { (why: String) in replyHandler(["ok": false, "error": why], nil) }
        guard message.frameInfo.isMainFrame, isCloudOrigin(message.frameInfo.securityOrigin),
              let page = message.webView?.url, isCloud(page) else {
            shellLog("cloud: refused a pairing hand-off from outside Cloud's main frame")
            refuse("not_cloud")
            return
        }
        guard let body = message.body as? [String: Any],
              let offer = body["offer"] as? String, !offer.isEmpty, offer.utf8.count <= 4096,
              let machineID = body["machine_id"] as? String, !machineID.isEmpty, machineID.utf8.count <= 256 else {
            refuse("bad_request")
            return
        }
        let shownName = (body["machine_name"] as? String).map(Self.oneLine) ?? ""
        let name = shownName.isEmpty ? machineID : shownName
        guard let window = message.webView?.window, !asking else {
            refuse("busy")
            return
        }
        asking = true
        let alert = NSAlert()
        alert.messageText = L.t.pairAgentAsks(name)
        alert.informativeText = L.t.pairAgentCommand(String(offer.prefix(12)) + "…")
        alert.addButton(withTitle: L.t.pairAgentStart)
        alert.addButton(withTitle: L.t.dialogCancel)
        alert.beginSheetModal(for: window) { [weak self] response in
            guard response == .alertFirstButtonReturn else {
                self?.asking = false
                shellLog("cloud: the person declined handing the pairing with \(machineID) to this Mac")
                refuse("cancelled")
                return
            }
            let view = message.webView
            Task { @MainActor in
                defer { self?.asking = false }
                let (reply, expiresAt) = await Self.handOff(offer: offer, machineID: machineID, machineName: shownName)
                replyHandler(reply, nil)
                if let taskID = reply["task_id"] as? String, !taskID.isEmpty, let view {
                    self?.follow(taskID: taskID, until: expiresAt, in: view)
                }
            }
        }
    }

    /// The one hand-off being followed; a new one replaces it.
    private var following: Task<Void, Never>?

    /// Follows a hand-off's task and tells the Cloud page each time it moves,
    /// so the card can show the AI starting, working, done, or why it
    /// stopped, instead of one sentence for the minute or two it takes.
    ///
    /// Every ~3 s until the task ends or the offer runs out. Each change is
    /// dispatched on the page's `window` as a `clawdline-pair-agent` event —
    /// only while the view is still on Cloud's origin; a view that went
    /// elsewhere is not told, and the following stops.
    private func follow(taskID: String, until expiresAt: Date?, in view: WKWebView) {
        following?.cancel()
        // Past the offer, the card has already said it ran out; a task with
        // no expiry is followed for its own timeout and a little more.
        let deadline = (expiresAt ?? Date().addingTimeInterval(16 * 60)).addingTimeInterval(5)
        following = Task { @MainActor [weak view] in
            var last: DaemonPairAgentProgress?
            while !Task.isCancelled, Date() < deadline {
                if let now = await Self.progress(taskID: taskID), now != last {
                    last = now
                    guard let view, let here = view.url, isCloud(here) else { return }
                    Self.tell(view, taskID: taskID, now)
                    if now.ended { return }
                }
                try? await Task.sleep(nanoseconds: 3_000_000_000)
            }
        }
    }

    private static func progress(taskID: String) async -> DaemonPairAgentProgress? {
        guard let token = LocalToken.read() else { return nil }
        var request = URLRequest(url: home.appendingPathComponent("v1/cloud/pairing/agent/\(taskID)"))
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.timeoutInterval = 10
        guard let (data, response) = try? await URLSession.shared.data(for: request),
              (response as? HTTPURLResponse)?.statusCode == 200 else { return nil }
        return try? JSONDecoder().decode(DaemonPairAgentProgress.self, from: data)
    }

    private static func tell(_ view: WKWebView, taskID: String, _ now: DaemonPairAgentProgress) {
        var detail: [String: Any] = ["task_id": taskID, "state": now.state]
        if let summary = now.summary { detail["summary"] = summary }
        if let reason = now.failedReason { detail["failed_reason"] = reason }
        guard let data = try? JSONSerialization.data(withJSONObject: detail),
              let json = String(data: data, encoding: .utf8) else { return }
        view.evaluateJavaScript(
            "window.dispatchEvent(new CustomEvent('clawdline-pair-agent', {detail: \(json)})); true",
            completionHandler: nil)
    }

    /// The daemon's route, with this Mac's own token, and when the offer runs
    /// out, for following the task it started.
    private static func handOff(offer: String, machineID: String, machineName: String) async -> ([String: Any], Date?) {
        guard let token = LocalToken.read() else {
            shellLog("cloud: no local token yet; cannot hand the pairing to this Mac")
            return (["ok": false, "error": "no_token"], nil)
        }
        var request = URLRequest(url: home.appendingPathComponent("v1/cloud/pairing/agent"))
        request.httpMethod = "POST"
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.timeoutInterval = 200
        guard let payload = try? JSONSerialization.data(withJSONObject: [
            "offer": offer, "machine_id": machineID, "machine_name": machineName,
        ]) else {
            return (["ok": false, "error": "bad_request"], nil)
        }
        request.httpBody = payload
        do {
            let (data, response) = try await URLSession.shared.data(for: request)
            let status = (response as? HTTPURLResponse)?.statusCode ?? 0
            let said = try? JSONDecoder().decode(DaemonPairAgent.self, from: data)
            guard status == 200, let mode = said?.mode else {
                // A refusal is words about the request, never the offer.
                let code = said?.error?.code ?? "http_\(status)"
                shellLog("cloud: this Mac refused the pairing hand-off with \(machineID): HTTP \(status) \(code)")
                return (["ok": false, "error": said?.error?.message ?? code], nil)
            }
            shellLog("cloud: handed the pairing with \(machineID) to this Mac (\(mode) \(said?.taskID ?? "") trust \(said?.trust ?? "-"))")
            let expires = said?.expiresAt.flatMap { $0 > 0 ? Date(timeIntervalSince1970: $0 / 1000) : nil }
            return (["ok": true, "mode": mode, "task_id": said?.taskID ?? "", "trust": said?.trust ?? ""], expires)
        } catch {
            shellLog("cloud: could not reach this Mac to hand the pairing over — \(error.localizedDescription)")
            return (["ok": false, "error": "unreachable"], nil)
        }
    }

    /// Cloud's own origin, as WebKit reports the frame's: the URL's pieces,
    /// with WebKit's 0 for a default port.
    private func isCloudOrigin(_ origin: WKSecurityOrigin) -> Bool {
        guard origin.`protocol`.lowercased() == cloudHome.scheme?.lowercased(),
              origin.host.lowercased() == cloudHome.host?.lowercased() else { return false }
        let asked: Int? = origin.port == 0 ? nil : origin.port
        return asked == cloudHome.port
    }

    /// A name for the sheet: one line, at most 64 characters.
    private static func oneLine(_ s: String) -> String {
        let flat = s.unicodeScalars
            .filter { !CharacterSet.controlCharacters.contains($0) || $0 == "\n" || $0 == "\t" }
            .map { CharacterSet.whitespacesAndNewlines.contains($0) ? " " : String($0) }
            .joined()
            .split(separator: " ", omittingEmptySubsequences: true)
            .joined(separator: " ")
        return String(flat.prefix(64))
    }
}


/// An explicit local handover. Unlike the assistant bridge, this handler has
/// no dispatch path: the authenticated daemon must name the requested machine.
@MainActor
final class CloudPairLocal: NSObject, WKScriptMessageHandlerWithReply {
    static let name = "clawdlinePairLocal"
    private var busy = false

    private func isAllowed(_ message: WKScriptMessage) -> Bool {
        let origin = message.frameInfo.securityOrigin
        return CloudPairingAccess.allows(mainFrame: message.frameInfo.isMainFrame,
            scheme: origin.`protocol`, host: origin.host, port: origin.port,
            page: message.webView?.url, trusted: cloudHome)
    }

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage,
                               replyHandler: @escaping (Any?, String?) -> Void) {
        guard isAllowed(message), let body = message.body as? [String: Any] else {
            replyHandler(["ok": false, "error": "not_cloud"], nil)
            return
        }
        guard !busy else { replyHandler(["ok": false, "error": "busy"], nil); return }
        busy = true
        Task { @MainActor in
            defer { busy = false }
            guard let status = await request("GET", path: "v1/cloud/status"),
                  let machine = status["machine_id"] as? String, !machine.isEmpty else {
                replyHandler(["ok": false, "error": "unavailable"], nil); return
            }
            if body["kind"] as? String == "status" {
                replyHandler(["ok": true, "machine_id": machine], nil); return
            }
            guard body["machine_id"] as? String == machine,
                  let offer = body["offer"] as? String, !offer.isEmpty, offer.utf8.count <= 4096,
                  isAllowed(message), let window = message.webView?.window else {
                replyHandler(["ok": false, "error": "not_this_machine"], nil); return
            }
            let alert = NSAlert()
            // The machine name comes from the authenticated local daemon, not
            // from hosted JavaScript. No code or credential appears in the sheet.
            alert.messageText = L.t.pairLocalAsks(status["machine_name"] as? String ?? machine)
            alert.informativeText = L.t.pairLocalBody
            alert.addButton(withTitle: L.t.pairLocalStart)
            alert.addButton(withTitle: L.t.dialogCancel)
            let accepted: Bool = await withCheckedContinuation { done in
                alert.beginSheetModal(for: window) { done.resume(returning: $0 == .alertFirstButtonReturn) }
            }
            guard accepted, isAllowed(message) else {
                replyHandler(["ok": false, "error": "cancelled"], nil); return
            }
            // Omitted scope preserves an existing pin. The normal offer route
            // verifies the account, expiry and cryptographic proof before saving.
            guard let result = await request("POST", path: "v1/cloud/pairing/offer", body: ["offer": offer]),
                  result["phase"] as? String == "paired" else {
                replyHandler(["ok": false, "error": "pairing_failed"], nil); return
            }
            replyHandler(["ok": true, "machine_id": machine], nil)
        }
    }

    private func request(_ method: String, path: String, body: [String: String]? = nil) async -> [String: Any]? {
        guard let token = LocalToken.read() else { return nil }
        var req = URLRequest(url: home.appendingPathComponent(path))
        req.httpMethod = method
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        req.timeoutInterval = 30
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try? JSONSerialization.data(withJSONObject: body)
        }
        guard let (data, response) = try? await URLSession.shared.data(for: req),
              (response as? HTTPURLResponse)?.statusCode == 200 else { return nil }
        return (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
    }
}
