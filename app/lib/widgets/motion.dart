import 'package:flutter/material.dart';

/// How long a one-off animation runs; zero when the system asks for less motion.
Duration motion(BuildContext context, int ms) =>
    MediaQuery.maybeDisableAnimationsOf(context) == true ? Duration.zero : Duration(milliseconds: ms);

/// Fades [child] in and lifts it a little, once, when it first appears.
/// [index] staggers the cards of one screen: the later, the later it comes.
class Appear extends StatelessWidget {
  const Appear({super.key, required this.child, this.index = 0});

  final Widget child;
  final int index;

  static const _step = 70, _run = 380;

  @override
  Widget build(BuildContext context) {
    final total = _run + index * _step;
    return TweenAnimationBuilder<double>(
      tween: Tween(begin: 0, end: 1),
      duration: motion(context, total),
      curve: Interval(index * _step / total, 1, curve: Curves.easeOutCubic),
      child: child,
      // The semantics stay while the card is still transparent.
      builder: (context, v, child) => Opacity(
        opacity: v,
        alwaysIncludeSemantics: true,
        child: Transform.translate(offset: Offset(0, 14 * (1 - v)), child: child),
      ),
    );
  }
}

/// A value that grows from zero to [value] once and then follows its changes:
/// the fill of a bar, the height of a column.
class Grow extends StatelessWidget {
  const Grow({super.key, required this.value, required this.builder, this.ms = 650});

  final double value;
  final int ms;
  final Widget Function(BuildContext context, double value) builder;

  @override
  Widget build(BuildContext context) => TweenAnimationBuilder<double>(
        tween: Tween(begin: 0, end: value),
        duration: motion(context, ms),
        curve: Curves.easeOutCubic,
        builder: (context, v, _) => builder(context, v),
      );
}
