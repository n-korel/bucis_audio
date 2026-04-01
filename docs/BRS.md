# BRS-01 — Описание проекта

Документ описывает структуру, назначение и работу программного обеспечения **БРС-01** (блок речевой связи), входящего в состав системы «Сармат» (ЦИК Москва, САЕШ.465489.127).

---

## 1. Назначение системы

- **БРС (блок речевой связи)** — устройство речевой связи в составе (вагоне/голове).
- Связь между блоками и с **БУЦИС** (блоком управления) — по IP-сети **192.168.5.0/24**.
- Реализованы: **SIP-телефония** (Sofia-SIP), **RTP-аудио** (GStreamer, G.726), **резервирование** активного БУЦИС по keepalive, **тестовый режим** (Tester + Player на устройстве).

---

## 2. Состав проекта (приложения и библиотеки)

Проект собирается как **несколько отдельных Qt-приложений** (qmake, `.pro`), без единого корневого `.pro`. Целевая ОС — **Linux** (в т.ч. встраиваемая, cortexa9, RISC-V в закомментированных вариантах). Установка: `/opt/<TARGET>/bin` или `/tmp/` для QNX.

| Компонент       | Тип        | Назначение |
|-----------------|------------|------------|
| **Manager**     | Приложение | Основная логика БРС: SIP-телефония, выбор активного БУЦИС, OpenSIPS, воспроизведение/звуки, работа с настройками. |
| **UdpHandler**  | Приложение | Приём/отправка UDP-команд в сети 192.168.5.x, парсинг keepalive/тестовых команд, проброс событий в Manager по D-Bus. |
| **Player**      | Приложение | Тестовая программа на устройстве: запись/воспроизведение по командам от Tester (по UDP/D-Bus), тесты 300/1000/3200 Гц, LED. |
| **Tester**      | Приложение | GUI (Qt Widgets) на ПК: управление тестами БРС по UDP (stop, record, 300/1000/3200, LED, запуск теста «Бес»). |
| **ButtonHandler** | Приложение | Обработка кнопок через GPIO (D-Bus адаптор). Зависит от **GpioLib**. |
| **GpioLib**     | Библиотека | Работа с GPIO (export, direction, set/get, edge). Используется ButtonHandler. |

Дополнительно в составе репозитория: **libs/sofia_arm** — библиотека **Sofia-SIP** (SIP-стек) для ARM.

---

## 3. Стек технологий

- **Язык:** C++ (C++17 в приложениях, C++14 в GpioLib).
- **Qt:** 5.15.2 (указано в Manager), модули: core, gui, widgets, network, dbus, multimedia.
- **SIP:** Sofia-SIP (libsofia-sip-ua), вызовы из потока SipClient (nua, NUA events).
- **Медиа:** GStreamer (gst-1.0, gstvideo, gstbase, gstapp), ALSA (libasound), libsndfile, fftw3.
- **Межпроцессное взаимодействие:** D-Bus (сигналы/методы между UdpHandler ↔ Manager, Manager ↔ Player/ButtonHandler).
- **Сборка:** qmake (Qt), без CMake.

---

## 4. Сетевая модель и протоколы

- **Подсеть:** 192.168.5.0/24, broadcast 192.168.5.255.
- **Фиксированные адреса (из кода):**
  - БУЦИС: 192.168.5.251 (BUCIS1), 192.168.5.252 (BUCIS2).
  - BRS: например .201, .202 (номера устройств 300, 400 и 100, 200 для БУЦИС).
- **Порты (UdpHandler):**
  - **8890** — EC keepalive (`ec_server_keepalive`, `ec_client_conversation`).
  - **8891** — обычный keepalive БУЦИС (`keepalive`).
  - **8888** — тестовые команды (stop, record, 300/1000/3200, TestLed*, startTestBes, BesTestStarted/BesTestStoped).
  - **8889** — синхронный звук (`sound_start` / `sound_stop`).
  - **6710** — PORT_LISTEN_BUCIS (в proto).
  - **8892/8881** — метрики (PORT_LISTEN_METRICS, PORT_SEND_METRICS).

