// swift-tools-version: 6.0

import PackageDescription

let package = Package(
    name: "Toss",
    platforms: [
        .iOS(.v17),
        .macOS(.v14),
    ],
    products: [
        // The app: pairing with computers, and sending from inside the app.
        .library(
            name: "Toss",
            targets: ["Toss"]
        ),
        // The share extension: "Toss" in every app's share sheet.
        .library(
            name: "TossShare",
            targets: ["TossShare"]
        ),
    ],
    targets: [
        .target(
            name: "Toss",
            dependencies: ["TossKit"]
        ),
        .target(
            name: "TossShare",
            dependencies: ["TossKit"],
            // iOS finds ShareViewController by name at runtime and nothing references it at link
            // time, so without this the release build's dead stripping removes all of our code.
            linkerSettings: [
                .unsafeFlags(["-Xlinker", "-u", "-Xlinker", "_OBJC_CLASS_$_ShareViewController"]),
            ]
        ),
        // Shared by the app and the extension: keychain storage, the network client, sharing UI.
        .target(
            name: "TossKit",
            dependencies: ["TossCore"]
        ),
        // Platform-independent protocol code, testable on Linux.
        .target(
            name: "TossCore"
        ),
        .testTarget(
            name: "TossCoreTests",
            dependencies: ["TossCore"]
        ),
    ],
    swiftLanguageModes: [.v5]
)
