import 'package:flutter/material.dart';

import '../api.dart';
import '../background.dart';
import '../health.dart';
import '../settings.dart';

/// Health Connect access, the background sync and deleting the summaries.
class HealthScreen extends StatefulWidget {
  const HealthScreen({
    super.key,
    required this.api,
    required this.storage,
    required this.source,
    required this.scheduler,
    this.now,
  });

  final LifeApi api;
  final AppStorage storage;
  final HealthSource source;
  final SyncScheduler scheduler;
  final DateTime Function()? now;

  @override
  State<HealthScreen> createState() => _HealthScreenState();
}

class _HealthScreenState extends State<HealthScreen> {
  HealthStatus? _status;
  bool _background = false, _syncOn = false, _busy = false;
  String? _message;
  bool _messageOk = true;
  ({String? at, String? note}) _last = (at: null, note: null);

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    HealthStatus status;
    try {
      status = await widget.source.status();
    } catch (_) {
      status = HealthStatus.unsupported;
    }
    var bg = false;
    if (status == HealthStatus.ready) {
      try {
        bg = await widget.source.backgroundAllowed();
      } catch (_) {}
    }
    final on = await widget.storage.healthSyncEnabled();
    final last = await widget.storage.lastHealthSync();
    if (!mounted) return;
    setState(() {
      _status = status;
      _background = bg;
      _syncOn = on;
      _last = last;
    });
  }

  void _say(String text, {bool ok = true}) => setState(() {
        _message = text;
        _messageOk = ok;
      });

  Future<void> _guard(Future<void> Function() body) async {
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      await body();
    } on ApiException catch (e) {
      _say(e.message, ok: false);
    } catch (_) {
      _say('Не получилось. Попробуй ещё раз.', ok: false);
    }
    if (mounted) setState(() => _busy = false);
  }

  Future<void> _grant() => _guard(() async {
        final ok = await widget.source.requestAccess();
        if (!ok) _say('Доступ не выдан. Без него сон и пульс не читаются.', ok: false);
        await _refresh();
      });

  Future<void> _syncNow() => _guard(() async {
        final result = await HealthSync(api: widget.api, source: widget.source, now: widget.now).run();
        final at = (widget.now?.call() ?? DateTime.now()).toIso8601String();
        await widget.storage.saveHealthSync(at, result.skipped);
        if (result.skipped != null) {
          _say(result.skipped!, ok: false);
        } else {
          _say('Отправлено дней: ${result.sent}.');
        }
        await _refresh();
      });

  /// One-off check: does the watch give HRV through Samsung Health? Health
  /// Connect has no stress type at all, so there is nothing to check for it.
  Future<void> _checkHrv() => _guard(() async {
        final days = await widget.source.hrvDays(widget.now?.call() ?? DateTime.now());
        _say(days > 0
            ? 'HRV есть за $days из 7 дней. Стресса в Health Connect нет: его мы не используем.'
            : 'HRV в Health Connect нет (или доступ не выдан). HRV и стресс не используем.');
      });

  Future<void> _toggle(bool on) => _guard(() async {
        await widget.storage.saveHealthSyncEnabled(on);
        await applyBackground(widget.storage, widget.scheduler);
        await _refresh();
      });

  Future<void> _delete() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Удалить данные здоровья?'),
        content: const Text('С сервера удалятся все сводки сна и пульса. Данные в Health Connect останутся.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Отмена')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Удалить')),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    await _guard(() async {
      final n = await widget.api.deleteHealth();
      _say('Удалено сводок: $n.');
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final status = _status;
    return Scaffold(
      appBar: AppBar(title: const Text('Здоровье')),
      body: status == null
          ? const Center(child: CircularProgressIndicator())
          : ListView(
              padding: const EdgeInsets.all(16),
              children: [
                const Text('На сервер уходят только дневные сводки: сон, пульс, пульс покоя, шаги и минуты тренировок. '
                    'Сырые замеры остаются в Health Connect.'),
                const SizedBox(height: 16),
                _statusCard(status),
                if (_message != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: Row(children: [
                      Icon(_messageOk ? Icons.check_circle : Icons.error,
                          color: _messageOk ? Colors.green : theme.colorScheme.error),
                      const SizedBox(width: 8),
                      Expanded(child: Text(_message!)),
                    ]),
                  ),
                if (status == HealthStatus.ready) ...[
                  const SizedBox(height: 12),
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('Отправлять в фоне'),
                    subtitle: Text(_background
                        ? 'Раз в 6 часов, без открытия приложения.'
                        : 'Фоновое чтение не разрешено: сводки уйдут, только когда приложение открыто. '
                            'Разреши «Доступ в фоновом режиме» в Health Connect.'),
                    value: _syncOn,
                    onChanged: _busy ? null : _toggle,
                  ),
                  if (_last.at != null)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 8),
                      child: Text(
                        'Последняя отправка: ${_last.at!.substring(0, 16).replaceFirst('T', ' ')}'
                        '${_last.note == null ? '' : '. ${_last.note}'}',
                        style: theme.textTheme.bodySmall,
                      ),
                    ),
                  OutlinedButton(
                    onPressed: _busy ? null : _syncNow,
                    child: const Text('Отправить сейчас'),
                  ),
                  const SizedBox(height: 8),
                  OutlinedButton(
                    onPressed: _busy ? null : _checkHrv,
                    child: const Text('Проверить HRV и стресс'),
                  ),
                ],
                const Divider(height: 32),
                Text('Чтобы фон не убивался', style: theme.textTheme.titleSmall),
                const SizedBox(height: 6),
                const Text('Samsung One UI закрывает фоновые задачи. Открой Настройки → Приложения → Жизнь → '
                    'Батарея и выбери «Не ограничено». В Настройки → Обслуживание устройства → Батарея → '
                    'Ограничения в фоновом режиме убери «Жизнь» из спящих приложений.'),
                const Divider(height: 32),
                OutlinedButton.icon(
                  style: OutlinedButton.styleFrom(foregroundColor: theme.colorScheme.error),
                  onPressed: _busy ? null : _delete,
                  icon: const Icon(Icons.delete_outline),
                  label: const Text('Удалить данные здоровья'),
                ),
              ],
            ),
    );
  }

  Widget _statusCard(HealthStatus status) {
    final (icon, text, action, onTap) = switch (status) {
      HealthStatus.unsupported => (Icons.block, 'Health Connect доступен только на Android.', null, null),
      HealthStatus.notInstalled => (
          Icons.download,
          'Health Connect не установлен.',
          'Установить',
          () => _guard(() async {
                await widget.source.install();
                await _refresh();
              })
        ),
      HealthStatus.needsAccess => (Icons.lock, 'Нужен доступ к сну и пульсу.', 'Дать доступ', _grant),
      HealthStatus.ready => (Icons.check_circle, 'Доступ выдан.', null, null),
    };
    return Card(
      margin: EdgeInsets.zero,
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Row(children: [
          Icon(icon),
          const SizedBox(width: 12),
          Expanded(child: Text(text)),
          if (action != null) FilledButton(onPressed: _busy ? null : onTap, child: Text(action)),
        ]),
      ),
    );
  }
}
