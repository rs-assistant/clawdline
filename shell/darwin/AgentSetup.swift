// The one-line installer has already installed the service and its own tmux.
// This window handles only the AI tool needed for a first local Session.
import AppKit
import Foundation

final class AgentSetupWindowController: NSObject, NSWindowDelegate {
    static let shared = AgentSetupWindowController()
    private override init() { super.init() }

    private var window: NSWindow?
    private var status: NSTextField!
    private var output: NSTextField!
    private var installButton: NSButton!
    private var signInButton: NSButton!
    private var cancelButton: NSButton!
    private var codex: URL?
    private var loginTask: Process?
    private var busy = false
    private var loginCancelled = false
    private var openedLoginPage = false
    private var onChange: (() -> Void)?

    private static func t(_ en: String, _ tw: String) -> String {
        let tag = Locale.preferredLanguages.first?.lowercased() ?? ""
        return tag.hasPrefix("zh-hant") || tag.hasPrefix("zh-tw") ? tw : en
    }

    private static func tool(_ name: String) -> URL? {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let paths = (ProcessInfo.processInfo.environment["PATH"] ?? "").split(separator: ":").map(String.init)
            + [home + "/.local/bin", home + "/.claude/local", "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"]
        for path in paths where path.hasPrefix("/") {
            let url = URL(fileURLWithPath: path).appendingPathComponent(name)
            if FileManager.default.isExecutableFile(atPath: url.path) { return url }
        }
        return nil
    }

    func show(onChange: @escaping () -> Void) {
        self.onChange = onChange
        if window == nil { build() }
        refresh()
        window?.center()
        NSApp.activate(ignoringOtherApps: true)
        window?.makeKeyAndOrderFront(nil)
    }

