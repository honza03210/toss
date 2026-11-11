import PhotosUI
import SwiftUI
import TossCore
import TossKit
import UniformTypeIdentifiers

struct ContentView: View {
    @ObservedObject var model: AppModel
    @Environment(\.scenePhase) private var scenePhase
    @State private var scanning = false
    @State private var sendModel: SendModel?
    @State private var photoSelection: [PhotosPickerItem] = []
    @State private var importing = false

    var body: some View {
        NavigationStack {
            List {
                if model.state.pairings.isEmpty {
                    setupSection
                } else {
                    computersSection
                    ReceivedSection(model: model)
                    sendSection
                }
                settingsSection
                aboutSection
            }
            .navigationTitle("Toss")
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button {
                        scanning = true
                    } label: {
                        Label("Pair a computer", systemImage: "qrcode.viewfinder")
                    }
                }
            }
            .refreshable {
                await model.refreshOnline()
                await model.fetchInbox()
            }
        }
        .sheet(isPresented: $scanning) {
            ScannerSheet { code in
                scanning = false
                model.handle(link: code)
            }
        }
        .sheet(item: Binding(get: { model.pendingLink.map(IdentifiedLink.init) }, set: { if $0 == nil { model.pendingLink = nil } })) { _ in
            PairSheet(model: model)
        }
        .sheet(item: Binding(get: { sendModel.map(IdentifiedSend.init) }, set: { if $0 == nil { sendModel = nil } })) { wrapper in
            SendView(model: wrapper.model)
                .presentationDetents([.medium])
        }
        .fileImporter(isPresented: $importing, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            if case let .success(urls) = result { sendFiles(urls) }
        }
        .onChange(of: photoSelection) { _, selection in
            guard !selection.isEmpty else { return }
            photoSelection = []
            sendPhotos(selection)
        }
        .alert("Toss", isPresented: Binding(get: { model.message != nil }, set: { if !$0 { model.message = nil } })) {
            Button("OK") { model.message = nil }
        } message: {
            Text(model.message ?? "")
        }
        .overlay(alignment: .top) {
            if let text = model.toast ?? model.busy {
                HStack(spacing: 8) {
                    if model.toast == nil { ProgressView() } else { Image(systemName: "checkmark.circle.fill").foregroundStyle(.green) }
                    Text(text).font(.callout.bold())
                }
                .padding(.horizontal, 16).padding(.vertical, 10)
                .background(.regularMaterial, in: Capsule())
                .shadow(radius: 6)
                .padding(.top, 8)
                .transition(.move(edge: .top).combined(with: .opacity))
            }
        }
        .animation(.spring(duration: 0.3), value: model.toast ?? model.busy)
        .task(id: scenePhase) {
            if scenePhase == .active {
                model.reload()
                await model.refreshOnline()
                await model.fetchInbox()
            }
        }
    }

    // MARK: Sections

    private var setupSection: some View {
        Section {
            VStack(alignment: .leading, spacing: 10) {
                Text("Send anything from your iPhone to your computer").font(.headline)
                Text("1. Install Toss on the computer (Linux, Windows or Mac):").font(.callout)
                CommandRow(title: "Linux or Mac (Terminal)", command: Links.installLinux)
                CommandRow(title: "Windows (PowerShell)", command: Links.installWindows)
                Text("2. On the computer, run `toss pair`.").font(.callout)
                Text("3. Scan the QR code with the Camera or the button below.").font(.callout)
                Button {
                    scanning = true
                } label: {
                    Label("Scan pairing code", systemImage: "qrcode.viewfinder").frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .padding(.top, 4)
            }
            .padding(.vertical, 6)
        }
    }

    private var computersSection: some View {
        Section {
            ForEach(model.state.pairingsByUse) { p in
                HStack {
                    Circle()
                        .fill(model.online[p.computerID] == true ? Color.green : model.online[p.computerID] == false ? Color.orange : Color.secondary.opacity(0.3))
                        .frame(width: 10, height: 10)
                    VStack(alignment: .leading) {
                        Text(p.computerName).font(.headline)
                        Text(model.online[p.computerID] == false ? "Not reachable right now" : (p.lastHost ?? p.hosts.first ?? ""))
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
                .swipeActions {
                    Button("Remove", role: .destructive) { model.remove(p) }
                }
            }
        } header: {
            Text("Computers")
        } footer: {
            Text("In any app, tap Share and choose Toss. If Toss isn't in the list, scroll the app row to the end, tap More and add it.")
        }
    }

    private var sendSection: some View {
        Section("Send from here") {
            Button {
                Task { await model.sendClipboard() }
            } label: {
                Label("Send clipboard", systemImage: "doc.on.clipboard")
            }
            Button {
                Task { await model.getComputerClipboard() }
            } label: {
                Label("Get computer's clipboard", systemImage: "laptopcomputer.and.arrow.down")
            }
            PhotosPicker(selection: $photoSelection, matching: .any(of: [.images, .videos]), photoLibrary: .shared()) {
                Label("Send photos & videos", systemImage: "photo.on.rectangle")
            }
            Button {
                importing = true
            } label: {
                Label("Send files", systemImage: "folder")
            }
        }
    }

    private var settingsSection: some View {
        Section {
            NavigationLink {
                ShortcutsSetupView()
            } label: {
                Label("Shortcuts & Back Tap", systemImage: "hand.tap")
            }
            Toggle("Send HEIC photos as JPEG", isOn: Binding(get: { model.state.convertHEIC }, set: { model.setConvertHEIC($0) }))
        } header: {
            Text("Settings")
        } footer: {
            Text("Most computers can't open HEIC, the iPhone's photo format. JPEG keeps the full resolution.")
        }
    }

    private var aboutSection: some View {
        Section {
            Link(destination: Links.website) { Label("Get Toss for your computer", systemImage: "laptopcomputer.and.arrow.down") }
            Link(destination: Links.privacy) { Label("Privacy", systemImage: "hand.raised") }
        } footer: {
            Text("Everything goes straight from this iPhone to your computer over your local network, encrypted. Nothing passes through any server.")
        }
    }

    // MARK: Sending from the app

    private func startSend(_ load: @escaping (URL, Bool) async -> (items: [OutgoingItem], failures: Int)) {
        let send = SendModel(loadItems: load)
        send.onFinish = { _ in sendModel = nil }
        sendModel = send
    }

    private func sendPhotos(_ selection: [PhotosPickerItem]) {
        startSend { folder, convertHEIC in
            var items: [OutgoingItem] = []
            var failures = 0
            for item in selection {
                if let file = try? await item.loadTransferable(type: PickedFile.self),
                   let prepared = try? Sender.prepareFile(file.url, suggestedName: file.url.lastPathComponent, into: folder, convertHEIC: convertHEIC) {
                    items.append(prepared)
                    try? FileManager.default.removeItem(at: file.url.deletingLastPathComponent())
                } else {
                    failures += 1
                }
            }
            return (items, failures)
        }
    }

    private func sendFiles(_ urls: [URL]) {
        startSend { folder, convertHEIC in
            var items: [OutgoingItem] = []
            var failures = 0
            for url in urls {
                let scoped = url.startAccessingSecurityScopedResource()
                defer { if scoped { url.stopAccessingSecurityScopedResource() } }
                if let prepared = try? Sender.prepareFile(url, suggestedName: url.lastPathComponent, into: folder, convertHEIC: convertHEIC) {
                    items.append(prepared)
                } else {
                    failures += 1
                }
            }
            return (items, failures)
        }
    }
}

/// A photo or video from the photo picker, exported to a temporary file that keeps its name.
struct PickedFile: Transferable {
    let url: URL

    static var transferRepresentation: some TransferRepresentation {
        FileRepresentation(importedContentType: .movie) { try PickedFile(copying: $0.file) }
        FileRepresentation(importedContentType: .image) { try PickedFile(copying: $0.file) }
    }

    init(copying file: URL) throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        url = dir.appendingPathComponent(file.lastPathComponent)
        try FileManager.default.copyItem(at: file, to: url)
    }
}

struct IdentifiedLink: Identifiable {
    let link: PairingLink
    var id: String { link.token }
}

struct IdentifiedSend: Identifiable {
    let model: SendModel
    var id: ObjectIdentifier { ObjectIdentifier(model) }
}

struct CommandRow: View {
    let title: String
    let command: String
    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.caption).foregroundStyle(.secondary)
            HStack(alignment: .top) {
                Text(command).font(.system(.caption, design: .monospaced)).textSelection(.enabled)
                Spacer()
                Button(copied ? "Copied" : "Copy") {
                    UIPasteboard.general.string = command
                    copied = true
                }
                .font(.caption)
                .buttonStyle(.bordered)
            }
        }
        .padding(8)
        .background(Color.secondary.opacity(0.1), in: RoundedRectangle(cornerRadius: 8))
    }
}

