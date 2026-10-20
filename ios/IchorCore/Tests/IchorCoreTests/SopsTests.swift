import XCTest
@testable import IchorCore

final class SopsTests: XCTestCase {
    func testDecodesAFileAndShortensItsKeys() throws {
        let json = #"""
        {"encrypted":true,"format":"yaml","recipients":[{"type":"age","id":"age1testrecipient00000000000000000000000000000000000000000000"},
         {"type":"kms","id":"arn:aws:kms:eu-west-1:000000000000:key/test-key-id-0000"}],"encryptedKeys":3,"clearKeys":["cluster.secret"]}
        """#
        let info = try JSONDecoder().decode(SopsInfo.self, from: Data(json.utf8))
        XCTAssertTrue(info.encrypted)
        XCTAssertEqual(info.clearKeys, ["cluster.secret"])
        XCTAssertEqual(info.recipients[0].short, "age1testre…000000")
        XCTAssertEqual(info.recipients[1].short, "…key/test-key-id-0000")
    }

    func testCiphertextCollapsesAndTheWarningDecodes() throws {
        XCTAssertEqual(collapseSopsValues("password: ENC[AES256_GCM,data:abc=,iv:x,tag:y,type:str]\nname: web"), "password: ENC[…]\nname: web")

        let check = try JSONDecoder().decode(GitOpsEditCheck.self, from: Data(#"""
        {"kind":"kustomize","ok":true,"sops":{"warn":true,"what":"Secret","rule":{"matched":true,"pathRegex":"kubernetes/.*secret.*","recipients":[{"type":"age","id":"age1x"}]}}}
        """#.utf8))
        XCTAssertEqual(check.sops?.warn, true)
        XCTAssertEqual(check.sops?.isSecret, true)
        XCTAssertEqual(check.sops?.rule?.pathRegex, "kubernetes/.*secret.*")
    }

    func testTreeEntriesCarryTheFlag() throws {
        let tree = try JSONDecoder().decode(GitTree.self, from: Data(#"{"ref":"main","entries":[{"path":"talos/talsecret.sops.yaml","type":"file","sops":true},{"path":"talos/nodes","type":"dir"}]}"#.utf8))
        XCTAssertEqual(tree.entries.map(\.sops), [true, false])
    }
}
