package name.levis.ichor.wear

import android.content.Context
import androidx.wear.protolayout.ActionBuilders
import androidx.wear.protolayout.DimensionBuilders.expand
import androidx.wear.protolayout.LayoutElementBuilders.Box
import androidx.wear.protolayout.LayoutElementBuilders.LayoutElement
import androidx.wear.protolayout.ModifiersBuilders.Clickable
import androidx.wear.protolayout.ModifiersBuilders.Modifiers

/**
 * [content] as a tile's root that opens [WatchActivity] when tapped: on the cluster at [cluster]
 * in the phone's list (the cluster tile's), or on the list when null (the fleet tile's).
 */
internal fun opensApp(context: Context, content: LayoutElement, cluster: Int? = null): LayoutElement {
    val activity = ActionBuilders.AndroidActivity.Builder()
        .setPackageName(context.packageName)
        .setClassName(WatchActivity::class.java.name)
    if (cluster != null) {
        activity.addKeyToExtraMapping(EXTRA_CLUSTER, ActionBuilders.AndroidIntExtra.Builder().setValue(cluster).build())
    }
    val click = Clickable.Builder()
        .setId(if (cluster == null) "open-list" else "open-cluster")
        .setOnClick(ActionBuilders.LaunchAction.Builder().setAndroidActivity(activity.build()).build())
        .build()
    return Box.Builder()
        .setWidth(expand())
        .setHeight(expand())
        .setModifiers(Modifiers.Builder().setClickable(click).build())
        .addContent(content)
        .build()
}
