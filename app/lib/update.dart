import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/services.dart';

/// The APK the server hands out (`GET /v1/app/latest`), put there by
/// `deploy.sh app`. The phone installs it over itself when [versionCode] is
/// newer than its own; the same signing key makes that possible.
class AppRelease {
  const AppRelease({required this.versionCode, required this.versionName, required this.sha256, required this.size});

  final int versionCode;
  final String versionName;

  /// Hex of the APK's SHA-256: a broken download is never installed.
  final String sha256;

  /// Bytes.
  final int size;

  factory AppRelease.fromJson(Map<String, dynamic> j) => AppRelease(
        versionCode: (j['version_code'] as num).toInt(),
        versionName: j['version_name'] as String? ?? '',
        sha256: j['sha256'] as String,
        size: (j['size'] as num).toInt(),
      );

  /// "1.0.0 (5)".
  String get label => versionName.isEmpty ? '$versionCode' : '$versionName ($versionCode)';
}

/// The server side of updates; [HttpLifeApi] has it.
abstract interface class UpdateApi {
  /// The release on the server; null when there is none.
  Future<AppRelease?> latestRelease();

  /// Downloads [release] into [path] and checks its SHA-256. A file that does
  /// not match is deleted and an [ApiException] is thrown.
  Future<void> downloadRelease(AppRelease release, String path, {void Function(int got, int total)? onProgress});
}

/// The phone side: what is installed and the system installer.
abstract interface class Installer {
  Future<int> installedVersion();

  /// Where the downloaded APK goes.
  Future<String> apkPath();

  /// Whether the user allowed this app to install apps ("unknown sources").
  Future<bool> canInstall();

  /// Opens the system screen where the user allows it, once.
  Future<void> openInstallSettings();

  /// Hands the APK at [apkPath] to the system installer, which asks the user.
  Future<void> install();
}

/// [Installer] over the channel of MainActivity.kt.
class ChannelInstaller implements Installer {
  const ChannelInstaller();

  static const _channel = MethodChannel('dev.boomchick.life_agent/update');

  @override
  Future<int> installedVersion() async => (await _channel.invokeMethod<num>('installedVersion'))?.toInt() ?? 0;

  @override
  Future<String> apkPath() async => (await _channel.invokeMethod<String>('apkPath'))!;

  @override
  Future<bool> canInstall() async => await _channel.invokeMethod<bool>('canInstall') ?? false;

  @override
  Future<void> openInstallSettings() => _channel.invokeMethod<void>('openInstallSettings');

  @override
  Future<void> install() => _channel.invokeMethod<void>('install');
}

/// The release on the server when it is newer than the installed app, else
/// null. A left-over APK of an installed update is removed on the way.
Future<AppRelease?> newerRelease(UpdateApi api, Installer installer) async {
  final release = await api.latestRelease();
  final installed = await installer.installedVersion();
  if (release != null && release.versionCode > installed) return release;
  try {
    final old = File(await installer.apkPath());
    if (await old.exists()) await old.delete();
  } catch (_) {
    // Only tidying: the cache is the system's to clear anyway.
  }
  return null;
}

/// Hex SHA-256 of a byte stream, computed while it is written elsewhere.
class Sha256Sink implements Sink<List<int>> {
  Sha256Sink() {
    _input = sha256.startChunkedConversion(_out);
  }

  final _out = _DigestSink();
  late final ByteConversionSink _input;

  @override
  void add(List<int> data) => _input.add(data);

  @override
  void close() => _input.close();

  /// After [close].
  String get hex => _out.value.toString();
}

class _DigestSink implements Sink<Digest> {
  late Digest value;

  @override
  void add(Digest data) => value = data;

  @override
  void close() {}
}