/// Confirms pairing with the scanned computer and lets the user name this phone.
struct PairSheet: View {
    @ObservedObject var model: AppModel
    @State private var name = ""
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                if let link = model.pendingLink {
                    Section {
                        Label(link.computerName, systemImage: "laptopcomputer").font(.headline)
                        Text(link.hosts.joined(separator: ", ")).font(.caption).foregroundStyle(.secondary)
                    } header: {
                        Text("Pair with")
                    }
                }
                Section {
                    TextField("Name", text: $name)
                } header: {
                    Text("This iPhone's name")
                } footer: {
                    Text("Shown on the computer, e.g. \"Photo from \(name.isEmpty ? "iPhone" : name)\".")
                }
                if let error = model.pairingError {
                    Section { Text(error).foregroundStyle(.orange) }
                }
                Section {
                    Button {
                        Task { await model.confirmPairing(deviceName: name) }
                    } label: {
                        HStack {
                            Text(model.pairing ? "Pairing…" : "Pair")
                            if model.pairing { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(model.pairing)
                } footer: {
                    Text("If iOS asks to find devices on your local network, tap Allow: that's how Toss reaches the computer.")
                }
            }
            .navigationTitle("Pair a computer")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        model.pendingLink = nil
                        dismiss()
                    }
                }
            }
        }
        .onAppear { name = model.state.deviceName }
    }
}
