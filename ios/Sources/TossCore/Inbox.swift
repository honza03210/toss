import Foundation

/// Something the computer sent that waits for this phone (PROTOCOL.md, `GET /v1/inbox`).
public struct InboxItem: Codable, Equatable, Identifiable {
    public var id: String
    public var kind: String // text, url, file
    public var text: String?
    public var name: String?
    public var size: Int64
    public var mime: String?
    /// RFC 3339 with fractional seconds, as Go writes it.
    public var created: String

    public init(id: String, kind: String, text: String? = nil, name: String? = nil, size: Int64 = 0, mime: String? = nil, created: String = "") {
        self.id = id
        self.kind = kind
        self.text = text
        self.name = name
        self.size = size
        self.mime = mime
        self.created = created
    }

    public var createdDate: Date? { parseRFC3339(created) }

    public static func decodeList(_ data: Data) throws -> [InboxItem] {
        struct List: Decodable { var items: [InboxItem] }
        return try JSONDecoder().decode(List.self, from: data).items
    }
}

/// Parses RFC 3339 dates with or without fractional seconds (Go writes nanoseconds).
public func parseRFC3339(_ s: String) -> Date? {
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    // ISO8601DateFormatter takes at most millisecond-ish precision reliably: cut nanoseconds.
    var trimmed = s
    if let dot = s.firstIndex(of: "."), let end = s[dot...].firstIndex(where: { $0 == "Z" || $0 == "+" || $0 == "-" }) {
        let digits = s[s.index(after: dot)..<end]
        trimmed = String(s[..<dot]) + "." + String(digits.prefix(3)).padding(toLength: 3, withPad: "0", startingAt: 0) + String(s[end...])
    }
    if let d = f.date(from: trimmed) { return d }
    f.formatOptions = [.withInternetDateTime]
    return f.date(from: s)
}

/// RFC 3339 for X-Toss-Modified.
public func formatRFC3339(_ date: Date) -> String {
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime]
    f.timeZone = TimeZone.current
    return f.string(from: date)
}

/// A file name that is safe inside one folder, and a free variant of it ("a (2).jpg").
public func safeLocalName(_ name: String) -> String {
    var out = name.replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: ":", with: "_")
    out = out.trimmingCharacters(in: .whitespacesAndNewlines)
    while out.hasPrefix(".") { out.removeFirst() }
    return out.isEmpty ? "file" : out
}

public func uniqueName(_ name: String, attempt: Int) -> String {
    guard attempt > 1 else { return name }
    let ext = (name as NSString).pathExtension
    let base = (name as NSString).deletingPathExtension
    return ext.isEmpty ? "\(base) (\(attempt))" : "\(base) (\(attempt)).\(ext)"
}

/// The EXIF date format ("2024:07:14 18:30:05") with an optional offset ("+02:00").
public func parseEXIFDate(_ value: String, offset: String?, defaultZone: TimeZone = .current) -> Date? {
    let f = DateFormatter()
    f.locale = Locale(identifier: "en_US_POSIX")
    f.dateFormat = "yyyy:MM:dd HH:mm:ss"
    f.timeZone = defaultZone
    if let offset, let zone = timeZone(fromOffset: offset) {
        f.timeZone = zone
    }
    return f.date(from: value.trimmingCharacters(in: .whitespaces))
}

func timeZone(fromOffset offset: String) -> TimeZone? {
    let parts = offset.trimmingCharacters(in: .whitespaces)
    guard parts.count == 6, let sign = parts.first, sign == "+" || sign == "-" else { return nil }
    let comps = parts.dropFirst().split(separator: ":")
    guard comps.count == 2, let h = Int(comps[0]), let m = Int(comps[1]) else { return nil }
    let seconds = (h * 3600 + m * 60) * (sign == "-" ? -1 : 1)
    return TimeZone(secondsFromGMT: seconds)
}
