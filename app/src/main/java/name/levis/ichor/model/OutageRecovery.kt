package name.levis.ichor.model

/** The first thing to try when no node answers. */
enum class RecoveryAction { VPN_SETTINGS, SIGN_IN, REPLACE_CONFIG, RETRY }

/** What the primary action will and will not do, said next to it. */
enum class RecoveryExplanation { OMNI_SESSION, REPLACE_CONFIG }

/** What the outage notice and banner offer, from why no node answered. */
data class OutageRecovery(
    val primary: RecoveryAction,
    /** Retry beside the primary action; false when Retry is the primary one. */
    val retrySecondary: Boolean,
    val scan: Boolean,
    val editEndpoints: Boolean,
    val explanation: RecoveryExplanation?,
)

/**
 * Retry cannot repair refused credentials: they get a way to replace them. A refusal means a node
 * answered, so its address is right, unless the certificate names another host. The demo has no
 * credentials or endpoints to change, and a masked screen shows no endpoints.
 */
fun outageRecovery(outage: ClusterOutage, omni: Boolean, demo: Boolean, masked: Boolean): OutageRecovery {
    val editable = !demo && !masked
    return when (outage.cause) {
        OutageCause.NETWORK -> OutageRecovery(RecoveryAction.VPN_SETTINGS, retrySecondary = true, scan = !demo, editEndpoints = editable, explanation = null)
        OutageCause.CREDENTIALS -> {
            val wrongAddress = editable && outage.errors.any { WRONG_HOST_CERTIFICATE in it }
            when {
                demo -> OutageRecovery(RecoveryAction.RETRY, retrySecondary = false, scan = false, editEndpoints = false, explanation = null)
                omni -> OutageRecovery(RecoveryAction.SIGN_IN, retrySecondary = true, scan = false, editEndpoints = wrongAddress, explanation = RecoveryExplanation.OMNI_SESSION)
                else -> OutageRecovery(RecoveryAction.REPLACE_CONFIG, retrySecondary = true, scan = false, editEndpoints = wrongAddress, explanation = RecoveryExplanation.REPLACE_CONFIG)
            }
        }
        OutageCause.OTHER -> OutageRecovery(RecoveryAction.RETRY, retrySecondary = false, scan = false, editEndpoints = editable, explanation = null)
    }
}

/** How Go's x509 error names a certificate for another host: the endpoint is the thing to change. */
private const val WRONG_HOST_CERTIFICATE = "certificate is valid for"
