# Publishing Toss on the App Store

Development builds go to your own iPhone with xtool (see the README). App Store builds are made
on a Mac in GitHub Actions (`.github/workflows/ios-appstore.yml`), because App Store Connect
requires an Xcode-compiled asset catalog and distribution signing, which xtool doesn't do.

## One-time setup

1. **Register the bundle IDs.** On developer.apple.com → Identifiers, add `com.honza.Toss` and
   `com.honza.Toss.TossShare` (explicit App IDs, no extra capabilities needed). Xcode's automatic
   signing can also create them on the first CI run.
2. **Create the app in App Store Connect** (My Apps → +): platform iOS, bundle ID `com.honza.Toss`,
   SKU e.g. `toss-ios`. The name must be unique on the store. "Toss" alone is almost certainly taken,
   so use something like **"Toss: Share to Computer"** (the name under the icon stays "Toss").
3. **API key for CI.** App Store Connect → Users and Access → Integrations → App Store Connect API:
   a key with the **Admin** role (automatic signing has to create distribution certificates and
   profiles). The key xtool uses may already qualify; check its role there.
4. **GitHub secrets** (repository → Settings → Secrets and variables → Actions):
   `ASC_KEY_ID`, `ASC_ISSUER_ID`, `ASC_KEY_P8` (the whole .p8 file), `APPLE_TEAM_ID`.
5. **GitHub Pages:** Settings → Pages → deploy from branch `main`, folder `/docs`. That hosts
   the support and privacy pages the listing links to.

## Each release

1. Actions → **iOS App Store** → Run workflow, version e.g. `1.0.0` (or push a tag `ios-v1.0.0`).
   The build number is the workflow run number, so it always increases.
2. The build appears in App Store Connect → TestFlight after processing (~15 min). Install it on
   your phone through TestFlight and check it once.
3. In App Store Connect, fill in the listing (below), pick the build, and **Add for Review**.

## Listing

**Subtitle** (30): Share to your Linux & Windows PC

**Promotional text** (170): Tap Share → Toss on a photo, link, note or file, and it pops up on your computer a second later. No account, no cloud: straight over your own network.

**Description:**

    Toss sends anything from your iPhone to your computer with the share button.

    • Photos and videos land in your computer's Downloads folder, in full quality.
    • Links open right away in your computer's browser.
    • Text goes straight to the computer's clipboard.
    • Files from any app — PDFs, documents, voice memos — arrive as they are.
    • Back Tap and Shortcuts: send your clipboard, or get the computer's, without opening a share sheet.
    • The other way too: right-click a file on the computer → Send to iPhone, and it's waiting in Toss.

    It works with Linux (GNOME, KDE and others), Windows 10 and 11, and macOS.

    Private by design: Toss has no account, no cloud and no tracking. Everything goes directly from
    your phone to your computer over your local network, encrypted, and the phone only talks to the
    computer you paired by scanning its QR code.

    Setup takes a minute: install the free Toss companion on your computer with one command (see
    the website), run "toss pair", and scan the code with your iPhone's camera.

**Keywords** (100): share,send,computer,pc,linux,windows,mac,transfer,photos,files,clipboard,backtap,desktop,link

**Support URL:** https://OWNER.github.io/toss/ · **Privacy policy URL:** https://OWNER.github.io/toss/privacy.html

**Category:** Utilities (secondary: Productivity) · **Age rating:** 4+ (no objectionable content)

**App Privacy:** "Data Not Collected". The app sends user content only to the user's own device
and never to the developer, which Apple doesn't count as collection.

**Export compliance:** the Info.plist already says `ITSAppUsesNonExemptEncryption = NO` (HTTPS/TLS only).

**Screenshots:** iPhone 6.9" (1320 × 2868) or 6.5" (1284 × 2778), at least 3. Suggested: the share
sheet with Toss, the sending sheet with "Sent", the computer's notification (a photo of the
screen or a mock-up), the pairing screen.

## App Review notes

Reviewers need a computer to test with. Put this in **App Review Information → Notes**, and attach a
short screen recording of the whole flow if you can (it speeds review up a lot):

    Toss is a companion app: it sends shared items to the user's own computer running the free
    open-source Toss desktop app (Linux, Windows or macOS).

    To test:
    1. On a Windows PC, in PowerShell: irm https://github.com/OWNER/toss/releases/latest/download/install.ps1 | iex
       (or on Linux: curl -fsSL https://github.com/OWNER/toss/releases/latest/download/install.sh | sh)
    2. Run: toss pair   — a QR code appears.
    3. Scan it with the iPhone camera, tap the banner, then Pair (allow Local Network access).
    4. In Photos or Safari, tap Share → Toss. The item appears on the computer as a notification.
    5. Computer to phone: on the computer run  toss send --text "hello"  then open Toss: it appears
       under "From your computers". Save to Photos there asks for add-only Photos access.

    The phone and computer must be on the same network. No account or login is involved.

## Before the first submission, check

- [ ] `Links.swift`, the website and the installers point at the real repository (Scripts/set-repo.sh)
- [ ] A desktop release exists (push a `v1.0.0` tag) so the install commands work for reviewers
- [ ] TestFlight build tested on a phone: pairing, share from Photos, Safari, Notes, Files
- [ ] Screenshots, listing, privacy answers filled in
