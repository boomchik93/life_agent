import 'dart:async';

import 'package:flutter/material.dart';

import '../advice.dart';
import '../api.dart';
import '../background.dart';
import '../format.dart';
import '../notifications.dart';
import '../settings.dart';

/// The history of advice, the button to ask for one, and the two switches:
/// notifications and the private data in the advice.
class AdviceScreen extends StatefulWidget {
  const AdviceScreen({
    super.key,
    required this.api,
    required this.storage,
    required this.scheduler,
    required this.notifier,
    this.askNow = false,
  });

  final LifeApi api;
  final AppStorage storage;
  final SyncScheduler scheduler;
  final Notifier notifier;

  /// Ask for an advice as soon as the screen opens (the phrase "дай совет").
  final bool askNow;

  @override
  State<AdviceScreen> createState() => _AdviceScreenState();
}

class _AdviceScreenState extends State<AdviceScreen> {
  List<Advice>? _history;

  /// Advice asked for on this screen. Kept apart from the loaded history so a
  /// slow load cannot wipe an advice that arrived first.
  final _fresh = <Advice>[];
  Object? _loadError;
  bool _asking = false, _notify = false, _private = false, _busy = false;
  String? _askError;

  @override
  void initState() {
    super.initState();
    _load();
    if (widget.askNow) _ask();
  }

  Future<void> _load() async {
    try {
      final history = await widget.api.adviceHistory();
      final set = await widget.api.adviceSettings();
      final notify = await widget.storage.adviceNotifyEnabled();
      if (!mounted) return;
      setState(() {
        _history = history;
        _private = set.includePrivate;
        _notify = notify;
        _loadError = null;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _loadError = e);
    }
  }

  Future<void> _ask() async {
    if (_asking) return;
    setState(() {
      _asking = true;
      _askError = null;
    });
    try {
      final advice = await widget.api.requestAdvice();
      if (!mounted) return;
      setState(() => _fresh.insert(0, advice));
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _askError = e.message);
    } catch (_) {
      if (!mounted) return;
      setState(() => _askError = 'Что-то пошло не так. Попробуй ещё раз.');
    }
    if (mounted) setState(() => _asking = false);
  }

  Future<void> _setNotify(bool on) async {
    setState(() => _busy = true);
    try {
      if (on && !await widget.notifier.requestPermission()) {
        if (mounted) {
          setState(() => _askError = 'Уведомления запрещены в настройках телефона.');
        }
        return;
      }
      await widget.storage.saveAdviceNotifyEnabled(on);
      await applyBackground(widget.storage, widget.scheduler);
      if (mounted) setState(() => _notify = on);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _setPrivate(bool on) async {
    if (on) {
      final ok = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Отправлять сон и чек-ин?'),
          content: const Text(
            'В запрос к модели добавятся средний сон и среднее энергии и настроения. '
            'Бесплатные провайдеры могут сохранять запросы и учиться на них. '
            'Без этого переключателя модель видит только часы событий и цвета нагрузки.',
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Не отправлять'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(ctx, true),
              child: const Text('Отправлять'),
            ),
          ],
        ),
      );
      if (ok != true || !mounted) return;
    }
    setState(() => _busy = true);
    try {
      await widget.api.saveAdviceSettings(AdviceSettings(includePrivate: on));
      if (mounted) setState(() => _private = on);
    } on ApiException catch (e) {
      if (mounted) setState(() => _askError = e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final loaded = _history;
    final history = loaded == null && _fresh.isEmpty
        ? null
        : [..._fresh, for (final a in loaded ?? const <Advice>[]) if (!_fresh.any((f) => f.id == a.id)) a];
    return Scaffold(
      appBar: AppBar(title: const Text('Совет')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          const Text(
            'Совет приходит, когда нагрузка выше нормы, не хватает сна или плохое состояние, '
            'не чаще раза в неделю. Можно попросить самому.',
          ),
          const SizedBox(height: 12),
          FilledButton.icon(
            onPressed: _asking ? null : _ask,
            icon: _asking
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.lightbulb_outline),
            label: Text(_asking ? 'Готовлю совет…' : 'Попросить совет'),
          ),
          if (_askError != null)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Row(
                children: [
                  Icon(Icons.error, color: theme.colorScheme.error),
                  const SizedBox(width: 8),
                  Expanded(child: Text(_askError!)),
                ],
              ),
            ),
          const SizedBox(height: 8),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('Уведомления о советах'),
            subtitle: const Text(
              'Телефон раз в 6 часов спрашивает сервер, есть ли готовый совет.',
            ),
            value: _notify,
            onChanged: _busy ? null : _setNotify,
          ),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('Отправлять в совет сон и чек-ин'),
            subtitle: const Text(
              'Запрос может сохраняться у провайдера модели. По умолчанию выключено.',
            ),
            value: _private,
            onChanged: _busy ? null : _setPrivate,
          ),
          const Divider(height: 32),
          Text('История', style: theme.textTheme.titleSmall),
          const SizedBox(height: 8),
          if (_loadError != null)
            Row(
              children: [
                Expanded(
                  child: Text(
                    _loadError is ApiException
                        ? (_loadError as ApiException).message
                        : 'Не удалось загрузить историю.',
                  ),
                ),
                TextButton(onPressed: _load, child: const Text('Повторить')),
              ],
            )
          else if (history == null)
            const Center(
              child: Padding(
                padding: EdgeInsets.all(16),
                child: CircularProgressIndicator(),
              ),
            )
          else if (history.isEmpty)
            const Text('Советов пока не было.')
          else
            for (final a in history) _AdviceTile(advice: a),
        ],
      ),
    );
  }
}

class _AdviceTile extends StatelessWidget {
  const _AdviceTile({required this.advice});

  final Advice advice;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final why = advice.asked ? 'По запросу' : 'Сам: ${advice.reasonText}';
    return Card(
      margin: const EdgeInsets.only(bottom: 8),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '${shortDate(advice.at)}, ${advice.at.hm} · $why',
              style: theme.textTheme.bodySmall,
            ),
            const SizedBox(height: 6),
            Text(advice.text),
          ],
        ),
      ),
    );
  }
}
