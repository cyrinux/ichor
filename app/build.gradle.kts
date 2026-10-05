plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
    // Generates the "Open-source licenses" list from the resolved dependencies at build time.
    alias(libs.plugins.aboutlibraries.android)
}

android {
    namespace = "name.levis.ichor"
    compileSdk = 37
    buildToolsVersion = "37.0.0"

    defaultConfig {
        // The Play build keeps the bare id; the open-source builds (GitHub releases, Obtainium,
        // debug) add ".foss" so both can be installed side by side and never update each other.
        applicationId = "name.levis.ichor"
        minSdk = 26
        targetSdk = 37
        // From scripts/version.sh (exported by build.sh); fallbacks for IDE builds.
        versionCode = System.getenv("ICHOR_BUILD_NUMBER")?.toIntOrNull() ?: 1
        versionName = System.getenv("ICHOR_VERSION") ?: "0.0.0-unknown"

        // GitHub repository the self-updater reads releases from (set by GitHub Actions).
        val updateRepo = System.getenv("GITHUB_REPOSITORY") ?: "cyrinux/ichor"
        buildConfigField("String", "UPDATE_REPO", "\"$updateRepo\"")
        // Off in the Play build: Play forbids self-updates and in-app donations outside its billing.
        buildConfigField("boolean", "SELF_UPDATE", "true")
        buildConfigField("boolean", "DONATIONS", "true")
        // On in the Play build: features funded with Play in-app products (Settings → About).
        buildConfigField("boolean", "FEATURE_FUNDING", "false")
    }

    // Release signing comes from env vars so CI (Forgejo) can inject secrets.
    val keystorePath = System.getenv("ICHOR_KEYSTORE")
    signingConfigs {
        if (keystorePath != null) {
            create("release") {
                storeFile = file(keystorePath)
                storePassword = System.getenv("ICHOR_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("ICHOR_KEY_ALIAS")
                // Like lectarr: the key password defaults to the store password.
                keyPassword = System.getenv("ICHOR_KEY_PASSWORD") ?: System.getenv("ICHOR_KEYSTORE_PASSWORD")
            }
        }
    }

    buildTypes {
        debug {
            applicationIdSuffix = ".foss"
        }
        release {
            applicationIdSuffix = ".foss"
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.findByName("release")
        }
        // Google Play: the release build without the self-updater and donation links, shipped
        // as an App Bundle (`bundlePlay`). src/play/AndroidManifest.xml drops the install
        // permission. Signed with the release key, which doubles as the Play upload key.
        create("play") {
            initWith(getByName("release"))
            applicationIdSuffix = null
            buildConfigField("boolean", "SELF_UPDATE", "false")
            buildConfigField("boolean", "DONATIONS", "false")
            buildConfigField("boolean", "FEATURE_FUNDING", "true")
        }
    }

    // The store behind feature funding and updates: Play Billing and Play in-app updates in
    // src/play, none in src/foss (shared by the open-source builds), so the proprietary Play
    // libraries never reach their APKs.
    // src/foss/res tells them apart from the Play build on a home screen (name and icon).
    sourceSets {
        getByName("debug").kotlin.directories.add("src/foss/java")
        getByName("release").kotlin.directories.add("src/foss/java")
        getByName("debug").res.directories.add("src/foss/res")
        getByName("release").res.directories.add("src/foss/res")
    }

    // One APK per ABI (app-<abi>-<buildType>.apk), each with only its own native libraries.
    // The list must match the Go core targets in build.sh.
    // Off for App Bundles (bundlePlay): Play splits by ABI itself, and optimized resource
    // shrinking refuses bundles built alongside multiple APKs (issuetracker 402800800).
    val buildingBundle = gradle.startParameter.taskNames.any { it.contains("bundle", ignoreCase = true) }
    splits {
        abi {
            isEnable = !buildingBundle
            reset()
            include("arm64-v8a", "armeabi-v7a", "x86_64")
            isUniversalApk = false
        }
    }

    // Compress native libraries in the APK (extracted at install). The Go core shrinks from
    // ~35 MB to ~10 MB, which matters for a sideloaded/self-updated APK; ELF segments stay
    // 16 KB-aligned, so the Android 15+ page-size requirement still holds.
    packaging {
        jniLibs { useLegacyPackaging = true }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    // Lists the app's languages in Android 13+ per-app language settings (res/resources.properties).
    androidResources {
        generateLocaleConfig = true
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }
}

// The licenses screen. Gradle dependencies are collected automatically; the Go core's modules
// (generated by scripts/go-licenses.py) and the bundled icon sets are invisible to Gradle and
// declared in aboutlibraries/libraries/. License texts are fetched from SPDX at build time.
aboutLibraries {
    collect {
        configPath = file("aboutlibraries")
    }
    export {
        // Unused by the screen; keeps the generated JSON (and the APK) small.
        excludeFields.addAll("developers", "funding")
    }
    library {
        duplicationMode = com.mikepenz.aboutlibraries.plugin.DuplicateMode.MERGE
    }
}

dependencies {
    implementation(files("libs/ichorgo.aar"))

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.navigation.compose)
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    implementation(libs.compose.material.icons)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.camerax.camera2)
    implementation(libs.camerax.lifecycle)
    implementation(libs.camerax.view)
    implementation(libs.mlkit.barcode)
    // QR encoder for sharing an issued talosconfig (ML Kit only decodes). Pure Java, no Android deps.
    implementation(libs.zxing.core)
    implementation(libs.termlib)
    implementation(libs.androidx.biometric)
    implementation(libs.androidx.fragment.ktx)
    implementation(libs.androidx.lifecycle.process)
    implementation(libs.androidx.work.runtime)
    implementation(libs.androidx.glance.appwidget)
    implementation(libs.androidx.glance.material3)
    implementation(libs.aboutlibraries.compose.m3)
    // Feature funding and in-app updates, Play build only (see sourceSets above).
    "playImplementation"(libs.billing)
    "playImplementation"(libs.play.app.update)
    debugImplementation(libs.compose.ui.tooling)

    testImplementation(libs.junit)
}
