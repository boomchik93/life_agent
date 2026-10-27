import 'package:flutter/material.dart';

import '../models.dart';

Color riskColor(RiskColor c, Brightness b) {
  final dark = b == Brightness.dark;
  return switch (c) {
    RiskColor.green => dark ? const Color(0xFF66BB6A) : const Color(0xFF2E7D32),
    RiskColor.yellow => dark ? const Color(0xFFFFCA28) : const Color(0xFFF9A825),
    RiskColor.red => dark ? const Color(0xFFEF5350) : const Color(0xFFC62828),
    RiskColor.gray => dark ? const Color(0xFF9E9E9E) : const Color(0xFF757575),
  };
}

String riskLabel(RiskColor c) => switch (c) {
      RiskColor.green => 'В норме',
      RiskColor.yellow => 'Внимание',
      RiskColor.red => 'Риск',
      RiskColor.gray => 'Нет данных',
    };

// The icon repeats the colour so the state does not depend on colour vision.
IconData riskIcon(RiskColor c) => switch (c) {
      RiskColor.green => Icons.check_circle,
      RiskColor.yellow => Icons.error,
      RiskColor.red => Icons.warning,
      RiskColor.gray => Icons.help,
    };

const _metricNames = {
  'week_hours': 'Учёба за неделю, ч',
  'max_day_hours': 'Больше всего учёбы за день, ч',
  'max_streak_hours': 'Дольше всего учёбы подряд, ч',
  'rest_hours': 'Отдых и развлечения за неделю, ч',
  'free_evenings': 'Свободных вечеров',
  'avg_sleep_hours': 'Средний сон, ч',
  'avg_score': 'Среднее энергии и настроения',
};

String _num(double v) =>
    (v == v.roundToDouble() ? v.toStringAsFixed(0) : v.toStringAsFixed(2).replaceFirst(RegExp(r'0+$'), ''))
        .replaceFirst('.', ',');

class RiskIndicator extends StatelessWidget {
  const RiskIndicator({super.key, required this.title, required this.risk});

  final String title;
  final Risk risk;

  @override
  Widget build(BuildContext context) {
    final color = riskColor(risk.color, Theme.of(context).brightness);
    return Semantics(
      label: '$title: ${riskLabel(risk.color)}',
      button: true,
      child: Card(
        margin: EdgeInsets.zero,
        color: color.withValues(alpha: 0.14),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(12),
          side: BorderSide(color: color, width: 1.5),
        ),
        child: InkWell(
          borderRadius: BorderRadius.circular(12),
          onTap: () => showRiskDetails(context, title, risk),
          child: Padding(
            padding: const EdgeInsets.all(12),
            child: Row(
              children: [
                Icon(riskIcon(risk.color), color: color, size: 32),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(title, style: Theme.of(context).textTheme.titleSmall),
                      Text(riskLabel(risk.color),
                          style: Theme.of(context).textTheme.bodySmall?.copyWith(color: color)),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

void showRiskDetails(BuildContext context, String title, Risk risk) {
  showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    builder: (ctx) {
      final color = riskColor(risk.color, Theme.of(ctx).brightness);
      return SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(children: [
                Icon(riskIcon(risk.color), color: color),
                const SizedBox(width: 8),
                Text('$title — ${riskLabel(risk.color)}', style: Theme.of(ctx).textTheme.titleMedium),
              ]),
              const SizedBox(height: 12),
              for (final r in risk.reasons)
                Padding(padding: const EdgeInsets.only(bottom: 6), child: Text(r)),
              if (risk.reasons.isEmpty && risk.color == RiskColor.green) const Text('Всё в пределах нормы.'),
              if (risk.metrics.isNotEmpty) const Divider(height: 24),
              for (final e in risk.metrics.entries)
                Padding(
                  padding: const EdgeInsets.only(bottom: 4),
                  child: Row(children: [
                    Expanded(child: Text(_metricNames[e.key] ?? e.key)),
                    Text(_num(e.value), style: const TextStyle(fontWeight: FontWeight.bold)),
                  ]),
                ),
            ],
          ),
        ),
      );
    },
  );
}

class RiskGrid extends StatelessWidget {
  const RiskGrid({super.key, required this.risks});

  final Risks risks;

  @override
  Widget build(BuildContext context) {
    final items = [
      ('Недосып', risks.sleep),
      ('Перегруз', risks.overload),
      ('Нет отдыха', risks.noRest),
      ('Состояние', risks.state),
    ];
    // Rows that grow with the text size; a fixed grid ratio clipped large fonts.
    Widget row(int i) => IntrinsicHeight(
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Expanded(child: RiskIndicator(title: items[i].$1, risk: items[i].$2)),
              const SizedBox(width: 8),
              Expanded(child: RiskIndicator(title: items[i + 1].$1, risk: items[i + 1].$2)),
            ],
          ),
        );
    return Column(children: [row(0), const SizedBox(height: 8), row(2)]);
  }
}

/// A risk before and after a change, side by side.
class RiskChange extends StatelessWidget {
  const RiskChange({super.key, required this.title, required this.before, required this.after, required this.worsened});

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
