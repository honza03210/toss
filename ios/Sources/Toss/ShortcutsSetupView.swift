import SwiftUI

/// How to put Toss on Back Tap (or the Lock Screen, Siri…). With the App Store/TestFlight build,
/// Toss's actions are in the Shortcuts app already; developer builds made with xtool lack the
/// App Intents metadata, so there a one-step "Open URL" shortcut does the same.
struct ShortcutsSetupView: View {
    /// Xcode puts Metadata.appintents in the app when it extracts the App Intents.
    private let hasIntents = Bundle.main.url(forResource: "Metadata", withExtension: "appintents") != nil

    var body: some View {
        List {
            Section {
                Text("Double-tap the back of the iPhone to send its clipboard to the computer, or to fetch the computer's clipboard.")
            }
            if hasIntents {
                Section {
                    Step(n: 1, text: "Open the Shortcuts app and tap + to make a new shortcut.")
                    Step(n: 2, text: "Search for Toss and add Send Clipboard to Computer (or Get Computer's Clipboard). Name the shortcut, e.g. \"Toss clipboard\".")
                    Step(n: 3, text: "Settings → Accessibility → Touch → Back Tap → Double Tap (or Triple Tap) → pick that shortcut under Shortcuts.")
                    openShortcutsButton
                } header: {
                    Text("Set up Back Tap")
                } footer: {
                    Text("Toss's actions also work in your own shortcuts, with Siri (\"Send clipboard with Toss\") and on the Lock Screen. The first time, iOS asks whether Toss may paste: choose Allow, or set Settings → Toss → Paste from Other Apps → Allow.")
                }
            } else {
                Section {
                    Step(n: 1, text: "Open the Shortcuts app and tap + to make a new shortcut.")
                    Step(n: 2, text: "Add the Open URLs action and enter one of these links:")
                    LinkRow(link: "toss://send-clipboard", note: "send the iPhone clipboard to the computer")
                    LinkRow(link: "toss://get-clipboard", note: "copy the computer's clipboard to the iPhone")
                    LinkRow(link: "toss://get-files", note: "pick up what the computer sent (toss send)")
                    Step(n: 3, text: "Name the shortcut, e.g. \"Toss clipboard\".")
                    Step(n: 4, text: "Settings → Accessibility → Touch → Back Tap → Double Tap → pick that shortcut under Shortcuts.")
                    openShortcutsButton
                } header: {
                    Text("Set up Back Tap")
                } footer: {
                    Text("This developer build opens Toss briefly to do it. The App Store version adds Toss actions to the Shortcuts app that run without opening it. To stop iOS asking every time, set Settings → Toss → Paste from Other Apps → Allow.")
                }
            }
        }
        .navigationTitle("Shortcuts & Back Tap")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var openShortcutsButton: some View {
        Button {
            UIApplication.shared.open(URL(string: "shortcuts://create-shortcut")!)
        } label: {
            Label("Open Shortcuts", systemImage: "square.2.layers.3d")
        }
    }
}

private struct Step: View {
    let n: Int
    let text: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text("\(n)").font(.callout.bold()).frame(width: 22, height: 22).background(Circle().fill(Color.accentColor.opacity(0.2)))
            Text(text).font(.callout)
        }
    }
}

private struct LinkRow: View {
    let link: String
    let note: String
    @State private var copied = false

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(link).font(.system(.callout, design: .monospaced))
                Text(note).font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            Button(copied ? "Copied" : "Copy") {
                UIPasteboard.general.string = link
                copied = true
            }
            .buttonStyle(.bordered)
            .font(.caption)
        }
    }
}
