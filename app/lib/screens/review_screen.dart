import 'package:flutter/material.dart';

import '../api.dart';
import '../format.dart';
import '../models.dart';
import '../review.dart';
import '../widgets/error_view.dart';
import 'plan_screen.dart';

/// The weekly review: four equal goals side by side, the rest of the week, the
/// biggest gaps. Norms are suggested by the server and confirmed here once.
class ReviewScreen extends StatefulWidget {
  const ReviewScreen({super.key, required this.api});

  final LifeApi api;

  @override
  State<ReviewScreen> createState() => _ReviewScreenState();
}

class _ReviewScreenState extends State<ReviewScreen> {
  Wall? _start; // null: the current week
  Review? _review;
  Goals? _goals;
  Object? _error;
  bool _loading = true, _busy = false;
  String? _message;

  @override
  void initState() {
    super.initState();
    // The answers kept from the last start show at once, while they are renewed.
    final api = widget.api;
    if (api is CachedReads) {
      final goals = (api as CachedReads).keptGoals(), review = (api as CachedReads).keptReview();
      if (goals != null && review != null) {
        _goals = goals.value;
        _review = review.value;
      }
    }
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      // The goals first: they may make a suggestion the review then uses.
      final goals = await widget.api.goals();
      final review = await widget.api.reviewWeek(start: _start);
      if (!mounted) return;
      setState(() {
        _goals = goals;
        _review = review;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = e);
    }
    if (mounted) setState(() => _loading = false);
  }

  void _move(int weeks) {
    final shown = _review;
    if (shown == null) return;
    _start = shown.weekStart.addDays(7 * weeks);
    _load();
  }

  Future<void> _answer(String goal, String action, {int? normMin}) async {
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      await widget.api.answerGoal(goal, action, normMin: normMin);
      await _load();
    } on ApiException catch (e) {
      if (mounted) setState(() => _message = e.message);
    }
    if (mounted) setState(() => _busy = false);
  }

  Future<void> _setNorm(ReviewGoal g) async {
    final hours = await showDialog<double>(
      context: context,
      builder: (ctx) => _NormDialog(title: g.title, current: g.normMin),
    );
    if (hours == null) return;
    if (hours <= 0) {
      await _answer(g.goal, 'clear');
    } else {
      await _answer(g.goal, 'set', normMin: (hours * 60).round());
    }
  }

  Future<void> _planGoals() async {
    final written = await Navigator.of(context).push<bool>(
      MaterialPageRoute(builder: (_) => PlanScreen(api: widget.api, goals: true)),
    );
    if (written == true) _load();
  }

  @override
  Widget build(BuildContext context) {
    final review = _review;
    return Scaffold(
      appBar: AppBar(
        title: Text(review == null ? 'Обзор недели' : weekRange(review.weekStart, review.weekEnd.addDays(1))),
        actions: [
          IconButton(tooltip: 'Прошлая неделя', icon: const Icon(Icons.chevron_left), onPressed: () => _move(-1)),
          IconButton(tooltip: 'Следующая неделя', icon: const Icon(Icons.chevron_right), onPressed: () => _move(1)),
        ],
      ),
      body: _loading && review == null
          ? const Center(child: CircularProgressIndicator())
          : _error != null && review == null
              ? ErrorView(error: _error!, onRetry: _load)
              : _body(review!, _goals!),
    );
  }

  Widget _body(Review r, Goals goals) {
    final theme = Theme.of(context);
    final suggestions = [for (final g in goals.goals) if (g.suggestedMin != null) g];
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (_message != null)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: Text(_message!, style: TextStyle(color: theme.colorScheme.error)),
            ),
          for (final w in r.warnings)
            Card(
              color: theme.colorScheme.errorContainer,
              child: Padding(
                padding: const EdgeInsets.all(12),
                child: Row(children: [
                  const Icon(Icons.info_outline),
                  const SizedBox(width: 8),
                  Expanded(child: Text(w)),
                ]),
              ),
            ),
          for (final g in suggestions) _SuggestionCard(goal: g, busy: _busy, onAnswer: (a) => _answer(g.goal, a)),
          if (goals.goals.every((g) => g.normMin == null) && suggestions.isEmpty)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: Text(
                goals.dataDays < goals.suggestAfterDays
                    ? 'Нормы пока нет, показан только факт. Через ${goals.suggestAfterDays} дней данных сервер предложит норму '
                        '(сейчас данных: ${goals.dataDays} дн.). Её можно задать и самому: нажми на цель.'
                    : 'Нормы пока нет, показан только факт. Нажми на цель, чтобы задать норму.',
                style: theme.textTheme.bodySmall,
              ),
            ),
          // The four goals, equal: the same size, the order of the plan.
          _GoalGrid(goals: r.goals, onTap: _setNorm),
          if (r.balanceMin != null) ...[
            const SizedBox(height: 8),
            Text('Баланс целей: меньше всего выполнено ${r.balanceMin}%, разброс ${r.balanceSpread} п. п.',
                style: theme.textTheme.bodySmall),
          ],
          if (r.gaps.isNotEmpty) ...[
            const SizedBox(height: 16),
            Text('Больше всего не хватает', style: theme.textTheme.titleSmall),
            const SizedBox(height: 4),
            for (final g in r.gaps)
              ListTile(
                contentPadding: EdgeInsets.zero,
                dense: true,
                leading: const Icon(Icons.trending_down),
                title: Text(g.title),
                trailing: Text(minutesLabel(g.missingMin)),
              ),
          ],
          const SizedBox(height: 16),
          Text('Остальная неделя', style: theme.textTheme.titleSmall),
          const SizedBox(height: 4),
          _line('Отдых и развлечения', minutesLabel(r.leisureMin)),
          _line('Общение', minutesLabel(r.socialMin)),
          _line(
            'Сон по данным часов',
            r.sleepDays == 0 ? 'нет данных' : '${minutesLabel(r.sleepMin)} за ${r.sleepDays} дн.',
          ),
          _line('Не опознано', '${r.unknownPercent}%'),
          _line('Ночью за экраном', minutesLabel(r.nightMin)),
          if (r.contributions > 0) _line('Вклад в GitHub и LeetCode', '${r.contributions}'),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: _busy ? null : _planGoals,
            icon: const Icon(Icons.auto_awesome),
            label: const Text('Разложить недостающее по неделе'),
          ),
        ],
      ),
    );
  }

  Widget _line(String label, String value) => Padding(
        padding: const EdgeInsets.symmetric(vertical: 3),
        child: Row(children: [
          Expanded(child: Text(label)),
          Text(value, style: const TextStyle(fontWeight: FontWeight.bold)),
        ]),
      );
}

