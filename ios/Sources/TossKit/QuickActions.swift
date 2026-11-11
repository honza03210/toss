import Foundation
import TossCore
import UIKit

/// The one-step actions behind Shortcuts, Back Tap and the toss:// links: send the clipboard,
/// send given text or files, fetch the computer's clipboard. They go to the most recently
/// used computer unless one is named.
public enum QuickActions {
    public enum ActionError: LocalizedError {
        case notPaired
        case unknownComputer(String)
        case emptyClipboard

        public var errorDescription: String? {
            switch self {
            case .notPaired: return "No computer is paired yet. Open Toss and pair one first."
            case let .unknownComputer(name): return "No paired computer is called \(name)."
            case .emptyClipboard: return "The clipboard is empty."
            }
        }
    }

    /// The computer to use: the named one, else the most recently used.
    public static func pairing(named name: String? = nil) throws -> Pairing {
        let pairings = KeychainStore.load().pairingsByUse
        guard !pairings.isEmpty else { throw ActionError.notPaired }
        guard let name, !name.isEmpty else { return pairings[0] }
        guard let match = pairings.first(where: { $0.computerName == name || $0.computerID == name }) else {
            throw ActionError.unknownComputer(name)
        }
        return match
    }

    /// Sends whatever the iPhone clipboard holds: a link, text or an image. Returns the computer's name.
    @MainActor
    @discardableResult
    public static func sendClipboard(to name: String? = nil) async throws -> String {
        let pairing = try pairing(named: name)
        let folder = Sender.temporaryFolder()
        defer { Sender.cleanUp(folder) }
        guard let item = try clipboardItem(into: folder) else { throw ActionError.emptyClipboard }
        try await Sender.send([item], to: pairing) { _ in }
        return pairing.computerName
    }

    /// Sends text (as a link if it's a single web address). Returns the computer's name.
    @discardableResult
    public static func send(text: String, to name: String? = nil) async throws -> String {
        let pairing = try pairing(named: name)
        try await Sender.send([item(forText: text)], to: pairing) { _ in }
        return pairing.computerName
    }

    /// Sends files given as (data, file name). Returns the computer's name.
    @discardableResult
    public static func send(files: [(Data, String)], to name: String? = nil) async throws -> String {
        let pairing = try pairing(named: name)
        let folder = Sender.temporaryFolder()
        defer { Sender.cleanUp(folder) }
        let convertHEIC = KeychainStore.load().convertHEIC
        var items: [OutgoingItem] = []
        for (data, name) in files {
            let source = folder.appendingPathComponent(UUID().uuidString + "-" + name.replacingOccurrences(of: "/", with: "_"))
            try data.write(to: source)
            items.append(try Sender.prepareFile(source, suggestedName: name, into: folder, convertHEIC: convertHEIC))
        }
        try await Sender.send(items, to: pairing) { _ in }
        return pairing.computerName
    }

    /// The computer's clipboard text and the computer's name.
    public static func computerClipboard(from name: String? = nil) async throws -> (text: String, computer: String) {
        let pairing = try pairing(named: name)
        let client = TossClient(fingerprint: pairing.fingerprint)
        let host = try await client.reachableHost(name: pairing.computerName, hosts: pairing.hostsToTry,
                                                  port: pairing.port, deviceKey: pairing.deviceKey)
        let text = try await client.clipboard(host: host, pairing: pairing)
        return (text, pairing.computerName)
    }

    public static func item(forText text: String) -> OutgoingItem {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.contains(where: \.isWhitespace), let url = URL(string: trimmed),
           let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https", url.host != nil {
            return .link(url)
        }
        return .text(text)
    }

    /// What's on the clipboard, as an item to send (images are written into `folder`).
    @MainActor
    static func clipboardItem(into folder: URL) throws -> OutgoingItem? {
        let board = UIPasteboard.general
        if let url = board.url, !url.isFileURL { return .link(url) }
        if board.hasStrings, let text = board.string, !text.isEmpty { return item(forText: text) }
        if board.hasImages {
            let stamp = ISO8601DateFormatter().string(from: Date()).replacingOccurrences(of: ":", with: "-")
            for (type, ext, mime) in [("public.png", "png", "image/png"), ("public.jpeg", "jpg", "image/jpeg")] {
                if let data = board.data(forPasteboardType: type) {
                    let url = folder.appendingPathComponent("Clipboard \(stamp).\(ext)")
                    try data.write(to: url)
                    return .file(url, name: url.lastPathComponent, mime: mime)
                }
            }
            if let data = board.image?.pngData() {
                let url = folder.appendingPathComponent("Clipboard \(stamp).png")
                try data.write(to: url)
                return .file(url, name: url.lastPathComponent, mime: "image/png")
            }
        }
        return nil
    }
}
