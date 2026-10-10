import XCTest
@testable import IchorCore

final class KubeAuthTests: XCTestCase {
    func testSignInRequiredReason() {
        let message = "kube-sign-in-required: sign in to this cluster (eks): the session expired"
        XCTAssertEqual(kubeSignInRequiredReason(message), "sign in to this cluster (eks): the session expired")
        XCTAssertTrue(isKubeSignInRequired(message))
        XCTAssertEqual(kubeSignInRequiredReason("kube-sign-in-required"), "")
        XCTAssertNil(kubeSignInRequiredReason("Kubernetes API: timed out"))
        XCTAssertFalse(isKubeSignInRequired("forbidden"))
    }

    func testRememberedValuesPickTheirOption() throws {
        let json = """
        {"method":"eks","kind":"credentials","options":[["awsAccessKeyId","awsSecretAccessKey"],["awsSsoStartUrl","awsSsoRegion"]],
         "values":{"awsSsoStartUrl":"https://acme.awsapps.com/start","awsSsoRegion":"eu-west-1"},"signedIn":false}
        """
        let info = try XCTUnwrap(try KubeSignInInfo.decode(json))
        XCTAssertEqual(info.values["awsSsoRegion"], "eu-west-1")
        XCTAssertEqual(info.rememberedOption, 1)
        XCTAssertTrue(kubeFieldsComplete(info.fieldSets[1], values: info.values))
    }

