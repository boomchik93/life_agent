import 'dart:convert';

import 'package:flutter/material.dart';

import '../api.dart';
import '../card.dart';
import '../format.dart';
import '../models.dart';
import '../slot.dart';
import '../widgets/error_view.dart';
import '../widgets/risk_indicator.dart';

const _riskTitles = {
  'sleep': 'Недосып',
  'overload': 'Перегруз',
  'no_rest': 'Нет отдыха',
  'state': 'Состояние',
};

/// The server's answer to "can this event be added", and the write itself.
/// Pops `true` once the event is in the calendar.
class VerdictScreen extends StatefulWidget {
  const VerdictScreen({super.key, required this.api, required this.slot});

  final LifeApi api;
  final SlotRequest slot;

  @override
  State<VerdictScreen> createState() => _VerdictScreenState();
}

class _VerdictScreenState extends State<VerdictScreen> {
  late SlotRequest _slot = widget.slot;
  final _keys = IdempotencyKeys();
  CheckResult? _result;
  Object? _error; // the check failed
  String? _writeError;
  bool _loading = true;
  bool _writing = false;

  @override
  void initState() {
    super.initState();
    _check();
  }

  Future<void> _check() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final r = await widget.api.check(_slot);
      if (!mounted) return;
      setState(() => _result = r);
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e);
    }
    if (mounted) setState(() => _loading = false);
  }

  void _setCategory(String category) {
    if (_writing) return;
    _slot = _slot.copyWith(category: category, categoryConfirmed: true);
    _writeError = null;
    _check();
  }

  Future<void> _write() async {
    final r = _result;
    if (r == null || _writing) return;
    final slot = _slot.copyWith(confirmed: r.needsConfirm);
    setState(() {
      _writing = true;
      _writeError = null;
    });
    try {
      await widget.api.writeEvent(slot, key: _keys.forPayload(jsonEncode(slot.toJson())));
      if (mounted) Navigator.of(context).pop(true);
      return;
    } on NeedsConfirm catch (e) {
      // The calendar changed since the check: show the new picture and ask again.
      if (mounted) setState(() => _result = e.check);
    } on ApiException catch (e) {
      if (mounted) setState(() => _writeError = e.message);
    }
    if (mounted) setState(() => _writing = false);
  }

  @override
  Widget build(BuildContext context) {
    final r = _result;
    return Scaffold(
      appBar: AppBar(title: const Text('Можно ли добавить')),
      body: Column(
        children: [
          if (_loading) const LinearProgressIndicator(),
          Expanded(child: _content(r)),
          if (r != null) _BottomBar(
            result: r,
            busy: _loading || _writing,
            writing: _writing,
            error: _writeError,
            onWrite: _write,
          ),
        ],
      ),
    );
  }

  Widget _content(CheckResult? r) {
    if (r == null) {
      if (_error != null) return ErrorView(error: _error!, onRetry: _check);
      return const Center(child: Text('Проверяю слот и нагрузку…'));
    }
    final theme = Theme.of(context);
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        _Summary(slot: _slot),
        const SizedBox(height: 12),
        _Headline(result: r),
        if (_error != null)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text('Обновить проверку не удалось: ${(_error as ApiException).message}',
                style: TextStyle(color: theme.colorScheme.error)),
          ),
        const SizedBox(height: 12),
        _CategoryCard(
          result: r,
          onPick: _setCategory,
        ),
        if (r.reasons.isNotEmpty) ...[
          const SizedBox(height: 12),
          Text('Что не так', style: theme.textTheme.titleSmall),
          const SizedBox(height: 6),
          for (final reason in r.reasons)
            Padding(
              padding: const EdgeInsets.only(bottom: 6),
              child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
                const Padding(padding: EdgeInsets.only(top: 2, right: 8), child: Icon(Icons.warning_amber, size: 18)),
                Expanded(child: Text(reason)),
              ]),
            ),
        ],
        const SizedBox(height: 12),
        Text('Нагрузка недели', style: theme.textTheme.titleSmall),
        const SizedBox(height: 6),
        for (final key in _riskTitles.keys)
          _RiskChange(
            title: _riskTitles[key]!,
            before: _risk(r.before, key),
            after: _risk(r.after, key),
            worsened: r.worsened.contains(key),
          ),
        if (r.recurrenceNote != null)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(r.recurrenceNote!, style: theme.textTheme.bodySmall?.copyWith(color: theme.hintColor)),
          ),
      ],
    );
  }

  Risk _risk(Risks r, String key) => switch (key) {
        'sleep' => r.sleep,
        'overload' => r.overload,
        'no_rest' => r.noRest,
        _ => r.state,
      };
}

class _Summary extends StatelessWidget {
  const _Summary({required this.slot});

  final SlotRequest slot;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(slot.title, style: theme.textTheme.titleLarge),
        const SizedBox(height: 4),
        Text('${dayTitle(slot.start)} · ${slot.start.hm}–${slot.end.hm}'
            '${slot.recurrence == null ? '' : ' · повторяется ${recurrenceText(slot.recurrence!)}'}'),
      ],
    );
  }
}

