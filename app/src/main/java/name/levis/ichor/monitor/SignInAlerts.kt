package name.levis.ichor.monitor

import kotlinx.coroutines.CancellationException
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.SignInRenewal
import name.levis.ichor.model.isKube

/** The key of the "sign in again" alert (one per cluster). */
const val SIGN_IN_KEY = "signin"

/**
 * What a run's renewals tell: [alerts], one "sign in again" per watched cluster whose session
 * ended and was not told yet; [notified], the fingerprints told so far, to keep for the next run.
 */
data class SignInAlertPlan(val alerts: List<ClusterAlerts>, val notified: Set<String>)

/**
 * Plans the "sign in again" alerts from [renewals] for the watched [contexts], [notified] being
 * the fingerprints already told. A cluster is told once until it is signed in again (a renewal
 * that succeeds forgets it); one that could not be renewed for another reason (offline) keeps
 * what it had. [label] names a cluster as the app shows it.
 */
fun signInAlertPlan(
    renewals: List<SignInRenewal>,
    contexts: List<ContextSummary>,
    notified: Set<String>,
    label: (ContextSummary) -> String,
): SignInAlertPlan {
    val alerts = mutableListOf<ClusterAlerts>()
    val next = notified.toMutableSet()
    renewals.forEach { renewal ->
        val context = contexts.firstOrNull { it.name == renewal.context && it.isKube == (renewal.kind == SignInRenewal.KIND_KUBE) }
            ?: return@forEach
        val fingerprint = context.fingerprint.takeIf { it.isNotBlank() } ?: return@forEach
        when {
            renewal.signedIn -> next -= fingerprint
            renewal.needsUser && fingerprint !in notified -> {
                next += fingerprint
                alerts += ClusterAlerts(context, listOf(signInAlert(label(context))))
            }
        }
    }
    return SignInAlertPlan(alerts, next)
}

/** "Sign in again: [cluster] needs you to sign in again before its next check." */
fun signInAlert(cluster: String): Alert = Alert(key = SIGN_IN_KEY, kind = AlertKind.SIGN_IN_REQUIRED, problem = true, subject = cluster)

/**
 * Renews every sign-in before a background check, then plans the "sign in again" alerts of the
 * watched [contexts]. A renewal that fails as a whole (core error) tells nothing.
 */
suspend fun renewSignIns(app: TalosApp, contexts: List<ContextSummary>, label: (ContextSummary) -> String): SignInAlertPlan {
    val notified = app.monitorStore.signInNotified.value
    val renewals = try {
        app.kubeAuthRepository.renewAll().also { app.googleTokenRenewer.renewExpired(it) }
    } catch (e: CancellationException) {
        throw e
    } catch (_: Exception) {
        return SignInAlertPlan(emptyList(), notified)
    }
    return signInAlertPlan(renewals, contexts, notified, label)
}
