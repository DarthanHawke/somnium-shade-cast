# Somnium Shade Cast Client

[Русский](README.md) · **English**

<p align="center">
  <img src="../assets/somnium-shade-cast-logo.png" alt="Somnium Shade Cast" width="600">
</p>

A Flutter demo client for Windows and Linux. It covers the workflow from uploading audio files to streaming music from the Somnium Shade Cast server. The app interface is in Russian.

## Demo features

- **Uploads.** Multiple audio files or an entire folder, including subfolders, can be queued for upload. Browsing and playback remain available during uploads and server processing.
- **Library.** Tracks, albums, and artists can be browsed, searched, and removed from a user's collection. The client displays cover art extracted by the server from audio files.
- **Playback.** The player supports seeking, manual and automatic track changes, and shuffle. An album played in shuffle mode loops.

## Running the demo

The client needs a running server. The default address is `http://127.0.0.1:8080`. In `dev` mode, a test user can be selected or created. In `prod` mode, the client prompts for the administrator credentials. [Server setup](../server/README.en.md)

In current builds, `mpv_audio_kit` downloads the playback engine at build time and bundles it with the app. Windows still requires the Microsoft Visual C++ Redistributable. [Microsoft download page](https://learn.microsoft.com/en-us/cpp/windows/latest-supported-vc-redist)

Running the prebuilt client on Ubuntu 24.04, including under WSLg, requires the system graphics and audio libraries.

```sh
sudo apt update
sudo apt install libgtk-3-0t64 libasound2t64 libpulse0 libegl1 libgles2 libgl1-mesa-dri
```

Prebuilt clients will be available on GitHub Releases. After extracting the full archive, run `somnium_shade_cast.exe` on Windows or `./somnium_shade_cast` from the extracted directory on Linux. [Client releases](https://github.com/DarthanHawke/somnium-shade-cast/releases)

## Building from source

The client uses Flutter 3.50, which includes Dart 3.14. Commands run from `client/` on the target OS. If the required files are missing from the cache, `flutter pub get` downloads Dart packages from pub.dev, and `mpv_audio_kit` downloads the prebuilt libmpv library for the target OS from GitHub Releases during `flutter build`. Both downloads are automatic.

- **Windows.** Terminal builds use MSVC, CMake, and the Windows SDK from the **Desktop development with C++** workload. This workload is available through Visual Studio or the standalone Visual Studio Build Tools. Windows Developer Mode enables plugin symlinks. [Flutter setup](https://docs.flutter.dev/platform-integration/windows/setup) · [Windows Developer Mode](https://learn.microsoft.com/en-us/windows/advanced-settings/developer-mode)
- **Linux.** Clang, CMake, Ninja, `pkg-config`, GTK 3 headers and C++ standard library headers. Audio uses ALSA, with PipeWire and PulseAudio support. [Flutter setup](https://docs.flutter.dev/platform-integration/linux/setup) · [mpv_audio_kit requirements](https://pub.dev/packages/mpv_audio_kit/versions/0.7.2)

```sh
flutter pub get --enforce-lockfile
```

The build command depends on the target platform.

| Platform | Command | Output relative to `client/` |
| --- | --- | --- |
| Windows x64 | `flutter build windows --release` | `build/windows/x64/runner/Release/` |
| Linux x64 | `flutter build linux --release` | `build/linux/x64/release/bundle/` |

For development, the app can be started with `flutter run -d windows` or `flutter run -d linux`. GitHub Actions checks both platforms in separate jobs. [Build workflow](../.github/workflows/client-desktop.yml)

## Third-party components

The `mpv_audio_kit` code is BSD-3-Clause. The bundled libmpv engine is licensed under LGPL 3.0 or later. Library notices are accessible through the About dialog. Binary distribution requires retaining license texts, access to the corresponding source, and the ability to replace the engine. Version 0.7.2 of `mpv_audio_kit` uses libmpv release `r26`. [Distribution terms](https://github.com/ales-drnz/mpv_audio_kit/blob/main/LICENSES.md) · [Engine release and `licenses.zip`](https://github.com/ales-drnz/mpv_audio_kit/releases/tag/libmpv-r26)