Формат UDP-сообщений парсится в **UdpHandler** (CmdHandler + пакеты типа KeepAlive, KeepAliveEC, TestCmd). По порту определяется тип трафика (EC, keepalive, тест).

---

## 5. Взаимодействие приложений (D-Bus и роли)

- **UdpHandler** — единственный процесс, слушающий UDP. Получив keepalive или тестовую команду, эмитит сигналы D-Bus:
  - `BRS01.UdpHandler`: `alive`, `aliveEC`, `startTestBes`, `stopSig`, `recordSig`, `play300`/`play1000`/`play3200`, `TestLedOn`/`TestBlink`/`TestLedOff`; методы: `sendKeepAlive`, `sendKeepAliveEC`, `onAnswer`, `onSoundStart`/`onSoundStop`.
- **Manager** — подписывается на эти сигналы (через адаптеры D-Bus), содержит класс **BRS** (наследник AbstractUnit): логика SIP, выбора активного БУЦИС, запуска OpenSIPS, воспроизведения треков (занято/конец/вход и т.д.).
- **Player** — подписывается на те же тестовые сигналы от UdpHandler и на кнопки от ButtonHandler; по таймеру шлёт «я жива» тестовой программе на ПК (`onTimeout` → D-Bus → UdpHandler → UDP «BesTestStarted» на IP тестера).
- **Tester** (ПК) — только UDP: шлёт команды на 192.168.5.255:8888 и принимает ответы о старте/остановке теста.

То есть: **UdpHandler** — шлюз между сетью и D-Bus; **Manager** и **Player** — потребители событий по D-Bus; **ButtonHandler** — источник событий кнопок по D-Bus.

---

## 6. Логика Manager (BRS) и резервирование

- **AbstractUnit** — базовый класс «блока»: общие `restart()`/`stop()` для телефона и ссылка на **TelephoneHandler** и **DbusButtonhandler**.
- **BRS** хранит:
  - `currentBusicIp` — IP текущего активного БУЦИС (на нём поднят OpenSIPS);
  - счётчики `countAlive`, `countAliveEC`, `countAnotherBRS` и таймер раз в 1 с.

Поведение по таймеру:

- Пока приходят **keepalive** от ближайшего БУЦИС (`Utils::ipNearBUCIS`) — считаем его живым, сбрасываем `countAlive`/`countAliveEC`, при необходимости не трогаем телефон.
- Когда **countAlive** обнуляется (никто не шлёт keepalive) — BRS сам начинает слать `sendKeepAlive()`.
- Когда **countAliveEC** обнуляется — решается, кто поднимает OpenSIPS:
  - если «мы» (наш БРС — резерв или первый запуск, по условию `currentBusicIp == ipNearBUCIS || currentBusicIp == "" || countAnotherBRS == TIME`) — шлём `sendKeepAliveEC()`, запускаем `systemctl start opensips`, вызываем `opensipsIp(selfIp)`, перечитываем SIP-настройки (bind-файл) на наш сервер, отключаем автоответ.
  - иначе даём время второму БРС (`countAnotherBRS++`).
- При приходе **aliveEC** от другого узла — обновляем `currentBusicIp`, при необходимости вызываем `opensipsIp(ip)` и `telephoneHandler->restart()` (перепривязка к чужому OpenSIPS), включаем автоответ.

Таким образом, **только один узел в сегменте поднимает OpenSIPS** и считается «держателем» EC; остальные к нему подключаются по SIP. При смене «головы» (status в aliveEC) выполняется перезапуск телефона и очистка очереди звонков.

---

## 7. SIP и телефония

- **SipService** — обёртка над **SipClient** (Sofia-SIP, nua). Читает **SipSettings** из настроек (в т.ч. из bind-файла).
- **TelephoneHandler** — оркестратор: регистрация, входящие/исходящие звонки, очередь вызовов (QueueIncomingCalls), воспроизведение звуков (занято, конец, вход) через **Stream** (GStreamer).
- **Stream** — тонкая обёртка над фабрикой RTP (**IFactoryRtp** → **RtpGstService**): пайплайн GStreamer задаётся строкой (из settings.ini или кода: alsasrc → webrtcdsp → avenc_g726 → rtpg726pay → udpsink и обратный путь для приёма).
- Настройки SIP и путь к трекам: `/opt/sarmat/` (SettingsService), треки в `/opt/sarmat/tracks/`, конфиг в `settings.ini` (секции GSTREAMER, UNIT).

