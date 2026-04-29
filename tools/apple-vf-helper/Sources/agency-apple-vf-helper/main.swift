import Foundation

#if canImport(Virtualization)
import Virtualization
#endif

enum HelperCommand: String, Codable {
    case health
    case version
    case prepare
    case start
    case stop
    case kill
    case inspect
    case delete
    case events
}

enum ComponentRole: String, Codable {
    case workload
    case enforcer
}

struct VMConfig: Codable {
    let kernelPath: String?
    let rootfsPath: String?
    let stateDir: String?
    let memoryMiB: Int?
    let cpuCount: Int?
    let enforcementMode: String?
}

struct HelperRequest: Codable {
    let requestID: String?
    let runtimeID: String?
    let role: ComponentRole?
    let backend: String?
    let agencyHomeHash: String?
    let config: VMConfig?
}

struct HelperResponse: Encodable {
    let ok: Bool
    let backend: String
    let command: String
    let version: String
    let requestID: String?
    let runtimeID: String?
    let role: ComponentRole?
    let agencyHomeHash: String?
    let darwin: String
    let arch: String
    let virtualizationAvailable: Bool
    let vmState: String?
    let details: [String: String]?
    let error: String?
}

struct HelperEvent: Encodable {
    let backend: String
    let version: String
    let requestID: String?
    let runtimeID: String
    let role: ComponentRole
    let agencyHomeHash: String?
    let type: String
    let vmState: String?
    let detail: String?
}

let backendName = "apple-vf-microvm"
let helperVersion = "0.1.0"

func writeJSON(_ response: HelperResponse) {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    let data = try! encoder.encode(response)
    FileHandle.standardOutput.write(data)
    FileHandle.standardOutput.write(Data([0x0a]))
}

func darwinVersion() -> String {
    var uts = utsname()
    uname(&uts)
    return withUnsafePointer(to: &uts.release) {
        $0.withMemoryRebound(to: CChar.self, capacity: 1) {
            String(cString: $0)
        }
    }
}

func currentArch() -> String {
    var uts = utsname()
    uname(&uts)
    return withUnsafePointer(to: &uts.machine) {
        $0.withMemoryRebound(to: CChar.self, capacity: 1) {
            String(cString: $0)
        }
    }
}

func virtualizationAvailable() -> Bool {
    #if canImport(Virtualization)
    if #available(macOS 13.0, *) {
        return VZVirtualMachine.isSupported
    }
    return false
    #else
    return false
    #endif
}

func emptyRequest() -> HelperRequest {
    HelperRequest(
        requestID: nil,
        runtimeID: nil,
        role: nil,
        backend: nil,
        agencyHomeHash: nil,
        config: nil
    )
}

func parseRequest(args: [String]) throws -> HelperRequest {
    guard let index = args.firstIndex(of: "--request-json") else {
        return emptyRequest()
    }
    let valueIndex = args.index(after: index)
    guard valueIndex < args.endIndex else {
        throw NSError(domain: "agency-apple-vf-helper", code: 64, userInfo: [NSLocalizedDescriptionKey: "--request-json requires a JSON value"])
    }
    let data = Data(args[valueIndex].utf8)
    return try JSONDecoder().decode(HelperRequest.self, from: data)
}

func health(command: String, request: HelperRequest) -> Int32 {
    let supported = virtualizationAvailable()
    let arch = currentArch()
    let ok = supported && (arch == "arm64" || arch == "arm64e")
    let err: String?
    if ok {
        err = nil
    } else if !supported {
        err = "Apple Virtualization.framework does not report VM support on this host"
    } else {
        err = "apple-vf-microvm requires Apple silicon; host architecture is \(arch)"
    }
    writeJSON(HelperResponse(
        ok: ok,
        backend: backendName,
        command: command,
        version: helperVersion,
        requestID: request.requestID,
        runtimeID: request.runtimeID,
        role: request.role,
        agencyHomeHash: request.agencyHomeHash,
        darwin: darwinVersion(),
        arch: arch,
        virtualizationAvailable: supported,
        vmState: nil,
        details: nil,
        error: err
    ))
    return ok ? 0 : 1
}

func notImplemented(command: String, request: HelperRequest) -> Int32 {
    writeJSON(HelperResponse(
        ok: false,
        backend: backendName,
        command: command,
        version: helperVersion,
        requestID: request.requestID,
        runtimeID: request.runtimeID,
        role: request.role,
        agencyHomeHash: request.agencyHomeHash,
        darwin: darwinVersion(),
        arch: currentArch(),
        virtualizationAvailable: virtualizationAvailable(),
        vmState: "not_implemented",
        details: [
            "protocol": "argv-json",
            "backend": backendName
        ],
        error: "agency-apple-vf-helper \(command) is not implemented"
    ))
    return 2
}

let args = CommandLine.arguments
let command = args.dropFirst().first ?? "help"
let request: HelperRequest

do {
    request = try parseRequest(args: Array(args.dropFirst(2)))
} catch {
    FileHandle.standardError.write(Data("\(error.localizedDescription)\n".utf8))
    exit(64)
}

switch command {
case "health", "version":
    exit(health(command: command, request: request))
case "prepare", "start", "stop", "kill", "inspect", "delete", "events":
    exit(notImplemented(command: command, request: request))
default:
    FileHandle.standardError.write(Data("usage: agency-apple-vf-helper <health|version|prepare|start|stop|kill|inspect|delete|events> [--request-json JSON]\n".utf8))
    exit(64)
}
