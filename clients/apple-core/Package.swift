// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "BeiShanClientCore",
    platforms: [
        .iOS(.v17),
        .macOS(.v14),
    ],
    products: [
        .library(name: "BeiShanClientCore", targets: ["BeiShanClientCore"]),
    ],
    targets: [
        .target(
            name: "BeiShanClientCore",
            path: "Sources/BeiShanClientCore"
        ),
        .testTarget(
            name: "BeiShanClientCoreTests",
            dependencies: ["BeiShanClientCore"],
            path: "Tests/BeiShanClientCoreTests"
        ),
    ]
)
