// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "agency-apple-container-wait-helper",
    platforms: [.macOS("15")],
    products: [
        .executable(name: "agency-apple-container-wait-helper", targets: ["agency-apple-container-wait-helper"])
    ],
    dependencies: [
        .package(url: "https://github.com/apple/container.git", exact: "0.11.0")
    ],
    targets: [
        .executableTarget(
            name: "agency-apple-container-wait-helper",
            dependencies: [
                .product(name: "ContainerAPIClient", package: "container")
            ]
        )
    ]
)
