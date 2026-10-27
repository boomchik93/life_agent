import 'package:flutter/foundation.dart';
import 'package:workmanager/workmanager.dart';

import 'api.dart';
import 'health.dart';
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
    try {
      final result = await HealthSync(api: api, source: HealthConnectSource()).run();
      await storage.saveHealthSync(DateTime.now().toIso8601String(), result.skipped);
      return true;
    } on ApiException catch (e) {
      await storage.saveHealthSync(DateTime.now().toIso8601String(), e.message);
      // Without a network the system tries again later; other errors would repeat.
      return e.problem != ApiProblem.network;
    } catch (e) {
      await storage.saveHealthSync(DateTime.now().toIso8601String(), 'Ошибка чтения данных здоровья.');
      return true;
    }
  });
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
