import Foundation
import Security
import Darwin

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
    case run
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
let stateFileName = "state.json"
let serialLogFileName = "serial.log"

struct RuntimeState: Codable {
    var backend: String
    var version: String
    var requestID: String?
    var runtimeID: String
    var role: ComponentRole
    var agencyHomeHash: String?
    var vmState: String
    var pid: Int32?
    var kernelPath: String
    var rootfsPath: String
    var stateDir: String
    var serialLogPath: String
    var startedAt: String?
    var updatedAt: String
    var error: String?
}

func isoNow() -> String {
    ISO8601DateFormatter().string(from: Date())
}

func statePath(_ stateDir: String) -> String {
    URL(fileURLWithPath: stateDir).appendingPathComponent(stateFileName).path
}

func serialLogPath(_ stateDir: String) -> String {
    URL(fileURLWithPath: stateDir).appendingPathComponent(serialLogFileName).path
}

func writeState(_ state: RuntimeState) throws {
    let data = try JSONEncoder().encode(state)
    try data.write(to: URL(fileURLWithPath: statePath(state.stateDir)), options: [.atomic])
}

func readState(_ stateDir: String) throws -> RuntimeState {
    let data = try Data(contentsOf: URL(fileURLWithPath: statePath(stateDir)))
    return try JSONDecoder().decode(RuntimeState.self, from: data)
}

func processAlive(_ pid: Int32?) -> Bool {
    guard let pid = pid, pid > 0 else {
        return false
    }
    if kill(pid, 0) == 0 {
        return true
    }
    return errno == EPERM
}

func detailsFromState(_ state: RuntimeState) -> [String: String] {
    var details = [
        "protocol": "argv-json",
        "backend": backendName,
        "runtimeID": state.runtimeID,
        "role": state.role.rawValue,
        "kernelPath": state.kernelPath,
        "rootfsPath": state.rootfsPath,
        "stateDir": state.stateDir,
        "statePath": statePath(state.stateDir),
        "serialLogPath": state.serialLogPath,
        "updatedAt": state.updatedAt
    ]
    if let requestID = state.requestID {
        details["requestID"] = requestID
    }
    if let pid = state.pid {
        details["pid"] = String(pid)
    }
    if let startedAt = state.startedAt {
        details["startedAt"] = startedAt
    }
    if let error = state.error {
        details["error"] = error
    }
    return details
}

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

