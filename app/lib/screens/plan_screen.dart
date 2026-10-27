import 'dart:convert';

import 'package:flutter/material.dart';

import '../api.dart';
import '../card.dart';
import '../format.dart';
import '../models.dart';
import '../plan.dart';
import '../slot.dart';
import '../widgets/error_view.dart';
import '../widgets/risk_indicator.dart';

const _riskTitles = ['Недосып', 'Перегруз', 'Нет отдыха', 'Состояние'];

List<Risk> _riskList(Risks r) => [r.sleep, r.overload, r.noRest, r.state];

/// Builds the week plan, shows it and writes it to the calendar after the
/// user confirms. Pops `true` once the plan is written.
class PlanScreen extends StatefulWidget {
  const PlanScreen({super.key, required this.api});

  final LifeApi api;

  @override
  State<PlanScreen> createState() => _PlanScreenState();
}

class _PlanScreenState extends State<PlanScreen> {
  final _keys = IdempotencyKeys();
  PlanData? _plan;
  bool _building = false, _applying = false, _applied = false;
  int _appliedCount = 0;
  Object? _buildError;
  String? _applyError;

  Future<void> _build() async {
    setState(() {
      _building = true;
      _buildError = null;
      _applyError = null;
    });
    try {
      final plan = await widget.api.planWeek();
      if (!mounted) return;
      setState(() {
        _plan = plan;
        _applied = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _buildError = e);
    } finally {
      if (mounted) setState(() => _building = false);
    }
  }

  Future<void> _apply() async {
    final plan = _plan;
    if (plan == null || plan.placed.isEmpty) return;
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Записать план в календарь?'),
        content: Text('В календарь добавится событий: ${plan.placed.length}. '
            'Существующие события не изменятся.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Отмена')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Записать')),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    setState(() {
      _applying = true;
      _applyError = null;
    });
    try {
      final res = await widget.api.applyPlan(plan, key: _keys.forPayload(jsonEncode(plan.applyJson())));
      if (!mounted) return;
      setState(() {
        _applied = true;
        _appliedCount = res.count;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _applyError = e.message);
    } catch (_) {
      if (!mounted) return;
      setState(() => _applyError = 'Что-то пошло не так. Попробуй ещё раз.');
    } finally {
      if (mounted) setState(() => _applying = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final plan = _plan;
    return Scaffold(
      appBar: AppBar(title: const Text('План недели')),
      body: _body(),
      bottomNavigationBar: plan == null ? null : _BottomBar(
        plan: plan,
        applied: _applied,
        appliedCount: _appliedCount,
        applying: _applying,
        building: _building,
        error: _applyError,
        onApply: _apply,
        onRebuild: _build,
        onDone: () => Navigator.of(context).pop(_applied),
      ),
    );
  }

  Widget _body() {
    if (_building && _plan == null) return const Center(child: CircularProgressIndicator());
    if (_buildError != null && _plan == null) return ErrorView(error: _buildError!, onRetry: _build);
    final plan = _plan;
    if (plan == null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            const Icon(Icons.auto_awesome, size: 48),
            const SizedBox(height: 12),
            const Text(
              'Разложу задачи по свободным окнам недели: без сна, без пересечений, '
              'с перерывами и свободными вечерами. В календарь ничего не запишется, '
              'пока ты не подтвердишь.',
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 16),
            FilledButton(onPressed: _build, child: const Text('Собрать неделю')),
          ]),
        ),
      );
    }
    return _PlanView(plan: plan, buildError: _buildError);
  }
}

class _PlanView extends StatelessWidget {
  const _PlanView({required this.plan, this.buildError});

