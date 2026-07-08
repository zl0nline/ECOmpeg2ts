# ECOmpeg2ts

ECOmpeg2ts is a userspace MPEG-2 Transport Stream analyzer for Linux routers,
set-top boxes, and small ARM boards. It watches UDP, multicast, or file input,
tracks MPEG-TS continuity counters per PID, and renders a live console dashboard
with packet, bitrate, drop, TEI, and discontinuity statistics.

The project was started for Amlogic S905x on Armbian Linux
`6.18.37-ophub`, but it does not depend on a kernel module or a specific CPU
architecture.

## Features

- UDP, multicast, and file input.
- MPEG-TS sync-byte resynchronization.
- Per-PID continuity counter tracking.
- Drop, duplicate, TEI, discontinuity, scrambled, and payload/adaptation stats.
- Colored ANSI console dashboard with per-PID bitrate and drop-rate columns.
- Sortable PID table: `--sort drops|bitrate|pid`.
- `--no-clear` mode for SSH/tmux logging.
- JSON-lines mode for machine-readable output and integrations.
- Static Linux ARM64 and AMD64 build targets.

## Build

```sh
make test
make build
make linux-arm64
make linux-amd64
```

Binaries are written to `dist/`.

### eBPF / TC Mode (Linux only)

```sh
make ebpf-object          # standard: 8 TS packets per UDP (1500 MTU)
make ebpf-object-jumbo    # jumbo: 16 TS packets per UDP (jumbo frames)
make linux-arm64-tc       # build ecompeg2ts-tc for ARM64
```

## Install On Armbian ARM64

Download the `ecompeg2ts-linux-arm64` binary from the latest GitHub release,
then install it on the target box:

```sh
chmod +x ecompeg2ts-linux-arm64
sudo install -m 0755 ecompeg2ts-linux-arm64 /usr/local/bin/ecompeg2ts
ecompeg2ts --help
```

Optional systemd service example:

```sh
sudo cp docs/systemd/ecompeg2ts.service /etc/systemd/system/ecompeg2ts.service
sudo systemctl daemon-reload
sudo systemctl enable --now ecompeg2ts
```

Edit the multicast group, port, and interface in the service before enabling it.

## Examples

Analyze a UDP stream on all interfaces:

```sh
ecompeg2ts --udp :1234
```

Join a multicast group:

```sh
ecompeg2ts --multicast 239.10.10.10:1234 --iface eth0
```

Join a multicast group from an IPTV-style URL. The interface is optional; when
it is omitted, the OS chooses the multicast interface:

```sh
ecompeg2ts --source udp://@239.3.1.1:1234
ecompeg2ts udp://@239.3.1.1:1234
```

Read a transport stream file:

```sh
ecompeg2ts --file sample.ts
```

Emit JSON lines instead of dashboard:

```sh
ecompeg2ts --udp :1234 --json
```

Dashboard with sorting and SSH-friendly output:

```sh
ecompeg2ts --multicast 239.3.1.1:1234 --sort bitrate
ecompeg2ts --multicast 239.3.1.1:1234 --sort drops --no-clear
```

## TC/eBPF Mode

For many simultaneous streams on small ARM boards, `ecompeg2ts-tc` attaches a TC
ingress eBPF program and reads aggregated counters from BPF maps instead of
copying every stream into userspace. This reduces CPU usage by ~250x.

Attach via TCX (default on modern kernels), with automatic clsact/netlink
fallback for older kernels:

```sh
sudo ./ecompeg2ts-tc --iface eth0 --object dist/ecompeg2ts_tc_bpfel.o --join udp://@239.3.1.1:1234
```

Force clsact/netlink attach mode:

```sh
sudo ./ecompeg2ts-tc --iface eth0 --object dist/ecompeg2ts_tc_bpfel.o --clsact --join udp://@239.3.1.1:1234
```

Jumbo frame variant (16 TS packets per UDP datagram):

```sh
sudo ./ecompeg2ts-tc --iface eth0 --object dist/ecompeg2ts_tc_bpfel_jumbo.o --join udp://@239.3.1.1:1234
```

See [docs/ebpf/README.md](docs/ebpf/README.md) for technical details.

### BPF Special PIDs

| PID | Meaning |
|-----|---------|
| `0xfffe` | Oversized datagrams: `packets` = oversized UDP datagrams, `drops` = TS packets not parsed beyond the limit |
| `0xffff` | Sync byte losses: `sync_losses` counter increments when expected sync byte `0x47` is missing |

## Notes

Continuity counter drops are counted per PID only for packets carrying payload.
Adaptation-only packets do not advance the expected counter. Packets with the
adaptation-field discontinuity indicator reset the PID expectation and are
counted separately.

## Описание на русском

