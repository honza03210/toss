import AppIntents
import Foundation
import TossCore
import TossKit
import UIKit

// Shortcuts actions. They run in the background (the app doesn't open) and go to the most
// recently used computer unless one is picked. The App Shortcuts at the bottom appear in the
// Shortcuts app by themselves, ready for Back Tap or Siri.
//
// Shortcuts finds these through Metadata.appintents, which Xcode generates at build time:
// the App Store/TestFlight build has it, xtool builds on Linux don't (there, use the toss://
// links in the app's "Shortcuts & Back Tap" screen instead).

struct ComputerEntity: AppEntity {
    static var typeDisplayRepresentation = TypeDisplayRepresentation(name: "Computer")
    static var defaultQuery = ComputerQuery()

    var id: String
    var name: String

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(name)")
    }
}

struct ComputerQuery: EntityQuery {
    func entities(for identifiers: [String]) async throws -> [ComputerEntity] {
        all().filter { identifiers.contains($0.id) }
    }

    func suggestedEntities() async throws -> [ComputerEntity] {
        all()
    }

    private func all() -> [ComputerEntity] {
        KeychainStore.load().pairingsByUse.map { ComputerEntity(id: $0.computerID, name: $0.computerName) }
    }
}

struct SendClipboardIntent: AppIntent {
    static var title: LocalizedStringResource = "Send Clipboard to Computer"
    static var description = IntentDescription("Sends what's on the iPhone clipboard (text, a link or an image) to your computer.")
    static var openAppWhenRun = false

    @Parameter(title: "Computer", description: "Leave empty for the computer you used last.")
    var computer: ComputerEntity?

    @MainActor
    func perform() async throws -> some IntentResult & ProvidesDialog {
        let name = try await QuickActions.sendClipboard(to: computer?.id)
        return .result(dialog: "Sent to \(name).")
    }
}

struct SendTextIntent: AppIntent {
    static var title: LocalizedStringResource = "Send Text to Computer"
    static var description = IntentDescription("Sends text or a link to your computer. A link opens in the computer's browser.")
    static var openAppWhenRun = false

    @Parameter(title: "Text")
    var text: String

    @Parameter(title: "Computer", description: "Leave empty for the computer you used last.")
    var computer: ComputerEntity?

    static var parameterSummary: some ParameterSummary {
        Summary("Send \(\.$text) to \(\.$computer)")
    }

    func perform() async throws -> some IntentResult & ProvidesDialog {
        let name = try await QuickActions.send(text: text, to: computer?.id)
        return .result(dialog: "Sent to \(name).")
    }
}

struct SendFilesIntent: AppIntent {
    static var title: LocalizedStringResource = "Send Files to Computer"
    static var description = IntentDescription("Sends photos or files to your computer's Downloads folder.")
    static var openAppWhenRun = false

    @Parameter(title: "Files", supportedTypeIdentifiers: ["public.item"])
    var files: [IntentFile]

    @Parameter(title: "Computer", description: "Leave empty for the computer you used last.")
    var computer: ComputerEntity?

    static var parameterSummary: some ParameterSummary {
        Summary("Send \(\.$files) to \(\.$computer)")
    }

    func perform() async throws -> some IntentResult & ProvidesDialog {
        let named = files.enumerated().map { index, file in
            (file.data, file.filename.isEmpty ? "File \(index + 1)" : file.filename)
        }
        let name = try await QuickActions.send(files: named, to: computer?.id)
        return .result(dialog: "Sent \(files.count) to \(name).")
    }
}

struct GetComputerClipboardIntent: AppIntent {
    static var title: LocalizedStringResource = "Get Computer's Clipboard"
    static var description = IntentDescription("Gets the text on your computer's clipboard and puts it on the iPhone clipboard.")
    static var openAppWhenRun = false

    @Parameter(title: "Copy to iPhone Clipboard", default: true)
    var copy: Bool

    @Parameter(title: "Computer", description: "Leave empty for the computer you used last.")
    var computer: ComputerEntity?

    @MainActor
    func perform() async throws -> some IntentResult & ReturnsValue<String> & ProvidesDialog {
        let (text, name) = try await QuickActions.computerClipboard(from: computer?.id)
        if copy { UIPasteboard.general.string = text }
        let dialog: IntentDialog = text.isEmpty ? "\(name)'s clipboard is empty." : (copy ? "Copied from \(name)." : "Got the clipboard from \(name).")
        return .result(value: text, dialog: dialog)
    }
}

struct GetFilesFromComputerIntent: AppIntent {
    static var title: LocalizedStringResource = "Get Files from Computer"
    static var description = IntentDescription("Picks up what your computers sent with `toss send`, Send to iPhone or Toss's page. Files are also kept in Toss; text and links come out as text files.")
    static var openAppWhenRun = false

    @MainActor
    func perform() async throws -> some IntentResult & ReturnsValue<[IntentFile]> & ProvidesDialog {
        let result = await Inbox.fetchAll()
        if result.items.isEmpty, let error = result.errors.first {
            throw NSError(domain: "Toss", code: 1, userInfo: [NSLocalizedDescriptionKey: error])
        }
        let files: [IntentFile] = result.items.compactMap { item in
            if let url = item.fileURL {
                return IntentFile(fileURL: url, filename: url.lastPathComponent)
            }
            guard let text = item.text else { return nil }
            return IntentFile(data: Data(text.utf8), filename: item.kind == "url" ? "Link.txt" : "Text.txt", type: .plainText)
        }
        let dialog: IntentDialog = files.isEmpty ? "Nothing is waiting on your computers." : "Got \(files.count) from your computer."
        return .result(value: files, dialog: dialog)
    }
}

struct TossShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(
            intent: SendClipboardIntent(),
            phrases: [
                "Send clipboard with \(.applicationName)",
                "\(.applicationName) my clipboard",
            ],
            shortTitle: "Send Clipboard",
            systemImageName: "doc.on.clipboard"
        )
        AppShortcut(
            intent: GetComputerClipboardIntent(),
            phrases: [
                "Get computer clipboard with \(.applicationName)",
                "Paste from computer with \(.applicationName)",
            ],
            shortTitle: "Get Computer Clipboard",
            systemImageName: "laptopcomputer.and.arrow.down"
        )
        AppShortcut(
            intent: GetFilesFromComputerIntent(),
            phrases: [
                "Get files from computer with \(.applicationName)",
            ],
            shortTitle: "Get Files from Computer",
            systemImageName: "tray.and.arrow.down"
        )
    }
}

/// Lets the App Store build's app target pull these intents' metadata out of this package
/// (see AppStore/Stubs/App/IntentsPackage.swift).
public struct TossIntentsPackage: AppIntentsPackage {}
