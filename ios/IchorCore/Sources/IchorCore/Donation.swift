import Foundation

/// Crypto addresses for donations, shown in Settings › About.
public enum Donation: String, CaseIterable, Identifiable, Sendable {
    case bitcoin, ethereum

    public static let btc = "bc1qc0dhqrgw6z08du94rkfequk8n5r3lgcr5lnxtl"
    public static let eth = "0xb32676301F9c4abD35Eb2e4c7C8cdA754BA29804"

    public var id: String { rawValue }

    public var address: String {
        switch self {
        case .bitcoin: Self.btc
        case .ethereum: Self.eth
        }
    }

    /// Payment URI (BIP-21 / EIP-681 style) for the QR code and wallet apps.
    public var uri: String { "\(rawValue):\(address)" }
}
