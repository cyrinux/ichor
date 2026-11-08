package name.levis.ichor.wear

import android.content.res.Resources
import androidx.concurrent.futures.CallbackToFutureAdapter
import androidx.wear.protolayout.LayoutElementBuilders
import androidx.wear.protolayout.LayoutElementBuilders.Box
import androidx.wear.protolayout.LayoutElementBuilders.Column
import androidx.wear.protolayout.LayoutElementBuilders.LayoutElement
import androidx.wear.protolayout.ModifiersBuilders.Modifiers
import androidx.wear.protolayout.ModifiersBuilders.Semantics
import androidx.wear.protolayout.ResourceBuilders
import androidx.wear.protolayout.TimelineBuilders.Timeline
import androidx.wear.tiles.RequestBuilders
import androidx.wear.tiles.TileBuilders
import androidx.wear.tiles.TileService
import com.google.common.util.concurrent.ListenableFuture
import name.levis.ichor.widget.FleetTally
import name.levis.ichor.widget.isStaleAt
import name.levis.ichor.widget.toneOf

private const val RESOURCES_VERSION = "1"

/** Redrawn this often without a new payload, so the tally turns stale and its age moves on. */
private const val FRESHNESS_MS = 15 * 60 * 1000L

/**
 * Every cluster at once, from the phone's last check: "2/6" in the worst tone's colour and
 * "need attention" under it, or "All 6 fine"; stale an hour after the newest check, then that
 * check's age instead. Counts only: no cluster name reaches this tile.
 */
class FleetTileService : TileService() {

    override fun onTileRequest(requestParams: RequestBuilders.TileRequest): ListenableFuture<TileBuilders.Tile> =
        CallbackToFutureAdapter.getFuture { completer ->
            WatchSnapshotStore.fetchIfMissing(this) {
                completer.set(fleetTile(resources, WatchSnapshotStore.fleet(this), System.currentTimeMillis()))
            }
            "FleetTile"
        }

    override fun onTileResourcesRequest(requestParams: RequestBuilders.ResourcesRequest): ListenableFuture<ResourceBuilders.Resources> =
        CallbackToFutureAdapter.getFuture { completer ->
            completer.set(ResourceBuilders.Resources.Builder().setVersion(RESOURCES_VERSION).build())
            "FleetTileResources"
        }
}

private fun fleetTile(res: Resources, fleet: FleetTally?, now: Long): TileBuilders.Tile =
    TileBuilders.Tile.Builder()
        .setResourcesVersion(RESOURCES_VERSION)
        .setFreshnessIntervalMillis(FRESHNESS_MS)
        .setTileTimeline(Timeline.fromLayoutElement(fleetLayout(res, fleet, now)))
        .build()

private fun fleetLayout(res: Resources, t: FleetTally?, now: Long): LayoutElement {
    val column = Column.Builder().setHorizontalAlignment(LayoutElementBuilders.HORIZONTAL_ALIGN_CENTER)
        .addContent(text(res.getString(R.string.wear_fleet_title), 14f, WatchColors.SECONDARY))
    if (t == null || t.total == 0) {
        column.addContent(gap(6f)).addContent(text(res.getString(R.string.wear_no_data), 13f, WatchColors.PRIMARY, maxLines = 3))
        return column.build()
    }
    val stale = isStaleAt(t.takenAt.takeIf { it > 0 }, now)
    val count = res.getString(R.string.wear_fleet_short, t.needAttention, t.total)
    column.addContent(gap(2f))
        .addContent(text(count, 28f, if (stale) WatchColors.DIMMED else WatchColors.of(toneOf(t, now)), bold = true))
        .addContent(gap(6f))
    val detail = if (stale) fleetStaleText(res, t, now) else fleetDetail(res, t)
    column.addContent(text(detail, 12f, WatchColors.SECONDARY, maxLines = 2))
    return Box.Builder()
        .setModifiers(Modifiers.Builder().setSemantics(Semantics.Builder().setContentDescription(fleetDescription(res, t, now)).build()).build())
        .addContent(column.build())
        .build()
}
