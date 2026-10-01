package name.levis.talosmobile.ui.capture

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.CancellationException
import name.levis.talosmobile.R
import name.levis.talosmobile.data.CaptureRepository
import name.levis.talosmobile.model.PacketDetail
import name.levis.talosmobile.model.PacketLayer
import name.levis.talosmobile.model.PacketSummary
import name.levis.talosmobile.ui.UiState
import name.levis.talosmobile.ui.asString
import name.levis.talosmobile.ui.theme.LocalStatusColors
import name.levis.talosmobile.ui.uiText
import java.io.File

/** Layers of one packet as expandable sections (all open at first), then its bytes. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PacketDetailSheet(captures: CaptureRepository, file: File, packet: PacketSummary, onDismiss: () -> Unit) {
    val state by produceState<UiState<PacketDetail>>(UiState.Loading, file, packet.n) {
        value = try {
            UiState.Loaded(captures.detail(file, packet.n))
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            UiState.Failed(e.uiText())
        }
    }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Text(
            stringResource(R.string.capture_detail_title, packet.n + 1),
            style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.padding(horizontal = 16.dp),
        )
        Text(
            "${packet.proto} ${packet.src} → ${packet.dst}",
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
        )
        when (val s = state) {
            UiState.Loading -> CircularProgressIndicator(Modifier.align(Alignment.CenterHorizontally).padding(32.dp))
            is UiState.Failed -> Text(
                s.message.asString(),
                color = LocalStatusColors.current.bad,
                modifier = Modifier.padding(16.dp),
            )
            is UiState.Loaded -> DetailContent(s.data)
        }
    }
}

@Composable
private fun DetailContent(detail: PacketDetail) {
    var collapsed by remember { mutableStateOf(emptySet<Int>()) }
    LazyColumn(contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 32.dp)) {
        detail.layers.forEachIndexed { index, layer ->
            item(key = "layer-$index") {
                LayerSection(layer, expanded = index !in collapsed) {
                    collapsed = if (index in collapsed) collapsed - index else collapsed + index
                }
            }
        }
        if (detail.hex.isNotEmpty()) {
            item(key = "hex") {
                Column(Modifier.padding(top = 12.dp)) {
                    Text(stringResource(R.string.capture_detail_bytes), style = MaterialTheme.typography.titleSmall)
                    SelectionContainer {
                        Text(
                            detail.hex,
                            fontFamily = FontFamily.Monospace,
                            fontSize = 11.sp,
                            lineHeight = 14.sp,
                            softWrap = false,
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(top = 4.dp)
                                .background(MaterialTheme.colorScheme.surfaceContainerHigh, RoundedCornerShape(4.dp))
                                .horizontalScroll(rememberScrollState())
                                .padding(8.dp),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun LayerSection(layer: PacketLayer, expanded: Boolean, onToggle: () -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier.fillMaxWidth().clickable(onClick = onToggle).padding(vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(layer.name, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
            Icon(if (expanded) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null)
        }
        if (expanded) {
            SelectionContainer {
                Column(verticalArrangement = Arrangement.spacedBy(2.dp), modifier = Modifier.padding(start = 8.dp, bottom = 4.dp)) {
                    layer.fields.forEach { f ->
                        Row {
                            Text(
                                f.k,
                                fontFamily = FontFamily.Monospace,
                                fontSize = 12.sp,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.weight(0.4f),
                            )
                            Text(f.v, fontFamily = FontFamily.Monospace, fontSize = 12.sp, modifier = Modifier.weight(0.6f))
                        }
                    }
                }
            }
        }
    }
}
