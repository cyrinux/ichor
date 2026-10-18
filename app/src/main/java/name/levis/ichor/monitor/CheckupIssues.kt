package name.levis.ichor.monitor

import name.levis.ichor.model.CheckupReport
import name.levis.ichor.model.alertIssues

/**
 * The checkup findings worth a notification ([alertIssues]: "section|kind|subject" → severity),
 * for a report where a section could not be read: that section keeps the issues [known] had for
 * it, so they neither clear falsely nor come back as new once it reads again. With
 * [gitopsWatched], the kinds the GitOps alert already raises are left to it.
 */
fun checkupIssuesWithGaps(report: CheckupReport, known: Map<String, String>, gitopsWatched: Boolean = false): Map<String, String> {
    val unread = report.sections.filter { it.error.isNotEmpty() }.map { "${it.id}|" }
    return report.alertIssues(gitopsWatched) + known.filterKeys { key -> unread.any { key.startsWith(it) } }
}

/**
 * The checkup issues a partial read may carry over: [previous]'s, only when it is the same cluster
 * ([context]) and that check watched and read them. Another cluster's issues must never leak in.
 */
fun knownCheckupIssues(previous: ClusterSnapshot?, context: String): Map<String, String> =
    previous?.takeIf { it.context == context && it.checkupWatched && it.checkupChecked }?.checkupIssues.orEmpty()