    func testSignInInfoDecodes() throws {
        let json = """
        {"method":"eks","kind":"credentials","fields":["awsSsoStartUrl","awsSsoRegion","awsAccountId","awsRoleName"],
         "options":[["awsSsoStartUrl","awsSsoRegion","awsAccountId","awsRoleName"],["awsAccessKeyId","awsSecretAccessKey","awsSessionToken"]],
         "signedIn":true,"user":"arn:aws:iam::1:user/me","sessionExpires":1790000000}
        """
        let info = try XCTUnwrap(try KubeSignInInfo.decode(json))
        XCTAssertEqual(info.method, "eks")
        XCTAssertTrue(info.isCredentials)
        XCTAssertEqual(info.fieldSets.count, 2)
        XCTAssertTrue(info.signedIn)
        XCTAssertEqual(info.user, "arn:aws:iam::1:user/me")
        XCTAssertEqual(info.sessionExpires, 1_790_000_000)

        XCTAssertEqual(info.values, [:])
        XCTAssertEqual(info.rememberedOption, 0)

        let browser = try XCTUnwrap(try KubeSignInInfo.decode(#"{"method":"oidc","kind":"browser","signedIn":false}"#))
        XCTAssertFalse(browser.isCredentials)
        XCTAssertEqual(browser.fieldSets, [])
        XCTAssertNil(browser.user)

        let single = KubeSignInInfo(method: "gke", kind: "credentials", fields: ["gcpServiceAccountJson"])
        XCTAssertEqual(single.fieldSets, [["gcpServiceAccountJson"]])
        XCTAssertEqual(kubeFieldInput("gcpUserCredentialsJson"), .json)
        XCTAssertEqual(kubeFieldInput("gcpOAuthClientSecret"), .secret)
        XCTAssertEqual(kubeFieldInput("gcpOAuthClientId"), .plain)

        // The OAuth client of the last sign-in reopens its option, filled.
        let gke = try XCTUnwrap(try KubeSignInInfo.decode(#"""
            {"method":"gke","kind":"credentials","options":[["gcpServiceAccountJson"],["gcpUserCredentialsJson"],["gcpOAuthClientId","gcpOAuthClientSecret"]],"values":{"gcpOAuthClientId":"123-abc.apps.googleusercontent.com","gcpOAuthClientSecret":"s"},"signedIn":false}
            """#))
        XCTAssertEqual(gke.rememberedOption, 2)
        XCTAssertTrue(kubeFieldsComplete(gke.fieldSets[2], values: gke.values))

        XCTAssertNil(try KubeSignInInfo.decode(""))
    }

    func testPromptDecodes() throws {
        let device = try TalosJSON.decode(KubeSignInPrompt.self, from: """
        {"kind":"device","url":"https://login.example/device?code=AB","userCode":"AB-CD",
         "verificationUrl":"https://login.example/device","expiresIn":900}
        """)
        XCTAssertTrue(device.isDevice)
        XCTAssertEqual(device.userCode, "AB-CD")
        XCTAssertEqual(device.openURL, "https://login.example/device?code=AB")
        XCTAssertEqual(device.expiresIn, 900)

        let bare = KubeSignInPrompt(kind: "device", url: "", verificationURL: "https://v.example")
        XCTAssertEqual(bare.openURL, "https://v.example")

        let browser = try TalosJSON.decode(KubeSignInPrompt.self, from: """
        {"kind":"browser","url":"https://idp.example/auth?x=1","redirectPrefix":"http://localhost:8000"}
        """)
        XCTAssertFalse(browser.isDevice)
        XCTAssertEqual(browser.redirectPrefix, "http://localhost:8000")
    }

    func testFieldInputs() {
        XCTAssertEqual(kubeFieldInput("gcpServiceAccountJson"), .json)
        XCTAssertEqual(kubeFieldInput("awsSecretAccessKey"), .secret)
        XCTAssertEqual(kubeFieldInput("rancherApiKey"), .secret)
        XCTAssertEqual(kubeFieldInput("awsAccessKeyId"), .plain)
        XCTAssertEqual(kubeFieldInput("rancherServer"), .plain)
        XCTAssertTrue(kubeFieldOptional("awsSessionToken"))
        XCTAssertFalse(kubeFieldOptional("awsAccessKeyId"))
    }

    func testSecretsJSONKeepsTheOptionFieldsTrimmed() throws {
        let fields = ["awsAccessKeyId", "awsSecretAccessKey", "awsSessionToken"]
        let values = ["awsAccessKeyId": " AKIA ", "awsSecretAccessKey": "s3cr\"et", "awsSessionToken": "  ",
                      "awsSsoStartUrl": "https://other"]
        let json = kubeSecretsJSON(fields: fields, values: values)
        let decoded = try JSONDecoder().decode([String: String].self, from: Data(json.utf8))
        XCTAssertEqual(decoded, ["awsAccessKeyId": "AKIA", "awsSecretAccessKey": "s3cr\"et"])
        XCTAssertEqual(kubeSecretsJSON(fields: [], values: [:]), "{}")

        XCTAssertTrue(kubeFieldsComplete(fields, values: values))
        XCTAssertFalse(kubeFieldsComplete(fields, values: ["awsAccessKeyId": "x"]))
    }

    func testDiscoverFields() throws {
        let fields = try decodeKubeDiscoverFields(#"{"eks":["awsRegion","awsAccessKeyId"],"gke":["gcpServiceAccountJson"]}"#)
        XCTAssertEqual(fields["eks"], ["awsRegion", "awsAccessKeyId"])
        XCTAssertEqual(kubeDiscoverProviders, ["eks", "gke", "aks", "digitalocean", "rancher"])

        // GKE takes a service account key or gcloud user credentials; the others their one set.
        let options = try decodeKubeDiscoverOptions(#"{"gke":[["gcpServiceAccountJson"],["gcpUserCredentialsJson","gcpProjects"]]}"#)
        XCTAssertEqual(kubeDiscoverOptionSets(provider: "gke", fields: fields, options: options),
                       [["gcpServiceAccountJson"], ["gcpUserCredentialsJson", "gcpProjects"]])
        XCTAssertEqual(kubeDiscoverOptionSets(provider: "eks", fields: fields, options: options), [["awsRegion", "awsAccessKeyId"]])
        XCTAssertEqual(kubeDiscoverOptionSets(provider: "aks", fields: fields, options: options), [])
        // The project IDs are optional: the credential alone is enough to search.
        XCTAssertTrue(kubeFieldsComplete(["gcpUserCredentialsJson", "gcpProjects"], values: ["gcpUserCredentialsJson": "{}"]))
    }

    func testImportedSignInContexts() {
        let summary = ConfigSummary(current: "a", contexts: [
            ContextSummary(name: "eks-a", kind: ContextKind.kube, signIn: "eks"),
            ContextSummary(name: "eks-b", kind: ContextKind.kube, signIn: "eks"),
            ContextSummary(name: "static", kind: ContextKind.kube),
            ContextSummary(name: "eks-c", kind: ContextKind.kube, signIn: "eks"),
            ContextSummary(name: "broken", kind: ContextKind.kube, problem: "kube-invalid", signIn: "eks"),
        ])
        let names = importedSignInContexts(summary: summary, selected: [0, 1, 2, 4], replacing: [1],
                                           conflicts: [(index: 0, suggested: "eks-a-1"), (index: 1, suggested: "eks-b-1")])
        XCTAssertEqual(names, ["eks-a-1", "eks-b"])
        XCTAssertTrue(isNotCredentialsMethod("this cluster does not sign in with credentials"))
        XCTAssertFalse(isNotCredentialsMethod("enter a DigitalOcean API token"))
    }

    func testAuthMap() {
        var map = KubeAuthMap.saving([:], key: "fp1", state: #"{"method":"oidc"}"#)
        map = KubeAuthMap.saving(map, key: "fp2", state: #"{"method":"gke"}"#)
        XCTAssertEqual(map.count, 2)
        map = KubeAuthMap.saving(map, key: "fp1", state: "")
        XCTAssertEqual(Array(map.keys), ["fp2"])
        XCTAssertEqual(KubeAuthMap.saving(map, key: "", state: "x"), map)

        let data = KubeAuthMap.encode(map)
        XCTAssertEqual(KubeAuthMap.decode(data), map)
        XCTAssertEqual(KubeAuthMap.decode(nil), [:])
        XCTAssertEqual(KubeAuthMap.decode(Data("not json".utf8)), [:])

        let kept = KubeAuthMap.keeping(["a": "1", "b": "2", "c": ""], fingerprints: ["a", "c", ""])
        XCTAssertEqual(kept, ["a": "1"])
    }

    func testKubeAccessRouting() {
        let talos = ContextSummary(name: "lab", fingerprint: "t1", roles: ["os:reader"])
        let kube = ContextSummary(name: "lab-oidc", kind: ContextKind.kube, fingerprint: "k1")
        let links = ["t1": "k1"]
        XCTAssertEqual(kubeAccessContext(of: talos, links: links, kubeContexts: [kube]), "lab-oidc")
        XCTAssertNil(kubeAccessContext(of: talos, links: [:], kubeContexts: [kube]))
        XCTAssertNil(kubeAccessContext(of: talos, links: ["t1": "gone"], kubeContexts: [kube]))
        XCTAssertNil(kubeAccessContext(of: kube, links: ["k1": "k1"], kubeContexts: [kube]))
        let demo = ContextSummary(name: "demo", fingerprint: "t1", demo: true)
        XCTAssertNil(kubeAccessContext(of: demo, links: links, kubeContexts: [kube]))
        // An Omni cluster's Kubernetes goes through Omni: a link set earlier does not apply.
        let omni = ContextSummary(name: "acme-demo", fingerprint: "t1", signIn: "omni", omni: true, authKey: "omnikey")
        XCTAssertNil(kubeAccessContext(of: omni, links: links, kubeContexts: [kube]))
        XCTAssertNil(kubeAccessContext(of: nil, links: links, kubeContexts: [kube]))

        XCTAssertEqual(keepKubeAccess(["t1": "k1", "t2": "k1", "t1x": "gone"], talos: ["t1", "t1x"], kube: ["k1"]), ["t1": "k1"])
    }

    func testLinkedTalosClusterAllowsKubernetes() {
        let reader = ContextSummary(name: "lab", fingerprint: "t1", roles: ["os:reader"])
        XCTAssertFalse(reader.allows(.workloads))
        XCTAssertTrue(reader.allows(.workloads, kubeLinked: true))
        XCTAssertFalse(reader.allows(.kubeconfig, kubeLinked: true))
        XCTAssertFalse(reader.allows(.upgrade, kubeLinked: true))
        XCTAssertFalse(reader.allows(.workloads, kubeLinked: false))
    }

    func testContextSummaryDecodesSignIn() throws {
        let json = #"{"name":"a","kind":"kube","fingerprint":"f","auth":"oidc","signIn":"oidc"}"#
        let ctx = try TalosJSON.decode(ContextSummary.self, from: json)
        XCTAssertEqual(ctx.signIn, "oidc")
        let plain = try TalosJSON.decode(ContextSummary.self, from: #"{"name":"b","signIn":""}"#)
        XCTAssertNil(plain.signIn)
    }

    func testBackupCarriesKubeAuthAndAccess() throws {
        let json = """
        {"format":2,"platform":"android","talosconfig":"context: lab\\n","kubeconfig":"kind: Config\\n",
         "clusters":{"t1":{"kubeAccess":"k1"},"t2":{"kubeAccess":"gone"},"k1":{"name":"OIDC"}},
         "kubeAuth":{"k1":"{\\"method\\":\\"gke\\"}","gone":"{}","k2":""}}
        """
        let payload = try JSONDecoder().decode(BackupPayload.self, from: Data(json.utf8))
        XCTAssertEqual(payload.kubeAuth?["k1"], #"{"method":"gke"}"#)
        XCTAssertEqual(restoredKubeAuth(payload, fingerprints: ["t1", "t2", "k1", "k2"]), ["k1": #"{"method":"gke"}"#])
        let restored = restoredClusters(payload.clusters, fingerprints: ["t1", "t2", "k1"])
        XCTAssertEqual(restored.kubeAccess, ["t1": "k1"])

        let clusters = backupClusters(fingerprints: ["t1", "k1"], names: [:], colors: [:], kubeServers: [:],
                                      kubeAccess: ["t1": "k1", "gone": "k1"])
        XCTAssertEqual(clusters["t1"]?.kubeAccess, "k1")
        XCTAssertNil(clusters["k1"]?.kubeAccess)
        XCTAssertNil(clusters["gone"])

        // Older payloads have neither.
        let old = try JSONDecoder().decode(BackupPayload.self, from: Data(#"{"format":1,"talosconfig":"x"}"#.utf8))
        XCTAssertNil(old.kubeAuth)
        XCTAssertEqual(restoredKubeAuth(old, fingerprints: ["k1"]), [:])

        // Written only when set.
        var out = BackupPayload(talosconfig: "x")
        XCTAssertFalse(try TalosJSON.encode(out).contains("kubeAuth"))
        out.kubeAuth = ["k1": "{}"]
        XCTAssertTrue(try TalosJSON.encode(out).contains("\"kubeAuth\""))
    }

    func testAuthStoreKeepsKubeAndOmniFingerprints() {
        let talos = [
            ContextSummary(name: "lab", fingerprint: "t1", roles: ["os:admin"]),
            ContextSummary(name: "prod", fingerprint: "t2", signIn: "omni", omni: true, identity: "ops@example.com"),
        ]
        let kube = [ContextSummary(name: "eks", kind: ContextKind.kube, fingerprint: "k1", auth: "eks")]
        XCTAssertEqual(authStoreFingerprints(talos: talos, kube: kube), ["k1", "t2"])
        XCTAssertEqual(authStoreFingerprints(talos: talos, kube: []), ["t2"])
        XCTAssertEqual(authStoreFingerprints(talos: [], kube: []), [])
    }
}
