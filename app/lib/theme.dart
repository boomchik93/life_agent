import 'package:flutter/material.dart';

/// Colours and shapes of the "Жизнь" design (docs: zhizn-design README).
class Palette {
  const Palette({
    required this.bg,
    required this.surface,
    required this.surface2,
    required this.line,
    required this.text,
    required this.text2,
    required this.text3,
    required this.primary,
    required this.onPrimary,
    required this.container,
    required this.onContainer,
    required this.brightness,
  });

  final Color bg, surface, surface2, line, text, text2, text3;
  final Color primary, onPrimary, container, onContainer;
  final Brightness brightness;

  static const light = Palette(
    bg: Color(0xFFF3F6F5),
    surface: Color(0xFFFFFFFF),
    surface2: Color(0xFFE8EFED),
    line: Color(0xFFD9E2DF),
    text: Color(0xFF12201F),
    text2: Color(0xFF3F5251),
    text3: Color(0xFF5A6C6A),
    primary: Color(0xFF0B7A83),
    onPrimary: Color(0xFFFFFFFF),
    container: Color(0xFFD5EDEF),
    onContainer: Color(0xFF06464C),
    brightness: Brightness.light,
  );

  static const dark = Palette(
    bg: Color(0xFF0B1415),
    surface: Color(0xFF122022),
    surface2: Color(0xFF1B2C2F),
    line: Color(0xFF26393C),
    text: Color(0xFFE9F2F1),
    text2: Color(0xFFBBCBC9),
    text3: Color(0xFF93A9A7),
    primary: Color(0xFF5CCFD8),
    onPrimary: Color(0xFF04252A),
    container: Color(0xFF14393E),
    onContainer: Color(0xFFBFF0F4),
    brightness: Brightness.dark,
  );

  static Palette of(BuildContext context) =>
      Theme.of(context).brightness == Brightness.dark ? dark : light;
}

const fontText = 'Onest';
const fontNumbers = 'Unbounded';

