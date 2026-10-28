import 'package:flutter/material.dart';

import '../screen_time.dart';
import '../theme.dart';
import '../timetrack.dart';

/// "3 ч 10 мин", "45 мин", "2 ч".
String hm(int min) {
  final h = min ~/ 60, m = min % 60;
  if (h == 0) return '$m мин';
  return m == 0 ? '$h ч' : '$h ч $m мин';
}

/// "+1 ч 10 мин", "−20 мин", "0 мин".
String signed(int min) {
  if (min.abs() < 5) return '0 мин';
  return '${min > 0 ? '+' : '−'}${hm(min.abs())}';
}

Color timeColor(BuildContext context, String category) =>
    categoryColor(category, Theme.of(context).brightness);

class TimeCard extends StatelessWidget {
  const TimeCard({super.key, required this.children, this.color, this.borderColor, this.onTap, this.padding});

  final List<Widget> children;
  final Color? color, borderColor;
  final VoidCallback? onTap;
  final EdgeInsetsGeometry? padding;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    final inner = Padding(
      padding: padding ?? const EdgeInsets.all(16),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: children),
    );
    return Container(
      margin: const EdgeInsets.only(top: 12),
      decoration: BoxDecoration(
        color: color ?? pal.surface,
        borderRadius: BorderRadius.circular(24),
        border: Border.all(color: borderColor ?? pal.line),
      ),
      child: onTap == null
          ? inner
          : InkWell(borderRadius: BorderRadius.circular(24), onTap: onTap, child: inner),
    );
  }
}

/// A row of options that looks like the segmented control of the design.
class SegRow<T> extends StatelessWidget {
  const SegRow({super.key, required this.value, required this.options, required this.onChanged});

  final T value;
  final List<(T, String)> options;
  final ValueChanged<T> onChanged;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(4),
      decoration: BoxDecoration(color: pal.surface2, borderRadius: BorderRadius.circular(20)),
      child: Row(children: [
        for (final (v, label) in options)
          Expanded(
            child: Material(
              color: v == value ? pal.surface : Colors.transparent,
              borderRadius: BorderRadius.circular(16),
              elevation: v == value ? 1 : 0,
              child: InkWell(
                borderRadius: BorderRadius.circular(16),
                onTap: () => onChanged(v),
                child: Container(
                  constraints: const BoxConstraints(minHeight: 48),
                  alignment: Alignment.center,
                  padding: const EdgeInsets.symmetric(horizontal: 6),
                  child: Text(label,
                      textAlign: TextAlign.center,
                      style: theme.textTheme.titleSmall?.copyWith(color: v == value ? pal.text : pal.text2)),
                ),
              ),
            ),
          ),
      ]),
    );
  }
}

class CatChip extends StatelessWidget {
  const CatChip({super.key, required this.category, required this.selected, required this.onTap});

  final String category;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    final c = timeColor(context, category);
    return Semantics(
      selected: selected,
      button: true,
      child: Material(
        color: selected ? c.withValues(alpha: 0.18) : pal.surface,
        shape: StadiumBorder(side: BorderSide(color: selected ? c : pal.line, width: selected ? 2 : 1.5)),
        child: InkWell(
          customBorder: const StadiumBorder(),
          onTap: onTap,
          child: Container(
            constraints: const BoxConstraints(minHeight: 44),
            padding: const EdgeInsets.symmetric(horizontal: 14),
            child: Row(mainAxisSize: MainAxisSize.min, children: [
              Container(width: 10, height: 10, decoration: BoxDecoration(color: c, shape: BoxShape.circle)),
              const SizedBox(width: 8),
              Text(timeCategoryNames[category]!, style: Theme.of(context).textTheme.titleSmall),
            ]),
          ),
        ),
      ),
    );
  }
}

class AppBadge extends StatelessWidget {
  const AppBadge(this.pkg, {super.key, this.warn = false});

  final String pkg;
  final bool warn;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    final dark = Theme.of(context).brightness == Brightness.dark;
    return Container(
      width: 44,
      height: 44,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: warn ? (dark ? const Color(0xFF38300F) : const Color(0xFFFFF1CF)) : pal.container,
        borderRadius: BorderRadius.circular(14),
      ),
      child: warn
          ? Icon(Icons.warning_amber, size: 22, color: dark ? const Color(0xFFFFDD73) : const Color(0xFF6A4600))
          : Text(appLabel(pkg)[0], style: Theme.of(context).textTheme.titleMedium?.copyWith(color: pal.onContainer)),
    );
  }
}

/// A thin bar filled to [fraction] (0..1).
class FillBar extends StatelessWidget {
  const FillBar({super.key, required this.fraction, required this.color});

