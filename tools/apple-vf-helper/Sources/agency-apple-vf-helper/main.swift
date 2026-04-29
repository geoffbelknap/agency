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

func baseDetails(request: HelperRequest) -> [String: String] {
    var details = [
        "protocol": "argv-json",
        "backend": backendName
    ]
    if let config = request.config {
        if let kernelPath = config.kernelPath {
            details["kernelPath"] = kernelPath
        }
        if let rootfsPath = config.rootfsPath {
            details["rootfsPath"] = rootfsPath
        }
        if let stateDir = config.stateDir {
            details["stateDir"] = stateDir
        }
        if let memoryMiB = config.memoryMiB {
            details["memoryMiB"] = String(memoryMiB)
        }
        if let cpuCount = config.cpuCount {
            details["cpuCount"] = String(cpuCount)
        }
        if let enforcementMode = config.enforcementMode {
            details["enforcementMode"] = enforcementMode
        }
    }
    return details
}

func fail(command: String, request: HelperRequest, vmState: String, details: [String: String], error: String) -> Int32 {
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
        vmState: vmState,
        details: details,
        error: error
    ))
    return 1
}

func pass(command: String, request: HelperRequest, vmState: String, details: [String: String]) -> Int32 {
    writeJSON(HelperResponse(
        ok: true,
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
        vmState: vmState,
        details: details,
        error: nil
    ))
    return 0
}

func requireNonEmpty(_ value: String?, _ name: String) throws -> String {
    let trimmed = value?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    if trimmed.isEmpty {
        throw NSError(domain: "agency-apple-vf-helper", code: 64, userInfo: [NSLocalizedDescriptionKey: "\(name) is required"])
    }
    return trimmed
}

func requireReadableFile(_ path: String, _ name: String) throws {
    var isDir: ObjCBool = false
    guard FileManager.default.fileExists(atPath: path, isDirectory: &isDir) else {
        throw NSError(domain: "agency-apple-vf-helper", code: 66, userInfo: [NSLocalizedDescriptionKey: "\(name) is not readable at \(path)"])
    }
    if isDir.boolValue {
        throw NSError(domain: "agency-apple-vf-helper", code: 66, userInfo: [NSLocalizedDescriptionKey: "\(name) path is a directory: \(path)"])
    }
    guard FileManager.default.isReadableFile(atPath: path) else {
        throw NSError(domain: "agency-apple-vf-helper", code: 66, userInfo: [NSLocalizedDescriptionKey: "\(name) is not readable at \(path)"])
    }
}

func requireWritableDirectory(_ path: String, _ name: String) throws {
    var isDir: ObjCBool = false
    if !FileManager.default.fileExists(atPath: path, isDirectory: &isDir) {
        try FileManager.default.createDirectory(atPath: path, withIntermediateDirectories: true)
        isDir = true
    }
    if !isDir.boolValue {
        throw NSError(domain: "agency-apple-vf-helper", code: 73, userInfo: [NSLocalizedDescriptionKey: "\(name) path is not a directory: \(path)"])
    }
    guard FileManager.default.isWritableFile(atPath: path) else {
        throw NSError(domain: "agency-apple-vf-helper", code: 73, userInfo: [NSLocalizedDescriptionKey: "\(name) is not writable at \(path)"])
    }
}

func prepare(command: String, request: HelperRequest) -> Int32 {
    var details = baseDetails(request: request)
    do {
        let runtimeID = try requireNonEmpty(request.runtimeID, "runtimeID")
        guard let role = request.role else {
            throw NSError(domain: "agency-apple-vf-helper", code: 64, userInfo: [NSLocalizedDescriptionKey: "role is required"])
        }
        guard request.backend == nil || request.backend == backendName else {
            throw NSError(domain: "agency-apple-vf-helper", code: 64, userInfo: [NSLocalizedDescriptionKey: "backend must be \(backendName)"])
        }
        guard let config = request.config else {
            throw NSError(domain: "agency-apple-vf-helper", code: 64, userInfo: [NSLocalizedDescriptionKey: "config is required"])
        }
        let kernelPath = try requireNonEmpty(config.kernelPath, "kernelPath")
        let rootfsPath = try requireNonEmpty(config.rootfsPath, "rootfsPath")
        let stateDir = try requireNonEmpty(config.stateDir, "stateDir")
        let memoryMiB = config.memoryMiB ?? 0
        let cpuCount = config.cpuCount ?? 0
        guard memoryMiB > 0 else {
            throw NSError(domain: "agency-apple-vf-helper", code: 64, userInfo: [NSLocalizedDescriptionKey: "memoryMiB must be positive"])
        }
        guard cpuCount > 0 else {
            throw NSError(domain: "agency-apple-vf-helper", code: 64, userInfo: [NSLocalizedDescriptionKey: "cpuCount must be positive"])
        }
        try requireReadableFile(kernelPath, "kernelPath")
        try requireReadableFile(rootfsPath, "rootfsPath")
        try requireWritableDirectory(stateDir, "stateDir")

        #if canImport(Virtualization)
        guard virtualizationAvailable() else {
            throw NSError(domain: "agency-apple-vf-helper", code: 69, userInfo: [NSLocalizedDescriptionKey: "Apple Virtualization.framework does not report VM support on this host"])
        }
        if #available(macOS 13.0, *) {
            let vmConfig = VZVirtualMachineConfiguration()
            vmConfig.platform = VZGenericPlatformConfiguration()
            vmConfig.bootLoader = VZLinuxBootLoader(kernelURL: URL(fileURLWithPath: kernelPath))
            vmConfig.cpuCount = cpuCount
            vmConfig.memorySize = UInt64(memoryMiB) * 1024 * 1024
            let attachment = try VZDiskImageStorageDeviceAttachment(url: URL(fileURLWithPath: rootfsPath), readOnly: false)
            vmConfig.storageDevices = [VZVirtioBlockDeviceConfiguration(attachment: attachment)]
            vmConfig.entropyDevices = [VZVirtioEntropyDeviceConfiguration()]
            vmConfig.serialPorts = [VZVirtioConsoleDeviceSerialPortConfiguration()]
            try vmConfig.validate()
        } else {
            throw NSError(domain: "agency-apple-vf-helper", code: 69, userInfo: [NSLocalizedDescriptionKey: "apple-vf-microvm requires macOS 13 or newer"])
        }
        #else
        throw NSError(domain: "agency-apple-vf-helper", code: 69, userInfo: [NSLocalizedDescriptionKey: "Virtualization.framework is not available in this build"])
        #endif

        details["runtimeID"] = runtimeID
        details["role"] = role.rawValue
        details["validated"] = "true"
        return pass(command: command, request: request, vmState: "prepared", details: details)
    } catch {
        details["validated"] = "false"
        return fail(command: command, request: request, vmState: "prepare_failed", details: details, error: error.localizedDescription)
    }
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
case "prepare":
    exit(prepare(command: command, request: request))
case "start", "stop", "kill", "inspect", "delete", "events":
    exit(notImplemented(command: command, request: request))
default:
    FileHandle.standardError.write(Data("usage: agency-apple-vf-helper <health|version|prepare|start|stop|kill|inspect|delete|events> [--request-json JSON]\n".utf8))
    exit(64)
}
