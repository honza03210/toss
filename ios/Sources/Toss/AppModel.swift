import Foundation
import SwiftUI
import TossCore
import TossKit

@MainActor
final class AppModel: ObservableObject {
    @Published private(set) var state = KeychainStore.load()
    @Published var online: [String: Bool] = [:]
    /// A scanned or opened link waiting for the user to confirm pairing.
    @Published var pendingLink: PairingLink?
    @Published var pairingError: String?
    @Published var pairing = false
    @Published var message: String?
    @Published private(set) var received = ReceivedStore.load()
    @Published var fetchingInbox = false
    @Published var showAllReceived = false

    func reload() {
        state = KeychainStore.load()
    }

    /// toss://pair?… (from the QR code), toss://send-clipboard, toss://get-clipboard and toss://get-files
    /// (one-step Shortcuts for Back Tap when the App Intents aren't available).
    func handle(url: URL) {
        switch url.host?.lowercased() {
        case "send-clipboard":
            Task { await sendClipboard() }
        case "get-clipboard":
            Task { await getComputerClipboard() }
        case "get-files":
            Task { await fetchInbox() }
        default:
            handle(link: url.absoluteString)
        }
    }

    @Published var busy: String?

    func sendClipboard() async {
        busy = "Sending clipboard…"
        defer { busy = nil }
        do {
            let name = try await QuickActions.sendClipboard()
            flash("Sent clipboard to \(name)")
        } catch {
            message = error.localizedDescription
        }
    }

    func getComputerClipboard() async {
        busy = "Getting the computer's clipboard…"
        defer { busy = nil }
        do {
            let (text, name) = try await QuickActions.computerClipboard()
            if text.isEmpty {
                message = "\(name)'s clipboard has no text."
            } else {
                UIPasteboard.general.string = text
                flash("Copied \(name)'s clipboard")
            }
        } catch {
            message = error.localizedDescription
        }
    }

    /// A short confirmation banner.
    @Published var toast: String?

    func flash(_ text: String) {
        toast = text
        UINotificationFeedbackGenerator().notificationOccurred(.success)
        Task {
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            if toast == text { toast = nil }
        }
    }

    /// Picks up what the computers sent (run whenever the app comes to the front).
    func fetchInbox() async {
        guard !fetchingInbox, !state.pairings.isEmpty else { return }
        fetchingInbox = true
        defer { fetchingInbox = false }
        let result = await Inbox.fetchAll()
        received = ReceivedStore.load()
        if !result.items.isEmpty {
            let from = Set(result.items.map(\.computer)).sorted().joined(separator: ", ")
            flash(result.items.count == 1 ? "Got \(result.items[0].title.prefix(40)) from \(from)" : "Got \(result.items.count) items from \(from)")
        }
        if let error = result.errors.first { message = error }
    }

    func deleteReceived(_ item: ReceivedItem) {
        ReceivedStore.remove(item.id, deleteFile: true)
        received = ReceivedStore.load()
    }

    func saveToPhotos(_ item: ReceivedItem) async {
        guard let url = item.fileURL else { return }
        do {
            try await PhotoSaver.save(url, video: item.isVideo)
            flash("Saved to Photos")
        } catch {
            message = error.localizedDescription
        }
    }

    func handle(link: String) {
        do {
            pendingLink = try PairingLink(string: link)
            pairingError = nil
        } catch {
            message = (error as? PairingLink.ParseError)?.description ?? error.localizedDescription
        }
    }

    func confirmPairing(deviceName: String) async {
        guard let link = pendingLink else { return }
        pairing = true
        pairingError = nil
        defer { pairing = false }
        let name = deviceName.trimmingCharacters(in: .whitespaces).isEmpty ? "iPhone" : deviceName
        do {
            let pairing = try await TossClient.pair(link: link, deviceID: state.deviceID, deviceName: name)
            state = try KeychainStore.modify {
                $0.deviceName = name
                $0.upsert(pairing)
            }
            online[pairing.computerID] = true
            pendingLink = nil
            message = "Paired with \(pairing.computerName). Share anything and pick Toss."
            UINotificationFeedbackGenerator().notificationOccurred(.success)
        } catch {
            pairingError = error.localizedDescription
        }
    }

    func remove(_ pairing: Pairing) {
        state = (try? KeychainStore.modify { $0.remove(pairing.computerID) }) ?? state
        online[pairing.computerID] = nil
    }

    func setConvertHEIC(_ on: Bool) {
        state = (try? KeychainStore.modify { $0.convertHEIC = on }) ?? state
    }

    func refreshOnline() async {
        await withTaskGroup(of: (String, Bool).self) { group in
            for p in state.pairings {
                group.addTask { (p.computerID, await Sender.isOnline(p)) }
            }
            for await (id, up) in group {
                online[id] = up
            }
        }
        reload() // lastHost may have changed
    }
}