  final double fraction;
  final Color color;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    return ClipRRect(
      borderRadius: BorderRadius.circular(4),
      child: LinearProgressIndicator(
        value: fraction.clamp(0.0, 1.0),
        minHeight: 8,
        color: color,
        backgroundColor: pal.surface2,
      ),
    );
  }
}

class DeltaText extends StatelessWidget {
  const DeltaText(this.delta, {super.key});

  final int delta;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    return Row(mainAxisSize: MainAxisSize.min, children: [
      Icon(delta < -4 ? Icons.arrow_downward : Icons.arrow_upward, size: 16, color: pal.text2),
      Text(signed(delta),
          style: Theme.of(context).textTheme.bodySmall?.copyWith(color: pal.text2, fontWeight: FontWeight.w700)),
    ]);
  }
}

/// The shared look of a screen opened from the tab: a back arrow and a title.
class TimeScaffold extends StatelessWidget {
  const TimeScaffold({super.key, required this.title, required this.child, this.leading});

  final String title;
  final Widget child;
  final Widget? leading;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Row(children: [
          if (leading != null) ...[leading!, const SizedBox(width: 12)],
          Expanded(child: Text(title, overflow: TextOverflow.ellipsis, style: Theme.of(context).textTheme.titleLarge)),
        ]),
      ),
      body: child,
    );
  }
}

class NoAccessView extends StatelessWidget {
  const NoAccessView({super.key, required this.onOpen, required this.onRetry});

  final VoidCallback onOpen, onRetry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    Widget step(int n, String text) => Padding(
          padding: const EdgeInsets.symmetric(vertical: 8),
          child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Container(
              width: 32,
              height: 32,
              alignment: Alignment.center,
              decoration: BoxDecoration(color: pal.container, shape: BoxShape.circle),
              child: Text('$n', style: const TextStyle(fontFamily: fontNumbers, fontWeight: FontWeight.w600)),
            ),
            const SizedBox(width: 12),
            Expanded(child: Padding(padding: const EdgeInsets.only(top: 3), child: Text(text, style: theme.textTheme.bodyLarge))),
          ]),
        );
    return Column(children: [
      const SizedBox(height: 24),
      Container(
        width: 96,
        height: 96,
        decoration: BoxDecoration(color: pal.container, shape: BoxShape.circle),
        child: Icon(Icons.lock_outline, size: 48, color: pal.onContainer),
      ),
      const SizedBox(height: 16),
      Text('Нет доступа к статистике приложений', textAlign: TextAlign.center, style: theme.textTheme.headlineMedium),
      const SizedBox(height: 8),
      Text('Без неё я не вижу, в каких приложениях проходит время. Диаграмма по категориям при этом работает.',
          textAlign: TextAlign.center, style: theme.textTheme.bodyLarge?.copyWith(color: pal.text2)),
      TimeCard(children: [
        step(1, 'Открой настройки телефона и найди «Доступ к статистике использования».'),
        step(2, 'Выбери «Жизнь» в списке приложений.'),
        step(3, 'Включи переключатель и вернись сюда.'),
      ]),
      const SizedBox(height: 16),
      SizedBox(
        width: double.infinity,
        child: FilledButton.icon(onPressed: onOpen, icon: const Icon(Icons.settings_outlined), label: const Text('Открыть настройки телефона')),
      ),
      const SizedBox(height: 8),
      SizedBox(
        width: double.infinity,
        child: FilledButton.tonalIcon(
          onPressed: onRetry,
          icon: const Icon(Icons.refresh),
          label: const Text('Проверить ещё раз'),
          style: FilledButton.styleFrom(backgroundColor: pal.container, foregroundColor: pal.onContainer),
        ),
      ),
    ]);
  }
}

/// A grey placeholder while a screen loads.
class Skeleton extends StatelessWidget {
  const Skeleton({super.key, required this.height, this.width, this.radius = 16});

  final double height, radius;
  final double? width;

  @override
  Widget build(BuildContext context) => Container(
        height: height,
        width: width,
        decoration: BoxDecoration(color: Palette.of(context).surface2, borderRadius: BorderRadius.circular(radius)),
      );
}

class LoadingList extends StatelessWidget {
  const LoadingList({super.key, this.label = 'Собираю статистику приложений…'});

  final String label;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.only(top: 12),
        child: Column(children: [
          const Skeleton(height: 132, radius: 24),
          const SizedBox(height: 16),
          for (var i = 0; i < 4; i++)
            const Padding(
              padding: EdgeInsets.symmetric(vertical: 8),
              child: Row(children: [
                Skeleton(height: 44, width: 44, radius: 14),
                SizedBox(width: 12),
                Expanded(child: Skeleton(height: 18)),
              ]),
            ),
          const SizedBox(height: 8),
          Text(label, style: Theme.of(context).textTheme.bodySmall),
        ]),
      );
}
