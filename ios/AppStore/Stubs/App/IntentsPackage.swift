import AppIntents
import Toss

// The intents live in the Toss Swift package; this tells Xcode's App Intents metadata step to
// include them in the app (Shortcuts discovers them through that metadata).
struct TossAppIntents: AppIntentsPackage {
    static var includedPackages: [any AppIntentsPackage.Type] { [TossIntentsPackage.self] }
}
