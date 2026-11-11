# Writes Generated/*.plist for the Xcode (App Store) build: the same plists xtool uses
# (../Info.plist, ../TossShare-Info.plist) plus the keys xtool fills in by itself.
# Version numbers come from MARKETING_VERSION / CURRENT_PROJECT_VERSION (set in CI).
import os, plistlib

here = os.path.dirname(os.path.abspath(__file__))
common = {
    "CFBundleIdentifier": "$(PRODUCT_BUNDLE_IDENTIFIER)",
    "CFBundleExecutable": "$(EXECUTABLE_NAME)",
    "CFBundleName": "$(PRODUCT_NAME)",
    "CFBundlePackageType": "$(PRODUCT_BUNDLE_PACKAGE_TYPE)",
    "CFBundleInfoDictionaryVersion": "6.0",
    "CFBundleDevelopmentRegion": "en",
    "CFBundleShortVersionString": "$(MARKETING_VERSION)",
    "CFBundleVersion": "$(CURRENT_PROJECT_VERSION)",
}
app_only = {"LSRequiresIPhoneOS": True, "UILaunchScreen": {}}

os.makedirs(os.path.join(here, "Generated"), exist_ok=True)
for source, out, extra in [("Info.plist", "Toss-Info.plist", app_only), ("TossShare-Info.plist", "TossShare-Info.plist", {})]:
    with open(os.path.join(here, "..", source), "rb") as f:
        plist = plistlib.load(f)
    plist.update(common)
    plist.update(extra)
    with open(os.path.join(here, "Generated", out), "wb") as f:
        plistlib.dump(plist, f, sort_keys=True)
    print("wrote", out)
