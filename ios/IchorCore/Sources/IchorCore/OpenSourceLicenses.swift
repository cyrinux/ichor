import Foundation

/// A third-party component the iOS app ships, for the "Open-source licenses" screen. Android
/// generates its list at build time (AboutLibraries); iOS has no such plugin, so this one is
/// kept by hand: the Swift packages (project.yml), the Go modules the core imports directly
/// (go/go.mod) and the bundled icons. Keep it in step with NOTICE.
public struct OpenSourceLibrary: Equatable, Identifiable, Sendable {
    public let name: String
    public let author: String
    /// SPDX identifiers.
    public let licenses: [String]
    public let website: String

    public var id: String { name }
}

/// The SPDX page of a license, where its full text is.
public func licenseURL(_ spdx: String) -> String { "https://spdx.org/licenses/\(spdx).html" }

public let openSourceLibraries: [OpenSourceLibrary] = [
    OpenSourceLibrary(name: "SwiftTerm", author: "Miguel de Icaza", licenses: ["MIT"],
                      website: "https://github.com/migueldeicaza/SwiftTerm"),
    OpenSourceLibrary(name: "Talos machinery client library", author: "Sidero Labs, Inc.", licenses: ["MPL-2.0"],
                      website: "https://github.com/siderolabs/talos"),
    OpenSourceLibrary(name: "COSI runtime", author: "Sidero Labs, Inc.", licenses: ["MPL-2.0"],
                      website: "https://github.com/cosi-project/runtime"),
    OpenSourceLibrary(name: "Go mobile bindings", author: "The Go Authors", licenses: ["BSD-3-Clause"],
                      website: "https://pkg.go.dev/golang.org/x/mobile"),
    OpenSourceLibrary(name: "Go standard library, x/crypto, x/net", author: "The Go Authors", licenses: ["BSD-3-Clause"],
                      website: "https://go.dev"),
    OpenSourceLibrary(name: "gRPC-Go", author: "The gRPC Authors", licenses: ["Apache-2.0"],
                      website: "https://github.com/grpc/grpc-go"),
    OpenSourceLibrary(name: "Go Protocol Buffers", author: "The Go Authors", licenses: ["BSD-3-Clause"],
                      website: "https://github.com/protocolbuffers/protobuf-go"),
    OpenSourceLibrary(name: "age", author: "The age Authors", licenses: ["BSD-3-Clause"],
                      website: "https://github.com/FiloSottile/age"),
    OpenSourceLibrary(name: "gopacket", author: "The GoPacket Authors", licenses: ["BSD-3-Clause"],
                      website: "https://github.com/gopacket/gopacket"),
    OpenSourceLibrary(name: "YAML for Go", author: "The go-yaml Authors", licenses: ["MIT", "Apache-2.0"],
                      website: "https://github.com/yaml/go-yaml"),
    OpenSourceLibrary(name: "Dashboard Icons", author: "Homarr Labs", licenses: ["Apache-2.0"],
                      website: "https://github.com/homarr-labs/dashboard-icons"),
    OpenSourceLibrary(name: "selfh.st/icons", author: "selfh.st", licenses: ["CC-BY-4.0"],
                      website: "https://selfh.st/icons/"),
]
