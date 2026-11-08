package name.levis.ichor.model

/**
 * The priority class, priority and QoS class most workloads of a list share: a value held by
 * more than half of them (null: no such value). Identical tags carry no information, so a row
 * shows only the values that differ from these.
 */
data class CapacityMajority(
    val priorityClass: String? = null,
    val priority: Int? = null,
    val qos: QosClass? = null,
)

/** Which of a workload's priority class, priority and QoS class its row shows. */
data class WorkloadTraits(val priorityClass: Boolean, val priority: Boolean, val qos: Boolean) {
    val any: Boolean get() = priorityClass || priority || qos
}

/** The class a row names: "" for a workload without a priority class. */
private val CapacityWorkload.classKey: String get() = if (noPriorityClass) "" else priorityClassName

fun List<CapacityWorkload>.majority(): CapacityMajority = CapacityMajority(
    priorityClass = majorityOf { it.classKey },
    priority = majorityOf { it.priority },
    qos = majorityOf { it.qosClass },
)

/** The traits of this workload that differ from [m]; an unknown QoS class is never shown. */
fun CapacityWorkload.traitsBeside(m: CapacityMajority): WorkloadTraits = WorkloadTraits(
    priorityClass = classKey != m.priorityClass && (noPriorityClass || priorityClassName.isNotEmpty()),
    priority = priority != m.priority,
    qos = qosClass != QosClass.UNKNOWN && qosClass != m.qos,
)

/** A node's roles as one line, control-plane first ("control-plane · storage, mail"); null without roles. */
fun nodeRolesLine(roles: List<String>): String? {
    if (roles.isEmpty()) return null
    val others = roles.filter { it != CONTROL_PLANE }.joinToString(", ")
    return when {
        CONTROL_PLANE !in roles -> others
        others.isEmpty() -> CONTROL_PLANE
        else -> "$CONTROL_PLANE · $others"
    }
}

/** Overcommitted nodes first, otherwise in the order given. */
fun List<CapacityNode>.overcommittedFirst(): List<CapacityNode> = sortedByDescending { it.overcommitted }

private const val CONTROL_PLANE = "control-plane"

private fun <T, K> List<T>.majorityOf(key: (T) -> K): K? {
    if (size < 2) return null
    val (value, count) = groupingBy(key).eachCount().maxByOrNull { it.value } ?: return null
    return if (count * 2 > size) value else null
}
