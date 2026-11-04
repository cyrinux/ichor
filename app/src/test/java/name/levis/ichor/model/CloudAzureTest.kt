package name.levis.ichor.model

import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CloudAzureTest {
    // The same fixture as ios/IchorCore/Tests/IchorCoreTests/CloudAzureTests.swift.
    private val options = TalosJson.decodeFromString(
        AzureOptions.serializer(),
        """
        {"subscription":"00000000-0000-0000-0000-000000000000","location":"westeurope",
         "subscriptions":[{"id":"00000000-0000-0000-0000-000000000000","name":"Scratch"}],
         "locations":[{"name":"northeurope","displayName":"North Europe"},{"name":"westeurope","displayName":"West Europe"}],
         "resourceGroups":[{"name":"talos","location":"westeurope"},{"name":"north","location":"NorthEurope"}],
         "sizes":[{"name":"Standard_A1_v2","vcpus":1,"memoryGB":2,"arch":"amd64","generations":["V1"]},
                  {"name":"Standard_B2s","vcpus":2,"memoryGB":4,"arch":"amd64","generations":["V1","V2"]},
                  {"name":"Standard_D2ps_v5","vcpus":2,"memoryGB":8,"arch":"arm64","generations":["V2"]},
                  {"name":"Standard_E2ps_v5","vcpus":2,"memoryGB":16,"arch":"arm64","generations":["V2"]}],
         "networks":[{"id":"vnet-a","name":"talos-vnet","resourceGroup":"talos","location":"westeurope",
                      "subnets":[{"id":"vnet-a/subnets/nodes","name":"nodes","cidr":"10.0.0.0/24","nsgId":"nsg-a","natGateway":false},
                                 {"id":"vnet-a/subnets/egress","name":"egress","cidr":"10.0.1.0/24","nsgId":"","natGateway":true}]}],
         "securityGroups":[{"id":"nsg-a","name":"talos-nodes","resourceGroup":"talos","wideOpen":true}],
         "images":[{"id":"gallery-arm","name":"talos/talos-arm64/1.12.0","resourceGroup":"talos","source":"gallery","arch":"arm64",
                    "generation":"V2","talosVersion":"v1.12.0","schematic":"sample-schematic"},
                   {"id":"image-amd","name":"talos-by-hand","resourceGroup":"talos","source":"managed","arch":"amd64",
                    "generation":"V1","talosVersion":"","schematic":""}],
         "warnings":["a warning"]}
        """,
    )

    @Test
    fun theImageChoosesTheSizesThatBootIt() {
        val (gallery, managed) = options.images
        assertEquals(listOf("Standard_D2ps_v5", "Standard_E2ps_v5"), options.sizesFor(gallery).map { it.name })
        assertEquals(listOf("Standard_A1_v2", "Standard_B2s"), options.sizesFor(managed).map { it.name })
        assertEquals(listOf("amd64", "arm64"), options.sizes.reversed().arches())
        assertEquals("gallery", gallery.source)
    }

    @Test
    fun theNetworkChoosesItsSubnets() {
        val subnets = options.subnetsIn("vnet-a")
        assertEquals(listOf("nodes", "egress"), subnets.map { it.name })
        assertTrue(subnets[1].natGateway)
        assertEquals("", subnets[1].nsgId)
        assertTrue(options.subnetsIn("vnet-z").isEmpty())
        assertTrue(options.securityGroups.single().wideOpen)
    }

    @Test
    fun theResourceGroupGivesTheLocation() {
        assertEquals("northeurope", options.locationOf("north"))
        assertEquals("westeurope", options.locationOf("unknown"))
        assertEquals("West Europe", options.locationLabel("westeurope"))
        assertEquals("eastus", options.locationLabel("eastus"))
        assertEquals(listOf("a warning"), options.warnings)
    }

    @Test
    fun theDiskSizeIsOneAzureTakes() {
        assertEquals(20, azureDiskGB(" 20 "))
        assertEquals(4095, azureDiskGB("4095"))
        assertNull(azureDiskGB("4096"))
        assertNull(azureDiskGB("5"))
        assertNull(azureDiskGB(""))
    }

    @Test
    fun nullListsDecodeEmpty() {
        val empty = TalosJson.decodeFromString(AzureOptions.serializer(), """{"location":"westeurope","sizes":null,"images":null,"warnings":null}""")
        assertTrue(empty.sizes.isEmpty() && empty.images.isEmpty() && empty.warnings.isEmpty())
    }

    @Test
    fun theCheckAndThePriceDecode() {
        val check = TalosJson.decodeFromString(
            AzureCheck.serializer(),
            """{"tenant":"t","subscription":"00000000-0000-0000-0000-000000000000","subscriptionName":"Scratch","canCreate":false,"checked":false}""",
        )
        assertFalse(check.checked)
        assertEquals("Scratch", check.subscriptionName)
        assertEquals("0.0480", TalosJson.decodeFromString(AzurePrice.serializer(), """{"hourly":"0.0480","currency":"USD"}""").hourly)
        assertEquals("", TalosJson.decodeFromString(AzurePrice.serializer(), """{"hourly":""}""").hourly)
    }

    @Test
    fun theCredentialAndTheSpecUseTheCoresNames() {
        val credential = TalosJson.encodeToString(
            AzureCredential.serializer(),
            AzureCredential("t", "c", "s", "sub", "AzureChinaCloud", "talos", "chinaeast2"),
        )
        listOf("\"tenantId\"", "\"clientId\"", "\"clientSecret\"", "\"subscriptionId\"", "\"environment\"", "\"resourceGroup\"", "\"location\"").forEach {
            assertTrue(credential, credential.contains(it))
        }
        val spec = AzureCreateSpec(
            resourceGroup = "talos", location = "westeurope", size = "Standard_B2s", imageId = "image-amd", subnetId = "vnet-a/subnets/nodes",
            securityGroupId = "nsg-a", associatePublicIp = true, osDiskGB = 20, hostname = "node-c",
        )
        val json = TalosJson.encodeToString(AzureCreateSpec.serializer(), spec)
        listOf("\"resourceGroup\"", "\"size\"", "\"imageId\"", "\"subnetId\"", "\"securityGroupId\"", "\"associatePublicIp\":true", "\"osDiskGB\":20").forEach {
            assertTrue(json, json.contains(it))
        }
    }

    @Test
    fun aRunWithAVMHasTheCloudTimeline() {
        val spec = AzureCreateSpec(resourceGroup = "talos", location = "westeurope", size = "Standard_B2s", imageId = "image-amd", subnetId = "s", hostname = "node-c")
        val plain = JoinRequest(address = "", template = "", role = "", hostname = "node-c", disk = "", staticAddress = "", azure = spec)
        assertTrue(plain.cloud)
        assertEquals(JoinPhase.cloudRun, plain.phases)
        assertEquals(JoinPhase.cloudOmniRun, plain.copy(omni = true).phases)
    }
}
