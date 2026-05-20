// swift-tools-version:5.7
import PackageDescription

let package = Package(
    name: "SwiftUIMini",
    platforms: [
        .macOS(.v13),
        .iOS(.v16),
    ],
    targets: [
        .executableTarget(
            name: "App",
            path: "Sources/App"
        ),
    ]
)