    private func build() {
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 600, height: 345),
                              styleMask: [.titled, .closable, .miniaturizable], backing: .buffered, defer: false)
        window.title = Self.t("AI tool setup", "AI 工具設定")
        window.isReleasedWhenClosed = false
        window.delegate = self
        let root = NSStackView()
        root.orientation = .vertical
        root.alignment = .leading
        root.spacing = 12
        root.edgeInsets = NSEdgeInsets(top: 20, left: 22, bottom: 20, right: 22)
        root.translatesAutoresizingMaskIntoConstraints = false
        window.contentView = root
        NSLayoutConstraint.activate([
            root.leadingAnchor.constraint(equalTo: window.contentView!.leadingAnchor),
            root.trailingAnchor.constraint(equalTo: window.contentView!.trailingAnchor),
            root.topAnchor.constraint(equalTo: window.contentView!.topAnchor),
            root.bottomAnchor.constraint(equalTo: window.contentView!.bottomAnchor),
        ])
        let intro = NSTextField(wrappingLabelWithString: Self.t(
            "Clawdline is installed. Choose Codex or Claude Code, then sign in with your own account before starting a Session.",
            "Clawdline 已安裝。請選擇 Codex 或 Claude Code，並以自己的帳號登入，再開始 Session。"))
        root.addArrangedSubview(intro)
        status = NSTextField(wrappingLabelWithString: "")
        status.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        root.addArrangedSubview(status)
        output = NSTextField(wrappingLabelWithString: "")
        output.isSelectable = true
        output.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        root.addArrangedSubview(output)
        let actions = NSStackView()
        actions.orientation = .vertical
        actions.alignment = .leading
        actions.spacing = 8
        let primary = NSStackView()
        primary.orientation = .horizontal
        primary.spacing = 8
        let secondary = NSStackView()
        secondary.orientation = .horizontal
        secondary.spacing = 8
        func button(_ row: NSStackView, _ title: String, _ action: Selector) -> NSButton {
            let b = NSButton(title: title, target: self, action: action)
            b.bezelStyle = .rounded
            row.addArrangedSubview(b)
            return b
        }
        installButton = button(primary, Self.t("Install Codex", "安裝 Codex"), #selector(installCodex))
        signInButton = button(primary, Self.t("Sign in to Codex", "登入 Codex"), #selector(signInCodex))
        cancelButton = button(primary, Self.t("Cancel sign-in", "取消登入"), #selector(cancelSignIn))
        _ = button(secondary, Self.t("Claude Code guide", "Claude Code 指引"), #selector(openClaudeGuide))
        _ = button(secondary, Self.t("Check again", "重新檢查"), #selector(checkAgain))
        actions.addArrangedSubview(primary)
        actions.addArrangedSubview(secondary)
        root.addArrangedSubview(actions)
        self.window = window
    }

    private func refresh(_ message: String? = nil) {
        codex = Self.tool("codex")
        let claude = Self.tool("claude")
        status.stringValue = message ?? (codex != nil
            ? Self.t("Codex found. Sign in if needed, then return to the Session picker and retry.",
                     "已找到 Codex。若尚未登入，請先登入，再回到 Session 選擇畫面重試。")
            : claude != nil
                ? Self.t("Claude Code found. Complete its sign-in, or install Codex here.",
                         "已找到 Claude Code。請完成其登入，或在這裡安裝 Codex。")
                : Self.t("No AI tool was found. Install Codex here, or follow the Claude Code guide.",
                         "找不到 AI 工具。可在這裡安裝 Codex，或依照 Claude Code 指引設定。"))
        installButton.isHidden = codex != nil
        installButton.isEnabled = !busy
        signInButton.isHidden = codex == nil
        signInButton.isEnabled = !busy
        cancelButton.isHidden = loginTask == nil
    }

    @objc private func checkAgain() { refresh(); output.stringValue = ""; onChange?() }

    @objc private func openClaudeGuide() {
        NSWorkspace.shared.open(URL(string: "https://code.claude.com/docs/en/setup")!)
    }

    @objc private func installCodex() {
        guard codex == nil, !busy else { return }
        let alert = NSAlert()
        alert.messageText = Self.t("Install Codex CLI?", "要安裝 Codex CLI 嗎？")
        alert.informativeText = Self.t(
            "Download pinned Codex 0.161.0 from OpenAI (about 98 MB), verify its checksum, OpenAI Developer ID and version, then copy it to ~/.local/bin/codex. Existing files are never replaced. Sign-in is a separate step.",
            "從 OpenAI 下載固定版本 Codex 0.161.0（約 98 MB），核對雜湊、OpenAI Developer ID 與版本，再複製到 ~/.local/bin/codex。不會覆蓋既有檔案；登入需另行完成。")
        alert.addButton(withTitle: Self.t("Install", "安裝"))
        alert.addButton(withTitle: Self.t("Cancel", "取消"))
        guard alert.runModal() == .alertFirstButtonReturn else { return }
        busy = true
        status.stringValue = Self.t("Downloading and verifying Codex…", "正在下載並核對 Codex……")
        refresh(status.stringValue)
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            let result: Result<Void, Error>
            do { try CodexInstaller.install(); result = .success(()) }
            catch { result = .failure(error) }
            DispatchQueue.main.async {
                guard let self else { return }
                self.busy = false
                switch result {
                case .success:
                    self.output.stringValue = Self.t("Codex installed. Sign in, then retry the Session.",
                                                    "Codex 已安裝。請先登入，再重試開始 Session。")
                    self.refresh()
                    self.onChange?()
                case .failure(let error):
                    self.output.stringValue = error.localizedDescription
                    self.refresh(Self.t("Codex installation did not finish. Read the reason below and try again.",
                                        "Codex 安裝未完成。請閱讀下方原因後重試。"))
                }
            }
        }
    }

    @objc private func signInCodex() {
        guard let codex, !busy else { return }
        let alert = NSAlert()
        alert.messageText = Self.t("Sign in to Codex?", "要登入 Codex 嗎？")
        alert.informativeText = Self.t(
            "Codex will show a one-time code here and open OpenAI's sign-in page. Complete sign-in with your own account in the browser. You can cancel without removing Codex.",
            "Codex 會在此顯示一次性代碼並開啟 OpenAI 登入頁。請在瀏覽器用自己的帳號完成登入；取消不會移除 Codex。")
        alert.addButton(withTitle: Self.t("Continue", "繼續"))
        alert.addButton(withTitle: Self.t("Cancel", "取消"))
        guard alert.runModal() == .alertFirstButtonReturn else { return }
        let task = Process()
        task.executableURL = codex
        task.arguments = ["login", "--device-auth"]
        let pipe = Pipe()
        task.standardOutput = pipe
        task.standardError = pipe
        do { try task.run() }
        catch { output.stringValue = error.localizedDescription; return }
        loginTask = task
        loginCancelled = false
        openedLoginPage = false
        busy = true
        output.stringValue = ""
        refresh(Self.t("Wait for the one-time code, then finish in your browser.",
                       "請等候一次性代碼，再到瀏覽器完成登入。"))
        pipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            if data.isEmpty { handle.readabilityHandler = nil; return }
            let chunk = String(data: data, encoding: .utf8) ?? ""
            DispatchQueue.main.async {
                guard let self else { return }
                self.output.stringValue += chunk.replacingOccurrences(of: "\u{001B}\\[[0-9;]*m", with: "", options: .regularExpression)
                if !self.openedLoginPage && self.output.stringValue.contains("https://auth.openai.com/codex/device") {
                    self.openedLoginPage = true
                    NSWorkspace.shared.open(URL(string: "https://auth.openai.com/codex/device")!)
                }
            }
        }
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            task.waitUntilExit()
            DispatchQueue.main.async {
                guard let self else { return }
                self.loginTask = nil
                self.busy = false
                let signedIn = task.terminationReason == .exit && task.terminationStatus == 0
                    && CodexInstaller.isSignedIn(codex)
                self.refresh(self.loginCancelled ? Self.t("Sign-in cancelled. You can try again.", "登入已取消，可再次嘗試。")
                    : signedIn ? Self.t("Codex sign-in verified. Return to the Session picker and retry.",
                                        "已確認 Codex 登入成功。請回到 Session 選擇畫面重試。")
                    : Self.t("Codex sign-in was not verified. Check the code and try again.",
                             "無法確認 Codex 已登入。請檢查代碼後重試。"))
                if signedIn { self.onChange?() }
            }
        }
    }

    @objc private func cancelSignIn() {
        guard let task = loginTask, task.isRunning else { return }
        loginCancelled = true
        task.terminate()
    }

    func windowWillClose(_ notification: Notification) { cancelSignIn() }
}
