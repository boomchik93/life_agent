/// One source of activity data and its state on the server.
class SourceInfo {
  const SourceInfo({required this.source, required this.enabled, this.lastSync, this.lastError});

  final String source;
  final bool enabled;
  final String? lastSync; // ISO 8601
  final String? lastError;

  factory SourceInfo.fromJson(Map<String, dynamic> j) => SourceInfo(
        source: j['source'] as String,
        enabled: j['enabled'] as bool? ?? true,
        lastSync: j['last_sync'] as String?,
        lastError: j['last_error'] as String?,
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
