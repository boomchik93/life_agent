import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import 'api.dart';

/// Shows a notification. A fake in tests, the system one on the phone.
abstract class Notifier {
  Future<void> show({
    required int id,
    required String title,
    required String body,
  });

  /// Android 13+ asks before the first notification. True when allowed.
  Future<bool> requestPermission();
}

class LocalNotifier implements Notifier {
  final _plugin = FlutterLocalNotificationsPlugin();
  bool _ready = false;

  static const _channel = AndroidNotificationDetails(
    'advice',
    'Советы',
    channelDescription: 'Советы, чтобы не выгореть',
    importance: Importance.defaultImportance,
  );

  Future<void> _init() async {
    if (_ready) return;
    await _plugin.initialize(
      settings: const InitializationSettings(
        android: AndroidInitializationSettings('@mipmap/ic_launcher'),
      ),
    );
    _ready = true;
  }

  @override
  Future<void> show({
    required int id,
    required String title,
    required String body,
  }) async {
    await _init();
    await _plugin.show(
      id: id,
      title: title,
      body: body,
      notificationDetails: const NotificationDetails(android: _channel),
    );
  }

  @override
  Future<bool> requestPermission() async {
    await _init();
    final android = _plugin
        .resolvePlatformSpecificImplementation<
          AndroidFlutterLocalNotificationsPlugin
        >();
    return await android?.requestNotificationsPermission() ?? false;
  }
}

/// Fetches the advice the server prepared and shows it. Run by the background
/// task: the server hands an advice over once, so it is never shown twice.
Future<bool> notifyPendingAdvice(LifeApi api, Notifier notifier) async {
  final advice = await api.pendingAdvice();
  if (advice == null) return false;
  await notifier.show(id: advice.id, title: 'Совет', body: advice.text);
  return true;
}
