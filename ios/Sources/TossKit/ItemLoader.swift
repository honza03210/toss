import Foundation
import TossCore
import UniformTypeIdentifiers

/// Turns what the share sheet hands over (item providers) into items to send.
public enum ItemLoader {
    public static func load(_ providers: [NSItemProvider], into folder: URL, convertHEIC: Bool) async -> (items: [OutgoingItem], failures: Int) {
        var items: [OutgoingItem] = []
        var failures = 0
        for provider in providers {
            if let item = try? await load(provider, into: folder, convertHEIC: convertHEIC) {
                items.append(item)
            } else {
                failures += 1
            }
        }
        return (dedupe(items), failures)
    }

    static func load(_ provider: NSItemProvider, into folder: URL, convertHEIC: Bool) async throws -> OutgoingItem? {
        let types = provider.registeredTypeIdentifiers.compactMap(UTType.init)
        // A web link (not a file URL): send as a link.
        if provider.hasItemConformingToTypeIdentifier(UTType.url.identifier),
           !provider.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier),
           !types.contains(where: { $0.conforms(to: .image) || $0.conforms(to: .movie) }) {
            if let url = try await provider.loadItem(forTypeIdentifier: UTType.url.identifier) as? URL, !url.isFileURL {
                return .link(url)
            }
        }
        // Plain text (notes, selected text): only when nothing file-like is offered.
        if types.allSatisfy({ $0.conforms(to: .text) && !$0.conforms(to: .fileURL) }),
           provider.hasItemConformingToTypeIdentifier(UTType.plainText.identifier) {
            let value = try await provider.loadItem(forTypeIdentifier: UTType.plainText.identifier)
            if let text = value as? String { return .text(text) }
            if let data = value as? Data, let text = String(data: data, encoding: .utf8) { return .text(text) }
            if let url = value as? URL, let text = try? String(contentsOf: url, encoding: .utf8) { return .text(text) }
        }
        // Anything else is a file. Prefer the most specific type: the original photo/video/document.
        let preferred = types.first { $0.conforms(to: .image) || $0.conforms(to: .audiovisualContent) }
            ?? types.first { $0.conforms(to: .data) && $0 != .data && !$0.conforms(to: .plainText) }
            ?? types.first { $0.conforms(to: .data) }
            ?? types.first { $0.conforms(to: .fileURL) }
        guard let type = preferred else { return nil }
        let suggestedName = provider.suggestedName
        return try await withCheckedThrowingContinuation { continuation in
            _ = provider.loadFileRepresentation(forTypeIdentifier: type.identifier) { url, error in
                guard let url else {
                    return continuation.resume(throwing: error ?? CocoaError(.fileReadUnknown))
                }
                // The file only exists during this callback: copy it out now.
                do {
                    let name = suggestedName.map { suggested in
                        (suggested as NSString).pathExtension.isEmpty ? suggested + "." + (type.preferredFilenameExtension ?? url.pathExtension) : suggested
                    } ?? url.lastPathComponent
                    continuation.resume(returning: try Sender.prepareFile(url, suggestedName: name, into: folder, convertHEIC: convertHEIC))
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }
}
