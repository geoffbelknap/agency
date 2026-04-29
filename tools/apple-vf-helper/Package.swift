// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "agency-apple-vf-helper",
    platforms: [
        .macOS(.v13)
    ],
    products: [
        .executable(name: "agency-apple-vf-helper", targets: ["agency-apple-vf-helper"])
    ],
    targets: [
        .executableTarget(name: "agency-apple-vf-helper")
    ]
)
