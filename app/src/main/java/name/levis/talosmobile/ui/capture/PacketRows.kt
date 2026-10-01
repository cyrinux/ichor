package name.levis.talosmobile.ui.capture

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ArrowDownward
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import name.levis.talosmobile.R
import name.levis.talosmobile.model.PacketSummary
import name.levis.talosmobile.model.ProtoKind
import name.levis.talosmobile.model.protoKind
import name.levis.talosmobile.model.relativeTime
import name.levis.talosmobile.ui.theme.LocalChartColors
import name.levis.talosmobile.ui.theme.LocalStatusColors

private val PacketFontSize = 11.sp
private val PacketLineHeight = 14.sp

/** Colours of packet rows, resolved once per theme (same spirit as the log rows). */
@Immutable
data class PacketPalette(val text: Color, val dim: Color, val proto: Map<ProtoKind, Color>)

@Composable
fun rememberPacketPalette(): PacketPalette {
    val scheme = MaterialTheme.colorScheme
    val status = LocalStatusColors.current
    val chart = LocalChartColors.current
    return remember(scheme, status, chart) {
        PacketPalette(
            text = scheme.onSurface,
            dim = scheme.onSurfaceVariant.copy(alpha = 0.7f),
            proto = mapOf(
                ProtoKind.TCP to chart.first,
                ProtoKind.UDP to chart.second,
                ProtoKind.ICMP to status.warn,
                ProtoKind.DNS to status.ok,
                ProtoKind.ARP to scheme.tertiary,
                ProtoKind.TLS to scheme.primary,
                ProtoKind.OTHER to scheme.onSurfaceVariant,
            ),
        )
    }
}

/** `+1.234 TCP src → dst len info`, the protocol tag coloured. */
fun packetRowText(p: PacketSummary, start: Long, palette: PacketPalette): AnnotatedString = buildAnnotatedString {
    withStyle(SpanStyle(color = palette.dim)) { append(relativeTime(p.ts, start)) }
    append(' ')
    val color = palette.proto.getValue(protoKind(p.proto))
    withStyle(SpanStyle(color = color, fontWeight = FontWeight.Bold)) { append(p.proto.ifEmpty { "?" }) }
    append(' ')
    withStyle(SpanStyle(color = palette.text)) { append("${p.src} → ${p.dst}") }
    withStyle(SpanStyle(color = palette.dim)) { append(" ${p.len}") }
    if (p.info.isNotEmpty()) {
        append(' ')
        withStyle(SpanStyle(color = palette.text)) { append(p.info) }
    }
}

@Composable
private fun PacketRow(p: PacketSummary, start: Long, palette: PacketPalette, onClick: (() -> Unit)?) {
    val text = remember(p, start, palette) { packetRowText(p, start, palette) }
    Text(
        text,
        fontFamily = FontFamily.Monospace,
        fontSize = PacketFontSize,
        lineHeight = PacketLineHeight,
        modifier = Modifier
            .fillMaxWidth()
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(vertical = 2.dp),
    )
}

/**
 * The packet list. With [live] it sticks to the newest packet until the user scrolls up, and
 * offers "Jump to latest" then. [onPacket] null: rows are not clickable. [footer] goes last.
 */
@Composable
fun PacketList(
    packets: List<PacketSummary>,
    start: Long,
    live: Boolean,
    empty: String,
    onPacket: ((PacketSummary) -> Unit)?,
    modifier: Modifier = Modifier,
    footer: (LazyListScope.() -> Unit)? = null,
) {
    if (packets.isEmpty()) {
        Text(empty, modifier = modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
        return
    }
    val palette = rememberPacketPalette()
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()
    var stickToEnd by remember { mutableStateOf(live) }
    LaunchedEffect(listState) {
        snapshotFlow { listState.isScrollInProgress to listState.canScrollForward }
            .collect { (scrolling, canScrollForward) -> if (scrolling && live) stickToEnd = !canScrollForward }
    }
    LaunchedEffect(packets.size, live) { if (live && stickToEnd) listState.scrollToItem(packets.lastIndex) }

    Box(modifier.fillMaxSize()) {
        LazyColumn(
            state = listState,
            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 8.dp),
            modifier = Modifier.fillMaxSize(),
        ) {
            items(packets, key = { it.n }) { p ->
                PacketRow(p, start, palette, onPacket?.let { { it(p) } })
            }
            footer?.invoke(this)
        }
        if (live && !stickToEnd) {
            ExtendedFloatingActionButton(
                onClick = {
                    stickToEnd = true
                    scope.launch { listState.scrollToItem(packets.lastIndex) }
                },
                icon = { Icon(Icons.Outlined.ArrowDownward, contentDescription = null) },
                text = { Text(stringResource(R.string.logs_jump_latest)) },
                modifier = Modifier.align(Alignment.BottomCenter).padding(16.dp),
            )
        }
    }
}
