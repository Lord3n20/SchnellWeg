import java.util.Properties

plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    namespace = "io.github.lord3n20.schnellweg"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        applicationId = "io.github.lord3n20.schnellweg"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = 31 // LocationManager.FUSED_PROVIDER/getCurrentLocation; Ziel ist GrapheneOS
        targetSdk = flutter.targetSdkVersion
        // Uses the version code from pubspec.yaml. When using split APKs, 1000 * ABI_VERSION
        // is added automatically by Flutter. (https://developer.android.com/studio/build/configure-apk-splits#configure-APK-versions)
        // You can force using the value of versionCode by specifying the `-P force-version-code-ignoring-abi=true`
        // flag during build.
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    // Eigener Schlüssel (seit 28.10.): lokal aus android/key.properties (gitignored, zeigt auf den Keystore außerhalb
    // des Repos), in der CI aus Umgebungsvariablen. Ohne beides: Debug-Schlüssel – so eine APK lässt sich nicht
    // über die echte installieren.
    val schluessel = Properties().apply {
        rootProject.file("key.properties").takeIf { it.exists() }?.inputStream()?.use { load(it) }
        System.getenv("SCHNELLWEG_KEYSTORE")?.let {
            setProperty("storeFile", it)
            setProperty("storePassword", System.getenv("SCHNELLWEG_KEYSTORE_PASSWORT"))
            setProperty("keyAlias", "schnellweg")
        }
    }
    signingConfigs {
        if (schluessel.containsKey("storeFile")) create("eigen") {
            storeFile = file(schluessel.getProperty("storeFile"))
            storePassword = schluessel.getProperty("storePassword")
            keyAlias = schluessel.getProperty("keyAlias")
            keyPassword = schluessel.getProperty("storePassword")
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.findByName("eigen") ?: signingConfigs.getByName("debug")
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}
