package dev.boomchick.life_agent

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.core.content.FileProvider
import io.flutter.embedding.android.FlutterFragmentActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.File

// Health Connect asks for permissions through registerForActivityResult,
// which needs a FragmentActivity.
class MainActivity : FlutterFragmentActivity() {
    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        // The app updates itself: Dart downloads the APK from our server into
        // the cache, the system installer asks the user to confirm.
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "dev.boomchick.life_agent/update")
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "installedVersion" -> result.success(installedVersion())
                    "apkPath" -> result.success(apkFile().absolutePath)
                    "canInstall" -> result.success(packageManager.canRequestPackageInstalls())
                    "openInstallSettings" -> {
                        startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:$packageName")))
                        result.success(null)
                    }
                    "install" -> {
                        val file = apkFile()
                        if (!file.exists()) {
                            result.error("no_apk", "the APK is not downloaded", null)
                        } else {
                            val uri = FileProvider.getUriForFile(this, "$packageName.update", file)
                            val intent = Intent(Intent.ACTION_VIEW)
                                .setDataAndType(uri, "application/vnd.android.package-archive")
                                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                            startActivity(intent)
                            result.success(null)
                        }
                    }
                    else -> result.notImplemented()
                }
            }
    }

    /** The one place the APK goes; res/xml/update_paths.xml shares only it. */
    private fun apkFile(): File = File(File(cacheDir, "update").apply { mkdirs() }, "life-agent.apk")

    private fun installedVersion(): Long {
        val info = packageManager.getPackageInfo(packageName, 0)
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            info.longVersionCode
        } else {
            @Suppress("DEPRECATION")
            info.versionCode.toLong()
        }
    }
}
