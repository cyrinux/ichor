import Foundation

// Generated with Android's lb_* resources (and the load balancing checkup_* ones) from one table:
// each text is a key of Localizable.xcstrings and must match it exactly. Arguments are already
// formatted strings, in the order the English sentence names them.

enum LoadBalancingText {
    static var title: String { String(localized: "Load balancing") }
    static var open: String { String(localized: "Open Load balancing") }
    static var empty: String {
        String(
            localized:
                "No load balancer controller and no LoadBalancer Service: install MetalLB, turn on Cilium’s LB IPAM, or use kube-vip or a cloud’s load balancer."
        )
    }
    static var partial: String {
        String(localized: "These credentials may not read every load balancer object: some details are missing.")
    }
    static var pools: String { String(localized: "Pools") }
    static var advertisements: String { String(localized: "Advertisements") }
    static var bgpPeers: String { String(localized: "BGP peers") }
    static var kubeVip: String { String(localized: "kube-vip") }
    static var services: String { String(localized: "LoadBalancer Services") }
    static var causeNoController: String {
        String(localized: "No load balancer controller was found to give it an address.")
    }
    static var causeConfigError: String {
        String(localized: "MetalLB refused its configuration: nothing gets a new address until it is fixed.")
    }
    static var causeControllerDown: String { String(localized: "The load balancer controller is not running.") }
    static var causeNoPool: String { String(localized: "There is no address pool.") }
    static var causePoolNotMatched: String {
        String(
            localized:
                "No pool matches it: check its pool annotation, its class, and the pools’ namespaces, selectors and auto-assign."
        )
    }
    static var causePoolExhausted: String { String(localized: "Its pool is exhausted: no address left.") }
    static var causePoolNotAdvertised: String {
        String(localized: "Nothing advertises its pool: add an L2Advertisement or a BGPAdvertisement.")
    }
    static var causeUnknown: String { String(localized: "The controller has not said why.") }
    static func waiting(_ a: String) -> String { String(localized: "\(a) waits for an address") }
    static func waitingSince(_ a: String) -> String { String(localized: "Waiting for \(a)") }
    static func configError(_ a: String) -> String { String(localized: "Configuration refused: \(a)") }
    static func poolFree(_ a: String, _ b: String) -> String { String(localized: "\(a) of \(b) free") }
    static var poolNotAdvertised: String { String(localized: "Not advertised") }
    static func poolAdvertisedBy(_ a: String) -> String { String(localized: "Advertised by \(a)") }
    static var poolManual: String { String(localized: "Assigned on request only") }
    static func poolNamespaces(_ a: String) -> String { String(localized: "Namespaces: \(a)") }
    static var advAllPools: String { String(localized: "Every pool") }
    static func advPools(_ a: String) -> String { String(localized: "Pools: \(a)") }
    static func advVia(_ a: String) -> String { String(localized: "Via \(a)") }
    static var advL2Disabled: String {
        String(localized: "Announces nothing: L2 announcements are off in cilium-config (enable-l2-announcements).")
    }
    static var advNoLBIPs: String {
        String(localized: "Announces no LoadBalancer address: loadBalancerIPs is not set.")
    }
    static var advNoService: String { String(localized: "No LoadBalancer Service matches it.") }
    static func peerASN(_ a: String, _ b: String) -> String { String(localized: "AS \(a), local AS \(b)") }
    static var peerSessionsUnknown: String {
        String(localized: "Session state unknown: the speakers’ metrics could not be read.")
    }
    static var peerNoSession: String { String(localized: "No speaker reports a session with this peer.") }
    static func peerSessionUp(_ a: String) -> String { String(localized: "\(a): up") }
    static func peerSessionDown(_ a: String) -> String { String(localized: "\(a): down") }
    static var kubeVipNoRange: String { String(localized: "No address range: only the addresses Services ask for.") }
    static var kubeVipGlobal: String { String(localized: "Every namespace") }
    static func kubeVipLease(_ a: String, _ b: String) -> String { String(localized: "\(a) held by \(b)") }
    static func kubeVipLeaseRenewed(_ a: String, _ b: String, _ c: String) -> String {
        String(localized: "\(a) held by \(b), renewed \(c) ago")
    }
    static func servicePool(_ a: String) -> String { String(localized: "Pool \(a)") }
    static func serviceAnnouncedBy(_ a: String) -> String { String(localized: "Announced by \(a)") }
    static var servicesHintKubeVip: String {
        String(
            localized:
                "A load balancer has no address: check kube-vip’s kubevip ConfigMap (cidr-global, or a range for its namespace) and that kube-vip runs."
        )
    }
    static func checkupPendingCause(_ a: String, _ b: String) -> String {
        String(localized: "No address for \(a): \(b)")
    }
    static var checkupPoolUnadvertised: String { String(localized: "Not advertised") }
    static var checkupPoolUnadvertisedFix: String {
        String(
            localized:
                "MetalLB hands out this pool’s addresses but nothing announces them: add an L2Advertisement or a BGPAdvertisement that selects it."
        )
    }
    static var checkupCiliumPoolUnannouncedFix: String {
        String(
            localized:
                "Cilium hands out this pool’s addresses but no CiliumL2AnnouncementPolicy announces its Services, and BGP is off: add a policy with loadBalancerIPs that selects them."
        )
    }
    static var checkupConfigError: String { String(localized: "MetalLB refused its configuration") }
    static var checkupConfigErrorFix: String {
        String(
            localized:
                "Fix what the message names (overlapping pools, a bad peer…): until then MetalLB keeps its last good configuration."
        )
    }
    static func checkupBGPPeerDown(_ a: String) -> String { String(localized: "BGP session down with \(a)") }
    static var checkupBGPPeerDownFix: String {
        String(
            localized:
                "Check the router: its address and AS numbers, that it accepts this node, and that nothing filters TCP 179 between them."
        )
    }
}
