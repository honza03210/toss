import SwiftUI
import TossCore

/// The sending sheet, used by the share extension (and the app's own Send buttons):
/// picks the computer, loads the items, sends them right away and reports progress.
@MainActor
public final class SendModel: ObservableObject {
    public enum Phase: Equatable {
        case loading
        case noPairing
        case sending
        case done
        case failed(String)
    }

    @Published public var phase: Phase = .loading
    @Published public var progress = 0.0
    @Published public var items: [OutgoingItem] = []
    @Published public var pairings: [Pairing] = []
    @Published public var selected: String?
    @Published public var skipped = 0

    private var folder: URL?
    private let loadItems: (URL, Bool) async -> (items: [OutgoingItem], failures: Int)
    public var onFinish: (Bool) -> Void = { _ in }

    /// `loadItems` gets a temporary folder to copy files into and whether to convert HEIC.
    public init(loadItems: @escaping (URL, Bool) async -> (items: [OutgoingItem], failures: Int)) {
        self.loadItems = loadItems
    }

    public var target: Pairing? { pairings.first { $0.computerID == selected } }

    public func start() async {
        let state = KeychainStore.load()
        pairings = state.pairingsByUse
        selected = pairings.first?.computerID
        guard !pairings.isEmpty else {
            phase = .noPairing
            return
        }
        let folder = Sender.temporaryFolder()
        self.folder = folder
        let loaded = await loadItems(folder, state.convertHEIC)
        items = loaded.items
        skipped = loaded.failures
        guard !items.isEmpty else {
            phase = .failed("There's nothing here Toss can send.")
            return
        }
        await send()
    }

    public func send() async {
        guard let target else { return }
        phase = .sending
        progress = 0
        do {
            try await Sender.send(items, to: target) { value in
                Task { @MainActor in self.progress = value }
            }
            phase = .done
            try? await Task.sleep(nanoseconds: 700_000_000)
            finish(success: true)
        } catch {
            phase = .failed(error.localizedDescription)
        }
    }

    public func finish(success: Bool) {
        if let folder { Sender.cleanUp(folder) }
        folder = nil
        onFinish(success)
    }
}

public struct SendView: View {
    @ObservedObject var model: SendModel

    public init(model: SendModel) {
        self.model = model
    }

    public var body: some View {
        NavigationStack {
            VStack(spacing: 18) {
                header
                if !model.items.isEmpty {
                    itemList
                }
                status
                Spacer(minLength: 0)
            }
            .padding()
            .navigationTitle("Toss")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(model.phase == .done ? "Done" : "Cancel") { model.finish(success: model.phase == .done) }
                }
            }
        }
        .task { await model.start() }
    }

    @ViewBuilder private var header: some View {
        if model.pairings.count > 1 {
            Picker("Send to", selection: Binding(get: { model.selected ?? "" }, set: { model.selected = $0 })) {
                ForEach(model.pairings) { Text($0.computerName).tag($0.computerID) }
            }
            .pickerStyle(.segmented)
            .disabled(model.phase == .sending)
        } else if let target = model.target {
            Label(target.computerName, systemImage: "laptopcomputer")
                .font(.headline)
        }
    }

    private var itemList: some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(Array(model.items.prefix(5).enumerated()), id: \.offset) { _, item in
                Label(item.label, systemImage: icon(item)).lineLimit(1).font(.callout)
            }
            if model.items.count > 5 {
                Text("and \(model.items.count - 5) more").font(.footnote).foregroundStyle(.secondary)
            }
            if model.skipped > 0 {
                Text("\(model.skipped) item\(model.skipped == 1 ? "" : "s") couldn't be read and will be skipped.")
                    .font(.footnote).foregroundStyle(.orange)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(12)
        .background(Color.secondary.opacity(0.1), in: RoundedRectangle(cornerRadius: 12))
    }

    @ViewBuilder private var status: some View {
        switch model.phase {
        case .loading:
            ProgressView("Preparing…")
        case .noPairing:
            VStack(spacing: 10) {
                Image(systemName: "qrcode.viewfinder").font(.largeTitle).foregroundStyle(.secondary)
                Text("No computer paired yet").font(.headline)
                Text("Open the Toss app and pair a computer first: run `toss pair` on the computer and scan the code.")
                    .font(.callout).multilineTextAlignment(.center).foregroundStyle(.secondary)
            }
        case .sending:
            VStack(spacing: 8) {
                ProgressView(value: model.progress)
                Text(model.progress < 0.01 ? "Connecting…" : "Sending… \(Int(model.progress * 100))%")
                    .font(.footnote).foregroundStyle(.secondary)
            }
        case .done:
            Label("Sent", systemImage: "checkmark.circle.fill").font(.title3.bold()).foregroundStyle(.green)
        case let .failed(message):
            VStack(spacing: 12) {
                Label("Not sent", systemImage: "exclamationmark.triangle.fill").font(.headline).foregroundStyle(.orange)
                Text(message).font(.callout).multilineTextAlignment(.center)
                if !model.items.isEmpty {
                    Button("Try again") { Task { await model.send() } }.buttonStyle(.borderedProminent)
                }
            }
        }
    }

    private func icon(_ item: OutgoingItem) -> String {
        switch item {
        case .text: return "text.alignleft"
        case .link: return "link"
        case let .file(_, _, mime):
            if mime.hasPrefix("image/") { return "photo" }
            if mime.hasPrefix("video/") { return "film" }
            return "doc"
        }
    }
}