  final PlanData plan;
  final Object? buildError;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final total = plan.placed.fold<int>(0, (s, c) => s + c.minutes);
    final byDay = <Wall, List<PlanChunk>>{};
    for (final c in plan.placed) {
      byDay.putIfAbsent(c.start.dayStart, () => []).add(c);
    }
    final days = byDay.keys.toList()..sort((a, b) => a.isBefore(b) ? -1 : 1);

    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        Text(weekRange(plan.from, plan.to), style: theme.textTheme.titleMedium),
        const SizedBox(height: 4),
        Text(plan.placed.isEmpty
            ? 'Размещать нечего.'
            : 'Задач разложено на ${minutesLabel(total)}, кусков: ${plan.placed.length}.'),
        if (buildError != null)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(
              'Обновить не удалось: ${buildError is ApiException ? (buildError as ApiException).message : 'ошибка'}',
              style: TextStyle(color: theme.colorScheme.error),
            ),
          ),
        if (plan.violations.isNotEmpty) ...[
          const SizedBox(height: 12),
          Card(
            color: theme.colorScheme.errorContainer,
            child: Padding(
              padding: const EdgeInsets.all(12),
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text('План нарушает правила, записывать его нельзя:',
                    style: theme.textTheme.titleSmall),
                for (final v in plan.violations) Text('• $v'),
              ]),
            ),
          ),
        ],
        if (plan.isEmpty) ...[
          const SizedBox(height: 16),
          const Text('Открытых задач с дедлайном нет. Добавь задачу фразой на главном экране, '
              'например «курсовая 6 часов до пятницы».'),
        ],
        for (final d in days) ...[
          const SizedBox(height: 16),
          Text(dayTitle(d), style: theme.textTheme.titleSmall),
          const SizedBox(height: 4),
          for (final c in byDay[d]!) _ChunkTile(chunk: c),
        ],
        if (plan.unplaced.isNotEmpty) ...[
          const SizedBox(height: 20),
          Text('Не поместилось', style: theme.textTheme.titleSmall),
          const SizedBox(height: 4),
          for (final u in plan.unplaced)
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: Icon(Icons.warning_amber, color: theme.colorScheme.error),
              title: Text(u.title),
              subtitle: Text('Не хватает ${minutesLabel(u.missingMin)} до дедлайна. '
                  'Правила ради этой задачи не нарушаются.'),
            ),
        ],
        if (plan.deferred.isNotEmpty) ...[
          const SizedBox(height: 20),
          Text('Дедлайн позже этой недели', style: theme.textTheme.titleSmall),
          const SizedBox(height: 4),
          for (final d in plan.deferred)
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.schedule),
              title: Text(d.title),
              subtitle: Text('До ${d.deadline}. Попадёт в план своей недели.'),
            ),
        ],
        if (plan.placed.isNotEmpty) ...[
          const SizedBox(height: 20),
          Text('Нагрузка недели', style: theme.textTheme.titleSmall),
          const SizedBox(height: 6),
          for (var i = 0; i < _riskTitles.length; i++)
            RiskChange(
              title: _riskTitles[i],
              before: _riskList(plan.before)[i],
              after: _riskList(plan.after)[i],
              worsened: _worse(_riskList(plan.before)[i].color, _riskList(plan.after)[i].color),
            ),
        ],
      ],
    );
  }

  static int _rank(RiskColor c) => switch (c) {
        RiskColor.yellow => 2,
        RiskColor.red => 3,
        _ => 1,
      };

  static bool _worse(RiskColor before, RiskColor after) =>
      (after == RiskColor.yellow || after == RiskColor.red) && _rank(after) > _rank(before);
}

class _ChunkTile extends StatelessWidget {
  const _ChunkTile({required this.chunk});

  final PlanChunk chunk;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: '${chunk.start.hm} до ${chunk.end.hm}, ${chunk.title}',
      excludeSemantics: true,
      child: ListTile(
        contentPadding: EdgeInsets.zero,
        dense: true,
        leading: Text('${chunk.start.hm}–${chunk.end.hm}', style: const TextStyle(fontFeatures: [FontFeature.tabularFigures()])),
        title: Text(chunk.title),
        subtitle: Text('${categoryNames[chunk.category] ?? chunk.category}, ${minutesLabel(chunk.minutes)}'),
      ),
    );
  }
}

class _BottomBar extends StatelessWidget {
  const _BottomBar({
    required this.plan,
    required this.applied,
    required this.appliedCount,
    required this.applying,
    required this.building,
    required this.error,
    required this.onApply,
    required this.onRebuild,
    required this.onDone,
  });

  final PlanData plan;
  final bool applied, applying, building;
  final int appliedCount;
  final String? error;
  final VoidCallback onApply, onRebuild, onDone;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final busy = applying || building;
    return Material(
      elevation: 6,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 10),
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            if (applied)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Row(children: [
                  const Icon(Icons.check_circle),
                  const SizedBox(width: 8),
                  Expanded(child: Text('Записано в календарь: $appliedCount')),
                ]),
              ),
            if (error != null)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Row(children: [
                  Icon(Icons.error, color: theme.colorScheme.error),
                  const SizedBox(width: 8),
                  Expanded(child: Text(error!)),
                ]),
              ),
            if (applied)
              SizedBox(width: double.infinity, child: FilledButton(onPressed: onDone, child: const Text('Готово')))
            else
              Row(children: [
                Expanded(
                  child: OutlinedButton(
                    onPressed: busy ? null : onRebuild,
                    child: building
                        ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                        : const Text('Собрать заново'),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  flex: 2,
                  child: FilledButton(
                    onPressed: busy || plan.placed.isEmpty || plan.violations.isNotEmpty ? null : onApply,
                    child: applying
                        ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                        : const Text('Применить'),
                  ),
                ),
              ]),
          ]),
        ),
      ),
    );
  }
}
