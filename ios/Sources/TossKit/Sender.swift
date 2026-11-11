import Foundation
import ImageIO
import TossCore
import UniformTypeIdentifiers

/// Finds the computer and sends items to it, updating the stored pairing (last host and use).
public enum Sender {
    public static func send(_ items: [OutgoingItem], to pairing: Pairing,
                            progress: @escaping @Sendable (Double) -> Void) async throws {
        let client = TossClient(fingerprint: pairing.fingerprint)
        let host = try await client.reachableHost(name: pairing.computerName, hosts: pairing.hostsToTry,
                                                  port: pairing.port, deviceKey: pairing.deviceKey)
        try await client.send(items, host: host, pairing: pairing, progress: progress)
        _ = try? KeychainStore.modify { state in
            state.update(pairing.computerID) {
                $0.lastHost = host
                $0.lastUsed = Date()
            }
        }
    }

    /// Whether the computer answers right now (for the app's status dots).
    public static func isOnline(_ pairing: Pairing) async -> Bool {
        let client = TossClient(fingerprint: pairing.fingerprint)
        return (try? await client.reachableHost(name: pairing.computerName, hosts: pairing.hostsToTry,
                                                port: pairing.port, deviceKey: pairing.deviceKey, timeout: 2.5)) != nil
    }

    /// A temporary folder for copies of files being sent; removed by `cleanUp`.
    public static func temporaryFolder() -> URL {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("toss-\(UUID().uuidString)", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }

    public static func cleanUp(_ folder: URL) {
        try? FileManager.default.removeItem(at: folder)
    }

    /// Copies a file into `folder` (the original may vanish when an item provider's callback ends),
    /// converting HEIC/HEIF photos to JPEG when asked. Returns the item to send.
    public static func prepareFile(_ source: URL, suggestedName: String?, into folder: URL, convertHEIC: Bool) throws -> OutgoingItem {
        var name = (suggestedName?.isEmpty == false ? suggestedName! : source.lastPathComponent)
        if (name as NSString).pathExtension.isEmpty, !source.pathExtension.isEmpty {
            name += "." + source.pathExtension
        }
        name = name.replacingOccurrences(of: "/", with: "_")
        let ext = (name as NSString).pathExtension.lowercased()
        if convertHEIC, ext == "heic" || ext == "heif" {
            let jpegName = (name as NSString).deletingPathExtension + ".jpg"
            let destination = folder.appendingPathComponent(UUID().uuidString + ".jpg")
            if convertToJPEG(source, destination) {
                return .file(destination, name: jpegName, mime: "image/jpeg")
            }
        }
        let destination = folder.appendingPathComponent(UUID().uuidString + (ext.isEmpty ? "" : "." + ext))
        try FileManager.default.copyItem(at: source, to: destination)
        return .file(destination, name: name, mime: mimeType(forFileName: name))
    }

    /// HEIC → JPEG keeping orientation and metadata (ImageIO, without decoding to UIImage, so
    /// big photos stay within the share extension's memory limit).
    static func convertToJPEG(_ source: URL, _ destination: URL) -> Bool {
        guard let src = CGImageSourceCreateWithURL(source as CFURL, nil),
              let dst = CGImageDestinationCreateWithURL(destination as CFURL, UTType.jpeg.identifier as CFString, 1, nil) else { return false }
        let options: [CFString: Any] = [kCGImageDestinationLossyCompressionQuality: 0.9]
        CGImageDestinationAddImageFromSource(dst, src, 0, options as CFDictionary)
        return CGImageDestinationFinalize(dst)
    }
}
