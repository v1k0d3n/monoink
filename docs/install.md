# Installing monoink

monoink is a [Decky Loader](https://decky.xyz) plugin. Everything happens in
Gaming Mode; you don't need a terminal or Desktop Mode.

## Before you start

- **Decky Loader** installed (see [decky.xyz](https://decky.xyz)).
- The **JSAUX E-Ink faceplate** switched on. It does **not** need to be
  paired in Steam's Bluetooth settings; leave it unpaired. The plugin finds
  it by itself.
- If you previously installed JSAUX's own software, uninstall it first with
  their uninstaller so two programs don't fight over the display.

## Install

1. Open the **Releases** page of this repository and download the newest
   `monoink-vX.Y.Z.zip` (the file under **Assets**). Save it somewhere easy
   to find, such as **Downloads**.
2. Press the **⋯** (Quick Access) button, open the **Decky** tab (the plug
   icon), then the **⚙ gear**.
3. Under **General**, turn on **Developer mode**.
4. A **Developer** tab appears in Decky's settings. Choose **Install Plugin
   From ZIP File** and pick the zip you downloaded.
5. **E-Ink Faceplate** now appears in the Decky menu.

Within a few seconds the plugin shows **Searching…**, then **Connected**
with the display's battery level, and the faceplate starts showing the
dashboard. The first display it connects to is remembered, so a neighbour's
faceplate will never be picked up by mistake.

## Update

Install the newer zip the same way. Your settings are kept.

## Set it up

Everything is in the plugin panel:

- **On the display**: a live copy of what's on the faceplate, plus **Next
  screen** and **Redraw now**.
- **Screens**: choose **Rotate** or pin one screen with **Always …**, and
  pick which screens rotate. **Show game while playing** switches to the
  game screen while a Steam game is running.
- **Clock & weather**: 12/24-hour clock, first day of the week, and your
  weather location (search by city name).
- **Photo frame**: choose a folder of pictures.
- **Providers**: approve or remove apps that send status cards. See
  [providers.md](providers.md).

The Clock, Dashboard and Performance screens update every minute. E-ink
panels repaint the whole screen with a brief black-and-white flash, which
is normal.

## Troubleshooting

| What you see | Try this |
| --- | --- |
| **Retrying — Display not found** | Make sure the faceplate is switched on and close by. Tap **Find displays** to check it's visible. |
| **Retrying — Bluetooth is turned off** | Turn Bluetooth on in Steam's settings. |
| It connects to the wrong display | **Find displays** → pick the right one. |
| The panel says the service isn't running | Restart Decky (Decky settings → General) or reboot. |

If it still doesn't work, please open an issue using the **Something isn't
working** form and attach the log from
`~/homebrew/logs/monoink/monoinkd.log`.

## Uninstall

Decky → ⚙ → **Plugins** → **E-Ink Faceplate** → **Uninstall**. monoink
doesn't change anything outside Decky's folders; its settings remain in
`~/homebrew/settings/monoink/` and can be deleted.
