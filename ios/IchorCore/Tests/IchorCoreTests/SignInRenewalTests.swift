import XCTest
@testable import IchorCore

final class SignInRenewalTests: XCTestCase {
    private let oidc = ContextSummary(name: "sso-lab", kind: ContextKind.kube, fingerprint: "fpoidc")
    private let omni = ContextSummary(name: "omni-lab", fingerprint: "fpomni")

    func testDecodesTheCoreReport() throws {
        let json =
            #"[{"context":"sso-lab","kind":"kube","method":"oidc","status":"renewed","expires":1900000000},"#
            + #"{"context":"omni-lab","kind":"talos","method":"omni","status":"signInRequired","sessionExpires":1800000000,"reason":"the Omni key expired"}]"#
        let list = try SignInRenewal.decodeList(json)
        XCTAssertEqual(list.count, 2)
        XCTAssertTrue(list[0].signedIn)
        XCTAssertEqual(list[0].expires, 1_900_000_000)
        XCTAssertTrue(list[1].signInRequired)
        XCTAssertEqual(list[1].reason, "the Omni key expired")
        XCTAssertEqual(try SignInRenewal.decodeList("[]"), [])
    }

    func testAnEndedSessionIsToldOnceUntilSignedInAgain() {
        let ended = [SignInRenewal(context: "sso-lab", status: SignInRenewal.signInRequiredStatus)]
        let contexts = [oidc, omni]

        let first = signInAlertPlan(renewals: ended, contexts: contexts, notified: [])
        XCTAssertEqual(first.contexts, [oidc])
        XCTAssertEqual(first.notified, ["fpoidc"])

        let second = signInAlertPlan(renewals: ended, contexts: contexts, notified: first.notified)
        XCTAssertEqual(second.contexts, [])

        let renewed = signInAlertPlan(
            renewals: [SignInRenewal(context: "sso-lab", status: SignInRenewal.renewed)],
            contexts: contexts, notified: second.notified)
        XCTAssertEqual(renewed.notified, [])
        XCTAssertEqual(
            signInAlertPlan(renewals: ended, contexts: contexts, notified: renewed.notified).contexts, [oidc])
    }

    func testOfflineTellsNothingAndKeepsWhatWasTold() {
        let offline = [SignInRenewal(context: "sso-lab", status: SignInRenewal.error)]
        let plan = signInAlertPlan(renewals: offline, contexts: [oidc], notified: ["fpoidc"])
        XCTAssertEqual(plan.contexts, [])
        XCTAssertEqual(plan.notified, ["fpoidc"])
    }

    func testMatchesTheContextOfItsKindAndWatchedOnly() {
        let kubeNamedLikeOmni = [SignInRenewal(context: "omni-lab", status: SignInRenewal.signInRequiredStatus)]
        XCTAssertEqual(signInAlertPlan(renewals: kubeNamedLikeOmni, contexts: [oidc, omni], notified: []).contexts, [])

        let talos = [
            SignInRenewal(context: "omni-lab", kind: ContextKind.talos, status: SignInRenewal.signInRequiredStatus)
        ]
        XCTAssertEqual(signInAlertPlan(renewals: talos, contexts: [oidc, omni], notified: []).contexts, [omni])
        XCTAssertEqual(signInAlertPlan(renewals: talos, contexts: [oidc], notified: []).contexts, [])
    }

    func testSignInWithGoogleIsRenewedByTheAppNotTold() {
        let google = [
            SignInRenewal(
                context: "sso-lab", status: SignInRenewal.signInRequiredStatus,
                reason: SignInRenewal.googleNativeReason)
        ]
        XCTAssertEqual(signInAlertPlan(renewals: google, contexts: [oidc], notified: []).contexts, [])
    }

    func testTheAlertIsToldAsACertificateAlert() {
        XCTAssertEqual(alertKind(key: signInAlertKey), "cert")
        let alert = signInAlert(
            title: "Sign in again", text: "sso-lab needs you to sign in again before its next check.")
        XCTAssertEqual(alert.key, signInAlertKey)
        XCTAssertTrue(alert.problem)
    }
}
