// A pinned OpenAI Codex CLI release gives a new Mac an assistant without
// Homebrew, Node, Git, a terminal, or executing a downloaded shell script.
import CryptoKit
import Foundation

enum CodexInstaller {
    static let version = "0.161.0"
    static let archiveSHA256 = "bd83479f3ae21474c407ec2164fbc2a63fa763f7afbbd123fb6a69c5201cfd38"
    static let archiveURL = URL(string: "https://releases.openai.com/codex/releases/\(version)/codex-aarch64-apple-darwin.tar.gz")!
    static let member = "codex-aarch64-apple-darwin"
    static let openAITeam = "2DC432GLL2"

    static var destination: URL {
        FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".local/bin/codex")
    }

    static func isSignedIn(_ executable: URL) -> Bool {
        (try? run(executable.path, ["login", "status"])) != nil
    }

    static func install() throws {
        let work = FileManager.default.temporaryDirectory.appendingPathComponent("clawdline-codex-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: work, withIntermediateDirectories: false)
        defer { try? FileManager.default.removeItem(at: work) }
        let archive = work.appendingPathComponent("codex.tar.gz")
        try download(to: archive)
        try install(archive: archive, target: destination)
    }

    private static func download(to archive: URL) throws {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 30
        config.timeoutIntervalForResource = 180
        let session = URLSession(configuration: config)
        defer { session.invalidateAndCancel() }
        let done = DispatchSemaphore(value: 0)
        var outcome: Result<Void, Error>?
        session.downloadTask(with: archiveURL) { temporary, response, error in
            defer { done.signal() }
            if let error { outcome = .failure(error); return }
            guard (response as? HTTPURLResponse)?.statusCode == 200 else {
                let status = (response as? HTTPURLResponse)?.statusCode ?? 0
                outcome = .failure(Problem(t("OpenAI download returned HTTP \(status). Try again later.",
                                             "OpenAI 下載回傳 HTTP \(status)；請稍後重試。")))
                return
            }
            guard let temporary else {
                outcome = .failure(Problem(t("Codex did not download. Check the connection and try again.",
                                             "Codex 未下載完成；請檢查網路後重試。")))
                return
            }
            do {
                try FileManager.default.copyItem(at: temporary, to: archive)
                outcome = .success(())
            } catch { outcome = .failure(error) }
        }.resume()
        guard done.wait(timeout: .now() + 210) == .success else {
            throw Problem(t("Codex download timed out. Check the connection and try again.",
                            "下載 Codex 逾時；請檢查網路後重試。"))
        }
        try outcome!.get()
    }

    private static func digest(_ url: URL) throws -> String {
        let file = try FileHandle(forReadingFrom: url)
        defer { try? file.close() }
        var hash = SHA256()
        while let chunk = try file.read(upToCount: 1024 * 1024), !chunk.isEmpty { hash.update(data: chunk) }
        return hash.finalize().map { String(format: "%02x", $0) }.joined()
    }

    @discardableResult private static func run(_ executable: String, _ arguments: [String],
                                               environment: [String: String]? = nil) throws -> String {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: executable)
        process.arguments = arguments
        process.environment = environment
        let output = Pipe()
        process.standardOutput = output
        process.standardError = output
        try process.run()
        let message = String(data: output.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
        process.waitUntilExit()
        guard process.terminationReason == .exit && process.terminationStatus == 0 else {
            throw Problem(t("Codex verification failed: \(message)", "Codex 驗證失敗：\(message)"))
        }
        return message
    }

    // The archive-taking form exercises the full verification and rollback
    // path in an empty test home, with no network or changes to the user.
    static func install(archive: URL, target: URL) throws {
        guard try digest(archive) == archiveSHA256 else {
            throw Problem(t("The Codex download did not match OpenAI's pinned checksum. Nothing was installed.",
                            "Codex 下載檔與固定的 OpenAI 雜湊不符；未安裝任何檔案。"))
        }
        let fm = FileManager.default
        if fm.fileExists(atPath: target.path) {
            throw Problem(t("A Codex program already exists in ~/.local/bin. It was left alone.",
                            "~/.local/bin 已有 Codex 程式；未修改該檔案。"))
        }
        let work = fm.temporaryDirectory.appendingPathComponent("clawdline-codex-extract-\(UUID().uuidString)")
        try fm.createDirectory(at: work, withIntermediateDirectories: false)
        defer { try? fm.removeItem(at: work) }
        let listing = try run("/usr/bin/tar", ["-tzf", archive.path])
        guard listing.trimmingCharacters(in: .whitespacesAndNewlines) == member else {
            throw Problem(t("The Codex archive contains unexpected files. Nothing was installed.",
                            "Codex 壓縮包包含非預期檔案；未安裝任何檔案。"))
        }
        try run("/usr/bin/tar", ["-xzf", archive.path, "-C", work.path])
        let binary = work.appendingPathComponent(member)
        try run("/usr/bin/codesign", ["--verify", "--strict", binary.path])
        let signature = try run("/usr/bin/codesign", ["--display", "--verbose=4", binary.path])
        guard signature.contains("TeamIdentifier=\(openAITeam)"),
              signature.contains("Authority=Developer ID Application: OpenAI") else {
            throw Problem(t("Codex was not signed by OpenAI. Nothing was installed.",
                            "Codex 並非由 OpenAI 簽署；未安裝任何檔案。"))
        }
        let codexHome = work.appendingPathComponent("codex-home")
        try fm.createDirectory(at: codexHome, withIntermediateDirectories: false)
        var checkEnvironment = ProcessInfo.processInfo.environment
        checkEnvironment["HOME"] = work.path
        checkEnvironment["CODEX_HOME"] = codexHome.path
        let versionText = try run(binary.path, ["--version"], environment: checkEnvironment)
        guard versionText.contains("codex-cli \(version)") else {
            throw Problem(t("Codex version did not match the verified release. Nothing was installed.",
                            "Codex 版本與已驗證的發行版不符；未安裝任何檔案。"))
        }
        try fm.createDirectory(at: target.deletingLastPathComponent(), withIntermediateDirectories: true)
        let staged = target.deletingLastPathComponent().appendingPathComponent(".codex.clawdline.\(UUID().uuidString).new")
        defer { try? fm.removeItem(at: staged) }
        try fm.copyItem(at: binary, to: staged)
        let expected = try digest(binary)
        guard try digest(staged) == expected else {
            throw Problem(t("Codex changed while being copied. Nothing was installed.",
                            "複製 Codex 時內容改變；未安裝任何檔案。"))
        }
        do { try fm.linkItem(at: staged, to: target) }
        catch {
            throw Problem(t("Codex could not be installed without replacing another file: \(error.localizedDescription)",
                            "無法在不取代其他檔案的情況下安裝 Codex：\(error.localizedDescription)"))
        }
        do {
            try run(target.path, ["--version"], environment: checkEnvironment)
        } catch {
            if (try? digest(target)) == expected { try? fm.removeItem(at: target) }
            throw error
        }
    }

    private static func t(_ en: String, _ tw: String) -> String {
        let tag = Locale.preferredLanguages.first?.lowercased() ?? ""
        return tag.hasPrefix("zh-hant") || tag.hasPrefix("zh-tw") ? tw : en
    }

    struct Problem: LocalizedError {
        let message: String
        init(_ message: String) { self.message = message }
        var errorDescription: String? { message }
    }
}
