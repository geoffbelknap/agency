import ContainerAPIClient
import Foundation

struct HelperEvent: Encodable {
    let id: String
    let source_type: String
    let source_name: String
    let event_type: String
    let timestamp: String
    let data: [String: JSONValue]
    let metadata: [String: JSONValue]
}

enum JSONValue: Encodable {
    case string(String)
    case bool(Bool)
    case int(Int32)

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .string(let value):
            try container.encode(value)
        case .bool(let value):
            try container.encode(value)
        case .int(let value):
            try container.encode(value)
        }
    }
}

enum HelperError: Error, CustomStringConvertible {
    case usage
    case invalidCommand(String)
    case missingContainerID
    case alreadyRunning(String)
    case notOwned(String)

    var description: String {
        switch self {
        case .usage:
            return "usage: agency-apple-container-wait-helper <health|start-wait> [container-id]"
        case .invalidCommand(let command):
            return "unknown command \(command)"
        case .missingContainerID:
            return "start-wait requires a container id"
        case .alreadyRunning(let id):
            return "container \(id) is already running; wait monitoring must be registered before start"
        case .notOwned(let id):
            return "container \(id) is not an Agency-owned apple-container resource"
        }
    }
}

@main
struct AgencyAppleContainerWaitHelper {
    static func main() async {
        do {
            try await run(Array(CommandLine.arguments.dropFirst()))
        } catch {
            fputs("\(error)\n", stderr)
            exit(1)
        }
    }

    static func run(_ args: [String]) async throws {
        guard let command = args.first else {
            throw HelperError.usage
        }
        switch command {
        case "health":
            guard args.count == 1 else {
                throw HelperError.usage
            }
            writeHealth()
        case "start-wait":
            guard args.count == 2 else {
                throw HelperError.missingContainerID
            }
            try await startAndWait(containerID: args[1])
        default:
            throw HelperError.invalidCommand(command)
        }
    }

    static func writeHealth() {
        print(#"{"ok":true,"backend":"apple-container","event_support":"process_wait"}"#)
        fflush(stdout)
    }

    static func startAndWait(containerID: String) async throws {
        let client = ContainerClient()
        let snapshot = try await client.get(id: containerID)
        let labels = snapshot.configuration.labels
        guard labels["agency.managed"] == "true", labels["agency.backend"] == "apple-container" else {
            throw HelperError.notOwned(containerID)
        }
        if snapshot.status == .running {
            throw HelperError.alreadyRunning(containerID)
        }

        let process = try await client.bootstrap(id: containerID, stdio: [nil, nil, nil])
        try await process.start()
        writeEvent(
            eventType: "runtime.container.started",
            containerID: containerID,
            labels: labels,
            status: "running",
            exitCode: nil
        )
        let exitCode = try await process.wait()
        writeEvent(
            eventType: "runtime.container.exited",
            containerID: containerID,
            labels: labels,
            status: "exited",
            exitCode: exitCode
        )
    }

    static func writeEvent(eventType: String, containerID: String, labels: [String: String], status: String, exitCode: Int32?) {
        var data: [String: JSONValue] = [
            "backend": .string("apple-container"),
            "container_id": .string(containerID),
            "agent": .string(labels["agency.agent"] ?? ""),
            "role": .string(labels["agency.role"] ?? labels["agency.type"] ?? ""),
            "instance": .string(labels["agency.instance"] ?? ""),
            "status": .string(status),
            "reason": .string("process_wait")
        ]
        if let exitCode {
            data["exit_code"] = .int(exitCode)
        }
        let event = HelperEvent(
            id: "evt-runtime-\(UUID().uuidString.lowercased())",
            source_type: "platform",
            source_name: "host-adapter/apple-container",
            event_type: eventType,
            timestamp: ISO8601DateFormatter().string(from: Date()),
            data: data,
            metadata: [
                "agency_home_hash": .string(labels["agency.home"] ?? ""),
                "owned": .bool(true)
            ]
        )
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.withoutEscapingSlashes]
        guard let encoded = try? encoder.encode(event), let line = String(data: encoded, encoding: .utf8) else {
            return
        }
        print(line)
        fflush(stdout)
    }
}
