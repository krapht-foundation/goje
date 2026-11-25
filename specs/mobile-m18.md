# Goje mobile wrappers, M18

`goje mobile` wraps a game's web build in Android and iOS projects that a developer builds and signs with Gradle and Xcode. [RFC 0019](../rfcs/0019-m18-live-matches-and-distribution.md) records why the wrappers use Capacitor rather than native Go hosts. The web build is the one that the [build specification](build-m15.md) defines, byte for byte.

## Command

```text
goje mobile -id APP_ID -build N [-o DIR] [-name NAME] [-version V] [-tags TAGS] [-icon PNG] [-platform android|ios|all] [PACKAGE]
```

| Flag | Default | Rule |
| --- | --- | --- |
| `-id` | None; it is required | The Android application ID and the iOS bundle identifier. It has two or more parts separated by periods. Each part is a letter followed by letters and digits, and no part is a Java keyword, because Capacitor uses the ID as the Java package of the Android activity. Hyphens and underscores are invalid because Android rejects the first and Apple the second. |
| `-build` | None; it is required | The native build number, from 1 to 2100000000, the largest Android version code that Google Play accepts. It becomes the Android `versionCode` and the iOS `CURRENT_PROJECT_VERSION`. Stores require it to grow with each upload, so Goje never derives it. |
| `-version` | `0.0.0` | The rule of `goje build`. It becomes the Android `versionName` in full and the iOS `MARKETING_VERSION` without its pre-release suffix. |
| `-platform` | `all` | `android`, `ios`, or `all`. |
| `-o`, `-name`, `-tags`, `-icon` | As for `goje build` | The rules of `goje build`. The icon is the 512 × 512 PNG that the web build uses. |

Argument errors exit with status 2 and print the usage; other errors exit with status 1. The command needs `node` and `npm` on `PATH`, and Capacitor 8 needs Node 22 or later. It does not need Gradle, a JDK, or Xcode; those build the projects afterward.

## Output

`goje mobile` first checks every existing native project that `-platform` selects. If the project's application ID differs from `-id`, it is an error that names both IDs, and no file changes. A changed ID needs a new project, because the ID is also the Android package directory and is part of signing.

It then runs `goje build -target web` into `DIR/web/` and writes `DIR/mobile/`:

| Path | Owner | Content |
| --- | --- | --- |
| `package.json`, `package-lock.json` | Goje, replaced on every run | Exactly `@capacitor/cli`, `@capacitor/core`, `@capacitor/android`, and `@capacitor/ios` at version 8.6.2, with the lock file's integrity digests for every package. They are embedded in `goje`. |
| `capacitor.config.json` | Goje, replaced on every run | `appId` set to the ID, `appName` set to the name, and `webDir` `../web`. It has no `server` entry, so the app loads only its packaged files, and no plugin settings. |
| `node_modules/` | npm | Installed by `npm ci --ignore-scripts`, which fails if the lock file and the registry disagree. |
| `android/`, `ios/` | The developer, except the fields below | Created by `cap add` when missing. iOS uses Swift Package Manager, not CocoaPods. Every run then runs `cap sync` for each platform, which copies `DIR/web/` into the project with two empty Cordova shim files, `cordova.js` and `cordova_plugins.js`, and regenerates Capacitor's own plugin lists. |

Because Goje replaces `package.json` on every run, a plugin added there is removed again, and `cap sync` drops it from the native projects. The template's Android permissions stay: `INTERNET`, which a networked game such as Outpost needs, and the receiver permission that AndroidX declares. Goje adds none.

Goje owns these fields in the native projects and sets them on every run:

| Platform | Field | Value |
| --- | --- | --- |
| Android | `versionCode` and `versionName` in `app/build.gradle` | `-build` and `-version`. The file must have exactly one line of each, or the run is an error. |
| Android | Launcher icons | `ic_launcher.png`, `ic_launcher_round.png`, and `ic_launcher_foreground.png` in `res/mipmap-xxxhdpi/` are the icon, and the same files in the other `mipmap-*dpi` directories are removed, so the launcher scales the single copy. `res/mipmap-anydpi-v26/ic_launcher.xml` and `ic_launcher_round.xml` are adaptive icons whose foreground is the icon inset by 16.667% on each side. That draws the whole icon in the 72 dp visible area of the 108 dp layer, so a launcher's mask stays outside the maskable safe circle. |
| iOS | `MARKETING_VERSION` and `CURRENT_PROJECT_VERSION` in `App/App.xcodeproj/project.pbxproj` | The numeric core of `-version` and `-build`, on every line that sets them. The project must set both. |
| iOS | `App/App/Assets.xcassets/AppIcon.appiconset/AppIcon-512@2x.png` | The icon at 1024 × 1024, with each pixel doubled so pixel art stays sharp, drawn over `#141c29`, and encoded without an alpha channel, because App Store Connect rejects one. |

