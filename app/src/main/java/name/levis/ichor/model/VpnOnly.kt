package name.levis.ichor.model

/** The clusters of [saved] (by fingerprint) still among [fingerprints]: a removed cluster's setting goes with it. */
fun keepVpnOnly(saved: Set<String>, fingerprints: List<String>): Set<String> =
    saved intersect fingerprints.filter { it.isNotBlank() }.toSet()

/**
 * Whether a call to the cluster [fingerprint] is held back: it is set to be reached over a
 * VPN only ([vpnOnly]) and none is up, so the call could only time out.
 */
fun heldBackForVpn(vpnOnly: Set<String>, fingerprint: String?, vpnUp: Boolean): Boolean =
    !vpnUp && !fingerprint.isNullOrBlank() && fingerprint in vpnOnly
