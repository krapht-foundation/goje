# Goje brand assets

Use the [Goje logo](goje.svg) when the mascot and wordmark appear together. Launcher icons and favicons use the right-glancing tomato on its own. The SVG lockup is the source for all icon artwork.

| Platform | Assets and usage |
| --- | --- |
| Android | Merge [`android/res/`](android/res/) into the app's resources and set `android:icon="@mipmap/goje_launcher"`. The set includes legacy density PNGs, an adaptive icon for Android 8+, and a monochrome layer for themed icons on Android 13+. Upload the [512 × 512 PNG](android/play-store-icon.png) separately to Google Play. |
| iOS | Copy [`AppIcon.appiconset`](ios/Assets.xcassets/AppIcon.appiconset/) into the app's asset catalog and select `AppIcon` as the target's app icon in Xcode. The 1024 × 1024 PNG is opaque; iOS applies the corner mask. |
| Web | Serve [`favicon.svg`](web/favicon.svg), [`favicon.ico`](web/favicon.ico), and [`apple-touch-icon.png`](web/apple-touch-icon.png) from the site root. The ICO contains 16, 32, and 48 px images, also available as individual PNGs under [`web/`](web/). |

```html
<link rel="icon" href="/favicon.ico" sizes="16x16 32x32 48x48">
<link rel="icon" href="/favicon.svg" type="image/svg+xml" sizes="any">
<link rel="apple-touch-icon" href="/apple-touch-icon.png" sizes="180x180">
```

Run from the repository root to regenerate the platform assets after editing the source SVG:

```sh
make setup-web
PLAYWRIGHT_BROWSERS_PATH="$PWD/.build/playwright" node scripts/generate-brand-icons.mjs
```

The icon assets do not add Android or iOS runtime targets to Goje.

## GitHub social preview

Upload [`social-preview.png`](social-preview.png) under **Settings → General → Social preview**. The image is 1280 × 640 px, with the logo and text inside the template's safe area. To update it, edit [`social-preview.html`](social-preview.html) and capture it in a 1280 × 640 browser viewport at 1× scale.
