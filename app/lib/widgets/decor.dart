import 'package:flutter/material.dart';

import '../theme.dart';

/// The icon of the part of the day next to the greeting.
class GreetingBadge extends StatelessWidget {
  const GreetingBadge({super.key, required this.hour, this.size = 52});

  final int hour;
  final double size;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    final night = hour < 5 || hour >= 22;
    final icon = night
        ? Icons.nightlight_round
        : hour < 12
            ? Icons.wb_twilight
            : hour < 18
                ? Icons.wb_sunny_rounded
                : Icons.wb_twilight;
    return ExcludeSemantics(
      child: Container(
        width: size,
        height: size,
        decoration: BoxDecoration(
          shape: BoxShape.circle,
          gradient: LinearGradient(
            begin: Alignment.topLeft,
            end: Alignment.bottomRight,
            colors: night ? [pal.heroFrom, pal.heroTo] : [pal.accentContainer, pal.accent.withValues(alpha: 0.55)],
          ),
        ),
        child: Icon(icon, size: size * 0.5, color: night ? pal.onHero : pal.onAccentContainer),
      ),
    );
  }
}

/// A screen or a card with nothing to show yet: a large icon in a soft
/// circle, what is missing and, when there is one, the way to fill it.
class EmptyState extends StatelessWidget {
  const EmptyState({super.key, required this.icon, required this.title, this.text, this.action, this.compact = false});

  final IconData icon;
  final String title;
  final String? text;
  final Widget? action;

  /// Smaller, for a card rather than a whole screen.
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final circle = compact ? 64.0 : 96.0;
    return Padding(
      padding: EdgeInsets.all(compact ? 12 : 24),
      child: Column(mainAxisSize: MainAxisSize.min, children: [
        Container(
          width: circle,
          height: circle,
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            gradient: LinearGradient(begin: Alignment.topLeft, end: Alignment.bottomRight, colors: [pal.container, pal.surface2]),
          ),
          child: Icon(icon, size: circle * 0.44, color: pal.primary),
        ),
        SizedBox(height: compact ? 10 : 16),
        Text(title, textAlign: TextAlign.center, style: compact ? theme.textTheme.titleMedium : theme.textTheme.titleLarge),
        if (text != null) ...[
          const SizedBox(height: 4),
          Text(text!, textAlign: TextAlign.center, style: theme.textTheme.bodyMedium?.copyWith(color: pal.text2)),
        ],
        if (action != null) ...[SizedBox(height: compact ? 10 : 18), action!],
      ]),
    );
  }
}
