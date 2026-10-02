package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class InventoryTest {

    // Shaped like go/talosmobile/inventory.go's output (demo cluster, trimmed).
    private val json = """
        {"at":1790000000000,"nodes":5,"answered":4,"apps":[
          {"id":"cilium","name":"Cilium","category":"networking","icon":"cilium","known":true,"system":false,
           "version":"v1.18.2","drift":true,"unpinned":false,"namespaces":["kube-system"],
           "nodes":["192.0.2.20","192.0.2.21"],"containers":3,"running":3,"memory":641728512,
           "images":[{"repo":"quay.io/cilium/cilium","tag":"v1.18.2","containers":1},
                     {"repo":"quay.io/cilium/cilium","tag":"v1.18.1","containers":1},
                     {"repo":"quay.io/cilium/operator-generic","tag":"v1.18.2","containers":1}],
           "pods":[{"namespace":"kube-system","pod":"cilium-8h2kd","node":"192.0.2.20",
                    "containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.18.2","status":"CONTAINER_RUNNING","memory":224395264}]}]},
          {"id":"coredns","name":"CoreDNS","category":"system","icon":"coredns","known":true,"system":true,
           "version":"v1.12.3","drift":false,"unpinned":false,"namespaces":["kube-system"],"nodes":["192.0.2.10"],
           "containers":2,"running":2,"memory":1000,"images":[],"pods":[]},
          {"id":"kimai","name":"Kimai","category":"productivity","remoteIcon":"kimai","known":true,"system":false,
           "version":"2.40","drift":false,"unpinned":false,"namespaces":["tools"],"nodes":[],"containers":1,"running":1,
           "memory":0,"images":null,"pods":null},
          {"id":"nightly-backup","name":"nightly-backup","category":"other","known":false,"system":false,
           "version":"","drift":false,"unpinned":false,"namespaces":["tools"],"nodes":["192.0.2.21"],
           "containers":1,"running":0,"memory":0,
           "images":[{"repo":"","tag":"","digest":"sha256:0123456789abcdef0123","containers":1}],"pods":[]},
          {"id":"uptime-kuma","name":"Uptime Kuma","category":"observability","icon":"uptime-kuma","known":true,
           "system":false,"version":"latest","drift":false,"unpinned":true,"namespaces":["default"],
           "nodes":["192.0.2.20"],"containers":1,"running":1,"memory":52428800,
           "images":[{"repo":"docker.io/louislam/uptime-kuma","tag":"latest","containers":1}],"pods":[]}
        ],"future":"ignored"}
    """.trimIndent()

    private val inventory = TalosJson.decodeFromString(Inventory.serializer(), json)
    private fun app(id: String) = inventory.apps.single { it.id == id }

    @Test
    fun decodesTheGoCoreJson() {
        assertEquals(1790000000000L, inventory.at)
        assertEquals(5, inventory.nodes)
        assertEquals(4, inventory.answered)
        assertEquals(5, inventory.apps.size)
        val cilium = app("cilium")
        assertEquals("cilium", cilium.icon)
        assertEquals(641728512L, cilium.memory)
        assertEquals("cilium-agent", cilium.pods.single().containers.single().name)
        // Missing optional fields and Go nil slices fall back to defaults.
        assertEquals("", app("kimai").icon)
        assertEquals("kimai", app("kimai").remoteIcon)
        assertTrue(app("kimai").images.isEmpty())
        assertTrue(app("kimai").pods.isEmpty())
    }

    @Test
    fun attentionIsDriftOrUnpinned() {
        assertTrue(app("cilium").attention)
        assertTrue(app("uptime-kuma").attention)
        assertFalse(app("kimai").attention)
        assertEquals(2, inventory.apps.attentionCount)
        assertTrue(inventory.partial)
    }

    @Test
    fun driftCountsTheTagsOfTheSameRepository() {
        assertEquals(2, app("cilium").driftVersions)
        assertEquals(setOf("quay.io/cilium/cilium"), app("cilium").driftingRepos)
        assertEquals(1, app("kimai").driftVersions)
    }

    @Test
    fun groupsSplitRecognisedSystemAndUnknown() {
        val groups = inventory.apps.groups()
        assertEquals(listOf("cilium", "kimai", "uptime-kuma"), groups.main.map { it.id })
        assertEquals(listOf("coredns"), groups.system.map { it.id })
        assertEquals(listOf("nightly-backup"), groups.unknown.map { it.id })
    }

    @Test
    fun searchMatchesNameIdNamespacesAndImageRepos() {
        assertEquals(listOf("cilium"), inventory.apps.filtered(" CILIUM ", AppFilter.All).map { it.id })
        assertEquals(listOf("kimai", "nightly-backup"), inventory.apps.filtered("tools", AppFilter.All).map { it.id })
        assertEquals(listOf("uptime-kuma"), inventory.apps.filtered("louislam", AppFilter.All).map { it.id })
        assertEquals(listOf("nightly-backup"), inventory.apps.filtered("nightly", AppFilter.All).map { it.id })
        assertTrue(inventory.apps.filtered("nothing-like-this", AppFilter.All).isEmpty())
        assertEquals(5, inventory.apps.filtered("", AppFilter.All).size)
    }

    @Test
    fun filtersByAttentionOrCategory() {
        assertEquals(listOf("cilium", "uptime-kuma"), inventory.apps.filtered("", AppFilter.Attention).map { it.id })
        assertEquals(listOf("kimai"), inventory.apps.filtered("", AppFilter.Category("productivity")).map { it.id })
        assertTrue(inventory.apps.filtered("kimai", AppFilter.Category("media")).isEmpty())
    }

    @Test
    fun categoryCountsFollowTheCatalogOrder() {
        assertEquals(
            listOf("system" to 1, "networking" to 1, "observability" to 1, "productivity" to 1, "other" to 1),
            inventory.apps.categoryCounts(),
        )
        // An unexpected category from a newer core counts as "other".
        val odd = inventory.apps.first().copy(id = "x", category = "quantum")
        assertEquals("other" to 2, (inventory.apps + odd).categoryCounts().last())
    }

    @Test
    fun overviewTilesPutAppsWithIconsFirstAndSkipSystem() {
        val tiles = inventory.apps.overviewTiles(max = 3)
        assertEquals(listOf("cilium", "uptime-kuma", "kimai"), tiles.map { it.id })
        assertEquals(4, inventory.apps.count { !it.system })
    }

    @Test
    fun monogramTakesTwoInitials() {
        assertEquals("UK", monogram("Uptime Kuma"))
        assertEquals("NB", monogram("nightly-backup"))
        assertEquals("GR", monogram("grafana"))
        assertEquals("X", monogram("x"))
        assertEquals("?", monogram("  "))
        assertEquals("ÉA", monogram("état actuel"))
    }

    @Test
    fun monogramHueIsStableAndInRange() {
        assertEquals(monogramHue("nightly-backup"), monogramHue("nightly-backup"), 0f)
        listOf("a", "b", "nightly-backup", "", "some-very-long-identifier").forEach {
            val hue = monogramHue(it)
            assertTrue(hue >= 0f && hue < 360f)
        }
        assertTrue(monogramHue("alpha") != monogramHue("beta"))
    }

    @Test
    fun remoteIconSlugsAreValidatedBeforeBuildingAUrl() {
        assertEquals("https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/kimai.webp", remoteIconUrl("kimai"))
        assertEquals("https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/home-assistant.webp", remoteIconUrl("home-assistant"))
        assertTrue(isValidIconSlug("0x"))
        listOf("", "-kimai", "Kimai", "../etc", "a/b", "kimai.webp", "a b", "a?x=1", "a".repeat(82)).forEach {
            assertFalse(it, isValidIconSlug(it))
            assertNull(it, remoteIconUrl(it))
        }
        assertTrue(isValidIconSlug("a".repeat(81)))
    }

    @Test
    fun bundledIconPathIsValidatedAndPrefersTheNightVariant() {
        val available = setOf("cilium.webp", "cilium-night.webp", "grafana.webp")
        assertEquals("appicons/cilium-night.webp", bundledIconAsset("cilium", dark = true, available))
        assertEquals("appicons/cilium.webp", bundledIconAsset("cilium", dark = false, available))
        assertEquals("appicons/grafana.webp", bundledIconAsset("grafana", dark = true, available))
        listOf("", "../secrets", "a/b", "Cilium", "x.webp").forEach {
            assertNull(it, bundledIconAsset(it, dark = true, available))
        }
    }

    @Test
    fun iconDecodingIsBoundedAndDownsampled() {
        assertEquals(1, iconSampleSize(128, 128))
        assertEquals(1, iconSampleSize(256, 300))
        assertEquals(2, iconSampleSize(512, 512))
        assertEquals(4, iconSampleSize(1024, 1024))
        // Not an icon: empty, unknown or huge declared sizes (a decompression bomb) are refused.
        assertNull(iconSampleSize(0, 64))
        assertNull(iconSampleSize(-1, -1))
        assertNull(iconSampleSize(1025, 16))
        assertNull(iconSampleSize(16_384, 16_384))
    }

    @Test
    fun remoteIconOnlyForAppsWithoutABundledOne() {
        assertEquals("kimai", app("kimai").remoteIconSlug)
        assertNull(app("cilium").copy(remoteIcon = "cilium").remoteIconSlug)
        assertNull(app("nightly-backup").remoteIconSlug)
    }

    @Test
    fun podStatusSummary() {
        assertEquals(PodState.RUNNING, app("cilium").pods.single().state)
        val pod = InventoryPod(
            "ns", "p", "n",
            listOf(InventoryContainer("a", status = "CONTAINER_RUNNING"), InventoryContainer("b", status = "CONTAINER_EXITED")),
        )
        assertEquals(PodState.STOPPED, pod.state)
        assertEquals(PodState.STARTING, pod.copy(containers = listOf(InventoryContainer("a", status = "CONTAINER_CREATED"))).state)
        assertEquals(30L, pod.copy(containers = listOf(InventoryContainer("a", memory = 10), InventoryContainer("b", memory = 20))).memory)
    }
}
