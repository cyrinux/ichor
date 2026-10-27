package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class BootMediaTest {
    // Invented: no real cluster's schematic.
    private val schematic = "9d2c5e4b1a7f0836c4d2e1b9a0f7c6d5e4b3a2918f7e6d5c4b3a29180f7e6d5c"
    private val image = "https://factory.talos.dev/image/$schematic/v1.14.0"

    @Test
    fun decodesPlainTalosMedia() {
        val got = TalosJson.decodeFromString(
            BootMedia.serializer(),
            """{"talosVersion":"v1.14.0","schematic":"$schematic","schematicSource":"member","arch":"amd64","secureBoot":false,
               "templateNode":"192.0.2.10","iso":"$image/metal-amd64.iso",
               "ipxe":"https://pxe.factory.talos.dev/pxe/$schematic/v1.14.0/metal-amd64",
               "kernel":"$image/kernel-amd64","initramfs":"$image/initramfs-amd64.xz","cmdline":"$image/cmdline-metal-amd64",
               "booterDocker":"sudo docker run --rm --network host ghcr.io/siderolabs/booter:v0.4.0 --talos-version=v1.14.0 --schematic-id=$schematic",
               "booterPodman":"sudo podman run --rm --network host ghcr.io/siderolabs/booter:v0.4.0 --talos-version=v1.14.0 --schematic-id=$schematic",
               "omni":null,"warnings":[]}""",
        )
        assertTrue(got.fromMember)
        assertEquals("amd64", got.arch)
        assertTrue(got.iso.endsWith("/metal-amd64.iso"))
        assertTrue(got.booterPodman.startsWith("sudo podman "))
        assertNull(got.omni)
        assertEquals(emptyList<String>(), got.warnings)
    }

    @Test
    fun decodesOmniMediaWithTheDefaultSchematic() {
        val got = TalosJson.decodeFromString(
            BootMedia.serializer(),
            """{"talosVersion":"v1.14.0","schematic":"$schematic","schematicSource":"default","arch":"arm64","secureBoot":true,
               "templateNode":"cp-1","omni":{"kernelArgs":"siderolink.api=https://omni.example.com?jointoken=sample",
               "booterDocker":"sudo docker run …","booterPodman":"sudo podman run …"},
               "warnings":["cp-1 installs registry.example.com/installer:v1.14.0, not an Image Factory image"],"unknown":1}""",
        )
        assertFalse(got.fromMember)
        assertTrue(got.secureBoot)
        assertEquals("siderolink.api=https://omni.example.com?jointoken=sample", got.omni?.kernelArgs)
        assertEquals(1, got.warnings.size)
    }
}