Режимы телефона (MODE_TELEPHONE): UNDEFINED, REGISTRATION, REGISTERED, INCOMING_CALL, CALLING, ANSWERED, CALL. Обработка кнопки (в т.ч. «тангента» TD): в режиме разговора — ответ/завершение/микрофон; иначе — звуковой тест (pipeLineTD, RTP на 192.168.5.255) и сигналы sound_start/sound_stop по D-Bus в UdpHandler.

---

## 8. Настройки и конфигурация

- **Путь приложения:** `/opt/sarmat/` (настройки, треки, bind-файлы для SIP).
- **SettingsService** (Manager): чтение/запись bind-файлов (BindFileParser, BindFileWriter), актуальный SipSettings (getActualBindFile, checkActualBindFile), FileWatcher за изменением файлов.
- **writeBindFile(dest, dest_ip, domain, user)** — формирование конфига при смене активного БУЦИС (domain — IP OpenSIPS).
- **settings.ini:** секция [GSTREAMER] — пайплайн с webrtcdsp, G.726, RTP; [UNIT] — IS_BUSIC.
- **Utils:** статические переменные для подсети (ipNearBUCIS, ipFarBUCIS, ipAnotherBRS), TIME (таймаут keepalive), pipeLinePlay/pipeLineTD, путь к трекам.

Исключения при старте Manager: SettingsReaderException, SettingsWriterException, SipErrorException — логируются, пауза 1 с, выход с -1.

---

## 9. Тестовый сценарий (Tester и Player)

- **Tester** (ПК): определяет свой IP в 192.168.5.0/24, по кнопкам шлёт UDP на 192.168.5.255:8888: `stop`, `record`, `300`, `1000`, `3200`, `startTestBes <currentHeadIp>`, `TestLedOn`/`TestBlink`/`TestLedOff`.
- **UdpHandler** на устройстве принимает эти команды и через D-Bus даёт сигналы Manager и Player. По `startTestBes` Manager может завершиться с кодом 100, а UdpHandler запоминает IP тестера и по сигналу от тестовой программы шлёт на этот IP:7777 `BesTestStarted` / `BesTestStoped`.
- **Player** по таймеру вызывает D-Bus `onTimeout` → UdpHandler шлёт «BesTestStarted» тестеру. При получении `startTestBes` проверяет наличие исполняемого файла Manager и при наличии шлёт `onStopTester` и выходит с кодом 101 (чтобы переключиться обратно на Manager).

То есть тестовая оболочка на устройстве — это **Player**; **Tester** на ПК только генерирует команды и отображает статус.

---

## 10. Кнопки и GPIO

- **ButtonHandler** использует **GpioLib** (gpio export/direction/set/get/edge) и два **StateService** (пины 17 и 21), эмитит сигналы `clicked`, `clickedTD`, `release` по D-Bus (адаптор dbusbuttonhandler.xml).
- Manager (BRS) и Player подписываются на эти сигналы через D-Bus и обрабатывают короткое/длинное нажатие (обычный клик и TD — «тангента»).

---

## 11. Перечисляемые типы и константы (proto.h, Manager)

- **TYPE_DEVICE / TYPE_DEVICE_DATA** — идентификаторы устройств (MIES, MDU, BNT, BIT, BES, BMT, MS_05, PKK_03, BPM_01, MUS_01, MSK_05, MP_1310 и резервы).
- **DEVICE_NUMBER:** BUCIS1=100, BUCIS2=200, BRS1=300, BRS2=400.
- **TYPE_DATA:** WAV, A_LAW, U_LAW, OGG, BIT, BMT, MP3.
- **CMD, Class_Operations, TYPE_OPERATION** — команды и операции протокола обмена с устройствами (FLASH, поток, сброс и т.д.).
- **STATE_BRS28:** CHANGE_FILE, CHANGE_OPENSIPS_IP, UNREGISTERED, REGISTRATION, REGISTERED.
- **MODE_TELEPHONE** — см. выше.
- **ECAbonent** — структура (id, bst, conn) для абонентов EC.

