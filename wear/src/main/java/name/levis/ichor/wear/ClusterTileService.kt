package name.levis.ichor.wear

import android.content.res.Resources
import androidx.concurrent.futures.CallbackToFutureAdapter
import androidx.wear.protolayout.ColorBuilders.argb
import androidx.wear.protolayout.DimensionBuilders.dp
import androidx.wear.protolayout.DimensionBuilders.sp
import androidx.wear.protolayout.LayoutElementBuilders
import androidx.wear.protolayout.LayoutElementBuilders.Box
import androidx.wear.protolayout.LayoutElementBuilders.Column
import androidx.wear.protolayout.LayoutElementBuilders.FontStyle
import androidx.wear.protolayout.LayoutElementBuilders.LayoutElement
import androidx.wear.protolayout.LayoutElementBuilders.Row
import androidx.wear.protolayout.LayoutElementBuilders.Spacer
import androidx.wear.protolayout.LayoutElementBuilders.Text
import androidx.wear.protolayout.ModifiersBuilders.Background
import androidx.wear.protolayout.ModifiersBuilders.Corner
import androidx.wear.protolayout.ModifiersBuilders.Modifiers
import androidx.wear.protolayout.ModifiersBuilders.Semantics
import androidx.wear.protolayout.ResourceBuilders
import androidx.wear.protolayout.TimelineBuilders.Timeline
import androidx.wear.tiles.RequestBuilders
import androidx.wear.tiles.TileBuilders
import androidx.wear.tiles.TileService
import com.google.common.util.concurrent.ListenableFuture
import name.levis.ichor.widget.WearCluster
import name.levis.ichor.widget.isStaleAt
import name.levis.ichor.widget.statusItems
import name.levis.ichor.widget.toneOf

private const val RESOURCES_VERSION = "1"

/** Redrawn this often without a new payload, so a check turns stale and its age moves on. */
private const val FRESHNESS_MS = 15 * 60 * 1000L

/**
 * The cluster on screen on the phone: its name, the ready count in the health colour and the
 * widget's two status items; stale after an hour without a check, then the check's age instead.
 */
class ClusterTileService : TileService() {

    override fun onTileRequest(requestParams: RequestBuilders.TileRequest): ListenableFuture<TileBuilders.Tile> =
        CallbackToFutureAdapter.getFuture { completer ->
            WatchSnapshotStore.fetchIfMissing(this) {
                completer.set(tile(resources, WatchSnapshotStore.active(this), System.currentTimeMillis()))
            }
            "ClusterTile"
        }

    override fun onTileResourcesRequest(requestParams: RequestBuilders.ResourcesRequest): ListenableFuture<ResourceBuilders.Resources> =
        CallbackToFutureAdapter.getFuture { completer ->
            completer.set(ResourceBuilders.Resources.Builder().setVersion(RESOURCES_VERSION).build())
            "ClusterTileResources"
        }
}

private fun tile(res: Resources, cluster: WearCluster?, now: Long): TileBuilders.Tile =
    TileBuilders.Tile.Builder()
        .setResourcesVersion(RESOURCES_VERSION)
        .setFreshnessIntervalMillis(FRESHNESS_MS)
        .setTileTimeline(Timeline.fromLayoutElement(layout(res, cluster, now)))
        .build()

private fun layout(res: Resources, c: WearCluster?, now: Long): LayoutElement {
    val column = Column.Builder().setHorizontalAlignment(LayoutElementBuilders.HORIZONTAL_ALIGN_CENTER)
    if (c == null) {
        column.addContent(text(res.getString(R.string.wear_app_name), 14f, WatchColors.SECONDARY))
            .addContent(gap(6f))
            .addContent(text(res.getString(R.string.wear_no_data), 13f, WatchColors.PRIMARY, maxLines = 3))
        return column.build()
    }
    val stale = isStaleAt(c.takenAt, now)
    val tone = toneOf(c.counts, c.takenAt, now)
    val count = res.getString(R.string.wear_ready_count, c.counts.ready, c.counts.total)
    column.addContent(text(c.label, 14f, WatchColors.SECONDARY))
        .addContent(gap(2f))
        .addContent(text(count, 28f, if (stale) WatchColors.DIMMED else WatchColors.of(tone), bold = true))
        .addContent(gap(6f))
    if (stale) {
        column.addContent(text(staleText(res, c.takenAt, now), 12f, WatchColors.SECONDARY, maxLines = 2))
    } else {
        statusItems(c.counts).forEach { item ->
            column.addContent(chip(statusText(res, item), WatchColors.of(toneOf(item))))
        }
    }
    val description = listOf(c.label, count, toneText(res, tone)).joinToString(", ")
    return Box.Builder()
        .setModifiers(Modifiers.Builder().setSemantics(Semantics.Builder().setContentDescription(description).build()).build())
        .addContent(column.build())
        .build()
}

/** A coloured dot and its label, like a widget status item. */
private fun chip(label: String, color: Int): LayoutElement =
    Row.Builder()
        .setVerticalAlignment(LayoutElementBuilders.VERTICAL_ALIGN_CENTER)
        .addContent(
            Box.Builder()
                .setWidth(dp(8f))
                .setHeight(dp(8f))
                .setModifiers(
                    Modifiers.Builder()
                        .setBackground(Background.Builder().setColor(argb(color)).setCorner(Corner.Builder().setRadius(dp(4f)).build()).build())
                        .build(),
                )
                .build(),
        )
        .addContent(Spacer.Builder().setWidth(dp(6f)).build())
        .addContent(text(label, 13f, WatchColors.PRIMARY))
        .build()

internal fun text(value: String, size: Float, color: Int, bold: Boolean = false, maxLines: Int = 1): LayoutElement =
    Text.Builder()
        .setText(value)
        .setMaxLines(maxLines)
        .setFontStyle(
            FontStyle.Builder()
                .setSize(sp(size))
                .setColor(argb(color))
                .setWeight(if (bold) LayoutElementBuilders.FONT_WEIGHT_BOLD else LayoutElementBuilders.FONT_WEIGHT_NORMAL)
                .build(),
        )
        .build()

internal fun gap(height: Float): LayoutElement = Spacer.Builder().setHeight(dp(height)).build()
