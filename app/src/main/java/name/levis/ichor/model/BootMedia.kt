package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/boot_media.go.

/**
 * What boots a new machine of the cluster into Talos (step 0 of adding a node): the template
 * member's version and Image Factory schematic, the factory's links, and the siderolabs/booter
 * command that serves a network boot from a laptop of the machine's network.
 */
@Serializable
data class BootMedia(
    val talosVersion: String = "",
    val schematic: String = "",
    /** "member": the template's own schematic; "default": the factory's, without extensions. */
    val schematicSource: String = "",
    val arch: String = "",
    val secureBoot: Boolean = false,
    val templateNode: String = "",
    val iso: String = "",
    val ipxe: String = "",
    val kernel: String = "",
    val initramfs: String = "",
    val cmdline: String = "",
    val booterDocker: String = "",
    val booterPodman: String = "",
    /** Null on a plain Talos cluster, and on Omni in privacy mode (see [warnings]). */
    val omni: BootMediaOmni? = null,
    val warnings: List<String> = emptyList(),
) {
    val fromMember: Boolean get() = schematicSource == "member"
}

/** How a new machine joins an Omni instance. [kernelArgs] carries the join token: a secret. */
@Serializable
data class BootMediaOmni(
    val kernelArgs: String = "",
    val booterDocker: String = "",
    val booterPodman: String = "",
)

/** The architectures the Image Factory builds media for. */
val bootArchitectures = listOf("amd64", "arm64")
