import 'package:flutter/material.dart';

import '../api.dart';

/// A readable error with a way out: retry, and settings when the token is wrong.
class ErrorView extends StatelessWidget {
  const ErrorView({super.key, required this.error, required this.onRetry, this.onSettings});

  final Object error;
  final VoidCallback onRetry;
  final VoidCallback? onSettings;

  @override
  Widget build(BuildContext context) {
    final api = error is ApiException ? error as ApiException : null;
    final message = api?.message ?? 'Что-то пошло не так. Попробуй ещё раз.';
    final icon = switch (api?.problem) {
      ApiProblem.network => Icons.wifi_off,
      ApiProblem.unauthorized => Icons.key_off,
      _ => Icons.cloud_off,
    };
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              width: 96,
              height: 96,
              decoration: BoxDecoration(color: Theme.of(context).colorScheme.errorContainer, shape: BoxShape.circle),
              child: Icon(icon, size: 42, color: Theme.of(context).colorScheme.onErrorContainer),
            ),
            const SizedBox(height: 16),
            Text(message, textAlign: TextAlign.center, style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 16),
            FilledButton(onPressed: onRetry, child: const Text('Повторить')),
            if (onSettings != null &&
                (api?.problem == ApiProblem.unauthorized || api?.problem == ApiProblem.network))
              TextButton(onPressed: onSettings, child: const Text('Настройки')),
          ],
        ),
      ),
    );
  }
}
