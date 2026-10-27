import 'package:flutter/foundation.dart';
import 'package:workmanager/workmanager.dart';

import 'api.dart';
import 'health.dart';
import 'notifications.dart';
import 'settings.dart';

const healthTaskName = 'health-sync';
const _healthUnique = 'life-agent-health-sync';

/// Entry point of the background isolate: the system calls it when a
/// scheduled task is due, also when the app is closed.
@pragma('vm:entry-point')
void callbackDispatcher() {
  Workmanager().executeTask((task, input) async {
    if (task != healthTaskName) return true;
    final storage = PlatformStorage();
    final api = apiFor(await storage.loadConnection());
    if (api == null) return true;
    return runBackgroundJobs(api, storage, HealthConnectSource(), LocalNotifier());
  });
}

/// The work of one background round: the health summaries, then the advice
/// the server prepared. One job failing does not stop the other. Returns false
/// when a retry makes sense (no network).
Future<bool> runBackgroundJobs(LifeApi api, AppStorage storage, HealthSource health, Notifier notifier,
    {DateTime Function()? now}) async {
  var retry = false;
  final clock = now ?? DateTime.now;

  if (await storage.healthSyncEnabled()) {
    try {
      final result = await HealthSync(api: api, source: health, now: now).run();
      await storage.saveHealthSync(clock().toIso8601String(), result.skipped);
    } on ApiException catch (e) {
      await storage.saveHealthSync(clock().toIso8601String(), e.message);
      retry |= e.problem == ApiProblem.network;
    } catch (_) {
      await storage.saveHealthSync(clock().toIso8601String(), 'Ошибка чтения данных здоровья.');
    }
  }

  if (await storage.adviceNotifyEnabled()) {
    try {
      await notifyPendingAdvice(api, notifier);
    } on ApiException catch (e) {
      retry |= e.problem == ApiProblem.network;
    } catch (_) {
      // A notification that fails to show is not worth a retry storm.
    }
  }
  return !retry;
}

/// Turns the periodic sync on and off.
abstract class SyncScheduler {
  Future<void> start();
  Future<void> stop();
}

class WorkmanagerScheduler implements SyncScheduler {
  /// The background task runs every 6 hours (PLAN.md section 14).
  static const interval = Duration(hours: 6);

  static bool get supported => !kIsWeb && defaultTargetPlatform == TargetPlatform.android;

  static Future<void> init() async {
    if (!supported) return;
    await Workmanager().initialize(callbackDispatcher);
  }

  @override
  Future<void> start() async {
    if (!supported) return;
    await Workmanager().registerPeriodicTask(
      _healthUnique,
      healthTaskName,
      frequency: interval,
      constraints: Constraints(networkType: NetworkType.connected),
      existingWorkPolicy: ExistingPeriodicWorkPolicy.update,
    );
  }

  @override
  Future<void> stop() async {
    if (!supported) return;
    await Workmanager().cancelByUniqueName(_healthUnique);
  }
}

/// Starts the periodic task while some job needs it, stops it when none does.
Future<void> applyBackground(AppStorage storage, SyncScheduler scheduler) async {
  final needed = await storage.healthSyncEnabled() || await storage.adviceNotifyEnabled();
  if (needed) {
    await scheduler.start();
  } else {
    await scheduler.stop();
  }
}
