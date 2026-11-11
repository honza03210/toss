import SwiftUI

@main
struct TossApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            ContentView(model: model)
                // The Camera app opens toss://pair links from the computer's QR code.
                .onOpenURL { model.handle(url: $0) }
        }
    }
}
