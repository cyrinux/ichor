package name.levis.ichor.ui.upgrade

import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.K8sUpgradeRunState
import name.levis.ichor.ui.RunService

/**
 * Keeps the app running while Kubernetes is upgraded node after node, like the upgrade
 * service: each control plane's static pods and each kubelet are waited for.
 */
class K8sUpgradeService : RunService<K8sUpgradeRunState>() {
    override val channelId = "talos-upgrades"
    override val progressId = 0x0b7a
    override val resultId = 0x0b7b
    override val channelName = R.string.upgrade_channel_name
    override val channelDescription = R.string.upgrade_channel_desc
    override val publicTitle = R.string.k8s_upgrade_notification_public
    override val resultPublicTitle = R.string.k8s_upgrade_notification_result_public

    override val current get() = (application as TalosApp).k8sUpgradeManager.current

    override fun progressText(res: Context, run: K8sUpgradeRunState): Pair<String, String> {
        val title = res.getString(if (run.dryRun) R.string.k8s_upgrade_dry_run_title else R.string.k8s_upgrade_notification_title, run.version)
        val p = run.latest ?: return title to res.getString(R.string.cluster_upgrade_starting)
        return title to k8sStepText(res, p)
    }

    override fun doneText(res: Context, run: K8sUpgradeRunState): Pair<String, String> =
        if (run.dryRun) {
            res.getString(R.string.k8s_upgrade_dry_run_done, run.version) to res.getString(R.string.k8s_upgrade_dry_run_done_detail)
        } else {
            res.getString(R.string.k8s_upgrade_notification_done, run.version) to run.hostname
        }

    override fun failedTitle(res: Context, run: K8sUpgradeRunState): String =
        res.getString(R.string.k8s_upgrade_notification_failed, run.version)

    companion object {
        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, K8sUpgradeService::class.java))
        }
    }
}

/** "Kubernetes upgrade 4/9: cp-2 scheduler", or what it does between nodes. */
fun k8sStepText(res: Context, p: name.levis.ichor.model.K8sUpgradeProgress): String = when {
    p.phase == name.levis.ichor.model.K8sUpgradeProgress.DONE -> res.getString(R.string.upgrade_phase_done)
    p.phase == name.levis.ichor.model.K8sUpgradeProgress.PROXY -> res.getString(R.string.k8s_upgrade_proxy)
    p.total > 0 -> res.getString(
        R.string.k8s_upgrade_step,
        (p.index + 1).coerceAtMost(p.total),
        p.total,
        p.name,
        p.component.ifEmpty { p.phase },
    )
    else -> p.message
}
