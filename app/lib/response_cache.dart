import 'dart:async';
import 'dart:convert';

import 'package:shared_preferences/shared_preferences.dart';

/// A kept answer and when it came from the server.
class Cached<T> {
  const Cached(this.value, this.at);

  final T value;
  final DateTime at;
}

/// The last answers of the screens' requests, kept on the phone: a screen
/// shows them at once on the next start while it asks the server again.
/// They belong to one server; another address starts empty.
class ResponseCache {
  ResponseCache(this.scope, {Map<String, Cached<Map<String, dynamic>>>? entries, this.save}) : _entries = entries ?? {};

  static const _key = 'response_cache';

  /// The oldest answers go first: a few screens, each over a few periods.
  static const maxEntries = 40;

  final String scope;
  final Map<String, Cached<Map<String, dynamic>>> _entries; // oldest first

  /// Writes the text of the cache where it lives; null: it lives in memory only.
  final Future<void> Function(String)? save;

  static Future<ResponseCache> open(String scope) async {
    final prefs = await SharedPreferences.getInstance();
    final entries = <String, Cached<Map<String, dynamic>>>{};
    try {
      final raw = jsonDecode(prefs.getString(_key) ?? '{}') as Map<String, dynamic>;
      if (raw['scope'] == scope) {
        for (final e in ((raw['entries'] as Map<String, dynamic>?) ?? const {}).entries) {
          final v = e.value as Map<String, dynamic>;
          entries[e.key] = Cached(v['body'] as Map<String, dynamic>, DateTime.parse(v['at'] as String));
        }
      }
    } catch (_) {
      // A broken copy only costs the speed-up: the screens ask the server.
    }
    return ResponseCache(scope, entries: entries, save: (s) async {
      await prefs.setString(_key, s);
    });
  }

  Cached<Map<String, dynamic>>? read(String key) => _entries[key];

  void write(String key, Map<String, dynamic> body, DateTime at) {
    _entries.remove(key);
    _entries[key] = Cached(body, at);
    while (_entries.length > maxEntries) {
      _entries.remove(_entries.keys.first);
    }
    _persist();
  }

  /// Forgets everything, for example after the data on the server was deleted.
  void clear() {
    _entries.clear();
    _persist();
  }

  void _persist() {
    final save = this.save;
    if (save == null) return;
    final text = jsonEncode({
      'scope': scope,
      'entries': {
        for (final e in _entries.entries) e.key: {'at': e.value.at.toIso8601String(), 'body': e.value.value},
      },
    });
    unawaited(save(text).catchError((_) {}));
  }
}