class _GoalGrid extends StatelessWidget {
  const _GoalGrid({required this.goals, required this.onTap});

  final List<ReviewGoal> goals;
  final void Function(ReviewGoal) onTap;

  @override
  Widget build(BuildContext context) {
    // Rows that grow with the text size, like the risk grid.
    Widget row(int i) => IntrinsicHeight(
          child: Row(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            Expanded(child: _GoalCard(goal: goals[i], onTap: () => onTap(goals[i]))),
            const SizedBox(width: 8),
            Expanded(child: _GoalCard(goal: goals[i + 1], onTap: () => onTap(goals[i + 1]))),
          ]),
        );
    return Column(children: [row(0), const SizedBox(height: 8), row(2)]);
  }
}

class _GoalCard extends StatelessWidget {
  const _GoalCard({required this.goal, required this.onTap});

  final ReviewGoal goal;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final norm = goal.normMin;
    final percent = goal.percent;
    return Semantics(
      label: '${goal.title}: ${minutesLabel(goal.factMin)}'
          '${norm == null ? ', норма не задана' : ' из ${minutesLabel(norm)}, $percent%'}',
      button: true,
      excludeSemantics: true,
      child: Card(
        margin: EdgeInsets.zero,
        child: InkWell(
          borderRadius: BorderRadius.circular(12),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.all(12),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [
              Text(goal.title, style: theme.textTheme.titleSmall),
              const SizedBox(height: 4),
              Text(minutesLabel(goal.factMin), style: theme.textTheme.titleMedium),
              const SizedBox(height: 4),
              if (norm != null) ...[
                LinearProgressIndicator(value: ((percent ?? 0) / 100).clamp(0.0, 1.0)),
                const SizedBox(height: 4),
                Text('$percent% от ${minutesLabel(norm)}', style: theme.textTheme.bodySmall),
              ] else
                Text('норма не задана', style: theme.textTheme.bodySmall?.copyWith(color: theme.hintColor)),
            ]),
          ),
        ),
      ),
    );
  }
}

class _SuggestionCard extends StatelessWidget {
  const _SuggestionCard({required this.goal, required this.busy, required this.onAnswer});

  final GoalInfo goal;
  final bool busy;
  final void Function(String action) onAnswer;

  @override
  Widget build(BuildContext context) {
    final has = goal.normMin != null;
    return Card(
      color: Theme.of(context).colorScheme.primaryContainer,
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text(
            has
                ? '${goal.title}: пересмотр нормы. Было ${minutesLabel(goal.normMin!)} в неделю, '
                    'по последним неделям выходит ${minutesLabel(goal.suggestedMin!)}.'
                : '${goal.title}: предлагаю норму ${minutesLabel(goal.suggestedMin!)} в неделю '
                    '(середина по прошлым неделям и 10%).',
          ),
          const SizedBox(height: 8),
          Wrap(spacing: 8, children: [
            FilledButton(onPressed: busy ? null : () => onAnswer('confirm'), child: const Text('Принять')),
            OutlinedButton(
              onPressed: busy ? null : () => onAnswer(has ? 'keep' : 'clear'),
              child: Text(has ? 'Оставить как было' : 'Без нормы'),
            ),
          ]),
        ]),
      ),
    );
  }
}

/// Asks for a weekly norm in hours; 0 removes the norm.
class _NormDialog extends StatefulWidget {
  const _NormDialog({required this.title, this.current});

  final String title;
  final int? current;

  @override
  State<_NormDialog> createState() => _NormDialogState();
}

class _NormDialogState extends State<_NormDialog> {
  late final _c = TextEditingController(
      text: widget.current == null ? '' : (widget.current! / 60).toStringAsFixed(1).replaceFirst(RegExp(r'\.0$'), ''));
  String? _error;

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  void _ok() {
    final v = double.tryParse(_c.text.trim().replaceAll(',', '.'));
    if (v == null || v < 0 || v > 168) {
      setState(() => _error = 'Часов в неделю: от 0 до 168. 0 убирает норму.');
      return;
    }
    Navigator.pop(context, v);
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text('Норма: ${widget.title}'),
      content: TextField(
        controller: _c,
        autofocus: true,
        keyboardType: const TextInputType.numberWithOptions(decimal: true),
        decoration: InputDecoration(labelText: 'Часов в неделю', errorText: _error, border: const OutlineInputBorder()),
        onSubmitted: (_) => _ok(),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context), child: const Text('Отмена')),
        FilledButton(onPressed: _ok, child: const Text('Сохранить')),
      ],
    );
  }
}
