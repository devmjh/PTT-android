#!/usr/bin/env python3
"""Initialize one private LAN installation with three expiring enrollment codes."""
import argparse
import ipaddress
import os
from pathlib import Path
import secrets
import shlex
import subprocess

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument("address", type=ipaddress.IPv4Address, help="LAN IPv4 address of this computer")
parser.add_argument("--state", type=Path, default=root / ".local/instance")
parser.add_argument("--binary", type=Path, default=root / "dist/ridge")
args = parser.parse_args()
ip = str(args.address)
state = args.state.resolve()
os.umask(0o077)
state.mkdir(parents=True, exist_ok=False)
os.chmod(state, 0o700)

def openssl(*a):
    subprocess.run(["openssl", *a], cwd=state, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

openssl("req", "-new", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes",
        "-days", "365", "-subj", "/CN=Ridge Radio lab CA", "-addext", "basicConstraints=critical,CA:TRUE",
        "-addext", "keyUsage=critical,keyCertSign,cRLSign", "-keyout", "ca.key", "-out", "ca.pem")
openssl("req", "-new", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes", "-subj",
        "/CN=" + ip, "-keyout", "server.key", "-out", "server.csr")
(state / "cert.ext").write_text(f"subjectAltName=IP:{ip}\nbasicConstraints=critical,CA:FALSE\nkeyUsage=digitalSignature\nextendedKeyUsage=serverAuth\n")
openssl("x509", "-req", "-days", "90", "-in", "server.csr", "-CA", "ca.pem", "-CAkey", "ca.key",
        "-CAcreateserial", "-extfile", "cert.ext", "-out", "server.pem")
values = {
    "RIDGE_DB": str(state / "ridge.db"), "RIDGE_CERT": str(state / "server.pem"),
    "RIDGE_KEY": str(state / "server.key"), "RIDGE_BIND": ip + ":8443", "RIDGE_MEDIA_BIND": ip + ":40000",
    "RIDGE_PUBLIC_IP": ip, "RIDGE_TURN_BIND": ip + ":3478",
    "RIDGE_ICE_URLS": f"turn:{ip}:3478?transport=udp,turn:{ip}:3478?transport=tcp",
    "RIDGE_TURN_SECRET": secrets.token_hex(32),
}
(state / "server.env").write_text("".join(f"export {k}={shlex.quote(v)}\n" for k,v in values.items()))
# Optional debug-build resources limit this CA to the chosen HTTPS host.
res = state / "android-res"
for folder in ("raw", "xml", "values"):
    (res / folder).mkdir(parents=True)
(res / "raw/ridge_lab_ca.pem").write_bytes((state / "ca.pem").read_bytes())
(res / "xml/debug_trust.xml").write_text(f"""<network-security-config>
  <base-config cleartextTrafficPermitted="false"><trust-anchors><certificates src="system"/></trust-anchors></base-config>
  <domain-config><domain>{ip}</domain><trust-anchors><certificates src="@raw/ridge_lab_ca"/></trust-anchors></domain-config>
</network-security-config>
""")
(res / "values/lab.xml").write_text(f'<resources><string name="service_hint">https://{ip}:8443</string></resources>\n')
for n,name in enumerate(("Radio A","Radio B","Radio C"),1):
    dial = f"164*123*456{n}"
    result = subprocess.run([str(args.binary.resolve()), "invite", dial, name], env={**os.environ,**values},
                            capture_output=True, text=True, check=True)
    (state / f"invite-{n}.txt").write_text(f"{name}\n{dial}\n{result.stdout.strip()}\n")
print(f"Initialized {state}\nService URL: https://{ip}:8443\nEnrollment files: invite-1.txt through invite-3.txt")
