// The Wear OS app: a tile and a complication showing the cluster's health from the snapshot the
// phone's background monitor sends over the Data Layer (the Play phone build's
// src/play/.../wear/WearSnapshotSyncs.kt). No talosconfig or kubeconfig on the watch.
plugins {
    alias(libs.plugins.android.application)
}

android {
    namespace = "name.levis.ichor.wear"
    compileSdk = 37
    buildToolsVersion = "37.0.0"

    defaultConfig {
        // The Data Layer only links apps with the same id and signing key: the Play phone build's.
        applicationId = "name.levis.ichor"
        minSdk = 30
        targetSdk = 37
        versionCode = System.getenv("ICHOR_BUILD_NUMBER")?.toIntOrNull() ?: 1
        versionName = System.getenv("ICHOR_VERSION") ?: "0.0.0-unknown"
    }

    // The phone's release key (see app/build.gradle.kts); unsigned without it.
    val keystorePath = System.getenv("ICHOR_KEYSTORE")
    signingConfigs {
        if (keystorePath != null) {
            create("release") {
                storeFile = file(keystorePath)
                storePassword = System.getenv("ICHOR_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("ICHOR_KEY_ALIAS")
                keyPassword = System.getenv("ICHOR_KEY_PASSWORD") ?: System.getenv("ICHOR_KEYSTORE_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
            signingConfig = signingConfigs.findByName("release")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation(project(":core-widget"))
    implementation(libs.androidx.core.ktx)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.play.services.wearable)
    implementation(libs.wear.tiles)
    implementation(libs.wear.protolayout)
    implementation(libs.wear.complications.data.source)
    implementation(libs.androidx.concurrent.futures)

    testImplementation(libs.junit)
}
