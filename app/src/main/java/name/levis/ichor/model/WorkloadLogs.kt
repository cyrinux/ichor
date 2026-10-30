package name.levis.ichor.model

import java.time.Instant
import kotlinx.serialization.Serializable

/** The time ranges a log sheet offers, in minutes; 0 for no bound. */
val LOG_SINCE_CHOICES = listOf(15, 60, 360, 1440, 0)

/** The lines per pod the workload log sheet offers. */
val WORKLOAD_LOG_TAILS = listOf(200, 500, 2000)

/** How many colours the core hands out to a workload's pods ([WorkloadLogPod.color]). */
const val WORKLOAD_LOG_COLORS = 8

/** What a log sheet keeps: lines holding [text] (a Go regular expression when [regex]), from the last [sinceMinutes]. */
data class LogFilter(val text: String = "", val regex: Boolean = false, val sinceMinutes: Int = 0)

@Serializable
data class WorkloadLogPod(
    val name: String,
    val container: String = "",
    /** 0 until [WORKLOAD_LOG_COLORS]. */
    val color: Int = 0,
    val lines: Int = 0,
    /** Its tail or its size cap was reached. */
    val truncated: Boolean = false,
)

@Serializable
data class WorkloadLogLine(
    val pod: String,
    /** Unix milliseconds, 0 when unknown. */
    val time: Long = 0,
    val text: String = "",
    val level: String = "",
)

/** The logs of a workload's pods, merged by time: `KubeWorkloadLogs`. */
@Serializable
data class WorkloadLogs(
    val pods: List<WorkloadLogPod> = emptyList(),
    val lines: List<WorkloadLogLine> = emptyList(),
    val matched: Int = 0,
    val scanned: Int = 0,
    val truncated: Boolean = false,
    val sinceMinutes: Int = 0,
)

/** What tells the pods of a workload apart on a chip: "x2k4" for "api-7d9f-x2k4", "0" for "db-0". */
val WorkloadLogLine.podSuffix: String get() = pod.substringAfterLast('-')

/** The merged log as text, to copy or share: one `time pod text` per line. */
fun WorkloadLogs.asText(): String = lines.joinToString(separator = "\n", postfix = "\n") { line ->
    val time = if (line.time > 0) Instant.ofEpochMilli(line.time).toString() else "-"
    "$time ${line.pod} ${line.text}"
}