class _Headline extends StatelessWidget {
  const _Headline({required this.result});

  final CheckResult result;

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness;
    final (RiskColor color, String text, String sub) = result.ok
        ? (RiskColor.green, 'Можно добавить', 'Пересечений нет, нагрузка в пределах нормы.')
        : result.needsConfirm
            ? (RiskColor.red, 'Лучше не добавлять', 'Нужно твоё подтверждение, чтобы записать.')
            : (RiskColor.yellow, 'Можно, но есть замечание', 'Нагрузка вырастет, но красного риска нет.');
    final c = riskColor(color, dark);
    return Semantics(
      container: true,
      label: text,
      child: Card(
        margin: EdgeInsets.zero,
        color: c.withValues(alpha: 0.14),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12), side: BorderSide(color: c, width: 1.5)),
        child: Padding(
          padding: const EdgeInsets.all(14),
          child: Row(children: [
            Icon(riskIcon(color), color: c, size: 36),
            const SizedBox(width: 12),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(text, style: Theme.of(context).textTheme.titleMedium),
                Text(sub),
              ]),
            ),
          ]),
        ),
      ),
    );
  }
}

class _CategoryCard extends StatelessWidget {
  const _CategoryCard({required this.result, required this.onPick});

  final CheckResult result;
  final ValueChanged<String> onPick;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final name = categoryNames[result.category] ?? result.category;
    return Card(
      margin: EdgeInsets.zero,
      color: result.askCategory ? theme.colorScheme.tertiaryContainer : null,
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(result.askCategory ? 'Категория верная: «$name»?' : 'Категория', style: theme.textTheme.titleSmall),
            if (result.askCategory)
              const Padding(
                padding: EdgeInsets.only(top: 2),
                child: Text('От неё зависит оценка нагрузки. Спрашиваю один раз, дальше запомню.'),
              )
            else if (result.categorySource == 'memory')
              const Padding(padding: EdgeInsets.only(top: 2), child: Text('Взята из памяти: ты отвечал раньше.')),
            const SizedBox(height: 8),
            Wrap(spacing: 8, runSpacing: 4, children: [
              for (final e in categoryNames.entries)
                ChoiceChip(
                  label: Text(e.value),
                  selected: result.category == e.key,
                  onSelected: (_) => onPick(e.key),
                ),
            ]),
            if (result.askCategory)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: FilledButton.tonal(
                  onPressed: () => onPick(result.category),
                  child: Text('Да, «$name»'),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _RiskChange extends StatelessWidget {
  const _RiskChange({required this.title, required this.before, required this.after, required this.worsened});

  final String title;
  final Risk before, after;
  final bool worsened;

  @override
  Widget build(BuildContext context) {
    final b = Theme.of(context).brightness;
    Widget state(Risk r) => Row(mainAxisSize: MainAxisSize.min, children: [
          Icon(riskIcon(r.color), color: riskColor(r.color, b), size: 20),
          const SizedBox(width: 4),
          Text(riskLabel(r.color)),
        ]);
    return Semantics(
      label: '$title: было ${riskLabel(before.color)}, станет ${riskLabel(after.color)}',
      excludeSemantics: true,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 6),
        child: Row(children: [
          Expanded(
            child: Text(title, style: worsened ? const TextStyle(fontWeight: FontWeight.bold) : null),
          ),
          state(before),
          const Padding(padding: EdgeInsets.symmetric(horizontal: 6), child: Icon(Icons.arrow_forward, size: 16)),
          state(after),
        ]),
      ),
    );
  }
}

class _BottomBar extends StatelessWidget {
  const _BottomBar({
    required this.result,
    required this.busy,
    required this.writing,
    required this.error,
    required this.onWrite,
  });

  final CheckResult result;
  final bool busy, writing;
  final String? error;
  final VoidCallback onWrite;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final risky = result.needsConfirm;
    return Material(
      elevation: 6,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 10),
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            if (error != null)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Row(children: [
                  Icon(Icons.error, color: theme.colorScheme.error),
                  const SizedBox(width: 8),
                  Expanded(child: Text(error!)),
                ]),
              ),
            if (result.askCategory)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Text('Сначала подтверди категорию.', style: TextStyle(color: theme.hintColor)),
              ),
            Row(children: [
              Expanded(
                child: OutlinedButton(
                  onPressed: writing ? null : () => Navigator.of(context).pop(false),
                  child: const Text('Изменить'),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                flex: 2,
                child: FilledButton(
                  style: risky
                      ? FilledButton.styleFrom(
                          backgroundColor: theme.colorScheme.error, foregroundColor: theme.colorScheme.onError)
                      : null,
                  onPressed: busy || result.askCategory ? null : onWrite,
                  child: writing
                      ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                      : Text(risky ? 'Всё равно записать' : 'Записать в календарь'),
                ),
              ),
            ]),
          ]),
        ),
      ),
    );
  }
}
