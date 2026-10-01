plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

android {
    namespace = "name.levis.ichor"
    compileSdk = 37
    buildToolsVersion = "37.0.0"

    defaultConfig {
        applicationId = "name.levis.ichor"
        minSdk = 26
        targetSdk = 37
        // From scripts/version.sh (exported by build.sh); fallbacks for IDE builds.
        versionCode = System.getenv("ICHOR_BUILD_NUMBER")?.toIntOrNull() ?: 1
        versionName = System.getenv("ICHOR_VERSION") ?: "0.0.0-unknown"

        // GitHub repository the self-updater reads releases from (set by GitHub Actions).
        val updateRepo = System.getenv("GITHUB_REPOSITORY") ?: "cyrinux/ichor"
        buildConfigField("String", "UPDATE_REPO", "\"$updateRepo\"")
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
        release {
            isMinifyEnabled = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.findByName("release")
        }
    }

    // One APK per ABI (app-<abi>-<buildType>.apk), each with only its own native libraries.
    // The list must match the Go core targets in build.sh.
    splits {
        abi {
            isEnable = true
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

dependencies {
    implementation(files("libs/talosmobile.aar"))

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
    debugImplementation(libs.compose.ui.tooling)

    testImplementation(libs.junit)
}
