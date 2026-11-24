#!/usr/bin/env python3
"""Enroll an unused lab debug app over its normal HTTPS sign-in screen."""
import argparse
import json
import os
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument("serial", help="an authorized adb device serial")
parser.add_argument("slot", choices=("1","2","3"), help="local lab invite number")
parser.add_argument("--state", type=Path, default=root / ".local/instance")
args = parser.parse_args()
_, dial, invite = (args.state / f"invite-{args.slot}.txt").read_text().splitlines()
from xml.etree import ElementTree
host = ElementTree.parse(args.state / "android-res/values/lab.xml").find("string").text
adb = ["adb","-s",args.serial]
for file in ("Ridge-Radio-lab-debug.apk","Ridge-Radio-instrumentation.apk"):
    subprocess.run([*adb,"install","-r","-t",str(root / "dist" / file)],check=True)
payload=json.dumps(dict(host=host,dial=dial,invite=invite)).encode()
subprocess.run([*adb,"shell","-T","run-as lab.ridgeradio sh -c 'umask 077; cat > cache/ridge-enroll.json'"],input=payload,check=True)
try:
    result=subprocess.run([*adb,"shell","am","instrument","-w","-e","class","lab.ridgeradio.HandsetTest#enrollLocalLab",
        "lab.ridgeradio.test/androidx.test.runner.AndroidJUnitRunner"],capture_output=True,text=True,check=True)
    if "OK (1 test)" not in result.stdout or "INSTRUMENTATION_STATUS_CODE: -3" in result.stdout:
        raise SystemExit("Enrollment did not complete. Check LAN reachability, invite expiry and existing sign-in.")
finally:
    subprocess.run([*adb,"shell","run-as","lab.ridgeradio","rm","-f","cache/ridge-enroll.json"],check=False)
subprocess.run([*adb,"shell","am","start","-W","-n","lab.ridgeradio/.MainActivity"],check=True)
print(f"Enrolled {dial}. Radio starts off. Use operator recovery to set a new password later.")
