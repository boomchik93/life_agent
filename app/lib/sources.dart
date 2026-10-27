/// One source of activity data and its state on the server.
class SourceInfo {
  const SourceInfo({
    required this.source,
    required this.enabled,
    this.lastSync,
    this.lastError,
    this.configured = true,
    this.missing,
  });

  final String source;
  final bool enabled;

  /// The server has what the source needs (a key in .env, the city).
  final bool configured;

  /// What is not set up, when [configured] is false.
  final String? missing;
  final String? lastSync; // ISO 8601
  final String? lastError;

  factory SourceInfo.fromJson(Map<String, dynamic> j) => SourceInfo(
        source: j['source'] as String,
        enabled: j['enabled'] as bool? ?? true,
        lastSync: j['last_sync'] as String?,
        lastError: j['last_error'] as String?,
        configured: j['configured'] as bool? ?? true,
        missing: j['missing'] as String?,
      );

  String get title => sourceTitles[source] ?? source;
}

const sourceTitles = {
  'android_usage': 'Телефон: время в приложениях',
  'mac_activitywatch': 'Mac: ActivityWatch',
  'steam': 'Steam',
  'github': 'GitHub',
  'leetcode': 'LeetCode',
  'weather': 'Погода',
};
