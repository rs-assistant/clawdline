import Foundation

/// Mirror of the local browser's *displayed* catalog, not the daemon's
/// product_language or the language used to author agent responses.
enum ShellLanguage {
    static let tags = ["en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"]
    private static let savedKey = "clawdline.nativeShellCatalog"
    private static var selected = initialTag()
    private static var catalog: [String: [String: String]] = {
        guard let url = Bundle.main.url(forResource: "shell-copy", withExtension: "json"),
              let data = try? Data(contentsOf: url),
              let words = try? JSONDecoder().decode([String: [String: String]].self, from: data)
        else { return [:] }
        return words
    }()

    static var tag: String { selected }

    /// The catalog runtime sends only a canonical tag after validation. Reject
    /// unknown values so an untrusted page cannot select arbitrary copy.
    @discardableResult
    static func select(_ tag: String) -> Bool {
        guard tags.contains(tag), selected != tag else { return false }
        selected = tag
        UserDefaults.standard.set(tag, forKey: savedKey)
        return true
    }

    static func resolve(_ preference: String, languages: [String]) -> String {
        if preference != "auto" && !preference.isEmpty { return resolveTag(preference) ?? "en" }
        for language in languages {
            if let tag = resolveTag(language) { return tag }
        }
        return "en"
    }

    static func resolveTag(_ raw: String) -> String? {
        let tag = raw.trimmingCharacters(in: .whitespacesAndNewlines)
            .replacingOccurrences(of: "_", with: "-").lowercased()
        let parts = tag.split(separator: "-").map(String.init)
        guard let base = parts.first, base.range(of: "^[a-z]{2,3}$", options: .regularExpression) != nil,
              parts.dropFirst().allSatisfy({ $0.range(of: "^[a-z0-9]{2,8}$", options: .regularExpression) != nil })
        else { return nil }
        if base == "zh" {
            if parts.count > 1, parts[1].count == 4, parts[1] != "hant", parts[1] != "hans" { return nil }
            if parts.contains("hant") { return "zh-Hant" }
            if parts.contains("hans") { return "zh-Hans" }
            if parts.contains(where: { ["tw", "hk", "mo"].contains($0) }) { return "zh-Hant" }
            if parts.contains(where: { ["cn", "sg"].contains($0) }) { return "zh-Hans" }
            return nil
        }
        if base == "pt" { return "pt-BR" }
        return tags.contains(base) ? base : nil
    }

    static func word(_ key: String, _ values: [String] = [], language: String? = nil) -> String {
        let source = catalog[language ?? selected]?[key] ?? catalog["en"]?[key] ?? key
        let regex = try! NSRegularExpression(pattern: "\\{arg([0-9]+)\\}")
        var result = source
        let range = NSRange(source.startIndex..<source.endIndex, in: source)
        for match in regex.matches(in: source, range: range).reversed() {
            guard let indexRange = Range(match.range(at: 1), in: source),
                  let index = Int(source[indexRange]), index < values.count,
                  let sourceRange = Range(match.range, in: result) else { continue }
            result.replaceSubrange(sourceRange, with: values[index])
        }
        return result
    }

    static func htmlText(_ value: String) -> String {
        value.replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
            .replacingOccurrences(of: "\"", with: "&quot;")
            .replacingOccurrences(of: "'", with: "&#39;")
    }

    static func loadCatalog(_ data: Data) throws {
        catalog = try JSONDecoder().decode([String: [String: String]].self, from: data)
    }

    private static func initialTag() -> String {
        if let saved = UserDefaults.standard.string(forKey: savedKey), tags.contains(saved) { return saved }
        return resolve("auto", languages: Locale.preferredLanguages)
    }
}
