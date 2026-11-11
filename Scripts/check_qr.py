# Decodes the PNGs written by `TOSS_QR_DIR=<dir> go test -run TestQRWritePNGs` with OpenCV
# and checks each against the expected text: python3 Scripts/check_qr.py <dir>
import os, sys
import cv2

d = sys.argv[1]
names = open(os.path.join(d, "manifest.txt")).read().split()
detector = cv2.QRCodeDetector()
bad = 0
for name in names:
    want = open(os.path.join(d, name + ".txt"), encoding="utf-8").read()
    got, _, _ = detector.detectAndDecode(cv2.imread(os.path.join(d, name)))
    ok = got == want
    bad += not ok
    print(("ok  " if ok else "FAIL"), name, len(want), "bytes")
print(f"{len(names) - bad}/{len(names)} decoded correctly")
sys.exit(1 if bad else 0)