ECOmpeg2ts — userspace-анализатор MPEG-2 Transport Stream для Linux-роутеров,
приставок и небольших ARM-плат. Он читает UDP, multicast или файл, отслеживает
continuity counter по каждому PID и показывает живой консольный dashboard со
статистикой packets, bitrate, drops, TEI и discontinuity.

Проект начат под Amlogic S905x на Armbian Linux `6.18.37-ophub`, но не требует
отдельного kernel module и не привязан к конкретной CPU-архитектуре.

### Возможности

- Вход из UDP, multicast и файла.
- Resync по MPEG-TS sync byte.
- Учёт continuity counter по каждому PID.
- Счётчики drops, duplicates, TEI, discontinuity, scrambled, payload/adaptation.
- Цветной ANSI dashboard с per-PID bitrate и drop-rate.
- Сортировка PID-таблицы: `--sort drops|bitrate|pid`.
- Режим `--no-clear` для логов в SSH/tmux.
- JSON-lines режим для машинной обработки и интеграций.
- Статические Linux-сборки под ARM64 и AMD64.

### Сборка

```sh
make test
make build
make linux-arm64
make linux-amd64
```

Готовые бинарники складываются в `dist/`.

#### eBPF / TC режим, только Linux

```sh
make ebpf-object          # standard: 8 TS packets per UDP (1500 MTU)
make ebpf-object-jumbo    # jumbo: 16 TS packets per UDP (jumbo frames)
make linux-arm64-tc       # ecompeg2ts-tc для ARM64
```

### Установка на Armbian ARM64

Скачайте `ecompeg2ts-linux-arm64` из последнего GitHub release и установите на
целевую коробку:

```sh
chmod +x ecompeg2ts-linux-arm64
sudo install -m 0755 ecompeg2ts-linux-arm64 /usr/local/bin/ecompeg2ts
ecompeg2ts --help
```

Пример systemd service:

```sh
sudo cp docs/systemd/ecompeg2ts.service /etc/systemd/system/ecompeg2ts.service
sudo systemctl daemon-reload
sudo systemctl enable --now ecompeg2ts
```

Перед включением сервиса измените multicast group, port и interface под свой
поток.

### Примеры запуска

Анализ UDP-потока на всех интерфейсах:

```sh
ecompeg2ts --udp :1234
```

Подключение к multicast group:

```sh
ecompeg2ts --multicast 239.10.10.10:1234 --iface eth0
```

IPTV-style URL. Интерфейс опционален; если его не указать, ОС выберет multicast
interface сама:

```sh
ecompeg2ts --source udp://@239.3.1.1:1234
ecompeg2ts udp://@239.3.1.1:1234
```

Чтение transport stream файла:

```sh
ecompeg2ts --file sample.ts
```

Вывод JSON lines вместо dashboard:

```sh
ecompeg2ts --udp :1234 --json
```

Dashboard с сортировкой и SSH-friendly выводом:

```sh
ecompeg2ts --multicast 239.3.1.1:1234 --sort bitrate
ecompeg2ts --multicast 239.3.1.1:1234 --sort drops --no-clear
```

### TC/eBPF режим

Для множества одновременных IPTV-потоков на небольших ARM-платах
`ecompeg2ts-tc` цепляет TC ingress eBPF-программу и читает агрегированные
счётчики из BPF maps вместо копирования каждого потока в userspace. Это снижает
нагрузку на CPU примерно в 250 раз.

Attach через TCX по умолчанию, с автоматическим fallback в `clsact`/netlink на
старых ядрах:

```sh
sudo ./ecompeg2ts-tc --iface eth0 --object dist/ecompeg2ts_tc_bpfel.o --join udp://@239.3.1.1:1234
```

Принудительный `clsact`/netlink attach:

```sh
sudo ./ecompeg2ts-tc --iface eth0 --object dist/ecompeg2ts_tc_bpfel.o --clsact --join udp://@239.3.1.1:1234
```

Jumbo-вариант на 16 TS packets per UDP datagram:

```sh
sudo ./ecompeg2ts-tc --iface eth0 --object dist/ecompeg2ts_tc_bpfel_jumbo.o --join udp://@239.3.1.1:1234
```

Технические детали — в [docs/ebpf/README.md](docs/ebpf/README.md).

#### Специальные BPF PID

| PID | Значение |
|-----|----------|
| `0xfffe` | Oversized datagrams: `packets` = oversized UDP datagrams, `drops` = TS packets, которые не разобраны сверх лимита |
| `0xffff` | Sync byte losses: счётчик `sync_losses` растёт, когда ожидаемый sync byte `0x47` отсутствует |

### Замечания

Continuity counter drops считаются по PID только для packets с payload.
Adaptation-only packets не двигают ожидаемый counter. Packets с
adaptation-field discontinuity indicator сбрасывают ожидание по PID и считаются
отдельно.
