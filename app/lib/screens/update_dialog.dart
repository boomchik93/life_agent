import 'package:flutter/material.dart';

import '../api.dart';
import '../update.dart';

/// Offers [release] and, on the user's yes, downloads and hands it to the
/// system installer. Android asks the user to confirm the install itself.
Future<void> showUpdateDialog(BuildContext context, AppRelease release, UpdateApi api, Installer installer) =>
    showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (_) => UpdateDialog(release: release, api: api, installer: installer),
    );

class UpdateDialog extends StatefulWidget {
  const UpdateDialog({super.key, required this.release, required this.api, required this.installer});

  final AppRelease release;
  final UpdateApi api;
  final Installer installer;

  @override
  State<UpdateDialog> createState() => _UpdateDialogState();
}

enum _Step { offer, permission, downloading, installing }

class _UpdateDialogState extends State<UpdateDialog> with WidgetsBindingObserver {
  _Step _step = _Step.offer;
  double? _progress;
  String? _error;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  /// Back from the system screen of "unknown sources": go on if allowed.
  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed && _step == _Step.permission) _start();
  }

  Future<void> _start() async {
    setState(() => _error = null);
    if (!await widget.installer.canInstall()) {
      if (mounted) setState(() => _step = _Step.permission);
      return;
    }
    if (!mounted) return;
    setState(() {
      _step = _Step.downloading;
      _progress = null;
    });
    try {
      final path = await widget.installer.apkPath();
      await widget.api.downloadRelease(widget.release, path, onProgress: (got, total) {
        if (mounted && total > 0) setState(() => _progress = got / total);
      });
      if (!mounted) return;
      setState(() => _step = _Step.installing);
      await widget.installer.install();
      // The system installer is on top now; this app is replaced once it's done.
      if (mounted) Navigator.of(context).pop();
    } on ApiException catch (e) {
      if (mounted) {
        setState(() {
          _step = _Step.offer;
          _error = e.message;
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          _step = _Step.offer;
          _error = 'Не удалось установить обновление. Попробуй ещё раз.';
        });
      }
    }
  }

  String get _size => '${(widget.release.size / (1024 * 1024)).toStringAsFixed(1)} МБ';

  @override
  Widget build(BuildContext context) {
    final busy = _step == _Step.downloading || _step == _Step.installing;
    return AlertDialog(
      title: const Text('Новая версия'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(switch (_step) {
            _Step.offer => 'На сервере версия ${widget.release.label}, $_size. Скачать и установить?',
            _Step.permission => 'Android спросит, можно ли этому приложению устанавливать приложения. '
                'Разреши один раз и вернись сюда.',
            _Step.downloading => 'Скачиваю ${widget.release.label}…',
            _Step.installing => 'Открываю установщик…',
          }),
          if (_step == _Step.downloading) ...[
            const SizedBox(height: 16),
            LinearProgressIndicator(value: _progress),
          ],
          if (_error != null) ...[
            const SizedBox(height: 12),
            Text(_error!, style: TextStyle(color: Theme.of(context).colorScheme.error)),
          ],
        ],
      ),
      actions: [
        TextButton(
          onPressed: busy ? null : () => Navigator.of(context).pop(),
          child: const Text('Позже'),
        ),
        if (_step == _Step.permission)
          FilledButton(
            onPressed: widget.installer.openInstallSettings,
            child: const Text('Открыть настройки'),
          )
        else
          FilledButton(
            onPressed: busy ? null : _start,
            child: Text(_error == null ? 'Обновить' : 'Ещё раз'),
          ),
      ],
    );
  }
}
