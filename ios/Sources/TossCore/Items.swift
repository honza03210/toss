import Foundation

/// Something to send to the computer.
public enum OutgoingItem: Equatable {
    case text(String)
    case link(URL)
    /// A file on disk (the extension's or app's temporary copy).
    case file(URL, name: String, mime: String)

    public var kind: String {
        switch self {
        case .text: return "text"
        case .link: return "url"
        case .file: return "file"
        }
    }

    /// A short label for lists.
    public var label: String {
        switch self {
        case let .text(t): return t.count > 60 ? String(t.prefix(59)) + "…" : t
        case let .link(u): return u.absoluteString
        case let .file(_, name, _): return name
        }
    }
}

public enum ItemRequest {
    /// Headers for one item of a batch (PROTOCOL.md, `POST /v1/items`).
    public static func headers(for item: OutgoingItem, deviceKey: String, batch: String, index: Int, count: Int) -> [String: String] {
        var h = [
            "Authorization": "Bearer \(deviceKey)",
            "X-Toss-Kind": item.kind,
            "X-Toss-Batch": batch,
            "X-Toss-Index": String(index),
            "X-Toss-Count": String(count),
        ]
        switch item {
        case .text, .link:
            h["Content-Type"] = "text/plain; charset=utf-8"
        case let .file(_, name, mime):
            h["Content-Type"] = mime
            h["X-Toss-Name"] = percentEncode(name)
        }
        return h
    }

    public static func body(for item: OutgoingItem) -> Data? {
        switch item {
        case let .text(t): return Data(t.utf8)
        case let .link(u): return Data(u.absoluteString.utf8)
        case .file: return nil
        }
    }

    /// Percent-encodes everything except unreserved characters (RFC 3986), as Go's PathUnescape expects.
    public static func percentEncode(_ s: String) -> String {
        var allowed = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
        allowed.insert(charactersIn: "-._~")
        return s.addingPercentEncoding(withAllowedCharacters: allowed) ?? s
    }
}

/// Removes duplicates the share sheet often hands over together: e.g. Safari's page link plus
/// the same link as text.
public func dedupe(_ items: [OutgoingItem]) -> [OutgoingItem] {
    let links = Set(items.compactMap { item -> String? in
        if case let .link(u) = item { return u.absoluteString }
        return nil
    })
    var seen = Set<String>()
    return items.filter { item in
        if case let .text(t) = item {
            let trimmed = t.trimmingCharacters(in: .whitespacesAndNewlines)
            if trimmed.isEmpty || links.contains(trimmed) { return false }
        }
        let key: String
        switch item {
        case let .text(t): key = "t:" + t
        case let .link(u): key = "u:" + u.absoluteString
        case let .file(url, _, _): key = "f:" + url.path
        }
        return seen.insert(key).inserted
    }
}

/// A MIME type from a file name's extension (falls back to application/octet-stream).
public func mimeType(forFileName name: String) -> String {
    let types: [String: String] = [
        "jpg": "image/jpeg", "jpeg": "image/jpeg", "png": "image/png", "gif": "image/gif", "webp": "image/webp",
        "heic": "image/heic", "heif": "image/heif", "tif": "image/tiff", "tiff": "image/tiff", "dng": "image/x-adobe-dng",
        "mp4": "video/mp4", "mov": "video/quicktime", "m4v": "video/mp4",
        "mp3": "audio/mpeg", "m4a": "audio/mp4", "wav": "audio/wav", "aac": "audio/aac",
        "pdf": "application/pdf", "zip": "application/zip", "json": "application/json",
        "txt": "text/plain", "md": "text/markdown", "csv": "text/csv", "html": "text/html", "vcf": "text/vcard",
        "doc": "application/msword", "docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
        "xls": "application/vnd.ms-excel", "xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
        "ppt": "application/vnd.ms-powerpoint", "pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
        "key": "application/vnd.apple.keynote", "pages": "application/vnd.apple.pages", "numbers": "application/vnd.apple.numbers",
    ]
    return types[(name as NSString).pathExtension.lowercased()] ?? "application/octet-stream"
}

/// What the computer said went wrong, or a description of the status code.
public func serverMessage(status: Int, body: Data) -> String {
    if let json = try? JSONSerialization.jsonObject(with: body) as? [String: Any], let error = json["error"] as? String {
        return error
    }
    switch status {
    case 401: return "This computer doesn't know this phone anymore. Pair again."
    case 403: return "The pairing code expired or was already used. Run `toss pair` again."
    case 413: return "That's too large to send."
    default: return "The computer answered with an error (\(status))."
    }
}
