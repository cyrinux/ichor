package name.levis.ichor.monitor

import android.content.Context
import android.os.Build
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.OutOfQuotaPolicy
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import kotlinx.coroutines.CancellationException
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.AlertmanagerRepository
import name.levis.ichor.data.GoCall
import name.levis.ichor.data.OnCallRepository
import name.levis.ichor.data.sourceFor
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.model.AmAlerts
import name.levis.ichor.model.OnCallVerb
import name.levis.ichor.ui.userMessage

private const val KEY_CLUSTER = "cluster"
private const val KEY_ALERT = "alert"
private const val KEY_TITLE = "title"

/** Queues the acknowledge of the page behind the alert [key]: the receiver must return at once. */
fun enqueuePageAck(context: Context, cluster: String, key: String, title: String) {
    val request = OneTimeWorkRequestBuilder<AlertPageAckWorker>()
        .setInputData(workDataOf(KEY_CLUSTER to cluster, KEY_ALERT to key, KEY_TITLE to title))
        .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
    // A page is urgent. Only where this needs no foreground service: no new service type for it.
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) request.setExpedited(OutOfQuotaPolicy.RUN_AS_NON_EXPEDITED_WORK_REQUEST)
    WorkManager.getInstance(context).enqueueUniqueWork("alert-page-ack|$cluster|$key", ExistingWorkPolicy.KEEP, request.build())
}

/**
 * "Acknowledge page" from an alert's notification: finds the incident behind the alert and
 * acknowledges it, on the alert's cluster whichever is on screen, then says how it went in place of
 * the alert. The alert's repeats stop too.
 */
class AlertPageAckWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val app = applicationContext as TalosApp
        val res = AppLocale.wrap(applicationContext)
        val cluster = inputData.getString(KEY_CLUSTER).orEmpty()
        val key = inputData.getString(KEY_ALERT).orEmpty()
        val text = try {
            acknowledge(app, cluster, key.substringAfter(':'))
            app.alertAcks.acknowledge(cluster, key, System.currentTimeMillis())
            res.getString(R.string.oncall_page_acked)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            res.getString(R.string.oncall_page_ack_failed, e.userMessage())
        }
        postAlertNotice(applicationContext, cluster, key, inputData.getString(KEY_TITLE).orEmpty(), text, AlertChannel.CLUSTER)
        return Result.success()
    }

    private suspend fun acknowledge(app: TalosApp, cluster: String, fingerprint: String) {
        val stored = app.configRepository.config.value ?: app.configRepository.load()
        val context = stored?.summary?.contexts?.firstOrNull { monitorKeyOf(it) == cluster }
        val go = GoCall(
            app.configRepository, app.kubeServers, offline = null, pinned = context?.name ?: error("no cluster"),
            idle = app.maskSwitcher::awaitIdle, googleRenewer = app.googleTokenRenewer,
        )
        val alertmanager = AlertmanagerRepository(go)
        val onCall = OnCallRepository(go)
        val source = alertmanager.sourceFor(app.alertmanagerStore, cluster)
        val alerts = source?.let { alertmanager.alerts(it) } ?: AmAlerts()
        val config = app.onCallStore.read(cluster)
        val incident = onCall.incidents(config, alerts).incidentOf(fingerprint)
            ?: error(AppLocale.wrap(applicationContext).getString(R.string.oncall_page_no_incident))
        onCall.act(config, incident.id, OnCallVerb.ACKNOWLEDGE)
    }
}
