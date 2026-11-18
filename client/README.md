# Somnium Shade Cast Client

**Русский** · [English](README.en.md)

<p align="center">
  <img src="../assets/somnium-shade-cast-logo.png" alt="Somnium Shade Cast" width="600">
</p>

Демо-клиент на Flutter для Windows и Linux. Через него можно проверить, как сервер Somnium Shade Cast принимает аудиофайлы, добавляет их в библиотеку и передаёт музыку для потокового воспроизведения.

## Что уже работает

- **Загрузка.** Можно выбрать несколько аудиофайлов или целую папку с подпапками. Пока файлы отправляются и обрабатываются, можно пользоваться библиотекой и слушать музыку.
- **Библиотека.** Треки, альбомы и исполнители доступны для просмотра, поиска и удаления из коллекции пользователя. Клиент показывает обложки, которые сервер извлёк из аудиофайлов.
- **Воспроизведение.** Плеер поддерживает перемотку, ручной и автоматический переход между треками и случайный порядок. Если включить случайный порядок для альбома, он играет по кругу.

## Запуск демо

Клиенту нужен запущенный сервер. Адрес по умолчанию — `http://127.0.0.1:8080`. В режиме `dev` можно выбрать или создать тестового пользователя. В режиме `prod` клиент запрашивает учётные данные администратора. [Запуск сервера](../server/README.md)

В текущих сборках `mpv_audio_kit` загружает аудиодвижок во время сборки и включает его в приложение. Для Windows сохраняется требование Microsoft Visual C++ Redistributable. [Загрузка с сайта Microsoft](https://learn.microsoft.com/en-us/cpp/windows/latest-supported-vc-redist)

Для запуска готовой сборки на Ubuntu 24.04, в том числе в WSLg, требуются системные библиотеки графики и звука.

```sh
sudo apt update
sudo apt install libgtk-3-0t64 libasound2t64 libpulse0 libegl1 libgles2 libgl1-mesa-dri
```

Готовые сборки будут доступны на GitHub Releases. После полной распаковки архива на Windows запускается `somnium_shade_cast.exe`, на Linux — `./somnium_shade_cast` из распакованного каталога. [Релизы клиента](https://github.com/DarthanHawke/somnium-shade-cast/releases)

## Сборка из исходников

Для сборки используется Flutter 3.50, в который входит Dart 3.14. Команды выполняются из `client/` на целевой ОС. Если нужных файлов ещё нет в кэше, `flutter pub get` скачивает Dart-пакеты с pub.dev, а во время `flutter build` пакет `mpv_audio_kit` скачивает готовую библиотеку libmpv для целевой ОС из GitHub Releases. Обе загрузки выполняются автоматически.

- **Windows.** Сборка из терминала использует MSVC, CMake и Windows SDK из набора **Desktop development with C++**. Этот набор можно установить через Visual Studio или отдельно через Visual Studio Build Tools. Для символических ссылок плагинов используется режим разработчика Windows. [Настройка Flutter](https://docs.flutter.dev/platform-integration/windows/setup) · [Режим разработчика Windows](https://learn.microsoft.com/en-us/windows/advanced-settings/developer-mode)
- **Linux.** Clang, CMake, Ninja, `pkg-config`, заголовки GTK 3 и стандартной библиотеки C++. Для аудио используется ALSA. Движок также поддерживает PipeWire и PulseAudio. [Настройка Flutter](https://docs.flutter.dev/platform-integration/linux/setup) · [Требования mpv_audio_kit](https://pub.dev/packages/mpv_audio_kit/versions/0.7.2)

```sh
flutter pub get --enforce-lockfile
```

Затем выполняется команда для нужной платформы.

| Платформа | Команда | Результат относительно `client/` |
| --- | --- | --- |
| Windows x64 | `flutter build windows --release` | `build/windows/x64/runner/Release/` |
| Linux x64 | `flutter build linux --release` | `build/linux/x64/release/bundle/` |

Для режима разработки используется `flutter run -d windows` или `flutter run -d linux`. Сборки обеих платформ проверяются отдельными заданиями в GitHub Actions. [Workflow сборки](../.github/workflows/client-desktop.yml)

## Сторонние компоненты

Код `mpv_audio_kit` распространяется под BSD-3-Clause. Включённый в приложение движок libmpv распространяется под LGPL 3.0 или более поздней версии. Уведомления о библиотеках доступны из меню «О программе». Для распространения бинарников требуется сохранить лицензионные тексты, доступ к соответствующим исходникам и возможность замены движка. Версия `mpv_audio_kit` 0.7.2 использует libmpv из выпуска `r26`. [Условия распространения](https://github.com/ales-drnz/mpv_audio_kit/blob/main/LICENSES.md) · [Выпуск движка и `licenses.zip`](https://github.com/ales-drnz/mpv_audio_kit/releases/tag/libmpv-r26)
