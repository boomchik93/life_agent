import 'package:flutter/material.dart';

import '../models.dart';
import 'motion.dart';

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
  'measured_sleep_days': 'Дней со сном по данным часов',
  'avg_score': 'Среднее энергии и настроения',
};

String _num(double v) =>
    (v == v.roundToDouble() ? v.toStringAsFixed(0) : v.toStringAsFixed(2).replaceFirst(RegExp(r'0+$'), ''))
        .replaceFirst('.', ',');

class RiskIndicator extends StatelessWidget {
  const RiskIndicator({super.key, required this.title, required this.risk, this.compact = false, this.history});

  final String title;
  final bool compact;
  final Risk risk;

  /// The colours of the last days, oldest first, drawn as small bars.
  final List<RiskColor>? history;

  @override
  Widget build(BuildContext context) {
    final color = riskColor(risk.color, Theme.of(context).brightness);
    final surface = Theme.of(context).colorScheme.surfaceContainer;
    return Semantics(
      label: '$title: ${riskLabel(risk.color)}',
      button: true,
      child: Material(
        color: Colors.transparent,
        borderRadius: BorderRadius.circular(20),
        clipBehavior: Clip.antiAlias,
        child: Ink(
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(20),
            border: Border.all(color: color.withValues(alpha: 0.3)),
            gradient: LinearGradient(
              begin: Alignment.topLeft,
              end: Alignment.bottomRight,
              colors: [
                Color.alphaBlend(color.withValues(alpha: 0.26), surface),
                Color.alphaBlend(color.withValues(alpha: 0.08), surface),
              ],
            ),
          ),
          child: InkWell(
            borderRadius: BorderRadius.circular(20),
            onTap: () => showRiskDetails(context, title, risk),
            child: Padding(
              padding: EdgeInsets.all(compact ? 12 : 14),
              child: compact
                  ? Row(children: [
                      _disc(color),
                      const SizedBox(width: 8),
                      Expanded(child: _texts(context)),
                    ])
                  : Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Row(crossAxisAlignment: CrossAxisAlignment.end, mainAxisAlignment: MainAxisAlignment.spaceBetween, children: [
                          _disc(color),
                          if (history != null && history!.isNotEmpty) _Spark(history!),
                        ]),
                        const SizedBox(height: 10),
                        _texts(context),
                      ],
                    ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Seven small bars: the taller and the warmer, the worse the day was.
class _Spark extends StatelessWidget {
  const _Spark(this.days);

  final List<RiskColor> days;

  @override
  Widget build(BuildContext context) {
    final b = Theme.of(context).brightness;
    return Semantics(
      label: 'Динамика за ${days.length} дней',
      excludeSemantics: true,
      child: SizedBox(
        height: 28,
        child: Row(crossAxisAlignment: CrossAxisAlignment.end, children: [
          for (final c in days)
            Grow(
              value: switch (c) { RiskColor.red => 28.0, RiskColor.yellow => 18.0, RiskColor.green => 9.0, _ => 5.0 },
              builder: (context, h) => Container(
                width: 5,
                height: h,
                margin: const EdgeInsets.only(left: 3),
                decoration: BoxDecoration(color: riskColor(c, b), borderRadius: BorderRadius.circular(2)),
              ),
            ),
        ]),
      ),
    );
  }
}

extension on RiskIndicator {
  Widget _disc(Color color) => Container(
        width: compact ? 32 : 40,
        height: compact ? 32 : 40,
        decoration: BoxDecoration(
          color: color,
          shape: BoxShape.circle,
          boxShadow: [BoxShadow(color: color.withValues(alpha: 0.35), blurRadius: 10, offset: const Offset(0, 3))],
        ),
        child: Icon(riskIcon(risk.color),
            size: 20,
            color: ThemeData.estimateBrightnessForColor(color) == Brightness.dark ? Colors.white : const Color(0xFF1A1A1A)),
      );

  Widget _texts(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        if (compact)
          FittedBox(fit: BoxFit.scaleDown, alignment: Alignment.centerLeft, child: Text(title, style: theme.textTheme.titleSmall))
        else
          Text(title, style: theme.textTheme.titleMedium),
        Text(riskLabel(risk.color),
            style: theme.textTheme.titleSmall?.copyWith(color: _textOn(risk.color, theme.brightness), fontSize: compact ? 14 : null)),
      ],
    );
  }
}

// Darker (light theme) or lighter (dark theme) shade of the risk colour for text.
Color _textOn(RiskColor c, Brightness b) {
  final dark = b == Brightness.dark;
  return switch (c) {
    RiskColor.green => dark ? const Color(0xFFA5DCA9) : const Color(0xFF1B5E20),
    RiskColor.yellow =>
      dark ? const Color(0xFFFFDD73) : const Color(0xFF6A4600),
    RiskColor.red => dark ? const Color(0xFFFFA6A3) : const Color(0xFF9B1C1C),
    RiskColor.gray => dark ? const Color(0xFFBAC6C4) : const Color(0xFF4B5654),
  };
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
  const RiskGrid({super.key, required this.risks, this.compact = false, this.history});

  final Risks risks;
  final bool compact;

  /// The last days of every risk by title; null: no bars.
  final Map<String, List<RiskColor>>? history;

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
              Expanded(child: RiskIndicator(title: items[i].$1, risk: items[i].$2, compact: compact, history: history?[items[i].$1])),
              const SizedBox(width: 8),
              Expanded(child: RiskIndicator(title: items[i + 1].$1, risk: items[i + 1].$2, compact: compact, history: history?[items[i + 1].$1])),
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
          // Two long labels ("Нет данных → Нет данных") do not fit a narrow
          // phone next to the title: the pair shrinks a little instead of overflowing.
          Flexible(
            flex: 2,
            child: FittedBox(
              fit: BoxFit.scaleDown,
              alignment: Alignment.centerRight,
              child: Row(mainAxisSize: MainAxisSize.min, children: [
                state(before),
                const Padding(padding: EdgeInsets.symmetric(horizontal: 6), child: Icon(Icons.arrow_forward, size: 16)),
                state(after),
              ]),
            ),
          ),
        ]),
      ),
    );
  }
}
