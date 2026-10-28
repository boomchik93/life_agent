import 'package:flutter/foundation.dart';

/// Whether this is the phone: Health Connect, the usage history of apps and
/// background work exist only there. The Mac shows the same screens without them.
bool get isPhone => defaultTargetPlatform == TargetPlatform.android;

/// Whether this is a computer: a keyboard, a pointer and a wide window.
bool get isDesktop => switch (defaultTargetPlatform) {
      TargetPlatform.macOS || TargetPlatform.windows || TargetPlatform.linux => true,
      _ => false,
    };
