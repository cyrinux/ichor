package name.levis.ichor.ui.etcd

import androidx.annotation.StringRes
import name.levis.ichor.R

/** The parts of the etcd screen its section rail jumps to. */
enum class EtcdPart(val key: String, @StringRes val title: Int) {
    SNAPSHOT("snapshot", R.string.etcd_snapshot_title),
    FIX("fix", R.string.etcd_fix_title),
    RECOVER("recover", R.string.etcd_recover_title),
    ALARMS("alarms", R.string.etcd_section_alarms),
    MEMBERS("members", R.string.etcd_section_members_short),
    UNPROBED("unprobed", R.string.etcd_section_unprobed),
}

/**
 * Which optional items the etcd list shows, in its order: everything that moves the position of
 * a section. `EtcdContent` builds it from the same values that decide its items.
 */
data class EtcdListShape(
    val error: Boolean = false,
    val defrag: Boolean = false,
    val memberPanel: Boolean = false,
    val snapshot: Boolean = false,
    val fixPanel: Boolean = false,
    val recoverPanel: Boolean = false,
    val recoverButton: Boolean = false,
    val alarmsError: Boolean = false,
    val alarms: Int = 0,
    val fixButton: Boolean = false,
    val disarmButton: Boolean = false,
    val disarmFailed: Boolean = false,
    val lagging: Boolean = false,
    val statuses: Int = 0,
    val unprobed: Int = 0,
)

private fun Boolean.count() = if (this) 1 else 0

/** Each section present with the `LazyColumn` index of its first item, in display order. */
fun etcdSections(shape: EtcdListShape): List<Pair<EtcdPart, Int>> = buildList {
    var index = shape.error.count() + shape.defrag.count() + shape.memberPanel.count()
    if (shape.snapshot) add(EtcdPart.SNAPSHOT to index++)
    if (shape.fixPanel) add(EtcdPart.FIX to index++)
    if (shape.recoverPanel) add(EtcdPart.RECOVER to index++)
    index += shape.recoverButton.count()
    val alarmsAt = index
    if (shape.alarmsError) index += 2
    if (shape.alarms > 0) index += 1 + shape.alarms + shape.fixButton.count() + shape.disarmButton.count()
    if (index > alarmsAt) add(EtcdPart.ALARMS to alarmsAt)
    index += shape.disarmFailed.count()
    add(EtcdPart.MEMBERS to index)
    index += 1 + shape.lagging.count() + shape.statuses
    if (shape.unprobed > 0) add(EtcdPart.UNPROBED to index)
}