В UdpHandler в proto вынесены в основном сетевые константы и TYPE_DEVICE; версия сборки читается из `/opt/sarmat/version.txt`.

---

## 12. Сборка и развёртывание

- Каждый модуль собирается отдельно: `qmake` по соответствующему `.pro` (Manager, UdpHandler, Player, Tester, ButtonHandler, GpioLib).
- Manager требует путь к sysroot и библиотекам (например, `/opt/seco-imx-fb/4.19-warrior/sysroots/...`) для glib, GStreamer, Sofia-SIP, alsa, sndfile, fftw3.
- GpioLib — библиотека, выход в `ReleaseX86_64/` или `DebugX86_64/` (при Qt 5.15.2).
- Установка: `target.path = /opt/$${TARGET}/bin`, при необходимости `INSTALLS += target`.

Итог: проект представляет собой **набор из нескольких Qt-приложений и одной библиотеки** для блока речевой связи БРС-01 с SIP-телефонией, резервированием по keepalive, тестовым режимом по UDP и управлением по D-Bus и GPIO.

---

## 13. Сверка документа с кодом `BRS/`

Проведена выборочная и целевая сверка `BRS.md` по исходникам в `BRS/` (Manager, UdpHandler, Player, Tester, ButtonHandler, GpioLib, `.pro`, `proto.h`, D-Bus XML).

### Подтверждено кодом

- Архитектура из отдельных Qt-приложений и `GpioLib`, без единого корневого `.pro` (есть только модульные `.pro`).
- Порты и UDP-роль `UdpHandler`: `8890`, `8891`, `8888`, `8889`, `6710`, `8892`, `8881` подтверждаются в `UdpHandler/proto.h` и использовании.
- D-Bus контракт `BRS01.UdpHandler` (сигналы `alive`, `aliveEC`, `startTestBes`, `TestLed*`; методы `sendKeepAlive*`, `onSoundStart/Stop`) подтвержден в `dbusudphandler.xml` и адаптерах.
- Логика резервирования в `Manager/units/brs.cpp` подтверждает сценарий с `countAlive`, `countAliveEC`, `countAnotherBRS`, запуском/остановкой OpenSIPS и перепривязкой SIP.
- Тестовый контур Tester/Player/UdpHandler (команды `startTestBes`, `stop`, `record`, `300/1000/3200`, `TestLed*`, ответы `BesTestStarted/BesTestStoped`) подтвержден.
- GPIO и кнопки через `ButtonHandler` + `GpioLib` с D-Bus сигналами `clicked`, `clickedTD`, `release` подтверждены.
- Пути `/opt/sarmat/`, `settings.ini`, треки `/opt/sarmat/tracks/`, чтение версии `/opt/sarmat/version.txt` подтверждены.
- Настройки сборки (`c++17` для приложений, `c++14` для `GpioLib`, `target.path` для Linux/QNX) подтверждены в `.pro`.

### Подтверждено частично (есть допущения в формулировках)

- Формулировка про «фиксированные адреса BRS .201/.202» в документе дана как пример; в коде жёстко зафиксированы адреса БУЦИС (`192.168.5.251`, `192.168.5.252`), а адреса BRS зависят от конфигурации.
- Формулировка про «только один узел поднимает OpenSIPS» соответствует целевой логике в `brs.cpp`, но это runtime-поведение с зависимостью от таймеров/сети, а не compile-time гарантия.
- Раздел по стеку (Qt/GStreamer/Sofia-SIP) подтвержден по зависимостям и использованию, но часть деталей пайплайнов берётся из `settings.ini` и может меняться конфигом.

### Вывод

`BRS.md` в целом соответствует коду в `BRS/`. Критичных расхождений не обнаружено; есть несколько пунктов, которые корректно трактовать как «типовой сценарий/конфигурация», а не как абсолютную жёсткую константу для всех запусков.
