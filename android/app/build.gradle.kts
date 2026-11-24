plugins { id("com.android.application") version "9.5.2" }
android {
    namespace = "lab.ridgeradio"
    compileSdk { version = release(37) { minorApiLevel = 2 } }
    buildToolsVersion = "37.0.0"
    defaultConfig {
        applicationId = "lab.ridgeradio"
        minSdk = 29
        targetSdk = 37
        versionCode = 1
        versionName = "0.1.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
    sourceSets.getByName("debug").res.srcDir(providers.gradleProperty("ridgeLabRes").orElse("src/trust/res").get())
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}
dependencies {
    implementation("io.github.webrtc-sdk:android:150.7871.08")
    implementation("com.squareup.okhttp3:okhttp:5.5.1")
    androidTestImplementation("androidx.test:runner:1.7.0")
    androidTestImplementation("androidx.test.ext:junit:1.3.0")
}
