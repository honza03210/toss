import Foundation
import TossCore

/// Something this phone got from a computer, kept in the app's list.
public struct ReceivedItem: Codable, Equatable, Identifiable {
    public var id: String
    public var kind: String // text, url, file
    public var text: String?
    /// The file, relative to the app's Documents folder (which the Files app shows as
    /// On My iPhone → Toss).
    public var file: String?
    public var computer: String
    public var date: Date

    public var fileURL: URL? {
        file.map { ReceivedStore.documents.appendingPathComponent($0) }
    }

    public var title: String {
        if let file { return (file as NSString).lastPathComponent }
        return text ?? ""
    }

    public var isImage: Bool { ["jpg", "jpeg", "png", "heic", "heif", "gif", "webp"].contains(ext) }
    public var isVideo: Bool { ["mov", "mp4", "m4v"].contains(ext) }
    private var ext: String { ((file ?? "") as NSString).pathExtension.lowercased() }
}

/// The list of received items (Application Support/received.json) and their files (Documents).
public enum ReceivedStore {
    public static var documents: URL {
        FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
    }

    static var listURL: URL {
        let dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir.appendingPathComponent("received.json")
    }

    static let limit = 100

    /// Newest first.
    public static func load() -> [ReceivedItem] {
        guard let data = try? Data(contentsOf: listURL) else { return [] }
        return (try? JSONDecoder().decode([ReceivedItem].self, from: data)) ?? []
    }

    static func save(_ items: [ReceivedItem]) {
        if let data = try? JSONEncoder().encode(Array(items.prefix(limit))) {
            try? data.write(to: listURL, options: .atomic)
        }
    }

    static func add(_ new: [ReceivedItem]) {
        save(new + load())
    }

    /// Removes an item from the list, and its file too when `deleteFile`.
    public static func remove(_ id: String, deleteFile: Bool) {
        var items = load()
        if deleteFile, let url = items.first(where: { $0.id == id })?.fileURL {
            try? FileManager.default.removeItem(at: url)
        }
        items.removeAll { $0.id == id }
        save(items)
    }

    /// Moves a downloaded file into Documents under a free version of `name`; returns the
    /// path relative to Documents.
    static func keep(_ temp: URL, name: String) throws -> String {
        let safe = safeLocalName(name)
        for attempt in 1... {
            let candidate = uniqueName(safe, attempt: attempt)
            let dest = documents.appendingPathComponent(candidate)
            if !FileManager.default.fileExists(atPath: dest.path) {
                try FileManager.default.moveItem(at: temp, to: dest)
                return candidate
            }
        }
        fatalError("unreachable")
    }
}

/// Picks up what the computers sent: downloads files into Documents, keeps text and links in
/// the list, and tells each computer to drop what arrived.
public enum Inbox {
    public struct Result {
        public var items: [ReceivedItem] = []
        public var errors: [String] = []
    }

    public static func fetchAll() async -> Result {
        var result = Result()
        for pairing in KeychainStore.load().pairings {
            do {
                result.items += try await fetch(from: pairing)
            } catch let error as TossClient.ClientError {
                // A computer that's off or elsewhere is normal; only report real refusals.
                if case .server = error { result.errors.append("\(pairing.computerName): \(error.localizedDescription)") }
            } catch {}
        }
        return result
    }

    public static func fetch(from pairing: Pairing) async throws -> [ReceivedItem] {
        let client = TossClient(fingerprint: pairing.fingerprint)
        let host = try await client.reachableHost(name: pairing.computerName, hosts: pairing.hostsToTry,
                                                  port: pairing.port, deviceKey: pairing.deviceKey, timeout: 2.5)
        let waiting = try await client.inbox(host: host, pairing: pairing)
        var got: [ReceivedItem] = []
        for item in waiting {
            var received = ReceivedItem(id: UUID().uuidString, kind: item.kind, text: item.text, file: nil,
                                        computer: pairing.computerName, date: Date())
            if item.kind == "file" {
                let temp = try await client.download(item, host: host, pairing: pairing)
                received.file = try ReceivedStore.keep(temp, name: item.name ?? "File")
            }
            // Saved first, acknowledged second: a lost acknowledgement means a duplicate, never a loss.
            ReceivedStore.add([received])
            got.append(received)
            try? await client.acknowledge(item, host: host, pairing: pairing)
        }
        return got
    }
}