ThemeData buildTheme(Palette p) {
  final dark = p.brightness == Brightness.dark;
  final scheme = ColorScheme(
    brightness: p.brightness,
    primary: p.primary,
    onPrimary: p.onPrimary,
    primaryContainer: p.container,
    onPrimaryContainer: p.onContainer,
    secondary: p.primary,
    onSecondary: p.onPrimary,
    secondaryContainer: p.container,
    onSecondaryContainer: p.onContainer,
    tertiary: p.primary,
    onTertiary: p.onPrimary,
    tertiaryContainer: p.container,
    onTertiaryContainer: p.onContainer,
    error: dark ? const Color(0xFFEF5350) : const Color(0xFFC62828),
    onError: dark ? const Color(0xFF3B0808) : Colors.white,
    errorContainer: dark ? const Color(0xFF3A1A1B) : const Color(0xFFFBE3E2),
    onErrorContainer: dark ? const Color(0xFFFFA6A3) : const Color(0xFF9B1C1C),
    surface: p.bg,
    onSurface: p.text,
    onSurfaceVariant: p.text2,
    surfaceContainerLowest: p.surface,
    surfaceContainerLow: p.surface,
    surfaceContainer: p.surface,
    surfaceContainerHigh: p.surface2,
    surfaceContainerHighest: p.surface2,
    outline: p.text3,
    outlineVariant: p.line,
    shadow: Colors.black,
    scrim: dark ? const Color(0x99000000) : const Color(0x730A1414),
  );

  TextStyle t(double size, FontWeight w, [Color? color, double h = 1.4]) =>
      TextStyle(
        fontFamily: fontText,
        fontSize: size,
        fontWeight: w,
        height: h,
        color: color ?? p.text,
      );

  final textTheme = TextTheme(
    displaySmall: t(36, FontWeight.w800, null, 1.15),
    headlineLarge: t(28, FontWeight.w800, null, 1.2),
    headlineMedium: t(24, FontWeight.w700, null, 1.25),
    headlineSmall: t(24, FontWeight.w700, null, 1.25),
    titleLarge: t(20, FontWeight.w700, null, 1.3),
    titleMedium: t(17, FontWeight.w700, null, 1.3),
    titleSmall: t(16, FontWeight.w700),
    bodyLarge: t(17, FontWeight.w500, null, 1.45),
    bodyMedium: t(16, FontWeight.w500, null, 1.45),
    bodySmall: t(14, FontWeight.w500, p.text3),
    labelLarge: t(17, FontWeight.w700, null, 1.2),
    labelMedium: t(14, FontWeight.w600),
    labelSmall: t(14, FontWeight.w600, p.text3),
  );

  const stadium = StadiumBorder();
  OutlineInputBorder field(Color c, [double w = 0]) => OutlineInputBorder(
    borderRadius: BorderRadius.circular(20),
    borderSide: w == 0 ? BorderSide.none : BorderSide(color: c, width: w),
  );

  return ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    fontFamily: fontText,
    textTheme: textTheme,
    scaffoldBackgroundColor: p.bg,
    canvasColor: p.bg,
    dividerColor: p.line,
    dividerTheme: DividerThemeData(color: p.line, space: 1, thickness: 1),
    splashFactory: InkRipple.splashFactory,
    appBarTheme: AppBarTheme(
      backgroundColor: p.bg,
      foregroundColor: p.text,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      scrolledUnderElevation: 0,
      centerTitle: false,
      titleTextStyle: t(24, FontWeight.w800, null, 1.2),
      iconTheme: IconThemeData(color: p.text, size: 24),
      actionsIconTheme: IconThemeData(color: p.text, size: 24),
    ),
    cardTheme: CardThemeData(
      color: p.surface,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(20),
        side: BorderSide(color: p.line),
      ),
    ),
    listTileTheme: ListTileThemeData(
      minVerticalPadding: 10,
      titleTextStyle: t(17, FontWeight.w700),
      subtitleTextStyle: t(15, FontWeight.w500, p.text2),
      iconColor: p.text2,
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        minimumSize: const Size(64, 56),
        padding: const EdgeInsets.symmetric(horizontal: 16),
        shape: stadium,
        textStyle: t(17, FontWeight.w700, null, 1.2),
        backgroundColor: p.primary,
        foregroundColor: p.onPrimary,
      ),
    ),
    elevatedButtonTheme: ElevatedButtonThemeData(
      style: ElevatedButton.styleFrom(
        minimumSize: const Size(64, 56),
        shape: stadium,
        textStyle: t(17, FontWeight.w700, null, 1.2),
        backgroundColor: p.container,
        foregroundColor: p.onContainer,
        elevation: 0,
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        minimumSize: const Size(64, 56),
        padding: const EdgeInsets.symmetric(horizontal: 16),
        shape: stadium,
        side: BorderSide(color: p.line, width: 1.5),
        textStyle: t(17, FontWeight.w700, null, 1.2),
        foregroundColor: p.primary,
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        minimumSize: const Size(48, 48),
        shape: stadium,
        textStyle: t(17, FontWeight.w700, null, 1.2),
        foregroundColor: p.primary,
      ),
    ),
    iconButtonTheme: IconButtonThemeData(
      style: IconButton.styleFrom(
        minimumSize: const Size(48, 48),
        foregroundColor: p.text,
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: p.surface2,
      contentPadding: const EdgeInsets.symmetric(horizontal: 18, vertical: 16),
      hintStyle: t(17, FontWeight.w500, p.text3),
      labelStyle: t(16, FontWeight.w600, p.text2),
      helperStyle: t(14, FontWeight.w500, p.text3),
      border: field(p.line),
      enabledBorder: field(p.line),
      focusedBorder: field(p.primary, 2),
      errorBorder: field(scheme.error, 2),
      focusedErrorBorder: field(scheme.error, 2),
    ),
    navigationBarTheme: NavigationBarThemeData(
      height: 84,
      backgroundColor: p.surface,
      surfaceTintColor: Colors.transparent,
      indicatorColor: p.container,
      indicatorShape: const StadiumBorder(),
      iconTheme: WidgetStateProperty.resolveWith(
        (s) => IconThemeData(
          size: 28,
          color: s.contains(WidgetState.selected) ? p.onContainer : p.text2,
        ),
      ),
      labelTextStyle: WidgetStateProperty.resolveWith(
        (s) => t(
          14,
          FontWeight.w600,
          s.contains(WidgetState.selected) ? p.text : p.text2,
        ),
      ),
    ),
    chipTheme: ChipThemeData(
      backgroundColor: p.surface,
      selectedColor: p.container,
      side: BorderSide(color: p.line),
      shape: const StadiumBorder(),
      labelStyle: t(16, FontWeight.w600),
      secondaryLabelStyle: t(16, FontWeight.w600, p.onContainer),
      checkmarkColor: p.onContainer,
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 8),
    ),
    switchTheme: SwitchThemeData(
      thumbColor: WidgetStateProperty.resolveWith(
        (s) => s.contains(WidgetState.selected) ? p.onPrimary : p.text3,
      ),
      trackColor: WidgetStateProperty.resolveWith(
        (s) => s.contains(WidgetState.selected) ? p.primary : p.surface2,
      ),
      trackOutlineColor: WidgetStateProperty.resolveWith(
        (s) => s.contains(WidgetState.selected) ? Colors.transparent : p.text3,
      ),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: p.surface,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(28)),
      titleTextStyle: t(24, FontWeight.w700, null, 1.25),
      contentTextStyle: t(16, FontWeight.w500, p.text2, 1.45),
    ),
    bottomSheetTheme: BottomSheetThemeData(
      backgroundColor: p.surface,
      surfaceTintColor: Colors.transparent,
      showDragHandle: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(28)),
      ),
    ),
    snackBarTheme: SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      backgroundColor: dark ? p.surface2 : const Color(0xFF12201F),
      contentTextStyle: t(16, FontWeight.w600, dark ? p.text : Colors.white),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
    ),
    progressIndicatorTheme: ProgressIndicatorThemeData(
      color: p.primary,
      linearTrackColor: p.surface2,
      circularTrackColor: p.surface2,
    ),
    segmentedButtonTheme: SegmentedButtonThemeData(
      style: SegmentedButton.styleFrom(
        side: BorderSide(color: p.line),
        selectedBackgroundColor: p.container,
        selectedForegroundColor: p.onContainer,
        textStyle: t(16, FontWeight.w600),
      ),
    ),
  );
}

/// Category colour of an event (design: bar next to the event).
Color categoryColor(String category, Brightness b) {
  final dark = b == Brightness.dark;
  return switch (category) {
    'study' => dark ? const Color(0xFF9AA5FF) : const Color(0xFF3F51D8),
    'work' => dark ? const Color(0xFFF0A56A) : const Color(0xFFA5601F),
    'hobby' => dark ? const Color(0xFFFF8FBD) : const Color(0xFFC23A74),
    'rest' => dark ? const Color(0xFF9CD09F) : const Color(0xFF4C8A50),
    'chores' => dark ? const Color(0xFFF2C14E) : const Color(0xFF9A7200),
    'career' => dark ? const Color(0xFFBFA2FF) : const Color(0xFF7B4FD0),
    'sport' => dark ? const Color(0xFF5CC8EE) : const Color(0xFF0B7FA8),
    _ => dark ? const Color(0xFFA8B0BD) : const Color(0xFF667085),
  };
}
