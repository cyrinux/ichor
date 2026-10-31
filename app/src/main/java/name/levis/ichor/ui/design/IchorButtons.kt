package name.levis.ichor.ui.design

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.RowScope
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonColors
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.ButtonElevation
import androidx.compose.material3.ExperimentalMaterial3ExpressiveApi
import androidx.compose.material3.IconButton
import androidx.compose.material3.IconButtonColors
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Shape
import name.levis.ichor.data.UiStyle

/** Expressive buttons change shape when pressed; one given its own shape keeps it. */
@Composable
private fun morphs(shape: Shape, default: Shape): Boolean = LocalUiStyle.current == UiStyle.EXPRESSIVE && shape == default

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun IchorButton(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    shape: Shape = ButtonDefaults.shape,
    colors: ButtonColors = ButtonDefaults.buttonColors(),
    elevation: ButtonElevation? = ButtonDefaults.buttonElevation(),
    border: BorderStroke? = null,
    contentPadding: PaddingValues = ButtonDefaults.ContentPadding,
    interactionSource: MutableInteractionSource? = null,
    content: @Composable RowScope.() -> Unit,
) {
    if (morphs(shape, ButtonDefaults.shape)) {
        Button(onClick, ButtonDefaults.shapes(), modifier, enabled, colors, elevation, border, contentPadding, interactionSource, content)
    } else {
        Button(onClick, modifier, enabled, shape, colors, elevation, border, contentPadding, interactionSource, content)
    }
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun IchorTextButton(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    shape: Shape = ButtonDefaults.textShape,
    colors: ButtonColors = ButtonDefaults.textButtonColors(),
    elevation: ButtonElevation? = null,
    border: BorderStroke? = null,
    contentPadding: PaddingValues = ButtonDefaults.TextButtonContentPadding,
    interactionSource: MutableInteractionSource? = null,
    content: @Composable RowScope.() -> Unit,
) {
    if (morphs(shape, ButtonDefaults.textShape)) {
        TextButton(onClick, ButtonDefaults.shapes(), modifier, enabled, colors, elevation, border, contentPadding, interactionSource, content)
    } else {
        TextButton(onClick, modifier, enabled, shape, colors, elevation, border, contentPadding, interactionSource, content)
    }
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun IchorOutlinedButton(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    shape: Shape = ButtonDefaults.outlinedShape,
    colors: ButtonColors = ButtonDefaults.outlinedButtonColors(),
    elevation: ButtonElevation? = null,
    border: BorderStroke? = ButtonDefaults.outlinedButtonBorder(enabled),
    contentPadding: PaddingValues = ButtonDefaults.ContentPadding,
    interactionSource: MutableInteractionSource? = null,
    content: @Composable RowScope.() -> Unit,
) {
    if (morphs(shape, ButtonDefaults.outlinedShape)) {
        OutlinedButton(onClick, ButtonDefaults.shapes(), modifier, enabled, colors, elevation, border, contentPadding, interactionSource, content)
    } else {
        OutlinedButton(onClick, modifier, enabled, shape, colors, elevation, border, contentPadding, interactionSource, content)
    }
}

@Composable
fun IchorIconButton(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    colors: IconButtonColors = IconButtonDefaults.iconButtonColors(),
    interactionSource: MutableInteractionSource? = null,
    shape: Shape = IconButtonDefaults.standardShape,
    content: @Composable () -> Unit,
) = IconButton(onClick, modifier, enabled, colors, interactionSource, shape, content)
