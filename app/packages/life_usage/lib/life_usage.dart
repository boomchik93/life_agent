import 'package:flutter/services.dart';

/// One continuous use of an app: a package name and two moments. This is all
/// the plugin knows; the package name never leaves the phone unless the app
/// could not be placed locally (see PLAN.md stage 10.1).
class AppSession {
  const AppSession({required this.pkg, required this.start, required this.end, required this.androidCategory});

  final String pkg;
  final DateTime start, end;

  /// `ApplicationInfo.category`: 0 game, 1 audio, 2 video, 3 image, 4 social,
  /// 5 news, 6 maps, 7 productivity, 8 accessibility, -1 undefined.
  final int androidCategory;
}

/// Android "usage access": when apps were in the foreground.
class LifeUsage {
  static const _channel = MethodChannel('dev.boomchick.life_usage');

  /// Whether the user has granted "usage access" to this app.
  static Future<bool> hasAccess() async => await _channel.invokeMethod<bool>('hasAccess') ?? false;

  /// Opens the system screen where the user grants the access, once.
  static Future<void> openAccessSettings() => _channel.invokeMethod<void>('openAccessSettings');

  /// The sessions between [from] and [to], oldest first.
  static Future<List<AppSession>> sessions(DateTime from, DateTime to) async {
    final raw = await _channel.invokeListMethod<Map<Object?, Object?>>('sessions', {
          'from': from.millisecondsSinceEpoch,
          'to': to.millisecondsSinceEpoch,
        }) ??
        const [];
    return [
      for (final m in raw)
        AppSession(
          pkg: m['pkg'] as String,
          start: DateTime.fromMillisecondsSinceEpoch((m['start'] as num).toInt()),
          end: DateTime.fromMillisecondsSinceEpoch((m['end'] as num).toInt()),
          androidCategory: (m['cat'] as num).toInt(),
        ),
    ]..sort((a, b) => a.start.compareTo(b.start));
  }
}
