# Livery

**One place to paint every RGB light on your Windows PC.** Pick a livery and Livery applies it to your keyboard, mouse, laptop chassis lights and anything [OpenRGB](https://openrgb.org) can see, all at once, from a small local control panel.

Livery is a single `.exe` with no installer. It runs quietly in the background, keeps your colors on after sleep, reboots and mouse wake-ups, and serves its control panel at `http://127.0.0.1:47820` (local only).

## Features

- **41 built-in liveries in nine categories**, named after colors rather than teams or sponsors:
  - *Italian Superbikes*: Rosso, Rosso Grafite, Tricolore, Winter Test, Verde Arancio, Giallo, Rally
  - *Group B Rally*: Turin Stripes, Blue & Gold, Turbo White, Night Stage, Tarmac Red
  - *Formula 1*: British Racing Green, Papaya, Silver & Teal, Black & Gold, Scarlet & Yellow, French Racing Blue, Midnight Blue
  - *Endurance*: Powder Blue & Orange, Purple & White, White, Navy & Gold, Endurance Night
  - *Expedition & Off-road*: Expedition Yellow, Desert Raid, Safari Green, Ice Raid
  - *Choppers & Customs*: Flame Job, Chrome & Black, Candy Apple, Black & Orange, Candy Purple, Stars & Stripes
  - *Stock Car*: Black & Silver, Speedway Blue & Red
  - *Rallycross & Gymkhana*: Gymkhana, Rallycross Blue
  - *Military*: Builder Navy & Gold, Eagle Black & Gold, Pirate Squadron, Sundown Squadron
- **Wi-Fi smart bulbs**: Feit Electric and other Tuya-based bulbs and light strips follow your livery, controlled directly on your home network.
- **Keyboard screen animations** (Ajazz AK820 Pro): a spinning street coupe, tuner sedan with underglow, or superbike on a neon grid in colors you pick.
- **Updates in place**: Livery checks GitHub once a day and installs new releases with one click, keeping your settings.
- **Build your own**: main and accent colors, accent keys (WASD, arrows, function row), left-to-right stripes, per-light colors for laptop chassis lights and mice.
- **Effects with speed and intensity controls**: Static, Breathe, Flicker (failing-neon stutter, in sync across devices), Strobe, Wave (the livery rolls across your setup, either direction), Redline (a rev counter with limiter flash) and Sparkle. Livery animates the Alienware keyboard and chassis lights and every OpenRGB device itself; devices that store lighting in their own memory or run on battery use their built-in breathing/wave or stay on the livery colors, and the power button is never animated. The panel shows a live preview.
- **Start at login** toggle (a normal per-user startup entry, no admin needed).
- **Self-healing**: re-applies after sleep, when a device is plugged in, when a wireless mouse wakes, and restarts itself if a device driver call hangs.

## Supported devices

Livery drives some devices itself and hands everything else to OpenRGB.

| Device | How | Notes |
|---|---|---|
| Alienware m18 R2 per-key keyboard (`0D62:AAB1`) | Built in | Per-key colors, accents, stripes, breathing |
| Alienware AW-ELC chassis lights (`187C:0551`): rear "Tron" bar, lid logo, power button | Built in | Power button colors are stored in the controller's power-state slots |
| Glorious GMBK 75% (`3794:B000` ANSI, `3794:B002` ISO) | Built in | Global color plus 10 column zones; settings are saved on the keyboard |
| Ajazz AK820 Pro (`0C45:8009` 2.4 GHz receiver, `0C45:800A` cable) | Built in | Whole-keyboard color and breathing (the keyboard has no per-key lighting command); not yet tested on real hardware, reports welcome |
| Logitech wireless mice on a Lightspeed receiver with HID++ RGB feature `0x8070` (e.g. G502 Lightspeed) | Built in | Logo and primary zones |
| Tuya-based Wi-Fi bulbs and strips (Feit Electric, Smart Life, many store brands), protocol 3.1 to 3.5 | Built in, over your Wi-Fi | One-time setup below; each bulb shows the main or accent color, or Auto spreads the livery across bulbs |
| Zone-lit keyboards through OpenRGB (e.g. SteelSeries Apex 3 / Apex 3 TKL) | Through OpenRGB | Stripes spread across the zones and accent columns light the matching zones; Breathe is animated by Livery |
| **Anything OpenRGB supports**: motherboards, RAM, GPUs, fans, LED strips, many keyboards and mice | Through the OpenRGB SDK server | Run OpenRGB with its SDK server on (see below) |

Other Alienware laptops that use the same controllers will probably work for chassis lights. The per-key keyboard map is currently specific to the m18 R2. Device reports and maps are welcome (see *Contributing*).

## Getting started

1. Download **`Livery-Setup-<version>.exe`** from the [latest release](../../releases/latest) and run it.
2. Pick what to install:
   - **Livery** goes in your user folder (`%LOCALAPPDATA%\Programs\Livery`). No admin rights needed.
   - **OpenRGB** (ticked if it isn't installed yet). Setup downloads the official OpenRGB installer from codeberg.org, checks its SHA-256, and installs it with its background service so the SDK server is always on. Windows asks for permission.
   - **PawnIO** (ticked if missing). OpenRGB needs this driver on Windows for RAM and some motherboard lighting. It's downloaded from its official GitHub release and checked the same way.
   - **Start Livery when I sign in**.
3. Finish opens the control panel. Pick a livery.

Livery shows up in *Settings → Apps* and uninstalls from there. OpenRGB and PawnIO stay installed unless you remove them too.

Windows SmartScreen may warn that the installer is from an unknown publisher, because it isn't code-signed. Choose *More info → Run anyway* if you trust the download, or build it yourself (see *Building*).

**Portable use:** `livery.exe` also runs on its own from any folder. It keeps `config.json` and a `logs` folder next to itself.

**OpenRGB:** if OpenRGB is installed but its server isn't running, Livery starts it minimized to the tray with `--server`. If you installed OpenRGB yourself without its service, that's all you need. Livery connects to `127.0.0.1:6742`, and the **OpenRGB** chip in the panel shows how many devices it painted.

### Smart bulbs (Feit Electric and other Tuya bulbs)

Tuya bulbs only accept local commands encrypted with a per-bulb key, which you get once from Tuya's free developer site:

1. Add the bulbs to the **Smart Life** app.
2. At [platform.tuya.com](https://platform.tuya.com), create a cloud project (Cloud → Development → Create Cloud Project) using your country's data center.
3. In the project, open **Devices → Link App Account** and scan the QR code with Smart Life.
4. In Livery's **Smart bulbs** section, enter the project's Access ID and Access Secret and click **Connect and find bulbs**.

Livery reads the keys, finds the bulbs on your network (it only makes outgoing connections to port 6668, so no firewall changes), and from then on talks to them directly. The Access Secret is stored encrypted with Windows DPAPI.

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

## Updates

Livery checks this repository's latest release once a day (turn it off under *Startup* in the panel). When a newer version exists, the panel offers **Install update**: Livery downloads the release's `Livery-Setup-x.y.z.exe`, checks it against the SHA-256 published with the release, and runs it silently; your settings and liveries are kept and Livery restarts on the new version. Releases need the installer attached plus its SHA-256 (GitHub shows asset digests automatically; a line like `<sha256>  Livery-Setup-x.y.z.exe` in the release notes also works).

## Building

Requires Go 1.22+, plus NSIS and mingw-w64 binutils for the installer and icon (`apt install nsis binutils-mingw-w64-x86-64` on Debian/Ubuntu).

```
./build.sh        # build/livery.exe and build/Livery-Setup-<version>.exe, runs the tests
```

Just the app: `GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui" -o livery.exe .` The control panel (`ui.html`) is embedded in the binary.

## Contributing and support reports

In the control panel, **Help → Create support report** (or Start menu → *Livery support report*) saves `Livery-report-<date>.zip` to the Desktop: Livery's recent logs, its status, OpenRGB's device list and every USB/HID device's IDs and report layout. It only reads devices, and leaves out Tuya keys, the cloud secret and project ID, and your Windows user name. Attach it to an issue together with what the device is; that's the first thing needed to fix a problem or add a device.

## Credits and trademarks

Livery's Alienware support builds on the protocol work of [alienfx-tools](https://github.com/T-Troll/alienfx-tools) and [alienware-m18-rgb](https://github.com/digin1/alienware-m18-rgb) (both MIT). See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

Livery is not affiliated with or endorsed by Dell/Alienware, Logitech, Glorious, the OpenRGB project, or any vehicle manufacturer. All product names and trademarks belong to their owners and are used only to identify compatible hardware.

## License

[MIT](LICENSE)
