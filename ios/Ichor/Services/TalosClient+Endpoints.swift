import Foundation
import Ichorgo
import IchorCore

/// Endpoints of the stored contexts: test, edit, and search the local networks for them.
extension TalosClient {
    /// Hosts on `networks` (CIDRs, see scanNetworks) that answer the Talos API with the
    /// credentials of a context of `stored`; control plane nodes first.
    static func findEndpoints(stored: String, networks: [String]) async throws -> [EndpointMatch] {
        let cidrs = networks.joined(separator: ",")
        return try await json { IchorgoFindEndpoints(stored, cidrs, $0) }
    }

    /// What `endpoint` answers when asked with `context`'s credentials.
    static func probeEndpoint(stored: String, context: String, endpoint: String) async throws -> EndpointProbe {
        try await json { IchorgoProbeEndpoint(stored, context, endpoint, $0) }
    }

    /// `stored` with `context`'s endpoints replaced by `endpoints`, in order.
    static func setContextEndpoints(stored: String, context: String, endpoints: [String]) async throws -> String {
        let list = endpoints.joined(separator: ",")
        return try await run { IchorgoSetContextEndpoints(stored, context, list, $0) }
    }

    /// `stored` with `endpoint` first among `context`'s endpoints.
    static func addContextEndpoint(stored: String, context: String, endpoint: String) async throws -> String {
        try await run { IchorgoAddContextEndpoint(stored, context, endpoint, $0) }
    }
}
