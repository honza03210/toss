import Photos
import QuickLook
import SwiftUI
import TossKit

/// "From your computers": what `toss send`, the file manager's "Send to iPhone" and the
/// computer's Toss page sent. Files live in Documents, so the Files app shows them too.
struct ReceivedSection: View {
    @ObservedObject var model: AppModel
    @State private var preview: URL?

    var body: some View {
        Section {
            if model.received.isEmpty {
                Text(model.fetchingInbox ? "Checking…" : "Nothing yet. On the computer, run `toss send <file>` or right-click a file → Send to iPhone.")
                    .font(.callout).foregroundStyle(.secondary)
            }
            ForEach(model.received.prefix(model.showAllReceived ? 100 : 5)) { item in
                ReceivedRow(item: item, model: model, preview: $preview)
                    .swipeActions {
                        Button("Delete", role: .destructive) { model.deleteReceived(item) }
                    }
            }
            if model.received.count > 5 && !model.showAllReceived {
                Button("Show all \(model.received.count)") { model.showAllReceived = true }
            }
        } header: {
            HStack {
                Text("From your computers")
                Spacer()
                if model.fetchingInbox { ProgressView().controlSize(.mini) }
            }
        } footer: {
            if !model.received.isEmpty {
                Text("Files are also in the Files app: On My iPhone → Toss.")
            }
        }
        .quickLookPreview($preview)
    }
}

struct ReceivedRow: View {
    let item: ReceivedItem
    @ObservedObject var model: AppModel
    @Binding var preview: URL?

    var body: some View {
        HStack(spacing: 12) {
            icon.frame(width: 40, height: 40)
            VStack(alignment: .leading, spacing: 2) {
                Text(item.title).lineLimit(item.kind == "text" ? 3 : 1)
                Text("\(item.computer) · \(item.date.formatted(date: .abbreviated, time: .shortened))")
                    .font(.caption).foregroundStyle(.secondary)
            }
            Spacer(minLength: 4)
            actions
        }
        .contentShape(Rectangle())
        .onTapGesture { primary() }
    }

    @ViewBuilder private var icon: some View {
        if item.isImage, let url = item.fileURL, let image = UIImage(contentsOfFile: url.path)?.preparingThumbnail(of: CGSize(width: 120, height: 120)) {
            Image(uiImage: image).resizable().scaledToFill().clipShape(RoundedRectangle(cornerRadius: 6))
        } else {
            Image(systemName: item.kind == "url" ? "link" : item.kind == "text" ? "text.alignleft" : item.isVideo ? "film" : "doc")
                .font(.title3).foregroundStyle(.tint)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.accentColor.opacity(0.12), in: RoundedRectangle(cornerRadius: 6))
        }
    }

    @ViewBuilder private var actions: some View {
        switch item.kind {
        case "file":
            if let url = item.fileURL {
                Menu {
                    Button { preview = url } label: { Label("Open", systemImage: "eye") }
                    ShareLink(item: url) { Label("Share…", systemImage: "square.and.arrow.up") }
                    if item.isImage || item.isVideo {
                        Button { Task { await model.saveToPhotos(item) } } label: { Label("Save to Photos", systemImage: "photo.badge.plus") }
                    }
                } label: {
                    Image(systemName: "ellipsis.circle").font(.title3)
                }
            }
        default:
            Button("Copy") {
                UIPasteboard.general.string = item.text
                model.flash("Copied")
            }
            .buttonStyle(.bordered)
            .font(.caption)
        }
    }

    private func primary() {
        switch item.kind {
        case "url":
            if let text = item.text, let url = URL(string: text) { UIApplication.shared.open(url) }
        case "file":
            preview = item.fileURL
        default:
            UIPasteboard.general.string = item.text
            model.flash("Copied")
        }
    }
}

enum PhotoSaver {
    /// Adds a photo or video to the library, keeping its original data and metadata.
    static func save(_ url: URL, video: Bool) async throws {
        let status = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
        guard status == .authorized || status == .limited else {
            throw NSError(domain: "Toss", code: 1, userInfo: [NSLocalizedDescriptionKey: "Toss isn't allowed to add to Photos. Allow it in Settings → Toss → Photos."])
        }
        try await PHPhotoLibrary.shared().performChanges {
            PHAssetCreationRequest.forAsset().addResource(with: video ? .video : .photo, fileURL: url, options: nil)
        }
    }
}
