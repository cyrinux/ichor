import XCTest
@testable import TalosdevMobileCore

final class DonationTests: XCTestCase {
    func testAddressesAndURIs() {
        XCTAssertEqual(Donation.btc, "bc1qc0dhqrgw6z08du94rkfequk8n5r3lgcr5lnxtl")
        XCTAssertEqual(Donation.eth, "0xb32676301F9c4abD35Eb2e4c7C8cdA754BA29804")
        XCTAssertEqual(Donation.bitcoin.uri, "bitcoin:bc1qc0dhqrgw6z08du94rkfequk8n5r3lgcr5lnxtl")
        XCTAssertEqual(Donation.ethereum.uri, "ethereum:0xb32676301F9c4abD35Eb2e4c7C8cdA754BA29804")
    }
}
