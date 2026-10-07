import XCTest
@testable import IchorCore

final class ClusterNameTests: XCTestCase {
    private let prod = ContextSummary(name: "admin@talos-prod-eu-west-1", fingerprint: "fp-prod")
    private let lab = ContextSummary(name: "admin@lab", fingerprint: "fp-lab")

    func testGivenNameReplacesTheContextName() {
        let labels = ClusterLabels(names: ["fp-prod": "Prod"])
        XCTAssertEqual(labels.of(prod), "Prod")
        XCTAssertEqual(labels.of(lab), "admin@lab")
    }

    func testScreenshotModeShowsTheMaskedContextName() {
        let labels = ClusterLabels(names: ["fp-prod": "Prod"], masked: true)
        XCTAssertEqual(labels.of(prod), "admin@talos-prod-eu-west-1")
        XCTAssertNil(labels.given(prod))
    }

    func testNoFingerprintNoGivenName() {
        XCTAssertEqual(ClusterLabels(names: ["": "Old"]).of(ContextSummary(name: "admin@old")), "admin@old")
    }

    func testTypedNamesAreTrimmedAndBlankResets() {
        XCTAssertEqual(normalizeClusterName("  Prod "), "Prod")
        XCTAssertNil(normalizeClusterName("   "))
        XCTAssertEqual(normalizeClusterName(String(repeating: "x", count: 100))?.count, clusterNameMax)
    }

    func testNamesOfRemovedClustersAreForgotten() {
        let saved = ["fp-prod": "Prod", "fp-gone": "Gone"]
        XCTAssertEqual(keepClusterNames(saved: saved, fingerprints: ["fp-prod", "fp-lab"]), ["fp-prod": "Prod"])
    }

    private let eks = ContextSummary(name: "arn:aws:eks:eu-north-1:731942086515:cluster/orbit-prod-eu", fingerprint: "fp-eks")
    private let gke = ContextSummary(name: "gke_lumen-sandbox-42_europe-west4_lumen-gke-blue", fingerprint: "fp-gke")

    func testEksArnIsParsed() {
        let cloud = parseCloudContext(eks.name)
        XCTAssertEqual(cloud?.provider, CloudContext.eks)
        XCTAssertEqual(cloud?.location, "eu-north-1")
        XCTAssertEqual(cloud?.owner, "731942086515")
        XCTAssertEqual(cloud?.cluster, "orbit-prod-eu")
        XCTAssertEqual(cloud?.shortOwner, "73…15")
        XCTAssertEqual(parseCloudContext("arn:aws-cn:eks:cn-north-1:123456789012:cluster/c")?.location, "cn-north-1")
        XCTAssertEqual(parseCloudContext("arn:aws-us-gov:eks:us-gov-west-1:123456789012:cluster/c")?.cluster, "c")
    }

    func testGkeContextIsParsed() {
        let cloud = parseCloudContext(gke.name)
        XCTAssertEqual(cloud?.provider, CloudContext.gke)
        XCTAssertEqual(cloud?.location, "europe-west4")
        XCTAssertEqual(cloud?.owner, "lumen-sandbox-42")
        XCTAssertEqual(cloud?.cluster, "lumen-gke-blue")
        // A project ID is a name, not a number: shown whole.
        XCTAssertEqual(cloud?.shortOwner, "lumen-sandbox-42")
        XCTAssertEqual(parseCloudContext("gke_proj_europe-west1-b_c")?.location, "europe-west1-b")
        XCTAssertEqual(parseCloudContext("gke_example.com:proj_us-east1_c")?.owner, "example.com:proj")
    }

    func testOtherNamesAreNoCloudContext() {
        XCTAssertNil(parseCloudContext("admin@lab"))
        XCTAssertNil(parseCloudContext("arn:aws:iam::123456789012:role/admin"))
        XCTAssertNil(parseCloudContext("arn:aws:eks:eu-north-1:123456789012:cluster/"))
        XCTAssertNil(parseCloudContext("arn:aws:eks:eu-north-1:account:cluster/c"))
        XCTAssertNil(parseCloudContext("gke_proj_us-central1"))
        XCTAssertNil(parseCloudContext("gke_proj_us-central1_my_cluster"))
    }

    func testCloudContextIsCalledByItsClusterName() {
        XCTAssertEqual(ClusterLabels().of(eks), "orbit-prod-eu")
        XCTAssertEqual(ClusterLabels().of(gke), "lumen-gke-blue")
        XCTAssertEqual(ClusterLabels().cloud(eks)?.location, "eu-north-1")
        XCTAssertEqual(ClusterLabels(names: ["fp-eks": "Prod"]).of(eks), "Prod")
    }

    func testScreenshotModeParsesNoContextName() {
        let labels = ClusterLabels(masked: true)
        XCTAssertEqual(labels.of(eks), eks.name)
        XCTAssertNil(labels.cloud(eks))
        XCTAssertNil(labels.cloud(gke))
    }

    private func kube(name: String = "ctx", auth: String = "cert", server: String = "https://10.0.0.1:6443") -> ContextSummary {
        ContextSummary(name: name, kind: ContextKind.kube, endpoints: [server], auth: auth)
    }

    func testLogoOfATalosClusterIsTalos() {
        XCTAssertEqual(clusterLogo(prod), "talos")
    }

    func testLogoFollowsTheSignInMethod() {
        XCTAssertEqual(clusterLogo(kube(auth: "eks")), "aws")
        XCTAssertEqual(clusterLogo(kube(auth: "gke")), "google-cloud")
        XCTAssertEqual(clusterLogo(kube(auth: "azure")), "azure")
        XCTAssertEqual(clusterLogo(kube(auth: "digitalocean")), "digital-ocean")
        XCTAssertEqual(clusterLogo(kube(auth: "rancher")), "rancher")
    }

    func testStaticCredentialsFallBackOnTheNameThenTheHost() {
        XCTAssertEqual(clusterLogo(kube(name: eks.name, auth: "token")), "aws")
        XCTAssertEqual(clusterLogo(kube(name: gke.name, auth: "cert")), "google-cloud")
        XCTAssertEqual(clusterLogo(kube(server: "https://ABCD.gr7.eu-west-1.eks.amazonaws.com")), "aws")
        XCTAssertEqual(clusterLogo(kube(server: "https://aks-dns-1234.hcp.westeurope.azmk8s.io:443")), "azure")
        XCTAssertEqual(clusterLogo(kube(server: "https://1234-abcd.k8s.ondigitalocean.com")), "digital-ocean")
        XCTAssertEqual(clusterLogo(kube()), "kubernetes")
        XCTAssertEqual(clusterLogo(kube(auth: "oidc")), "kubernetes")
    }

    func testDiscoveryProvidersShowTheirCloudLogo() {
        XCTAssertEqual(kubeDiscoverProviders.map(discoveryLogo), ["aws", "google-cloud", "azure", "digital-ocean", "rancher"])
        XCTAssertEqual(discoveryLogo("other"), "kubernetes")
    }
}
