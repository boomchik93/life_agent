package dev.boomchick.life_usage

import android.app.AppOpsManager
import android.app.usage.UsageEvents
import android.app.usage.UsageStatsManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Handler
import android.os.Looper
import android.os.Process
import android.provider.Settings
import io.flutter.embedding.engine.plugins.FlutterPlugin
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import io.flutter.plugin.common.MethodChannel.MethodCallHandler
import io.flutter.plugin.common.MethodChannel.Result
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

/**
 * Reads when apps were in the foreground. A session is a package and two
 * moments; nothing else is read: no titles, no content. The plugin is a
 * normal Flutter plugin so that the background isolate of WorkManager has it too.
 */
class LifeUsagePlugin : FlutterPlugin, MethodCallHandler {
    private lateinit var context: Context
    private lateinit var channel: MethodChannel

    /** The events of weeks take long to read: not on the main thread, which draws and takes touches. */
    private val worker: ExecutorService = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    override fun onAttachedToEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        context = binding.applicationContext
        channel = MethodChannel(binding.binaryMessenger, "dev.boomchick.life_usage")
        channel.setMethodCallHandler(this)
    }

    override fun onDetachedFromEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        channel.setMethodCallHandler(null)
        worker.shutdown()
    }

    override fun onMethodCall(call: MethodCall, result: Result) {
        when (call.method) {
            "hasAccess" -> result.success(hasAccess())
            "openAccessSettings" -> {
                val intent = Intent(Settings.ACTION_USAGE_ACCESS_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                context.startActivity(intent)
                result.success(null)
            }
            "sessions" -> {
                val from = call.argument<Number>("from")?.toLong()
                val to = call.argument<Number>("to")?.toLong()
                if (from == null || to == null || to <= from) {
                    result.error("bad_args", "from and to are required, to > from", null)
                } else if (!hasAccess()) {
                    result.error("no_access", "usage access is not granted", null)
                } else {
                    worker.execute {
                        try {
                            val list = sessions(from, to)
                            main.post { result.success(list) }
                        } catch (e: Exception) {
                            main.post { result.error("read_failed", e.message, null) }
                        }
                    }
                }
            }
            else -> result.notImplemented()
        }
    }

    private fun hasAccess(): Boolean {
        val ops = context.getSystemService(Context.APP_OPS_SERVICE) as AppOpsManager
        val mode = ops.checkOpNoThrow(AppOpsManager.OPSTR_GET_USAGE_STATS, Process.myUid(), context.packageName)
        return mode == AppOpsManager.MODE_ALLOWED
    }

    /** Packages that are not "use of an app": the launcher, the system UI, ourselves. */
    private fun ignoredPackages(): Set<String> {
        val home = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_HOME)
        val launchers = context.packageManager.queryIntentActivities(home, 0).map { it.activityInfo.packageName }
        return (launchers + listOf("android", "com.android.systemui", context.packageName)).toSet()
    }

    private fun androidCategory(pkg: String): Int = try {
        context.packageManager.getApplicationInfo(pkg, 0).category
    } catch (e: PackageManager.NameNotFoundException) {
        -1
    }

    private fun sessions(from: Long, to: Long): List<Map<String, Any>> {
        val manager = context.getSystemService(Context.USAGE_STATS_SERVICE) as UsageStatsManager
        val events = manager.queryEvents(from, to)
        val ignored = ignoredPackages()
        val open = HashMap<String, Long>() // package -> moment it came to the foreground
        val out = ArrayList<Map<String, Any>>()
        val categories = HashMap<String, Int>()

        fun close(pkg: String, end: Long) {
            val start = open.remove(pkg) ?: return
            if (end > start && pkg !in ignored) {
                out.add(mapOf("pkg" to pkg, "start" to start, "end" to end, "cat" to categories.getOrPut(pkg) { androidCategory(pkg) }))
            }
        }

        val e = UsageEvents.Event()
        while (events.hasNextEvent()) {
            events.getNextEvent(e)
            val pkg = e.packageName ?: continue
            when (e.eventType) {
                UsageEvents.Event.ACTIVITY_RESUMED -> {
                    // Another app coming forward ends the previous one even if its pause was lost.
                    for (other in open.keys.filter { it != pkg }) close(other, e.timeStamp)
                    if (pkg !in open) open[pkg] = e.timeStamp
                }
                UsageEvents.Event.ACTIVITY_PAUSED, UsageEvents.Event.ACTIVITY_STOPPED -> close(pkg, e.timeStamp)
                UsageEvents.Event.SCREEN_NON_INTERACTIVE, UsageEvents.Event.DEVICE_SHUTDOWN ->
                    for (other in open.keys.toList()) close(other, e.timeStamp)
            }
        }
        // Still in the foreground at the end of the window.
        val now = minOf(to, System.currentTimeMillis())
        for (pkg in open.keys.toList()) close(pkg, now)
        return out
    }
}
