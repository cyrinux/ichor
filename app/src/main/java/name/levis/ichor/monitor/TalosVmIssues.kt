package name.levis.ichor.monitor

import name.levis.ichor.model.TalosVmProblem
import name.levis.ichor.model.TalosVmProblemReport

/** The problem kinds Go reports (go/ichorgo/talos_vm_problems.go). */
const val TALOS_VM_DOWN = "vmDown"
const val TALOS_VM_ERROR = "vmError"
const val TALOS_VM_DISK_FAILED = "vmDiskFailed"

/**
 * A Talos VM issue's stored value, split: "severity|kind|hostname|vm|detail" ([kind] one of
 * [TALOS_VM_DOWN], [TALOS_VM_ERROR], [TALOS_VM_DISK_FAILED]; [detail] the node's own words).
 */
data class TalosVmDetail(
    val severity: String,
    val kind: String,
    val hostname: String,
    val vm: String,
    val detail: String = "",
) {
    fun format(): String = listOf(severity, kind, hostname.clean(), vm.clean(), detail.clean()).joinToString("|")

    companion object {
        fun parse(value: String): TalosVmDetail {
            val parts = value.split('|', limit = 5)
            fun part(i: Int) = parts.getOrElse(i) { "" }
            return TalosVmDetail(part(0), part(1), part(2), part(3), part(4))
        }
    }
}

/** A VM that should run and does not is critical: it alerts at once. */
fun talosVmSeverity(@Suppress("UNUSED_PARAMETER") value: String): String = DATA_CRITICAL

/** The issue key of [problem]: "node|vm|name", stable across checks. */
fun talosVmIssueKey(problem: TalosVmProblem): String = "${problem.node}|vm|${problem.vm}"

/**
 * The Talos VM issues of [vms], from the problems Go judged (one per VM at most). A node that did
 * not answer, or whose VM statuses could not be read, keeps its [known] issues: unreadable, not
 * resolved. A node without the hypervisor (Talos older than 1.15) has none.
 */
fun talosVmIssuesOf(vms: TalosVmProblemReport, known: Map<String, String> = emptyMap()): Map<String, String> {
    val out = sortedMapOf<String, String>()
    vms.nodes.forEach { node ->
        if (node.error.isNotEmpty() || node.errors.containsKey(VM_SECTION_STATUSES)) {
            out += known.filterKeys { it.substringBefore('|') == node.node }
            return@forEach
        }
        val host = node.hostname.ifBlank { node.node }
        node.problems.forEach { p ->
            out[talosVmIssueKey(p)] = TalosVmDetail(DATA_CRITICAL, p.kind, host, p.vm, p.detail).format()
        }
    }
    return out
}

/** The Talos VM issues kept by [previous] when it is the same cluster ([context]) and that check read them. */
fun knownTalosVmIssues(previous: ClusterSnapshot?, context: String): Map<String, String> =
    previous?.takeIf { it.context == context && it.vmWatched && it.vmChecked }?.vmIssues.orEmpty()

/** The section of a node's VM report that holds the statuses (go/ichorgo/talos_vms.go). */
private const val VM_SECTION_STATUSES = "vms"

/** Without the separator, so a value always splits back the same way. */
private fun String.clean(): String = replace('|', '/')
