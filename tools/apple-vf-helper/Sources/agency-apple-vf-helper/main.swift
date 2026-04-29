import Foundation

#if canImport(Virtualization)
import Virtualization
#endif

struct HelperResponse: Encodable {
    let ok: Bool
    let backend: String
    let command: String
    let version: String
    let darwin: String
    let arch: String
    let virtualizationAvailable: Bool
    let error: String?
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

func health(command: String) -> Int32 {
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
        darwin: darwinVersion(),
        arch: arch,
        virtualizationAvailable: supported,
        error: err
    ))
    return ok ? 0 : 1
}

func notImplemented(command: String) -> Int32 {
    writeJSON(HelperResponse(
        ok: false,
        backend: backendName,
        command: command,
        version: helperVersion,
        darwin: darwinVersion(),
        arch: currentArch(),
        virtualizationAvailable: virtualizationAvailable(),
        error: "agency-apple-vf-helper \(command) is not implemented"
    ))
    return 2
}

let args = CommandLine.arguments
let command = args.dropFirst().first ?? "help"

switch command {
case "health", "version":
    exit(health(command: command))
case "prepare", "start", "stop", "kill", "inspect", "delete", "events":
    exit(notImplemented(command: command))
default:
    FileHandle.standardError.write(Data("usage: agency-apple-vf-helper <health|version|prepare|start|stop|kill|inspect|delete|events>\n".utf8))
    exit(64)
}