func hasVirtualizationEntitlement() -> Bool {
    guard let task = SecTaskCreateFromSelf(nil) else {
        return false
    }
    guard let value = SecTaskCopyValueForEntitlement(task, "com.apple.security.virtualization" as CFString, nil) else {
        return false
    }
    return (value as? Bool) == true
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

func requestJSON(_ request: HelperRequest) throws -> String {
    let data = try JSONEncoder().encode(request)
    return String(data: data, encoding: .utf8) ?? "{}"
}

func health(command: String, request: HelperRequest) -> Int32 {
    let supported = virtualizationAvailable()
    let arch = currentArch()
    let entitled = hasVirtualizationEntitlement()
    let ok = supported && entitled && (arch == "arm64" || arch == "arm64e")
    let err: String?
    if ok {
        err = nil
    } else if !supported {
        err = "Apple Virtualization.framework does not report VM support on this host"
    } else if !entitled {
        err = "agency-apple-vf-helper is missing com.apple.security.virtualization entitlement"
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

func validatedConfig(request: HelperRequest, validateVM: Bool) throws -> (runtimeID: String, role: ComponentRole, kernelPath: String, rootfsPath: String, stateDir: String, memoryMiB: Int, cpuCount: Int) {
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

        if validateVM {
            try validateVirtualMachineConfiguration(kernelPath: kernelPath, rootfsPath: rootfsPath, memoryMiB: memoryMiB, cpuCount: cpuCount, serialLogPath: nil)
        }
        return (runtimeID, role, kernelPath, rootfsPath, stateDir, memoryMiB, cpuCount)
}

func validateVirtualMachineConfiguration(kernelPath: String, rootfsPath: String, memoryMiB: Int, cpuCount: Int, serialLogPath: String?) throws {
        #if canImport(Virtualization)
        guard virtualizationAvailable() else {
            throw NSError(domain: "agency-apple-vf-helper", code: 69, userInfo: [NSLocalizedDescriptionKey: "Apple Virtualization.framework does not report VM support on this host"])
        }
        if #available(macOS 13.0, *) {
            let vmConfig = VZVirtualMachineConfiguration()
            vmConfig.platform = VZGenericPlatformConfiguration()
            let bootLoader = VZLinuxBootLoader(kernelURL: URL(fileURLWithPath: kernelPath))
            bootLoader.commandLine = "console=hvc0 root=/dev/vda rw init=/sbin/init-spike"
            vmConfig.bootLoader = bootLoader
            vmConfig.cpuCount = cpuCount
            vmConfig.memorySize = UInt64(memoryMiB) * 1024 * 1024
            let attachment = try VZDiskImageStorageDeviceAttachment(url: URL(fileURLWithPath: rootfsPath), readOnly: false)
            vmConfig.storageDevices = [VZVirtioBlockDeviceConfiguration(attachment: attachment)]
            vmConfig.entropyDevices = [VZVirtioEntropyDeviceConfiguration()]
            let serial = VZVirtioConsoleDeviceSerialPortConfiguration()
            if let serialLogPath = serialLogPath {
                FileManager.default.createFile(atPath: serialLogPath, contents: nil)
                let serialHandle = try FileHandle(forWritingTo: URL(fileURLWithPath: serialLogPath))
                try serialHandle.seekToEnd()
                serial.attachment = VZFileHandleSerialPortAttachment(fileHandleForReading: nil, fileHandleForWriting: serialHandle)
            }
            vmConfig.serialPorts = [serial]
            try vmConfig.validate()
        } else {
            throw NSError(domain: "agency-apple-vf-helper", code: 69, userInfo: [NSLocalizedDescriptionKey: "apple-vf-microvm requires macOS 13 or newer"])
        }
        #else
        throw NSError(domain: "agency-apple-vf-helper", code: 69, userInfo: [NSLocalizedDescriptionKey: "Virtualization.framework is not available in this build"])
        #endif
}

func prepare(command: String, request: HelperRequest) -> Int32 {
    var details = baseDetails(request: request)
    do {
        let config = try validatedConfig(request: request, validateVM: true)

        details["runtimeID"] = config.runtimeID
        details["role"] = config.role.rawValue
        details["validated"] = "true"
        return pass(command: command, request: request, vmState: "prepared", details: details)
    } catch {
        details["validated"] = "false"
        return fail(command: command, request: request, vmState: "prepare_failed", details: details, error: error.localizedDescription)
    }
}

#if canImport(Virtualization)
@available(macOS 13.0, *)
final class VMRunDelegate: NSObject, VZVirtualMachineDelegate {
    let stateDir: String
    init(stateDir: String) {
        self.stateDir = stateDir
    }

    func guestDidStop(_ virtualMachine: VZVirtualMachine) {
        updateStoredVMState(stateDir: stateDir, vmState: "stopped", error: nil)
        CFRunLoopStop(CFRunLoopGetMain())
    }

    func virtualMachine(_ virtualMachine: VZVirtualMachine, didStopWithError error: Error) {
        updateStoredVMState(stateDir: stateDir, vmState: "failed", error: error.localizedDescription)
        CFRunLoopStop(CFRunLoopGetMain())
    }
}
#endif

func updateStoredVMState(stateDir: String, vmState: String, error: String?) {
    do {
        var state = try readState(stateDir)
        state.vmState = vmState
        state.updatedAt = isoNow()
        state.error = error
        try writeState(state)
    } catch {
        // There is no safe stderr contract for background state updates.
    }
}

func start(command: String, request: HelperRequest) -> Int32 {
    var details = baseDetails(request: request)
    do {
        let config = try validatedConfig(request: request, validateVM: true)
        let state = RuntimeState(
            backend: backendName,
            version: helperVersion,
            requestID: request.requestID,
            runtimeID: config.runtimeID,
            role: config.role,
            agencyHomeHash: request.agencyHomeHash,
            vmState: "starting",
            pid: nil,
            kernelPath: config.kernelPath,
            rootfsPath: config.rootfsPath,
            stateDir: config.stateDir,
            serialLogPath: serialLogPath(config.stateDir),
            startedAt: nil,
            updatedAt: isoNow(),
            error: nil
        )
        try writeState(state)
        let arg = try requestJSON(request)
        let process = Process()
        process.executableURL = URL(fileURLWithPath: CommandLine.arguments[0])
        process.arguments = ["run", "--request-json", arg]
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        try process.run()
        var started = state
        started.pid = process.processIdentifier
        started.vmState = "starting"
        started.startedAt = isoNow()
        started.updatedAt = started.startedAt ?? isoNow()
        try writeState(started)
        return pass(command: command, request: request, vmState: "starting", details: detailsFromState(started))
    } catch {
        details["validated"] = "false"
        return fail(command: command, request: request, vmState: "start_failed", details: details, error: error.localizedDescription)
    }
}

func runVM(command: String, request: HelperRequest) -> Int32 {
    do {
        let config = try validatedConfig(request: request, validateVM: false)
        updateStoredVMState(stateDir: config.stateDir, vmState: "starting", error: nil)
        try runVirtualMachine(config: config, request: request)
        return 0
    } catch {
        let stateDir = request.config?.stateDir ?? ""
        if !stateDir.isEmpty {
            updateStoredVMState(stateDir: stateDir, vmState: "failed", error: error.localizedDescription)
        }
        return 1
    }
}

func runVirtualMachine(config: (runtimeID: String, role: ComponentRole, kernelPath: String, rootfsPath: String, stateDir: String, memoryMiB: Int, cpuCount: Int), request: HelperRequest) throws {
    #if canImport(Virtualization)
    guard virtualizationAvailable() else {
        throw NSError(domain: "agency-apple-vf-helper", code: 69, userInfo: [NSLocalizedDescriptionKey: "Apple Virtualization.framework does not report VM support on this host"])
    }
    if #available(macOS 13.0, *) {
        let vmConfig = VZVirtualMachineConfiguration()
        vmConfig.platform = VZGenericPlatformConfiguration()
        let bootLoader = VZLinuxBootLoader(kernelURL: URL(fileURLWithPath: config.kernelPath))
        bootLoader.commandLine = "console=hvc0 root=/dev/vda rw init=/sbin/init-spike"
        vmConfig.bootLoader = bootLoader
        vmConfig.cpuCount = config.cpuCount
        vmConfig.memorySize = UInt64(config.memoryMiB) * 1024 * 1024
        let attachment = try VZDiskImageStorageDeviceAttachment(url: URL(fileURLWithPath: config.rootfsPath), readOnly: false)
        vmConfig.storageDevices = [VZVirtioBlockDeviceConfiguration(attachment: attachment)]
        vmConfig.entropyDevices = [VZVirtioEntropyDeviceConfiguration()]
        let serial = VZVirtioConsoleDeviceSerialPortConfiguration()
        FileManager.default.createFile(atPath: serialLogPath(config.stateDir), contents: nil)
        let serialHandle = try FileHandle(forWritingTo: URL(fileURLWithPath: serialLogPath(config.stateDir)))
        try serialHandle.seekToEnd()
        serial.attachment = VZFileHandleSerialPortAttachment(fileHandleForReading: nil, fileHandleForWriting: serialHandle)
        vmConfig.serialPorts = [serial]
        try vmConfig.validate()

        let vm = VZVirtualMachine(configuration: vmConfig)
        let delegate = VMRunDelegate(stateDir: config.stateDir)
        vm.delegate = delegate
        let semaphore = DispatchSemaphore(value: 0)
        var startError: Error?
        vm.start { result in
            switch result {
            case .success:
                updateStoredVMState(stateDir: config.stateDir, vmState: "running", error: nil)
            case .failure(let error):
                startError = error
                updateStoredVMState(stateDir: config.stateDir, vmState: "failed", error: error.localizedDescription)
            }
            semaphore.signal()
        }
        while semaphore.wait(timeout: .now()) == .timedOut {
            RunLoop.current.run(mode: .default, before: Date(timeIntervalSinceNow: 0.05))
        }
        if let startError = startError {
            throw startError
        }
        withExtendedLifetime(delegate) {
            CFRunLoopRun()
        }
        try? serialHandle.close()
    } else {
        throw NSError(domain: "agency-apple-vf-helper", code: 69, userInfo: [NSLocalizedDescriptionKey: "apple-vf-microvm requires macOS 13 or newer"])
    }
    #else
    throw NSError(domain: "agency-apple-vf-helper", code: 69, userInfo: [NSLocalizedDescriptionKey: "Virtualization.framework is not available in this build"])
    #endif
}

func inspect(command: String, request: HelperRequest) -> Int32 {
    let details = baseDetails(request: request)
    do {
        let stateDir = try requireNonEmpty(request.config?.stateDir, "stateDir")
        var state = try readState(stateDir)
        if !processAlive(state.pid) && (state.vmState == "starting" || state.vmState == "running") {
            state.vmState = "stopped"
            state.updatedAt = isoNow()
            try writeState(state)
        }
        return pass(command: command, request: request, vmState: state.vmState, details: detailsFromState(state))
    } catch {
        return fail(command: command, request: request, vmState: "inspect_failed", details: details, error: error.localizedDescription)
    }
}

func stop(command: String, request: HelperRequest, force: Bool) -> Int32 {
    let details = baseDetails(request: request)
    do {
        let stateDir = try requireNonEmpty(request.config?.stateDir, "stateDir")
        var state = try readState(stateDir)
        if processAlive(state.pid), let pid = state.pid {
            let signal = force ? SIGKILL : SIGTERM
            if kill(pid, signal) != 0 && errno != ESRCH {
                throw NSError(domain: NSPOSIXErrorDomain, code: Int(errno), userInfo: [NSLocalizedDescriptionKey: "signal \(pid) failed with errno \(errno)"])
            }
        }
        state.vmState = force ? "killed" : "stopped"
        state.updatedAt = isoNow()
        try writeState(state)
        return pass(command: command, request: request, vmState: state.vmState, details: detailsFromState(state))
    } catch {
        return fail(command: command, request: request, vmState: force ? "kill_failed" : "stop_failed", details: details, error: error.localizedDescription)
    }
}

func deleteVM(command: String, request: HelperRequest) -> Int32 {
    var details = baseDetails(request: request)
    do {
        let stateDir = try requireNonEmpty(request.config?.stateDir, "stateDir")
        if let state = try? readState(stateDir), processAlive(state.pid), let pid = state.pid {
            if kill(pid, SIGTERM) != 0 && errno != ESRCH {
                throw NSError(domain: NSPOSIXErrorDomain, code: Int(errno), userInfo: [NSLocalizedDescriptionKey: "signal \(pid) failed with errno \(errno)"])
            }
        }
        if FileManager.default.fileExists(atPath: stateDir) {
            try FileManager.default.removeItem(atPath: stateDir)
        }
        details["stateDir"] = stateDir
        return pass(command: command, request: request, vmState: "deleted", details: details)
    } catch {
        return fail(command: command, request: request, vmState: "delete_failed", details: details, error: error.localizedDescription)
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
case "start":
    exit(start(command: command, request: request))
case "run":
    exit(runVM(command: command, request: request))
case "inspect":
    exit(inspect(command: command, request: request))
case "stop":
    exit(stop(command: command, request: request, force: false))
case "kill":
    exit(stop(command: command, request: request, force: true))
case "delete":
    exit(deleteVM(command: command, request: request))
case "events":
    exit(notImplemented(command: command, request: request))
default:
    FileHandle.standardError.write(Data("usage: agency-apple-vf-helper <health|version|prepare|start|stop|kill|inspect|delete|events> [--request-json JSON]\n".utf8))
    exit(64)
}