Every other file in `android/` and `ios/` keeps the developer's changes, including the app's display name, which `cap add` sets from `-name` only when it creates the project. Capacitor's default splash images also stay until the developer replaces them.

The web page does not register its service worker when `window.Capacitor` exists. The app already contains every file, and a worker would keep serving the previous game after an app update. Browsers still install the offline web app as before.

## Verification

`scripts/mobile-android.sh` and `scripts/mobile-ios.sh` run the gates for Beacon, and the [mobile workflow](../.github/workflows/mobile.yml) runs them on hosted runners. Both scripts generate the project, append a marker comment to a native source file, run `goje mobile` again with a new version and build number, and require the marker, the new version fields, and a refusal of a different application ID.

The build and simulator scripts use fresh directories under `.build/`. Move an earlier run or set `MOBILE_OUT` to a new directory; the scripts refuse to overwrite an existing one.

`scripts/mobile-android.sh build` needs JDK 21 and `ANDROID_HOME`. It creates a disposable RSA upload key for the run, builds a release AAB with Gradle's injected signing properties, and requires `jarsigner -verify -strict` to pass and the AAB's certificate to be the disposable key's. It validates the AAB with pinned bundletool 1.18.3, derives a universal APK signed with the same key, and requires `apksigner` to report that certificate. The APK's `assets/public/` must equal `DIR/web/` apart from the two empty shim files, its plugin list must be empty, its Capacitor configuration must have no server, and its permissions must be the two template permissions. `scripts/mobile-android.sh emulator` turns off Wi-Fi and mobile data and enables airplane mode on the running emulator, installs that APK, launches it, and plays the challenge.

`scripts/mobile-ios.sh simulator` needs Xcode with an iOS simulator runtime. It builds the Release configuration for the simulator without signing and checks the bundle ID, both version fields, `public/` against `DIR/web/` apart from the shim files, and a Capacitor configuration without a server or plugins. It creates a simulator of the newest iPhone type on the newest iOS runtime, installs and launches the app, and plays the challenge with pinned AXe 2.2.0, whose taps are simulator touches. A hosted Mac cannot turn off the simulator's network without the runner's, so this gate shows that the app contains every file it loads, and an offline run needs a Mac whose network the maintainer turns off.

`scripts/smoke-mobile.mjs` plays [Beacon's challenge](../examples/beacon) from screenshots, without reading the page. It finds the three unlit beacons, each one 12 × 12 logical pixel block of `(91,141,209)`, and derives the logical-to-screen mapping from their centers. It requires the ready state and waits for each beacon to light before tapping the next, so multiple taps cannot land in one simulation tick. It then requires the gold win banner and three lit beacons, taps the board, and requires the ready state again.

`scripts/mobile-ios.sh archive` proves distribution signing only with credentials that the caller supplies: `IOS_APP_ID`, `APPLE_TEAM_ID`, `IOS_CERTIFICATE_P12`, `IOS_CERTIFICATE_PASSWORD`, and `IOS_PROFILE`. It imports the certificate into a temporary keychain, installs the App Store profile for the run, archives with manual signing, and exports with `destination` `export`, so nothing is uploaded. It verifies the IPA with `codesign --verify --deep --strict`, requires the `Apple Distribution` authority, the team ID, the profile's UUID in `embedded.mobileprovision`, and the bundle ID, and removes the keychain and any profile it installed. In the workflow, only a manual run with `ios-archive` selected runs it, in the protected `mobile-release` environment, whose variables `IOS_APP_ID` and `APPLE_TEAM_ID` and secrets `IOS_CERTIFICATE_P12_BASE64`, `IOS_CERTIFICATE_PASSWORD`, and `IOS_PROFILE_BASE64` the maintainer provides. An unsigned or simulator build says nothing about distribution signing or store acceptance.

## Limits

- Wrappers come only from Capacitor 8.6.2. Changing the version means changing the embedded lock file and rerunning every gate.
- Plugins, a remote server URL, live reload, and native Go hosts are not supported.
- Goje does not upload to a store, manage store listings, or claim that a store accepted a build.
- Splash screens and localized display names are the developer's to change in the native projects.
