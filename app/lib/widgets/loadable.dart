import 'package:flutter/material.dart';

import '../api.dart';
import '../response_cache.dart';
import '../theme.dart';
import 'error_view.dart';

/// The data of a screen that is asked again now and then. The last answer
/// stays on the screen while a new one is on its way, so the screen does not
/// go blank; an answer kept from an earlier start fills it at once.
class Loadable<T> extends ChangeNotifier {
  T? data;

  /// When the shown data came from the server; null: it is the newest answer.
  DateTime? keptAt;

  /// Why the last load failed; null when it did not.
  Object? error;
  bool loading = false;

  int _seq = 0;
  bool _disposed = false;

  /// Shows [future] when it comes. [kept] fills an empty screen meanwhile;
  /// [clear] drops the shown data first, when it is about something else
  /// (another week, another period).
  Future<void> load(Future<T> future, {Cached<T>? kept, bool clear = false}) async {
    final seq = ++_seq;
    if (clear) {
      data = null;
      keptAt = null;
    }
    if (data == null && kept != null) {
      data = kept.value;
      keptAt = kept.at;
    }
    loading = true;
    error = null;
    _notify();
    try {
      final value = await future;
      if (seq != _seq) return;
      data = value;
      keptAt = null;
    } catch (e) {
      if (seq != _seq) return;
      error = e;
    }
    loading = false;
    _notify();
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}

/// Shows a [Loadable]: [placeholder] (a spinner by default) the first time,
/// the error when there is nothing to show, otherwise the data with a thin
/// bar while it is renewed and a strip when renewing failed. [expand] fills
/// the height given; off, it takes the height of the content (in a list).
class LoadableView<T> extends StatelessWidget {
  const LoadableView({
    super.key,
    required this.source,
    required this.builder,
    required this.onRetry,
    this.onSettings,
    this.placeholder,
    this.expand = true,
  });

  final Loadable<T> source;
  final Widget Function(BuildContext context, T data) builder;
  final VoidCallback onRetry;
  final VoidCallback? onSettings;
  final Widget? placeholder;
  final bool expand;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: source,
      builder: (context, _) {
        final data = source.data;
        if (data == null) {
          final error = source.error;
          if (error != null && !source.loading) return ErrorView(error: error, onRetry: onRetry, onSettings: onSettings);
          return placeholder ?? const Center(child: CircularProgressIndicator());
        }
        final content = builder(context, data);
        final top = <Widget>[
          SizedBox(height: 2, child: source.loading ? const LinearProgressIndicator(minHeight: 2) : null),
          if (source.error != null) StaleNotice(error: source.error!, keptAt: source.keptAt, onRetry: onRetry),
        ];
        return expand
            ? Column(children: [...top, Expanded(child: content)])
            : Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [...top, content]);
      },
    );
  }
}

/// A strip over data that could not be renewed: why, and since when it is.
class StaleNotice extends StatelessWidget {
  const StaleNotice({super.key, required this.error, required this.keptAt, required this.onRetry});

  final Object error;
  final DateTime? keptAt;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final api = error is ApiException ? error as ApiException : null;
    final why = api?.problem == ApiProblem.network ? 'Нет связи с сервером' : 'Не удалось обновить';
    final at = keptAt;
    final text = at == null ? why : '$why. Данные от ${keptLabel(at, DateTime.now())}';
    return Material(
      color: pal.surface2,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 2, 4, 2),
        child: Row(children: [
          Icon(api?.problem == ApiProblem.network ? Icons.wifi_off : Icons.cloud_off, size: 18, color: pal.text2),
          const SizedBox(width: 8),
          Expanded(child: Text(text, style: theme.textTheme.bodySmall?.copyWith(color: pal.text2))),
          TextButton(onPressed: onRetry, child: const Text('Повторить')),
        ]),
      ),
    );
  }
}

/// "14:05" today, "вчера 14:05", "26.10 14:05" earlier.
String keptLabel(DateTime at, DateTime now) {
  final hm = '${at.hour.toString().padLeft(2, '0')}:${at.minute.toString().padLeft(2, '0')}';
  final day = DateTime(at.year, at.month, at.day);
  final today = DateTime(now.year, now.month, now.day);
  if (day == today) return hm;
  if (day == DateTime(now.year, now.month, now.day - 1)) return 'вчера $hm';
  return '${at.day.toString().padLeft(2, '0')}.${at.month.toString().padLeft(2, '0')} $hm';
}
