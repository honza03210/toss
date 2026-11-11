# Toss

Share anything from your iPhone to your computer: tap **Share → Toss** in any app and it pops up
on your Linux, Windows or Mac computer as a notification. Links open in the browser, text goes to the
clipboard, and photos and files land in `Downloads/From iPhone` (dated when they were taken). The other
way, `toss send` or the file manager's **Send to iPhone** puts things in the app's inbox. It all goes
directly between phone and computer over the local network, encrypted, with no account and no cloud.

- `desktop/`: the computer app (`toss`), a single Go program with no dependencies, for Linux,
  Windows and macOS.
- `ios/`: the iPhone app (pairing, plus sending from inside the app) and its share extension.
- `docs/`: the website for GitHub Pages (install instructions, support, privacy policy).
- `PROTOCOL.md`: how they talk.

## How it works

1. `toss pair` on the computer prints a QR code. It holds a `toss://pair` link with the computer's
   addresses (`name.local` first, then IPs), its certificate fingerprint and a one-time token.
2. The iPhone scans it, either with the Camera app (which opens Toss for `toss://` links) or with
   the scanner in Toss. It pairs over HTTPS and receives a per-device key.
3. From then on, the share extension sends each item to `POST /v1/items`. It trusts only the
   certificate whose fingerprint was in the QR code, so another machine on the network can't pose
   as the computer. Items shared together make one notification ("3 photos from Hiha").
4. Files are sent resumably: if the connection drops, the computer keeps what arrived and the phone
   sends the rest. Photos and videos carry their capture date, which becomes the file's date.
5. Computer → phone: iOS doesn't let an app receive in the background without Apple's push
   service, so `toss send` queues items (for 7 days) and the app picks them up from
   `GET /v1/inbox` whenever it comes to the front, or when its "Get Files from Computer" Shortcut
   runs. Files go to the app's Documents folder, which the Files app shows as On My iPhone → Toss.

Pairings live in a keychain item shared by the app and the extension: the
`$(AppIdentifierPrefix)com.honza.Toss.shared` access group, set in both entitlements files.

## Computer app

Install (once published, see below):

```bash
curl -fsSL https://github.com/honza03210/toss/releases/latest/download/install.sh | sh
```

```powershell
irm https://github.com/honza03210/toss/releases/latest/download/install.ps1 | iex
```

These install for the current user (Linux: `~/.local/bin/toss`; Windows:
`%LOCALAPPDATA%\Programs\Toss`), verify the download's checksum, and set Toss to start at login
(Linux: XDG autostart; Windows: the HKCU Run key). The Windows installer offers to add a firewall
rule.

```
toss pair                 QR code for pairing an iPhone
toss status               running? settings, addresses, paired phones
toss devices / toss unpair <name>
toss config dir <folder>  where files go (default Downloads/From iPhone)
toss config autocopy off  don't copy received text and links automatically
toss config autoopen off  don't open received links in the browser automatically
toss open                 open the received-files folder
toss send <file>…         send to the iPhone (--text <text>, --clipboard, - for stdin, or a URL)
toss inbox [clear]        what waits for the phone
toss history [n|clear]    what was received recently
toss web [--pair]         local page: history with Copy/Open, drag-and-drop sending, pairing QR
toss install / uninstall  start at login (and now) / stop
```

`toss install` also adds **Send to iPhone** to the file manager: a script in Files (Nautilus, under
right-click → Scripts) and Nemo, a service menu in Dolphin, and Explorer's *Send to* menu on Windows.

The local page (`toss web`) listens on `127.0.0.1:47801` only. Its API needs the key in
`web-token` (passed in the URL fragment, sent back as a header, so other websites can't call it), and
it checks the Host header against DNS rebinding. It can only open files Toss received.

**Notifications:**
- **Linux:** `notify-send`, with buttons: Open, Show in folder (through the file manager's D-Bus
  API) and Copy. The clipboard uses `wl-copy`, `xclip` or `xsel`.
- **Windows:** native toasts through PowerShell's WinRT bridge, under Toss's own name (registered
  in `HKCU\Software\Classes\AppUserModelId`). Their buttons open the file, the folder or the link.
  The clipboard uses Win32 directly. `tossw.exe` is the same program without a console window, for
  running in the background; it shows a notification-area icon (click: the Toss page; right-click:
  pair, received files, quit).
- **macOS:** `terminal-notifier` if installed (clickable), else AppleScript notifications;
  `pbcopy`/`pbpaste`; a LaunchAgent for login. No Finder menu yet (use `toss send` or the page).
- Big files (50 MB and up) show a "Receiving…" notice that the final notice replaces.

Build and test (Go from go.dev; installed here in `~/.local/go`):

```bash
cd desktop && go test ./... && go build -o toss .
```

`Scripts/build-desktop.sh <version>` builds all the release archives into `dist/`.
`TOSS_QR_DIR=<dir> go test -run TestQRWritePNGs` followed by `python3 Scripts/check_qr.py <dir>`
checks the built-in QR encoder against OpenCV's decoder.

## iPhone app

Development builds install on your own iPhone with xtool. Use **`~/.local/bin/xtool-toss`**: it's
xtool 1.24 plus `Scripts/xtool-keychain-groups.patch`, which keeps the shared keychain group (stock
xtool replaces it with one group per bundle, so the extension wouldn't see the pairings). Rebuild
it with `Scripts/build-xtool.sh`.

```bash
cd ios
source ~/.local/share/swiftly/env.sh
~/.local/bin/xtool-toss dev run -c release
```

TossCore (the pairing link, request headers, state) is pure Swift. Test it on Linux from a scratch
package that symlinks only `Sources/TossCore` and `Tests/TossCoreTests` (the app package itself
can't build on Linux).

App Store builds: see [ios/AppStore/APPSTORE.md](ios/AppStore/APPSTORE.md). They're built and
uploaded by GitHub Actions on a Mac.

## Publishing

1. Create a GitHub repository, then run `Scripts/set-repo.sh <owner>/<repo>` and push.
2. Push a tag `v1.0.0`. The **Desktop** workflow tests on Linux and Windows, smoke-tests both
   installers, and publishes a release with `install.sh`, `install.ps1`, the archives and
   `checksums.txt`. The install commands above then work.
3. Turn on GitHub Pages (branch `main`, folder `/docs`).
4. Follow [ios/AppStore/APPSTORE.md](ios/AppStore/APPSTORE.md) for the App Store.
