# Livery

**One place to paint every RGB light on your Windows PC.** Pick a livery and Livery applies it to your keyboard, mouse, laptop chassis lights and anything [OpenRGB](https://openrgb.org) can see, all at once, from a small local control panel.

Livery is a single `.exe` with no installer. It runs quietly in the background, keeps your colors on after sleep, reboots and mouse wake-ups, and serves its control panel at `http://127.0.0.1:47820` (local only).

## Features

- **Seven built-in liveries** inspired by motorsport paint: Rosso, Rosso Grafite, Tricolore, Winter Test, Verde Arancio, Giallo and Rally.
- **Build your own**: main and accent colors, accent keys (WASD, arrows, function row), left-to-right stripes, per-light colors for laptop chassis lights and mice.
- **Brightness** and a synced **Breathe** effect.
- **Start at login** toggle (a normal per-user startup entry, no admin needed).
- **Self-healing**: re-applies after sleep, when a device is plugged in, when a wireless mouse wakes, and restarts itself if a device driver call hangs.

## Supported devices

Livery drives some devices itself and hands everything else to OpenRGB.

| Device | How | Notes |
|---|---|---|
| Alienware m18 R2 per-key keyboard (`0D62:AAB1`) | Built in | Per-key colors, accents, stripes, breathing |
| Alienware AW-ELC chassis lights (`187C:0551`): rear "Tron" bar, lid logo, power button | Built in | Power button colors are stored in the controller's power-state slots |
| Glorious GMBK 75% (`3794:B000` ANSI, `3794:B002` ISO) | Built in | Global color plus 10 column zones; settings are saved on the keyboard |
| Logitech wireless mice on a Lightspeed receiver with HID++ RGB feature `0x8070` (e.g. G502 Lightspeed) | Built in | Logo and primary zones |
| **Anything OpenRGB supports**: motherboards, RAM, GPUs, fans, LED strips, many keyboards and mice | Through the OpenRGB SDK server | Run OpenRGB with its SDK server on (see below) |

Other Alienware laptops that use the same controllers will probably work for chassis lights. The per-key keyboard map is currently specific to the m18 R2. Device reports and maps are welcome (see *Contributing*).

## Getting started

1. Download `livery.exe` from the [latest release](../../releases/latest) and put it in its own folder (Livery keeps `config.json` and a `logs` folder next to it).
2. Double-click it. The control panel opens in your browser.
3. Pick a livery. Turn on **Start Livery when I log in** if you want it to persist.

**To include OpenRGB devices:** install [OpenRGB](https://openrgb.org) (1.0 or newer). Either install it with the background service option, or open OpenRGB → *SDK Server* → *Start Server*. Livery connects to `127.0.0.1:6742` automatically and the **OpenRGB** chip in the panel shows how many devices it painted. On Windows, OpenRGB needs [PawnIO](https://pawnio.eu) for RAM and some motherboard lighting.

**Avoid two apps fighting over one device.** Close or disable lighting control in vendor apps (Alienware Command Center lighting, iCUE, Synapse, G HUB, Glorious CORE) for the devices Livery manages. Livery skips devices it drives itself when talking to OpenRGB.

### Command line

```
livery.exe            open the control panel (starts background mode if needed)
livery.exe run        background mode (what "start at login" runs)
livery.exe stop       stop background mode
livery.exe apply      apply the saved livery once and exit
livery.exe scan       list HID devices and what Livery recognises (read-only)
livery.exe install    enable start at login    |  uninstall   disable it and stop
```

## Safety notes

- Livery only talks to devices with the documented or observed commands listed in the source. A few settings are **persistent**: Alienware power-button/chassis colors are written to the lighting controller's power-state slots, and the GMBK stores its lighting on the keyboard. Livery only rewrites these when the colors actually change.
- The control panel listens on `127.0.0.1` only, checks the `Host` header, and requires a custom header on every change, so web pages can't drive it.
- This is hobby software provided as-is (see LICENSE). If Alienware lights ever stop responding, a full shutdown with the charger unplugged resets the lighting controller.

## Building

Requires Go 1.22+.

```
go mod tidy
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui" -o livery.exe .
go test ./...
```

The control panel (`ui.html`) is embedded in the binary.

## Contributing

`livery.exe scan` writes `logs/scan.txt` with every HID device's IDs and report layout, which is the first thing needed to add a device. Issues with that file attached, plus what the device is, are the most useful bug reports.

## Credits and trademarks

Livery's Alienware support builds on the protocol work of [alienfx-tools](https://github.com/T-Troll/alienfx-tools) and [alienware-m18-rgb](https://github.com/digin1/alienware-m18-rgb) (both MIT). See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

Livery is not affiliated with or endorsed by Dell/Alienware, Logitech, Glorious, the OpenRGB project, or any vehicle manufacturer. All product names and trademarks belong to their owners and are used only to identify compatible hardware.

## License

[MIT](LICENSE)
